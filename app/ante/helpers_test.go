package ante_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/app/ante"
	chain "github.com/ararat-network/ark/pkg/chain"
	markettypes "github.com/ararat-network/ark/x/market/types"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

type treasuryFeeTx struct {
	msgs    []sdk.Msg
	fee     sdk.Coins
	gas     uint64
	payer   sdk.AccAddress
	granter sdk.AccAddress
}

func (tx treasuryFeeTx) GetMsgs() []sdk.Msg { return tx.msgs }

func (treasuryFeeTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }

func (tx treasuryFeeTx) GetGas() uint64 { return tx.gas }

func (tx treasuryFeeTx) GetFee() sdk.Coins { return tx.fee }

func (tx treasuryFeeTx) FeePayer() []byte { return tx.payer }

func (tx treasuryFeeTx) FeeGranter() []byte { return tx.granter }

// The two collectors the fee decorator pays.
var (
	feeCollector = authtypes.NewModuleAddress(authtypes.FeeCollectorName)
	taxCollector = authtypes.NewModuleAddress(treasurytypes.TransferTaxCollectorName)
)

// passThrough asserts the decorator reached its successor.
func passThrough(t *testing.T, reached *bool) sdk.AnteHandler {
	t.Helper()
	return func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		*reached = true
		return ctx, nil
	}
}

// feeDecorator is the production fee decorator over the app's keepers.
func feeDecorator(arkApp *app.ArkApp) ante.FeeDecorator {
	return ante.NewFeeDecorator(arkApp.AccountKeeper, arkApp.BankKeeper, arkApp.FeeGrantKeeper, arkApp.TreasuryKeeper)
}

// runFee runs the fee decorator over tx and returns the context it handed
// on, zero when it refused.
func runFee(t *testing.T, arkApp *app.ArkApp, ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
	t.Helper()
	var handed sdk.Context
	_, err := feeDecorator(arkApp).AnteHandle(ctx, tx, simulate, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		handed = ctx
		return ctx, nil
	})
	return handed, err
}

// feeRun is one run of the fee decorator on a cache of the test context: the
// context it handed on, the cache it wrote, and what reached each collector.
type feeRun struct {
	handed sdk.Context
	cached sdk.Context
	gas    string
	tax    string
	err    error
}

func runFeeOnCache(t *testing.T, arkApp *app.ArkApp, ctx sdk.Context, tx sdk.Tx, simulate bool) feeRun {
	t.Helper()
	cached, _ := ctx.CacheContext()
	handed, err := runFee(t, arkApp, cached, tx, simulate)
	return feeRun{
		handed: handed,
		cached: cached,
		gas:    collected(arkApp, ctx, cached, feeCollector).String(),
		tax:    collected(arkApp, ctx, cached, taxCollector).String(),
		err:    err,
	}
}

// usdBalance reads a balance in the fixture's taxed denomination.
func usdBalance(arkApp *app.ArkApp, ctx sdk.Context, addr sdk.AccAddress) math.Int {
	return arkApp.BankKeeper.GetBalance(ctx, addr, chain.USDBaseDenom).Amount
}

// collected is what addr holds on cached beyond what it holds on base: what
// a run on the cache moved to it.
func collected(arkApp *app.ArkApp, base, cached sdk.Context, addr sdk.AccAddress) sdk.Coins {
	return arkApp.BankKeeper.GetAllBalances(cached, addr).Sub(arkApp.BankKeeper.GetAllBalances(base, addr)...)
}

// txEvent returns the attributes of the tx event the fee decorator emitted
// on ctx.
func txEvent(t *testing.T, ctx sdk.Context) map[string]string {
	t.Helper()
	for _, event := range ctx.EventManager().Events() {
		if event.Type != sdk.EventTypeTx {
			continue
		}
		attributes := make(map[string]string, len(event.Attributes))
		for _, attribute := range event.Attributes {
			attributes[attribute.Key] = attribute.Value
		}
		return attributes
	}
	t.Fatal("no tx event")
	return nil
}

// fundAccount mints through the market module, the one module account with
// the permission, and hands the coins to addr.
func fundAccount(tb testing.TB, arkApp *app.ArkApp, ctx sdk.Context, addr sdk.AccAddress, coins sdk.Coins) {
	tb.Helper()
	require.NoError(tb, arkApp.BankKeeper.MintCoins(ctx, markettypes.ModuleName, coins))
	require.NoError(tb, arkApp.BankKeeper.SendCoinsFromModuleToAccount(
		ctx, markettypes.ModuleName, addr, coins,
	))
}

func setupTreasuryAnteTest(t *testing.T) (*app.ArkApp, sdk.Context, treasuryFeeTx) {
	t.Helper()
	arkApp := app.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{Height: 1})
	params, err := arkApp.TreasuryKeeper.Params.Get(ctx)
	require.NoError(t, err)
	params.ReferenceTaxCap = math.NewInt(100)
	// The launch floor is atto-scaled; the fixture prices gas at a tenth of
	// a base unit so fee arithmetic in tests reads in small integers.
	params.MinBaseGasPrice = math.LegacyMustNewDecFromStr("0.1")
	params.TransferTaxRate = math.LegacyMustNewDecFromStr("0.1")
	require.NoError(t, arkApp.TreasuryKeeper.Params.Set(ctx, params))
	require.NoError(t, arkApp.TreasuryKeeper.BaseGasPrice.Set(ctx, params.MinBaseGasPrice))
	require.NoError(t, arkApp.TreasuryKeeper.ConversionFactors.Set(ctx, chain.USDBaseDenom, treasurytypes.ConversionFactor{
		Denom:  chain.USDBaseDenom,
		Factor: math.LegacyOneDec(),
	}))

	// The payer holds twenty: the fifteen the fixture fee declares — the ten
	// the send owes in tax, and five for gas against a requirement of one
	// base unit at this gas limit, four of which the ceiling never charges —
	// and five to spare.
	payer := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	fundAccount(t, arkApp, ctx, payer, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 20)))

	msg := &banktypes.MsgSend{
		FromAddress: payer.String(),
		ToAddress:   sdk.AccAddress(bytes.Repeat([]byte{2}, 20)).String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)),
	}
	return arkApp, ctx, treasuryFeeTx{
		msgs:  []sdk.Msg{msg},
		fee:   sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 15)),
		gas:   1,
		payer: payer,
	}
}
