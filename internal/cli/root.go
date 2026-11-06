package cli

import (
	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "kubetoggle",
		Short:         "Manage Kubernetes workloads through safe scaling operations",
		Long:          "KubeToggle is a Kubernetes workload management tool for safely scaling toggleable Deployments.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.AddCommand(
		newListCommand(),
		newOffCommand(),
		newOnCommand(),
		newStatusCommand(),
		newTUICommand(),
	)

	return rootCmd
}
