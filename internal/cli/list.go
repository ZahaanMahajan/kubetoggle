package cli

import (
	"fmt"

	"github.com/zahaanmahajan/kubetoggle/internal/kubernetes"
	"github.com/zahaanmahajan/kubetoggle/internal/workload"

	"github.com/spf13/cobra"
)

func newListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List toggleable Kubernetes Deployments",
		Long:  "Discover and list all eligible Deployments grouped by their Argo CD Application.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			client, err := kubernetes.NewClientset()
			if err != nil {
				return err
			}

			deployments, err := workload.Discover(ctx, client)
			if err != nil {
				return fmt.Errorf("discover workloads: %w", err)
			}

			out := cmd.OutOrStdout()

			if len(deployments) == 0 {
				fmt.Fprintln(out, "No toggleable Deployments found.")
				return nil
			}

			fmt.Fprintf(out, "Discovered %d toggleable Deployments\n\n", len(deployments))

			currentApplication := ""

			for _, deployment := range deployments {
				application := deployment.Application
				if application == "" {
					application = "unmanaged"
				}

				if application != currentApplication {
					currentApplication = application
					fmt.Fprintf(out, "[%s]\n", currentApplication)
				}

				fmt.Fprintf(
					out,
					"  %-12s %-24s replicas=%d\n",
					deployment.Namespace,
					deployment.Name,
					deployment.Replicas,
				)
			}

			return nil
		},
	}
}
