package app

import (
	"bytes"
	"testing"

	protov2 "google.golang.org/protobuf/proto"

	"cosmossdk.io/math"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	chain "ark/pkg/chain"
	markettypes "ark/x/market/types"
	treasurytypes "ark/x/treasury/types"
)

type treasuryFeeTx struct {
	msgs []sdk.Msg
	fee  sdk.Coins
	gas  uint64
}

func (tx treasuryFeeTx) GetMsgs() []sdk.Msg { return tx.msgs }

func (treasuryFeeTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }

func (tx treasuryFeeTx) GetGas() uint64 { return tx.gas }

func (tx treasuryFeeTx) GetFee() sdk.Coins { return tx.fee }

func (treasuryFeeTx) FeePayer() []byte { return nil }

func (treasuryFeeTx) FeeGranter() []byte { return nil }

func TestTreasuryFeeCheckerSeparatesTaxFromGasFee(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)

	checkCtx := ctx.WithIsCheckTx(true).WithMinGasPrices(sdk.NewDecCoins(
		sdk.NewDecCoin(chain.MicroSDRDenom, math.NewInt(5)),
	))
	fee, priority, err := arkApp.treasuryFeeChecker(checkCtx, tx)
	require.NoError(t, err)
	require.Equal(t, tx.fee, fee)
	require.Equal(t, int64(5), priority)

	tx.fee = sdk.NewCoins(sdk.NewInt64Coin(chain.MicroSDRDenom, 14))
	_, _, err = arkApp.treasuryFeeChecker(checkCtx, tx)
	require.ErrorContains(t, err, "insufficient gas fees")

	tx.fee = sdk.NewCoins(sdk.NewInt64Coin(chain.MicroSDRDenom, 9))
	_, _, err = arkApp.treasuryFeeChecker(checkCtx, tx)
	require.ErrorContains(t, err, "cover stability tax")
}

func TestTreasuryFeeCheckerOnlyWaivesTaxBeforeTreasuryGenesis(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	ctx = ctx.WithBlockHeight(0)
	tx.fee = sdk.NewCoins(sdk.NewInt64Coin(chain.MicroSDRDenom, 9))

	_, _, err := arkApp.treasuryFeeChecker(ctx, tx)
	require.ErrorContains(t, err, "cover stability tax")

	require.NoError(t, arkApp.TreasuryKeeper.MonetaryPolicy.Remove(ctx))
	fee, _, err := arkApp.treasuryFeeChecker(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, tx.fee, fee)
}

func TestRouteStabilityTaxCollectsExactTax(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	feeCollector := authtypes.NewModuleAddress(authtypes.FeeCollectorName)
	collector := authtypes.NewModuleAddress(treasurytypes.StabilityTaxCollectorName)

	require.NoError(t, arkApp.BankKeeper.MintCoins(
		ctx,
		markettypes.ModuleName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.MicroSDRDenom, 20)),
	))
	require.NoError(t, arkApp.BankKeeper.SendCoinsFromModuleToModule(
		ctx,
		markettypes.ModuleName,
		authtypes.FeeCollectorName,
		sdk.NewCoins(sdk.NewInt64Coin(chain.MicroSDRDenom, 20)),
	))

	router := arkApp.routeStabilityTax(func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	_, err := router(ctx, tx, false)
	require.NoError(t, err)
	require.Equal(
		t,
		math.NewInt(10),
		arkApp.BankKeeper.GetBalance(ctx, feeCollector, chain.MicroSDRDenom).Amount,
	)
	require.Equal(
		t,
		math.NewInt(10),
		arkApp.BankKeeper.GetBalance(ctx, collector, chain.MicroSDRDenom).Amount,
	)
	_, err = router(ctx, tx, true)
	require.NoError(t, err)
	require.Equal(
		t,
		math.NewInt(10),
		arkApp.BankKeeper.GetBalance(ctx, collector, chain.MicroSDRDenom).Amount,
	)
}

func TestTreasuryAnteFailsClosedWhenConfiguredTaxCapIsMissing(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)
	require.NoError(t, arkApp.TreasuryKeeper.TaxCaps.Remove(ctx, chain.MicroSDRDenom))
	tx.msgs = []sdk.Msg{&banktypes.MsgSend{
		FromAddress: sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String(),
		ToAddress:   sdk.AccAddress(bytes.Repeat([]byte{2}, 20)).String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.MicroSDRDenom, 100)),
	}}

	_, _, err := arkApp.treasuryFeeChecker(ctx, tx)
	require.ErrorIs(t, err, treasurytypes.ErrTaxCapUnavailable)

	router := arkApp.routeStabilityTax(func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	_, err = router(ctx, tx, false)
	require.ErrorIs(t, err, treasurytypes.ErrTaxCapUnavailable)
}

func setupTreasuryAnteTest(t *testing.T) (*ArkApp, sdk.Context, treasuryFeeTx) {
	t.Helper()
	arkApp := Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{Height: 1})
	params, err := arkApp.TreasuryKeeper.Params.Get(ctx)
	require.NoError(t, err)
	params.ReferenceTaxCap.Amount = math.NewInt(100)
	require.NoError(t, arkApp.TreasuryKeeper.Params.Set(ctx, params))
	policy := treasurytypes.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	require.NoError(t, arkApp.TreasuryKeeper.MonetaryPolicy.Set(ctx, policy))
	require.NoError(t, arkApp.TreasuryKeeper.TaxCaps.Set(ctx, chain.MicroSDRDenom, math.NewInt(100)))

	msg := &banktypes.MsgSend{
		FromAddress: sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String(),
		ToAddress:   sdk.AccAddress(bytes.Repeat([]byte{2}, 20)).String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.MicroSDRDenom, 100)),
	}
	return arkApp, ctx, treasuryFeeTx{
		msgs: []sdk.Msg{msg},
		fee:  sdk.NewCoins(sdk.NewInt64Coin(chain.MicroSDRDenom, 15)),
		gas:  1,
	}
}
