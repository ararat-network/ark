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
	assettypes "ark/x/asset/types"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/types"
)

// liabilityValuationGas is the flat gas charged for one transaction-time
// aggregate-liability valuation. The preblocker does the real scan as unmetered
// block work, so the honest marginal cost is the single transient read.
//
// Transient stores opened through the store service are metered with the KV gas
// config rather than the cheaper transient one, because OpenTransientStore calls
// Context.KVStore: 1,000 flat plus 3 per key and value byte. Measured against
// it, a typical valuation reads 1,066 gas and the largest storable LegacyDec
// 1,291. Recalibrate if the app customises store gas configs, or if a second key
// is ever added, since two reads would cost about 2,009.
//
// The lazy scan reached on an unvalued block is deliberately not covered: it is
// bounded to once per block, and it is the path simulation always takes, where
// gas is not limiting. A constant keeps swap gas position-independent within a
// block and independent of the oracle whitelist size.
const liabilityValuationGas = 2_000

var (
	// liabilityValuationKey holds the block's valuation: either a marshalled
	// LegacyDec, or liabilityUnavailableSentinel when the block could not price
	// every listed denom. Rates are fixed at preblock, so an unavailable
	// valuation cannot become available within the block, and recording it stops
	// later callers from rescanning. Both states share one key so that every
	// lookup is a single read and they are mutually exclusive by construction.
	liabilityValuationKey = []byte{0x01}

	// liabilityUnavailableSentinel cannot collide with a stored valuation:
	// LegacyDec.Marshal emits big.Int decimal text, which never contains NUL.
	liabilityUnavailableSentinel = []byte{0x00}
)

// liabilityValuationState describes what the current block has recorded about
// its aggregate liability.
type liabilityValuationState uint8

const (
	liabilityUnvalued liabilityValuationState = iota
	// liabilityUnavailable means the block was valued but could not price every
	// listed denom.
	liabilityUnavailable
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

// PrimeLiabilitySnapshot values the aggregate recognised liability once for the
// block. The preblocker calls it after oracle price application, feed
// advancement, and asset lifecycle completions, so transaction-time callers
// always find a valuation built from post-completion statuses and never rescan.
// Hard state errors fail the block; an incomplete valuation is an expected
// degraded mode and is recorded as unavailable.
func (k Keeper) PrimeLiabilitySnapshot(ctx context.Context) error {
	partition, err := k.liabilityPartitionValue(ctx, nil)
	if err != nil {
		return err
	}

	if !partition.complete {
		return k.markLiabilityUnavailable(ctx)
	}
	recognised, err := partition.recognizedNoah()
	if err != nil {
		return k.markLiabilityUnavailable(ctx)
	}
	return k.storeLiabilitySnapshot(ctx, recognised)
}

// liabilityPartition is one block's liability report, partitioned by asset
// lifecycle status. The two valued buckets carry recognised liability; the two
// lists are disclosure. complete reports whether every recognised liability was
// valued — untrusted suspended exposure is recognised but valuable by no honest
// rate, so its presence alone makes the total unavailable.
type liabilityPartition struct {
	pricedNoah     math.LegacyDec
	settlementNoah math.LegacyDec
	untrusted      []sdk.Coin
	writtenOff     []types.WrittenOffExposure
	complete       bool
}

// recognizedNoah is the aggregate every fund target derives from. Meaningful
// only when the partition is complete.
func (p liabilityPartition) recognizedNoah() (math.LegacyDec, error) {
	return decimal.Add(p.pricedNoah, p.settlementNoah)
}

// liabilityPartitionValue folds the asset registry into the block's liability
// partition, classifying each holding by its pricing verdict. A valuation
// failure zeroes the affected bucket and marks the partition incomplete rather
// than aborting, because the disclosure lists never depend on rates and the
// degraded report is what the stress scenario needs. Hard state errors
// propagate.
func (k Keeper) liabilityPartitionValue(ctx context.Context, rates oracletypes.RateSet) (liabilityPartition, error) {
	assets, err := k.assetKeeper.ListAssets(ctx)
	if err != nil {
		return liabilityPartition{}, fmt.Errorf("listing assets for liability partition: %w", err)
	}

	partition := liabilityPartition{
		pricedNoah:     math.LegacyZeroDec(),
		settlementNoah: math.LegacyZeroDec(),
		untrusted:      []sdk.Coin{},
		writtenOff:     []types.WrittenOffExposure{},
		complete:       true,
	}
	pricedComplete := true
	settlementComplete := true

	type liabilityHolding struct {
		asset  assettypes.Asset
		supply sdk.Coin
	}
	holdings := make([]liabilityHolding, 0, len(assets))
	held := make([]string, 0, len(assets))
	for _, asset := range assets {
		supply := k.bankKeeper.GetSupply(ctx, asset.Denom)
		if !supply.Amount.IsPositive() {
			continue
		}
		holdings = append(holdings, liabilityHolding{asset: asset, supply: supply})
		held = append(held, asset.Denom)
	}

	// rates belongs to the caller — Market passes its in-flight quote rates
	// straight through — so it is an overlay the registry captures around,
	// never a map this fold grows behind another module's back.
	pricings, err := k.assetKeeper.Pricings(ctx, rates, held...)
	if err != nil {
		return liabilityPartition{}, fmt.Errorf("pricing aggregate liability: %w", err)
	}

	for _, holding := range holdings {
		pricing := pricings[holding.supply.Denom]
		switch {
		case pricing.Priced && pricing.Source == assettypes.PriceSourceSettlement:
			// Once the bucket is unvaluable it stays zeroed: accumulating later
			// plans onto a zeroed total would report a number that is neither
			// whole nor honestly partial.
			if !settlementComplete {
				continue
			}
			// Plan rates are quoted per one NOAH like oracle rates, so this is
			// the same arithmetic that values a member.
			partition.settlementNoah, settlementComplete, err = accrueLiability(
				partition.settlementNoah,
				holding.supply,
				pricings,
			)
			if err != nil {
				return liabilityPartition{}, err
			}
		case pricing.Priced:
			if !pricedComplete {
				continue
			}
			partition.pricedNoah, pricedComplete, err = accrueLiability(
				partition.pricedNoah,
				holding.supply,
				pricings,
			)
			if err != nil {
				return liabilityPartition{}, err
			}
		case pricing.Reason == assettypes.UnpricedUntrusted:
			partition.untrusted = append(partition.untrusted, holding.supply)
			partition.complete = false
		case pricing.Reason == assettypes.UnpricedWrittenOff:
			partition.writtenOff = append(partition.writtenOff, types.WrittenOffExposure{
				OutstandingSupply: holding.supply,
				WriteOffVersion:   holding.asset.Version,
			})
		case pricing.Reason == assettypes.UnpricedFeedUnavailable:
			pricedComplete = false
		default:
			// Pending, retired, and unrecognised supply is invisible to the
			// liability report.
		}
	}
	if !settlementComplete {
		partition.settlementNoah = math.LegacyZeroDec()
		partition.complete = false
	}
	if !pricedComplete {
		partition.pricedNoah = math.LegacyZeroDec()
		partition.complete = false
	}

	return partition, nil
}

// accrueLiability adds the NOAH value of supply to a liability bucket, folding
// both priced buckets through one conversion.
func accrueLiability(total math.LegacyDec, supply sdk.Coin, pricings assettypes.DenomPricings) (math.LegacyDec, bool, error) {
	converted, err := pricings.Convert(sdk.NewDecCoinFromCoin(supply), chain.NoahBaseDenom)
	if err != nil {
		if !isValuationUnavailable(err) {
			return total, false, fmt.Errorf("valuing liability %s: %w", supply.Denom, err)
		}
		return total, false, nil
	}

	sum, err := decimal.Add(total, converted.Amount)
	if errors.Is(err, decimal.ErrOutOfRange) {
		return total, false, nil
	}
	if err != nil {
		return total, false, fmt.Errorf("summing liability %s: %w", supply.Denom, err)
	}

	return sum, true, nil
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

// cachedLiabilityValue returns the current block's aggregate recognised
// liability, charging a flat fee and evaluating the lookup itself against a free
// meter. Do not free-meter the query path: FundStatus builds the partition
// directly so node query gas limits keep bounding its work.
//
// The preblocker primes it; the scan below is the fallback. An incomplete
// valuation records the whole block as unavailable instead of retrying, because
// neither oracle rates nor asset statuses can change until the next preblock.
func (k Keeper) cachedLiabilityValue(ctx context.Context, rates oracletypes.RateSet) (math.LegacyDec, bool, error) {
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

	partition, err := k.liabilityPartitionValue(freeCtx, rates)
	if err != nil {
		return math.LegacyDec{}, false, err
	}
	recognised := math.LegacyZeroDec()
	complete := partition.complete
	if complete {
		recognised, err = partition.recognizedNoah()
		if errors.Is(err, decimal.ErrOutOfRange) {
			recognised = math.LegacyZeroDec()
			complete = false
		} else if err != nil {
			return math.LegacyDec{}, false, err
		}
	}
	if !complete {
		if err := k.markLiabilityUnavailable(freeCtx); err != nil {
			return math.LegacyDec{}, false, err
		}
		return recognised, false, nil
	}
	if err := k.storeLiabilitySnapshot(freeCtx, recognised); err != nil {
		return math.LegacyDec{}, false, err
	}
	return recognised, true, nil
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

func (k Keeper) markLiabilityUnavailable(ctx context.Context) error {
	store := k.transientStoreService.OpenTransientStore(ctx)
	if err := store.Set(liabilityValuationKey, liabilityUnavailableSentinel); err != nil {
		return fmt.Errorf("marking liability valuation unavailable: %w", err)
	}
	return nil
}
