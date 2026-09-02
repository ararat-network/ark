package ante_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/ararat-network/ark/app"
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

// passThrough asserts the decorator reached its successor.
func passThrough(t *testing.T, reached *bool) sdk.AnteHandler {
	t.Helper()
	return func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		*reached = true
		return ctx, nil
	}
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
	require.NoError(t, arkApp.TreasuryKeeper.Params.Set(ctx, params))
	require.NoError(t, arkApp.TreasuryKeeper.BaseGasPrice.Set(ctx, params.MinBaseGasPrice))
	policy := treasurytypes.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	require.NoError(t, arkApp.TreasuryKeeper.MonetaryPolicy.Set(ctx, policy))
	require.NoError(t, arkApp.TreasuryKeeper.ConversionFactors.Set(ctx, chain.USDBaseDenom, treasurytypes.ConversionFactor{
		Denom:  chain.USDBaseDenom,
		Factor: math.LegacyOneDec(),
	}))

	// The payer holds twice the tax the fixture transaction owes.
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
