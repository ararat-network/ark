//go:build sims

package app

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/store/v2"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sims "github.com/cosmos/cosmos-sdk/testutil/simsx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
	"github.com/cosmos/cosmos-sdk/x/simulation"
	simcli "github.com/cosmos/cosmos-sdk/x/simulation/client/cli"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/ararat-network/ark/app/ante"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

const AppChainID = "ark-simapp"

var FlagEnableStreamingValue bool

// Get flags every time the simulator is run
func init() {
	simcli.GetSimulatorFlags()
	flag.BoolVar(&FlagEnableStreamingValue, "EnableStreaming", false, "Enable streaming service")
}

// TestMain switches off the two consensus rules the simulation cannot satisfy,
// for every entry point in this file.
//
// The harness draws each transaction's fee uniformly from the sender's
// spendable balance, so it cannot be told what a transaction owes and
// eventually offers nothing at all; and it picks governance voters at random,
// so it cannot be told to stake them. Neither is a figure genesis can tune —
// the fee requirement ceils to at least one base unit under every valid
// parameter, and an unstaked voter is refused by construction. The Hub turns
// off its own fee market and vote floor here for the same reasons.
func TestMain(m *testing.M) {
	ante.SetBaseFeeGate(false)
	ante.SetMinVoterStake(math.LegacyZeroDec())

	telemetry.TestingMain(m, nil)
}

// interBlockCacheOpt returns a BaseApp option function that sets the persistent
// inter-block write-through cache.
func interBlockCacheOpt() func(*baseapp.BaseApp) {
	return baseapp.SetInterBlockCache(store.NewCommitKVStoreCacheManager())
}

func TestFullAppSimulation(t *testing.T) {
	sims.Run(t, NewArkApp, setupStateFactory)
}

func setupStateFactory(app *ArkApp) sims.SimStateFactory {
	return sims.SimStateFactory{
		Codec:         app.AppCodec(),
		AppStateFn:    simtestutil.AppStateFn(app.AppCodec(), app.SimulationManager(), app.DefaultGenesis()),
		BlockedAddr:   BlockedAddresses(),
		AccountSource: app.AccountKeeper,
		BalanceSource: app.BankKeeper,
	}
}

var (
	exportAllModules       = []string{}
	exportWithValidatorSet = []string{}
)

func TestAppImportExport(t *testing.T) {
	sims.Run(t, NewArkApp, setupStateFactory, func(tb testing.TB, ti sims.TestInstance[*ArkApp], accs []simtypes.Account) {
		tb.Helper()
		app := ti.App
		tb.Log("exporting genesis...\n")
		exported, err := app.ExportAppStateAndValidators(false, exportWithValidatorSet, exportAllModules)
		require.NoError(tb, err)

		tb.Log("importing genesis...\n")
		newTestInstance := sims.NewSimulationAppInstance(tb, ti.Cfg, NewArkApp)
		newApp := newTestInstance.App
		var genesisState map[string]json.RawMessage
		require.NoError(tb, json.Unmarshal(exported.AppState, &genesisState))
		ctxB := newApp.NewContextLegacy(true, cmtproto.Header{
			Height: app.LastBlockHeight(),
			Time:   exportedRateTime(tb, newApp.appCodec, genesisState),
		})
		_, err = newApp.ModuleManager.InitGenesis(ctxB, newApp.appCodec, genesisState)
		if IsEmptyValidatorSetErr(err) {
			tb.Skip("Skipping simulation as all validators have been unbonded")
			return
		}
		require.NoError(tb, err)
		// InitGenesis is only part of what InitChain does: the runtime's
		// InitChainer also stamps the module version map, and the store
		// comparison below covers it.
		require.NoError(tb, newApp.UpgradeKeeper.SetModuleVersionMap(ctxB, newApp.ModuleManager.GetVersionMap()))
		err = newApp.StoreConsensusParams(ctxB, exported.ConsensusParams)
		require.NoError(tb, err)

		tb.Log("comparing stores...")
		// skip certain prefixes
		skipPrefixes := map[string][][]byte{
			stakingtypes.StoreKey: {
				stakingtypes.UnbondingQueueKey, stakingtypes.RedelegationQueueKey, stakingtypes.ValidatorQueueKey,
				stakingtypes.HistoricalInfoKey, stakingtypes.UnbondingIDKey, stakingtypes.UnbondingIndexKey,
				stakingtypes.UnbondingTypeKey,
				stakingtypes.ValidatorUpdatesKey, // todo (Alex): double check why there is a diff with test-sim-import-export
			},
			authzkeeper.StoreKey:   {authzkeeper.GrantQueuePrefix},
			feegrant.StoreKey:      {feegrant.FeeAllowanceQueueKeyPrefix},
			slashingtypes.StoreKey: {slashingtypes.ValidatorMissedBlockBitmapKeyPrefix},
			// The per-block transaction counter CountTXDecorator stamps, which
			// contract address derivation reads. It is block scratch, not
			// genesis state: a chain that ran blocks holds the last one's
			// count, and a chain freshly imported from its export holds none.
			wasmtypes.StoreKey: {wasmtypes.TXCounterPrefix},
		}
		AssertEqualStores(tb, app, newApp, app.SimulationManager().StoreDecoders, skipPrefixes)
	})
}

// Scenario:
//
//	Start a fresh node and run n blocks, export state
//	set up a new node instance, Init chain from exported genesis
//	run new instance for n blocks
func TestAppSimulationAfterImport(t *testing.T) {
	sims.Run(t, NewArkApp, setupStateFactory, func(tb testing.TB, ti sims.TestInstance[*ArkApp], accs []simtypes.Account) {
		tb.Helper()
		app := ti.App
		tb.Log("exporting genesis...\n")
		exported, err := app.ExportAppStateAndValidators(false, exportWithValidatorSet, exportAllModules)
		require.NoError(tb, err)

		tb.Log("importing genesis...\n")
		newTestInstance := sims.NewSimulationAppInstance(tb, ti.Cfg, NewArkApp)
		newApp := newTestInstance.App
		var genesisState map[string]json.RawMessage
		require.NoError(tb, json.Unmarshal(exported.AppState, &genesisState))
		_, err = newApp.InitChain(&cmtabci.RequestInitChain{
			AppStateBytes: exported.AppState,
			ChainId:       sims.SimAppChainID,
			Time:          exportedRateTime(tb, newApp.appCodec, genesisState),
		})
		if IsEmptyValidatorSetErr(err) {
			tb.Skip("Skipping simulation as all validators have been unbonded")
			return
		}
		require.NoError(tb, err)
		newStateFactory := setupStateFactory(newApp)
		_, _, err = simulation.SimulateFromSeedX(
			tb,
			newTestInstance.AppLogger,
			sims.WriteToDebugLog(newTestInstance.AppLogger),
			newApp.BaseApp,
			newStateFactory.AppStateFn,
			simtypes.RandomAccounts,
			simtestutil.BuildSimulationOperations(newApp, newApp.AppCodec(), newTestInstance.Cfg, newApp.GetTxConfig()),
			newStateFactory.BlockedAddr,
			newTestInstance.Cfg,
			newStateFactory.Codec,
			ti.ExecLogWriter,
		)
		require.NoError(tb, err)
	})
}

func IsEmptyValidatorSetErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "validator set is empty after InitGenesis")
}

// exportedRateTime is the newest rate timestamp the exported state carries,
// which is the block the export was taken at as closely as the state records
// it.
//
// The SDK's import tests leave the genesis block time at its zero value, which
// no SDK module reads. Ark's Oracle does: it refuses a genesis rate stamped
// after the genesis block time, and every exported rate is stamped with the
// simulated time it was applied at. So the import has to be told when the
// state it is importing came from.
func exportedRateTime(tb testing.TB, cdc codec.Codec, genesisState map[string]json.RawMessage) time.Time {
	tb.Helper()

	raw, ok := genesisState[oracletypes.ModuleName]
	require.True(tb, ok, "exported state carries no oracle genesis")

	var oracleGenesis oracletypes.GenesisState
	cdc.MustUnmarshalJSON(raw, &oracleGenesis)

	latest := time.Unix(0, 0).UTC()
	for _, rate := range oracleGenesis.ExchangeRates {
		if rate.BlockTimestamp.After(latest) {
			latest = rate.BlockTimestamp
		}
	}
	return latest
}

func TestAppStateDeterminism(t *testing.T) {
	const numTimesToRunPerSeed = 3
	var seeds []int64
	if s := simcli.NewConfigFromFlags().Seed; s != simcli.DefaultSeedValue {
		// We will be overriding the random seed and just run a single simulation on the provided seed value
		for j := 0; j < numTimesToRunPerSeed; j++ { // multiple rounds
			seeds = append(seeds, s)
		}
	} else {
		// setup with 3 random seeds
		for i := 0; i < 3; i++ {
			seed := rand.Int63()
			for j := 0; j < numTimesToRunPerSeed; j++ { // multiple rounds
				seeds = append(seeds, seed)
			}
		}
	}
	// overwrite default app config
	interBlockCachingAppFactory := func(logger log.Logger, db dbm.DB, loadLatest bool, appOpts servertypes.AppOptions, baseAppOptions ...func(*baseapp.BaseApp)) *ArkApp {
		if FlagEnableStreamingValue {
			m := map[string]any{
				"streaming.abci.keys":             []string{"*"},
				"streaming.abci.plugin":           "abci_v1",
				"streaming.abci.stop-node-on-err": true,
			}
			others := appOpts
			appOpts = appOptionsFn(func(k string) any {
				if v, ok := m[k]; ok {
					return v
				}
				return others.Get(k)
			})
		}
		return NewArkApp(logger, db, true, appOpts, append(baseAppOptions, interBlockCacheOpt())...)
	}
	var mx sync.Mutex
	appHashResults := make(map[int64][][]byte)
	appSimLogger := make(map[int64][]simulation.LogWriter)
	captureAndCheckHash := func(tb testing.TB, ti sims.TestInstance[*ArkApp], _ []simtypes.Account) {
		tb.Helper()
		seed, appHash := ti.Cfg.Seed, ti.App.LastCommitID().Hash
		mx.Lock()
		otherHashes, execWriters := appHashResults[seed], appSimLogger[seed]
		if len(otherHashes) < numTimesToRunPerSeed-1 {
			appHashResults[seed], appSimLogger[seed] = append(otherHashes, appHash), append(execWriters, ti.ExecLogWriter)
		} else { // cleanup
			delete(appHashResults, seed)
			delete(appSimLogger, seed)
		}
		mx.Unlock()

		var failNow bool
		// and check that all app hashes per seed are equal for each iteration
		for i := 0; i < len(otherHashes); i++ {
			if !assert.Equal(tb, otherHashes[i], appHash) {
				execWriters[i].PrintLogs()
				failNow = true
			}
		}
		if failNow {
			ti.ExecLogWriter.PrintLogs()
			tb.Fatalf("non-determinism in seed %d", seed)
		}
	}
	// run simulations
	sims.RunWithSeeds(t, interBlockCachingAppFactory, setupStateFactory, seeds, []byte{}, captureAndCheckHash)
}

type ComparableStoreApp interface {
	LastBlockHeight() int64
	NewContextLegacy(isCheckTx bool, header cmtproto.Header) sdk.Context
	GetKey(storeKey string) *storetypes.KVStoreKey
	GetStoreKeys() []storetypes.StoreKey
}

func AssertEqualStores(
	tb testing.TB,
	app, newApp ComparableStoreApp,
	storeDecoders simtypes.StoreDecoderRegistry,
	skipPrefixes map[string][][]byte,
) {
	tb.Helper()
	ctxA := app.NewContextLegacy(true, cmtproto.Header{Height: app.LastBlockHeight()})
	ctxB := newApp.NewContextLegacy(true, cmtproto.Header{Height: app.LastBlockHeight()})

	storeKeys := app.GetStoreKeys()
	require.NotEmpty(tb, storeKeys)

	for _, appKeyA := range storeKeys {
		// only compare kvstores
		if _, ok := appKeyA.(*storetypes.KVStoreKey); !ok {
			continue
		}

		keyName := appKeyA.Name()
		appKeyB := newApp.GetKey(keyName)

		storeA := ctxA.KVStore(appKeyA)
		storeB := ctxB.KVStore(appKeyB)

		failedKVAs, failedKVBs := simtestutil.DiffKVStores(storeA, storeB, skipPrefixes[keyName])
		require.Equal(tb, len(failedKVAs), len(failedKVBs), "unequal sets of key-values to compare %s, key stores %s and %s", keyName, appKeyA, appKeyB)

		tb.Logf("compared %d different key/value pairs between %s and %s\n", len(failedKVAs), appKeyA, appKeyB)
		if !assert.Equal(tb, 0, len(failedKVAs), simtestutil.GetSimulationLog(keyName, storeDecoders, failedKVAs, failedKVBs)) {
			for _, v := range failedKVAs {
				tb.Logf("store mismatch: %q\n", v)
			}
			tb.FailNow()
		}
	}
}

// appOptionsFn is an adapter to the single method AppOptions interface
type appOptionsFn func(string) any

func (f appOptionsFn) Get(k string) any {
	return f(k)
}

// FauxMerkleModeOpt returns a BaseApp option to use a dbStoreAdapter instead of
// an IAVLStore for faster simulation speed.
func FauxMerkleModeOpt(bapp *baseapp.BaseApp) {
	bapp.SetFauxMerkleMode()
}

func FuzzFullAppSimulation(f *testing.F) {
	f.Fuzz(func(t *testing.T, rawSeed []byte) {
		if len(rawSeed) < 8 {
			t.Skip()
			return
		}
		sims.RunWithSeeds(
			t,
			NewArkApp,
			setupStateFactory,
			[]int64{int64(binary.BigEndian.Uint64(rawSeed))},
			rawSeed[8:],
		)
	})
}
