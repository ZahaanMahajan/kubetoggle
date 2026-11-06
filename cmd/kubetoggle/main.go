package main

import (
	"context"
	"fmt"
	"os"

	"github.com/zahaanmahajan/kubetoggle/internal/cli"
)

func main() {
	rootCmd := cli.NewRootCommand()

	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
