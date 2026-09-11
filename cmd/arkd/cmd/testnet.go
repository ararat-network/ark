// SPDX-License-Identifier: Apache-2.0
// Adapted from Cosmos SDK, simapp/simd/cmd/testnet.go.
// Modified for Ark: testnet configuration, genesis, and node setup.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	cmtcfg "github.com/cometbft/cometbft/config"
	cmttime "github.com/cometbft/cometbft/types/time"

	"cosmossdk.io/math"
	"cosmossdk.io/math/unsafe"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	"github.com/cosmos/cosmos-sdk/testutil/network"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/version"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	arkgenesis "github.com/ararat-network/ark/app/genesis"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/telemetry"
)

const (
	flagNodeDirPrefix     = "node-dir-prefix"
	flagNumValidators     = "validator-count"
	flagOutputDir         = "output-dir"
	flagNodeDaemonHome    = "node-daemon-home"
	flagStartingIPAddress = "starting-ip-address"
	flagListenIPAddress   = "listen-ip-address"
	flagEnableLogging     = "enable-logging"
	flagGRPCAddress       = "grpc.address"
	flagRPCAddress        = "rpc.address"
	flagAPIAddress        = "api.address"
	flagPrintMnemonic     = "print-mnemonic"
	flagStakingDenom      = "staking-denom"
	flagCommitTimeout     = "commit-timeout"
	flagSingleHost        = "single-host"
	flagGenesis           = "genesis"

	defaultNumValidators     = 4
	defaultOutputDir         = "./.testnets"
	defaultNodeDirPrefix     = "node"
	defaultStartingIPAddress = "192.168.0.1"
	defaultListenIPAddress   = "0.0.0.0"
	defaultEnableLogging     = false
	defaultPrintMnemonic     = true
	defaultSingleHost        = false
)

// The in-process testnet's listen addresses, on the same ports init-files
// hands each node.
var (
	defaultMinGasPrices = fmt.Sprintf("6000000%s", sdk.DefaultBondDenom)
	defaultRPCAddress   = fmt.Sprintf("tcp://0.0.0.0:%d", rpcPort)
	defaultAPIAddress   = fmt.Sprintf("tcp://0.0.0.0:%d", apiPort)
	defaultGRPCAddress  = fmt.Sprintf("0.0.0.0:%d", grpcPort)
)

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

func addTestnetFlagsToCmd(cmd *cobra.Command) {
	cmd.Flags().IntP(flagNumValidators, "v", defaultNumValidators, "Number of validators to initialise the testnet with")
	cmd.Flags().StringP(flagOutputDir, "o", defaultOutputDir, "Directory to store initialization data for the testnet")
	cmd.Flags().String(flags.FlagChainID, "", "genesis file chain-id, if left blank will be randomly created")
	cmd.Flags().String(server.FlagMinGasPrices, defaultMinGasPrices, "Minimum gas prices to accept for transactions; all fees in a tx must meet this minimum (e.g. 0.01anoah,0.001ausd)")
	cmd.Flags().String(flags.FlagKeyType, string(hd.Secp256k1Type), "Key signing algorithm to generate keys for")
	cmd.Flags().Duration(flagCommitTimeout, defaultCommitTimeout, "Time to wait after a block commit before starting on the new height")
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
			args.minGasPrices, _ = cmd.Flags().GetString(server.FlagMinGasPrices)
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

	addTestnetFlagsToCmd(cmd)
	cmd.Flags().String(flagNodeDirPrefix, defaultNodeDirPrefix, "Prefix for the name of per-validator subdirectories (to be number-suffixed like node0, node1, ...)")
	cmd.Flags().String(flagGenesis, "", "Curated genesis to start from: every validator becomes a seat granted from its community pool, and its chain ID is the default")
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
			args.minGasPrices, _ = cmd.Flags().GetString(server.FlagMinGasPrices)
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

	addTestnetFlagsToCmd(cmd)
	cmd.Flags().Bool(flagEnableLogging, defaultEnableLogging, "Enable INFO logging of CometBFT validator nodes")
	cmd.Flags().String(flagRPCAddress, defaultRPCAddress, "the RPC address to listen on")
	cmd.Flags().String(flagAPIAddress, defaultAPIAddress, "the address to listen on for REST API")
	cmd.Flags().String(flagGRPCAddress, defaultGRPCAddress, "the gRPC server address to listen on")
	cmd.Flags().Bool(flagPrintMnemonic, defaultPrintMnemonic, "print mnemonic of first validator to stdout for manual testing")
	return cmd
}

const nodeDirPerm = 0o755

// Listener ports. A single-host testnet offsets each by the node index, with
// P2P moved off the RPC range so the two never meet.
const (
	rpcPort           = 26657
	p2pPort           = 26656
	singleHostP2PPort = 16656
	apiPort           = 1317
	grpcPort          = 9090
	pprofPort         = 6060
	cometMetricsPort  = 27780
	metricsPort       = 9464
)

// initTestnetFiles initialises testnet files for a testnet to be run in a separate process
func initTestnetFiles(
	clientCtx client.Context,
	cmd *cobra.Command,
	nodeConfig *cmtcfg.Config,
	mm module.BasicManager,
	genBalIterator banktypes.GenesisBalancesIterator,
	args initArgs,
) error {
	var base *genutiltypes.AppGenesis
	if args.genesisFile != "" {
		var err error
		if base, err = genutiltypes.AppGenesisFromFile(args.genesisFile); err != nil {
			return fmt.Errorf("reading base genesis %s: %w", args.genesisFile, err)
		}
		if args.chainID == "" {
			args.chainID = base.ChainID
		}
	}
	if args.chainID == "" {
		args.chainID = "chain-" + unsafe.Str(6)
	}
	nodeIDs := make([]string, args.numValidators)
	valPubKeys := make([]cryptotypes.PubKey, args.numValidators)

	appConfig := defaultAppConfig()
	appConfig.MinGasPrices = args.minGasPrices
	appConfig.API.Enable = true
	// Cosmos SDK v0.54 still emits some metrics through its deprecated wrappers.
	// Route those metrics into the node-owned OpenTelemetry provider.
	appConfig.Telemetry.Enabled = true       //nolint:staticcheck // SDK legacy metrics bridge
	appConfig.Telemetry.MetricsSink = "otel" //nolint:staticcheck // SDK legacy metrics bridge

	var (
		genAccounts []authtypes.GenesisAccount
		genBalances []banktypes.Balance
		seats       []sdk.AccAddress
		genFiles    []string
	)
	p2pPortStart := p2pPort
	if args.singleMachine {
		p2pPortStart = singleHostP2PPort
		nodeConfig.P2P.AddrBookStrict = false
		nodeConfig.P2P.PexReactor = false
		nodeConfig.P2P.AllowDuplicateIP = true
	}
	// CometBFT's own metrics, the third surface in docs/operations/PROCESS_MONITORING.md §1,
	// on its default :26660 unless the single-host layout offsets it in
	// collectGenFiles.
	nodeConfig.Instrumentation.Prometheus = true
	serverconfig.SetConfigTemplate(appConfigTemplate)

	inBuf := bufio.NewReader(cmd.InOrStdin())
	// generate private keys, node IDs, and initial transactions
	for i := range args.numValidators {
		var portOffset int
		if args.singleMachine {
			portOffset = i
			appConfig.API.Address = fmt.Sprintf("tcp://0.0.0.0:%d", apiPort+portOffset)
			appConfig.GRPC.Address = fmt.Sprintf("0.0.0.0:%d", grpcPort+portOffset)
		}

		nodeDirName := fmt.Sprintf("%s%d", args.nodeDirPrefix, i)
		nodeDir := filepath.Join(args.outputDir, nodeDirName, args.nodeDaemonHome)
		gentxsDir := filepath.Join(args.outputDir, "gentxs")

		nodeConfig.SetRoot(nodeDir)
		nodeConfig.Moniker = nodeDirName
		nodeConfig.RPC.ListenAddress = fmt.Sprintf("tcp://%s:%d", args.listenIPAddress, rpcPort+portOffset)

		if err := os.MkdirAll(filepath.Join(nodeDir, "config"), nodeDirPerm); err != nil {
			return err
		}
		var (
			err error
			ip  string
		)
		if args.singleMachine {
			ip = "127.0.0.1"
		} else {
			ip, err = getIP(i, args.startingIPAddress)
			if err != nil {
				return err
			}
		}

		nodeIDs[i], valPubKeys[i], err = genutil.InitializeNodeValidatorFiles(nodeConfig)
		if err != nil {
			return err
		}

		memo := fmt.Sprintf("%s@%s:%d", nodeIDs[i], ip, p2pPortStart+portOffset)
		genFiles = append(genFiles, nodeConfig.GenesisFile())

		kb, err := keyring.New(sdk.KeyringServiceName(), args.keyringBackend, nodeDir, inBuf, clientCtx.Codec)
		if err != nil {
			return err
		}

		keyringAlgos, _ := kb.SupportedAlgorithms()
		algo, err := keyring.NewSigningAlgoFromString(args.algo, keyringAlgos)
		if err != nil {
			return err
		}

		addr, secret, err := sdktestutil.GenerateSaveCoinKey(kb, nodeDirName, "", true, algo)
		if err != nil {
			return err
		}

		info := map[string]string{"secret": secret}

		cliPrint, err := json.Marshal(info)
		if err != nil {
			return err
		}

		// save private key seed words
		if err := writeFile("key_seed.json", nodeDir, cliPrint); err != nil {
			return err
		}

		// From code defaults each validator holds test balances and bonds a
		// token; from an artefact it is a seat, bonding its whole locked
		// grant at the artefact's commission floor.
		bond := sdk.NewCoin(args.bondTokenDenom, sdk.TokensFromConsensusPower(100, sdk.DefaultPowerReduction))
		commission := stakingtypes.NewCommissionRates(math.LegacyOneDec(), math.LegacyOneDec(), math.LegacyOneDec())
		if base != nil {
			seats = append(seats, addr)
			if bond, commission, err = seatGentx(clientCtx.Codec, base); err != nil {
				return err
			}
		} else {
			coins := sdk.Coins{
				sdk.NewCoin("testtoken", sdk.TokensFromConsensusPower(1000, sdk.DefaultPowerReduction)),
				sdk.NewCoin(args.bondTokenDenom, sdk.TokensFromConsensusPower(500, sdk.DefaultPowerReduction)),
			}
			genBalances = append(genBalances, banktypes.Balance{Address: addr.String(), Coins: coins.Sort()})
			genAccounts = append(genAccounts, authtypes.NewBaseAccount(addr, nil, 0, 0))
		}

		createValMsg, err := stakingtypes.NewMsgCreateValidator(
			sdk.ValAddress(addr).String(),
			valPubKeys[i],
			bond,
			stakingtypes.NewDescription(nodeDirName, "", "", "", ""),
			commission,
			math.OneInt(),
		)
		if err != nil {
			return err
		}

		txBuilder := clientCtx.TxConfig.NewTxBuilder()
		if err := txBuilder.SetMsgs(createValMsg); err != nil {
			return err
		}

		txBuilder.SetMemo(memo)

		txFactory := clienttx.Factory{}
		txFactory = txFactory.
			WithChainID(args.chainID).
			WithMemo(memo).
			WithKeybase(kb).
			WithTxConfig(clientCtx.TxConfig)

		if err := clienttx.Sign(cmd.Context(), txFactory, nodeDirName, txBuilder, true); err != nil {
			return err
		}

		txBz, err := clientCtx.TxConfig.TxJSONEncoder()(txBuilder.GetTx())
		if err != nil {
			return err
		}

		if err := writeFile(fmt.Sprintf("%v.json", nodeDirName), gentxsDir, txBz); err != nil {
			return err
		}

		// Any interface: the nodes are scraped from outside their container.
		appConfig.Prometheus = telemetry.PrometheusConfig{
			Enabled: true,
			Address: fmt.Sprintf("0.0.0.0:%d", metricsPort+portOffset),
		}
		serverconfig.WriteConfigFile(filepath.Join(nodeDir, "config", "app.toml"), appConfig)

		// Empty selects noop telemetry, as `arkd init` writes. The otelconf
		// release cosmos-sdk v0.54 pins dropped the Prometheus pull reader,
		// so the scrape endpoint comes from [prometheus] above, not this file.
		if err := writeFile("otel.yaml", filepath.Join(nodeDir, "config"), nil); err != nil {
			return err
		}
	}

	if err := initGenFiles(clientCtx, mm, args.chainID, base, seats, genAccounts, genBalances, genFiles, args.numValidators); err != nil {
		return err
	}

	err := collectGenFiles(
		clientCtx, nodeConfig, args.chainID, nodeIDs, valPubKeys, args.numValidators,
		args.outputDir, args.nodeDirPrefix, args.nodeDaemonHome, genBalIterator,
		p2pPortStart, args.singleMachine,
	)
	if err != nil {
		return err
	}
	for _, genFile := range genFiles {
		if err := finaliseGenesisConsensusParams(genFile, base); err != nil {
			return err
		}
	}

	cmd.PrintErrf("Successfully initialised %d node directories\n", args.numValidators)
	return nil
}

func initGenFiles(
	clientCtx client.Context, mm module.BasicManager, chainID string,
	base *genutiltypes.AppGenesis, seats []sdk.AccAddress,
	genAccounts []authtypes.GenesisAccount, genBalances []banktypes.Balance,
	genFiles []string, numValidators int,
) error {
	if base != nil {
		appGenesis, err := seatedGenesis(clientCtx.Codec, base, chainID, seats)
		if err != nil {
			return err
		}
		for i := range numValidators {
			if err := appGenesis.SaveAs(genFiles[i]); err != nil {
				return err
			}
		}
		return nil
	}

	appGenState := mm.DefaultGenesis(clientCtx.Codec)

	// set the accounts in the genesis state
	var authGenState authtypes.GenesisState
	clientCtx.Codec.MustUnmarshalJSON(appGenState[authtypes.ModuleName], &authGenState)

	accounts, err := authtypes.PackAccounts(genAccounts)
	if err != nil {
		return err
	}

	authGenState.Accounts = accounts
	appGenState[authtypes.ModuleName] = clientCtx.Codec.MustMarshalJSON(&authGenState)

	// set the balances in the genesis state
	var bankGenState banktypes.GenesisState
	clientCtx.Codec.MustUnmarshalJSON(appGenState[banktypes.ModuleName], &bankGenState)

	bankGenState.Balances = banktypes.SanitizeGenesisBalances(genBalances)

	for _, bal := range bankGenState.Balances {
		bankGenState.Supply = bankGenState.Supply.Add(bal.Coins...)
	}
	appGenState[banktypes.ModuleName] = clientCtx.Codec.MustMarshalJSON(&bankGenState)

	appGenStateJSON, err := json.MarshalIndent(appGenState, "", "  ")
	if err != nil {
		return err
	}

	appGenesis := genutiltypes.NewAppGenesisWithVersion(chainID, appGenStateJSON)
	// generate empty genesis files for each validator and save
	for i := range numValidators {
		if err := appGenesis.SaveAs(genFiles[i]); err != nil {
			return err
		}
	}
	return nil
}

func collectGenFiles(
	clientCtx client.Context,
	nodeConfig *cmtcfg.Config,
	chainID string,
	nodeIDs []string,
	valPubKeys []cryptotypes.PubKey,
	numValidators int,
	outputDir, nodeDirPrefix, nodeDaemonHome string,
	genBalIterator banktypes.GenesisBalancesIterator,
	p2pPortStart int,
	singleMachine bool,
) error {
	var appState json.RawMessage
	genTime := cmttime.Now()

	for i := range numValidators {
		// GenAppStateFromConfig writes config.toml, so the per-node listeners
		// are set here: set in initTestnetFiles, the last node's would win.
		if singleMachine {
			portOffset := i
			nodeConfig.RPC.ListenAddress = fmt.Sprintf("tcp://0.0.0.0:%d", rpcPort+portOffset)
			nodeConfig.P2P.ListenAddress = fmt.Sprintf("tcp://0.0.0.0:%d", p2pPortStart+portOffset)
			nodeConfig.RPC.PprofListenAddress = fmt.Sprintf("localhost:%d", pprofPort+portOffset)
			nodeConfig.Instrumentation.PrometheusListenAddr = fmt.Sprintf(":%d", cometMetricsPort+portOffset)
		}

		nodeDirName := fmt.Sprintf("%s%d", nodeDirPrefix, i)
		nodeDir := filepath.Join(outputDir, nodeDirName, nodeDaemonHome)
		gentxsDir := filepath.Join(outputDir, "gentxs")
		nodeConfig.Moniker = nodeDirName

		nodeConfig.SetRoot(nodeDir)

		nodeID, valPubKey := nodeIDs[i], valPubKeys[i]
		initCfg := genutiltypes.NewInitConfig(chainID, gentxsDir, nodeID, valPubKey)

		appGenesis, err := genutiltypes.AppGenesisFromFile(nodeConfig.GenesisFile())
		if err != nil {
			return err
		}

		nodeAppState, err := genutil.GenAppStateFromConfig(
			clientCtx.Codec,
			clientCtx.TxConfig,
			nodeConfig,
			initCfg,
			appGenesis,
			genBalIterator,
			genutiltypes.DefaultMessageValidator,
			clientCtx.TxConfig.SigningContext().ValidatorAddressCodec(),
		)
		if err != nil {
			return err
		}

		if appState == nil {
			// set the canonical application state (they should not differ)
			appState = nodeAppState
		}

		genFile := nodeConfig.GenesisFile()

		// overwrite each validator's genesis file to have a canonical genesis time
		if err := genutil.ExportGenesisFileWithTime(genFile, chainID, nil, appState, genTime); err != nil {
			return err
		}
	}

	return nil
}

// finaliseGenesisConsensusParams sets what module genesis cannot reach: the
// governance authority, and vote extensions from height 1, which oracle votes
// ride on.
func finaliseGenesisConsensusParams(genFile string, base *genutiltypes.AppGenesis) error {
	genesis, err := genutiltypes.AppGenesisFromFile(genFile)
	if err != nil {
		return fmt.Errorf("reading generated genesis %s: %w", genFile, err)
	}
	if genesis.Consensus == nil || genesis.Consensus.Params == nil {
		return fmt.Errorf("generated genesis %s has no consensus params", genFile)
	}
	// Collection rebuilt the file with default consensus params; an
	// artefact's block, evidence age and gas budget included, goes back first.
	if base != nil && base.Consensus != nil && base.Consensus.Params != nil {
		params := *base.Consensus.Params
		genesis.Consensus.Params = &params
	}

	genesis.Consensus.Params.Authority.Authority = authtypes.NewModuleAddress(govtypes.ModuleName).String()
	genesis.Consensus.Params.ABCI.VoteExtensionsEnableHeight = 1
	if err := genesis.SaveAs(genFile); err != nil {
		return fmt.Errorf("saving generated genesis %s: %w", genFile, err)
	}
	return nil
}

func getIP(i int, startingIPAddr string) (ip string, err error) {
	if len(startingIPAddr) == 0 {
		ip, err = server.ExternalIP()
		if err != nil {
			return "", err
		}
		return ip, nil
	}
	return calculateIP(startingIPAddr, i)
}

// maxOctet is the largest value the final IPv4 octet can hold.
const maxOctet = 255

func calculateIP(ip string, i int) (string, error) {
	ipv4 := net.ParseIP(ip).To4()
	if ipv4 == nil {
		return "", fmt.Errorf("%v: non ipv4 address", ip)
	}
	// Refusing the carry keeps every node inside the operator's chosen /24.
	// Incrementing the octet in a loop instead wraps it silently, which hands
	// two validators the same address and a peer list that cannot converge.
	if i < 0 || int(ipv4[3])+i > maxOctet {
		return "", fmt.Errorf("%s: offset %d overflows the last octet", ip, i)
	}
	ipv4[3] += byte(i)

	return ipv4.String(), nil
}

func writeFile(name, dir string, contents []byte) error {
	file := filepath.Join(dir, name)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("could not create directory %q: %w", dir, err)
	}

	if err := os.WriteFile(file, contents, 0o600); err != nil {
		return err
	}

	return nil
}

// startTestnet starts an in-process testnet
func startTestnet(cmd *cobra.Command, args startArgs) error {
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

// seatGentx is a seat's self-delegation: the whole locked grant at the
// artefact's commission floor, with a 20% ceiling and a 1% daily change.
func seatGentx(cdc codec.Codec, base *genutiltypes.AppGenesis) (sdk.Coin, stakingtypes.CommissionRates, error) {
	var appState map[string]json.RawMessage
	if err := json.Unmarshal(base.AppState, &appState); err != nil {
		return sdk.Coin{}, stakingtypes.CommissionRates{}, fmt.Errorf("unmarshal base app state: %w", err)
	}
	var stakingGenesis stakingtypes.GenesisState
	if err := cdc.UnmarshalJSON(appState[stakingtypes.ModuleName], &stakingGenesis); err != nil {
		return sdk.Coin{}, stakingtypes.CommissionRates{}, fmt.Errorf("unmarshal base staking genesis: %w", err)
	}
	floor := stakingGenesis.Params.MinCommissionRate
	ceiling := math.LegacyMaxDec(floor, math.LegacyMustNewDecFromStr("0.2"))
	return sdk.NewCoin(chain.NoahBaseDenom, chain.NativeBaseAmount(chain.SeatGrantNoah)),
		stakingtypes.NewCommissionRates(floor, ceiling, math.LegacyMustNewDecFromStr("0.01")),
		nil
}

// seatedGenesis is the artefact under chainID with one seat granted per
// validator; its consensus block is kept as written.
func seatedGenesis(cdc codec.Codec, base *genutiltypes.AppGenesis, chainID string, seats []sdk.AccAddress) (*genutiltypes.AppGenesis, error) {
	var appState map[string]json.RawMessage
	if err := json.Unmarshal(base.AppState, &appState); err != nil {
		return nil, fmt.Errorf("unmarshal base app state: %w", err)
	}
	for _, seat := range seats {
		if err := arkgenesis.AddValidatorSeat(cdc, appState, seat); err != nil {
			return nil, fmt.Errorf("seating %s: %w", seat, err)
		}
	}
	appStateJSON, err := json.MarshalIndent(appState, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal seated app state: %w", err)
	}
	seated := *base
	seated.ChainID = chainID
	seated.AppState = appStateJSON
	return &seated, nil
}
