package workload

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsfake "k8s.io/metrics/pkg/client/clientset/versioned/fake"
)

func TestAggregateUsage(t *testing.T) {
	now := metav1.NewTime(time.Now())

	deployment := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "strapi",
			Namespace: "apps",
			UID:       types.UID("deployment-uid"),
		},
	}

	replicaSet := appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "strapi-abc123",
			Namespace: "apps",
			UID:       types.UID("replicaset-uid"),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "strapi",
					UID:        deployment.UID,
				},
			},
		},
	}

	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "strapi-abc123-xyz",
			Namespace: "apps",
			UID:       types.UID("pod-uid"),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "ReplicaSet",
					Name:       replicaSet.Name,
					UID:        replicaSet.UID,
				},
			},
		},
	}

	tests := []struct {
		name        string
		deployments []Deployment
		replicaSets []appsv1.ReplicaSet
		pods        []corev1.Pod
		podMetrics  []metricsv1beta1.PodMetrics
		expected    map[string]Usage
	}{
		{
			name: "aggregates CPU and memory through the ownership chain",
			deployments: []Deployment{
				{
					UID:       deployment.UID,
					Namespace: "apps",
					Name:      "strapi",
				},
			},
			replicaSets: []appsv1.ReplicaSet{replicaSet},
			pods:        []corev1.Pod{pod},
			podMetrics: []metricsv1beta1.PodMetrics{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      pod.Name,
						Namespace: pod.Namespace,
					},
					Containers: []metricsv1beta1.ContainerMetrics{
						{
							Name: "strapi",
							Usage: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("250m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
						{
							Name: "sidecar",
							Usage: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("50m"),
								corev1.ResourceMemory: resource.MustParse("32Mi"),
							},
						},
					},
				},
			},
			expected: map[string]Usage{
				"apps/strapi": {
					CPUUsageMilli:    300,
					MemoryUsageBytes: 160 * 1024 * 1024,
				},
			},
		},
		{
			name: "ignores ReplicaSets owned by a different Deployment UID",
			deployments: []Deployment{
				{
					UID:       types.UID("new-deployment-uid"),
					Namespace: "apps",
					Name:      "strapi",
				},
			},
			replicaSets: []appsv1.ReplicaSet{replicaSet},
			pods:        []corev1.Pod{pod},
			podMetrics: []metricsv1beta1.PodMetrics{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      pod.Name,
						Namespace: pod.Namespace,
					},
					Containers: []metricsv1beta1.ContainerMetrics{
						{
							Name: "strapi",
							Usage: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("250m"),
							},
						},
					},
				},
			},
			expected: map[string]Usage{},
		},
		{
			name: "ignores Pods without a matching ReplicaSet owner",
			deployments: []Deployment{
				{
					UID:       deployment.UID,
					Namespace: "apps",
					Name:      "strapi",
				},
			},
			replicaSets: []appsv1.ReplicaSet{replicaSet},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "unrelated-pod",
						Namespace: "apps",
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind: "ReplicaSet",
								UID:  types.UID("unknown-replicaset-uid"),
							},
						},
					},
				},
			},
			podMetrics: []metricsv1beta1.PodMetrics{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "unrelated-pod",
						Namespace: "apps",
					},
					Containers: []metricsv1beta1.ContainerMetrics{
						{
							Name: "app",
							Usage: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("500m"),
							},
						},
					},
				},
			},
			expected: map[string]Usage{},
		},
		{
			name: "ignores terminating Pods",
			deployments: []Deployment{
				{
					UID:       deployment.UID,
					Namespace: "apps",
					Name:      "strapi",
				},
			},
			replicaSets: []appsv1.ReplicaSet{replicaSet},
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              pod.Name,
						Namespace:         pod.Namespace,
						DeletionTimestamp: &now,
						OwnerReferences:   pod.OwnerReferences,
					},
				},
			},
			podMetrics: []metricsv1beta1.PodMetrics{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      pod.Name,
						Namespace: pod.Namespace,
					},
					Containers: []metricsv1beta1.ContainerMetrics{
						{
							Name: "strapi",
							Usage: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("250m"),
							},
						},
					},
				},
			},
			expected: map[string]Usage{},
		},
		{
			name:        "returns empty usage when no Deployments are supplied",
			deployments: nil,
			expected:    map[string]Usage{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kubeClient := k8sfake.NewSimpleClientset()
			metricsClient := metricsfake.NewSimpleClientset()

			for i := range tt.replicaSets {
				if _, err := kubeClient.AppsV1().
					ReplicaSets(tt.replicaSets[i].Namespace).
					Create(context.Background(), &tt.replicaSets[i], metav1.CreateOptions{}); err != nil {
					t.Fatalf("create fake ReplicaSet: %v", err)
				}
			}

			for i := range tt.pods {
				if _, err := kubeClient.CoreV1().
					Pods(tt.pods[i].Namespace).
					Create(context.Background(), &tt.pods[i], metav1.CreateOptions{}); err != nil {
					t.Fatalf("create fake Pod: %v", err)
				}
			}

			for i := range tt.podMetrics {
				if err := metricsClient.Tracker().Add(
					&tt.podMetrics[i],
				); err != nil {
					t.Fatalf("add fake Pod metrics: %v", err)
				}
			}

			got, err := AggregateUsage(
				context.Background(),
				kubeClient,
				metricsClient,
				tt.deployments,
			)
			if err != nil {
				t.Fatalf("AggregateUsage() error = %v", err)
			}

			if len(got) != len(tt.expected) {
				t.Fatalf("AggregateUsage() returned %d entries, want %d: %#v",
					len(got), len(tt.expected), got)
			}

			for key, expected := range tt.expected {
				if got[key] != expected {
					t.Errorf("usage[%q] = %+v, want %+v",
						key, got[key], expected)
				}
			}
		})
	}
}

func TestUsageKey(t *testing.T) {
	got := UsageKey("apps", "strapi")
	want := "apps/strapi"

	if got != want {
		t.Errorf("UsageKey() = %q, want %q", got, want)
	}
}
