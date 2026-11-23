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

// inProcessNetworkConfig is the in-process network for args, once the
// process can run one: the commit timeout leaves the first block inside
// network.New's budget, and the SDK's app.toml template is back in place.
func inProcessNetworkConfig(args startArgs) (network.Config, error) {
	if args.timeoutCommit > maxInProcessCommitTimeout {
		return network.Config{}, fmt.Errorf(
			"--%s %s is above %s: the in-process network fails unless its first block lands within five seconds of starting",
			flagCommitTimeout, args.timeoutCommit, maxInProcessCommitTimeout,
		)
	}
	// network.New writes each validator's app.toml from the SDK's own config
	// type. The root pre-run installs Ark's template when it creates a missing
	// app.toml, and Ark's template does not render the SDK's type.
	serverconfig.SetConfigTemplate(serverconfig.DefaultConfigTemplate)

	networkConfig := network.DefaultConfig(apptestutil.NewTestNetworkFixture)

	// Default networkConfig.ChainID is random, and we should only override it if chainID provided
	// is non-empty
	if args.chainID != "" {
		networkConfig.ChainID = args.chainID
	}
	networkConfig.SigningAlgo = args.algo
	networkConfig.MinGasPrices = args.minGasPrices
	networkConfig.NumValidators = args.numValidators
	networkConfig.EnableLogging = args.enableLogging
	networkConfig.RPCAddress = args.rpcAddress
	networkConfig.APIAddress = args.apiAddress
	networkConfig.GRPCAddress = args.grpcAddress
	networkConfig.PrintMnemonic = args.printMnemonic
	networkConfig.TimeoutCommit = args.timeoutCommit
	return networkConfig, nil
}

// startTestnet starts an in-process testnet
func startTestnet(cmd *cobra.Command, args startArgs) error {
	networkConfig, err := inProcessNetworkConfig(args)
	if err != nil {
		return err
	}
	networkLogger := network.NewCLILogger(cmd)

	baseDir := filepath.Join(args.outputDir, networkConfig.ChainID)
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
