// SPDX-License-Identifier: Apache-2.0
// Adapted from Cosmos SDK, simapp/simd/cmd/testnet.go.
// Modified for Ark: testnet configuration, genesis, and node setup.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/server"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/version"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

const (
	flagNodeDirPrefix     = "node-dir-prefix"
	flagNumValidators     = "validator-count"
	flagOutputDir         = "output-dir"
	flagNodeDaemonHome    = "node-daemon-home"
	flagStartingIPAddress = "starting-ip-address"
	flagListenIPAddress   = "listen-ip-address"
	flagEnableLogging     = "enable-logging"
	flagPrintMnemonic     = "print-mnemonic"
	flagStakingDenom      = "staking-denom"
	flagCommitTimeout     = "commit-timeout"
	flagSingleHost        = "single-host"
	flagGenesis           = "genesis"

	// The root pre-run binds every flag to the home's app.toml under the
	// flag's name: an unset flag takes the file's value, and on a fresh home
	// the flag's default is written into the file. These settings therefore
	// carry names no app.toml key has, unlike the SDK's flags for the same.
	flagMinGasPrices = "min-gas-prices"
	flagGRPCAddress  = "grpc-address"
	flagRPCAddress   = "rpc-address"
	flagAPIAddress   = "api-address"

	defaultNumValidators     = 4
	defaultOutputDir         = "./.testnets"
	defaultNodeDirPrefix     = "node"
	defaultStartingIPAddress = "192.168.0.1"
	defaultListenIPAddress   = "0.0.0.0"
	defaultEnableLogging     = false
	defaultPrintMnemonic     = true
	defaultSingleHost        = false
)

var defaultMinGasPrices = fmt.Sprintf("6000000%s", sdk.DefaultBondDenom)

type initArgs struct {
	algo              string
	chainID           string
	keyringBackend    string
	minGasPrices      string
	nodeDaemonHome    string
	nodeDirPrefix     string
	numValidators     int
	outputDir         string
	startingIPAddress string
	listenIPAddress   string
	singleMachine     bool
	bondTokenDenom    string
	genesisFile       string
}

type startArgs struct {
	algo          string
	apiAddress    string
	chainID       string
	enableLogging bool
	grpcAddress   string
	minGasPrices  string
	numValidators int
	outputDir     string
	printMnemonic bool
	rpcAddress    string
	timeoutCommit time.Duration
}

// validatorCount reads --validator-count and refuses what no testnet can
// have; a negative count would panic the slice allocation below.
func validatorCount(cmd *cobra.Command) (int, error) {
	count, _ := cmd.Flags().GetInt(flagNumValidators)
	if count < 1 {
		return 0, fmt.Errorf("--%s must be at least 1, got %d", flagNumValidators, count)
	}
	return count, nil
}

func addTestnetFlagsToCmd(cmd *cobra.Command, commitTimeout time.Duration) {
	cmd.Flags().IntP(flagNumValidators, "v", defaultNumValidators, "Number of validators to initialise the testnet with")
	cmd.Flags().StringP(flagOutputDir, "o", defaultOutputDir, "Directory to store initialization data for the testnet")
	cmd.Flags().String(flags.FlagChainID, "", "genesis file chain-id, if left blank will be randomly created")
	cmd.Flags().String(flagMinGasPrices, defaultMinGasPrices, "Minimum gas prices each node accepts for transactions; all fees in a tx must meet this minimum (e.g. 0.01anoah,0.001ausd)")
	cmd.Flags().String(flags.FlagKeyType, string(hd.Secp256k1Type), "Key signing algorithm to generate keys for")
	cmd.Flags().Duration(flagCommitTimeout, commitTimeout, "Time to wait after a block commit before starting on the new height")
}

// newTestnetCmd creates a root testnet command with subcommands to run an in-process testnet or initialise
// validator configuration files for running a multi-validator testnet in a separate process
func newTestnetCmd(mm module.BasicManager, genBalIterator banktypes.GenesisBalancesIterator) *cobra.Command {
	testnetCmd := &cobra.Command{
		Use:                        "testnet",
		Short:                      "Subcommands for starting or configuring local testnets",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	testnetCmd.AddCommand(newTestnetStartCmd())
	testnetCmd.AddCommand(newTestnetInitFilesCmd(mm, genBalIterator))

	return testnetCmd
}

// newTestnetInitFilesCmd returns a cmd to initialise all files for CometBFT testnet and application
func newTestnetInitFilesCmd(mm module.BasicManager, genBalIterator banktypes.GenesisBalancesIterator) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init-files",
		Short: "Initialise config directories & files for a multi-validator testnet running locally via separate processes (e.g. Docker Compose or similar)",
		Long: fmt.Sprintf(`init-files will setup one directory per validator and populate each with
necessary files (private validator, genesis, config, etc.) for running validator nodes.

Booting up a network with these validator folders is intended to be used with Docker Compose,
or a similar setup where each node has a manually configurable IP address.

Note, strict routability for addresses is turned off in the config file.

Example:
	%s testnet init-files --validator-count 4 --output-dir ./.testnets --starting-ip-address 192.168.10.2
	`, version.AppName),
		RunE: func(cmd *cobra.Command, _ []string) error {
			numValidators, err := validatorCount(cmd)
			if err != nil {
				return err
			}
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}

			serverCtx := server.GetServerContextFromCmd(cmd)
			config := serverCtx.Config

			args := initArgs{numValidators: numValidators}
			args.outputDir, _ = cmd.Flags().GetString(flagOutputDir)
			args.keyringBackend, _ = cmd.Flags().GetString(flags.FlagKeyringBackend)
			args.chainID, _ = cmd.Flags().GetString(flags.FlagChainID)
			args.minGasPrices, _ = cmd.Flags().GetString(flagMinGasPrices)
			args.nodeDirPrefix, _ = cmd.Flags().GetString(flagNodeDirPrefix)
			args.nodeDaemonHome, _ = cmd.Flags().GetString(flagNodeDaemonHome)
			args.startingIPAddress, _ = cmd.Flags().GetString(flagStartingIPAddress)
			args.listenIPAddress, _ = cmd.Flags().GetString(flagListenIPAddress)
			args.algo, _ = cmd.Flags().GetString(flags.FlagKeyType)
			args.bondTokenDenom, _ = cmd.Flags().GetString(flagStakingDenom)
			args.singleMachine, _ = cmd.Flags().GetBool(flagSingleHost)
			args.genesisFile, _ = cmd.Flags().GetString(flagGenesis)
			config.Consensus.TimeoutCommit, err = cmd.Flags().GetDuration(flagCommitTimeout)
			if err != nil {
				return err
			}

			return initTestnetFiles(clientCtx, cmd, config, mm, genBalIterator, args)
		},
	}

	addTestnetFlagsToCmd(cmd, defaultCommitTimeout)
	cmd.Flags().String(flagNodeDirPrefix, defaultNodeDirPrefix, "Prefix for the name of per-validator subdirectories (to be number-suffixed like node0, node1, ...)")
	cmd.Flags().String(flagGenesis, "", "Curated genesis to start from: every validator becomes a seat granted from its community pool, which bounds the count, and its chain ID is the default")
	cmd.Flags().String(flagNodeDaemonHome, serviceName, "Home directory of the node's daemon configuration")
	cmd.Flags().String(flagStartingIPAddress, defaultStartingIPAddress, "Starting IP address (192.168.0.1 results in persistent peers list ID0@192.168.0.1:46656, ID1@192.168.0.2:46656, ...)")
	cmd.Flags().String(flagListenIPAddress, defaultListenIPAddress, "TCP or UNIX socket IP address for the RPC server to listen on")
	cmd.Flags().String(flags.FlagKeyringBackend, flags.DefaultKeyringBackend, "Select keyring's backend (os|file|test)")
	cmd.Flags().Bool(flagSingleHost, defaultSingleHost, "Cluster runs on a single host machine with different ports")
	cmd.Flags().String(flagStakingDenom, sdk.DefaultBondDenom, "Default staking token denomination")

	return cmd
}

// newTestnetStartCmd returns a cmd to start multi validator in-process testnet
func newTestnetStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Launch an in-process multi-validator testnet",
		Long: fmt.Sprintf(`testnet will launch an in-process multi-validator testnet,
and generate a directory for each validator populated with necessary
configuration files (private validator, genesis, config, etc.).

Example:
	%s testnet start --validator-count 4 --output-dir ./.testnets
	`, version.AppName),
		RunE: func(cmd *cobra.Command, _ []string) error {
			numValidators, err := validatorCount(cmd)
			if err != nil {
				return err
			}
			args := startArgs{numValidators: numValidators}
			args.outputDir, _ = cmd.Flags().GetString(flagOutputDir)
			args.chainID, _ = cmd.Flags().GetString(flags.FlagChainID)
			args.minGasPrices, _ = cmd.Flags().GetString(flagMinGasPrices)
			args.algo, _ = cmd.Flags().GetString(flags.FlagKeyType)
			args.enableLogging, _ = cmd.Flags().GetBool(flagEnableLogging)
			args.rpcAddress, _ = cmd.Flags().GetString(flagRPCAddress)
			args.apiAddress, _ = cmd.Flags().GetString(flagAPIAddress)
			args.grpcAddress, _ = cmd.Flags().GetString(flagGRPCAddress)
			args.printMnemonic, _ = cmd.Flags().GetBool(flagPrintMnemonic)
			args.timeoutCommit, _ = cmd.Flags().GetDuration(flagCommitTimeout)

			return startTestnet(cmd, args)
		},
	}

	addTestnetFlagsToCmd(cmd, defaultInProcessCommitTimeout)
	cmd.Flags().Bool(flagEnableLogging, defaultEnableLogging, "Enable INFO logging of CometBFT validator nodes")
	cmd.Flags().String(flagRPCAddress, defaultRPCAddress, "the RPC address to listen on")
	cmd.Flags().String(flagAPIAddress, defaultAPIAddress, "the address to listen on for REST API")
	cmd.Flags().String(flagGRPCAddress, defaultGRPCAddress, "the gRPC server address to listen on")
	cmd.Flags().Bool(flagPrintMnemonic, defaultPrintMnemonic, "print mnemonic of first validator to stdout for manual testing")
	return cmd
}
