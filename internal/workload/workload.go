package workload

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/zahaanmahajan/kubetoggle/internal/safety"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sclient "k8s.io/client-go/kubernetes"
)

const (
	ArgoTrackingID = "argocd.argoproj.io/tracking-id"
	ArgoInstance   = "app.kubernetes.io/instance"
)

type Deployment struct {
	Namespace   string
	Name        string
	Application string
	Replicas    int32
	PVCs        []string
}

// Discover lists Deployments explicitly opted in to kubetoggle.
func Discover(
	ctx context.Context,
	client k8sclient.Interface,
) ([]Deployment, error) {
	list, err := client.AppsV1().Deployments("").List(
		ctx,
		metav1.ListOptions{
			LabelSelector: safety.ToggleLabel + "=true",
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list toggleable Deployments: %w", err)
	}

	result := make([]Deployment, 0, len(list.Items))

	for _, deployment := range list.Items {
		if !safety.IsEligible(
			deployment.Namespace,
			deployment.Labels,
		) {
			continue
		}

		result = append(result, Deployment{
			Namespace:   deployment.Namespace,
			Name:        deployment.Name,
			Application: applicationName(&deployment),
			Replicas:    replicaCount(&deployment),
			PVCs:        PVCNames(&deployment),
		})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Application != result[j].Application {
			return result[i].Application < result[j].Application
		}

		if result[i].Namespace != result[j].Namespace {
			return result[i].Namespace < result[j].Namespace
		}

		return result[i].Name < result[j].Name
	})

	return result, nil
}

// applicationName resolves the owning Argo CD Application.
//
// Tracking ID takes precedence over the instance label.
func applicationName(deployment *appsv1.Deployment) string {
	if trackingID := deployment.Annotations[ArgoTrackingID]; trackingID != "" {
		application, _, found := strings.Cut(trackingID, ":")
		if found && application != "" {
			return application
		}
	}

	return deployment.Labels[ArgoInstance]
}

func replicaCount(deployment *appsv1.Deployment) int32 {
	if deployment.Spec.Replicas == nil {
		return 1
	}

	return *deployment.Spec.Replicas
}

// pvcNames returns the unique PVC names directly referenced by a Deployment.
func PVCNames(deployment *appsv1.Deployment) []string {
	seen := make(map[string]struct{})
	var names []string

	for _, volume := range deployment.Spec.Template.Spec.Volumes {
		claim := volume.PersistentVolumeClaim
		if claim == nil || claim.ClaimName == "" {
			continue
		}

		if _, exists := seen[claim.ClaimName]; exists {
			continue
		}

		seen[claim.ClaimName] = struct{}{}
		names = append(names, claim.ClaimName)
	}

	sort.Strings(names)
	return names
}
