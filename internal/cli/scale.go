package cli

import (
	"fmt"

	"github.com/zahaanmahajan/kubetoggle/internal/kubernetes"
	"github.com/zahaanmahajan/kubetoggle/internal/safety"
	"github.com/zahaanmahajan/kubetoggle/internal/scale"
	"github.com/zahaanmahajan/kubetoggle/internal/workload"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sclient "k8s.io/client-go/kubernetes"
)

func newOffCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "off <namespace> <deployment>",
		Short: "Scale a toggleable Deployment to zero",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScaleCommand(cmd, args[0], args[1], false)
		},
	}
}

func newOnCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "on <namespace> <deployment>",
		Short: "Restore a toggleable Deployment",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScaleCommand(cmd, args[0], args[1], true)
		},
	}
}

func runScaleCommand(
	cmd *cobra.Command,
	namespace string,
	name string,
	enable bool,
) error {
	client, err := kubernetes.NewClientset()
	if err != nil {
		return err
	}

	manager := scale.NewManager(client)
	ctx := cmd.Context()

	if enable {
		if err := manager.On(ctx, namespace, name); err != nil {
			return fmt.Errorf("enable Deployment %s/%s: %w", namespace, name, err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Enabled Deployment %s/%s\n", namespace, name)
		return nil
	}

	if err := warnAboutPVCs(cmd, client, namespace, name); err != nil {
		return err
	}
	if err := manager.Off(ctx, namespace, name); err != nil {
		return fmt.Errorf("disable Deployment %s/%s: %w", namespace, name, err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Disabled Deployment %s/%s\n", namespace, name)
	return nil
}

func warnAboutPVCs(
	cmd *cobra.Command,
	client k8sclient.Interface,
	namespace string,
	name string,
) error {
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

	claims := workload.PVCNames(deployment)
	if len(claims) == 0 {
		return nil
	}

	fmt.Fprintf(
		cmd.OutOrStdout(),
		"\nWARNING: Deployment %s/%s references persistent storage:\n",
		namespace,
		name,
	)

	for _, claim := range claims {
		fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", claim)
	}

	fmt.Fprintln(
		cmd.OutOrStdout(),
		"Scaling to zero stops its Pods. PVCs and PVs will not be deleted.",
	)

	return nil
}
