package workload

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/zahaanmahajan/kubetoggle/internal/safety"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sclient "k8s.io/client-go/kubernetes"
)

const (
	ArgoTrackingID = "argocd.argoproj.io/tracking-id"
	ArgoInstance   = "app.kubernetes.io/instance"
)

// Resources contains the effective resource requirements for one Pod.
// Values are stored in millicores and bytes.
type Resources struct {
	CPURequestMilli    int64
	MemoryRequestBytes int64
	CPULimitMilli      int64
	MemoryLimitBytes   int64
}

// Usage represents actual CPU and memory consumption reported
// by the Kubernetes Metrics API.
type Usage struct {
	CPUUsageMilli    int64
	MemoryUsageBytes int64
}

type Deployment struct {
	UID         types.UID
	Namespace   string
	Name        string
	Application string
	Replicas    int32
	PVCs        []string
	Resources   Resources
}

// ResourceSummary aggregates the resource reservations of toggleable Deployments.
type ResourceSummary struct {
	TotalWorkloads   int
	RunningWorkloads int
	StoppedWorkloads int
	RunningReplicas  int64

	// Resources currently reserved by running toggleable Deployments.
	RunningResources Resources

	// Resources that could potentially be released by disabling all
	// currently running toggleable Deployments.
	RecoverableResources Resources
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
			UID:         deployment.UID,
			Namespace:   deployment.Namespace,
			Name:        deployment.Name,
			Application: applicationName(&deployment),
			Replicas:    replicaCount(&deployment),
			PVCs:        PVCNames(&deployment),
			Resources:   PodResources(deployment.Spec.Template.Spec),
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

// PodResources calculates effective CPU and memory requirements for one Pod.
//
// It accounts for:
//   - Regular application containers.
//   - Regular init containers.
//   - Restartable init sidecars.
//   - Pod overhead.
//
// The calculation follows Kubernetes' effective per-Pod resource accounting:
// steady-state application requirements are compared against the peak
// requirements during initialization.
func PodResources(podSpec corev1.PodSpec) Resources {
	var applicationRequests corev1.ResourceList
	var applicationLimits corev1.ResourceList

	applicationRequests = make(corev1.ResourceList)
	applicationLimits = make(corev1.ResourceList)

	for _, container := range podSpec.Containers {
		addResourceList(applicationRequests, container.Resources.Requests)
		addResourceList(applicationLimits, container.Resources.Limits)
	}

	restartableInitRequests := make(corev1.ResourceList)
	restartableInitLimits := make(corev1.ResourceList)

	maxInitRequests := make(corev1.ResourceList)
	maxInitLimits := make(corev1.ResourceList)

	for _, container := range podSpec.InitContainers {
		if isRestartableInitContainer(container) {
			addResourceList(restartableInitRequests, container.Resources.Requests)
			addResourceList(restartableInitLimits, container.Resources.Limits)

			maxResourceList(maxInitRequests, restartableInitRequests)
			maxResourceList(maxInitLimits, restartableInitLimits)

			continue
		}

		peakRequests := copyResourceList(restartableInitRequests)
		peakLimits := copyResourceList(restartableInitLimits)

		addResourceList(peakRequests, container.Resources.Requests)
		addResourceList(peakLimits, container.Resources.Limits)

		maxResourceList(maxInitRequests, peakRequests)
		maxResourceList(maxInitLimits, peakLimits)
	}

	// Restartable init containers continue running alongside application
	// containers, so include them in steady-state requirements.
	steadyRequests := copyResourceList(applicationRequests)
	steadyLimits := copyResourceList(applicationLimits)

	addResourceList(steadyRequests, restartableInitRequests)
	addResourceList(steadyLimits, restartableInitLimits)

	// Effective requirements are the greater of steady-state and
	// initialization requirements, calculated independently per resource.
	effectiveRequests := copyResourceList(steadyRequests)
	effectiveLimits := copyResourceList(steadyLimits)

	maxResourceList(effectiveRequests, maxInitRequests)
	maxResourceList(effectiveLimits, maxInitLimits)

	// Pod overhead contributes to scheduling requests.
	addResourceList(effectiveRequests, podSpec.Overhead)

	return Resources{
		CPURequestMilli:    quantityMilli(effectiveRequests, corev1.ResourceCPU),
		MemoryRequestBytes: quantityValue(effectiveRequests, corev1.ResourceMemory),
		CPULimitMilli:      quantityMilli(effectiveLimits, corev1.ResourceCPU),
		MemoryLimitBytes:   quantityValue(effectiveLimits, corev1.ResourceMemory),
	}
}

func isRestartableInitContainer(container corev1.Container) bool {
	return container.RestartPolicy != nil &&
		*container.RestartPolicy == corev1.ContainerRestartPolicyAlways
}

func addResourceList(destination, source corev1.ResourceList) {
	for name, quantity := range source {
		current := destination[name]
		current.Add(quantity)
		destination[name] = current
	}
}

func maxResourceList(destination, candidate corev1.ResourceList) {
	for name, quantity := range candidate {
		current := destination[name]

		if quantity.Cmp(current) > 0 {
			destination[name] = quantity.DeepCopy()
		}
	}
}

func copyResourceList(source corev1.ResourceList) corev1.ResourceList {
	result := make(corev1.ResourceList, len(source))

	for name, quantity := range source {
		result[name] = quantity.DeepCopy()
	}

	return result
}

func quantityMilli(resources corev1.ResourceList, name corev1.ResourceName) int64 {
	quantity, exists := resources[name]
	if !exists {
		return 0
	}

	return quantity.MilliValue()
}

func quantityValue(resources corev1.ResourceList, name corev1.ResourceName) int64 {
	quantity, exists := resources[name]
	if !exists {
		return 0
	}

	return quantity.Value()
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

// PVCNames returns the unique PVC names directly referenced by a Deployment.
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

// Summarize aggregates workload counts and resource reservations.
//
// Resource values are calculated by multiplying each Deployment's
// per-Pod resources by its current replica count.
func Summarize(deployments []Deployment) ResourceSummary {
	summary := ResourceSummary{
		TotalWorkloads: len(deployments),
	}

	for _, deployment := range deployments {
		if deployment.Replicas <= 0 {
			summary.StoppedWorkloads++
			continue
		}

		summary.RunningWorkloads++
		summary.RunningReplicas += int64(deployment.Replicas)

		replicas := int64(deployment.Replicas)
		resources := deployment.Resources

		summary.RunningResources.CPURequestMilli +=
			resources.CPURequestMilli * replicas

		summary.RunningResources.MemoryRequestBytes +=
			resources.MemoryRequestBytes * replicas

		summary.RunningResources.CPULimitMilli +=
			resources.CPULimitMilli * replicas

		summary.RunningResources.MemoryLimitBytes +=
			resources.MemoryLimitBytes * replicas
	}

	// Disabling every currently running toggleable Deployment could
	// release its current resource reservations.
	summary.RecoverableResources = summary.RunningResources

	return summary
}
