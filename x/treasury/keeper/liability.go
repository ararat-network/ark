package keeper

import (
	"bytes"
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
// aggregate-liability valuation, whatever it finds. The preblocker performs the
// real scan as unmetered block work, so the honest marginal cost is the single
// transient read that every lookup performs.
//
// Transient stores opened through the store service are metered with the KV gas
// config rather than the cheaper transient one, because OpenTransientStore
// calls Context.KVStore; that is 1,000 flat plus 3 per key and value byte.
// Measured against that config: a typical valuation reads 1,066 gas, an
// unvalued block 1,003, and the largest storable LegacyDec 1,291 (upperLimit is
// raw 2^256 * 10^18 - 1, which Marshal emits as 96 bytes of decimal text). So
// 2,000 strictly covers every lookup with at least 709 gas of headroom.
// Recalibrate if the app customises store gas configs, or if a second key is
// ever added, since two reads would cost about 2,009.
//
// The lazy scan reached on an unvalued block is deliberately not covered: it is
// bounded to once per block by the recorded valuation, and it is the path
// simulation always takes, where gas is not limiting. Charging a constant keeps
// swap gas position-independent within a block and independent of the oracle
// whitelist size, and simulation, which always runs with an empty transient
// store, quotes exactly what execution consumes.
const liabilityValuationGas = 2_000

var (
	// liabilityValuationKey holds the block's aggregate liability valuation.
	// Its value is either a marshalled LegacyDec, meaning the valuation
	// succeeded, or liabilityUnavailableSentinel, meaning the block could not
	// price every listed denom. Rates are fixed at preblock, so an unavailable
	// valuation cannot become available within the block; recording it stops
	// later callers from rescanning until the transient store resets at commit.
	//
	// Both states share one key so that every transaction-time lookup is a
	// single transient read and so that they are mutually exclusive by
	// construction rather than by a rule each writer has to honour.
	liabilityValuationKey = []byte{0x01}

	// liabilityUnavailableSentinel cannot collide with a stored valuation:
	// LegacyDec.Marshal emits big.Int decimal text, which is always non-empty
	// ASCII digits with an optional leading minus and never contains NUL.
	liabilityUnavailableSentinel = []byte{0x00}
)

// liabilityValuationState describes what the current block has recorded about
// its aggregate liability.
type liabilityValuationState uint8

const (
	// liabilityUnvalued means the block has not been valued yet.
	liabilityUnvalued liabilityValuationState = iota
	// liabilityUnavailable means the block was valued but could not price every
	// listed denom.
	liabilityUnavailable
	// liabilityAvailable means the block has a usable aggregate liability.
	liabilityAvailable
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

	liability, state, err := k.loadLiabilityValuation(ctx)
	if err != nil {
		return err
	}
	if state != liabilityAvailable {
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
// and vote-target advancement, so transaction-time callers always find a
// recorded valuation and never rescan. Hard state errors propagate and fail the
// block; an incomplete valuation (stale or missing rates) is an expected
// degraded mode and is recorded as unavailable.
func (k Keeper) PrimeLiabilitySnapshot(ctx context.Context) error {
	tobinTaxes, err := k.oracleKeeper.GetTobinTaxes(ctx)
	if err != nil {
		return fmt.Errorf("getting Tobin taxes: %w", err)
	}
	liability, complete, err := k.nominalLiabilityValue(ctx, tobinTaxes, nil)
	if err != nil {
		return err
	}

	// Both outcomes write the same key, so priming is authoritative for the
	// block without having to retract anything an earlier prime left behind.
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

// cachedLiabilityValue returns the current block's aggregate stable liability.
// It charges a flat fee and evaluates the lookup itself against a free meter.
// Do not free-meter the query path: FundStatus calls nominalLiabilityValue
// directly so node query gas limits keep bounding its work.
//
// The preblocker primes it; the lazy scan below is the fallback. An incomplete
// valuation records the whole block as unavailable instead of retrying, because
// oracle rates cannot change until the next block's preblock.
func (k Keeper) cachedLiabilityValue(
	ctx context.Context,
	tobinTaxes []oracletypes.TobinTax,
	rates oracletypes.RateSet,
) (math.LegacyDec, bool, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.GasMeter().ConsumeGas(liabilityValuationGas, "treasury liability valuation")

	// Every access below must use freeCtx rather than ctx, or the store reads
	// and writes are metered again and the flat charge stops being flat.
	freeCtx := sdkCtx.WithGasMeter(storetypes.NewInfiniteGasMeter())

	liability, state, err := k.loadLiabilityValuation(freeCtx)
	if err != nil {
		return math.LegacyDec{}, false, err
	}
	switch state {
	case liabilityAvailable:
		return liability, true, nil
	case liabilityUnavailable:
		return math.LegacyZeroDec(), false, nil
	}

	liability, complete, err := k.nominalLiabilityValue(freeCtx, tobinTaxes, rates)
	if err != nil {
		return math.LegacyDec{}, false, err
	}
	if !complete {
		if err := k.markLiabilityUnavailable(freeCtx); err != nil {
			return math.LegacyDec{}, false, err
		}
		return liability, false, nil
	}
	if err := k.storeLiabilitySnapshot(freeCtx, liability); err != nil {
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

// loadLiabilityValuation reads the block's recorded valuation. The returned
// liability is meaningful only when the state is liabilityAvailable.
func (k Keeper) loadLiabilityValuation(ctx context.Context) (math.LegacyDec, liabilityValuationState, error) {
	store := k.transientStoreService.OpenTransientStore(ctx)
	bz, err := store.Get(liabilityValuationKey)
	if err != nil {
		return math.LegacyDec{}, liabilityUnvalued, fmt.Errorf("getting cached liability: %w", err)
	}
	if bz == nil {
		return math.LegacyDec{}, liabilityUnvalued, nil
	}
	if len(bz) == 0 {
		return math.LegacyDec{}, liabilityUnvalued, fmt.Errorf("cached liability is empty")
	}
	if bytes.Equal(bz, liabilityUnavailableSentinel) {
		return math.LegacyDec{}, liabilityUnavailable, nil
	}

	var liability math.LegacyDec
	if err := liability.Unmarshal(bz); err != nil {
		return math.LegacyDec{}, liabilityUnvalued, fmt.Errorf("decoding cached liability: %w", err)
	}
	if liability.IsNil() || liability.IsNegative() || !liability.IsInValidRange() {
		return math.LegacyDec{}, liabilityUnvalued, fmt.Errorf("cached liability is invalid")
	}
	return liability, liabilityAvailable, nil
}

// markLiabilityUnavailable records that this block cannot value liability. It
// overwrites any valuation already stored, so a later prime always wins.
func (k Keeper) markLiabilityUnavailable(ctx context.Context) error {
	store := k.transientStoreService.OpenTransientStore(ctx)
	if err := store.Set(liabilityValuationKey, liabilityUnavailableSentinel); err != nil {
		return fmt.Errorf("marking liability valuation unavailable: %w", err)
	}
	return nil
}

// storeLiabilitySnapshot records a usable aggregate liability for this block.
// It overwrites any unavailable marking already stored.
func (k Keeper) storeLiabilitySnapshot(ctx context.Context, liability math.LegacyDec) error {
	if liability.IsNil() || liability.IsNegative() || !liability.IsInValidRange() {
		return fmt.Errorf("cannot cache invalid liability")
	}
	value, err := liability.Marshal()
	if err != nil {
		return fmt.Errorf("encoding cached liability: %w", err)
	}
	if bytes.Equal(value, liabilityUnavailableSentinel) {
		return fmt.Errorf("encoded liability collides with the unavailable sentinel")
	}

	store := k.transientStoreService.OpenTransientStore(ctx)
	if err := store.Set(liabilityValuationKey, value); err != nil {
		return fmt.Errorf("setting cached liability: %w", err)
	}
	return nil
}
