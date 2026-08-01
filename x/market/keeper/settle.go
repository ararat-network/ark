package keeper

import (
	"context"
	"fmt"

	sdkerrors "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	assettypes "ark/x/asset/types"
	"ark/x/market/types"
	oracletypes "ark/x/oracle/types"
)

// Settle redeems a suspended asset against its governance-approved settlement
// plan: asset to NOAH, at the plan rate, burning what it takes in.
//
// This is a separate path from conversion rather than a special case of it,
// because the two answer different questions. Conversion asks what the market
// says a denomination is worth; settlement asks what governance committed to
// pay holders of a denomination the market can no longer price honestly. Giving
// the plan rate its own entry point is what keeps it out of ordinary routing —
// nothing can reach it through a swap, and it can never mint the failed asset
// back into existence.
func (k Keeper) Settle(ctx context.Context, trader sdk.AccAddress, offerCoin sdk.Coin) (sdk.Coin, error) {
	if err := offerCoin.Validate(); err != nil {
		return sdk.Coin{}, fmt.Errorf("invalid settlement offer: %w", err)
	}
	if !offerCoin.IsPositive() {
		return sdk.Coin{}, fmt.Errorf("settlement offer must be positive: %s", offerCoin)
	}

	asset, err := k.assetKeeper.GetAsset(ctx, offerCoin.Denom)
	if err != nil {
		return sdk.Coin{}, err
	}
	// Settlement is the suspended holder's exit. Every other status either has
	// a better route out (conversion, while the asset is still priceable) or
	// none that governance has committed to.
	if asset.Status != assettypes.AssetStatus_ASSET_STATUS_SUSPENDED {
		return sdk.Coin{}, sdkerrors.Wrapf(
			types.ErrIneligibleAsset,
			"%s asset %s cannot be settled",
			asset.Status,
			offerCoin.Denom,
		)
	}

	plan, found, err := k.assetKeeper.ActiveSettlementPlan(ctx, offerCoin.Denom)
	if err != nil {
		return sdk.Coin{}, err
	}
	if !found {
		return sdk.Coin{}, sdkerrors.Wrap(types.ErrNoActiveSettlement, offerCoin.Denom)
	}

	// Treasury converts the redeemed asset through whatever rates it is handed,
	// so it is handed the plan's. The oracle rate is deliberately not read: a
	// suspended asset's market price is exactly what stopped being trustworthy,
	// and passing the committed rate is what makes the resulting liability
	// accounting settlement-priced rather than a fiction.
	//
	// The plan rate needs no adjustment to serve as a rate set: it is quoted in
	// units per one NOAH like every oracle rate, so it drops in beside the
	// numeraire carried at one.
	planRates := oracletypes.NewRateSet()
	planRates[plan.Denom] = plan.RedemptionRate

	// The entitlement is quoted through that same rate set, so the payout and
	// the liability Treasury records for it are one conversion, not two
	// roundings of the same rate.
	entitlementDec, err := planRates.Convert(
		sdk.NewDecCoinFromCoin(offerCoin),
		chain.NoahBaseDenom,
	)
	if err != nil {
		return sdk.Coin{}, sdkerrors.Wrapf(err, "quoting settlement redemption of %s", offerCoin)
	}
	// Truncation is the holder's cost, never the protocol's: a partial anoah
	// cannot be paid, and rounding it up would mint NOAH the plan never
	// committed to.
	entitlement := sdk.NewCoin(chain.NoahBaseDenom, entitlementDec.Amount.TruncateInt())
	if !entitlement.IsPositive() {
		return sdk.Coin{}, sdkerrors.Wrapf(
			types.ErrZeroSwapCoin,
			"settlement of %s at rate %s rounds to zero",
			offerCoin,
			plan.RedemptionRate,
		)
	}

	offerCoins := sdk.NewCoins(offerCoin)
	if err := k.bankKeeper.SendCoinsFromAccountToModule(
		ctx,
		trader,
		types.ModuleName,
		offerCoins,
	); err != nil {
		return sdk.Coin{}, sdkerrors.Wrapf(
			err,
			"sending settlement offer %s from trader %s to module",
			offerCoins,
			trader,
		)
	}

	draw, err := k.treasuryKeeper.DrawRedemptionBuffer(
		ctx,
		offerCoin,
		entitlement.Amount,
		planRates,
	)
	if err != nil {
		return sdk.Coin{}, sdkerrors.Wrapf(
			err,
			"drawing redemption buffer for settlement of %s",
			offerCoin,
		)
	}

	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, offerCoins); err != nil {
		return sdk.Coin{}, sdkerrors.Wrapf(err, "burning settled coins %s", offerCoin)
	}
	// The buffer covers what it can and the remainder is minted, so the holder
	// always receives the whole entitlement: the cost of an orderly failure
	// lands as bounded NOAH dilution rather than as a haircut on the exit.
	minted := sdk.NewCoin(chain.NoahBaseDenom, entitlement.Amount.Sub(draw.BufferPaid))
	if minted.IsPositive() {
		if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, sdk.NewCoins(minted)); err != nil {
			return sdk.Coin{}, sdkerrors.Wrapf(err, "minting settlement coins %s", minted)
		}
	}
	if err := k.treasuryKeeper.RecordSupplyChange(ctx, offerCoin, minted, planRates); err != nil {
		return sdk.Coin{}, sdkerrors.Wrapf(
			err,
			"recording settlement supply change from %s to %s",
			offerCoin,
			minted,
		)
	}

	if err := k.bankKeeper.SendCoinsFromModuleToAccount(
		ctx,
		types.ModuleName,
		trader,
		sdk.NewCoins(entitlement),
	); err != nil {
		return sdk.Coin{}, sdkerrors.Wrapf(
			err,
			"sending settlement entitlement %s to trader %s",
			entitlement,
			trader,
		)
	}

	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventSettle{
		Trader:         trader.String(),
		OfferDenom:     offerCoin.Denom,
		OfferAmount:    offerCoin.Amount,
		RedeemedAmount: entitlement.Amount,
		RedemptionRate: plan.RedemptionRate,
	}); err != nil {
		return sdk.Coin{}, fmt.Errorf("emitting Market settlement event: %w", err)
	}

	return entitlement, nil
}
