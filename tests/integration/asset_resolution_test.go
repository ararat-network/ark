package integration

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/ararat-network/ark/pkg/chain"
	assetkeeper "github.com/ararat-network/ark/x/asset/keeper"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	marketkeeper "github.com/ararat-network/ark/x/market/keeper"
	markettypes "github.com/ararat-network/ark/x/market/types"
	oraclekeeper "github.com/ararat-network/ark/x/oracle/keeper"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	treasurykeeper "github.com/ararat-network/ark/x/treasury/keeper"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// TestSuspensionSettlementAndRecovery runs the whole failure path on one listed
// asset: suspension is a status move on a claim, and the reference unit's price
// data is a different axis entirely — so ReferenceState, Market's base pool,
// and Treasury's cap must all sit still while an asset fails, settles, and
// recovers. The reference denomination itself carries no listed asset, which is
// what makes that separation structural rather than merely observed.
func TestSuspensionSettlementAndRecovery(t *testing.T) {
	f := newActivationFixture(t)
	marketMsgServer := marketkeeper.NewMsgServerImpl(f.app.MarketKeeper)
	treasuryQueryServer := treasurykeeper.NewQueryServerImpl(f.app.TreasuryKeeper)
	bufferAddress := authtypes.NewModuleAddress(treasurytypes.RedemptionBufferName)

	fundStatus := func(ctx sdk.Context) *treasurytypes.QueryFundStatusResponse {
		response, err := treasuryQueryServer.FundStatus(ctx, &treasurytypes.QueryFundStatusRequest{})
		require.NoError(t, err)

		return response
	}

	var acquired sdk.Coin
	f.nextBlock(func(ctx sdk.Context) {
		response, err := marketMsgServer.Swap(ctx, &markettypes.MsgSwap{
			Trader:         f.trader.String(),
			OfferCoin:      sdk.NewCoin(chain.NoahBaseDenom, chain.NativeBaseAmount(1_000)),
			AskDenom:       chain.USDBaseDenom,
			MinimumReceive: sdk.NewCoin(chain.USDBaseDenom, math.OneInt()),
		})
		require.NoError(t, err)
		acquired = response.SwapCoin
		require.True(t, acquired.IsPositive())
	})

	f.nextBlock(func(ctx sdk.Context) {
		marketCapacity, err := f.app.MarketKeeper.ConversionPolicy.Get(ctx)
		require.NoError(t, err)
		poolBefore := marketCapacity.BasePool
		treasuryParams, err := f.app.TreasuryKeeper.Params.Get(ctx)
		require.NoError(t, err)
		capBefore := treasuryParams.ReferenceTaxCap

		asset, err := f.app.AssetKeeper.Assets.Get(ctx, chain.USDBaseDenom)
		require.NoError(t, err)
		require.NoError(t, f.app.AssetKeeper.SuspendAsset(ctx, chain.USDBaseDenom, asset.Version))

		// Nothing in the reference machinery moved, and no rebase executor ran.
		referenceDenom, err := f.app.OracleKeeper.GetReferenceDenom(ctx)
		require.NoError(t, err)
		require.Equal(t, chain.XDRBaseDenom, referenceDenom)
		marketCapacity, err = f.app.MarketKeeper.ConversionPolicy.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, poolBefore, marketCapacity.BasePool)
		treasuryParams, err = f.app.TreasuryKeeper.Params.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, capBefore, treasuryParams.ReferenceTaxCap)
		for _, event := range ctx.EventManager().Events() {
			require.NotEqual(t, "ark.market.v1.EventPoolUpdated", event.Type)
			require.NotEqual(t, "ark.treasury.v1.EventReferenceTaxCapRebased", event.Type)
		}

		// Both conversion directions close in the block the suspension lands:
		// the failed claim can neither be issued nor exited at a market price.
		_, err = marketMsgServer.Swap(ctx, &markettypes.MsgSwap{
			Trader:         f.trader.String(),
			OfferCoin:      sdk.NewCoin(chain.NoahBaseDenom, chain.NativeBaseAmount(1)),
			AskDenom:       chain.USDBaseDenom,
			MinimumReceive: sdk.NewCoin(chain.USDBaseDenom, math.OneInt()),
		})
		require.ErrorIs(t, err, markettypes.ErrIneligibleAsset)
		_, err = marketMsgServer.Swap(ctx, &markettypes.MsgSwap{
			Trader:         f.trader.String(),
			OfferCoin:      acquired,
			AskDenom:       chain.NoahBaseDenom,
			MinimumReceive: sdk.NewCoin(chain.NoahBaseDenom, math.OneInt()),
		})
		require.ErrorIs(t, err, markettypes.ErrIneligibleAsset)

		// Balances are preserved and the exposure is disclosed by name rather
		// than valued by a rate nobody should trust.
		require.Equal(t, acquired.Amount, f.app.BankKeeper.GetBalance(ctx, f.trader, chain.USDBaseDenom).Amount)
		status := fundStatus(ctx)
		require.False(t, valuationComplete(status))
		require.Equal(
			t,
			[]sdk.Coin{sdk.NewCoin(chain.USDBaseDenom, acquired.Amount)},
			status.UntrustedSuspendedSupply,
		)
	})

	// A plan is recognised from the block it opens: the commitment is
	// irrevocable from open, and the activation delay gates execution only.
	// Quoted in NOAH per one unit of the settled asset, the same orientation as
	// an oracle rate, so the plan serves as a rate set unchanged.
	redemptionRate := math.LegacyNewDecWithPrec(5, 3)
	noahFor := func(amount math.Int) math.LegacyDec {
		return math.LegacyNewDecFromInt(amount).Mul(redemptionRate)
	}
	f.nextBlock(func(ctx sdk.Context) {
		asset, err := f.app.AssetKeeper.Assets.Get(ctx, chain.USDBaseDenom)
		require.NoError(t, err)
		params, err := f.app.AssetKeeper.Params.Get(ctx)
		require.NoError(t, err)
		activationHeight := ctx.BlockHeight() + int64(params.SettlementActivationDelayBlocks)
		require.NoError(t, f.app.AssetKeeper.OpenSettlement(
			ctx,
			chain.USDBaseDenom,
			asset.Version,
			redemptionRate,
			activationHeight+1,
		))

		status := fundStatus(ctx)
		require.True(t, valuationComplete(status))
		require.True(t, noahFor(acquired.Amount).Equal(
			status.SettlementLiability.Amount,
		))
		require.Empty(t, status.UntrustedSuspendedSupply)

		// Treasury owes it; Market may not yet pay it. That split is the
		// activation delay's entire purpose.
		_, err = marketMsgServer.Settle(ctx, &markettypes.MsgSettle{
			Trader:    f.trader.String(),
			OfferCoin: sdk.NewCoin(chain.USDBaseDenom, math.OneInt()),
		})
		require.ErrorIs(t, err, markettypes.ErrNoActiveSettlement)
	})

	// The announced activation is a day of blocks away, which no fixture walks;
	// the delay itself is covered in the keeper suite. Bring the plan forward in
	// place to reach the executable state, and fund the buffer below the
	// recognised liability so the draw is partial and provably proportional.
	var earliestClosing int64
	f.nextBlock(func(ctx sdk.Context) {
		plan, found, err := f.app.AssetKeeper.GetSettlementPlan(ctx, chain.USDBaseDenom)
		require.NoError(t, err)
		require.True(t, found)
		plan.ActivationHeight = ctx.BlockHeight()
		earliestClosing = ctx.BlockHeight() + 4
		plan.EarliestClosingHeight = earliestClosing
		require.NoError(t, f.app.AssetKeeper.SettlementPlans.Set(ctx, chain.USDBaseDenom, plan))

		half := noahFor(acquired.Amount).QuoInt64(2).TruncateInt()
		require.True(t, half.IsPositive())
		require.NoError(t, f.app.BankKeeper.MintCoins(
			ctx,
			markettypes.ModuleName,
			sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, half)),
		))
		require.NoError(t, f.app.BankKeeper.SendCoinsFromModuleToModule(
			ctx,
			markettypes.ModuleName,
			treasurytypes.RedemptionBufferName,
			sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, half)),
		))
	})

	redeemed := acquired.Amount.QuoRaw(2)
	entitlement := noahFor(redeemed).TruncateInt()
	var bufferBefore, noahSupplyBefore math.Int
	f.nextBlock(func(ctx sdk.Context) {
		offer := sdk.NewCoin(chain.USDBaseDenom, redeemed)
		bufferBefore = f.app.BankKeeper.GetBalance(ctx, bufferAddress, chain.NoahBaseDenom).Amount
		noahSupplyBefore = f.app.BankKeeper.GetSupply(ctx, chain.NoahBaseDenom).Amount
		traderNoahBefore := f.app.BankKeeper.GetBalance(ctx, f.trader, chain.NoahBaseDenom).Amount

		response, err := marketMsgServer.Settle(ctx, &markettypes.MsgSettle{
			Trader:    f.trader.String(),
			OfferCoin: offer,
		})
		require.NoError(t, err)
		require.Equal(t, sdk.NewCoin(chain.NoahBaseDenom, entitlement), response.RedeemedCoin)

		// The holder receives the whole committed entitlement immediately and in
		// full, minted outright. The exit never waits on a valuation, which is
		// the point of paying it before the Buffer's share of it is worked out.
		require.Equal(
			t,
			entitlement,
			f.app.BankKeeper.GetBalance(ctx, f.trader, chain.NoahBaseDenom).Amount.Sub(traderNoahBefore),
		)
		require.Equal(
			t,
			bufferBefore,
			f.app.BankKeeper.GetBalance(ctx, bufferAddress, chain.NoahBaseDenom).Amount,
			"the Buffer funds the exit at settlement, not inside it",
		)

		// The settled asset is burned, never returned to circulation.
		require.Equal(
			t,
			acquired.Amount.Sub(redeemed),
			f.app.BankKeeper.GetSupply(ctx, chain.USDBaseDenom).Amount,
		)
	})

	// The block's settlement funded the exit: the buffer paid its coverage share
	// and only the uncovered remainder survives as new supply, which is what
	// keeps an orderly failure a bounded dilution rather than a haircut.
	f.nextBlock(func(ctx sdk.Context) {
		bufferAfter := f.app.BankKeeper.GetBalance(ctx, bufferAddress, chain.NoahBaseDenom).Amount
		bufferPaid := bufferBefore.Sub(bufferAfter)
		require.True(t, bufferPaid.IsPositive(), "buffer covered part of the exit")
		require.True(t, bufferPaid.LT(entitlement), "buffer did not cover all of it")
		require.Equal(
			t,
			entitlement.Sub(bufferPaid),
			f.app.BankKeeper.GetSupply(ctx, chain.NoahBaseDenom).Amount.Sub(noahSupplyBefore),
		)
	})

	// The announced window binds against the one message that could break it.
	// Cancellation is long gone — the plan activated — and write-off is refused
	// until the committed height, so no governance act can derecognize a holder
	// inside the period they were shown.
	f.nextBlock(func(ctx sdk.Context) {
		asset, err := f.app.AssetKeeper.Assets.Get(ctx, chain.USDBaseDenom)
		require.NoError(t, err)
		err = f.app.AssetKeeper.CancelSettlement(ctx, chain.USDBaseDenom, asset.Version)
		require.ErrorIs(t, err, assettypes.ErrInvalidAssetTransition)
		require.ErrorContains(t, err, "can no longer be cancelled")

		err = f.app.AssetKeeper.WriteOffAsset(ctx, chain.USDBaseDenom, asset.Version)
		require.ErrorIs(t, err, assettypes.ErrInvalidAssetTransition)
		require.ErrorContains(t, err, "committed until height")
	})

	// Blocks pass inside the window and the guarantee holds on every one of
	// them. The bound stops one short: nextBlock runs its transaction at the
	// following height, so this walks up to the last block still inside the
	// commitment.
	for f.height < earliestClosing-1 {
		f.nextBlock(func(ctx sdk.Context) {
			asset, err := f.app.AssetKeeper.Assets.Get(ctx, chain.USDBaseDenom)
			require.NoError(t, err)
			err = f.app.AssetKeeper.WriteOffAsset(ctx, chain.USDBaseDenom, asset.Version)
			require.ErrorIs(t, err, assettypes.ErrInvalidAssetTransition)
		})
		waiting, err := f.app.AssetKeeper.Assets.Get(f.readCtx(), chain.USDBaseDenom)
		require.NoError(t, err)
		require.Equal(t, assettypes.AssetStatus_ASSET_STATUS_SUSPENDED, waiting.Status)
	}

	// Recovery is the exception: it closes the plan in the same act, without
	// consulting the window, because holders get the ordinary exit back rather
	// than losing one. It lands in the block it executes, into ISSUANCE_HALTED
	// and never straight back to ACTIVE.
	remaining := acquired.Amount.Sub(redeemed)
	f.nextBlock(func(ctx sdk.Context) {
		asset, err := f.app.AssetKeeper.Assets.Get(ctx, chain.USDBaseDenom)
		require.NoError(t, err)
		require.NoError(t, f.app.AssetKeeper.RecoverAsset(ctx, chain.USDBaseDenom, asset.Version))

		_, found, err := f.app.AssetKeeper.GetSettlementPlan(ctx, chain.USDBaseDenom)
		require.NoError(t, err)
		require.False(t, found, "recovery closed the plan")

		recovered, err := f.app.AssetKeeper.Assets.Get(ctx, chain.USDBaseDenom)
		require.NoError(t, err)
		require.Equal(t, assettypes.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED, recovered.Status)

		// Executed redemptions stand, and the supply nobody redeemed is priced
		// by the live feed again rather than by the settlement commitment.
		status := fundStatus(ctx)
		require.True(t, status.SettlementLiability.Amount.IsZero())
		require.Empty(t, status.UntrustedSuspendedSupply)
		require.Equal(
			t,
			remaining,
			f.app.BankKeeper.GetSupply(ctx, chain.USDBaseDenom).Amount,
		)
	})

	f.nextBlock(func(ctx sdk.Context) {
		// Recognised and priced again, and exitable — but not issuable until
		// governance says so in a separate act.
		status := fundStatus(ctx)
		require.True(t, valuationComplete(status))
		require.True(t, status.PricedLiability.Amount.IsPositive())
		require.Empty(t, status.UntrustedSuspendedSupply)

		_, err := marketMsgServer.Swap(ctx, &markettypes.MsgSwap{
			Trader:         f.trader.String(),
			OfferCoin:      sdk.NewCoin(chain.NoahBaseDenom, chain.NativeBaseAmount(1)),
			AskDenom:       chain.USDBaseDenom,
			MinimumReceive: sdk.NewCoin(chain.USDBaseDenom, math.OneInt()),
		})
		require.ErrorIs(t, err, markettypes.ErrIneligibleAsset)

		asset, err := f.app.AssetKeeper.Assets.Get(ctx, chain.USDBaseDenom)
		require.NoError(t, err)
		require.NoError(t, f.app.AssetKeeper.ResumeIssuance(ctx, chain.USDBaseDenom, asset.Version))

		_, err = marketMsgServer.Swap(ctx, &markettypes.MsgSwap{
			Trader:         f.trader.String(),
			OfferCoin:      sdk.NewCoin(chain.NoahBaseDenom, chain.NativeBaseAmount(1)),
			AskDenom:       chain.USDBaseDenom,
			MinimumReceive: sdk.NewCoin(chain.USDBaseDenom, math.OneInt()),
		})
		require.NoError(t, err)
	})
}

// TestRecoveryRequiresAnActiveFeed isolates the other half of the recovery
// gate. The plan gate is covered above on the reference-carrying asset; this
// uses a separate suspended asset so the fixture can take one feed down without
// taking the protocol reference's price data with it.
//
// A suspended asset holds no claim on its feed, so governance may legitimately
// retire it. Recovery then has nothing to price against, and because the
// transition takes effect immediately a scheduled re-addition is not enough:
// an adding feed has no rate yet.
func TestRecoveryRequiresAnActiveFeed(t *testing.T) {
	f := newActivationFixture(t)
	oracleMsgServer := oraclekeeper.NewMsgServerImpl(f.app.OracleKeeper)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	f.nextBlock(func(ctx sdk.Context) {
		asset, err := f.app.AssetKeeper.Assets.Get(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		require.NoError(t, f.app.AssetKeeper.SuspendAsset(ctx, chain.KRWBaseDenom, asset.Version))
	})

	// Suspension released the referent, so the feed may be retired. Removal is
	// in flight from here, and a removing feed already cannot price the asset.
	f.nextBlock(func(ctx sdk.Context) {
		_, err := oracleMsgServer.RemoveFeed(ctx, &oracletypes.MsgRemoveFeed{
			Authority: authority,
			Denom:     chain.KRWBaseDenom,
		})
		require.NoError(t, err)

		asset, err := f.app.AssetKeeper.Assets.Get(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		err = f.app.AssetKeeper.RecoverAsset(ctx, chain.KRWBaseDenom, asset.Version)
		require.ErrorIs(t, err, assettypes.ErrAssetNotPriceable)
	})

	// Once the removal activates the feed is off entirely, and still no basis
	// for recovery.
	f.advancePastFeed(chain.KRWBaseDenom)
	f.nextBlock(func(ctx sdk.Context) {
		asset, err := f.app.AssetKeeper.Assets.Get(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		err = f.app.AssetKeeper.RecoverAsset(ctx, chain.KRWBaseDenom, asset.Version)
		require.ErrorIs(t, err, assettypes.ErrAssetNotPriceable)
	})

	// A scheduled re-addition is not enough either: an adding feed has no rate.
	f.nextBlock(func(ctx sdk.Context) {
		_, err := oracleMsgServer.AddFeed(ctx, &oracletypes.MsgAddFeed{
			Authority: authority,
			Denom:     chain.KRWBaseDenom,
		})
		require.NoError(t, err)

		asset, err := f.app.AssetKeeper.Assets.Get(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		err = f.app.AssetKeeper.RecoverAsset(ctx, chain.KRWBaseDenom, asset.Version)
		require.ErrorIs(t, err, assettypes.ErrAssetNotPriceable)
	})

	f.advanceToFeed(chain.KRWBaseDenom)

	f.nextBlock(func(ctx sdk.Context) {
		asset, err := f.app.AssetKeeper.Assets.Get(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		require.NoError(t, f.app.AssetKeeper.RecoverAsset(ctx, chain.KRWBaseDenom, asset.Version))

		recovered, err := f.app.AssetKeeper.Assets.Get(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		require.Equal(t, assettypes.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED, recovered.Status)
	})
}

// TestWriteOffAndReinstatement drives derecognition and its reversal. The claim
// under test is that a write-off is an accounting act: what it changes is what
// Treasury recognises and what the record says, never what holders hold.
func TestWriteOffAndReinstatement(t *testing.T) {
	f := newActivationFixture(t)
	marketMsgServer := marketkeeper.NewMsgServerImpl(f.app.MarketKeeper)
	treasuryQueryServer := treasurykeeper.NewQueryServerImpl(f.app.TreasuryKeeper)
	assetQueryServer := assetkeeper.NewQueryServerImpl(f.app.AssetKeeper)

	fundStatus := func(ctx sdk.Context) *treasurytypes.QueryFundStatusResponse {
		response, err := treasuryQueryServer.FundStatus(ctx, &treasurytypes.QueryFundStatusRequest{})
		require.NoError(t, err)

		return response
	}
	resolutionRecords := func(ctx sdk.Context) []assettypes.ResolutionRecord {
		response, err := assetQueryServer.ResolutionHistory(ctx, &assettypes.QueryResolutionHistoryRequest{
			Denom: chain.KRWBaseDenom,
		})
		require.NoError(t, err)

		return response.ResolutionRecords
	}

	var acquired sdk.Coin
	f.nextBlock(func(ctx sdk.Context) {
		response, err := marketMsgServer.Swap(ctx, &markettypes.MsgSwap{
			Trader:         f.trader.String(),
			OfferCoin:      sdk.NewCoin(chain.NoahBaseDenom, chain.NativeBaseAmount(1_000)),
			AskDenom:       chain.KRWBaseDenom,
			MinimumReceive: sdk.NewCoin(chain.KRWBaseDenom, math.OneInt()),
		})
		require.NoError(t, err)
		acquired = response.SwapCoin

		asset, err := f.app.AssetKeeper.Assets.Get(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		require.NoError(t, f.app.AssetKeeper.SuspendAsset(ctx, chain.KRWBaseDenom, asset.Version))
	})

	var firstRecord assettypes.ResolutionRecord
	f.nextBlock(func(ctx sdk.Context) {
		asset, err := f.app.AssetKeeper.Assets.Get(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		require.NoError(t, f.app.AssetKeeper.WriteOffAsset(ctx, chain.KRWBaseDenom, asset.Version))

		// Balances untouched.
		require.Equal(t, acquired.Amount, f.app.BankKeeper.GetBalance(ctx, f.trader, chain.KRWBaseDenom).Amount)
		require.Equal(t, acquired.Amount, f.app.BankKeeper.GetSupply(ctx, chain.KRWBaseDenom).Amount)

		// The exposure moves from unvaluable-but-recognised to disclosed and
		// excluded, which is what makes the total available again: nothing
		// recognised is left unpriced.
		status := fundStatus(ctx)
		require.True(t, valuationComplete(status))
		require.Empty(t, status.UntrustedSuspendedSupply)
		require.Len(t, status.WrittenOffExposure, 1)
		require.Equal(
			t,
			sdk.NewCoin(chain.KRWBaseDenom, acquired.Amount),
			status.WrittenOffExposure[0].OutstandingSupply,
		)
		require.True(t, status.PricedLiability.Amount.IsZero())
		require.True(t, status.NominalLiability.Amount.IsZero())

		records := resolutionRecords(ctx)
		require.Len(t, records, 1)
		firstRecord = records[0]
		require.Equal(t, assettypes.ResolutionKind_RESOLUTION_KIND_WRITE_OFF, firstRecord.Kind)
		require.Equal(t, sdk.NewCoin(chain.KRWBaseDenom, acquired.Amount), firstRecord.OutstandingSupply)
	})

	// Reinstatement through the recovery path returns it to the oracle-priced set
	// in one act, and the exposure stops being disclosed as written off. The
	// historical record is append-only and is not rewritten by the reversal.
	f.nextBlock(func(ctx sdk.Context) {
		asset, err := f.app.AssetKeeper.Assets.Get(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		require.NoError(t, f.app.AssetKeeper.RecoverAsset(ctx, chain.KRWBaseDenom, asset.Version))

		reinstated, err := f.app.AssetKeeper.Assets.Get(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		require.Equal(t, assettypes.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED, reinstated.Status)

		status := fundStatus(ctx)
		require.Empty(t, status.UntrustedSuspendedSupply)
		require.Empty(t, status.WrittenOffExposure)

		records := resolutionRecords(ctx)
		require.Len(t, records, 1)
		require.Equal(t, firstRecord, records[0])
	})

	// That block's preblock completed the recovery, because a written-off asset
	// reinstated through recovery carries no plan to wait on.
	recovered, err := f.app.AssetKeeper.Assets.Get(f.readCtx(), chain.KRWBaseDenom)
	require.NoError(t, err)
	require.Equal(t, assettypes.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED, recovered.Status)

	// A second cycle appends rather than replaces: the first record survives the
	// asset going through the same door twice.
	f.nextBlock(func(ctx sdk.Context) {
		asset, err := f.app.AssetKeeper.Assets.Get(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		require.NoError(t, f.app.AssetKeeper.SuspendAsset(ctx, chain.KRWBaseDenom, asset.Version))
	})

	f.nextBlock(func(ctx sdk.Context) {
		asset, err := f.app.AssetKeeper.Assets.Get(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		require.NoError(t, f.app.AssetKeeper.WriteOffAsset(ctx, chain.KRWBaseDenom, asset.Version))

		records := resolutionRecords(ctx)
		require.Len(t, records, 2)
		require.Equal(t, firstRecord, records[0])
		require.Greater(t, records[1].Version, firstRecord.Version)
	})

	// Written-off supply retires directly, and appends nothing: the
	// derecognition was already recorded, so there is nothing left to disclose.
	f.nextBlock(func(ctx sdk.Context) {
		asset, err := f.app.AssetKeeper.Assets.Get(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		require.Equal(t, assettypes.AssetStatus_ASSET_STATUS_WRITTEN_OFF, asset.Status)
		require.NoError(t, f.app.AssetKeeper.FinaliseRetirement(
			ctx,
			chain.KRWBaseDenom,
			asset.Version,
			math.ZeroInt(),
		))

		retired, err := f.app.AssetKeeper.Assets.Get(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		require.Equal(t, assettypes.AssetStatus_ASSET_STATUS_RETIRED, retired.Status)

		require.Len(t, resolutionRecords(ctx), 2)

		// Balances outlive the tombstone.
		require.Equal(t, acquired.Amount, f.app.BankKeeper.GetBalance(ctx, f.trader, chain.KRWBaseDenom).Amount)
	})
}

// TestEmergencyMandateSingleProposal drives the committee path end to end: a
// signer who is not the governance authority moving consensus-visible state
// under a live mandate. The keeper suite owns the rejection matrix; what is only
// provable here is that the emergency route reaches the same semantics as
// governance, and that no suspension stirs the reference machinery — the
// reference denom denominates Market's base pool and Treasury's cap, and it names a
// feed no asset is listed against.
func TestEmergencyMandateSingleProposal(t *testing.T) {
	f := newActivationFixture(t)
	assetMsgServer := assetkeeper.NewMsgServerImpl(f.app.AssetKeeper)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	// Account addresses are exactly 20 bytes; the committee is validated as a
	// canonical account address on the way in.
	committee := sdk.AccAddress([]byte("emergency-committee.")).String()

	// The mandate is governance's appointment: a window, and exactly one signer.
	var term uint64
	f.nextBlock(func(ctx sdk.Context) {
		_, err := assetMsgServer.SetEmergencyMandate(ctx, &assettypes.MsgSetEmergencyMandate{
			Authority:        authority,
			Committee:        committee,
			ActivationHeight: uint64(ctx.BlockHeight()),
			ExpiryHeight:     uint64(ctx.BlockHeight()) + 1_000,
		})
		require.NoError(t, err)

		mandate, err := f.app.AssetKeeper.EmergencyMandate.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, committee, mandate.Committee)
		term = mandate.Term
		require.NotZero(t, term)
	})

	// Committee-suspend a listed asset. The suspension is a claim's failure;
	// the reference unit's price data is untouched, so no rebase executor may
	// run and neither consumer's reference-denominated state may move.
	f.nextBlock(func(ctx sdk.Context) {
		before, err := f.app.AssetKeeper.Assets.Get(ctx, chain.USDBaseDenom)
		require.NoError(t, err)
		marketCapacity, err := f.app.MarketKeeper.ConversionPolicy.Get(ctx)
		require.NoError(t, err)
		poolBefore := marketCapacity.BasePool
		treasuryParams, err := f.app.TreasuryKeeper.Params.Get(ctx)
		require.NoError(t, err)
		capBefore := treasuryParams.ReferenceTaxCap

		_, err = assetMsgServer.EmergencySuspendAsset(ctx, &assettypes.MsgEmergencySuspendAsset{
			Committee:    committee,
			Denom:        chain.USDBaseDenom,
			ExpectedTerm: term,
		})
		require.NoError(t, err)

		suspended, err := f.app.AssetKeeper.Assets.Get(ctx, chain.USDBaseDenom)
		require.NoError(t, err)
		require.Equal(t, assettypes.AssetStatus_ASSET_STATUS_SUSPENDED, suspended.Status)
		require.Equal(t, before.Version+1, suspended.Version)

		referenceDenom, err := f.app.OracleKeeper.GetReferenceDenom(ctx)
		require.NoError(t, err)
		require.Equal(t, chain.XDRBaseDenom, referenceDenom)
		marketCapacity, err = f.app.MarketKeeper.ConversionPolicy.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, poolBefore, marketCapacity.BasePool)
		treasuryParams, err = f.app.TreasuryKeeper.Params.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, capBefore, treasuryParams.ReferenceTaxCap)

		// The suspension is attributed to its term, which is what makes the
		// usage bound below term-scoped rather than permanent.
		var reportedTerm string
		for _, event := range ctx.EventManager().Events() {
			require.NotEqual(t, "ark.market.v1.EventPoolUpdated", event.Type)
			require.NotEqual(t, "ark.treasury.v1.EventReferenceTaxCapRebased", event.Type)
			if event.Type != "ark.asset.v1.EventEmergencySuspended" {
				continue
			}
			for _, attribute := range event.Attributes {
				if attribute.Key == "term" {
					reportedTerm = attribute.Value
				}
			}
		}
		require.Equal(t, strconv.FormatUint(term, 10), strings.Trim(reportedTerm, `"`))

		used, err := f.app.AssetKeeper.EmergencySuspensions.Has(ctx, chain.USDBaseDenom)
		require.NoError(t, err)
		require.True(t, used, "suspension recorded against the term")
	})

	// Halting is governance's, suspending is the committee's. A wind-down that
	// turns into a peg failure crosses that line, and the committee's single
	// tool must reach the already-halted asset.
	f.nextBlock(func(ctx sdk.Context) {
		asset, err := f.app.AssetKeeper.Assets.Get(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		require.NoError(t, f.app.AssetKeeper.HaltIssuance(ctx, chain.KRWBaseDenom, asset.Version))
		halted, err := f.app.AssetKeeper.Assets.Get(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		require.Equal(t, assettypes.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED, halted.Status)

		_, err = assetMsgServer.EmergencySuspendAsset(ctx, &assettypes.MsgEmergencySuspendAsset{
			Committee:    committee,
			Denom:        chain.KRWBaseDenom,
			ExpectedTerm: term,
		})
		require.NoError(t, err)
		suspended, err := f.app.AssetKeeper.Assets.Get(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		require.Equal(t, assettypes.AssetStatus_ASSET_STATUS_SUSPENDED, suspended.Status)
	})

	// Governance restores the suspended asset by the ordinary route.
	f.nextBlock(func(ctx sdk.Context) {
		asset, err := f.app.AssetKeeper.Assets.Get(ctx, chain.USDBaseDenom)
		require.NoError(t, err)
		require.NoError(t, f.app.AssetKeeper.RecoverAsset(ctx, chain.USDBaseDenom, asset.Version))
	})
	recovered, err := f.app.AssetKeeper.Assets.Get(f.readCtx(), chain.USDBaseDenom)
	require.NoError(t, err)
	require.Equal(t, assettypes.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED, recovered.Status)

	f.nextBlock(func(ctx sdk.Context) {
		asset, err := f.app.AssetKeeper.Assets.Get(ctx, chain.USDBaseDenom)
		require.NoError(t, err)
		require.NoError(t, f.app.AssetKeeper.ResumeIssuance(ctx, chain.USDBaseDenom, asset.Version))

		// One shot per asset per term: repeating a suspension that governance
		// has already undone is griefing, and needs a governance vote or a
		// fresh mandate.
		_, err = assetMsgServer.EmergencySuspendAsset(ctx, &assettypes.MsgEmergencySuspendAsset{
			Committee:    committee,
			Denom:        chain.USDBaseDenom,
			ExpectedTerm: term,
		})
		require.ErrorIs(t, err, assettypes.ErrEmergencySuspensionConsumed)

		// Governance is never bound by the committee's per-term budget.
		restored, err := f.app.AssetKeeper.Assets.Get(ctx, chain.USDBaseDenom)
		require.NoError(t, err)
		require.Equal(t, assettypes.AssetStatus_ASSET_STATUS_ACTIVE, restored.Status)
		require.NoError(t, f.app.AssetKeeper.SuspendAsset(ctx, chain.USDBaseDenom, restored.Version))
	})

	// Replacing the mandate advances the term, retires the old one, and clears
	// recorded usage — so suspension becomes available to the new committee,
	// and messages carrying the old term are stale.
	f.nextBlock(func(ctx sdk.Context) {
		asset, err := f.app.AssetKeeper.Assets.Get(ctx, chain.USDBaseDenom)
		require.NoError(t, err)
		require.NoError(t, f.app.AssetKeeper.RecoverAsset(ctx, chain.USDBaseDenom, asset.Version))
	})

	f.nextBlock(func(ctx sdk.Context) {
		successor := sdk.AccAddress([]byte("successor-committee.")).String()
		_, err := assetMsgServer.SetEmergencyMandate(ctx, &assettypes.MsgSetEmergencyMandate{
			Authority:        authority,
			Committee:        successor,
			ActivationHeight: uint64(ctx.BlockHeight()),
			ExpiryHeight:     uint64(ctx.BlockHeight()) + 1_000,
		})
		require.NoError(t, err)

		mandate, err := f.app.AssetKeeper.EmergencyMandate.Get(ctx)
		require.NoError(t, err)
		require.Greater(t, mandate.Term, term)
		require.Equal(t, successor, mandate.Committee)

		// The retired committee cannot act at all, and the old term is stale
		// even in the successor's hands.
		_, err = assetMsgServer.EmergencySuspendAsset(ctx, &assettypes.MsgEmergencySuspendAsset{
			Committee:    committee,
			Denom:        chain.KRWBaseDenom,
			ExpectedTerm: mandate.Term,
		})
		require.ErrorIs(t, err, assettypes.ErrEmergencyMandateInactive)
		_, err = assetMsgServer.EmergencySuspendAsset(ctx, &assettypes.MsgEmergencySuspendAsset{
			Committee:    successor,
			Denom:        chain.KRWBaseDenom,
			ExpectedTerm: term,
		})
		require.ErrorIs(t, err, assettypes.ErrEmergencyMandateInactive)

		// Usage was cleared with the replacement, so suspension is available
		// again under the new term. USD is mid-recovery and KRW is suspended,
		// so an untouched asset is what proves the successor can act.
		_, err = assetMsgServer.EmergencySuspendAsset(ctx, &assettypes.MsgEmergencySuspendAsset{
			Committee:    successor,
			Denom:        chain.EURBaseDenom,
			ExpectedTerm: mandate.Term,
		})
		require.NoError(t, err)

		usedUnderOldTerm, err := f.app.AssetKeeper.EmergencySuspensions.Has(ctx, chain.USDBaseDenom)
		require.NoError(t, err)
		require.False(t, usedUnderOldTerm, "replacement cleared recorded usage")
	})
}

// TestEmergencySuspensionIsSeenByBlockSettlement pins what the committee's
// ordering freedom costs now. Governance transitions execute in x/gov's
// EndBlocker, after every reader, but the committee acts in an ordinary
// transaction, so a suspension can land ahead of an expansion in the same
// block. Under per-conversion settlement that expansion would have sized
// targets and burned the remainder against whatever valuation it happened to
// find — both irreversible, in the block an asset just failed.
//
// Deferring settlement removes the ordering from the question entirely: the
// block's only valuation is taken after both acts, so the suspension is seen
// however the transactions fell, the whole block's principal parks, and the
// degradation is disclosed once.
func TestEmergencySuspensionIsSeenByBlockSettlement(t *testing.T) {
	f := newActivationFixture(t)
	assetMsgServer := assetkeeper.NewMsgServerImpl(f.app.AssetKeeper)
	marketMsgServer := marketkeeper.NewMsgServerImpl(f.app.MarketKeeper)
	treasuryQueryServer := treasurykeeper.NewQueryServerImpl(f.app.TreasuryKeeper)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	committee := sdk.AccAddress([]byte("emergency-committee.")).String()

	// The asset must carry supply, or the partition skips it and suspending it
	// changes nothing about completeness.
	f.nextBlock(func(ctx sdk.Context) {
		_, err := marketMsgServer.Swap(ctx, &markettypes.MsgSwap{
			Trader:         f.trader.String(),
			OfferCoin:      sdk.NewCoin(chain.NoahBaseDenom, chain.NativeBaseAmount(1_000)),
			AskDenom:       chain.KRWBaseDenom,
			MinimumReceive: sdk.NewCoin(chain.KRWBaseDenom, math.OneInt()),
		})
		require.NoError(t, err)
	})

	var term uint64
	f.nextBlock(func(ctx sdk.Context) {
		_, err := assetMsgServer.SetEmergencyMandate(ctx, &assettypes.MsgSetEmergencyMandate{
			Authority:        authority,
			Committee:        committee,
			ActivationHeight: uint64(ctx.BlockHeight()),
			ExpiryHeight:     uint64(ctx.BlockHeight()) + 1_000,
		})
		require.NoError(t, err)
		mandate, err := f.app.AssetKeeper.EmergencyMandate.Get(ctx)
		require.NoError(t, err)
		term = mandate.Term
	})

	// Valuation is complete going in: every listed asset is oracle-priced and the
	// feed has been aggregating every block.
	f.nextBlock(func(ctx sdk.Context) {
		status, err := treasuryQueryServer.FundStatus(ctx, &treasurytypes.QueryFundStatusRequest{})
		require.NoError(t, err)
		require.True(t, valuationComplete(status))
	})

	// Both acts in one block: the committee suspends, then a NOAH offer expands,
	// and settlement runs at the end of it.
	//
	// This is the hazard the deferred design removes rather than manages. There
	// is no valuation taken before the suspension for the expansion to be placed
	// against: the only fold happens after both acts have landed, so the
	// suspension is simply seen, with nothing to invalidate and no ordering
	// within the block that could hide it.
	f.nextBlock(func(ctx sdk.Context) {
		_, err := assetMsgServer.EmergencySuspendAsset(ctx, &assettypes.MsgEmergencySuspendAsset{
			Committee:    committee,
			Denom:        chain.KRWBaseDenom,
			ExpectedTerm: term,
		})
		require.NoError(t, err)

		_, err = marketMsgServer.Swap(ctx, &markettypes.MsgSwap{
			Trader:         f.trader.String(),
			OfferCoin:      sdk.NewCoin(chain.NoahBaseDenom, chain.NativeBaseAmount(10)),
			AskDenom:       chain.USDBaseDenom,
			MinimumReceive: sdk.NewCoin(chain.USDBaseDenom, math.OneInt()),
		})
		require.NoError(t, err)
	})

	// Settlement must have seen the suspension: an incomplete valuation sizes
	// no targets, so nothing overflows and the whole eligible principal parks
	// in the Reserve.
	var allocations int
	for _, event := range f.blockEvents {
		if event.Type != "ark.treasury.v1.EventExpansionAllocated" {
			continue
		}
		allocations++
		for _, attribute := range event.Attributes {
			switch attribute.Key {
			case "overflow_burn":
				require.Equal(t, `"0"`, attribute.Value,
					"an incomplete valuation burns no principal")
			case "redemption_buffer_credit":
				require.Equal(t, `"0"`, attribute.Value,
					"an incomplete valuation commits nothing to the Buffer")
			}
		}
	}
	require.Equal(t, 1, allocations, "the block settled its expansions once")

	// One fold, one disclosure. A block whose valuation degraded inside it
	// reports that degradation exactly once, because the only valuation it
	// takes happens after everything that could degrade it.
	var disclosures int
	for _, event := range f.blockEvents {
		if event.Type != "ark.treasury.v1.EventLiabilityIncomplete" {
			continue
		}
		disclosures++
		for _, attribute := range event.Attributes {
			if attribute.Key == "untrusted_suspended_supply" {
				require.Contains(t, attribute.Value, chain.KRWBaseDenom,
					"the disclosure must name the suspended member")
			}
		}
	}
	require.Equal(t, 1, disclosures, "the block disclosed its degraded valuation once")
}
