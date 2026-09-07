package ante_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/app/ante"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
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

// taxDecorator is the production transfer tax decorator over the app's
// keepers.
func taxDecorator(arkApp *app.ArkApp) ante.TransferTaxDecorator {
	return ante.NewTransferTaxDecorator(arkApp.AccountKeeper, arkApp.BankKeeper, arkApp.FeeGrantKeeper)
}

// runAnteFee runs the fee decorator alone over tx and returns the context it
// handed on, zero when it refused.
func runAnteFee(t *testing.T, arkApp *app.ArkApp, ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
	t.Helper()
	var handed sdk.Context
	_, err := feeDecorator(arkApp).AnteHandle(ctx, tx, simulate, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		handed = ctx
		return ctx, nil
	})
	return handed, err
}

// runTax runs the transfer tax decorator over tx as BaseApp runs it after
// the messages, told whether they succeeded, and returns the context it
// handed on, zero when it refused.
func runTax(t *testing.T, arkApp *app.ArkApp, ctx sdk.Context, tx sdk.Tx, simulate, success bool) (sdk.Context, error) {
	t.Helper()
	var handed sdk.Context
	_, err := taxDecorator(arkApp).PostHandle(ctx, tx, simulate, success, func(ctx sdk.Context, _ sdk.Tx, _, _ bool) (sdk.Context, error) {
		handed = ctx
		return ctx, nil
	})
	return handed, err
}

// runFee runs the pair BaseApp runs around a transaction whose messages
// succeed: the fee decorator, then, once it has admitted the transaction,
// the transfer tax decorator on the context it handed on. It returns that
// context, zero when the ante refused.
func runFee(t *testing.T, arkApp *app.ArkApp, ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
	t.Helper()
	handed, err := runAnteFee(t, arkApp, ctx, tx, simulate)
	if err != nil {
		return handed, err
	}
	if _, err := runTax(t, arkApp, handed, tx, simulate, true); err != nil {
		return handed, err
	}
	return handed, nil
}

// feeRun is one run of the fee mechanism on a cache of the test context:
// the context the ante handed on, the cache the run wrote, and what reached
// each collector.
type feeRun struct {
	handed sdk.Context
	cached sdk.Context
	gas    string
	tax    string
	err    error
}

// runFeeOnCache runs the ante and post pair on a cache.
func runFeeOnCache(t *testing.T, arkApp *app.ArkApp, ctx sdk.Context, tx sdk.Tx, simulate bool) feeRun {
	t.Helper()
	return runOnCache(t, arkApp, ctx, tx, simulate, runFee)
}

// runAnteOnCache runs the fee decorator alone on a cache, in execution mode,
// for tests that drive the post decorator by hand afterwards.
func runAnteOnCache(t *testing.T, arkApp *app.ArkApp, ctx sdk.Context, tx sdk.Tx) feeRun {
	t.Helper()
	return runOnCache(t, arkApp, ctx, tx, false, runAnteFee)
}

func runOnCache(
	t *testing.T, arkApp *app.ArkApp, ctx sdk.Context, tx sdk.Tx, simulate bool,
	run func(*testing.T, *app.ArkApp, sdk.Context, sdk.Tx, bool) (sdk.Context, error),
) feeRun {
	t.Helper()
	cached, _ := ctx.CacheContext()
	handed, err := run(t, arkApp, cached, tx, simulate)
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

// txEvents returns the attributes of every tx event on ctx, in order: the
// fee decorator's, then the transfer tax decorator's when it charged.
func txEvents(ctx sdk.Context) []map[string]string {
	var events []map[string]string
	for _, event := range ctx.EventManager().Events() {
		if event.Type != sdk.EventTypeTx {
			continue
		}
		attributes := make(map[string]string, len(event.Attributes))
		for _, attribute := range event.Attributes {
			attributes[attribute.Key] = attribute.Value
		}
		events = append(events, attributes)
	}
	return events
}

// txEvent returns the attributes of the tx events on ctx, merged: the fee
// and tip the ante named, and the tax the post named.
func txEvent(t *testing.T, ctx sdk.Context) map[string]string {
	t.Helper()
	events := txEvents(ctx)
	if len(events) == 0 {
		t.Fatal("no tx event")
	}
	merged := make(map[string]string)
	for _, event := range events {
		for key, value := range event {
			merged[key] = value
		}
	}
	return merged
}

func setupTreasuryAnteTest(t *testing.T) (*app.ArkApp, sdk.Context, treasuryFeeTx) {
	t.Helper()
	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{Height: 1}).WithExecMode(sdk.ExecModeFinalize)
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
	apptestutil.FundAccount(t, arkApp, ctx, payer, sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 20)))

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

// gasOf runs a step under a fresh meter and returns what it consumed.
func gasOf(t *testing.T, ctx sdk.Context, run func(sdk.Context) error) storetypes.Gas {
	t.Helper()
	metered := ctx.WithGasMeter(storetypes.NewGasMeter(10_000_000))
	require.NoError(t, run(metered))
	return metered.GasMeter().GasConsumed()
}
