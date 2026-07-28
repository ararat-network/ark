package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/math"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	"ark/pkg/decimal"
	oracletypes "ark/x/oracle/types"
)

// liabilityValuationGas is the flat gas charged for one transaction-time
// aggregate-liability valuation, hit or miss. The preblocker performs the real
// scan as unmetered block work, so the honest marginal cost is the primed
// path's single transient snapshot read. Transient stores opened through the
// store service are metered with the KV gas config rather than the cheaper
// transient one, because OpenTransientStore calls Context.KVStore; that is
// 1,000 flat plus 3 per key and value byte, so a typical snapshot read costs
// ~1,066 gas and the largest representable LegacyDec ~1,183. 2,000 covers the
// worst case with headroom. Recalibrate if the app customises store gas
// configs. Charging a constant keeps swap gas position-independent within a
// block and independent of the oracle whitelist size, and simulation (which
// always runs with an empty transient store) quotes exactly what execution
// consumes.
const liabilityValuationGas = 2_000

var (
	liabilitySnapshotKey = []byte{0x01}
	// liabilityUnavailableKey marks the block as having an incomplete liability
	// valuation. Rates are fixed at preblock, so retrying within the block
	// cannot succeed; the marker suppresses repeat scans until the transient
	// store resets at commit.
	liabilityUnavailableKey = []byte{0x02}
)

// RecordSupplyChange updates an existing block-local liability snapshot after
// Market has successfully applied its burn and mint. It deliberately does not
// build a snapshot when none exists; a later valuation will read current Bank
// supply directly.
func (k Keeper) RecordSupplyChange(ctx context.Context, burned sdk.Coin, minted sdk.Coin, rates oracletypes.RateSet) error {
	if err := burned.Validate(); err != nil {
		return fmt.Errorf("invalid burned coin: %w", err)
	}
	if err := minted.Validate(); err != nil {
		return fmt.Errorf("invalid minted coin: %w", err)
	}

	liability, found, err := k.loadLiabilitySnapshot(ctx)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}

	burnedValue, err := liabilityCoinValue(burned, rates)
	if err != nil {
		return fmt.Errorf("valuing burned supply %s: %w", burned, err)
	}
	liability, err = decimal.Sub(liability, burnedValue)
	if err != nil {
		return fmt.Errorf("subtracting burned supply %s from cached liability: %w", burned, err)
	}
	if liability.IsNegative() {
		return fmt.Errorf("burned supply %s exceeds cached liability", burned)
	}

	mintedValue, err := liabilityCoinValue(minted, rates)
	if err != nil {
		return fmt.Errorf("valuing minted supply %s: %w", minted, err)
	}
	liability, err = decimal.Add(liability, mintedValue)
	if err != nil {
		return fmt.Errorf("adding minted supply %s to cached liability: %w", minted, err)
	}

	return k.storeLiabilitySnapshot(ctx, liability)
}

// PrimeLiabilitySnapshot values the aggregate stable liability once for the
// block. The preblocker calls it immediately after oracle price application
// and vote-target advancement, so transaction-time callers always find either
// the snapshot or the unavailability marker and never rescan. Hard state
// errors propagate and fail the block; an incomplete valuation (stale or
// missing rates) is an expected degraded mode and only sets the marker.
func (k Keeper) PrimeLiabilitySnapshot(ctx context.Context) error {
	tobinTaxes, err := k.oracleKeeper.GetTobinTaxes(ctx)
	if err != nil {
		return fmt.Errorf("getting Tobin taxes: %w", err)
	}
	liability, complete, err := k.nominalLiabilityValue(ctx, tobinTaxes, nil)
	if err != nil {
		return err
	}
	if !complete {
		return k.markLiabilityUnavailable(ctx)
	}
	return k.storeLiabilitySnapshot(ctx, liability)
}

func (k Keeper) nominalLiabilityValue(
	ctx context.Context,
	tobinTaxes []oracletypes.TobinTax,
	rates oracletypes.RateSet,
) (math.LegacyDec, bool, error) {
	if rates == nil {
		rates = oracletypes.RateSet{}
	}
	coins := make([]sdk.Coin, 0, len(tobinTaxes))
	needed := make([]string, 0, len(tobinTaxes))
	for _, tax := range tobinTaxes {
		supply := k.bankKeeper.GetSupply(ctx, tax.Denom)
		if supply.Amount.IsPositive() {
			coins = append(coins, supply)
			if _, ok := rates[tax.Denom]; !ok {
				needed = append(needed, tax.Denom)
			}
		}
	}
	if len(needed) > 0 {
		captured, err := k.oracleKeeper.GetRateSet(ctx, needed...)
		if err != nil {
			if isValuationUnavailable(err) {
				return math.LegacyZeroDec(), false, nil
			}
			return math.LegacyDec{}, false, fmt.Errorf("capturing aggregate liability rates: %w", err)
		}
		for denom, rate := range captured {
			if _, exists := rates[denom]; !exists {
				rates[denom] = rate
			}
		}
	}

	total := math.LegacyZeroDec()
	for _, coin := range coins {
		converted, err := rates.Convert(sdk.NewDecCoinFromCoin(coin), chain.NoahBaseDenom)
		if err != nil {
			if isValuationUnavailable(err) {
				return math.LegacyZeroDec(), false, nil
			}
			return math.LegacyDec{}, false, fmt.Errorf("valuing liability %s: %w", coin.Denom, err)
		}
		total, err = decimal.Add(total, converted.Amount)
		if errors.Is(err, decimal.ErrOutOfRange) {
			return math.LegacyZeroDec(), false, nil
		}
		if err != nil {
			return math.LegacyDec{}, false, fmt.Errorf("summing liability %s: %w", coin.Denom, err)
		}
	}
	return total, true, nil
}

// cachedLiabilityValue charges a flat fee for the block-local liability lookup
// and runs the lookup itself against a free meter. Do not free-meter the query
// path: FundStatus calls nominalLiabilityValue directly so node query gas
// limits keep bounding its work.
func (k Keeper) cachedLiabilityValue(
	ctx context.Context,
	tobinTaxes []oracletypes.TobinTax,
	rates oracletypes.RateSet,
) (math.LegacyDec, bool, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.GasMeter().ConsumeGas(liabilityValuationGas, "treasury liability valuation")
	return k.liabilityValue(sdkCtx.WithGasMeter(storetypes.NewInfiniteGasMeter()), tobinTaxes, rates)
}

// liabilityValue returns the current block's aggregate stable liability.
// The preblocker primes it; the lazy scan below is the fallback. An incomplete
// valuation marks the whole block unavailable instead of retrying, because
// oracle rates cannot change until the next block's preblock. The snapshot is
// checked before the marker: the two keys are mutually exclusive by
// construction, and this order keeps the common primed path at a single
// transient read.
func (k Keeper) liabilityValue(
	ctx context.Context,
	tobinTaxes []oracletypes.TobinTax,
	rates oracletypes.RateSet,
) (math.LegacyDec, bool, error) {
	liability, found, err := k.loadLiabilitySnapshot(ctx)
	if err != nil {
		return math.LegacyDec{}, false, err
	}
	if found {
		return liability, true, nil
	}

	unavailable, err := k.liabilityUnavailable(ctx)
	if err != nil {
		return math.LegacyDec{}, false, err
	}
	if unavailable {
		return math.LegacyZeroDec(), false, nil
	}

	liability, complete, err := k.nominalLiabilityValue(ctx, tobinTaxes, rates)
	if err != nil {
		return math.LegacyDec{}, false, err
	}
	if !complete {
		if err := k.markLiabilityUnavailable(ctx); err != nil {
			return math.LegacyDec{}, false, err
		}
		return liability, false, nil
	}
	if err := k.storeLiabilitySnapshot(ctx, liability); err != nil {
		return math.LegacyDec{}, false, err
	}
	return liability, true, nil
}

func liabilityCoinValue(coin sdk.Coin, rates oracletypes.RateSet) (math.LegacyDec, error) {
	if coin.Denom == chain.NoahBaseDenom || coin.Amount.IsZero() {
		return math.LegacyZeroDec(), nil
	}

	converted, err := rates.Convert(sdk.NewDecCoinFromCoin(coin), chain.NoahBaseDenom)
	if err != nil {
		return math.LegacyDec{}, err
	}
	return converted.Amount, nil
}

func (k Keeper) loadLiabilitySnapshot(ctx context.Context) (math.LegacyDec, bool, error) {
	store := k.transientStoreService.OpenTransientStore(ctx)
	bz, err := store.Get(liabilitySnapshotKey)
	if err != nil {
		return math.LegacyDec{}, false, fmt.Errorf("getting cached liability: %w", err)
	}
	if bz == nil {
		return math.LegacyDec{}, false, nil
	}
	if len(bz) == 0 {
		return math.LegacyDec{}, false, fmt.Errorf("cached liability is empty")
	}

	var liability math.LegacyDec
	if err := liability.Unmarshal(bz); err != nil {
		return math.LegacyDec{}, false, fmt.Errorf("decoding cached liability: %w", err)
	}
	if liability.IsNil() || liability.IsNegative() || !liability.IsInValidRange() {
		return math.LegacyDec{}, false, fmt.Errorf("cached liability is invalid")
	}
	return liability, true, nil
}

func (k Keeper) markLiabilityUnavailable(ctx context.Context) error {
	store := k.transientStoreService.OpenTransientStore(ctx)
	if err := store.Set(liabilityUnavailableKey, []byte{0x01}); err != nil {
		return fmt.Errorf("marking liability valuation unavailable: %w", err)
	}
	return nil
}

func (k Keeper) liabilityUnavailable(ctx context.Context) (bool, error) {
	store := k.transientStoreService.OpenTransientStore(ctx)
	bz, err := store.Get(liabilityUnavailableKey)
	if err != nil {
		return false, fmt.Errorf("getting liability unavailability marker: %w", err)
	}
	return bz != nil, nil
}

func (k Keeper) storeLiabilitySnapshot(ctx context.Context, liability math.LegacyDec) error {
	if liability.IsNil() || liability.IsNegative() || !liability.IsInValidRange() {
		return fmt.Errorf("cannot cache invalid liability")
	}
	value, err := liability.Marshal()
	if err != nil {
		return fmt.Errorf("encoding cached liability: %w", err)
	}

	store := k.transientStoreService.OpenTransientStore(ctx)
	if err := store.Set(liabilitySnapshotKey, value); err != nil {
		return fmt.Errorf("setting cached liability: %w", err)
	}
	return nil
}
