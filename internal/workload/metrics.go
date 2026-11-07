package workload

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sclient "k8s.io/client-go/kubernetes"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

// AggregateUsage returns actual CPU and memory usage grouped by
// toggleable Deployment.
//
// Ownership is resolved through Pod -> ReplicaSet -> Deployment.
// UID matching prevents metrics from being attributed to a different
// Deployment that happens to reuse the same name.
func AggregateUsage(
	ctx context.Context,
	client k8sclient.Interface,
	metrics metricsclient.Interface,
	deployments []Deployment,
) (map[string]Usage, error) {
	result := make(map[string]Usage)

	if len(deployments) == 0 {
		return result, nil
	}

	// Group eligible Deployments by namespace and UID.
	deploymentUIDs := make(map[string]map[string]struct{})
	for _, deployment := range deployments {
		if deployment.UID == "" {
			continue
		}

		if deploymentUIDs[deployment.Namespace] == nil {
			deploymentUIDs[deployment.Namespace] = make(map[string]struct{})
		}

		deploymentUIDs[deployment.Namespace][string(deployment.UID)] = struct{}{}
	}

	for namespace, eligibleUIDs := range deploymentUIDs {
		// Map ReplicaSet UID to its owning toggleable Deployment.
		replicaSets, err := client.AppsV1().
			ReplicaSets(namespace).
			List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf(
				"list ReplicaSets in namespace %q: %w",
				namespace,
				err,
			)
		}

		replicaSetOwners := make(map[string]string)

		for _, replicaSet := range replicaSets.Items {
			for _, owner := range replicaSet.OwnerReferences {
				if owner.Kind != "Deployment" {
					continue
				}

				if _, eligible := eligibleUIDs[string(owner.UID)]; !eligible {
					continue
				}

				replicaSetOwners[string(replicaSet.UID)] =
					namespace + "/" + owner.Name

				break
			}
		}

		if len(replicaSetOwners) == 0 {
			continue
		}

		// Map Pod names to their owning Deployment.
		pods, err := client.CoreV1().
			Pods(namespace).
			List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf(
				"list Pods in namespace %q: %w",
				namespace,
				err,
			)
		}

		podOwners := make(map[string]string)

		for _, pod := range pods.Items {
			// Ignore Pods that are being terminated.
			if pod.DeletionTimestamp != nil {
				continue
			}

			for _, owner := range pod.OwnerReferences {
				if owner.Kind != "ReplicaSet" {
					continue
				}

				deploymentKey, found := replicaSetOwners[string(owner.UID)]
				if !found {
					continue
				}

				podOwners[pod.Name] = deploymentKey
				break
			}
		}

		if len(podOwners) == 0 {
			continue
		}

		// Retrieve actual usage from the Kubernetes Metrics API.
		podMetrics, err := metrics.MetricsV1beta1().
			PodMetricses(namespace).
			List(ctx, metav1.ListOptions{})

		if err != nil {
			return nil, fmt.Errorf(
				"list Pod metrics in namespace %q: %w",
				namespace,
				err,
			)
		}

		for _, podMetric := range podMetrics.Items {
			deploymentKey, found := podOwners[podMetric.Name]
			if !found {
				continue
			}

			var podUsage Usage

			for _, container := range podMetric.Containers {
				podUsage.CPUUsageMilli += container.Usage.Cpu().MilliValue()
				podUsage.MemoryUsageBytes += container.Usage.Memory().Value()
			}

			deploymentUsage := result[deploymentKey]
			deploymentUsage.CPUUsageMilli += podUsage.CPUUsageMilli
			deploymentUsage.MemoryUsageBytes += podUsage.MemoryUsageBytes

			result[deploymentKey] = deploymentUsage
		}
	}

	return result, nil
}

// UsageKey returns the map key used to identify a Deployment's usage.
func UsageKey(namespace, name string) string {
	return namespace + "/" + name
}

// Ensure the core API resource import remains explicit for ownership-related
// Kubernetes types used by this package.
var _ = corev1.ResourceCPU
