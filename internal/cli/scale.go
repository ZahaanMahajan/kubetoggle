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
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "off <namespace> <deployment>",
		Short: "Scale a toggleable Deployment to zero",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScaleCommand(cmd, args[0], args[1], false, dryRun)
		},
	}

	cmd.Flags().BoolVar(
		&dryRun,
		"dry-run",
		false,
		"Preview the scaling operation without changing the cluster",
	)

	return cmd
}

func newOnCommand() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "on <namespace> <deployment>",
		Short: "Restore a toggleable Deployment",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScaleCommand(cmd, args[0], args[1], true, dryRun)
		},
	}

	cmd.Flags().BoolVar(
		&dryRun,
		"dry-run",
		false,
		"Preview the scaling operation without changing the cluster",
	)

	return cmd
}

func runScaleCommand(
	cmd *cobra.Command,
	namespace string,
	name string,
	enable bool,
	dryRun bool,
) error {
	client, err := kubernetes.NewClientset()
	if err != nil {
		return err
	}

	manager := scale.NewManager(client)
	ctx := cmd.Context()

	if dryRun {
		plan, err := manager.Preview(ctx, namespace, name, enable)
		if err != nil {
			return err
		}

		if !enable {
			if err := warnAboutPVCs(cmd, client, namespace, name); err != nil {
				return err
			}
		}

		return printPlan(cmd, plan)
	}

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

func printPlan(cmd *cobra.Command, plan *scale.Plan) error {
	action := "Disable"
	if plan.Enable {
		action = "Enable"
	}

	cmd.Printf(
		"DRY RUN: %s Deployment %s/%s\n",
		action,
		plan.Namespace,
		plan.Deployment,
	)

	cmd.Printf("Current replicas: %d\n", plan.CurrentReplicas)
	cmd.Printf("Target replicas:  %d\n", plan.TargetReplicas)

	if plan.SavedReplicas != nil {
		cmd.Printf("Saved replicas:   %d\n", *plan.SavedReplicas)
	} else {
		cmd.Println("Saved replicas:   none")
	}

	if plan.Changes {
		cmd.Println("A scaling operation would be performed.")
	} else {
		cmd.Println("No operation required; Deployment is already in the requested state.")
	}

	cmd.Println("No changes made to the cluster.")

	return nil
}
