// SPDX-License-Identifier: Apache-2.0
// Adapted from Cosmos SDK, simapp/app_test.go and simapp/testutil_network_test.go.
// Modified for Ark: application fixtures and chain-specific assertions.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package app_test

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"
	"time"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/gogoproto/proto"
	icatypes "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/core/address"
	"cosmossdk.io/depinject"
	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil/network"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	"github.com/cosmos/cosmos-sdk/version"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/app/params"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
	claimstypes "github.com/ararat-network/ark/x/claims/types"
	markettypes "github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

func TestAppConstructs(t *testing.T) {
	db := dbm.NewMemDB()
	arkApp := app.NewArkApp(log.NewTestLogger(t), db, true, simtestutil.NewAppOptionsWithFlagHome(t.TempDir()))

	require.NotNil(t, arkApp)
	require.NotNil(t, arkApp.BaseApp)
	require.NotNil(t, arkApp.LegacyAmino())
	require.NotNil(t, arkApp.AppCodec())
	require.NotNil(t, arkApp.GetTxConfig())
	require.NotNil(t, arkApp.InterfaceRegistry())
	require.NotNil(t, arkApp.MsgServiceRouter())
	require.NotNil(t, arkApp.GRPCQueryRouter())

	require.NotNil(t, arkApp.AccountKeeper)
	require.NotNil(t, arkApp.BankKeeper)
	require.NotNil(t, arkApp.StakingKeeper)
	require.NotNil(t, arkApp.GovKeeper)
	require.NotNil(t, arkApp.UpgradeKeeper)
	require.NotNil(t, arkApp.MarketKeeper)
	require.NotNil(t, arkApp.TreasuryKeeper)
	require.NotNil(t, arkApp.OracleKeeper)
	require.NotContains(t, arkApp.ModuleManager.Modules, "mint")
	require.NotContains(t, arkApp.DefaultGenesis(), "mint")
	requireOrderBefore(
		t,
		arkApp.ModuleManager.OrderInitGenesis,
		oracletypes.ModuleName,
		markettypes.ModuleName,
	)
}

// TestAppDoesNotMeterBlockGas checks the absence of a cumulative execution gas meter. SDK proposal
// handlers enforce max_gas and per-transaction meters bound execution; see README.md, "Block
// gas-meter policy".
func TestAppDoesNotMeterBlockGas(t *testing.T) {
	const chainID = "ark-block-gas-test"

	validators := apptestutil.NewValidators(t, 1)
	funder := apptestutil.NewFunder(t, sdk.NewCoins(
		sdk.NewCoin(
			sdk.DefaultBondDenom,
			sdk.DefaultPowerReduction.MulRaw(1_000),
		),
		// The fee gate prices gas in the reference denom, at the atto-scaled
		// launch floor: 0.02 XDR per 200k-gas transaction, ten transactions
		// funded with headroom.
		sdk.NewInt64Coin(chain.XDRBaseDenom, 300_000_000_000_000_000),
	))

	arkApp := app.NewArkApp(
		log.NewTestLogger(t),
		dbm.NewMemDB(),
		true,
		simtestutil.NewAppOptionsWithFlagHome(t.TempDir()),
		baseapp.SetChainID(chainID),
	)
	genesisState, err := simtestutil.GenesisStateWithValSet(
		arkApp.AppCodec(),
		arkApp.DefaultGenesis(),
		validators.Set,
		funder.Accounts(),
		funder.Balance,
	)
	require.NoError(t, err)
	stateBytes, err := json.Marshal(genesisState)
	require.NoError(t, err)
	consensusParams := proto.Clone(simtestutil.DefaultConsensusParams).(*cmtproto.ConsensusParams)
	consensusParams.Block.MaxGas = 200_000
	_, err = arkApp.InitChain(&cmtabci.RequestInitChain{
		ChainId:         chainID,
		ConsensusParams: consensusParams,
		AppStateBytes:   stateBytes,
	})
	require.NoError(t, err)

	recipient := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	txs := make([][]byte, 10)
	for i := range txs {
		msg := banktypes.NewMsgSend(
			funder.Address(),
			recipient,
			sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 1)),
		)
		tx, err := simtestutil.GenSignedMockTx(
			rand.New(rand.NewSource(int64(i+1))),
			arkApp.GetTxConfig(),
			[]sdk.Msg{msg},
			sdk.NewCoins(sdk.NewInt64Coin(chain.XDRBaseDenom, 20_000_000_000_000_000)),
			200_000,
			chainID,
			[]uint64{0},
			[]uint64{uint64(i)},
			funder.Key,
		)
		require.NoError(t, err)
		txs[i], err = arkApp.GetTxConfig().TxEncoder()(tx)
		require.NoError(t, err)
	}

	response, err := arkApp.FinalizeBlock(&cmtabci.RequestFinalizeBlock{
		Height:             1,
		NextValidatorsHash: validators.Set.Hash(),
		Txs:                txs,
	})
	require.NoError(t, err)
	require.Len(t, response.TxResults, len(txs))
	for i, result := range response.TxResults {
		require.Zero(t, result.Code, "tx %d: %s", i, result.Log)
		require.NotContains(t, result.Log, "block gas meter", "tx %d", i)
	}
}

func TestArkAppExportAndBlockedAddrs(t *testing.T) {
	db := dbm.NewMemDB()
	logger := log.NewTestLogger(t)
	arkApp := apptestutil.NewArkappWithCustomOptions(t, false, apptestutil.SetupOptions{
		Logger:  logger.With("instance", "first"),
		DB:      db,
		AppOpts: simtestutil.NewAppOptionsWithFlagHome(t.TempDir()),
	})

	// app.BlockedAddresses returns a map of addresses in app v1 and a map of modules name in app di.
	for acc := range app.BlockedAddresses() {
		var addr sdk.AccAddress
		if modAddr, err := sdk.AccAddressFromBech32(acc); err == nil {
			addr = modAddr
		} else {
			addr = arkApp.AccountKeeper.GetModuleAddress(acc)
		}

		require.True(
			t,
			arkApp.BankKeeper.BlockedAddr(addr),
			fmt.Sprintf("ensure that blocked addresses are properly set in bank keeper: %s should be blocked", acc),
		)
	}

	// finalise block so we have CheckTx state set
	_, err := arkApp.FinalizeBlock(&cmtabci.RequestFinalizeBlock{
		Height: 1,
	})
	require.NoError(t, err)

	_, err = arkApp.Commit()
	require.NoError(t, err)

	// Making a new app object with the db, so that initchain hasn't been called
	app2 := app.NewArkApp(logger.With("instance", "second"), db, true, simtestutil.NewAppOptionsWithFlagHome(t.TempDir()))
	_, err = app2.ExportAppStateAndValidators(false, []string{}, []string{})
	require.NoError(t, err, "ExportAppStateAndValidators should not have an error")
}

func TestUpgradeStateOnGenesis(t *testing.T) {
	db := dbm.NewMemDB()
	arkApp := apptestutil.NewArkappWithCustomOptions(t, false, apptestutil.SetupOptions{
		Logger:  log.NewTestLogger(t),
		DB:      db,
		AppOpts: simtestutil.NewAppOptionsWithFlagHome(t.TempDir()),
	})

	// make sure the upgrade keeper has version map in state
	ctx := arkApp.NewContext(false)
	vm, err := arkApp.UpgradeKeeper.GetModuleVersionMap(ctx)
	require.NoError(t, err)
	for v, i := range arkApp.ModuleManager.Modules {
		if i, ok := i.(module.HasConsensusVersion); ok {
			require.Equal(t, vm[v], i.ConsensusVersion())
		}
	}

	require.NotNil(t, arkApp.UpgradeKeeper.GetVersionSetter())
}

func TestProtoAnnotations(t *testing.T) {
	r, err := proto.MergedRegistry()
	require.NoError(t, err)
	err = msgservice.ValidateProtoAnnotations(r)
	require.NoError(t, err)
}

// TestAddressCodecsAgreeWithSDKConfig checks that depinject codecs and sealed SDK configuration
// encode every account, validator, and consensus address class identically.
func TestAddressCodecsAgreeWithSDKConfig(t *testing.T) {
	var (
		addrCodec address.Codec
		valCodec  runtime.ValidatorAddressCodec
		consCodec runtime.ConsensusAddressCodec
	)
	require.NoError(t, depinject.Inject(
		depinject.Configs(app.AppConfig, depinject.Supply(log.NewNopLogger(), unwiredWasmKeeper{})),
		&addrCodec, &valCodec, &consCodec,
	))

	bz := make([]byte, 20)
	for i := range bz {
		bz[i] = byte(i)
	}
	for _, tc := range []struct {
		name  string
		codec address.Codec
		want  string
		hrp   string
	}{
		{"account", addrCodec, sdk.AccAddress(bz).String(), params.Bech32PrefixAccAddr},
		{"validator", valCodec, sdk.ValAddress(bz).String(), params.Bech32PrefixValAddr},
		{"consensus", consCodec, sdk.ConsAddress(bz).String(), params.Bech32PrefixConsAddr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.codec.BytesToString(bz)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			require.True(t, strings.HasPrefix(got, tc.hrp+"1"), got)
		})
	}
}

func TestNativeUnitConfiguration(t *testing.T) {
	require.Equal(t, chain.NoahBaseDenom, sdk.DefaultBondDenom)
	require.True(t, chain.NativeBaseAmount(1).Equal(sdk.DefaultPowerReduction))
	require.Equal(t, "0.100000000000000000", govv1.DefaultParams().MinInitialDepositRatio)
}

// The keyring service name is the OS credential store's service label on the
// default backend, so an unset version.Name would silently file Ark keys under
// "cosmos" and a later rename would leave them unreachable.
func TestKeyringServiceName(t *testing.T) {
	require.Equal(t, app.Name, version.Name)
	require.Equal(t, app.Name, sdk.KeyringServiceName())
	require.Equal(t, app.Name+"d", version.AppName)
}

func TestTreasuryAccountAndLifecycleWiring(t *testing.T) {
	permissions := app.GetMaccPerms()
	fundAddresses := make(map[string]struct{}, len(treasurytypes.FundAccountNames()))
	for _, moduleName := range treasurytypes.FundAccountNames() {
		perms, ok := permissions[moduleName]
		require.True(t, ok, "missing Treasury fund account %s", moduleName)
		require.Empty(t, perms, "Treasury fund account %s must have no permissions", moduleName)
		address := authtypes.NewModuleAddress(moduleName).String()
		_, duplicate := fundAddresses[address]
		require.False(t, duplicate, "Treasury fund accounts must be distinct")
		fundAddresses[address] = struct{}{}
	}
	_, hasTreasuryAccount := permissions[treasurytypes.ModuleName]
	require.False(t, hasTreasuryAccount, "Treasury module identity must not be a custody account")
	collectorPermissions, hasCollector := permissions[treasurytypes.TransferTaxCollectorName]
	require.True(t, hasCollector, "missing Oracle tax collector account")
	require.Empty(t, collectorPermissions, "Oracle tax collector must have no permissions")
	var minters []string
	for moduleName, perms := range permissions {
		if slices.Contains(perms, authtypes.Minter) {
			minters = append(minters, moduleName)
		}
	}
	require.ElementsMatch(
		t,
		[]string{markettypes.ModuleName, ibctransfertypes.ModuleName},
		minters,
		"Only Market and IBC transfer may mint",
	)
	require.ElementsMatch(
		t,
		[]string{authtypes.Minter, authtypes.Burner},
		permissions[markettypes.ModuleName],
		"Market must retain conversion mint and burn permissions",
	)
	require.ElementsMatch(
		t,
		[]string{authtypes.Minter, authtypes.Burner},
		permissions[ibctransfertypes.ModuleName],
		"IBC transfer must retain voucher mint and burn permissions",
	)

	blocked := app.BlockedAddresses()
	for _, moduleName := range append([]string{govtypes.ModuleName}, treasurytypes.FundAccountNames()...) {
		require.False(t, blocked[moduleName], "%s must remain reachable", moduleName)
	}
	for _, moduleName := range []string{
		authtypes.FeeCollectorName,
		distrtypes.ModuleName,
		stakingtypes.BondedPoolName,
		stakingtypes.NotBondedPoolName,
		markettypes.ModuleName,
		ibctransfertypes.ModuleName,
		icatypes.ModuleName,
		treasurytypes.TransferTaxCollectorName,
		oracletypes.ModuleName,
	} {
		require.True(t, blocked[moduleName], "%s must remain blocked", moduleName)
	}

	arkApp := app.NewArkApp(
		log.NewTestLogger(t),
		dbm.NewMemDB(),
		true,
		simtestutil.NewAppOptionsWithFlagHome(t.TempDir()),
	)
	requireOrderBefore(
		t,
		arkApp.ModuleManager.OrderInitGenesis,
		banktypes.ModuleName,
		treasurytypes.ModuleName,
	)
	// Treasury follows gov so a fee-param change enacted this block applies at
	// the same settlement. Market's must-lead slot is pinned in module_order_test.
	requireOrderBefore(
		t,
		arkApp.ModuleManager.OrderEndBlockers,
		govtypes.ModuleName,
		treasurytypes.ModuleName,
	)
}

// Module ordering is app wiring rather than anything the keepers can enforce:
// a reordering compiles, passes every module's tests, and changes the
// economics on every node at once. These tests pin the orders that matter.

func requireOrderBefore(t *testing.T, order []string, first, second string) {
	t.Helper()
	firstIndex := slices.Index(order, first)
	secondIndex := slices.Index(order, second)
	require.NotEqual(t, -1, firstIndex, "%s missing from lifecycle order", first)
	require.NotEqual(t, -1, secondIndex, "%s missing from lifecycle order", second)
	require.Less(t, firstIndex, secondIndex, "%s must run before %s", first, second)
}

// TestMarketSettlesBeforeEveryOtherEndBlocker checks that conversion settlement precedes changes to
// the lifecycle, supply, and fund balances its valuation reads.
func TestMarketSettlesBeforeEveryOtherEndBlocker(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	order := arkApp.ModuleManager.OrderEndBlockers

	marketAt := slices.Index(order, markettypes.ModuleName)
	require.GreaterOrEqual(t, marketAt, 0, "market must have an EndBlocker")
	require.Equal(t, 0, marketAt, "market must settle before every other EndBlocker: %v", order)

	// Named individually so a failure says which actor would have moved first.
	for _, later := range []string{
		govtypes.ModuleName,
		claimstypes.ModuleName,
		banktypes.ModuleName,
	} {
		at := slices.Index(order, later)
		if at < 0 {
			continue
		}
		require.Greater(t, at, marketAt, "%s must run after conversion settlement", later)
	}
}

// TestDistributionLeadsSlashingInBeginBlockers checks the SDK requirement that distribution empties
// the validator fee pool before slashing.
func TestDistributionLeadsSlashingInBeginBlockers(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	requireOrderBefore(
		t,
		arkApp.ModuleManager.OrderBeginBlockers,
		distrtypes.ModuleName,
		slashingtypes.ModuleName,
	)
}

// TestOracleJailsBeforeStakingEmitsValidatorUpdates checks that attendance jails affect this
// block's validator update. Governance precedes Oracle so parameter changes apply at the same
// settlement.
func TestOracleJailsBeforeStakingEmitsValidatorUpdates(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	order := arkApp.ModuleManager.OrderEndBlockers

	requireOrderBefore(t, order, govtypes.ModuleName, oracletypes.ModuleName)
	requireOrderBefore(t, order, oracletypes.ModuleName, stakingtypes.ModuleName)
}

// The contract runtime's cache counters register on whatever registry start
// hands over, and on nothing when none is handed over.
func TestWasmVMCacheMetricsFollowTheHandedRegistry(t *testing.T) {
	registry := prometheus.NewRegistry()
	appOpts := simtestutil.NewAppOptionsWithFlagHome(t.TempDir()).(simtestutil.AppOptionsMap)
	appOpts[app.WasmVMCacheMetricsRegistererOpt] = registry

	arkApp := app.NewArkApp(log.NewNopLogger(), dbm.NewMemDB(), true, appOpts)
	t.Cleanup(func() { require.NoError(t, arkApp.Close()) })

	families, err := registry.Gather()
	require.NoError(t, err)
	names := make([]string, 0, len(families))
	for _, family := range families {
		names = append(names, family.GetName())
	}
	require.Contains(t, names, "wasmvm_cache_misses_total")
	require.Contains(t, names, "wasmvm_cache_size_bytes")
}

type IntegrationTestSuite struct {
	suite.Suite

	network *network.Network
}

func (s *IntegrationTestSuite) SetupSuite() {
	s.T().Log("setting up integration test suite")

	cfg := network.DefaultConfig(apptestutil.NewTestNetworkFixture)
	// The default two-second commit timeout is what this suite costs; nothing
	// it asserts is a function of wall-clock block time.
	cfg.TimeoutCommit = 200 * time.Millisecond
	// One validator, so no peers: CometBFT's per-peer consensus goroutines can
	// outlive a stopped node and read its closed block store, panicking the
	// package. The E2E suites cover multi-validator consensus.
	cfg.NumValidators = 1

	var err error
	s.network, err = network.New(s.T(), s.T().TempDir(), cfg)
	s.Require().NoError(err)

	h, err := s.network.WaitForHeight(1)
	s.Require().NoError(err, "failed to reach height 1; got %d", h)
}

func (s *IntegrationTestSuite) TearDownSuite() {
	s.T().Log("tearing down integration test suite")
	s.network.Cleanup()
}

func (s *IntegrationTestSuite) TestNetwork_Liveness() {
	h, err := s.network.WaitForHeightWithTimeout(10, time.Minute)
	s.Require().NoError(err, "expected to reach 10 blocks; got %d", h)
}

func TestIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(IntegrationTestSuite))
}
