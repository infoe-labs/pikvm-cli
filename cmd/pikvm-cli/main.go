package main

import (
	"context"
	"fmt"
	"github.com/infoe-labs/pikvm-cli/cli"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := cli.NewRootCommand().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, cli.ErrorMessage(err))
		os.Exit(1)
	}
}
