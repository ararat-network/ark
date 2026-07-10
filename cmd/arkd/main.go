package main

import (
	"fmt"
	"os"

	clientv2helpers "cosmossdk.io/client/v2/helpers"

	svrcmd "github.com/cosmos/cosmos-sdk/server/cmd"

	"ark/app"
	"ark/cmd/arkd/cmd"
)

func main() {
	os.Exit(run())
}

func run() int {
	rootCmd := cmd.NewRootCmd()
	if err := svrcmd.Execute(rootCmd, clientv2helpers.EnvPrefix, app.DefaultNodeHome); err != nil {
		fmt.Fprintln(rootCmd.OutOrStderr(), err)
		return 1
	}

	return 0
}
