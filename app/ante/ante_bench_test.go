package ante_test

import (
	"math/rand"
	"testing"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/app/ante"
	"github.com/ararat-network/ark/pkg/chain"
)

// benchFixture drives the production ante chain over real chain state with one
// funded, staked signing account, so every decorator does the work it does in
// a block.
type benchFixture struct {
	app     *app.ArkApp
	ctx     sdk.Context
	handler sdk.AnteHandler
	priv    cryptotypes.PrivKey
	addr    sdk.AccAddress
	accNum  uint64
}

func newBenchFixture(b *testing.B) *benchFixture {
	b.Helper()
	arkApp := app.Setup(b, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{Height: 1}).
		WithExecMode(sdk.ExecModeFinalize)

	// A nonzero rate and cap, so the tax path computes and moves coins rather
	// than early-outing.
	params, err := arkApp.TreasuryKeeper.Params.Get(ctx)
	require.NoError(b, err)
	params.ReferenceTaxCap = math.NewInt(1_000_000)
	require.NoError(b, arkApp.TreasuryKeeper.Params.Set(ctx, params))
	policy, err := arkApp.TreasuryKeeper.MonetaryPolicy.Get(ctx)
	require.NoError(b, err)
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	require.NoError(b, arkApp.TreasuryKeeper.MonetaryPolicy.Set(ctx, policy))

	priv := secp256k1.GenPrivKey()
	addr := sdk.AccAddress(priv.PubKey().Address())
	account := arkApp.AccountKeeper.NewAccountWithAddress(ctx, addr)
	arkApp.AccountKeeper.SetAccount(ctx, account)

	// Enough for one pass of fee plus tax; every iteration replays against the
	// same base state through a fresh cache.
	fundAccount(b, arkApp, ctx, addr, sdk.NewCoins(
		sdk.NewInt64Coin(chain.SDRBaseDenom, 1_000_000_000_000_000_000),
		sdk.NewInt64Coin(chain.USDBaseDenom, 1_000_000),
	))

	// Stake past the vote floor, so the vote benchmark measures an admitted
	// vote rather than a refusal.
	validators, err := arkApp.StakingKeeper.GetAllValidators(ctx)
	require.NoError(b, err)
	require.NotEmpty(b, validators)
	bond := chain.NativeBaseAmount(2)
	fundVoter(b, arkApp, ctx, addr, bond)
	_, err = arkApp.StakingKeeper.Delegate(ctx, addr, bond, stakingtypes.Unbonded, validators[0], true)
	require.NoError(b, err)

	handler := ante.NewAnteHandler(
		arkApp.AppCodec(),
		arkApp.GetTxConfig(),
		arkApp.AccountKeeper,
		arkApp.BankKeeper,
		arkApp.FeeGrantKeeper,
		arkApp.StakingKeeper,
		arkApp.TreasuryKeeper,
		arkApp.IBCKeeper,
		arkApp.WasmKeeper.GetGasRegister(),
		wasmtypes.DefaultNodeConfig(),
		runtime.NewKVStoreService(arkApp.GetKey(wasmtypes.StoreKey)),
	)

	return &benchFixture{
		app:     arkApp,
		ctx:     ctx,
		handler: handler,
		priv:    priv,
		addr:    addr,
		accNum:  account.GetAccountNumber(),
	}
}

// signedTx signs msgs with the fixture key at sequence zero, priced exactly at
// the consensus floor for the declared gas.
func (f *benchFixture) signedTx(b *testing.B, gas uint64, msgs ...sdk.Msg) sdk.Tx {
	b.Helper()
	params, err := f.app.TreasuryKeeper.Params.Get(f.ctx)
	require.NoError(b, err)
	price, err := f.app.TreasuryKeeper.BaseGasPrice.Get(f.ctx)
	require.NoError(b, err)
	required, _, err := f.app.TreasuryKeeper.GetRequiredGasFee(f.ctx, params, price, gas, params.ReferenceDenom)
	require.NoError(b, err)
	tx, err := simtestutil.GenSignedMockTx(
		rand.New(rand.NewSource(1)),
		f.app.GetTxConfig(),
		msgs,
		sdk.NewCoins(required),
		gas,
		f.ctx.ChainID(),
		[]uint64{f.accNum},
		[]uint64{0},
		f.priv,
	)
	require.NoError(b, err)
	return tx
}

func (f *benchFixture) run(b *testing.B, ctx sdk.Context, tx sdk.Tx) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		cacheCtx, _ := ctx.CacheContext()
		if _, err := f.handler(cacheCtx, tx, false); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAnteHandler runs the production decorator chain end to end over a
// representative transaction mix. Iterations branch a fresh cache off the same
// base state, so sequence checks and balance deductions replay identically.
func BenchmarkAnteHandler(b *testing.B) {
	f := newBenchFixture(b)

	send := &banktypes.MsgSend{
		FromAddress: f.addr.String(),
		ToAddress:   guardAddr(42).String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 1_000)),
	}
	sendTx := f.signedTx(b, 300_000, send)

	// The block-execution path: every decorator, signature verification
	// included.
	b.Run("bank_send", func(b *testing.B) {
		f.run(b, f.ctx, sendTx)
	})

	// The per-block mempool multiplier: ReCheckTx skips signature
	// verification, so this is the cost of everything that is not crypto.
	b.Run("bank_send_recheck", func(b *testing.B) {
		f.run(b, f.ctx.WithIsReCheckTx(true).WithExecMode(sdk.ExecModeReCheck), sendTx)
	})

	// An admitted vote: the stake walk on top of the send path, minus the tax
	// transfer.
	b.Run("gov_vote", func(b *testing.B) {
		f.run(b, f.ctx, f.signedTx(b, 300_000, vote(f.addr)))
	})

	// The fan-out guard and its quadratic surcharge over ten outputs.
	b.Run("multisend_10", func(b *testing.B) {
		f.run(b, f.ctx, f.signedTx(b, 500_000, multiSend(f.addr, 10)))
	})
}
