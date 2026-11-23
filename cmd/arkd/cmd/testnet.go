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

// testnetOptions are the flags init-files and start share.
type testnetOptions struct {
	numValidators int
	outputDir     string
	chainID       string
	minGasPrices  string
	algo          string
	commitTimeout time.Duration
}

func (o *testnetOptions) bind(cmd *cobra.Command, commitTimeout time.Duration) {
	f := cmd.Flags()
	f.IntVarP(&o.numValidators, flagNumValidators, "v", defaultNumValidators, "Number of validators to initialise the testnet with")
	f.StringVarP(&o.outputDir, flagOutputDir, "o", defaultOutputDir, "Directory to store initialization data for the testnet")
	f.StringVar(&o.chainID, flags.FlagChainID, "", "genesis file chain-id, if left blank will be randomly created")
	f.StringVar(&o.minGasPrices, flagMinGasPrices, defaultMinGasPrices, "Minimum gas prices each node accepts for transactions; all fees in a tx must meet this minimum (e.g. 0.01anoah,0.001ausd)")
	f.StringVar(&o.algo, flags.FlagKeyType, string(hd.Secp256k1Type), "Key signing algorithm to generate keys for")
	f.DurationVar(&o.commitTimeout, flagCommitTimeout, commitTimeout, "Time to wait after a block commit before starting on the new height")
}

// validate refuses what no testnet can have; a negative count would panic
// the slice allocation in init-files.
func (o testnetOptions) validate() error {
	if o.numValidators < 1 {
		return fmt.Errorf("--%s must be at least 1, got %d", flagNumValidators, o.numValidators)
	}
	return nil
}

type testnetInitOptions struct {
	testnetOptions
	keyringBackend    string
	nodeDaemonHome    string
	nodeDirPrefix     string
	startingIPAddress string
	listenIPAddress   string
	singleMachine     bool
	bondTokenDenom    string
	genesisFile       string
}

type testnetStartOptions struct {
	testnetOptions
	apiAddress    string
	enableLogging bool
	grpcAddress   string
	printMnemonic bool
	rpcAddress    string
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
	var options testnetInitOptions
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
			if err := options.validate(); err != nil {
				return err
			}
			clientCtx, err := client.GetClientQueryContext(cmd)
			if err != nil {
				return err
			}
			config := server.GetServerContextFromCmd(cmd).Config
			config.Consensus.TimeoutCommit = options.commitTimeout

			return runTestnetInitFiles(cmd.Context(), clientCtx, config, mm, genBalIterator, options, cmd.InOrStdin(), cmd.ErrOrStderr())
		},
	}

	options.bind(cmd, defaultCommitTimeout)
	f := cmd.Flags()
	f.StringVar(&options.nodeDirPrefix, flagNodeDirPrefix, defaultNodeDirPrefix, "Prefix for the name of per-validator subdirectories (to be number-suffixed like node0, node1, ...)")
	f.StringVar(&options.genesisFile, flagGenesis, "", "Curated genesis to start from: every validator becomes a seat granted from its community pool, which bounds the count, and its chain ID is the default")
	f.StringVar(&options.nodeDaemonHome, flagNodeDaemonHome, serviceName, "Home directory of the node's daemon configuration")
	f.StringVar(&options.startingIPAddress, flagStartingIPAddress, defaultStartingIPAddress, "Starting IP address (192.168.0.1 results in persistent peers list ID0@192.168.0.1:46656, ID1@192.168.0.2:46656, ...)")
	f.StringVar(&options.listenIPAddress, flagListenIPAddress, defaultListenIPAddress, "TCP or UNIX socket IP address for the RPC server to listen on")
	f.StringVar(&options.keyringBackend, flags.FlagKeyringBackend, flags.DefaultKeyringBackend, "Select keyring's backend (os|file|test)")
	f.BoolVar(&options.singleMachine, flagSingleHost, defaultSingleHost, "Cluster runs on a single host machine with different ports")
	f.StringVar(&options.bondTokenDenom, flagStakingDenom, sdk.DefaultBondDenom, "Default staking token denomination")

	return cmd
}

// newTestnetStartCmd returns a cmd to start multi validator in-process testnet
func newTestnetStartCmd() *cobra.Command {
	var options testnetStartOptions
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
			if err := options.validate(); err != nil {
				return err
			}
			return runTestnetStart(cmd, options)
		},
	}

	options.bind(cmd, defaultInProcessCommitTimeout)
	f := cmd.Flags()
	f.BoolVar(&options.enableLogging, flagEnableLogging, defaultEnableLogging, "Enable INFO logging of CometBFT validator nodes")
	f.StringVar(&options.rpcAddress, flagRPCAddress, defaultRPCAddress, "the RPC address to listen on")
	f.StringVar(&options.apiAddress, flagAPIAddress, defaultAPIAddress, "the address to listen on for REST API")
	f.StringVar(&options.grpcAddress, flagGRPCAddress, defaultGRPCAddress, "the gRPC server address to listen on")
	f.BoolVar(&options.printMnemonic, flagPrintMnemonic, defaultPrintMnemonic, "print mnemonic of first validator to stdout for manual testing")
	return cmd
}
