package keeper

import (
	"context"
	"errors"
	"fmt"

	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	core "noah/pkg/types"
	"noah/x/market/types"
	oracletypes "noah/x/oracle/types"
)

// ApplySwapToPool updates each pool with offerCoin and askCoin taken from swap operation,
// OfferPool = OfferPool + offerAmt (Fills the swap pool with offerAmt)
// AskPool = AskPool - askAmt       (Uses askAmt from the swap pool)
func (k Keeper) ApplySwapToPool(ctx context.Context, offerCoin sdk.Coin, askCoin sdk.DecCoin) error {
	// No delta update in case Noah to Noah swap
	if offerCoin.Denom != core.MicroArkDenom && askCoin.Denom != core.MicroArkDenom {
		return nil
	}

	noahPoolDelta, err := k.NoahPoolDelta.Get(ctx)
	if err != nil {
		return err
	}

	// In case swapping Noah to Ark, the noah swap pool(offer) must be increased and the ark swap pool(ask) must be decreased
	if offerCoin.Denom != core.MicroArkDenom && askCoin.Denom == core.MicroArkDenom {
		offerBaseCoin, err := k.ComputeOracleRate(ctx, sdk.NewDecCoinFromCoin(offerCoin), core.MicroSDRDenom)
		if err != nil {
			return err
		}

		noahPoolDelta = noahPoolDelta.Add(offerBaseCoin.Amount)
	}

	// In case swapping Ark to Noah, the ark swap pool(offer) must be increased and the noah swap pool(ask) must be decreased
	if offerCoin.Denom == core.MicroArkDenom && askCoin.Denom != core.MicroArkDenom {
		askBaseCoin, err := k.ComputeOracleRate(ctx, askCoin, core.MicroSDRDenom)
		if err != nil {
			return err
		}

		noahPoolDelta = noahPoolDelta.Sub(askBaseCoin.Amount)
	}

	if err := k.NoahPoolDelta.Set(ctx, noahPoolDelta); err != nil {
		return err
	}

	return nil
}

// ComputeSwap returns the amount of asked coins that should be returned for a given offerCoin at the effective
// exchange rate registered with the oracle. Returns an error if the swap is recursive, the coins to be traded
// are unknown by the oracle, or the amount to trade is too small.
func (k Keeper) ComputeSwap(ctx context.Context, offerCoin sdk.Coin, askDenom string) (sdk.DecCoin, math.LegacyDec, error) {
	// Return invalid recursive swap err
	if offerCoin.Denom == askDenom {
		return sdk.DecCoin{}, math.LegacyZeroDec(), sdkerrors.Wrap(types.ErrRecursiveSwap, askDenom)
	}

	// Swap offer coin to base denom for simplicity of swap process
	baseOfferDecCoin, err := k.ComputeOracleRate(ctx, sdk.NewDecCoinFromCoin(offerCoin), core.MicroSDRDenom)
	if err != nil {
		return sdk.DecCoin{}, math.LegacyDec{}, err
	}

	// Get swap amount based on the oracle price
	retDecCoin, err := k.ComputeOracleRate(ctx, baseOfferDecCoin, askDenom)
	if err != nil {
		return sdk.DecCoin{}, math.LegacyDec{}, err
	}

	// Noah => Noah swap
	// Apply only tobin tax without constant product spread
	if offerCoin.Denom != core.MicroArkDenom && askDenom != core.MicroArkDenom {
		var tobinTax math.LegacyDec
		offerTobinTax, err := k.oracleKeeper.GetTobinTax(ctx, offerCoin.Denom)
		if err != nil {
			return sdk.DecCoin{}, math.LegacyDec{}, err
		}

		askTobinTax, err := k.oracleKeeper.GetTobinTax(ctx, askDenom)
		if err != nil {
			return sdk.DecCoin{}, math.LegacyDec{}, err
		}

		// Apply highest tobin tax for the denoms in the swap operation
		if askTobinTax.GT(offerTobinTax) {
			tobinTax = askTobinTax
		} else {
			tobinTax = offerTobinTax
		}

		return retDecCoin, tobinTax, nil
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return sdk.DecCoin{}, math.LegacyDec{}, fmt.Errorf("getting params: %w", err)
	}
	basePool := params.BasePool
	minSpread := params.MinStabilitySpread

	// constantProduct is square of base pool
	constantProduct := basePool.Mul(basePool)
	noahPoolDelta, err := k.NoahPoolDelta.Get(ctx)
	if err != nil {
		return sdk.DecCoin{}, math.LegacyDec{}, fmt.Errorf("getting NoahPoolDelta: %w", err)
	}
	noahPool := basePool.Add(noahPoolDelta)
	arkPool := constantProduct.Quo(noahPool)

	var offerPool math.LegacyDec // base denom(usdr) unit
	var askPool math.LegacyDec   // base denom(usdr) unit
	if offerCoin.Denom != core.MicroArkDenom {
		// Noah->Ark swap
		offerPool = noahPool
		askPool = arkPool
	} else {
		// Ark->Noah swap
		offerPool = arkPool
		askPool = noahPool
	}

	// Get constantProduct based swap amount
	// askBaseAmount = askPool - constantProduct / (offerPool + offerBaseAmount)
	// askBaseAmount is base denom(usdr) unit
	askBaseAmount := askPool.Sub(constantProduct.Quo(offerPool.Add(baseOfferDecCoin.Amount)))

	// Both baseOffer and baseAsk are usdr units, so spread can be calculated by
	// spread = (baseOfferAmt - baseAskAmt) / baseOfferAmt
	baseOfferAmount := baseOfferDecCoin.Amount
	spread := baseOfferAmount.Sub(askBaseAmount).Quo(baseOfferAmount)

	if spread.LT(minSpread) {
		spread = minSpread
	}

	return retDecCoin, spread, nil
}

// ComputeOracleRate converts an offer coin to the ask denom using oracle exchange rates.
func (k Keeper) ComputeOracleRate(ctx context.Context, offerCoin sdk.DecCoin, askDenom string) (sdk.DecCoin, error) {
	if offerCoin.Denom == askDenom {
		return offerCoin, nil
	}

	offerRate, err := k.oracleKeeper.GetExchangeRate(ctx, offerCoin.Denom)
	if err != nil {
		if errors.Is(err, oracletypes.ErrUnknownDenom) {
			return sdk.DecCoin{}, sdkerrors.Wrapf(types.ErrNoEffectivePrice, "no oracle price for denom %s", offerCoin.Denom)
		}
		return sdk.DecCoin{}, fmt.Errorf("getting oracle exchange rate for denom %s: %w", offerCoin.Denom, err)
	}

	askRate, err := k.oracleKeeper.GetExchangeRate(ctx, askDenom)
	if err != nil {
		if errors.Is(err, oracletypes.ErrUnknownDenom) {
			return sdk.DecCoin{}, sdkerrors.Wrapf(types.ErrNoEffectivePrice, "no oracle price for denom %s", askDenom)
		}
		return sdk.DecCoin{}, fmt.Errorf("getting oracle exchange rate for denom %s: %w", askDenom, err)
	}

	retAmount := offerCoin.Amount.Mul(askRate).Quo(offerRate)
	if retAmount.LTE(math.LegacyZeroDec()) {
		return sdk.DecCoin{}, sdkerrors.Wrap(errortypes.ErrInvalidCoins, offerCoin.String())
	}

	return sdk.NewDecCoinFromDec(askDenom, retAmount), nil
}
