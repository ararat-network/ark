package keeper

import (
	"context"
	"errors"
	"fmt"

	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	"ark/pkg/decimal"
	"ark/x/market/types"
	oracletypes "ark/x/oracle/types"
)

// swapQuote captures every rate and pool value used to price and settle one swap.
// It is execution-local and is never persisted.
type swapQuote struct {
	swapDecCoin      sdk.DecCoin
	spread           math.LegacyDec
	baseOfferDecCoin sdk.DecCoin
	arkPoolDelta     math.LegacyDec
	basePool         math.LegacyDec
	rates            oracletypes.RateSnapshot
}

// ComputeSwap returns the amount of asked coins and spread for a swap quote.
func (k Keeper) ComputeSwap(ctx context.Context, offerCoin sdk.Coin, askDenom string) (sdk.DecCoin, math.LegacyDec, error) {
	quote, err := k.quoteSwap(ctx, offerCoin, askDenom)
	if err != nil {
		return sdk.DecCoin{}, math.LegacyDec{}, err
	}
	return quote.swapDecCoin, quote.spread, nil
}

func (k Keeper) quoteSwap(ctx context.Context, offerCoin sdk.Coin, askDenom string) (*swapQuote, error) {
	if offerCoin.Denom == askDenom {
		return nil, sdkerrors.Wrap(types.ErrRecursiveSwap, askDenom)
	}

	rates, err := k.oracleKeeper.GetRateSnapshot(
		ctx,
		offerCoin.Denom,
		chain.MicroSDRDenom,
		askDenom,
	)
	if err != nil {
		return nil, marketRateError(err)
	}

	baseOfferDecCoin, err := rates.Convert(sdk.NewDecCoinFromCoin(offerCoin), chain.MicroSDRDenom)
	if err != nil {
		return nil, marketRateError(err)
	}
	swapDecCoin, err := rates.Convert(baseOfferDecCoin, askDenom)
	if err != nil {
		return nil, marketRateError(err)
	}

	quote := &swapQuote{
		swapDecCoin:      swapDecCoin,
		baseOfferDecCoin: baseOfferDecCoin,
		rates:            rates,
	}

	// Stablecoin-to-stablecoin swaps use only the larger Tobin tax.
	if offerCoin.Denom != chain.MicroNoahDenom && askDenom != chain.MicroNoahDenom {
		offerTobinTax, err := k.oracleKeeper.GetTobinTax(ctx, offerCoin.Denom)
		if err != nil {
			return nil, err
		}
		askTobinTax, err := k.oracleKeeper.GetTobinTax(ctx, askDenom)
		if err != nil {
			return nil, err
		}
		if askTobinTax.GT(offerTobinTax) {
			quote.spread = askTobinTax
		} else {
			quote.spread = offerTobinTax
		}
		return quote, nil
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting params: %w", err)
	}
	arkPoolDelta, err := k.ArkPoolDelta.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting ArkPoolDelta: %w", err)
	}
	quote.arkPoolDelta = arkPoolDelta
	quote.basePool = params.BasePool

	pools, err := types.NewEffectivePools(params.BasePool, arkPoolDelta)
	if err != nil {
		return nil, arithmeticError("constructing effective pools", err)
	}

	var offerPool math.LegacyDec
	var askPool math.LegacyDec
	if offerCoin.Denom != chain.MicroNoahDenom {
		offerPool = pools.ArkPool
		askPool = pools.NoahPool
	} else {
		offerPool = pools.NoahPool
		askPool = pools.ArkPool
	}

	// Preserve the existing arithmetic order: offer -> SDR, then constant-product spread.
	updatedOfferPool, err := decimal.Add(offerPool, baseOfferDecCoin.Amount)
	if err != nil {
		return nil, arithmeticError("adding the offer amount to the effective pool", err)
	}
	remainingAskPool, err := decimal.Quo(pools.ConstantProduct, updatedOfferPool)
	if err != nil {
		return nil, arithmeticError("computing the remaining ask pool", err)
	}
	askBaseAmount, err := decimal.Sub(askPool, remainingAskPool)
	if err != nil {
		return nil, arithmeticError("computing the ask amount", err)
	}
	if askBaseAmount.IsNegative() {
		return nil, arithmeticError("computing the ask amount", errors.New("ask amount is negative"))
	}
	baseOfferAmount := baseOfferDecCoin.Amount
	spreadAmount, err := decimal.Sub(baseOfferAmount, askBaseAmount)
	if err != nil {
		return nil, arithmeticError("computing the spread amount", err)
	}
	spread, err := decimal.Quo(spreadAmount, baseOfferAmount)
	if err != nil {
		return nil, arithmeticError("computing the spread", err)
	}
	if spread.IsNegative() || spread.GT(math.LegacyOneDec()) {
		return nil, arithmeticError("computing the spread", fmt.Errorf("spread %s is outside [0, 1]", spread))
	}
	if spread.LT(params.MinStabilitySpread) {
		spread = params.MinStabilitySpread
	}
	quote.spread = spread

	return quote, nil
}

func (k Keeper) applySwapToPool(
	ctx context.Context,
	offerCoin sdk.Coin,
	askCoin sdk.DecCoin,
	quote *swapQuote,
) error {
	if offerCoin.Denom != chain.MicroNoahDenom && askCoin.Denom != chain.MicroNoahDenom {
		return nil
	}

	arkPoolDelta := quote.arkPoolDelta
	if offerCoin.Denom != chain.MicroNoahDenom {
		var err error
		arkPoolDelta, err = decimal.Add(arkPoolDelta, quote.baseOfferDecCoin.Amount)
		if err != nil {
			return arithmeticError("adding the offer amount to the ark pool delta", err)
		}
	} else {
		askBaseCoin, err := quote.rates.Convert(askCoin, chain.MicroSDRDenom)
		if err != nil {
			return marketRateError(err)
		}
		arkPoolDelta, err = decimal.Sub(arkPoolDelta, askBaseCoin.Amount)
		if err != nil {
			return arithmeticError("subtracting the ask amount from the ark pool delta", err)
		}
	}

	if _, err := types.NewEffectivePools(quote.basePool, arkPoolDelta); err != nil {
		return arithmeticError("validating the updated effective pools", err)
	}

	return k.ArkPoolDelta.Set(ctx, arkPoolDelta)
}

func marketRateError(err error) error {
	if errors.Is(err, oracletypes.ErrUnknownDenom) {
		return sdkerrors.Wrap(types.ErrNoEffectivePrice, err.Error())
	}
	return err
}

func arithmeticError(operation string, err error) error {
	return sdkerrors.Wrapf(types.ErrArithmeticOutOfRange, "%s: %v", operation, err)
}
