// Package testutil builds ArkApp fixtures for tests. It lives outside package
// app so the chain's own package exports the chain and nothing else.
package testutil

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmtjson "github.com/cometbft/cometbft/libs/json"

	"cosmossdk.io/log/v2"

	bam "github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	pruningtypes "github.com/cosmos/cosmos-sdk/store/v2/pruning/types"
	"github.com/cosmos/cosmos-sdk/testutil/network"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/pkg/chain"
)

// SetupOptions defines arguments that are passed into `ArkApp` constructor.
type SetupOptions struct {
	Logger  log.Logger
	DB      *dbm.MemDB
	AppOpts servertypes.AppOptions
}

// defaultFunderCoins is the opening balance of the genesis account Setup and
// NewArkappWithCustomOptions create.
func defaultFunderCoins() sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, chain.NativeBaseAmount(100_000_000)))
}

// setup builds a test app rooted at its own home directory. The home must be
// per-test: the Wasm VM takes an exclusive lock on its cache directory, so two
// apps sharing one home cannot exist at once, and DefaultNodeHome would put
// that cache in the developer's real ~/.arkd.
func setup(tb testing.TB, withGenesis bool) (*app.ArkApp, map[string]json.RawMessage) {
	tb.Helper()

	db := dbm.NewMemDB()

	appOptions := make(simtestutil.AppOptionsMap, 0)
	appOptions[flags.FlagHome] = tb.TempDir()

	arkApp := app.NewArkApp(log.NewNopLogger(), db, true, appOptions)
	if withGenesis {
		return arkApp, arkApp.DefaultGenesis()
	}
	return arkApp, map[string]json.RawMessage{}
}

// NewArkappWithCustomOptions initialises a new ArkApp with custom options.
func NewArkappWithCustomOptions(tb testing.TB, isCheckTx bool, options SetupOptions) *app.ArkApp {
	tb.Helper()

	validators := NewValidators(tb, 1)
	funder := NewFunder(tb, defaultFunderCoins())

	arkApp := app.NewArkApp(options.Logger, options.DB, true, options.AppOpts)
	genesisState, err := simtestutil.GenesisStateWithValSet(
		arkApp.AppCodec(), arkApp.DefaultGenesis(), validators.Set, funder.Accounts(), funder.Balance,
	)
	require.NoError(tb, err)

	if !isCheckTx {
		// init chain must be called to stop deliverState from being nil
		stateBytes, err := cmtjson.MarshalIndent(genesisState, "", " ")
		require.NoError(tb, err)

		// Initialise the chain
		_, err = arkApp.InitChain(&cmtabci.RequestInitChain{
			Validators:      []cmtabci.ValidatorUpdate{},
			ConsensusParams: simtestutil.DefaultConsensusParams,
			AppStateBytes:   stateBytes,
		})
		require.NoError(tb, err)
	}

	return arkApp
}

// Setup initialises a new ArkApp. A Nop logger is set in ArkApp.
func Setup(tb testing.TB, isCheckTx bool) *app.ArkApp {
	tb.Helper()

	funder := NewFunder(tb, defaultFunderCoins())

	return SetupWithGenesisValSet(tb, NewValidators(tb, 1), funder.Accounts(), funder.Balance)
}

// SetupWithGenesisValSet initialises a new ArkApp with a validator set and genesis accounts
// that also act as delegators. For simplicity, each validator is bonded with a delegation
// of one consensus engine unit in the default token of the ArkApp from first genesis
// account. A Nop logger is set in ArkApp.
func SetupWithGenesisValSet(tb testing.TB, validators Validators, genAccs []authtypes.GenesisAccount, balances ...banktypes.Balance) *app.ArkApp {
	tb.Helper()

	arkApp, genesisState := setup(tb, true)
	genesisState, err := simtestutil.GenesisStateWithValSet(arkApp.AppCodec(), genesisState, validators.Set, genAccs, balances...)
	require.NoError(tb, err)
	CorrectBondedPool(tb, arkApp.AppCodec(), genesisState, validators.Count())

	stateBytes, err := json.MarshalIndent(genesisState, "", " ")
	require.NoError(tb, err)

	// init chain will set the validator set and initialise the genesis accounts
	_, err = arkApp.InitChain(&cmtabci.RequestInitChain{
		Validators:      []cmtabci.ValidatorUpdate{},
		ConsensusParams: simtestutil.DefaultConsensusParams,
		AppStateBytes:   stateBytes,
	},
	)
	require.NoError(tb, err)

	_, err = arkApp.FinalizeBlock(&cmtabci.RequestFinalizeBlock{
		Height:             arkApp.LastBlockHeight() + 1,
		Hash:               arkApp.LastCommitID().Hash,
		NextValidatorsHash: validators.Set.Hash(),
	})
	require.NoError(tb, err)

	return arkApp
}

// NewTestNetworkFixture returns a new ArkApp AppConstructor for network simulation tests
func NewTestNetworkFixture() network.TestFixture {
	dir, err := os.MkdirTemp("", "arkapp")
	if err != nil {
		panic(fmt.Sprintf("failed creating temporary directory: %v", err))
	}
	defer os.RemoveAll(dir)

	arkApp := app.NewArkApp(log.NewNopLogger(), dbm.NewMemDB(), true, simtestutil.NewAppOptionsWithFlagHome(dir))

	appCtr := func(val network.ValidatorI) servertypes.Application {
		return app.NewArkApp(
			val.GetCtx().Logger, dbm.NewMemDB(), true,
			simtestutil.NewAppOptionsWithFlagHome(val.GetCtx().Config.RootDir),
			bam.SetPruning(pruningtypes.NewPruningOptionsFromString(val.GetAppConfig().Pruning)),
			bam.SetMinGasPrices(val.GetAppConfig().MinGasPrices),
			bam.SetChainID(val.GetCtx().Viper.GetString(flags.FlagChainID)),
		)
	}

	return network.TestFixture{
		AppConstructor: appCtr,
		GenesisState:   arkApp.DefaultGenesis(),
		EncodingConfig: moduletestutil.TestEncodingConfig{
			InterfaceRegistry: arkApp.InterfaceRegistry(),
			Codec:             arkApp.AppCodec(),
			TxConfig:          arkApp.GetTxConfig(),
			Amino:             arkApp.LegacyAmino(),
		},
	}
}
