package keeper

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	"github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// Settle burns a suspended asset for NOAH at its active governance settlement rate. This holder
// redemption is separate from swaps and cannot issue the settled asset.
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
		return sdk.Coin{}, errorsmod.Wrapf(
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
		return sdk.Coin{}, errorsmod.Wrap(types.ErrNoActiveSettlement, offerCoin.Denom)
	}

	// Value redeemed liability at the committed plan rate, never the suspended asset's Oracle rate.
	// Both quote NOAH per asset unit, with NOAH itself at one.
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
		return sdk.Coin{}, errorsmod.Wrapf(err, "quoting settlement redemption of %s", offerCoin)
	}
	// Truncation is the holder's cost, never the protocol's: a partial anoah
	// cannot be paid, and rounding it up would mint NOAH the plan never
	// committed to.
	entitlement := chain.NoahCoin(entitlementDec.Amount.TruncateInt())
	if !entitlement.IsPositive() {
		return sdk.Coin{}, errorsmod.Wrapf(
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
		return sdk.Coin{}, errorsmod.Wrapf(
			err,
			"sending settlement offer %s from trader %s to module",
			offerCoins,
			trader,
		)
	}

	// The entitlement is recorded at the plan's own committed rate, which never
	// appears in the oracle set, so this path values its own redemption rather
	// than leaving it to settlement.
	if err := k.recordRedemption(ctx, entitlementDec.Amount, entitlement.Amount); err != nil {
		return sdk.Coin{}, errorsmod.Wrapf(
			err,
			"recording settlement redemption of %s",
			offerCoin,
		)
	}

	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, offerCoins); err != nil {
		return sdk.Coin{}, errorsmod.Wrapf(err, "burning settled coins %s", offerCoin)
	}
	// The whole entitlement is minted here and the Buffer's share of it burned
	// back at settlement, so the holder always receives the whole entitlement:
	// the cost of an orderly failure lands as bounded NOAH dilution rather than
	// as a haircut on the exit, and the exit never waits on a valuation.
	if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, sdk.NewCoins(entitlement)); err != nil {
		return sdk.Coin{}, errorsmod.Wrapf(err, "minting settlement coins %s", entitlement)
	}

	if err := k.bankKeeper.SendCoinsFromModuleToAccount(
		ctx,
		types.ModuleName,
		trader,
		sdk.NewCoins(entitlement),
	); err != nil {
		return sdk.Coin{}, errorsmod.Wrapf(
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
