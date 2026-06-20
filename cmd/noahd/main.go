package main

import (
	"context"
	"fmt"
	"os"

	clientv2helpers "cosmossdk.io/client/v2/helpers"

	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"

	"noah/app"
	"noah/cmd/noahd/cmd"
	"noah/pkg/telemetry"
)

func main() {
	os.Exit(run())
}

func run() int {
	provider, err := telemetry.InitPrometheus(app.Name + "d")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()

	rootCmd := cmd.NewRootCmd()
	if err := svrcmd.Execute(rootCmd, clientv2helpers.EnvPrefix, app.DefaultNodeHome); err != nil {
		fmt.Fprintln(rootCmd.OutOrStderr(), err)
		return 1
	}

	return 0
}
