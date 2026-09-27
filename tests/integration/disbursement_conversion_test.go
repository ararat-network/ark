package integration

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
	disbursementkeeper "github.com/ararat-network/ark/x/disbursement/keeper"
	disbursementtypes "github.com/ararat-network/ark/x/disbursement/types"
	markettypes "github.com/ararat-network/ark/x/market/types"
)

// TestDisbursementConvertsThroughMarket runs authorised conversion orders through real blocks:
// Market escrows and settles disbursement's NOAH like any trader's, the stablecoin lands in
// custody for compensation, and an order's spread cap refuses a fill it did not authorise.
func TestDisbursementConvertsThroughMarket(t *testing.T) {
	f := newActivationFixture(t)
	msgServer := disbursementkeeper.NewMsgServerImpl(f.app.DisbursementKeeper)
	queryServer := disbursementkeeper.NewQueryServerImpl(f.app.DisbursementKeeper)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	custody := authtypes.NewModuleAddress(disbursementtypes.ModuleName)
	escrow := authtypes.NewModuleAddress(markettypes.ModuleName)
	// Small against the fixture pool's depth, so the fill pays the 2% floor rather than slippage.
	amount := chain.NativeBaseAmount(10)

	f.nextBlock(func(ctx sdk.Context) {
		params, err := f.app.DisbursementKeeper.Params.Get(ctx)
		require.NoError(t, err)
		params.CompensationDenoms = []string{chain.KRWBaseDenom, chain.NoahBaseDenom, chain.USDBaseDenom}
		_, err = msgServer.UpdateParams(ctx, &disbursementtypes.MsgUpdateParams{Authority: authority, Params: params})
		require.NoError(t, err)
		// Stands in for genesis funding: only genesis grows a pool.
		apptestutil.FundModule(t, f.app, ctx, disbursementtypes.ModuleName, sdk.NewCoins(chain.NoahCoin(amount.MulRaw(2))))
		require.NoError(t, f.app.DisbursementKeeper.ContributorPool.Set(ctx, disbursementtypes.PoolBalance{Unallocated: amount.MulRaw(2), Open: amount.MulRaw(2)}))
		// The KRW cap sits below Market's 2% floor, so no fill can satisfy it.
		for denom, maxSpread := range map[string]string{chain.USDBaseDenom: "0.03", chain.KRWBaseDenom: "0.01"} {
			_, err := msgServer.AuthoriseConversion(ctx, &disbursementtypes.MsgAuthoriseConversion{
				Authority: authority, Denom: denom, Amount: amount, MaxSpread: math.LegacyMustNewDecFromStr(maxSpread),
			})
			require.NoError(t, err)
		}
	})

	var output sdk.Coin
	f.nextBlock(func(ctx sdk.Context) {
		refused, _ := ctx.CacheContext()
		_, err := msgServer.Convert(refused, &disbursementtypes.MsgConvert{Sender: f.trader.String(), Denom: chain.KRWBaseDenom, Amount: amount})
		require.ErrorContains(t, err, "exceeds the order's cap")

		res, err := msgServer.Convert(ctx, &disbursementtypes.MsgConvert{Sender: f.trader.String(), Denom: chain.USDBaseDenom, Amount: amount})
		require.NoError(t, err)
		output = res.Output
		require.True(t, output.IsPositive())
	})

	ctx := f.readCtx()
	require.True(t, f.app.BankKeeper.GetBalance(ctx, escrow, chain.NoahBaseDenom).IsZero(), "settlement allocated or burned the whole offer")
	require.Equal(t, output, f.app.BankKeeper.GetBalance(ctx, custody, chain.USDBaseDenom))
	require.Equal(t, amount, f.app.BankKeeper.GetBalance(ctx, custody, chain.NoahBaseDenom).Amount, "only the executed order left custody")
	has, err := f.app.DisbursementKeeper.ConversionOrders.Has(ctx, chain.USDBaseDenom)
	require.NoError(t, err)
	require.False(t, has, "an exhausted order is removed")
	krw, err := f.app.DisbursementKeeper.ConversionOrders.Get(ctx, chain.KRWBaseDenom)
	require.NoError(t, err)
	require.Equal(t, amount, krw.Remaining)

	balance, err := queryServer.Balance(ctx, &disbursementtypes.QueryBalanceRequest{Denom: chain.USDBaseDenom})
	require.NoError(t, err)
	require.Equal(t, output.Amount, balance.Unallocated, "converted stablecoins fund the next compensation award")
	noah, err := queryServer.Balance(ctx, &disbursementtypes.QueryBalanceRequest{Denom: chain.NoahBaseDenom})
	require.NoError(t, err)
	require.Equal(t, amount, noah.Converting)
	require.True(t, noah.Unallocated.IsZero())

	f.nextBlock(func(ctx sdk.Context) {
		_, err := msgServer.CreateGrant(ctx, &disbursementtypes.MsgCreateGrant{
			Authority: authority, Kind: disbursementtypes.GrantKind_GRANT_KIND_COMPENSATION, Beneficiary: f.trader.String(),
			Amount: output, Schedule: []disbursementtypes.Period{{Length: 1, Parts: 1}}, Reference: "converted pay",
		})
		require.NoError(t, err)
	})
}
