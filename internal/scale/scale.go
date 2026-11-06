package scale

import (
	"context"
	"fmt"
	"strconv"

	"github.com/zahaanmahajan/kubetoggle/internal/safety"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubernetes "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

const PreviousReplicasAnnotation = "kubetoggle.io/previous-replicas"

type Manager struct {
	client kubernetes.Interface
}

func NewManager(client kubernetes.Interface) *Manager {
	return &Manager{client: client}
}

// Off saves the current replica count and scales the Deployment to zero.
func (m *Manager) Off(ctx context.Context, namespace, name string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		deployment, err := m.getEligibleDeployment(ctx, namespace, name)
		if err != nil {
			return err
		}

		current := replicaCount(deployment)

		// Already disabled: preserve the previously saved count.
		if current == 0 {
			return nil
		}

		// Save the original replica count before scaling down.
		if deployment.Annotations == nil {
			deployment.Annotations = make(map[string]string)
		}

		deployment.Annotations[PreviousReplicasAnnotation] = strconv.FormatInt(int64(current), 10)

		_, err = m.client.AppsV1().Deployments(namespace).Update(
			ctx,
			deployment,
			metav1.UpdateOptions{},
		)
		if err != nil {
			return err
		}

		return m.updateScale(ctx, namespace, name, 0)
	})
}

// On restores the previous replica count and removes the saved annotation.
func (m *Manager) On(ctx context.Context, namespace, name string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		deployment, err := m.getEligibleDeployment(ctx, namespace, name)
		if err != nil {
			return err
		}

		current := replicaCount(deployment)

		// Already running and no saved state: nothing to do.
		if current > 0 {
			if deployment.Annotations == nil {
				return nil
			}
			if _, exists := deployment.Annotations[PreviousReplicasAnnotation]; !exists {
				return nil
			}
		}

		target := int32(1)

		if deployment.Annotations != nil {
			if value, exists := deployment.Annotations[PreviousReplicasAnnotation]; exists {
				parsed, parseErr := strconv.ParseInt(value, 10, 32)
				if parseErr == nil && parsed > 0 {
					target = int32(parsed)
				}
			}
		}

		// Restore scale first. Keep the annotation if this operation fails.
		if err := m.updateScale(ctx, namespace, name, target); err != nil {
			return err
		}

		// Remove saved state only after the scale operation succeeds.
		if deployment.Annotations != nil {
			if _, exists := deployment.Annotations[PreviousReplicasAnnotation]; exists {
				patch := []byte(`{
					"metadata": {
						"annotations": {
							"` + PreviousReplicasAnnotation + `": null
						}
					}
				}`)

				_, err = m.client.AppsV1().Deployments(namespace).Patch(
					ctx,
					name,
					types.MergePatchType,
					patch,
					metav1.PatchOptions{},
				)
				if err != nil {
					return fmt.Errorf(
						"remove saved replica annotation from %s/%s: %w",
						namespace,
						name,
						err,
					)
				}
			}
		}

		return nil
	})
}

func (m *Manager) getEligibleDeployment(
	ctx context.Context,
	namespace string,
	name string,
) (*appsv1.Deployment, error) {
	deployment, err := m.client.AppsV1().Deployments(namespace).Get(
		ctx,
		name,
		metav1.GetOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("get Deployment %s/%s: %w", namespace, name, err)
	}

	if !safety.IsEligible(namespace, deployment.Labels) {
		return nil, fmt.Errorf(
			"Deployment %s/%s is not eligible for kubetoggle",
			namespace,
			name,
		)
	}

	if err := m.ensureNoHPA(ctx, namespace, name); err != nil {
		return nil, err
	}

	return deployment, nil
}

func (m *Manager) updateScale(
	ctx context.Context,
	namespace string,
	name string,
	replicas int32,
) error {
	scale, err := m.client.AppsV1().Deployments(namespace).GetScale(
		ctx,
		name,
		metav1.GetOptions{},
	)
	if err != nil {
		return fmt.Errorf("get scale for %s/%s: %w", namespace, name, err)
	}

	scale.Spec.Replicas = replicas

	_, err = m.client.AppsV1().Deployments(namespace).UpdateScale(
		ctx,
		name,
		scale,
		metav1.UpdateOptions{},
	)
	if err != nil {
		return fmt.Errorf("update scale for %s/%s: %w", namespace, name, err)
	}

	return nil
}

func replicaCount(deployment *appsv1.Deployment) int32 {
	if deployment.Spec.Replicas == nil {
		return 1
	}
	return *deployment.Spec.Replicas
}

// IsNotFound reports whether the error represents a missing Deployment.
func IsNotFound(err error) bool {
	return apierrors.IsNotFound(err)
}

func (m *Manager) ensureNoHPA(
	ctx context.Context,
	namespace string,
	deploymentName string,
) error {
	hpas, err := m.client.AutoscalingV2().
		HorizontalPodAutoscalers(namespace).
		List(ctx, metav1.ListOptions{})

	if err != nil {
		return fmt.Errorf(
			"list HPAs in namespace %s: %w",
			namespace,
			err,
		)
	}

	for _, hpa := range hpas.Items {
		target := hpa.Spec.ScaleTargetRef

		if target.Kind == "Deployment" &&
			target.Name == deploymentName {
			return fmt.Errorf(
				"Deployment %s/%s is managed by HPA %s; refusing to scale",
				namespace,
				deploymentName,
				hpa.Name,
			)
		}
	}

	return nil
}
