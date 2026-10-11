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
	marketkeeper "github.com/ararat-network/ark/x/market/keeper"
	markettypes "github.com/ararat-network/ark/x/market/types"
)

// TestDisbursementFundsAwardsThroughMarket runs stablecoin awards through real blocks: an award
// converts only its shortfall from the contributor pool, Market escrows and settles the NOAH like
// any trader's, the stablecoin is reserved for the award, a cap that buys too little or a pool that
// cannot cover the offer refuses it, and cancelled pay funds the next award without a conversion.
func TestDisbursementFundsAwardsThroughMarket(t *testing.T) {
	f := newActivationFixture(t)
	msgServer := disbursementkeeper.NewMsgServerImpl(f.app.DisbursementKeeper)
	queryServer := disbursementkeeper.NewQueryServerImpl(f.app.DisbursementKeeper)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	contributors := authtypes.NewModuleAddress(disbursementtypes.ContributorsName)
	escrow := authtypes.NewModuleAddress(markettypes.ModuleName)
	pool := chain.NativeBaseAmount(1_000_000)
	// Small against the fixture pool's depth, so the fill pays the 2% floor rather than slippage.
	pay := sdk.NewCoin(chain.USDBaseDenom, chain.NativeBaseAmount(10))
	// A lump on the default fortnight: nothing accrues before the cancellation below.
	award := func(ctx sdk.Context, maxOffer math.Int) (uint64, error) {
		res, err := msgServer.Compensate(ctx, &disbursementtypes.MsgCompensate{
			Authority: authority, Beneficiary: f.trader.String(),
			Amount: pay, StartTime: uint64(ctx.BlockTime().Unix()), Periods: 1, Reference: "converted pay", MaxOffer: maxOffer,
		})
		if err != nil {
			return 0, err
		}
		return res.GrantId, nil
	}

	var offer, output sdk.Coin
	var first uint64
	f.nextBlock(func(ctx sdk.Context) {
		// Stands in for genesis funding; the contributor pool needs no opening.
		apptestutil.FundModule(t, f.app, ctx, disbursementtypes.ContributorsName, sdk.NewCoins(chain.NoahCoin(pool)))
		quote, err := marketkeeper.NewQueryServerImpl(f.app.MarketKeeper).Buy(ctx, &markettypes.QueryBuyRequest{AskCoin: pay.String(), OfferDenom: chain.NoahBaseDenom})
		require.NoError(t, err)
		offer, output = quote.OfferCoin, quote.SwapCoin
		require.True(t, output.Amount.GTE(pay.Amount))
		// A cap below the sized offer refuses the award whole; the sized offer funds it.
		refused, _ := ctx.CacheContext()
		_, err = award(refused, offer.Amount.SubRaw(1))
		require.ErrorIs(t, err, markettypes.ErrMaximumOfferExceeded)
		// Reserved principal is not for sale: an offer the pool cannot cover is refused though the balance holds it.
		short, _ := ctx.CacheContext()
		_, err = msgServer.GrantOwnership(short, &disbursementtypes.MsgGrantOwnership{
			Authority: authority, Beneficiary: f.trader.String(),
			Amount: pool.Sub(offer.Amount).AddRaw(1), StartTime: uint64(ctx.BlockTime().Unix()), Reference: "reserved",
		})
		require.NoError(t, err)
		_, err = award(short, offer.Amount)
		require.ErrorContains(t, err, "exceeds the contributor pool")
		first, err = award(ctx, offer.Amount)
		require.NoError(t, err)
	})

	ctx := f.readCtx()
	require.True(t, f.app.BankKeeper.GetBalance(ctx, escrow, chain.NoahBaseDenom).IsZero(), "settlement allocated or burned the whole offer")
	require.Equal(t, pool.Sub(offer.Amount), f.app.BankKeeper.GetBalance(ctx, contributors, chain.NoahBaseDenom).Amount, "only the sized offer left the account")
	balance, err := queryServer.Balance(ctx, &disbursementtypes.QueryBalanceRequest{Denom: pay.Denom})
	require.NoError(t, err)
	require.Len(t, balance.Accounts, 1)
	require.Equal(t, pay.Amount, balance.Accounts[0].Reserved.Amount)
	require.Equal(t, output.Amount.Sub(pay.Amount), balance.Accounts[0].Uncommitted.Amount, "the surplus waits for the next award")
	journal, err := queryServer.GrantJournal(ctx, &disbursementtypes.QueryGrantJournalRequest{GrantId: first})
	require.NoError(t, err)
	actions := make([]disbursementtypes.GrantAction, 0, len(journal.Entries))
	for _, e := range journal.Entries {
		actions = append(actions, e.Action)
	}
	require.Equal(t, []disbursementtypes.GrantAction{disbursementtypes.GrantAction_GRANT_ACTION_CREATED, disbursementtypes.GrantAction_GRANT_ACTION_CONVERTED}, actions)

	f.nextBlock(func(ctx sdk.Context) {
		_, err := msgServer.CancelGrants(ctx, &disbursementtypes.MsgCancelGrants{Authority: authority, GrantIds: []uint64{first}})
		require.NoError(t, err)
		// Cancelled pay funds the next award, so it needs no cap and converts nothing.
		_, err = award(ctx, math.ZeroInt())
		require.NoError(t, err)
	})
	ctx = f.readCtx()
	require.Equal(t, pool.Sub(offer.Amount), f.app.BankKeeper.GetBalance(ctx, contributors, chain.NoahBaseDenom).Amount, "the second award converted nothing")
}
