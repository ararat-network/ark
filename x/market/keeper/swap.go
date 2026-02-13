package keeper

import (
	"context"
	"fmt"

	core "noah/types"
	"noah/x/market/types"

	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
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
		offerBaseCoin, err := k.ComputeInternalSwap(ctx, sdk.NewDecCoinFromCoin(offerCoin), core.MicroSDRDenom)
		if err != nil {
			return err
		}

		noahPoolDelta = noahPoolDelta.Add(offerBaseCoin.Amount)
	}

	// In case swapping Ark to Noah, the ark swap pool(offer) must be increased and the noah swap pool(ask) must be decreased
	if offerCoin.Denom == core.MicroArkDenom && askCoin.Denom != core.MicroArkDenom {
		askBaseCoin, err := k.ComputeInternalSwap(ctx, askCoin, core.MicroSDRDenom)
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

// ComputeSwap returns the amount of asked coins should be returned for a given offerCoin at the effective
// exchange rate registered with the oracle.
// Returns an Error if the swap is recursive, or the coins to be traded are unknown by the oracle, or the amount
// to trade is too small.
func (k Keeper) ComputeSwap(ctx context.Context, offerCoin sdk.Coin, askDenom string) (retDecCoin sdk.DecCoin, spread math.LegacyDec, err error) {
	// Return invalid recursive swap err
	if offerCoin.Denom == askDenom {
		return sdk.DecCoin{}, math.LegacyZeroDec(), sdkerrors.Wrap(types.ErrRecursiveSwap, askDenom)
	}

	// Swap offer coin to base denom for simplicity of swap process
	baseOfferDecCoin, err := k.ComputeInternalSwap(ctx, sdk.NewDecCoinFromCoin(offerCoin), core.MicroSDRDenom)
	if err != nil {
		return sdk.DecCoin{}, math.LegacyDec{}, err
	}

	// Get swap amount based on the oracle price
	retDecCoin, err = k.ComputeInternalSwap(ctx, baseOfferDecCoin, askDenom)
	if err != nil {
		return sdk.DecCoin{}, math.LegacyDec{}, err
	}

	// Noah => Noah swap
	// Apply only tobin tax without constant product spread
	if offerCoin.Denom != core.MicroArkDenom && askDenom != core.MicroArkDenom {
		var tobinTax math.LegacyDec
		offerTobinTax, err2 := k.OracleKeeper.GetTobinTax(ctx, offerCoin.Denom)
		if err2 != nil {
			return sdk.DecCoin{}, math.LegacyDec{}, err2
		}

		askTobinTax, err2 := k.OracleKeeper.GetTobinTax(ctx, askDenom)
		if err2 != nil {
			return sdk.DecCoin{}, math.LegacyDec{}, err2
		}

		// Apply highest tobin tax for the denoms in the swap operation
		if askTobinTax.GT(offerTobinTax) {
			tobinTax = askTobinTax
		} else {
			tobinTax = offerTobinTax
		}

		spread = tobinTax
		return
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return sdk.DecCoin{}, math.LegacyDec{}, fmt.Errorf("Getting params: %w", err)
	}
	basePool, err := math.LegacyNewDecFromStr(params.BasePool)
	if err != nil {
		return sdk.DecCoin{}, math.LegacyDec{}, fmt.Errorf("Converting BasePool: %w", err)
	}
	minSpread, err := math.LegacyNewDecFromStr(params.MinStabilitySpread)
	if err != nil {
		return sdk.DecCoin{}, math.LegacyDec{}, fmt.Errorf("Converting MinStabilitySpread: %w", err)
	}

	// constant-product, which by construction is square of base(equilibrium) pool
	cp := basePool.Mul(basePool)
	noahPoolDelta, err := k.NoahPoolDelta.Get(ctx)
	if err != nil {
		return sdk.DecCoin{}, math.LegacyDec{}, fmt.Errorf("Getting NoahPoolDelta: %w", err)
	}
	noahPool := basePool.Add(noahPoolDelta)
	arkPool := cp.Quo(noahPool)

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

	// Get cp(constant-product) based swap amount
	// askBaseAmount = askPool - cp / (offerPool + offerBaseAmount)
	// askBaseAmount is base denom(usdr) unit
	askBaseAmount := askPool.Sub(cp.Quo(offerPool.Add(baseOfferDecCoin.Amount)))

	// Both baseOffer and baseAsk are usdr units, so spread can be calculated by
	// spread = (baseOfferAmt - baseAskAmt) / baseOfferAmt
	baseOfferAmount := baseOfferDecCoin.Amount
	spread = baseOfferAmount.Sub(askBaseAmount).Quo(baseOfferAmount)

	if spread.LT(minSpread) {
		spread = minSpread
	}

	return retDecCoin, spread, nil
}

// ComputeInternalSwap returns the amount of asked DecCoin should be returned for a given offerCoin at the effective
// exchange rate registered with the oracle.
// Different from ComputeSwap, ComputeInternalSwap does not charge a spread as its use is system internal.
func (k Keeper) ComputeInternalSwap(ctx context.Context, offerCoin sdk.DecCoin, askDenom string) (sdk.DecCoin, error) {
	if offerCoin.Denom == askDenom {
		return offerCoin, nil
	}

	offerRate, err := k.OracleKeeper.GetArkExchangeRate(ctx, offerCoin.Denom)
	if err != nil {
		return sdk.DecCoin{}, sdkerrors.Wrap(types.ErrNoEffectivePrice, offerCoin.Denom)
	}

	askRate, err := k.OracleKeeper.GetArkExchangeRate(ctx, askDenom)
	if err != nil {
		return sdk.DecCoin{}, sdkerrors.Wrap(types.ErrNoEffectivePrice, askDenom)
	}

	retAmount := offerCoin.Amount.Mul(askRate).Quo(offerRate)
	if retAmount.LTE(math.LegacyZeroDec()) {
		return sdk.DecCoin{}, sdkerrors.Wrap(errortypes.ErrInvalidCoins, offerCoin.String())
	}

	return sdk.NewDecCoinFromDec(askDenom, retAmount), nil
}

// simulateSwap interface for simulate swap
func (k Keeper) simulateSwap(ctx context.Context, offerCoin sdk.Coin, askDenom string) (sdk.Coin, error) {
	if askDenom == offerCoin.Denom {
		return sdk.Coin{}, sdkerrors.Wrap(types.ErrRecursiveSwap, askDenom)
	}

	if offerCoin.Amount.BigInt().BitLen() > 100 {
		return sdk.Coin{}, sdkerrors.Wrap(errortypes.ErrInvalidCoins, offerCoin.String())
	}

	swapCoin, spread, err := k.ComputeSwap(ctx, offerCoin, askDenom)
	if err != nil {
		return sdk.Coin{}, sdkerrors.Wrap(sdkerrors.ErrPanic, err.Error())
	}

	if spread.IsPositive() {
		swapFeeAmt := spread.Mul(swapCoin.Amount)
		if swapFeeAmt.IsPositive() {
			swapFee := sdk.NewDecCoinFromDec(swapCoin.Denom, swapFeeAmt)
			swapCoin = swapCoin.Sub(swapFee)
		}
	}

	retCoin, _ := swapCoin.TruncateDecimal()
	return retCoin, nil
}
