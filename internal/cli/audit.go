package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zahaanmahajan/kubetoggle/internal/audit"
)

func newAuditRecorder(cmd *cobra.Command) (*audit.Recorder, error) {
	path, err := cmd.Flags().GetString("audit-file")
	if err != nil {
		return nil, fmt.Errorf("read audit-file flag: %w", err)
	}

	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home directory: %w", err)
		}

		path = filepath.Join(home, path[2:])
	}

	recorder, err := audit.New(path)
	if err != nil {
		return nil, fmt.Errorf("initialize audit recorder: %w", err)
	}

	return recorder, nil
}
