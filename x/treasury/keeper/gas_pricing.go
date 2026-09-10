package keeper

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/ararat-network/ark/pkg/decimal"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	"github.com/ararat-network/ark/x/treasury/types"
)

// blockGasTallyKey addresses the single transient scalar behind the base-fee
// update. The transient store clears at commit, so the tally is block-scoped
// by construction and never needs a reset write. It is raw store access
// rather than a collection because one block-lived scalar is not a schema.
var blockGasTallyKey = []byte{0}

// TallyBlockGas records declared gas after fee acceptance; later ante failure rolls it back. The
// uint64 sum saturates instead of failing, yielding full utilisation for the controller.
func (k Keeper) TallyBlockGas(ctx context.Context, gasLimit uint64) error {
	kv := k.transientStoreService.OpenTransientStore(ctx)
	stored, err := kv.Get(blockGasTallyKey)
	if err != nil {
		return fmt.Errorf("reading block gas tally: %w", err)
	}
	tally := sdk.BigEndianToUint64(stored)
	if gasLimit > ^uint64(0)-tally {
		tally = ^uint64(0)
	} else {
		tally += gasLimit
	}
	if err := kv.Set(blockGasTallyKey, sdk.Uint64ToBigEndian(tally)); err != nil {
		return fmt.Errorf("writing block gas tally: %w", err)
	}
	return nil
}

// GetRequiredGasFee returns ceil(price * gasLimit * factor) and the same factor used for tip
// normalisation. Callers reuse params and price reads across denominations. Missing factors return
// collections.ErrNotFound.
func (k Keeper) GetRequiredGasFee(
	ctx context.Context,
	params types.Params,
	price math.LegacyDec,
	gasLimit uint64,
	denom string,
) (sdk.Coin, math.LegacyDec, error) {
	factor, _, err := k.gasFactor(ctx, params.ReferenceDenom, denom)
	if err != nil {
		return sdk.Coin{}, math.LegacyDec{}, err
	}
	product, err := decimal.Mul(price, math.LegacyNewDecFromInt(math.NewIntFromUint64(gasLimit)))
	if err != nil {
		// Unreachable while the price respects its ceiling: MaxBaseGasPrice
		// times a uint64 of gas stays far inside the Dec domain. The backstop
		// rejects one transaction, never a block.
		return sdk.Coin{}, math.LegacyDec{}, err
	}
	converted, err := decimal.Mul(product, factor)
	if err != nil {
		// Reachable by a lopsided stored factor, on deriveTaxCap's terms —
		// but a fee requirement past the Dec domain refuses the denomination
		// rather than deriving a sentinel: unpayable beats free.
		return sdk.Coin{}, math.LegacyDec{}, err
	}
	return sdk.NewCoin(denom, converted.Ceil().TruncateInt()), factor, nil
}

// updateBaseGasPrice adjusts the fee from gas utilisation within the per-block rate limit and
// governance bounds. Unbounded gas or zero adjustment holds the price before clamping. No Oracle
// read is required; see x/treasury/README.md for the controller formula.
func (k Keeper) updateBaseGasPrice(ctx context.Context) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}
	current, err := k.BaseGasPrice.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting base gas price: %w", err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	maxGas := int64(-1)
	if block := sdkCtx.ConsensusParams().Block; block != nil {
		maxGas = block.MaxGas
	}

	next := current
	if maxGas > 0 && params.BaseFeeAdjustmentRate.IsPositive() {
		tallyBytes, err := k.transientStoreService.OpenTransientStore(ctx).Get(blockGasTallyKey)
		if err != nil {
			return fmt.Errorf("reading block gas tally: %w", err)
		}
		tally := math.LegacyNewDecFromInt(math.NewIntFromUint64(sdk.BigEndianToUint64(tallyBytes)))

		// Scale deviation before division to avoid intermediate rounding. The pivot is positive
		// because target validation and the MaxGas guard exclude zero.
		pivot := params.BaseFeeTargetUtilisation.MulInt64(maxGas)
		scaled, err := decimal.Mul(params.BaseFeeAdjustmentRate, tally.Sub(pivot))
		if err != nil {
			// Unreachable: the rate is at most one and the tally at most a
			// uint64 of gas, orders of magnitude inside the Dec domain. Kept
			// as the loud backstop.
			return fmt.Errorf("scaling base-fee deviation: %w", err)
		}
		delta := scaled.Quo(pivot)
		if delta.GT(params.BaseFeeAdjustmentRate) {
			delta = params.BaseFeeAdjustmentRate
		}
		if delta.LT(params.BaseFeeAdjustmentRate.Neg()) {
			delta = params.BaseFeeAdjustmentRate.Neg()
		}
		next, err = decimal.Mul(current, math.LegacyOneDec().Add(delta))
		if err != nil {
			// Unreachable: the factor is at most two and the price at most
			// MaxBaseGasPrice after every prior write's clamp. Saturating is
			// the same verdict the clamp below would give.
			next = types.MaxBaseGasPrice
		}
	}

	if next.LT(params.MinBaseGasPrice) {
		next = params.MinBaseGasPrice
	}
	if next.GT(types.MaxBaseGasPrice) {
		next = types.MaxBaseGasPrice
	}

	if next.Equal(current) {
		return nil
	}
	if err := k.BaseGasPrice.Set(ctx, next); err != nil {
		return fmt.Errorf("setting base gas price: %w", err)
	}
	if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventBaseGasPriceUpdated{
		BaseGasPrice: next,
	}); err != nil {
		return fmt.Errorf("emitting base gas price update: %w", err)
	}
	return nil
}

// rescaleBaseFee converts the live gas price and returns the converted governance floor for the
// caller's params write. The supplied rate pair prices both reference units; conversion failure
// aborts the rebase.
func (k Keeper) rescaleBaseFee(ctx context.Context, params types.Params, to string, rates oracletypes.RateSet) (math.LegacyDec, error) {
	unit, err := rates.Convert(sdk.NewDecCoin(params.ReferenceDenom, math.OneInt()), to)
	if err != nil {
		return math.LegacyDec{}, err
	}
	if !unit.Amount.IsPositive() {
		return math.LegacyDec{}, errorsmod.Wrapf(
			errortypes.ErrInvalidRequest,
			"non-positive reference cross rate %s converting the base fee",
			unit.Amount,
		)
	}

	minGasPrice := convertGasPrice(params.MinBaseGasPrice, unit.Amount)

	price, err := k.BaseGasPrice.Get(ctx)
	if err != nil {
		return math.LegacyDec{}, fmt.Errorf("getting base gas price: %w", err)
	}
	converted := convertGasPrice(price, unit.Amount)
	if converted.LT(minGasPrice) {
		converted = minGasPrice
	}
	if err := k.BaseGasPrice.Set(ctx, converted); err != nil {
		return math.LegacyDec{}, fmt.Errorf("setting rebased base gas price: %w", err)
	}
	// The re-quote reports through the price's one stream: refresh emits on
	// change only, so a consumer missing this write would hold the old-unit
	// price until the controller next moves.
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventBaseGasPriceUpdated{
		BaseGasPrice: converted,
	}); err != nil {
		return math.LegacyDec{}, fmt.Errorf("emitting base gas price update: %w", err)
	}
	return minGasPrice, nil
}

// convertGasPrice re-quotes one per-gas price by the unit cross and lands it
// inside the controller's domain: saturation and underflow resolve to the
// nearest bound, the same degrade class as the cap's floor-at-one — a bad
// bound beats wedging the re-point.
func convertGasPrice(price, unit math.LegacyDec) math.LegacyDec {
	converted, err := decimal.Mul(price, unit)
	if err != nil {
		return types.MaxBaseGasPrice
	}
	if converted.GT(types.MaxBaseGasPrice) {
		return types.MaxBaseGasPrice
	}
	if !converted.IsPositive() {
		return math.LegacySmallestDec()
	}
	return converted
}

// gasFactor returns reference identity at height zero or a stored denomination cross and derivation
// height, including NOAH. Missing crosses return collections.ErrNotFound and cannot pay gas.
func (k Keeper) gasFactor(ctx context.Context, reference, denom string) (math.LegacyDec, uint64, error) {
	if denom == reference {
		return math.LegacyOneDec(), 0, nil
	}
	entry, err := k.ConversionFactors.Get(ctx, denom)
	if err != nil {
		return math.LegacyDec{}, 0, err
	}
	return entry.Factor, entry.DerivedHeight, nil
}

// gasPrice prices one denomination for the fee queries: the base price
// carried through the denomination's cross, derivation height attached. An
// unrepresentable product propagates — the price is unpayable, and the
// sheet omits what the gate would refuse.
func (k Keeper) gasPrice(ctx context.Context, reference, denom string) (types.GasPrice, error) {
	factor, height, err := k.gasFactor(ctx, reference, denom)
	if err != nil {
		return types.GasPrice{}, err
	}
	base, err := k.BaseGasPrice.Get(ctx)
	if err != nil {
		return types.GasPrice{}, fmt.Errorf("getting base gas price: %w", err)
	}
	price, err := decimal.Mul(base, factor)
	if err != nil {
		return types.GasPrice{}, err
	}
	return types.GasPrice{
		Denom:         denom,
		GasPrice:      price,
		DerivedHeight: height,
	}, nil
}
