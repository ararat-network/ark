package app_test

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	dbm "github.com/cosmos/cosmos-db"
	ibcwasmtypes "github.com/cosmos/ibc-go/modules/light-clients/08-wasm/v11/types"
	icagenesistypes "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/genesis/types"
	icatypes "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	ibcexported "github.com/cosmos/ibc-go/v11/modules/core/exported"
	ibctypes "github.com/cosmos/ibc-go/v11/modules/core/types"
	"github.com/stretchr/testify/require"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"cosmossdk.io/depinject"
	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/testutil/mock"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"github.com/ararat-network/ark/app"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	reservetypes "github.com/ararat-network/ark/x/reserve/types"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// cliBasicManager rebuilds the manager the arkd commands generate genesis
// from: the depinject-resolved set, plus the IBC, Wasm, and GMP basics
// NewRootCmd merges in by hand because those modules are wired outside
// depinject.
func cliBasicManager(t *testing.T) (module.BasicManager, codec.Codec) {
	t.Helper()

	var (
		basics module.BasicManager
		cdc    codec.Codec
	)
	require.NoError(t, depinject.Inject(
		depinject.Configs(app.AppConfig, depinject.Supply(log.NewNopLogger())),
		&basics,
		&cdc,
	))

	for _, manual := range []module.BasicManager{
		app.IBCModuleBasics(),
		app.WasmModuleBasics(),
		app.GMPModuleBasics(),
	} {
		for name, basic := range manual {
			basics[name] = basic
		}
	}

	return basics, cdc
}

// TestCLIAndAppGenesisAgree checks that CLI, application, and simulation genesis use the same
// module basics and defaults.
func TestCLIAndAppGenesisAgree(t *testing.T) {
	arkApp := app.NewArkApp(
		log.NewTestLogger(t),
		dbm.NewMemDB(),
		true,
		simtestutil.NewAppOptionsWithFlagHome(t.TempDir()),
	)

	basics, cdc := cliBasicManager(t)
	cli := basics.DefaultGenesis(cdc)
	fromApp := arkApp.DefaultGenesis()

	require.NotEmpty(t, cli)
	require.Equal(t,
		slices.Sorted(maps.Keys(fromApp)),
		slices.Sorted(maps.Keys(cli)),
		"the CLI and the app disagree on which modules have genesis",
	)

	for name, cliState := range cli {
		appState := fromApp[name]
		if len(cliState) == 0 {
			require.Empty(t, appState, name)
			continue
		}
		require.JSONEq(t, string(cliState), string(appState), name)
	}
}

// TestDefaultGenesisSetsGovDeposits pins the gov deposit defaults config.go's
// init mutates at the package level. Bank and distribution carry no Ark
// defaults any more: launch values live in app/genesis/genesis.json, pinned by
// the launch test.
func TestDefaultGenesisSetsGovDeposits(t *testing.T) {
	arkApp := app.NewArkApp(
		log.NewTestLogger(t),
		dbm.NewMemDB(),
		true,
		simtestutil.NewAppOptionsWithFlagHome(t.TempDir()),
	)

	var govGenesis govv1.GenesisState
	arkApp.AppCodec().MustUnmarshalJSON(
		arkApp.DefaultGenesis()[govtypes.ModuleName],
		&govGenesis,
	)
	require.NotNil(t, govGenesis.Params)
	require.Equal(
		t,
		sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, chain.NativeBaseAmount(10))),
		sdk.Coins(govGenesis.Params.MinDeposit),
	)
	require.Equal(
		t,
		sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, chain.NativeBaseAmount(50))),
		sdk.Coins(govGenesis.Params.ExpeditedMinDeposit),
	)
}

// TestInitGenesisFollowsRegistryDependencies checks Oracle before Asset, then Asset before Treasury
// and Reserve, so each genesis importer can validate against its dependency registry.
func TestInitGenesisFollowsRegistryDependencies(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	order := arkApp.ModuleManager.OrderInitGenesis

	requireOrderBefore(t, order, oracletypes.ModuleName, assettypes.ModuleName)
	requireOrderBefore(t, order, assettypes.ModuleName, treasurytypes.ModuleName)
	requireOrderBefore(t, order, assettypes.ModuleName, reservetypes.ModuleName)
}

// launchGenesisPath identifies the reviewed network genesis. Tests validate it through the CLI's
// manager and boot it with a funded validator.
const launchGenesisPath = "genesis/genesis.json"

// launchGenesis loads the artefact and its app state.
func launchGenesis(t *testing.T) (*genutiltypes.AppGenesis, map[string]json.RawMessage) {
	t.Helper()

	appGenesis, err := genutiltypes.AppGenesisFromFile(launchGenesisPath)
	require.NoError(t, err)

	var state map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(appGenesis.AppState, &state))

	return appGenesis, state
}

// TestLaunchGenesisIsValid runs the artefact through the validation
// `arkd genesis validate` would apply: the CLI basic manager over every
// module's slice, plus the genesis doc's own checks.
func TestLaunchGenesisIsValid(t *testing.T) {
	appGenesis, state := launchGenesis(t)
	require.NoError(t, appGenesis.ValidateAndComplete())

	basics, cdc := cliBasicManager(t)
	var txConfig client.TxConfig
	require.NoError(t, depinject.Inject(
		depinject.Configs(app.AppConfig, depinject.Supply(log.NewNopLogger())),
		&txConfig,
	))

	require.NoError(t, basics.ValidateGenesis(cdc, txConfig, state))
}

// TestLaunchGenesisCarriesArkEconomics checks the launch artefact: zero Distribution community tax
// and native-unit metadata. arkd init emits SDK defaults.
func TestLaunchGenesisCarriesArkEconomics(t *testing.T) {
	_, state := launchGenesis(t)
	_, cdc := cliBasicManager(t)

	var distrGenesis distrtypes.GenesisState
	cdc.MustUnmarshalJSON(state[distrtypes.ModuleName], &distrGenesis)
	require.True(t, distrGenesis.Params.CommunityTax.IsZero(), distrGenesis.Params.CommunityTax)
	sdkDefault := distrtypes.DefaultParams()
	sdkDefault.CommunityTax = distrGenesis.Params.CommunityTax
	require.Equal(t, sdkDefault, distrGenesis.Params, "only the community tax departs from the SDK default")

	bankGenesis := banktypes.GetGenesisStateFromAppState(cdc, state)
	require.Len(t, bankGenesis.DenomMetadata, 1)
	// Proto-JSON equality: the file round-trip turns nil slices into empty
	// ones, which reflect equality would refuse and the wire does not.
	want := chain.NoahMetadata()
	require.JSONEq(t,
		string(cdc.MustMarshalJSON(&want)),
		string(cdc.MustMarshalJSON(&bankGenesis.DenomMetadata[0])),
	)
}

// TestLaunchGenesisBoots starts a chain from the artefact, adding only what a
// real launch adds on top of it: a validator set and a funded account. The
// first block must execute, and the artefact's economics must be the state the
// chain is left holding.
func TestLaunchGenesisBoots(t *testing.T) {
	appGenesis, state := launchGenesis(t)
	want := chain.NoahMetadata()

	privVal := mock.NewPV()
	pubKey, err := privVal.GetPubKey()
	require.NoError(t, err)
	valSet := cmttypes.NewValidatorSet([]*cmttypes.Validator{cmttypes.NewValidator(pubKey, 1)})

	senderPrivKey := secp256k1.GenPrivKey()
	acc := authtypes.NewBaseAccount(senderPrivKey.PubKey().Address().Bytes(), senderPrivKey.PubKey(), 0, 0)
	balance := banktypes.Balance{
		Address: acc.GetAddress().String(),
		Coins:   sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, chain.NativeBaseAmount(100_000_000))),
	}

	arkApp := app.NewArkApp(
		log.NewTestLogger(t),
		dbm.NewMemDB(),
		true,
		simtestutil.NewAppOptionsWithFlagHome(t.TempDir()),
	)
	artefactBank := banktypes.GetGenesisStateFromAppState(arkApp.AppCodec(), state)
	state, err = simtestutil.GenesisStateWithValSet(arkApp.AppCodec(), state, valSet, []authtypes.GenesisAccount{acc}, balance)
	require.NoError(t, err)
	// GenesisStateWithValSet rebuilds bank genesis around the test balances,
	// dropping the artefact's metadata; restore it before boot.
	var bankGenesis banktypes.GenesisState
	arkApp.AppCodec().MustUnmarshalJSON(state[banktypes.ModuleName], &bankGenesis)
	bankGenesis.DenomMetadata = artefactBank.DenomMetadata
	state[banktypes.ModuleName] = arkApp.AppCodec().MustMarshalJSON(&bankGenesis)
	stateBytes, err := json.Marshal(state)
	require.NoError(t, err)

	consensusParams := appGenesis.Consensus.Params.ToProto()
	_, err = arkApp.InitChain(&cmtabci.RequestInitChain{
		Validators:      []cmtabci.ValidatorUpdate{},
		ConsensusParams: &consensusParams,
		AppStateBytes:   stateBytes,
	})
	require.NoError(t, err)

	_, err = arkApp.FinalizeBlock(&cmtabci.RequestFinalizeBlock{
		Height:             arkApp.LastBlockHeight() + 1,
		Hash:               arkApp.LastCommitID().Hash,
		NextValidatorsHash: valSet.Hash(),
	})
	require.NoError(t, err)
	_, err = arkApp.Commit()
	require.NoError(t, err)

	ctx := arkApp.NewContext(true)
	distrParams, err := arkApp.DistrKeeper.Params.Get(ctx)
	require.NoError(t, err)
	require.True(t, distrParams.CommunityTax.IsZero())
	metadata, found := arkApp.BankKeeper.GetDenomMetaData(ctx, chain.NoahBaseDenom)
	require.True(t, found)
	require.JSONEq(t,
		string(arkApp.AppCodec().MustMarshalJSON(&want)),
		string(arkApp.AppCodec().MustMarshalJSON(&metadata)),
	)

	// The IBC and Wasm modules are registered by hand, outside depinject, so
	// a module whose InitGenesis the wiring skipped would boot on its code
	// defaults — which open every IBC surface the artefact shuts.
	require.Empty(t, arkApp.IBCKeeper.ClientKeeper.GetParams(ctx).AllowedClients)
	transferParams := arkApp.TransferKeeper.GetParams(ctx)
	require.False(t, transferParams.SendEnabled)
	require.False(t, transferParams.ReceiveEnabled)
	require.False(t, arkApp.ICAControllerKeeper.GetParams(ctx).ControllerEnabled)
	hostParams := arkApp.ICAHostKeeper.GetParams(ctx)
	require.False(t, hostParams.HostEnabled)
	require.Empty(t, hostParams.AllowMessages)
	wasmParams := arkApp.WasmKeeper.GetParams(ctx)
	require.Equal(t, wasmtypes.AccessTypeEverybody, wasmParams.CodeUploadAccess.Permission)
	require.Equal(t, wasmtypes.AccessTypeEverybody, wasmParams.InstantiateDefaultPermission)
}

// The launch genesis disables all IBC client types and separately disables transfer and ICA.
// Admitting a client type alone therefore opens only client creation; Wasm client checksums remain
// empty.
func TestLaunchGenesisHoldsTheHubShut(t *testing.T) {
	_, state := launchGenesis(t)
	_, cdc := cliBasicManager(t)

	var ibcGenesis ibctypes.GenesisState
	require.NoError(t, cdc.UnmarshalJSON(state[ibcexported.ModuleName], &ibcGenesis))
	require.Empty(t, ibcGenesis.ClientGenesis.Params.AllowedClients,
		"no client type may be created at launch; governance admits 07-tendermint when it opens the hub (D45, D50)")

	var transferGenesis ibctransfertypes.GenesisState
	require.NoError(t, cdc.UnmarshalJSON(state[ibctransfertypes.ModuleName], &transferGenesis))
	require.False(t, transferGenesis.Params.SendEnabled, "ICS-20 send is off until the activation matrix passes")
	require.False(t, transferGenesis.Params.ReceiveEnabled, "ICS-20 receive is off until the activation matrix passes")

	var icaGenesis icagenesistypes.GenesisState
	require.NoError(t, cdc.UnmarshalJSON(state[icatypes.ModuleName], &icaGenesis))
	require.False(t, icaGenesis.ControllerGenesisState.Params.ControllerEnabled, "ICA controller launches disabled")
	require.False(t, icaGenesis.HostGenesisState.Params.HostEnabled, "ICA host launches disabled")
	require.Empty(t, icaGenesis.HostGenesisState.Params.AllowMessages,
		"the host allowlist ships empty; activation names explicit type URLs, never the wildcard")

	require.NotContains(t, state, "mint", "Ark has no mint module (D1)")
}

// The launch genesis enables vote extensions from the first height so oracle pricing is available.
// Zero disables the pipeline.
func TestLaunchGenesisEnablesVoteExtensions(t *testing.T) {
	appGenesis, _ := launchGenesis(t)
	require.EqualValues(t, 1, appGenesis.Consensus.Params.ABCI.VoteExtensionsEnableHeight)
}

// The launch genesis permits contract upload and instantiation from height one and contains no
// deployed code or contracts. Execution policy, query restrictions, and the empty IBC client
// allowlist bound the runtime.
func TestWasmGenesisShipsOpen(t *testing.T) {
	_, appState := launchGenesis(t)
	require.NotNil(t, appState[wasmtypes.ModuleName], "wasm genesis must be present")

	var state wasmtypes.GenesisState
	require.NoError(t, json.Unmarshal(appState[wasmtypes.ModuleName], &state))

	require.Equal(t, wasmtypes.AccessTypeEverybody, state.Params.CodeUploadAccess.Permission,
		"anyone may upload contract code at launch")
	require.Equal(t, wasmtypes.AccessTypeEverybody, state.Params.InstantiateDefaultPermission,
		"anyone may instantiate contracts at launch")
	require.Empty(t, state.Codes, "launch genesis carries no contract code")
	require.Empty(t, state.Contracts, "launch genesis carries no contracts")
}

// The 08-wasm light-client host ships the same way: present, with no client
// code. Uploading a light client is a governance action after launch.
func TestWasmLightClientGenesisShipsEmpty(t *testing.T) {
	_, appState := launchGenesis(t)
	require.NotNil(t, appState[ibcwasmtypes.ModuleName], "08-wasm genesis must be present")

	var state ibcwasmtypes.GenesisState
	require.NoError(t, json.Unmarshal(appState[ibcwasmtypes.ModuleName], &state))
	require.Empty(t, state.Contracts, "launch genesis carries no light-client code")
}
