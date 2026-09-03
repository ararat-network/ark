package app

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/gogoproto/proto"
	icatypes "github.com/cosmos/ibc-go/v11/modules/apps/27-interchain-accounts/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"
	"github.com/stretchr/testify/require"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"cosmossdk.io/core/address"
	"cosmossdk.io/depinject"
	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil/mock"
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
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/ararat-network/ark/app/params"
	"github.com/ararat-network/ark/pkg/chain"
	markettypes "github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

func TestAppConstructs(t *testing.T) {
	db := dbm.NewMemDB()
	arkApp := NewArkApp(log.NewTestLogger(t), db, true, simtestutil.NewAppOptionsWithFlagHome(t.TempDir()))

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

// TestAppDoesNotMeterBlockGas pins the deliberate absence of
// baseapp.EnableBlockGasMeter: ten txs each declaring the entire MaxGas budget
// all execute, because the app applies no cumulative bound at DeliverTx. Comet's
// proposal-level max_gas gate and the per-tx meters are the only limits, and
// re-adding the meter would foreclose block-stm — see docs/BLOCK_EXECUTION.md.
func TestAppDoesNotMeterBlockGas(t *testing.T) {
	const chainID = "ark-block-gas-test"

	privVal := mock.NewPV()
	pubKey, err := privVal.GetPubKey()
	require.NoError(t, err)
	valSet := cmttypes.NewValidatorSet([]*cmttypes.Validator{cmttypes.NewValidator(pubKey, 1)})

	senderPrivKey := secp256k1.GenPrivKey()
	sender := authtypes.NewBaseAccount(senderPrivKey.PubKey().Address().Bytes(), senderPrivKey.PubKey(), 0, 0)
	balance := banktypes.Balance{
		Address: sender.GetAddress().String(),
		Coins: sdk.NewCoins(
			sdk.NewCoin(
				sdk.DefaultBondDenom,
				sdk.DefaultPowerReduction.MulRaw(1_000),
			),
			// The fee gate prices gas in the reference denom, at the
			// atto-scaled launch floor: 0.02 XDR per 200k-gas transaction,
			// ten transactions funded with headroom.
			sdk.NewInt64Coin(chain.XDRBaseDenom, 300_000_000_000_000_000),
		),
	}

	arkApp := NewArkApp(
		log.NewTestLogger(t),
		dbm.NewMemDB(),
		true,
		simtestutil.NewAppOptionsWithFlagHome(t.TempDir()),
		baseapp.SetChainID(chainID),
	)
	genesisState, err := simtestutil.GenesisStateWithValSet(
		arkApp.AppCodec(),
		arkApp.DefaultGenesis(),
		valSet,
		[]authtypes.GenesisAccount{sender},
		balance,
	)
	require.NoError(t, err)
	stateBytes, err := json.Marshal(genesisState)
	require.NoError(t, err)
	consensusParams := proto.Clone(simtestutil.DefaultConsensusParams).(*cmtproto.ConsensusParams)
	consensusParams.Block.MaxGas = 200_000
	_, err = arkApp.InitChain(&abci.RequestInitChain{
		ChainId:         chainID,
		ConsensusParams: consensusParams,
		AppStateBytes:   stateBytes,
	})
	require.NoError(t, err)

	recipient := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	txs := make([][]byte, 10)
	for i := range txs {
		msg := banktypes.NewMsgSend(
			sender.GetAddress(),
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
			senderPrivKey,
		)
		require.NoError(t, err)
		txs[i], err = arkApp.GetTxConfig().TxEncoder()(tx)
		require.NoError(t, err)
	}

	response, err := arkApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height:             1,
		NextValidatorsHash: valSet.Hash(),
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
	app := NewArkappWithCustomOptions(t, false, SetupOptions{
		Logger:  logger.With("instance", "first"),
		DB:      db,
		AppOpts: simtestutil.NewAppOptionsWithFlagHome(t.TempDir()),
	})

	// BlockedAddresses returns a map of addresses in app v1 and a map of modules name in app di.
	for acc := range BlockedAddresses() {
		var addr sdk.AccAddress
		if modAddr, err := sdk.AccAddressFromBech32(acc); err == nil {
			addr = modAddr
		} else {
			addr = app.AccountKeeper.GetModuleAddress(acc)
		}

		require.True(
			t,
			app.BankKeeper.BlockedAddr(addr),
			fmt.Sprintf("ensure that blocked addresses are properly set in bank keeper: %s should be blocked", acc),
		)
	}

	// finalise block so we have CheckTx state set
	_, err := app.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: 1,
	})
	require.NoError(t, err)

	_, err = app.Commit()
	require.NoError(t, err)

	// Making a new app object with the db, so that initchain hasn't been called
	app2 := NewArkApp(logger.With("instance", "second"), db, true, simtestutil.NewAppOptionsWithFlagHome(t.TempDir()))
	_, err = app2.ExportAppStateAndValidators(false, []string{}, []string{})
	require.NoError(t, err, "ExportAppStateAndValidators should not have an error")
}

func TestUpgradeStateOnGenesis(t *testing.T) {
	db := dbm.NewMemDB()
	app := NewArkappWithCustomOptions(t, false, SetupOptions{
		Logger:  log.NewTestLogger(t),
		DB:      db,
		AppOpts: simtestutil.NewAppOptionsWithFlagHome(t.TempDir()),
	})

	// make sure the upgrade keeper has version map in state
	ctx := app.NewContext(false)
	vm, err := app.UpgradeKeeper.GetModuleVersionMap(ctx)
	require.NoError(t, err)
	for v, i := range app.ModuleManager.Modules {
		if i, ok := i.(module.HasConsensusVersion); ok {
			require.Equal(t, vm[v], i.ConsensusVersion())
		}
	}

	require.NotNil(t, app.UpgradeKeeper.GetVersionSetter())
}

func TestProtoAnnotations(t *testing.T) {
	r, err := proto.MergedRegistry()
	require.NoError(t, err)
	err = msgservice.ValidateProtoAnnotations(r)
	require.NoError(t, err)
}

// TestAddressCodecsAgreeWithSDKConfig pins that the codecs depinject derives
// from the auth prefix encode every address class exactly as the sealed
// sdk.Config does. config.go seals the params prefixes; the runtime derives
// the validator and consensus prefixes from the auth one. The two are wired
// independently, so only this keeps them from drifting.
func TestAddressCodecsAgreeWithSDKConfig(t *testing.T) {
	var (
		addrCodec address.Codec
		valCodec  runtime.ValidatorAddressCodec
		consCodec runtime.ConsensusAddressCodec
	)
	require.NoError(t, depinject.Inject(
		depinject.Configs(AppConfig, depinject.Supply(log.NewNopLogger())),
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
	require.Equal(t, Name, version.Name)
	require.Equal(t, Name, sdk.KeyringServiceName())
	require.Equal(t, Name+"d", version.AppName)
}

func TestTreasuryAccountAndLifecycleWiring(t *testing.T) {
	permissions := GetMaccPerms()
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

	blocked := BlockedAddresses()
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

	arkApp := NewArkApp(
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
