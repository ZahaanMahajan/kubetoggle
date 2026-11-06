package cli

import (
	"fmt"

	"github.com/zahaanmahajan/kubetoggle/internal/kubernetes"
	"github.com/zahaanmahajan/kubetoggle/internal/tui"
	"github.com/zahaanmahajan/kubetoggle/internal/workload"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

func newTUICommand() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Launch the interactive terminal interface",
		RunE: func(cmd *cobra.Command, args []string) error {

			client, err := kubernetes.NewClientset()
			if err != nil {
				return err
			}

			recorder, err := newAuditRecorder(cmd)
			if err != nil {
				return err
			}
			defer recorder.Close()

			deployments, err := workload.Discover(cmd.Context(), client)
			if err != nil {
				return fmt.Errorf("discover workloads: %w", err)
			}

			program := tea.NewProgram(
				tui.NewModel(deployments, client, recorder),
				tea.WithAltScreen(),
			)

			if _, err := program.Run(); err != nil {
				return fmt.Errorf("run TUI: %w", err)
			}

			return nil
		},
	}
}
