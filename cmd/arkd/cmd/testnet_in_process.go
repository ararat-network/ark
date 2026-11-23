// SPDX-License-Identifier: Apache-2.0
// Adapted from Cosmos SDK, simapp/simd/cmd/testnet.go.
// Modified for Ark: testnet configuration, genesis, and node setup.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/cosmos/cosmos-sdk/testutil/network"

	apptestutil "github.com/ararat-network/ark/app/testutil"
)

// The in-process network's first block has to land within five seconds:
// network.New polls that long for it and then fails, and the block lands one
// commit timeout after the validators start. The default sits well inside the
// budget and a timeout near it is refused.
const (
	defaultInProcessCommitTimeout = time.Second
	maxInProcessCommitTimeout     = 4 * time.Second
)

// The in-process testnet's listen addresses, on the same ports init-files
// hands each node.
var (
	defaultRPCAddress  = fmt.Sprintf("tcp://0.0.0.0:%d", rpcPort)
	defaultAPIAddress  = fmt.Sprintf("tcp://0.0.0.0:%d", apiPort)
	defaultGRPCAddress = fmt.Sprintf("0.0.0.0:%d", grpcPort)
)

// inProcessNetworkConfig is the in-process network for options, once the
// process can run one: the commit timeout leaves the first block inside
// network.New's budget, and the SDK's app.toml template is back in place.
func inProcessNetworkConfig(options testnetStartOptions) (network.Config, error) {
	if options.commitTimeout > maxInProcessCommitTimeout {
		return network.Config{}, fmt.Errorf(
			"--%s %s is above %s: the in-process network fails unless its first block lands within five seconds of starting",
			flagCommitTimeout, options.commitTimeout, maxInProcessCommitTimeout,
		)
	}
	// network.New writes each validator's app.toml from the SDK's own config
	// type. The root pre-run installs Ark's template when it creates a missing
	// app.toml, and Ark's template does not render the SDK's type.
	serverconfig.SetConfigTemplate(serverconfig.DefaultConfigTemplate)

	networkConfig := network.DefaultConfig(apptestutil.NewTestNetworkFixture)

	// Default networkConfig.ChainID is random, and we should only override it if chainID provided
	// is non-empty
	if options.chainID != "" {
		networkConfig.ChainID = options.chainID
	}
	networkConfig.SigningAlgo = options.algo
	networkConfig.MinGasPrices = options.minGasPrices
	networkConfig.NumValidators = options.numValidators
	networkConfig.EnableLogging = options.enableLogging
	networkConfig.RPCAddress = options.rpcAddress
	networkConfig.APIAddress = options.apiAddress
	networkConfig.GRPCAddress = options.grpcAddress
	networkConfig.PrintMnemonic = options.printMnemonic
	networkConfig.TimeoutCommit = options.commitTimeout
	return networkConfig, nil
}

// runTestnetStart starts an in-process testnet. It keeps the command:
// the network's logger is built from one.
func runTestnetStart(cmd *cobra.Command, options testnetStartOptions) error {
	networkConfig, err := inProcessNetworkConfig(options)
	if err != nil {
		return err
	}
	networkLogger := network.NewCLILogger(cmd)

	baseDir := filepath.Join(options.outputDir, networkConfig.ChainID)
	if _, err := os.Stat(baseDir); !os.IsNotExist(err) {
		return fmt.Errorf(
			"testnets directory already exists for chain-id '%s': %s, please remove or select a new --chain-id",
			networkConfig.ChainID, baseDir)
	}

	testnet, err := network.New(networkLogger, baseDir, networkConfig)
	if err != nil {
		return err
	}

	if _, err := testnet.WaitForHeight(1); err != nil {
		return err
	}
	cmd.Println("press the Enter Key to terminate")
	if _, err := fmt.Scanln(); err != nil { // wait for Enter Key
		return err
	}
	testnet.Cleanup()

	return nil
}
