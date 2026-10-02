// SPDX-License-Identifier: Apache-2.0
// Originates from Ark's Terra Classic port of x/market/keeper/swap.go.
// Modified for Ark: conversion quoting, eligibility, and settlement.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package keeper

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/decimal"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	"github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// maxOfferAdjustments bounds the confirming quotes a buy spends absorbing truncation.
const maxOfferAdjustments = 8

// snapshot is one pair's price at the current state, read once per order: the rates every
// conversion of the pair is valued at, the floor its spread never drops below, and, for a NOAH
// pair, the pool its slippage is drawn from. The forward quote and its inverse are methods on it,
// so they read the same state and sit beside each other.
type snapshot struct {
	offerDenom string
	askDenom   string
	rates      oracletypes.RateSet
	// floor is the larger Tobin tax of a stable pair's legs, or a NOAH pair's stability floor.
	floor math.LegacyDec
	// pool is nil for a stable pair, which moves no pool.
	pool *pool
}

// pool is a NOAH pair's constant-product pool in base units, oriented to the trade: the offer
// joins in and the ask leaves out.
type pool struct {
	base         string
	depth        math.LegacyDec
	delta        math.LegacyDec
	in           math.LegacyDec
	out          math.LegacyDec
	k            math.LegacyDec
	offeringNoah bool
}

// kind is what a conversion does to supply.
type kind int

const (
	// cross burns a stablecoin offer and mints a stablecoin output.
	cross kind = iota
	// expansion retains a NOAH offer for block settlement and mints the stablecoin output.
	expansion
	// redemption burns a stablecoin offer and mints the NOAH output, valued at the quote's rates.
	redemption
	// planRedemption is a redemption valued at a settlement plan's committed rate.
	planRedemption
)

// kindOf classifies a pair by where NOAH sits.
func kindOf(offerDenom, askDenom string) kind {
	switch chain.NoahBaseDenom {
	case offerDenom:
		return expansion
	case askDenom:
		return redemption
	}
	return cross
}

// quote is one priced order: what moves, the spread paid, and what booking it needs.
// It is never persisted.
type quote struct {
	offer  sdk.Coin
	output sdk.Coin
	fee    sdk.DecCoin
	kind   kind
	// value is the NOAH value of the leg block settlement accounts for, the stablecoin an expansion
	// issues or the supply a redemption retires, at the rate that priced the quote: Treasury values
	// the same swap for seigniorage and liability accounting, and a second read could differ.
	value math.LegacyDec
	// delta is the pool gap the swap leaves behind, already validated against the effective pools.
	// It is nil for a stablecoin pair, which moves no pool at all.
	delta math.LegacyDec
}

// snapshot reads a pair's price. Eligibility precedes every rate read: a rate exists for any active
// feed, including one whose asset is suspended or not yet listed, so status is what decides
// convertibility, never the presence of a price.
func (k Keeper) snapshot(ctx context.Context, offerDenom, askDenom string) (snapshot, error) {
	if err := k.requireConvertible(ctx, offerDenom, askDenom); err != nil {
		return snapshot{}, err
	}
	s := snapshot{offerDenom: offerDenom, askDenom: askDenom}
	if kindOf(offerDenom, askDenom) == cross {
		rates, err := k.oracleKeeper.GetRateSet(ctx, offerDenom, askDenom)
		if err != nil {
			return snapshot{}, err
		}

		// Each leg contributes its own rate and the wider one governs: the spread has to cover
		// whichever side carries more oracle-staleness risk, so an override on one denomination
		// protects both directions of every pair it trades in.
		taxes, err := k.GetTobinTaxes(ctx, offerDenom, askDenom)
		if err != nil {
			return snapshot{}, err
		}

		s.rates = rates
		s.floor = math.LegacyMaxDec(taxes[askDenom], taxes[offerDenom])

		return s, nil
	}

	policy, err := k.ConversionPolicy.Get(ctx)
	if err != nil {
		return snapshot{}, fmt.Errorf("getting conversion policy: %w", err)
	}
	delta, err := k.ArkPoolDelta.Get(ctx)
	if err != nil {
		return snapshot{}, fmt.Errorf("getting ArkPoolDelta: %w", err)
	}

	rates, err := k.oracleKeeper.GetRateSet(ctx, offerDenom, policy.BasePool.Denom, askDenom)
	if err != nil {
		return snapshot{}, err
	}

	pools, err := types.NewEffectivePools(policy.BasePool.Amount, delta)
	if err != nil {
		return snapshot{}, errorsmod.Wrapf(types.ErrArithmeticOutOfRange, "constructing effective pools: %v", err)
	}
	p := &pool{
		base:         policy.BasePool.Denom,
		depth:        policy.BasePool.Amount,
		delta:        delta,
		in:           pools.ArkPool,
		out:          pools.NoahPool,
		k:            pools.ConstantProduct,
		offeringNoah: offerDenom == chain.NoahBaseDenom,
	}
	if p.offeringNoah {
		p.in, p.out = pools.NoahPool, pools.ArkPool
	}

	s.rates = rates
	s.floor = policy.MinStabilitySpread
	s.pool = p

	return s, nil
}

// sell prices an offer: its gross value at the pair's rates less the spread its size incurs. A
// stable pair charges the floor alone; a NOAH pair the larger of the floor and the pool's
// slippage, and reports the gap it leaves behind. Truncation dust joins the fee: the protocol
// keeps what it cannot pay out in whole base units.
func (s snapshot) sell(offer sdk.Coin) (quote, error) {
	offerDec := sdk.NewDecCoinFromCoin(offer)
	gross, err := s.rates.Convert(offerDec, s.askDenom)
	if err != nil {
		return quote{}, err
	}

	spread := s.floor
	var x math.LegacyDec
	if s.pool != nil {
		base, err := s.rates.Convert(offerDec, s.pool.base)
		if err != nil {
			return quote{}, err
		}
		x = base.Amount
		slippage, err := s.pool.slippage(x)
		if err != nil {
			return quote{}, err
		}
		spread = math.LegacyMaxDec(s.floor, slippage)
	}

	// The spread is at most one, so nothing here exceeds gross, and a valid decimal always fits an
	// Int.
	fee := spread.Mul(gross.Amount)
	net := sdk.DecCoin{Denom: s.askDenom, Amount: gross.Amount.Sub(fee)}
	output, dust := net.TruncateDecimal()
	if !output.IsPositive() {
		return quote{}, types.ErrZeroSwapCoin
	}

	q := quote{
		offer:  offer,
		output: output,
		kind:   kindOf(offer.Denom, s.askDenom),
		fee:    sdk.NewDecCoinFromDec(s.askDenom, fee.Add(dust.Amount)),
	}

	switch q.kind {
	case expansion:
		issued, err := s.rates.Convert(sdk.NewDecCoinFromCoin(q.output), chain.NoahBaseDenom)
		if err != nil {
			return quote{}, errorsmod.Wrapf(err, "valuing issued output %s", output)
		}
		q.value = issued.Amount
	case redemption:
		retired, err := s.rates.Convert(offerDec, chain.NoahBaseDenom)
		if err != nil {
			return quote{}, errorsmod.Wrapf(err, "valuing redeemed offer %s", offer)
		}
		q.value = retired.Amount
	case cross, planRedemption:
	}

	if s.pool != nil {
		if q.delta, err = s.gap(x, net); err != nil {
			return quote{}, err
		}
	}

	return q, nil
}

// buy sizes the least offer whose quote returns at least ask and returns that quote: sell inverted
// and ceiled, since an offer sizes a requirement, then confirmed forward. A stable pair divides
// the ask's gross value by what the floor leaves. A NOAH pair's output is the smaller of two
// curves, so its offer is the larger of their inverses, the floor's and the pool's. A confirmation
// left short by truncation bumps the offer by what its own rate says is missing.
func (s snapshot) buy(ask sdk.Coin) (quote, error) {
	askDec := sdk.NewDecCoinFromCoin(ask)
	gross, err := s.rates.Convert(askDec, s.offerDenom)
	if err != nil {
		return quote{}, err
	}

	offer, err := decimal.Quo(gross.Amount, math.LegacyOneDec().Sub(s.floor))
	if err != nil {
		return quote{}, err
	}

	if s.pool != nil {
		y, err := s.rates.Convert(askDec, s.pool.base)
		if err != nil {
			return quote{}, err
		}
		x, err := s.pool.offer(y.Amount)
		if err != nil {
			return quote{}, err
		}
		fromPool, err := s.rates.Convert(sdk.NewDecCoinFromDec(s.pool.base, x), s.offerDenom)
		if err != nil {
			return quote{}, err
		}
		offer = math.LegacyMaxDec(offer, fromPool.Amount)
	}

	// A valid offer is below 2^256, so its integer part always fits an Int, but its ceiling can be
	// 2^256 itself: the checked add is the one place the ceiling can overflow.
	amount := offer.TruncateInt()
	if offer.GT(math.LegacyNewDecFromInt(amount)) {
		if amount, err = amount.SafeAdd(math.OneInt()); err != nil {
			return quote{}, errorsmod.Wrapf(types.ErrArithmeticOutOfRange, "sizing the %s offer: %v", s.offerDenom, err)
		}
	}
	for range maxOfferAdjustments {
		q, err := s.sell(sdk.NewCoin(s.offerDenom, amount))
		if err != nil {
			return quote{}, err
		}

		if !q.output.Amount.LT(ask.Amount) {
			return q, nil
		}

		// The output is below the ask here, so the shortfall is bounded by the ask.
		short, err := amount.SafeMul(ask.Amount.Sub(q.output.Amount))
		if err != nil {
			return quote{}, err
		}
		if amount, err = amount.SafeAdd(ceilDiv(short, q.output.Amount).AddRaw(1)); err != nil {
			return quote{}, err
		}
	}

	return quote{}, fmt.Errorf("sizing a %s offer for %s did not converge", s.offerDenom, ask)
}

// slippage is the spread the pool charges an offer of x base units: the shortfall of what it hands
// over against a frictionless fill, as a share of x. A fill that beats frictionless charges none.
func (p pool) slippage(x math.LegacyDec) (math.LegacyDec, error) {
	in, err := decimal.Add(p.in, x)
	if err != nil {
		return math.LegacyDec{}, errorsmod.Wrapf(types.ErrArithmeticOutOfRange, "adding the offer amount to the effective pool: %v", err)
	}

	// in is at least the pool's own side, so the quotient is at most the other side and the
	// difference is non-negative.
	out := p.out.Sub(p.k.Quo(in))
	if !out.LT(x) {
		return math.LegacyZeroDec(), nil
	}
	// Reached only when out is below x, so x is positive and the ratio lies in (0, 1].
	return x.Sub(out).Quo(x), nil
}

// offer inverts slippage: the base units that make the pool hand over y, y·in/(out − y). The whole
// out side is unreachable.
func (p pool) offer(y math.LegacyDec) (math.LegacyDec, error) {
	if !y.LT(p.out) {
		return math.LegacyDec{}, errorsmod.Wrapf(types.ErrArithmeticOutOfRange, "ask of %s base units is not below the %s effective pool", y, p.out)
	}

	numerator, err := decimal.Mul(y, p.in)
	if err != nil {
		return math.LegacyDec{}, errorsmod.Wrapf(types.ErrArithmeticOutOfRange, "sizing the pool offer: %v", err)
	}

	remaining, err := decimal.Sub(p.out, y)
	if err != nil {
		return math.LegacyDec{}, errorsmod.Wrapf(types.ErrArithmeticOutOfRange, "sizing the pool offer: %v", err)
	}

	x, err := decimal.Quo(numerator, remaining)
	if err != nil {
		return math.LegacyDec{}, errorsmod.Wrapf(types.ErrArithmeticOutOfRange, "sizing the pool offer: %v", err)
	}

	return x, nil
}

// gap is the pool gap a NOAH-pair swap leaves behind. The pool absorbs the swap in base units, in
// whichever direction NOAH moved; the ask side is booked from the post-fee decimal rather than the
// truncated coin, since the fee stays with the protocol and the pool must see the amount actually
// leaving it.
func (s snapshot) gap(x math.LegacyDec, net sdk.DecCoin) (math.LegacyDec, error) {
	p := s.pool
	var gap math.LegacyDec
	if p.offeringNoah {
		leaving, err := s.rates.Convert(net, p.base)
		if err != nil {
			return math.LegacyDec{}, err
		}

		if gap, err = decimal.Sub(p.delta, leaving.Amount); err != nil {
			return math.LegacyDec{}, errorsmod.Wrapf(types.ErrArithmeticOutOfRange, "subtracting the ask amount from the ark pool delta: %v", err)
		}
	} else {
		var err error
		if gap, err = decimal.Add(p.delta, x); err != nil {
			return math.LegacyDec{}, errorsmod.Wrapf(types.ErrArithmeticOutOfRange, "adding the offer amount to the ark pool delta: %v", err)
		}
	}

	if _, err := types.NewEffectivePools(p.depth, gap); err != nil {
		return math.LegacyDec{}, errorsmod.Wrapf(types.ErrArithmeticOutOfRange, "validating the updated effective pools: %v", err)
	}

	return gap, nil
}

// ceilDiv rounds a quotient up: an offer sizes a requirement. The denominator is a quote's output,
// which sell keeps positive, and a sized offer is far too small for the quotient to reach the
// integer bound.
func ceilDiv(numerator, denominator math.Int) math.Int {
	quotient := numerator.Quo(denominator)
	if !numerator.Mod(denominator).IsZero() {
		quotient = quotient.AddRaw(1)
	}
	return quotient
}

// requireConvertible checks status before any rate read: offers may be ACTIVE or ISSUANCE_HALTED,
// asks must be ACTIVE, and NOAH needs no registry entry. This keeps suspended assets and settlement
// rates outside ordinary swaps. See x/market/README.md.
func (k Keeper) requireConvertible(ctx context.Context, offerDenom, askDenom string) error {
	if offerDenom != chain.NoahBaseDenom {
		offerAsset, err := k.assetKeeper.GetAsset(ctx, offerDenom)
		if err != nil {
			return err
		}
		if !offerAsset.IsOraclePriced() {
			return errorsmod.Wrapf(
				types.ErrIneligibleAsset,
				"%s asset %s cannot be offered for conversion",
				offerAsset.Status,
				offerDenom,
			)
		}
	}
	if askDenom != chain.NoahBaseDenom {
		askAsset, err := k.assetKeeper.GetAsset(ctx, askDenom)
		if err != nil {
			return err
		}
		if askAsset.Status != assettypes.AssetStatus_ASSET_STATUS_ACTIVE {
			return errorsmod.Wrapf(
				types.ErrIneligibleAsset,
				"%s asset %s cannot be produced by conversion",
				askAsset.Status,
				askDenom,
			)
		}
	}

	return nil
}
