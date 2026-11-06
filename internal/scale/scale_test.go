package scale

import (
	"context"
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const (
	testNamespace = "portfolio"
	testName      = "portfolio"
)

func newTestManager(t *testing.T, replicas int32, annotations map[string]string) (*Manager, *fake.Clientset) {
	t.Helper()

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        testName,
			Namespace:   testNamespace,
			Labels:      map[string]string{"kubetoggle.io/toggleable": "true"},
			Annotations: annotations,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
		},
	}

	client := fake.NewSimpleClientset(deployment)

	// Simulate GET deployments/<name>/scale.
	client.PrependReactor("get", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "scale" {
			return false, nil, nil
		}

		getAction := action.(k8stesting.GetAction)

		obj, err := client.Tracker().Get(
			appsv1.SchemeGroupVersion.WithResource("deployments"),
			action.GetNamespace(),
			getAction.GetName(),
		)

		if err != nil {
			return true, nil, err
		}

		dep := obj.(*appsv1.Deployment)

		return true, &autoscalingv1.Scale{
			ObjectMeta: metav1.ObjectMeta{
				Name:            dep.Name,
				Namespace:       dep.Namespace,
				ResourceVersion: dep.ResourceVersion,
			},
			Spec: autoscalingv1.ScaleSpec{
				Replicas: replicaCount(dep),
			},
		}, nil
	})

	// Simulate PUT deployments/<name>/scale.
	client.PrependReactor("update", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "scale" {
			return false, nil, nil
		}

		updateAction := action.(k8stesting.UpdateAction)
		scaleObject := updateAction.GetObject().(*autoscalingv1.Scale)

		obj, err := client.Tracker().Get(
			appsv1.SchemeGroupVersion.WithResource("deployments"),
			action.GetNamespace(),
			scaleObject.Name,
		)
		if err != nil {
			return true, nil, err
		}

		dep := obj.(*appsv1.Deployment)
		replicas := scaleObject.Spec.Replicas
		dep.Spec.Replicas = &replicas

		if err := client.Tracker().Update(
			appsv1.SchemeGroupVersion.WithResource("deployments"),
			dep,
			action.GetNamespace(),
		); err != nil {
			return true, nil, err
		}

		return true, scaleObject, nil
	})

	return NewManager(client), client
}

func getTestDeployment(t *testing.T, client *fake.Clientset) *appsv1.Deployment {
	t.Helper()

	deployment, err := client.AppsV1().Deployments(testNamespace).Get(
		context.Background(),
		testName,
		metav1.GetOptions{},
	)
	if err != nil {
		t.Fatalf("get Deployment: %v", err)
	}

	return deployment
}

func TestOffSavesReplicasAndScalesToZero(t *testing.T) {
	manager, client := newTestManager(t, 3, nil)

	if err := manager.Off(context.Background(), testNamespace, testName); err != nil {
		t.Fatalf("Off() error = %v", err)
	}

	deployment := getTestDeployment(t, client)

	if got := replicaCount(deployment); got != 0 {
		t.Errorf("replicas = %d, want 0", got)
	}

	if got := deployment.Annotations[PreviousReplicasAnnotation]; got != "3" {
		t.Errorf("saved replicas = %q, want %q", got, "3")
	}
}

func TestOffPreservesSavedReplicasWhenAlreadyOff(t *testing.T) {
	manager, client := newTestManager(t, 0, map[string]string{
		PreviousReplicasAnnotation: "3",
	})

	if err := manager.Off(context.Background(), testNamespace, testName); err != nil {
		t.Fatalf("Off() error = %v", err)
	}

	deployment := getTestDeployment(t, client)

	if got := deployment.Annotations[PreviousReplicasAnnotation]; got != "3" {
		t.Errorf("saved replicas = %q, want %q", got, "3")
	}
}

func TestOnRestoresSavedReplicas(t *testing.T) {
	manager, client := newTestManager(t, 0, map[string]string{
		PreviousReplicasAnnotation: "3",
	})

	if err := manager.On(context.Background(), testNamespace, testName); err != nil {
		t.Fatalf("On() error = %v", err)
	}

	deployment := getTestDeployment(t, client)

	if got := replicaCount(deployment); got != 3 {
		t.Errorf("replicas = %d, want 3", got)
	}

	if _, exists := deployment.Annotations[PreviousReplicasAnnotation]; exists {
		t.Error("previous-replicas annotation was not removed")
	}
}

func TestOnDefaultsToOneWithoutSavedReplicas(t *testing.T) {
	manager, client := newTestManager(t, 0, nil)

	if err := manager.On(context.Background(), testNamespace, testName); err != nil {
		t.Fatalf("On() error = %v", err)
	}

	if got := replicaCount(getTestDeployment(t, client)); got != 1 {
		t.Errorf("replicas = %d, want 1", got)
	}
}

func TestOnDefaultsToOneForInvalidSavedReplicas(t *testing.T) {
	manager, client := newTestManager(t, 0, map[string]string{
		PreviousReplicasAnnotation: "invalid",
	})

	if err := manager.On(context.Background(), testNamespace, testName); err != nil {
		t.Fatalf("On() error = %v", err)
	}

	deployment := getTestDeployment(t, client)

	if got := replicaCount(deployment); got != 1 {
		t.Errorf("replicas = %d, want 1", got)
	}

	if _, exists := deployment.Annotations[PreviousReplicasAnnotation]; exists {
		t.Error("invalid previous-replicas annotation was not removed")
	}
}

func TestOnPreservesAnnotationWhenScaleFails(t *testing.T) {
	manager, client := newTestManager(t, 0, map[string]string{
		PreviousReplicasAnnotation: "3",
	})

	failNextScale := true

	client.PrependReactor("update", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() == "scale" && failNextScale {
			failNextScale = false
			return true, nil, errors.New("simulated scale failure")
		}
		return false, nil, nil
	})

	err := manager.On(context.Background(), testNamespace, testName)
	if err == nil {
		t.Fatal("On() error = nil, want simulated scale failure")
	}

	deployment := getTestDeployment(t, client)

	if got := deployment.Annotations[PreviousReplicasAnnotation]; got != "3" {
		t.Fatalf("saved replicas after failure = %q, want %q", got, "3")
	}

	// Retry after the simulated API failure.
	if err := manager.On(context.Background(), testNamespace, testName); err != nil {
		t.Fatalf("On() retry error = %v", err)
	}

	deployment = getTestDeployment(t, client)

	if got := replicaCount(deployment); got != 3 {
		t.Errorf("replicas after retry = %d, want 3", got)
	}

	if _, exists := deployment.Annotations[PreviousReplicasAnnotation]; exists {
		t.Error("previous-replicas annotation remains after successful retry")
	}
}

func TestOffRejectsNonToggleableDeployment(t *testing.T) {
	manager, client := newTestManager(t, 2, nil)

	deployment := getTestDeployment(t, client)
	delete(deployment.Labels, "kubetoggle.io/toggleable")

	_, err := client.AppsV1().Deployments(testNamespace).Update(
		context.Background(),
		deployment,
		metav1.UpdateOptions{},
	)
	if err != nil {
		t.Fatalf("update test Deployment: %v", err)
	}

	err = manager.Off(context.Background(), testNamespace, testName)
	if err == nil {
		t.Fatal("Off() error = nil, want eligibility error")
	}
}

func createTestHPA(t *testing.T, client *fake.Clientset, targetName string) {
	t.Helper()

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-hpa",
			Namespace: testNamespace,
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       targetName,
			},
			MinReplicas: func() *int32 {
				value := int32(1)
				return &value
			}(),
			MaxReplicas: 3,
		},
	}

	_, err := client.AutoscalingV2().
		HorizontalPodAutoscalers(testNamespace).
		Create(context.Background(), hpa, metav1.CreateOptions{})

	if err != nil {
		t.Fatalf("create test HPA: %v", err)
	}
}

func TestOffRejectsDeploymentManagedByHPA(t *testing.T) {
	manager, client := newTestManager(t, 2, nil)
	createTestHPA(t, client, testName)

	err := manager.Off(context.Background(), testNamespace, testName)
	if err == nil {
		t.Fatal("Off() error = nil, want HPA protection error")
	}

	if !strings.Contains(err.Error(), "managed by HPA") {
		t.Errorf("unexpected error: %v", err)
	}

	deployment := getTestDeployment(t, client)

	if got := replicaCount(deployment); got != 2 {
		t.Errorf("replicas = %d, want 2; Deployment should remain unchanged", got)
	}

	if _, exists := deployment.Annotations[PreviousReplicasAnnotation]; exists {
		t.Error("previous-replicas annotation should not be created")
	}
}

func TestOnRejectsDeploymentManagedByHPA(t *testing.T) {
	manager, client := newTestManager(t, 0, map[string]string{
		PreviousReplicasAnnotation: "2",
	})
	createTestHPA(t, client, testName)

	err := manager.On(context.Background(), testNamespace, testName)
	if err == nil {
		t.Fatal("On() error = nil, want HPA protection error")
	}

	if !strings.Contains(err.Error(), "managed by HPA") {
		t.Errorf("unexpected error: %v", err)
	}

	deployment := getTestDeployment(t, client)

	if got := replicaCount(deployment); got != 0 {
		t.Errorf("replicas = %d, want 0; Deployment should remain unchanged", got)
	}

	if got := deployment.Annotations[PreviousReplicasAnnotation]; got != "2" {
		t.Errorf("saved replicas = %q, want %q", got, "2")
	}
}

func TestOffAllowsDeploymentWhenHPATargetsAnotherDeployment(t *testing.T) {
	manager, client := newTestManager(t, 2, nil)
	createTestHPA(t, client, "another-deployment")

	if err := manager.Off(context.Background(), testNamespace, testName); err != nil {
		t.Fatalf("Off() error = %v, want nil", err)
	}

	deployment := getTestDeployment(t, client)

	if got := replicaCount(deployment); got != 0 {
		t.Errorf("replicas = %d, want 0", got)
	}

	if got := deployment.Annotations[PreviousReplicasAnnotation]; got != "2" {
		t.Errorf("saved replicas = %q, want %q", got, "2")
	}
}
