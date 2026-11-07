package workload

import (
	"context"
	"reflect"
	"testing"

	"github.com/zahaanmahajan/kubetoggle/internal/safety"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDiscover(t *testing.T) {
	tests := []struct {
		name     string
		objects  []appsv1.Deployment
		expected []Deployment
	}{
		{
			name: "discovers toggleable deployments and groups by Argo CD application",
			objects: []appsv1.Deployment{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "nextcloud",
						Namespace: "nextcloud",
						Labels: map[string]string{
							safety.ToggleLabel: "true",
						},
						Annotations: map[string]string{
							ArgoTrackingID: "nextcloud:apps/Deployment:nextcloud/nextcloud",
						},
					},
					Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(2)},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "redis",
						Namespace: "nextcloud",
						Labels: map[string]string{
							safety.ToggleLabel: "true",
						},
						Annotations: map[string]string{
							ArgoTrackingID: "nextcloud:apps/Deployment:nextcloud/redis",
						},
					},
					Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(1)},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "portfolio",
						Namespace: "portfolio",
						Labels: map[string]string{
							safety.ToggleLabel: "true",
							ArgoInstance:       "portfolio",
						},
					},
					Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(1)},
				},
			},
			expected: []Deployment{
				{Namespace: "nextcloud", Name: "nextcloud", Application: "nextcloud", Replicas: 2},
				{Namespace: "nextcloud", Name: "redis", Application: "nextcloud", Replicas: 1},
				{Namespace: "portfolio", Name: "portfolio", Application: "portfolio", Replicas: 1},
			},
		},
		{
			name: "ignores protected and non-toggleable deployments",
			objects: []appsv1.Deployment{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "argocd-server",
						Namespace: "argocd",
						Labels: map[string]string{
							safety.ToggleLabel: "true",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "unmanaged",
						Namespace: "portfolio",
						Labels:    map[string]string{},
					},
				},
			},
			expected: []Deployment{},
		},
		{
			name: "defaults nil replicas to one",
			objects: []appsv1.Deployment{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "default-replicas",
						Namespace: "portfolio",
						Labels: map[string]string{
							safety.ToggleLabel: "true",
						},
					},
				},
			},
			expected: []Deployment{
				{Namespace: "portfolio", Name: "default-replicas", Replicas: 1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtimeObjects := make([]interface{}, 0, len(tt.objects))
			for i := range tt.objects {
				runtimeObjects = append(runtimeObjects, &tt.objects[i])
			}

			client := fake.NewSimpleClientset()
			for _, object := range runtimeObjects {
				deployment := object.(*appsv1.Deployment)
				if _, err := client.AppsV1().Deployments(deployment.Namespace).Create(
					context.Background(),
					deployment,
					metav1.CreateOptions{},
				); err != nil {
					t.Fatalf("create fake Deployment: %v", err)
				}
			}

			got, err := Discover(context.Background(), client)
			if err != nil {
				t.Fatalf("Discover() error = %v", err)
			}

			if len(got) != len(tt.expected) {
				t.Fatalf("Discover() returned %d deployments, want %d: %#v",
					len(got), len(tt.expected), got)
			}

			for i := range tt.expected {
				if !reflect.DeepEqual(got[i], tt.expected[i]) {
					t.Errorf("deployment[%d] = %#v, want %#v",
						i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func int32Ptr(value int32) *int32 {
	return &value
}

func TestPVCNames(t *testing.T) {
	deployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "nextcloud-data",
								},
							},
						},
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "nextcloud-config",
								},
							},
						},
						{
							Name: "duplicate",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "nextcloud-data",
								},
							},
						},
						{
							Name: "config-map",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{},
							},
						},
					},
				},
			},
		},
	}

	got := PVCNames(deployment)

	expected := []string{
		"nextcloud-config",
		"nextcloud-data",
	}

	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("pvcNames() = %v, want %v", got, expected)
	}
}

func TestSummarize(t *testing.T) {
	deployments := []Deployment{
		{
			Name:     "strapi",
			Replicas: 2,
			Resources: Resources{
				CPURequestMilli:    500,
				MemoryRequestBytes: 512 * 1024 * 1024,
				CPULimitMilli:      1000,
				MemoryLimitBytes:   1024 * 1024 * 1024,
			},
		},
		{
			Name:     "linkding",
			Replicas: 1,
			Resources: Resources{
				CPURequestMilli:    250,
				MemoryRequestBytes: 256 * 1024 * 1024,
				CPULimitMilli:      500,
				MemoryLimitBytes:   512 * 1024 * 1024,
			},
		},
		{
			Name:     "nextcloud",
			Replicas: 0,
			Resources: Resources{
				CPURequestMilli:    1000,
				MemoryRequestBytes: 1024 * 1024 * 1024,
				CPULimitMilli:      2000,
				MemoryLimitBytes:   2 * 1024 * 1024 * 1024,
			},
		},
	}

	got := Summarize(deployments)

	if got.TotalWorkloads != 3 {
		t.Errorf("TotalWorkloads = %d, want 3", got.TotalWorkloads)
	}

	if got.RunningWorkloads != 2 {
		t.Errorf("RunningWorkloads = %d, want 2", got.RunningWorkloads)
	}

	if got.StoppedWorkloads != 1 {
		t.Errorf("StoppedWorkloads = %d, want 1", got.StoppedWorkloads)
	}

	if got.RunningReplicas != 3 {
		t.Errorf("RunningReplicas = %d, want 3", got.RunningReplicas)
	}

	expected := Resources{
		CPURequestMilli:    1250,
		MemoryRequestBytes: 1280 * 1024 * 1024,
		CPULimitMilli:      2500,
		MemoryLimitBytes:   2560 * 1024 * 1024,
	}

	if got.RunningResources != expected {
		t.Errorf("RunningResources = %+v, want %+v", got.RunningResources, expected)
	}

	if got.RecoverableResources != expected {
		t.Errorf("RecoverableResources = %+v, want %+v", got.RecoverableResources, expected)
	}
}

func TestSummarizeEmpty(t *testing.T) {
	got := Summarize(nil)

	if got != (ResourceSummary{}) {
		t.Errorf("Summarize(nil) = %+v, want zero-value summary", got)
	}
}
