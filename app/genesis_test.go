package app_test

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	dbm "github.com/cosmos/cosmos-db"
	ibcwasmtypes "github.com/cosmos/ibc-go/modules/light-clients/08-wasm/v11/types"
	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"
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

// TestCLIAndAppGenesisAgree pins the property every genesis generator rests on:
// `arkd init`, testnet init-files, the app, and the sims all compose from one
// BasicManager, so a module or default added for one reaches all of them.
//
// The failure it guards is silent. A chain-specific default applied above the
// composition — post-processing an assembled map rather than overriding the
// module's own basic — is invisible to whichever callers do not run that code,
// and the genesis they emit differs from the one every test asserts against.
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

// TestInitGenesisFollowsRegistryDependencies pins the init order the asset
// registry imposes. Asset genesis checks every oracle-priced asset's feed
// against the Oracle registry, and Treasury and Reserve genesis check their
// denoms against the asset registry, so each must find the registry it reads
// already imported. A wrong order fails InitChain loudly rather than silently;
// the pin is so the failure names the wiring instead of a genesis file.
func TestInitGenesisFollowsRegistryDependencies(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	order := arkApp.ModuleManager.OrderInitGenesis

	requireOrderBefore(t, order, oracletypes.ModuleName, assettypes.ModuleName)
	requireOrderBefore(t, order, assettypes.ModuleName, treasurytypes.ModuleName)
	requireOrderBefore(t, order, assettypes.ModuleName, reservetypes.ModuleName)
}

// launchGenesisPath is the reviewed artefact a network starts from. The tests
// below are what let it be data rather than code: they parse it, validate it
// with the manager `arkd genesis validate` uses, and boot a chain from it, so
// an SDK upgrade or a hand edit that breaks it fails here instead of on a
// validator at launch.
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

// TestLaunchGenesisCarriesArkEconomics pins the launch values that used to be
// genesis defaults: distribution's community tax is zero, and the native unit
// carries its metadata. They are asserted against the artefact because the
// artefact is now their only carrier — `arkd init` emits SDK defaults.
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
	_, err = arkApp.InitChain(&abci.RequestInitChain{
		Validators:      []abci.ValidatorUpdate{},
		ConsensusParams: &consensusParams,
		AppStateBytes:   stateBytes,
	})
	require.NoError(t, err)

	_, err = arkApp.FinalizeBlock(&abci.RequestFinalizeBlock{
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
}

// The contract runtime must ship shut. Wasmd's default genesis opens upload
// and instantiation to everybody and Ark keeps that default in code, so the
// launch posture holds in exactly one place: the curated genesis under
// app/genesis. This pins it there.
func TestWasmGenesisShipsDisabled(t *testing.T) {
	_, appState := launchGenesis(t)
	require.NotNil(t, appState[wasmtypes.ModuleName], "wasm genesis must be present")

	var state wasmtypes.GenesisState
	require.NoError(t, json.Unmarshal(appState[wasmtypes.ModuleName], &state))

	require.Equal(t, wasmtypes.AccessTypeNobody, state.Params.CodeUploadAccess.Permission,
		"nobody may upload contract code at launch")
	require.Equal(t, wasmtypes.AccessTypeNobody, state.Params.InstantiateDefaultPermission,
		"nobody may instantiate contracts at launch")
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
