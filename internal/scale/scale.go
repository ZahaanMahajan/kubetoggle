package scale

import (
	"context"
	"fmt"
	"log/slog"
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

// Plan describes a scaling operation without modifying the cluster.
type Plan struct {
	Namespace       string
	Deployment      string
	Enable          bool
	CurrentReplicas int32
	TargetReplicas  int32
	SavedReplicas   *int32
	Changes         bool
}

func NewManager(client kubernetes.Interface) *Manager {
	return &Manager{client: client}
}

// Off saves the current replica count and scales the Deployment to zero.
func (m *Manager) Off(ctx context.Context, namespace, name string) error {
	slog.InfoContext(ctx, "disable requested",
		"namespace", namespace,
		"deployment", name,
	)

	var previousReplicas int32
	alreadyDisabled := false

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		deployment, err := m.getEligibleDeployment(ctx, namespace, name)
		if err != nil {
			return err
		}

		current := replicaCount(deployment)

		// Already disabled: preserve the previously saved count.
		if current == 0 {
			alreadyDisabled = true
			return nil
		}

		previousReplicas = current

		if deployment.Annotations == nil {
			deployment.Annotations = make(map[string]string)
		}

		deployment.Annotations[PreviousReplicasAnnotation] =
			strconv.FormatInt(int64(current), 10)

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

	if err != nil {
		slog.ErrorContext(ctx, "disable operation failed",
			"namespace", namespace,
			"deployment", name,
			"error", err,
		)
		return err
	}

	if alreadyDisabled {
		slog.InfoContext(ctx, "deployment already disabled",
			"namespace", namespace,
			"deployment", name,
		)
		return nil
	}

	slog.InfoContext(ctx, "deployment scaled successfully",
		"namespace", namespace,
		"deployment", name,
		"previous_replicas", previousReplicas,
		"target_replicas", 0,
	)

	return nil
}

// On restores the previous replica count and removes the saved annotation.
func (m *Manager) On(ctx context.Context, namespace, name string) error {
	slog.InfoContext(ctx, "enable requested",
		"namespace", namespace,
		"deployment", name,
	)

	var currentReplicas int32
	var targetReplicas int32
	alreadyEnabled := false

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		deployment, err := m.getEligibleDeployment(ctx, namespace, name)
		if err != nil {
			return err
		}

		current := replicaCount(deployment)
		currentReplicas = current

		// Already running and no saved state: nothing to do.
		if current > 0 {
			if deployment.Annotations == nil {
				alreadyEnabled = true
				return nil
			}

			if _, exists := deployment.Annotations[PreviousReplicasAnnotation]; !exists {
				alreadyEnabled = true
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

		targetReplicas = target

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

	if err != nil {
		slog.ErrorContext(ctx, "enable operation failed",
			"namespace", namespace,
			"deployment", name,
			"error", err,
		)
		return err
	}

	if alreadyEnabled {
		slog.InfoContext(ctx, "deployment already enabled",
			"namespace", namespace,
			"deployment", name,
			"replicas", currentReplicas,
		)
		return nil
	}

	slog.InfoContext(ctx, "deployment scaled successfully",
		"namespace", namespace,
		"deployment", name,
		"previous_replicas", currentReplicas,
		"target_replicas", targetReplicas,
	)

	return nil
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
		slog.WarnContext(ctx, "scaling operation rejected",
			"reason", "deployment is not toggleable or namespace is protected",
			"namespace", namespace,
			"deployment", name,
		)

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
			slog.WarnContext(ctx, "scaling operation rejected",
				"reason", "deployment is managed by HPA",
				"namespace", namespace,
				"deployment", deploymentName,
				"hpa", hpa.Name,
			)

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

// Preview calculates a scaling operation without modifying Kubernetes resources.
func (m *Manager) Preview(
	ctx context.Context,
	namespace string,
	name string,
	enable bool,
) (*Plan, error) {
	deployment, err := m.getEligibleDeployment(ctx, namespace, name)
	if err != nil {
		return nil, err
	}

	current := replicaCount(deployment)

	plan := &Plan{
		Namespace:       namespace,
		Deployment:      name,
		Enable:          enable,
		CurrentReplicas: current,
		TargetReplicas:  current,
	}

	savedValue, hasSavedValue := deployment.Annotations[PreviousReplicasAnnotation]

	if !enable {
		if current == 0 {
			// Already disabled: preserve any previously saved replica count.
			if hasSavedValue {
				if parsed, err := strconv.ParseInt(savedValue, 10, 32); err == nil && parsed > 0 {
					saved := int32(parsed)
					plan.SavedReplicas = &saved
				}
			}
			logPreview(ctx, plan)
			return plan, nil
		}

		saved := current
		plan.SavedReplicas = &saved
		plan.TargetReplicas = 0
		plan.Changes = true
		logPreview(ctx, plan)
		return plan, nil
	}

	if current > 0 && !hasSavedValue {
		// Already enabled, with no saved state.
		logPreview(ctx, plan)
		return plan, nil
	}

	target := int32(1)

	if hasSavedValue {
		if parsed, err := strconv.ParseInt(savedValue, 10, 32); err == nil && parsed > 0 {
			target = int32(parsed)
			saved := target
			plan.SavedReplicas = &saved
		}
	}

	plan.TargetReplicas = target
	plan.Changes = current != target || hasSavedValue
	logPreview(ctx, plan)
	return plan, nil
}

func logPreview(ctx context.Context, plan *Plan) {
	operation := "disable"
	if plan.Enable {
		operation = "enable"
	}

	slog.DebugContext(ctx, "scaling preview generated",
		"operation", operation,
		"namespace", plan.Namespace,
		"deployment", plan.Deployment,
		"current_replicas", plan.CurrentReplicas,
		"target_replicas", plan.TargetReplicas,
		"changes", plan.Changes,
	)
}
