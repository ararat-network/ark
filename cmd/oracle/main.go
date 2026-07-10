// Command oracle runs the standalone Ark price-oracle sidecar and its
// administrative and inspection commands.
package main

import (
	"fmt"
	"os"

	oraclecmd "ark/cmd/oracle/cmd"
)

func main() {
	if err := oraclecmd.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
