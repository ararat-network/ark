package keeper

import (
	"context"
	"errors"
	"fmt"

	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	chain "github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/decimal"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	"github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// swapQuote is the complete answer to one conversion: what the trader receives,
// what it costs, the rates it was priced at, and the pool state it leaves
// behind. Settlement executes this and computes nothing further.
//
// It is execution-local and is never persisted.
type swapQuote struct {
	swapCoin sdk.Coin
	swapFee  sdk.DecCoin
	// rates priced this quote and must price its settlement too: Treasury values
	// the same swap for seigniorage and liability accounting, and a second read
	// could differ.
	rates oracletypes.RateSet
	// updatedArkPoolDelta is the pool gap this swap leaves behind, already
	// validated against the effective pools. It is nil for a stablecoin pair,
	// which moves no pool at all.
	updatedArkPoolDelta math.LegacyDec
}

// Swap quotes and settles one conversion, delivering the output to receiver.
// It is the only way to execute a conversion: quoting and settling are split
// internally, but nothing outside this method may hold a quote and settle it,
// which is what keeps the eligibility gates in quoteSwap unskippable.
//
// A zero-valued minimumReceive means the trader set no floor. A set one must be
// denominated in the ask denom and is enforced between quote and settlement, so
// a crossed floor costs the trader nothing but gas.
func (k Keeper) Swap(
	ctx context.Context,
	trader sdk.AccAddress,
	receiver sdk.AccAddress,
	offerCoin sdk.Coin,
	askDenom string,
	minimumReceive sdk.Coin,
) (sdk.Coin, sdk.DecCoin, error) {
	hasFloor, err := validateMinimumReceive(minimumReceive, askDenom)
	if err != nil {
		return sdk.Coin{}, sdk.DecCoin{}, err
	}

	quote, err := k.quoteSwap(ctx, offerCoin, askDenom)
	if err != nil {
		return sdk.Coin{}, sdk.DecCoin{}, sdkerrors.Wrapf(
			err,
			"computing swap from %s to %s",
			offerCoin,
			askDenom,
		)
	}

	if hasFloor && quote.swapCoin.Amount.LT(minimumReceive.Amount) {
		return sdk.Coin{}, sdk.DecCoin{}, sdkerrors.Wrapf(
			types.ErrMinimumReceiveNotMet,
			"minimum %s, received %s",
			minimumReceive,
			quote.swapCoin,
		)
	}

	if err := k.settleSwap(ctx, trader, receiver, offerCoin, quote); err != nil {
		return sdk.Coin{}, sdk.DecCoin{}, err
	}

	return quote.swapCoin, quote.swapFee, nil
}

// validateMinimumReceive reports whether the trader set an output floor.
//
// The floor is optional: the zero coin means the trader accepts market
// execution, which is a legitimate intent with exactly one spelling — absence.
// A denominated zero stays invalid rather than reading as a second way to opt
// out, because a floor of nothing is a computed value of zero protection,
// which is far more likely a caller bug than a choice. When a floor is set it
// must be positive and in the ask denomination: a floor in any other unit
// would compare unlike quantities and enforce nothing.
func validateMinimumReceive(minimumReceive sdk.Coin, askDenom string) (bool, error) {
	if minimumReceive.Denom == "" && (minimumReceive.Amount.IsNil() || minimumReceive.Amount.IsZero()) {
		return false, nil
	}
	if err := minimumReceive.Validate(); err != nil {
		return false, sdkerrors.Wrapf(errortypes.ErrInvalidCoins, "invalid minimum receive: %v", err)
	}
	if !minimumReceive.IsPositive() {
		return false, sdkerrors.Wrap(errortypes.ErrInvalidCoins, minimumReceive.String())
	}
	if minimumReceive.Denom != askDenom {
		return false, sdkerrors.Wrapf(
			errortypes.ErrInvalidRequest,
			"minimum receive denom %q does not match ask denom %q",
			minimumReceive.Denom,
			askDenom,
		)
	}

	return true, nil
}

func (k Keeper) quoteSwap(ctx context.Context, offerCoin sdk.Coin, askDenom string) (swapQuote, error) {
	if err := offerCoin.Validate(); err != nil {
		return swapQuote{}, sdkerrors.Wrapf(errortypes.ErrInvalidCoins, "invalid offer coin: %v", err)
	}
	if err := sdk.ValidateDenom(askDenom); err != nil {
		return swapQuote{}, sdkerrors.Wrapf(errortypes.ErrInvalidRequest, "invalid ask denom %q: %v", askDenom, err)
	}
	if offerCoin.Amount.LTE(math.ZeroInt()) {
		return swapQuote{}, sdkerrors.Wrap(errortypes.ErrInvalidCoins, offerCoin.String())
	}
	if offerCoin.Denom == askDenom {
		return swapQuote{}, sdkerrors.Wrap(types.ErrRecursiveSwap, askDenom)
	}
	// Eligibility precedes every rate read. A rate exists for any active feed,
	// including one whose asset is suspended or not yet listed, so status is
	// what decides convertibility — never the presence of a price.
	if err := k.requireConvertible(ctx, offerCoin.Denom, askDenom); err != nil {
		return swapQuote{}, err
	}
	offerDecCoin := sdk.NewDecCoinFromCoin(offerCoin)
	if offerCoin.Denom != chain.NoahBaseDenom && askDenom != chain.NoahBaseDenom {
		return k.quoteStablePair(ctx, offerDecCoin, askDenom)
	}

	return k.quoteNoahPair(ctx, offerDecCoin, askDenom)
}

// quoteStablePair prices a stablecoin-to-stablecoin conversion at oracle cross
// rates. No pool moves, so the quote carries no pool delta and the spread is
// the Tobin tax alone.
func (k Keeper) quoteStablePair(ctx context.Context, offerDecCoin sdk.DecCoin, askDenom string) (swapQuote, error) {
	rates, err := k.oracleKeeper.GetRateSet(ctx, offerDecCoin.Denom, askDenom)
	if err != nil {
		return swapQuote{}, marketRateError(err)
	}
	grossDecCoin, err := rates.Convert(offerDecCoin, askDenom)
	if err != nil {
		return swapQuote{}, err
	}
	// Each leg contributes its own rate and the wider one governs: the spread
	// has to cover whichever side carries more oracle-staleness risk, so an
	// override on one denomination protects both directions of every pair it
	// trades in.
	offerTobinTax, err := k.GetTobinTax(ctx, offerDecCoin.Denom)
	if err != nil {
		return swapQuote{}, err
	}
	askTobinTax, err := k.GetTobinTax(ctx, askDenom)
	if err != nil {
		return swapQuote{}, err
	}

	swapCoin, _, swapFee, err := applySpread(grossDecCoin, math.LegacyMaxDec(askTobinTax, offerTobinTax))
	if err != nil {
		return swapQuote{}, err
	}

	return swapQuote{swapCoin: swapCoin, swapFee: swapFee, rates: rates}, nil
}

// quoteNoahPair prices either direction of a NOAH pair against the virtual
// constant-product pools, and returns the pool gap the swap leaves behind.
//
// The spread is what the trade costs the pool: the offer is expressed in
// base-pool units, run through the constant product, and the shortfall against
// a frictionless fill becomes the spread, floored at the policy's
// MinStabilitySpread. Depth, recovery, and that floor are one policy object, so
// pricing a NOAH pair reads conversion state and nothing else.
func (k Keeper) quoteNoahPair(ctx context.Context, offerDecCoin sdk.DecCoin, askDenom string) (swapQuote, error) {
	capacity, err := k.ConversionPolicy.Get(ctx)
	if err != nil {
		return swapQuote{}, fmt.Errorf("getting conversion policy: %w", err)
	}
	arkPoolDelta, err := k.ArkPoolDelta.Get(ctx)
	if err != nil {
		return swapQuote{}, fmt.Errorf("getting ArkPoolDelta: %w", err)
	}
	rates, err := k.oracleKeeper.GetRateSet(
		ctx,
		offerDecCoin.Denom,
		capacity.BasePool.Denom,
		askDenom,
	)
	if err != nil {
		return swapQuote{}, marketRateError(err)
	}
	baseOfferDecCoin, err := rates.Convert(offerDecCoin, capacity.BasePool.Denom)
	if err != nil {
		return swapQuote{}, err
	}
	grossDecCoin, err := rates.Convert(offerDecCoin, askDenom)
	if err != nil {
		return swapQuote{}, err
	}

	pools, err := types.NewEffectivePools(capacity.BasePool.Amount, arkPoolDelta)
	if err != nil {
		return swapQuote{}, sdkerrors.Wrapf(
			types.ErrArithmeticOutOfRange,
			"constructing effective pools: %v",
			err,
		)
	}

	offeringNoah := offerDecCoin.Denom == chain.NoahBaseDenom
	offerPool, askPool := pools.ArkPool, pools.NoahPool
	if offeringNoah {
		offerPool, askPool = pools.NoahPool, pools.ArkPool
	}

	// Express the offer in base-pool units before calculating the constant-product spread.
	baseOfferAmount := baseOfferDecCoin.Amount
	updatedOfferPool, err := decimal.Add(offerPool, baseOfferAmount)
	if err != nil {
		return swapQuote{}, sdkerrors.Wrapf(
			types.ErrArithmeticOutOfRange,
			"adding the offer amount to the effective pool: %v",
			err,
		)
	}
	remainingAskPool := pools.ConstantProduct.Quo(updatedOfferPool)
	askBaseAmount := askPool.Sub(remainingAskPool)

	spread := capacity.MinStabilitySpread
	if askBaseAmount.LT(baseOfferAmount) {
		rawSpread := baseOfferAmount.Sub(askBaseAmount).Quo(baseOfferAmount)
		spread = math.LegacyMaxDec(capacity.MinStabilitySpread, rawSpread)
	}

	swapCoin, postFeeDecCoin, swapFee, err := applySpread(grossDecCoin, spread)
	if err != nil {
		return swapQuote{}, err
	}

	// The pool absorbs the swap in base-pool units, in whichever direction NOAH
	// moved. The ask side settles from the post-fee decimal rather than the
	// truncated coin: the fee stays with the protocol, so the pool must see the
	// amount actually leaving it.
	updatedArkPoolDelta := arkPoolDelta
	if offeringNoah {
		askBaseCoin, err := rates.Convert(postFeeDecCoin, capacity.BasePool.Denom)
		if err != nil {
			return swapQuote{}, err
		}
		updatedArkPoolDelta, err = decimal.Sub(updatedArkPoolDelta, askBaseCoin.Amount)
		if err != nil {
			return swapQuote{}, sdkerrors.Wrapf(
				types.ErrArithmeticOutOfRange,
				"subtracting the ask amount from the ark pool delta: %v",
				err,
			)
		}
	} else {
		updatedArkPoolDelta, err = decimal.Add(updatedArkPoolDelta, baseOfferAmount)
		if err != nil {
			return swapQuote{}, sdkerrors.Wrapf(
				types.ErrArithmeticOutOfRange,
				"adding the offer amount to the ark pool delta: %v",
				err,
			)
		}
	}
	if _, err := types.NewEffectivePools(capacity.BasePool.Amount, updatedArkPoolDelta); err != nil {
		return swapQuote{}, sdkerrors.Wrapf(
			types.ErrArithmeticOutOfRange,
			"validating the updated effective pools: %v",
			err,
		)
	}

	return swapQuote{
		swapCoin:            swapCoin,
		swapFee:             swapFee,
		rates:               rates,
		updatedArkPoolDelta: updatedArkPoolDelta,
	}, nil
}

// applySpread charges the spread against a gross output and returns the payable
// coin, the post-fee decimal the pools settle from, and the fee. Truncation
// dust joins the fee rather than the payout: the protocol keeps the remainder
// it cannot pay out in whole base units.
func applySpread(grossDecCoin sdk.DecCoin, spread math.LegacyDec) (sdk.Coin, sdk.DecCoin, sdk.DecCoin, error) {
	feeAmount := spread.Mul(grossDecCoin.Amount)
	postFeeDecCoin := grossDecCoin
	postFeeDecCoin.Amount = grossDecCoin.Amount.Sub(feeAmount)
	swapCoin, dust := postFeeDecCoin.TruncateDecimal()
	if !swapCoin.IsPositive() {
		return sdk.Coin{}, sdk.DecCoin{}, sdk.DecCoin{}, types.ErrZeroSwapCoin
	}

	return swapCoin,
		postFeeDecCoin,
		sdk.NewDecCoinFromDec(postFeeDecCoin.Denom, feeAmount.Add(dust.Amount)),
		nil
}

// settleSwap applies pool changes, moves funds through the module account, and emits swap events.
func (k Keeper) settleSwap(
	ctx context.Context,
	trader sdk.AccAddress,
	receiver sdk.AccAddress,
	offerCoin sdk.Coin,
	quote swapQuote,
) error {
	// A nil delta is a stablecoin pair, which moves no pool. Every other quote
	// already computed and validated its post-swap gap, so settlement writes it
	// without recomputing.
	if !quote.updatedArkPoolDelta.IsNil() {
		if err := k.ArkPoolDelta.Set(ctx, quote.updatedArkPoolDelta); err != nil {
			return sdkerrors.Wrapf(
				err,
				"applying swap to pool for offer %s and receive %s",
				offerCoin,
				quote.swapCoin,
			)
		}
	}

	offerCoins := sdk.NewCoins(offerCoin)
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, trader, types.ModuleName, offerCoins); err != nil {
		return sdkerrors.Wrapf(err, "sending offer coins %s from trader %s to module", offerCoins, trader)
	}

	// An expansion keeps its whole offer in module custody for the block's
	// settlement: spread and principal alike go down the waterfall (D6), so
	// nothing burns here. A redemption burns the offer outright and mints the
	// whole quoted output; the Buffer's share of it is burned back at
	// settlement, so the trader is paid the same either way and no conversion
	// waits on a valuation.
	burned := offerCoin
	minted := quote.swapCoin
	if offerCoin.Denom == chain.NoahBaseDenom {
		if err := k.recordExpansion(ctx, offerCoin, quote.swapCoin, quote.rates); err != nil {
			return sdkerrors.Wrapf(err, "recording expansion of offer %s into output %s", offerCoin, quote.swapCoin)
		}
		burned = chain.NoahCoin(math.ZeroInt())
	} else if quote.swapCoin.Denom == chain.NoahBaseDenom {
		// The redeemed supply is valued at the rate this swap quoted, as the
		// settlement-plan path values its own at the plan's committed rate. What
		// the recorder sums is the value; whose rate produced it stays with the
		// caller holding that rate.
		redeemed, err := quote.rates.Convert(sdk.NewDecCoinFromCoin(offerCoin), chain.NoahBaseDenom)
		if err != nil {
			return sdkerrors.Wrapf(err, "valuing redeemed offer %s", offerCoin)
		}
		if err := k.recordRedemption(ctx, redeemed.Amount, quote.swapCoin.Amount); err != nil {
			return sdkerrors.Wrapf(err, "recording redemption of offer %s into output %s", offerCoin, quote.swapCoin)
		}
	}

	if !burned.IsZero() {
		if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, sdk.NewCoins(burned)); err != nil {
			return sdkerrors.Wrapf(err, "burning settlement coins %s from module", burned)
		}
	}
	if !minted.IsZero() {
		if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, sdk.NewCoins(minted)); err != nil {
			return sdkerrors.Wrapf(err, "minting settlement coins %s in module", minted)
		}
	}

	swapCoins := sdk.NewCoins(quote.swapCoin)
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, receiver, swapCoins); err != nil {
		return sdkerrors.Wrapf(err, "sending swap coins %s from module to receiver %s", swapCoins, receiver)
	}

	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventSwap{
		Trader:      trader.String(),
		Recipient:   receiver.String(),
		OfferDenom:  offerCoin.Denom,
		OfferAmount: offerCoin.Amount,
		SwapDenom:   quote.swapCoin.Denom,
		SwapAmount:  quote.swapCoin.Amount,
		FeeDenom:    quote.swapFee.Denom,
		FeeAmount:   quote.swapFee.Amount,
	}); err != nil {
		return fmt.Errorf("emitting Market swap event: %w", err)
	}

	return nil
}

func marketRateError(err error) error {
	if errors.Is(err, oracletypes.ErrUnknownDenom) {
		return sdkerrors.Wrap(types.ErrNoEffectivePrice, err.Error())
	}
	return err
}

// requireConvertible enforces the lifecycle gate on both legs of a conversion.
//
// This gate, not the absence of a rate, is the whole of suspension's
// containment here: a suspended feed keeps running and its stored rate stays
// fresh, so the Oracle would price a suspended denomination on request. Quotes
// therefore read raw rates rather than registry pricing verdicts, which would
// hand back a settlement-backed rate for exactly the denominations this refuses
// — conversion asks what the market says, settlement asks what governance
// committed, and keeping the swap path unable to see plan rates is what keeps
// the two from merging. The status check must stay mandatory and ahead of
// every rate read.
//
// The asymmetry is the point: an offer may be ISSUANCE_HALTED because halting
// issuance is meant to preserve every exit — holders keep converting out — while
// an ask must be ACTIVE because producing more of a denomination governance has
// stopped issuing is exactly what the halt forbids. NOAH is the numeraire and
// has no registry entry, so it is always convertible; every other status is
// excluded, which is what makes suspension a containment tool.
func (k Keeper) requireConvertible(ctx context.Context, offerDenom, askDenom string) error {
	if offerDenom != chain.NoahBaseDenom {
		offerAsset, err := k.assetKeeper.GetAsset(ctx, offerDenom)
		if err != nil {
			return err
		}
		if !offerAsset.IsOraclePriced() {
			return sdkerrors.Wrapf(
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
			return sdkerrors.Wrapf(
				types.ErrIneligibleAsset,
				"%s asset %s cannot be produced by conversion",
				askAsset.Status,
				askDenom,
			)
		}
	}

	return nil
}
