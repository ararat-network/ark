package keeper

import (
	"context"
	"errors"
	"fmt"

	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	chain "ark/pkg/chain"
	"ark/pkg/decimal"
	"ark/x/market/types"
	oracletypes "ark/x/oracle/types"
)

// swapQuote captures the outcome and every rate and pool value used to settle one swap.
// It is execution-local and is never persisted.
type swapQuote struct {
	swapDecCoin      sdk.DecCoin
	swapCoin         sdk.Coin
	swapFee          sdk.DecCoin
	baseOfferDecCoin sdk.DecCoin
	arkPoolDelta     math.LegacyDec
	basePool         sdk.DecCoin
	rates            oracletypes.RateSet
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
	offerDecCoin := sdk.NewDecCoinFromCoin(offerCoin)
	var quote swapQuote
	var swapDecCoin sdk.DecCoin
	var spread math.LegacyDec

	if offerCoin.Denom != chain.NoahBaseDenom && askDenom != chain.NoahBaseDenom {
		// Stablecoin-to-stablecoin swaps use only the larger Tobin tax.
		rates, err := k.oracleKeeper.GetRateSet(ctx, offerCoin.Denom, askDenom)
		if err != nil {
			return swapQuote{}, marketRateError(err)
		}
		swapDecCoin, err = rates.Convert(offerDecCoin, askDenom)
		if err != nil {
			return swapQuote{}, err
		}
		tobinTaxes, err := k.oracleKeeper.GetTobinTaxes(ctx)
		if err != nil {
			return swapQuote{}, err
		}
		var offerTobinTax, askTobinTax math.LegacyDec
		var offerFound, askFound bool
		for _, tobinTax := range tobinTaxes {
			switch tobinTax.Denom {
			case offerCoin.Denom:
				offerTobinTax = tobinTax.TobinTax
				offerFound = true
			case askDenom:
				askTobinTax = tobinTax.TobinTax
				askFound = true
			}
		}
		if !offerFound {
			return swapQuote{}, sdkerrors.Wrap(oracletypes.ErrUnknownDenom, offerCoin.Denom)
		}
		if !askFound {
			return swapQuote{}, sdkerrors.Wrap(oracletypes.ErrUnknownDenom, askDenom)
		}
		spread = math.LegacyMaxDec(askTobinTax, offerTobinTax)
		quote.rates = rates
	} else {
		params, err := k.Params.Get(ctx)
		if err != nil {
			return swapQuote{}, fmt.Errorf("getting params: %w", err)
		}
		arkPoolDelta, err := k.ArkPoolDelta.Get(ctx)
		if err != nil {
			return swapQuote{}, fmt.Errorf("getting ArkPoolDelta: %w", err)
		}
		rates, err := k.oracleKeeper.GetRateSet(
			ctx,
			offerCoin.Denom,
			params.BasePool.Denom,
			askDenom,
		)
		if err != nil {
			return swapQuote{}, marketRateError(err)
		}
		baseOfferDecCoin, err := rates.Convert(offerDecCoin, params.BasePool.Denom)
		if err != nil {
			return swapQuote{}, err
		}
		swapDecCoin, err = rates.Convert(offerDecCoin, askDenom)
		if err != nil {
			return swapQuote{}, err
		}
		quote = swapQuote{
			baseOfferDecCoin: baseOfferDecCoin,
			arkPoolDelta:     arkPoolDelta,
			basePool:         params.BasePool,
			rates:            rates,
		}

		pools, err := types.NewEffectivePools(params.BasePool.Amount, arkPoolDelta)
		if err != nil {
			return swapQuote{}, sdkerrors.Wrapf(
				types.ErrArithmeticOutOfRange,
				"constructing effective pools: %v",
				err,
			)
		}

		var offerPool math.LegacyDec
		var askPool math.LegacyDec
		if offerCoin.Denom != chain.NoahBaseDenom {
			offerPool = pools.ArkPool
			askPool = pools.NoahPool
		} else {
			offerPool = pools.NoahPool
			askPool = pools.ArkPool
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

		spread = params.MinStabilitySpread
		if askBaseAmount.LT(baseOfferAmount) {
			rawSpread := baseOfferAmount.Sub(askBaseAmount).Quo(baseOfferAmount)
			spread = math.LegacyMaxDec(params.MinStabilitySpread, rawSpread)
		}
	}

	feeAmount := spread.Mul(swapDecCoin.Amount)
	swapDecCoin.Amount = swapDecCoin.Amount.Sub(feeAmount)
	swapCoin, dust := swapDecCoin.TruncateDecimal()
	if !swapCoin.IsPositive() {
		return swapQuote{}, types.ErrZeroSwapCoin
	}

	quote.swapDecCoin = swapDecCoin
	quote.swapCoin = swapCoin
	quote.swapFee = sdk.NewDecCoinFromDec(swapDecCoin.Denom, feeAmount.Add(dust.Amount))

	return quote, nil
}

// settleSwap applies pool changes, moves funds through the module account, and emits swap events.
func (k Keeper) settleSwap(
	ctx context.Context,
	trader sdk.AccAddress,
	receiver sdk.AccAddress,
	offerCoin sdk.Coin,
	quote swapQuote,
) error {
	if err := k.applySwapToPool(ctx, offerCoin, quote); err != nil {
		return sdkerrors.Wrapf(err, "applying swap to pool for offer %s and receive %s", offerCoin, quote.swapDecCoin)
	}

	offerCoins := sdk.NewCoins(offerCoin)
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, trader, types.ModuleName, offerCoins); err != nil {
		return sdkerrors.Wrapf(err, "sending offer coins %s from trader %s to module", offerCoins, trader)
	}

	burned := offerCoin
	minted := quote.swapCoin
	if offerCoin.Denom == chain.NoahBaseDenom {
		allocation, err := k.treasuryKeeper.RouteExpansion(ctx, offerCoin, quote.swapCoin, quote.rates)
		if err != nil {
			return sdkerrors.Wrapf(err, "routing expansion for offer %s and output %s", offerCoin, quote.swapCoin)
		}
		burned = sdk.NewCoin(chain.NoahBaseDenom, allocation.TotalBurn())
	} else if quote.swapCoin.Denom == chain.NoahBaseDenom {
		draw, err := k.treasuryKeeper.DrawRedemptionBuffer(ctx, offerCoin, quote.swapCoin.Amount, quote.rates)
		if err != nil {
			return sdkerrors.Wrapf(err, "drawing redemption buffer for offer %s and output %s", offerCoin, quote.swapCoin)
		}
		minted = sdk.NewCoin(chain.NoahBaseDenom, quote.swapCoin.Amount.Sub(draw.BufferPaid))
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

	if err := k.treasuryKeeper.RecordSupplyChange(ctx, burned, minted, quote.rates); err != nil {
		return sdkerrors.Wrapf(err, "recording supply change from %s to %s", burned, minted)
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

func (k Keeper) applySwapToPool(
	ctx context.Context,
	offerCoin sdk.Coin,
	quote swapQuote,
) error {
	if offerCoin.Denom != chain.NoahBaseDenom && quote.swapDecCoin.Denom != chain.NoahBaseDenom {
		return nil
	}

	arkPoolDelta := quote.arkPoolDelta
	if offerCoin.Denom != chain.NoahBaseDenom {
		var err error
		arkPoolDelta, err = decimal.Add(arkPoolDelta, quote.baseOfferDecCoin.Amount)
		if err != nil {
			return sdkerrors.Wrapf(
				types.ErrArithmeticOutOfRange,
				"adding the offer amount to the ark pool delta: %v",
				err,
			)
		}
	} else {
		askBaseCoin, err := quote.rates.Convert(quote.swapDecCoin, quote.basePool.Denom)
		if err != nil {
			return err
		}
		arkPoolDelta, err = decimal.Sub(arkPoolDelta, askBaseCoin.Amount)
		if err != nil {
			return sdkerrors.Wrapf(
				types.ErrArithmeticOutOfRange,
				"subtracting the ask amount from the ark pool delta: %v",
				err,
			)
		}
	}

	if _, err := types.NewEffectivePools(quote.basePool.Amount, arkPoolDelta); err != nil {
		return sdkerrors.Wrapf(
			types.ErrArithmeticOutOfRange,
			"validating the updated effective pools: %v",
			err,
		)
	}

	return k.ArkPoolDelta.Set(ctx, arkPoolDelta)
}

func marketRateError(err error) error {
	if errors.Is(err, oracletypes.ErrUnknownDenom) {
		return sdkerrors.Wrap(types.ErrNoEffectivePrice, err.Error())
	}
	return err
}
