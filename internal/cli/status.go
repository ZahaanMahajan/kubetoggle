package cli

import (
	"fmt"

	"github.com/zahaanmahajan/kubetoggle/internal/kubernetes"
	"github.com/zahaanmahajan/kubetoggle/internal/safety"
	"github.com/zahaanmahajan/kubetoggle/internal/scale"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status <namespace> <deployment>",
		Short: "Show the status of a toggleable Deployment",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			namespace, name := args[0], args[1]

			client, err := kubernetes.NewClientset()
			if err != nil {
				return err
			}

			deployment, err := client.AppsV1().Deployments(namespace).Get(
				cmd.Context(),
				name,
				metav1.GetOptions{},
			)
			if err != nil {
				return fmt.Errorf("get Deployment %s/%s: %w", namespace, name, err)
			}

			if !safety.IsEligible(namespace, deployment.Labels) {
				return fmt.Errorf(
					"Deployment %s/%s is not eligible for kubetoggle",
					namespace,
					name,
				)
			}

			replicas := int32(1)
			if deployment.Spec.Replicas != nil {
				replicas = *deployment.Spec.Replicas
			}

			state := "ON"
			if replicas == 0 {
				state = "OFF"
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Deployment:       %s/%s\n", namespace, name)
			fmt.Fprintf(cmd.OutOrStdout(), "State:            %s\n", state)
			fmt.Fprintf(cmd.OutOrStdout(), "Desired replicas: %d\n", replicas)
			fmt.Fprintf(cmd.OutOrStdout(), "Ready replicas:   %d\n", deployment.Status.ReadyReplicas)
			fmt.Fprintf(cmd.OutOrStdout(), "Available:        %d\n", deployment.Status.AvailableReplicas)

			if previous, exists := deployment.Annotations[scale.PreviousReplicasAnnotation]; exists {
				fmt.Fprintf(cmd.OutOrStdout(), "Saved replicas:   %s\n", previous)
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Saved replicas:   none")
			}

			return nil
		},
	}
}
