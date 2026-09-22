package app_test

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"testing"
	"time"

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
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/testutil/mock"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/ararat-network/ark/app"
	arkgenesis "github.com/ararat-network/ark/app/genesis"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	claimstypes "github.com/ararat-network/ark/x/claims/types"
	markettypes "github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	reservetypes "github.com/ararat-network/ark/x/reserve/types"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// unwiredWasmKeeper stands in for the contract store the CLI graph never
// builds, as cmd/arkd does: no appointment can run through a basic manager.
type unwiredWasmKeeper struct{}

func (unwiredWasmKeeper) HasContractInfo(context.Context, sdk.AccAddress) bool {
	panic("contract store read from a graph with no state")
}

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
		depinject.Configs(app.AppConfig, depinject.Supply(log.NewNopLogger(), unwiredWasmKeeper{})),
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
		sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, chain.NativeBaseAmount(1000))),
		sdk.Coins(govGenesis.Params.MinDeposit),
	)
	require.Equal(
		t,
		sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, chain.NativeBaseAmount(5000))),
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

// launchGenesisPath identifies the reviewed network genesis and
// testnetGenesisPath its time-compressed derivation. Tests validate both
// through the CLI's manager and boot both with a funded validator.
const (
	launchGenesisPath  = "genesis/genesis.json"
	testnetGenesisPath = "genesis/testnet.json"
)

// loadGenesis loads an artefact and its app state.
func loadGenesis(t *testing.T, path string) (*genutiltypes.AppGenesis, map[string]json.RawMessage) {
	t.Helper()

	appGenesis, err := genutiltypes.AppGenesisFromFile(path)
	require.NoError(t, err)

	var state map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(appGenesis.AppState, &state))

	return appGenesis, state
}

// launchGenesis loads the launch artefact.
func launchGenesis(t *testing.T) (*genutiltypes.AppGenesis, map[string]json.RawMessage) {
	t.Helper()
	return loadGenesis(t, launchGenesisPath)
}

// artefacts lists the curated files the validity and boot tests run over.
func artefacts() []struct{ name, path string } {
	return []struct{ name, path string }{
		{"launch", launchGenesisPath},
		{"testnet", testnetGenesisPath},
	}
}

// TestLaunchGenesisIsValid runs each artefact through the validation
// `arkd genesis validate` would apply: the CLI basic manager over every
// module's slice, plus the genesis doc's own checks.
func TestLaunchGenesisIsValid(t *testing.T) {
	for _, artefact := range artefacts() {
		t.Run(artefact.name, func(t *testing.T) {
			appGenesis, state := loadGenesis(t, artefact.path)
			require.NoError(t, appGenesis.ValidateAndComplete())

			basics, cdc := cliBasicManager(t)
			var txConfig client.TxConfig
			require.NoError(t, depinject.Inject(
				depinject.Configs(app.AppConfig, depinject.Supply(log.NewNopLogger(), unwiredWasmKeeper{})),
				&txConfig,
			))

			require.NoError(t, basics.ValidateGenesis(cdc, txConfig, state))
		})
	}
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
	for _, artefact := range artefacts() {
		t.Run(artefact.name, func(t *testing.T) {
			appGenesis, state := loadGenesis(t, artefact.path)
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
			// dropping the artefact's metadata and supply ledger; restore both, or
			// Distribution finds no balance behind its community pool and panics.
			var bankGenesis banktypes.GenesisState
			arkApp.AppCodec().MustUnmarshalJSON(state[banktypes.ModuleName], &bankGenesis)
			bankGenesis.DenomMetadata = artefactBank.DenomMetadata
			bankGenesis.Balances = append(bankGenesis.Balances, artefactBank.Balances...)
			bankGenesis.Supply = bankGenesis.Supply.Add(artefactBank.Supply...)
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
		})
	}
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

// Launch supply ledger, mirrored in docs/governance/GENESIS.md §3. The
// artefact carries the whole community pool and zero reward targets; each
// seat assembly adds moves its grant and float out of the pool and adds one
// seat's share to both targets (app/genesis).
const (
	launchTotalSupplyNoah   = 1_000_000_000
	launchSubsidyNoah       = 100_000_000
	launchReserveNoah       = 50_000_000
	launchBufferNoah        = 10_000_000
	launchInsuranceNoah     = 5_000_000
	launchCommunityPoolNoah = 835_000_000
)

func noah(whole int64) math.Int { return chain.NativeBaseAmount(whole) }

// TestLaunchGenesisPinsChainParams pins the consensus and SDK-module values
// the launch review decided, each beside the coupling that fixed it.
func TestLaunchGenesisPinsChainParams(t *testing.T) {
	appGenesis, state := launchGenesis(t)
	_, cdc := cliBasicManager(t)

	consensus := appGenesis.Consensus.Params
	require.Equal(t, authtypes.NewModuleAddress(govtypes.ModuleName).String(), consensus.Authority.Authority,
		"chain authority is stated once, in consensus params")

	var stakingGenesis stakingtypes.GenesisState
	cdc.MustUnmarshalJSON(state[stakingtypes.ModuleName], &stakingGenesis)
	unbonding := stakingGenesis.Params.UnbondingTime
	require.Equal(t, 21*24*time.Hour, unbonding)
	// Evidence expires only once both bounds are exceeded, so each must reach
	// the unbonding period or a double-sign outlives its stake's exposure.
	require.EqualValues(t, 21*chain.BlocksPerDay, consensus.Evidence.MaxAgeNumBlocks)
	require.Equal(t, unbonding, consensus.Evidence.MaxAgeDuration)
	require.EqualValues(t, 100, stakingGenesis.Params.MaxValidators)
	require.Equal(t, math.LegacyMustNewDecFromStr("0.05"), stakingGenesis.Params.MinCommissionRate)

	var slashingGenesis slashingtypes.GenesisState
	cdc.MustUnmarshalJSON(state[slashingtypes.ModuleName], &slashingGenesis)
	require.EqualValues(t, 10_000, slashingGenesis.Params.SignedBlocksWindow)
	require.Equal(t, math.LegacyMustNewDecFromStr("0.05"), slashingGenesis.Params.MinSignedPerWindow)
	require.Equal(t, 10*time.Minute, slashingGenesis.Params.DowntimeJailDuration)
	require.Equal(t, math.LegacyMustNewDecFromStr("0.0001"), slashingGenesis.Params.SlashFractionDowntime)
	require.Equal(t, math.LegacyMustNewDecFromStr("0.05"), slashingGenesis.Params.SlashFractionDoubleSign)

	var govGenesis govv1.GenesisState
	cdc.MustUnmarshalJSON(state[govtypes.ModuleName], &govGenesis)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, noah(1_000))), sdk.Coins(govGenesis.Params.MinDeposit))
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, noah(5_000))), sdk.Coins(govGenesis.Params.ExpeditedMinDeposit))
	// Equal seats make governance one validator one vote, and the pool it
	// guards holds most of the supply: half the set must vote and two thirds
	// of the votes must agree.
	require.Equal(t, "0.500000000000000000", govGenesis.Params.Quorum)
	require.Equal(t, "0.667000000000000000", govGenesis.Params.Threshold)
	require.Equal(t, "0.750000000000000000", govGenesis.Params.ExpeditedThreshold, "the SDK requires it above the regular threshold")
	require.Equal(t, "0.334000000000000000", govGenesis.Params.VetoThreshold)
	require.Equal(t, 48*time.Hour, *govGenesis.Params.VotingPeriod)
	require.Equal(t, 24*time.Hour, *govGenesis.Params.ExpeditedVotingPeriod)

	var authGenesis authtypes.GenesisState
	cdc.MustUnmarshalJSON(state[authtypes.ModuleName], &authGenesis)
	require.EqualValues(t, 7, authGenesis.Params.TxSigLimit, "committee multisigs hold at most seven member keys")
	require.Empty(t, authGenesis.Accounts, "seats are added at assembly and committees appointed after launch")
}

// TestLaunchGenesisSupplyLedger pins the billion-NOAH supply and its
// allocation: four fund seeds as plain balances and the rest in the community
// pool, whose fee-pool entry must match the module balance or Distribution's
// InitGenesis panics.
func TestLaunchGenesisSupplyLedger(t *testing.T) {
	_, state := launchGenesis(t)
	_, cdc := cliBasicManager(t)

	bankGenesis := banktypes.GetGenesisStateFromAppState(cdc, state)
	want := map[string]math.Int{
		authtypes.NewModuleAddress(treasurytypes.SubsidyPoolName).String():      noah(launchSubsidyNoah),
		authtypes.NewModuleAddress(reservetypes.StrategicReserveName).String():  noah(launchReserveNoah),
		authtypes.NewModuleAddress(treasurytypes.RedemptionBufferName).String(): noah(launchBufferNoah),
		authtypes.NewModuleAddress(claimstypes.InsuranceName).String():          noah(launchInsuranceNoah),
		authtypes.NewModuleAddress(distrtypes.ModuleName).String():              noah(launchCommunityPoolNoah),
	}
	require.Len(t, bankGenesis.Balances, len(want))
	total := math.ZeroInt()
	for _, balance := range bankGenesis.Balances {
		amount, known := want[balance.Address]
		require.True(t, known, balance.Address)
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, amount)), balance.Coins, balance.Address)
		total = total.Add(amount)
	}
	require.Equal(t, noah(launchTotalSupplyNoah), total)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, total)), bankGenesis.Supply)

	var distrGenesis distrtypes.GenesisState
	cdc.MustUnmarshalJSON(state[distrtypes.ModuleName], &distrGenesis)
	require.Equal(t,
		sdk.NewDecCoinsFromCoins(sdk.NewCoin(chain.NoahBaseDenom, noah(launchCommunityPoolNoah))),
		distrGenesis.FeePool.CommunityPool,
	)
}

// TestLaunchGenesisPinsArkEconomics pins the launch values of the Ark modules.
func TestLaunchGenesisPinsArkEconomics(t *testing.T) {
	_, state := launchGenesis(t)
	_, cdc := cliBasicManager(t)

	var treasuryGenesis treasurytypes.GenesisState
	cdc.MustUnmarshalJSON(state[treasurytypes.ModuleName], &treasuryGenesis)
	params := treasuryGenesis.Params
	require.Equal(t, chain.XDRBaseDenom, params.ReferenceDenom)
	require.Equal(t, noah(1_000), params.ReferenceTaxCap, "1,000 XDR: proportional up to 200,000 XDR at 0.5%")
	require.Equal(t, math.LegacyMustNewDecFromStr("0.005"), params.TransferTaxRate)
	require.Equal(t, math.LegacyNewDec(2), params.MultiplierCap, "ratios summing to 0.5 reach full retention at 2")
	require.Equal(t, math.LegacyMustNewDecFromStr("0.1"), params.MultiplierMaxStep)

	policy := treasuryGenesis.EconomicPolicy
	require.True(t, policy.ValidatorBlockRewardTarget.IsZero() && policy.OracleBlockRewardTarget.IsZero(),
		"targets start at zero; every seat adds its share")
	require.Equal(t, math.LegacyMustNewDecFromStr("0.30"), policy.RedemptionBufferTargetRatio)
	require.Equal(t, math.LegacyMustNewDecFromStr("0.15"), policy.StrategicReserveTargetRatio)
	require.Equal(t, math.LegacyMustNewDecFromStr("0.05"), policy.InsuranceTargetRatio)
	require.True(t, policy.LiabilityRatioWeight.IsZero() && policy.VolatilityWeight.IsZero() && policy.FlowWeight.IsZero(),
		"weights stay zero until the calibration window (D72)")

	require.Len(t, treasuryGenesis.ConversionFactors, 1)
	require.Equal(t, chain.NoahBaseDenom, treasuryGenesis.ConversionFactors[0].Denom)
	require.Equal(t, math.LegacyMustNewDecFromStr("1.371"), treasuryGenesis.ConversionFactors[0].Factor,
		"NOAH opens at 1 USD with XDR at 1.371 USD; the first axdr rate replaces it")

	var marketGenesis markettypes.GenesisState
	cdc.MustUnmarshalJSON(state[markettypes.ModuleName], &marketGenesis)
	conversion := marketGenesis.ConversionPolicy
	require.Equal(t, sdk.NewDecCoinFromDec(chain.XDRBaseDenom, math.LegacyNewDecFromInt(noah(5_000_000))), conversion.BasePool)
	require.Equal(t, math.LegacyMustNewDecFromStr("0.02"), conversion.MinStabilitySpread)
	require.Equal(t, chain.BlocksPerDay, conversion.PoolRecoveryPeriod)
	require.Equal(t, math.LegacyMustNewDecFromStr("0.0025"), marketGenesis.Params.DefaultTobinTax)
	require.True(t, params.TransferTaxRate.LTE(conversion.MinStabilitySpread), "tax at or below the spread floor (D81)")

	var oracleGenesis oracletypes.GenesisState
	cdc.MustUnmarshalJSON(state[oracletypes.ModuleName], &oracleGenesis)
	require.Equal(t, chain.BlocksPerWeek, oracleGenesis.Params.RewardWindow)
	require.Equal(t, 13*chain.BlocksPerWeek, oracleGenesis.Params.RewardDistributionWindow,
		"a quarter: steady subsidy inflow needs no year of smoothing")
	require.Equal(t, oracleGenesis.Params.RewardDistributionWindow, oracleGenesis.Accounting.RewardDistributionWindow)

	var assetGenesis assettypes.GenesisState
	cdc.MustUnmarshalJSON(state[assettypes.ModuleName], &assetGenesis)
	require.Equal(t, 3*chain.BlocksPerDay, assetGenesis.Params.SettlementActivationDelayBlocks, "outlasts the two-day vote")
	currencies := map[string]string{
		chain.AUDBaseDenom: "Australian dollar",
		chain.CADBaseDenom: "Canadian dollar",
		chain.CNYBaseDenom: "Chinese yuan",
		chain.EURBaseDenom: "euro",
		chain.GBPBaseDenom: "pound sterling",
		chain.JPYBaseDenom: "Japanese yen",
		chain.KRWBaseDenom: "South Korean won",
		chain.MXNBaseDenom: "Mexican peso",
		chain.SGDBaseDenom: "Singapore dollar",
		chain.USDBaseDenom: "United States dollar",
	}
	require.Len(t, assetGenesis.Assets, len(currencies))
	for _, asset := range assetGenesis.Assets {
		require.Equal(t, "An Ark currency tracking the "+currencies[asset.Denom]+".", asset.Metadata.Description, asset.Denom)
	}
}

// TestLaunchGenesisBootsVestingSeat boots the artefact the way assembly
// builds it: one seat granted by arkgenesis.AddValidatorSeats, the function
// behind arkd genesis add-validator-seats, whose gentx self-delegates the
// grant while none of it has vested. Unvested coins cannot pay fees; the
// gentx pays none at height zero and the float pays afterwards.
func TestLaunchGenesisBootsVestingSeat(t *testing.T) {
	genesisTime := time.Date(2027, time.January, 4, 12, 0, 0, 0, time.UTC)
	for _, artefact := range artefacts() {
		t.Run(artefact.name, func(t *testing.T) {
			appGenesis, state := loadGenesis(t, artefact.path)
			arkApp := app.NewArkApp(
				log.NewTestLogger(t),
				dbm.NewMemDB(),
				true,
				simtestutil.NewAppOptionsWithFlagHome(t.TempDir()),
				baseapp.SetChainID(appGenesis.ChainID),
			)
			cdc := arkApp.AppCodec()
			txConfig := arkApp.TxConfig()

			// The operator key lives in a keyring so the gentx signs as arkd does.
			kr := keyring.NewInMemory(cdc)
			record, _, err := kr.NewMnemonic("seat", keyring.English, sdk.FullFundraiserPath, keyring.DefaultBIP39Passphrase, hd.Secp256k1)
			require.NoError(t, err)
			operator, err := record.GetAddress()
			require.NoError(t, err)

			grant := sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, noah(chain.SeatGrantNoah)))
			float := sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, noah(chain.SeatFloatNoah)))
			require.NoError(t, arkgenesis.AddValidatorSeats(cdc, state, []sdk.AccAddress{operator}, genesisTime))

			privVal := mock.NewPV()
			consPub, err := privVal.GetPubKey()
			require.NoError(t, err)
			consPubKey, err := cryptocodec.FromCmtPubKeyInterface(consPub)
			require.NoError(t, err)
			createValidator, err := stakingtypes.NewMsgCreateValidator(
				sdk.ValAddress(operator).String(),
				consPubKey,
				sdk.NewCoin(chain.NoahBaseDenom, noah(chain.SeatGrantNoah)),
				stakingtypes.NewDescription("seat", "", "", "", ""),
				stakingtypes.NewCommissionRates(
					math.LegacyMustNewDecFromStr("0.05"),
					math.LegacyMustNewDecFromStr("0.2"),
					math.LegacyMustNewDecFromStr("0.01"),
				),
				math.OneInt(),
			)
			require.NoError(t, err)
			txBuilder := txConfig.NewTxBuilder()
			require.NoError(t, txBuilder.SetMsgs(createValidator))
			factory := clienttx.Factory{}.WithChainID(appGenesis.ChainID).WithKeybase(kr).WithTxConfig(txConfig)
			require.NoError(t, clienttx.Sign(context.Background(), factory, "seat", txBuilder, true))
			gentx, err := txConfig.TxJSONEncoder()(txBuilder.GetTx())
			require.NoError(t, err)
			state[genutiltypes.ModuleName] = cdc.MustMarshalJSON(&genutiltypes.GenesisState{GenTxs: []json.RawMessage{gentx}})

			stateBytes, err := json.Marshal(state)
			require.NoError(t, err)
			consensusParams := appGenesis.Consensus.Params.ToProto()
			_, err = arkApp.InitChain(&cmtabci.RequestInitChain{
				Time:            genesisTime,
				ChainId:         appGenesis.ChainID,
				Validators:      []cmtabci.ValidatorUpdate{},
				ConsensusParams: &consensusParams,
				AppStateBytes:   stateBytes,
			})
			require.NoError(t, err)

			valSet := cmttypes.NewValidatorSet([]*cmttypes.Validator{cmttypes.NewValidator(consPub, chain.SeatGrantNoah)})
			_, err = arkApp.FinalizeBlock(&cmtabci.RequestFinalizeBlock{
				Height:             arkApp.LastBlockHeight() + 1,
				Time:               genesisTime,
				Hash:               arkApp.LastCommitID().Hash,
				NextValidatorsHash: valSet.Hash(),
			})
			require.NoError(t, err)
			_, err = arkApp.Commit()
			require.NoError(t, err)

			ctx := arkApp.NewContext(true)
			validator, err := arkApp.StakingKeeper.GetValidator(ctx, sdk.ValAddress(operator))
			require.NoError(t, err)
			require.True(t, validator.IsBonded())
			require.Equal(t, noah(chain.SeatGrantNoah), validator.Tokens)

			account, ok := arkApp.AccountKeeper.GetAccount(ctx, operator).(*vestingtypes.ContinuousVestingAccount)
			require.True(t, ok, "the seat stays a continuous vesting account")
			require.Equal(t, grant, account.OriginalVesting)
			require.Equal(t, genesisTime.AddDate(chain.SeatVestingCliffYears, 0, 0).Unix(), account.StartTime)
			require.Equal(t, genesisTime.AddDate(chain.SeatVestingEndYears, 0, 0).Unix(), account.EndTime)
			require.Equal(t, grant, account.DelegatedVesting, "the whole grant is staked unvested")
			require.Equal(t, float, arkApp.BankKeeper.SpendableCoins(ctx, operator), "only the float is spendable")

			feePool, err := arkApp.DistrKeeper.FeePool.Get(ctx)
			require.NoError(t, err)
			require.Equal(t,
				math.LegacyNewDecFromInt(noah(launchCommunityPoolNoah).Sub(noah(chain.SeatGrantNoah+chain.SeatFloatNoah))),
				feePool.CommunityPool.AmountOf(chain.NoahBaseDenom),
			)
			require.Equal(t, noah(launchTotalSupplyNoah), arkApp.BankKeeper.GetSupply(ctx, chain.NoahBaseDenom).Amount)

			policy, err := arkApp.TreasuryKeeper.EconomicPolicy.Get(ctx)
			require.NoError(t, err)
			require.Equal(t, chain.SeatValidatorShare, policy.ValidatorBlockRewardTarget, "one seat, one share")
			require.Equal(t, chain.SeatOracleShare, policy.OracleBlockRewardTarget)
		})
	}
}

// testnetOverrides is the whole difference between the testnet artefact and
// the launch artefact: time compressed so a governance cycle, an unbonding, a
// settlement, and an oracle payout each complete inside a test day, and
// nothing else. Mirrored in docs/governance/GENESIS.md §15.
func testnetOverrides(cdc codec.Codec, launch *genutiltypes.AppGenesis, state map[string]json.RawMessage) {
	launch.ChainID = "ark-testnet-1"
	launch.Consensus.Params.Evidence.MaxAgeNumBlocks = int64(chain.BlocksPerDay)
	launch.Consensus.Params.Evidence.MaxAgeDuration = 24 * time.Hour

	var govGenesis govv1.GenesisState
	cdc.MustUnmarshalJSON(state[govtypes.ModuleName], &govGenesis)
	voting, expedited, deposit := time.Hour, 30*time.Minute, time.Hour
	govGenesis.Params.VotingPeriod = &voting
	govGenesis.Params.ExpeditedVotingPeriod = &expedited
	govGenesis.Params.MaxDepositPeriod = &deposit
	state[govtypes.ModuleName] = cdc.MustMarshalJSON(&govGenesis)

	var assetGenesis assettypes.GenesisState
	cdc.MustUnmarshalJSON(state[assettypes.ModuleName], &assetGenesis)
	assetGenesis.Params.SettlementActivationDelayBlocks = 2 * chain.BlocksPerHour
	state[assettypes.ModuleName] = cdc.MustMarshalJSON(&assetGenesis)

	var claimsGenesis claimstypes.GenesisState
	cdc.MustUnmarshalJSON(state[claimstypes.ModuleName], &claimsGenesis)
	claimsGenesis.Params.ClaimCancellationPeriodBlocks = 4 * chain.BlocksPerHour
	state[claimstypes.ModuleName] = cdc.MustMarshalJSON(&claimsGenesis)

	var stakingGenesis stakingtypes.GenesisState
	cdc.MustUnmarshalJSON(state[stakingtypes.ModuleName], &stakingGenesis)
	stakingGenesis.Params.UnbondingTime = 24 * time.Hour
	state[stakingtypes.ModuleName] = cdc.MustMarshalJSON(&stakingGenesis)

	var treasuryGenesis treasurytypes.GenesisState
	cdc.MustUnmarshalJSON(state[treasurytypes.ModuleName], &treasuryGenesis)
	treasuryGenesis.Params.RewardFundingWindow = chain.BlocksPerDay
	state[treasurytypes.ModuleName] = cdc.MustMarshalJSON(&treasuryGenesis)

	var oracleGenesis oracletypes.GenesisState
	cdc.MustUnmarshalJSON(state[oracletypes.ModuleName], &oracleGenesis)
	oracleGenesis.Params.RewardWindow = chain.BlocksPerDay
	oracleGenesis.Params.AttendanceWindow = chain.BlocksPerDay
	oracleGenesis.Params.RewardDistributionWindow = chain.BlocksPerWeek
	oracleGenesis.Accounting.RewardWindow = chain.BlocksPerDay
	oracleGenesis.Accounting.AttendanceWindow = chain.BlocksPerDay
	oracleGenesis.Accounting.RewardDistributionWindow = chain.BlocksPerWeek
	state[oracletypes.ModuleName] = cdc.MustMarshalJSON(&oracleGenesis)
}

// TestTestnetGenesisDerivesFromLaunch pins the testnet artefact to the launch
// artefact plus testnetOverrides, module by module, so the two cannot drift.
func TestTestnetGenesisDerivesFromLaunch(t *testing.T) {
	launch, launchState := launchGenesis(t)
	testnet, testnetState := loadGenesis(t, testnetGenesisPath)
	_, cdc := cliBasicManager(t)

	testnetOverrides(cdc, launch, launchState)

	require.Equal(t, launch.ChainID, testnet.ChainID)
	require.Equal(t, launch.GenesisTime, testnet.GenesisTime)
	require.Equal(t, launch.InitialHeight, testnet.InitialHeight)
	require.Equal(t, launch.Consensus.Params, testnet.Consensus.Params)
	require.Equal(t, slices.Sorted(maps.Keys(launchState)), slices.Sorted(maps.Keys(testnetState)))
	for module, want := range launchState {
		require.JSONEq(t, string(want), string(testnetState[module]), module)
	}
}
