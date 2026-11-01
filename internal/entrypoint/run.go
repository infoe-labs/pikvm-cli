// Package entrypoint shares process startup between source and installed binaries.
package entrypoint

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/infoe-labs/pikvm-cli/cli"
)

func Run(version string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	root := cli.NewRootCommand()
	root.Version = version
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, cli.ErrorMessage(err))
		return 1
	}
	return 0
}
