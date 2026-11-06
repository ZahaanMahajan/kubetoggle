package cli

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/zahaanmahajan/kubetoggle/internal/logging"
)

func NewRootCommand() *cobra.Command {
	var logLevel string
	var auditFile string

	rootCmd := &cobra.Command{
		Use:           "kubetoggle",
		Short:         "Manage Kubernetes workloads through safe scaling operations",
		Long:          "KubeToggle is a Kubernetes workload management tool for safely scaling toggleable Deployments.",
		SilenceUsage:  true,
		SilenceErrors: true,

		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			logger, err := logging.New(logLevel, os.Stderr)
			if err != nil {
				return err
			}

			slog.SetDefault(logger)
			return nil
		},
	}

	rootCmd.PersistentFlags().StringVar(
		&logLevel,
		"log-level",
		"warn",
		"Logging level: debug, info, warn, error",
	)

	rootCmd.PersistentFlags().StringVar(
		&auditFile,
		"audit-file",
		filepath.Join(os.Getenv("HOME"), ".local", "state", "kubetoggle", "audit.jsonl"),
		"Path to the JSONL audit log",
	)

	rootCmd.AddCommand(
		newListCommand(),
		newOffCommand(),
		newOnCommand(),
		newStatusCommand(),
		newTUICommand(),
	)

	return rootCmd
}
