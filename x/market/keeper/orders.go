// SPDX-License-Identifier: Apache-2.0
// Originates from Ark's Terra Classic port of x/market/keeper/swap.go.
// Modified for Ark: conversion quoting, eligibility, and settlement.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

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

// Sell converts a stated offer for trader and returns the output and the spread it paid. An
// optional positive ask-denom minimum is checked before booking; a crossed floor costs only
// transaction gas.
func (k Keeper) Sell(ctx context.Context, trader sdk.AccAddress, offer sdk.Coin, askDenom string, minimumReceive sdk.Coin) (sdk.Coin, sdk.DecCoin, error) {
	hasFloor, err := validateBound(minimumReceive, askDenom, false)
	if err != nil {
		return sdk.Coin{}, sdk.DecCoin{}, errorsmod.Wrap(err, "minimum receive")
	}
	q, err := k.priceSell(ctx, offer, askDenom)
	if err != nil {
		return sdk.Coin{}, sdk.DecCoin{}, errorsmod.Wrapf(err, "computing swap from %s to %s", offer, askDenom)
	}
	if hasFloor && q.output.Amount.LT(minimumReceive.Amount) {
		return sdk.Coin{}, sdk.DecCoin{}, errorsmod.Wrapf(types.ErrMinimumReceiveNotMet, "minimum %s, received %s", minimumReceive, q.output)
	}
	if err := k.fill(ctx, trader, q); err != nil {
		return sdk.Coin{}, sdk.DecCoin{}, err
	}
	return q.output, q.fee, nil
}

// Buy converts the least offer of offerDenom whose quote returns at least ask, back to the trader.
// A buy states no spend of its own, so the maximum is what bounds the trader's loss and it is
// required. The ask is the swap's floor.
func (k Keeper) Buy(ctx context.Context, trader sdk.AccAddress, ask sdk.Coin, offerDenom string, maximumOffer sdk.Coin) (sdk.Coin, sdk.Coin, sdk.DecCoin, error) {
	if _, err := validateBound(maximumOffer, offerDenom, true); err != nil {
		return sdk.Coin{}, sdk.Coin{}, sdk.DecCoin{}, errorsmod.Wrap(err, "maximum offer")
	}
	q, err := k.priceBuy(ctx, ask, offerDenom)
	if err != nil {
		return sdk.Coin{}, sdk.Coin{}, sdk.DecCoin{}, err
	}
	if q.offer.Amount.GT(maximumOffer.Amount) {
		return sdk.Coin{}, sdk.Coin{}, sdk.DecCoin{}, errorsmod.Wrapf(types.ErrMaximumOfferExceeded, "maximum %s, needed %s", maximumOffer, q.offer)
	}
	if err := k.fill(ctx, trader, q); err != nil {
		return sdk.Coin{}, sdk.Coin{}, sdk.DecCoin{}, err
	}
	return q.offer, q.output, q.fee, nil
}

// Settle burns a suspended asset for NOAH at its active governance settlement rate. This holder
// redemption is separate from swaps and cannot issue the settled asset.
func (k Keeper) Settle(ctx context.Context, trader sdk.AccAddress, offer sdk.Coin) (sdk.Coin, error) {
	if err := offer.Validate(); err != nil {
		return sdk.Coin{}, errorsmod.Wrapf(types.ErrInvalidOrder, "invalid settlement offer: %v", err)
	}
	if !offer.IsPositive() {
		return sdk.Coin{}, errorsmod.Wrapf(types.ErrInvalidOrder, "settlement offer %s is not positive", offer)
	}

	asset, err := k.assetKeeper.GetAsset(ctx, offer.Denom)
	if err != nil {
		return sdk.Coin{}, err
	}
	// Settlement is the suspended holder's exit. Every other status either has a better route out
	// (conversion, while the asset is still priceable) or none that governance has committed to.
	if asset.Status != assettypes.AssetStatus_ASSET_STATUS_SUSPENDED {
		return sdk.Coin{}, errorsmod.Wrapf(types.ErrIneligibleAsset, "%s asset %s cannot be settled", asset.Status, offer.Denom)
	}

	plan, found, err := k.assetKeeper.ActiveSettlementPlan(ctx, offer.Denom)
	if err != nil {
		return sdk.Coin{}, err
	}
	if !found {
		return sdk.Coin{}, errorsmod.Wrap(types.ErrNoActiveSettlement, offer.Denom)
	}

	// Value redeemed liability at the committed plan rate, never the suspended asset's Oracle rate.
	// The entitlement and the liability Treasury records for it are one conversion through that
	// rate, not two roundings of it.
	planRates := oracletypes.NewRateSet()
	planRates[plan.Denom] = plan.RedemptionRate
	entitlementDec, err := planRates.Convert(sdk.NewDecCoinFromCoin(offer), chain.NoahBaseDenom)
	if err != nil {
		return sdk.Coin{}, errorsmod.Wrapf(err, "quoting settlement redemption of %s", offer)
	}
	// Truncation is the holder's cost, never the protocol's: a partial anoah cannot be paid, and
	// rounding it up would mint NOAH the plan never committed to.
	entitlement := chain.NoahCoin(entitlementDec.Amount.TruncateInt())
	if !entitlement.IsPositive() {
		return sdk.Coin{}, errorsmod.Wrapf(types.ErrZeroSwapCoin, "settlement of %s at rate %s rounds to zero", offer, plan.RedemptionRate)
	}

	// The whole entitlement is minted here and the Buffer's share of it burned back at block
	// settlement, so the holder always receives the whole entitlement: the cost of an orderly
	// failure lands as bounded NOAH dilution rather than as a haircut on the exit, and the exit
	// never waits on a valuation.
	q := quote{offer: offer, output: entitlement, kind: planRedemption, value: entitlementDec.Amount}
	if err := k.book(ctx, trader, q); err != nil {
		return sdk.Coin{}, err
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventSettle{
		Trader:         trader.String(),
		OfferDenom:     offer.Denom,
		OfferAmount:    offer.Amount,
		RedeemedAmount: entitlement.Amount,
		RedemptionRate: plan.RedemptionRate,
	}); err != nil {
		return sdk.Coin{}, fmt.Errorf("emitting Market settlement event: %w", err)
	}
	return entitlement, nil
}

// priceSell validates a sell's legs, reads the pair, and prices the offer.
func (k Keeper) priceSell(ctx context.Context, offer sdk.Coin, askDenom string) (quote, error) {
	if err := validateLegs(offer, askDenom); err != nil {
		return quote{}, err
	}
	s, err := k.snapshot(ctx, offer.Denom, askDenom)
	if err != nil {
		return quote{}, err
	}
	return s.sell(offer)
}

// priceBuy validates a buy's legs, reads the pair, and sizes the offer.
func (k Keeper) priceBuy(ctx context.Context, ask sdk.Coin, offerDenom string) (quote, error) {
	if err := validateLegs(ask, offerDenom); err != nil {
		return quote{}, err
	}
	s, err := k.snapshot(ctx, offerDenom, ask.Denom)
	if err != nil {
		return quote{}, err
	}
	return s.buy(ask)
}

// fill books a quote for trader and emits the swap event: the tail every order shares.
func (k Keeper) fill(ctx context.Context, trader sdk.AccAddress, q quote) error {
	if err := k.book(ctx, trader, q); err != nil {
		return err
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventSwap{
		Trader:      trader.String(),
		OfferDenom:  q.offer.Denom,
		OfferAmount: q.offer.Amount,
		SwapDenom:   q.output.Denom,
		SwapAmount:  q.output.Amount,
		FeeDenom:    q.fee.Denom,
		FeeAmount:   q.fee.Amount,
	}); err != nil {
		return fmt.Errorf("emitting Market swap event: %w", err)
	}
	return nil
}

// validateLegs checks an order's two legs: the stated coin, the other denomination, and that they
// differ.
func validateLegs(coin sdk.Coin, denom string) error {
	if err := coin.Validate(); err != nil {
		return errorsmod.Wrapf(types.ErrInvalidOrder, "invalid coin: %v", err)
	}
	if !coin.Amount.IsPositive() {
		return errorsmod.Wrap(types.ErrInvalidOrder, coin.String())
	}
	if err := sdk.ValidateDenom(denom); err != nil {
		return errorsmod.Wrapf(types.ErrInvalidOrder, "invalid denom %q: %v", denom, err)
	}
	if coin.Denom == denom {
		return errorsmod.Wrap(types.ErrRecursiveSwap, denom)
	}
	return nil
}

// validateBound checks an order's bound, the floor a sell must clear or the cap a buy must stay within.
// The empty coin is no bound where one is optional, in either of its encodings, a nil or a zero
// amount; a stated bound is positive and in denom.
func validateBound(bound sdk.Coin, denom string, required bool) (bool, error) {
	if bound.Denom == "" && (bound.Amount.IsNil() || bound.Amount.IsZero()) {
		if required {
			return false, errorsmod.Wrap(types.ErrInvalidOrder, "not stated")
		}
		return false, nil
	}
	if err := bound.Validate(); err != nil {
		return false, errorsmod.Wrap(types.ErrInvalidOrder, err.Error())
	}
	if !bound.IsPositive() {
		return false, errorsmod.Wrapf(types.ErrInvalidOrder, "%s is not positive", bound)
	}
	if bound.Denom != denom {
		return false, errorsmod.Wrapf(types.ErrInvalidOrder, "denom %q is not %q", bound.Denom, denom)
	}
	return true, nil
}
