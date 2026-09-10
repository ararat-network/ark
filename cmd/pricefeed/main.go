package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	pricefeedcmd "github.com/ararat-network/ark/cmd/pricefeed/cmd"
)

func main() {
	os.Exit(run())
}

// run hands every command one context that ends on SIGINT or SIGTERM, so a
// long-running check winds down the same way the sidecar does.
func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rootCmd := pricefeedcmd.NewRootCmd()
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(rootCmd.ErrOrStderr(), err)
		return 1
	}

	return 0
}
