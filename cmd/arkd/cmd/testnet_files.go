// SPDX-License-Identifier: Apache-2.0
// Adapted from Cosmos SDK, simapp/simd/cmd/testnet.go.
// Modified for Ark: testnet configuration, genesis, and node setup.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	cmtcfg "github.com/cometbft/cometbft/config"
	cmttime "github.com/cometbft/cometbft/types/time"

	"cosmossdk.io/math"
	"cosmossdk.io/math/unsafe"

	"github.com/cosmos/cosmos-sdk/client"
	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	arkgenesis "github.com/ararat-network/ark/app/genesis"
	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/telemetry"
)

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

// nodeName is validator i's directory, moniker, and key name.
func (o testnetInitOptions) nodeName(i int) string {
	return fmt.Sprintf("%s%d", o.nodeDirPrefix, i)
}

// nodeDir is validator i's home.
func (o testnetInitOptions) nodeDir(i int) string {
	return filepath.Join(o.outputDir, o.nodeName(i), o.nodeDaemonHome)
}

// gentxsDir is where every validator's gentx is collected from.
func (o testnetInitOptions) gentxsDir() string {
	return filepath.Join(o.outputDir, "gentxs")
}

// runTestnetInitFiles initialises testnet files for a testnet to be run in a separate process
func runTestnetInitFiles(
	ctx context.Context,
	clientCtx client.Context,
	nodeConfig *cmtcfg.Config,
	mm module.BasicManager,
	genBalIterator banktypes.GenesisBalancesIterator,
	options testnetInitOptions,
	in io.Reader,
	errOut io.Writer,
) error {
	var base *genutiltypes.AppGenesis
	if options.genesisFile != "" {
		var err error
		if base, err = genutiltypes.AppGenesisFromFile(options.genesisFile); err != nil {
			return fmt.Errorf("reading base genesis %s: %w", options.genesisFile, err)
		}
		if options.chainID == "" {
			options.chainID = base.ChainID
		}
	}
	if options.chainID == "" {
		options.chainID = "chain-" + unsafe.Str(6)
	}
	nodeIDs := make([]string, options.numValidators)
	valPubKeys := make([]cryptotypes.PubKey, options.numValidators)

	appConfig := defaultAppConfig()
	appConfig.MinGasPrices = options.minGasPrices
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
	if options.singleMachine {
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

	inBuf := bufio.NewReader(in)
	// generate private keys, node IDs, and initial transactions
	for i := range options.numValidators {
		var portOffset int
		if options.singleMachine {
			portOffset = i
			appConfig.API.Address = fmt.Sprintf("tcp://0.0.0.0:%d", apiPort+portOffset)
			appConfig.GRPC.Address = fmt.Sprintf("0.0.0.0:%d", grpcPort+portOffset)
		}

		nodeDirName := options.nodeName(i)
		nodeDir := options.nodeDir(i)

		nodeConfig.SetRoot(nodeDir)
		nodeConfig.Moniker = nodeDirName
		nodeConfig.RPC.ListenAddress = fmt.Sprintf("tcp://%s:%d", options.listenIPAddress, rpcPort+portOffset)

		if err := os.MkdirAll(filepath.Join(nodeDir, "config"), nodeDirPerm); err != nil {
			return err
		}
		var (
			err error
			ip  string
		)
		if options.singleMachine {
			ip = "127.0.0.1"
		} else {
			ip, err = getIP(i, options.startingIPAddress)
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

		kb, err := keyring.New(sdk.KeyringServiceName(), options.keyringBackend, nodeDir, inBuf, clientCtx.Codec)
		if err != nil {
			return err
		}

		keyringAlgos, _ := kb.SupportedAlgorithms()
		algo, err := keyring.NewSigningAlgoFromString(options.algo, keyringAlgos)
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

		// The SDK writes client.toml lazily and with its defaults, so every
		// client command against the home would need the chain ID, keyring,
		// key, and node as flags.
		clientToml := fmt.Sprintf(clientConfigTemplate, options.chainID, options.keyringBackend, nodeDirName, rpcPort+portOffset)
		if err := writeFile("client.toml", filepath.Join(nodeDir, "config"), []byte(clientToml)); err != nil {
			return err
		}

		// From code defaults each validator holds test balances and bonds a
		// token; from an artefact it is a seat, bonding its whole grant at
		// the artefact's commission floor.
		bond := sdk.NewCoin(options.bondTokenDenom, chain.NativeBaseAmount(100))
		commission := stakingtypes.NewCommissionRates(math.LegacyOneDec(), math.LegacyOneDec(), math.LegacyOneDec())
		if base != nil {
			seats = append(seats, addr)
			if bond, commission, err = seatGentx(clientCtx.Codec, base); err != nil {
				return err
			}
		} else {
			coins := sdk.Coins{
				sdk.NewCoin("testtoken", chain.NativeBaseAmount(1_000)),
				sdk.NewCoin(options.bondTokenDenom, chain.NativeBaseAmount(500)),
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
			WithChainID(options.chainID).
			WithMemo(memo).
			WithKeybase(kb).
			WithTxConfig(clientCtx.TxConfig)

		if err := clienttx.Sign(ctx, txFactory, nodeDirName, txBuilder, true); err != nil {
			return err
		}

		txBz, err := clientCtx.TxConfig.TxJSONEncoder()(txBuilder.GetTx())
		if err != nil {
			return err
		}

		if err := writeFile(fmt.Sprintf("%v.json", nodeDirName), options.gentxsDir(), txBz); err != nil {
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

	// One time for every file: the seats vest from it, and collection stamps it.
	genTime := cmttime.Now()
	if err := initGenFiles(clientCtx, mm, options.chainID, genTime, base, seats, genAccounts, genBalances, genFiles); err != nil {
		return err
	}

	err := collectGenFiles(clientCtx, nodeConfig, options, genTime, nodeIDs, valPubKeys, genBalIterator, p2pPortStart)
	if err != nil {
		return err
	}
	for _, genFile := range genFiles {
		if err := finaliseGenesisConsensusParams(genFile, base); err != nil {
			return err
		}
	}

	_, err = fmt.Fprintf(errOut, "Successfully initialised %d node directories\n", options.numValidators)
	return err
}

func initGenFiles(
	clientCtx client.Context, mm module.BasicManager, chainID string, genTime time.Time,
	base *genutiltypes.AppGenesis, seats []sdk.AccAddress,
	genAccounts []authtypes.GenesisAccount, genBalances []banktypes.Balance,
	genFiles []string,
) error {
	if base != nil {
		appGenesis, err := seatedGenesis(clientCtx.Codec, base, chainID, genTime, seats)
		if err != nil {
			return err
		}
		for _, genFile := range genFiles {
			if err := saveGenesis(appGenesis, genFile); err != nil {
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
	for _, genFile := range genFiles {
		if err := saveGenesis(appGenesis, genFile); err != nil {
			return err
		}
	}
	return nil
}

func collectGenFiles(
	clientCtx client.Context,
	nodeConfig *cmtcfg.Config,
	options testnetInitOptions,
	genTime time.Time,
	nodeIDs []string,
	valPubKeys []cryptotypes.PubKey,
	genBalIterator banktypes.GenesisBalancesIterator,
	p2pPortStart int,
) error {
	var appState json.RawMessage

	for i := range options.numValidators {
		// GenAppStateFromConfig writes config.toml, so the per-node listeners
		// are set here: set in runTestnetInitFiles, the last node's would win.
		if options.singleMachine {
			portOffset := i
			nodeConfig.RPC.ListenAddress = fmt.Sprintf("tcp://0.0.0.0:%d", rpcPort+portOffset)
			nodeConfig.P2P.ListenAddress = fmt.Sprintf("tcp://0.0.0.0:%d", p2pPortStart+portOffset)
			nodeConfig.RPC.PprofListenAddress = fmt.Sprintf("localhost:%d", pprofPort+portOffset)
			nodeConfig.Instrumentation.PrometheusListenAddr = fmt.Sprintf(":%d", cometMetricsPort+portOffset)
		}

		nodeConfig.Moniker = options.nodeName(i)
		nodeConfig.SetRoot(options.nodeDir(i))

		nodeID, valPubKey := nodeIDs[i], valPubKeys[i]
		initCfg := genutiltypes.NewInitConfig(options.chainID, options.gentxsDir(), nodeID, valPubKey)

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
		if err := exportGenesisFileWithTime(genFile, options.chainID, nil, appState, genTime); err != nil {
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
	if err := saveGenesis(genesis, genFile); err != nil {
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
	ipv4[3] += byte(i) //nolint:gosec // G115: the check above holds the sum within the octet.

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

// clientConfigTemplate is the SDK's client.toml, which the SDK renders only
// with its defaults: chain ID, keyring backend, default key, and node.
const clientConfigTemplate = `# This is a TOML config file.
# For more information, see https://github.com/toml-lang/toml

###############################################################################
###                           Client Configuration                            ###
###############################################################################

# The network chain ID
chain-id = "%s"
# The keyring's backend, where the keys are stored (os|file|kwallet|pass|test|memory)
keyring-backend = "%s"
# Default key name, if set, defines the default key to use for signing transaction when the --from flag is not specified
keyring-default-keyname = "%s"
# CLI output format (text|json)
output = "text"
# <host>:<port> to CometBFT RPC interface for this chain
node = "tcp://localhost:%d"
# Transaction broadcasting mode (sync|async)
broadcast-mode = "sync"
`

// seatGentx is a seat's self-delegation: one seat, the founder's included,
// at the artefact's commission floor, with a 20% ceiling and a 1% daily
// change.
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
	return sdk.NewCoin(chain.NoahBaseDenom, chain.SeatGrant),
		stakingtypes.NewCommissionRates(floor, ceiling, math.LegacyMustNewDecFromStr("0.01")),
		nil
}

// seatedGenesis is the artefact under chainID and genTime with one seat
// granted per validator, the first validator's the founder's, vesting from
// genTime; its consensus block is kept as written.
func seatedGenesis(cdc codec.Codec, base *genutiltypes.AppGenesis, chainID string, genTime time.Time, seats []sdk.AccAddress) (*genutiltypes.AppGenesis, error) {
	var appState map[string]json.RawMessage
	if err := json.Unmarshal(base.AppState, &appState); err != nil {
		return nil, fmt.Errorf("unmarshal base app state: %w", err)
	}
	if err := arkgenesis.AddValidatorSeats(cdc, appState, seats, seats[0], genTime); err != nil {
		return nil, err
	}
	appStateJSON, err := json.MarshalIndent(appState, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal seated app state: %w", err)
	}
	seated := *base
	seated.ChainID = chainID
	seated.GenesisTime = genTime
	seated.AppState = appStateJSON
	return &seated, nil
}
