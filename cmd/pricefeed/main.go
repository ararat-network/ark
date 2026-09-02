package main

import (
	"fmt"
	"os"

	pricefeedcmd "github.com/ararat-network/ark/cmd/pricefeed/cmd"
)

func main() {
	if err := pricefeedcmd.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
