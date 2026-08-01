package keeper

import (
	"context"
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
// it, a typical valuation reads about 1,069 gas and the largest storable
// LegacyDec about 1,294 (the value carries a one-byte completeness flag ahead
// of the marshalled Dec). Recalibrate if the app customises store gas configs,
// or if a second key is ever added, since two reads would cost about 2,015.
//
// The lazy scan reached on an unvalued block is deliberately not covered: it is
// bounded to once per block, and it is the path simulation always takes, where
// gas is not limiting. A constant keeps swap gas position-independent within a
// block and independent of the oracle whitelist size.
const liabilityValuationGas = 2_000

// liabilityValuationKey holds the block's claimable-liability snapshot: one
// completeness flag byte followed by a marshalled LegacyDec. The value is the
// claimable aggregate — the liability that can currently redeem — and the flag
// records whether every recognised liability was valued or some unclaimable
// exposure was excluded and disclosed. Rates are fixed at preblock, so neither
// the value's inputs nor the flag can change within the block, and the single
// key keeps every lookup one read.
var liabilityValuationKey = []byte{0x01}

const (
	// liabilitySnapshotIncomplete marks a snapshot whose block excluded some
	// recognised exposure from the claimable aggregate — a member without a
	// fresh feed, or suspended supply without a plan. The excluded supply
	// cannot itself redeem while in that state, so the aggregate stays an
	// honest denominator for every draw that can actually arrive.
	liabilitySnapshotIncomplete byte = 0x00
	// liabilitySnapshotComplete marks a snapshot that valued every recognised
	// liability.
	liabilitySnapshotComplete byte = 0x01
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

	liability, complete, found, err := k.loadLiabilityValuation(ctx)
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

	return k.storeLiabilitySnapshot(ctx, liability, complete)
}

// PrimeLiabilitySnapshot values the claimable liability once for the block.
// The preblocker calls it after oracle price application, feed advancement,
// and asset lifecycle completions, so transaction-time callers always find a
// valuation built from post-completion statuses and never rescan. Hard state
// errors fail the block; an incomplete valuation is an expected degraded mode
// — the snapshot still carries the claimable aggregate, with the flag
// recording that some unclaimable exposure was excluded and disclosed.
func (k Keeper) PrimeLiabilitySnapshot(ctx context.Context) error {
	partition, err := k.liabilityPartitionValue(ctx, nil)
	if err != nil {
		return err
	}

	recognised, err := partition.recognizedNoah()
	if err != nil {
		return err
	}
	return k.storeLiabilitySnapshot(ctx, recognised, partition.complete)
}

// liabilityPartition is one block's liability report, partitioned by asset
// lifecycle status.
type liabilityPartition struct {
	pricedNoah     math.LegacyDec
	settlementNoah math.LegacyDec
	// stale enumerates members whose feed is unavailable, and staleNoah values
	// them at their last known rate. The pair is separated from pricedNoah so
	// the figure is auditable rather than folded away, and both are empty or
	// zero unless the partition is incomplete.
	//
	// staleNoah can be zero while stale is not: a member the Oracle has never
	// priced has no rate to count it by. That state is a genesis
	// misconfiguration rather than anything a running chain reaches, since a
	// feed cannot be removed under a live asset and an asset cannot activate
	// before its first rate.
	stale      []sdk.Coin
	staleNoah  math.LegacyDec
	untrusted  []sdk.Coin
	writtenOff []types.WrittenOffExposure
	complete   bool
}

// recognizedNoah is the claimable aggregate: every liability this block could
// put a number against, and the denominator redemption coverage divides by. It
// is meaningful whether or not the partition is complete. complete says whether
// every recognised liability was valued at a rate good enough to transact at,
// which is the stronger property fund targets need.
//
// A member whose feed is unavailable is counted at its last known rate rather
// than dropped. Its holders cannot redeem this block, but their claim on the
// funds is untouched — a stale feed says nothing about the obligation — and
// leaving them out would raise everyone else's coverage share for as long as
// the outage lasts, paying away the excluded holders' share to whoever happens
// to be transacting. That is the difference from the two lists: a write-off
// extinguishes the obligation and an untrusted suspension withdraws the
// protocol's rate for it, so both are genuinely outside the aggregate, while an
// absent feed is only absent evidence.
func (p liabilityPartition) recognizedNoah() (math.LegacyDec, error) {
	valued, err := decimal.Add(p.pricedNoah, p.settlementNoah)
	if err != nil {
		return math.LegacyDec{}, err
	}
	return decimal.Add(valued, p.staleNoah)
}

// liabilityPartitionValue folds the asset registry into the block's liability
// partition, classifying each holding by its pricing verdict.
func (k Keeper) liabilityPartitionValue(ctx context.Context, rates oracletypes.RateSet) (liabilityPartition, error) {
	assets, err := k.assetKeeper.ListAssets(ctx)
	if err != nil {
		return liabilityPartition{}, fmt.Errorf("listing assets for liability partition: %w", err)
	}

	partition := liabilityPartition{
		pricedNoah:     math.LegacyZeroDec(),
		settlementNoah: math.LegacyZeroDec(),
		stale:          []sdk.Coin{},
		staleNoah:      math.LegacyZeroDec(),
		untrusted:      []sdk.Coin{},
		writtenOff:     []types.WrittenOffExposure{},
		complete:       true,
	}

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

	pricings, err := k.assetKeeper.Pricings(ctx, rates, held...)
	if err != nil {
		return liabilityPartition{}, fmt.Errorf("pricing aggregate liability: %w", err)
	}

	for _, holding := range holdings {
		pricing := pricings[holding.supply.Denom]
		switch {
		case pricing.Priced && pricing.Source == assettypes.PriceSourceSettlement:
			partition.settlementNoah, err = accrueLiability(
				partition.settlementNoah,
				holding.supply,
				pricings,
			)
			if err != nil {
				return liabilityPartition{}, err
			}
		case pricing.Priced:
			partition.pricedNoah, err = accrueLiability(
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
			// Disclosed and marked incomplete either way: the flag reports that
			// no fresh rate stood behind part of the total, which is what fund
			// targets must not be sized on, independently of whether the last
			// known rate let the aggregate keep counting this supply.
			partition.stale = append(partition.stale, holding.supply)
			partition.complete = false
			if pricing.LastRate.IsNil() {
				// Never priced, so there is no evidence to count it by. The
				// exclusion is honest here in a way it is not for a member with
				// a history.
				continue
			}
			partition.staleNoah, err = accrueStaleLiability(
				partition.staleNoah,
				holding.supply,
				pricings,
			)
			if err != nil {
				return liabilityPartition{}, err
			}
		default:
			// Pending, retired, and unrecognised supply is invisible to the
			// liability report.
		}
	}

	return partition, nil
}

// accrueLiability adds the NOAH value of supply to a liability bucket, folding
// both priced buckets through one conversion.
func accrueLiability(total math.LegacyDec, supply sdk.Coin, pricings assettypes.DenomPricings) (math.LegacyDec, error) {
	return accrue(total, supply, pricings.Convert)
}

// accrueStaleLiability adds supply valued at its last known rate to the stale
// bucket. It is a separate entry point rather than a flag on accrueLiability so
// that reading a rate the freshness gate rejected is visible at the call site,
// and so the one place it is legitimate stays one place.
func accrueStaleLiability(total math.LegacyDec, supply sdk.Coin, pricings assettypes.DenomPricings) (math.LegacyDec, error) {
	return accrue(total, supply, pricings.ConvertLastKnown)
}

func accrue(
	total math.LegacyDec,
	supply sdk.Coin,
	convert func(sdk.DecCoin, string) (sdk.DecCoin, error),
) (math.LegacyDec, error) {
	converted, err := convert(sdk.NewDecCoinFromCoin(supply), chain.NoahBaseDenom)
	if err != nil {
		return total, fmt.Errorf("valuing liability %s: %w", supply.Denom, err)
	}

	sum, err := decimal.Add(total, converted.Amount)
	if err != nil {
		return total, fmt.Errorf("summing liability %s: %w", supply.Denom, err)
	}

	return sum, nil
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

// cachedLiabilityValue returns the current block's claimable liability and
// whether it covers every recognised liability, charging a flat fee and
// evaluating the lookup itself against a free meter. Do not free-meter the
// query path: FundStatus builds the partition directly so node query gas limits
// keep bounding its work.
func (k Keeper) cachedLiabilityValue(ctx context.Context, rates oracletypes.RateSet) (math.LegacyDec, bool, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.GasMeter().ConsumeGas(liabilityValuationGas, "treasury liability valuation")

	freeCtx := sdkCtx.WithGasMeter(storetypes.NewInfiniteGasMeter())

	liability, complete, found, err := k.loadLiabilityValuation(freeCtx)
	if err != nil {
		return math.LegacyDec{}, false, err
	}
	if found {
		return liability, complete, nil
	}

	partition, err := k.liabilityPartitionValue(freeCtx, rates)
	if err != nil {
		return math.LegacyDec{}, false, err
	}
	recognised, err := partition.recognizedNoah()
	if err != nil {
		return math.LegacyDec{}, false, err
	}
	if err := k.storeLiabilitySnapshot(freeCtx, recognised, partition.complete); err != nil {
		return math.LegacyDec{}, false, err
	}
	return recognised, partition.complete, nil
}

// loadLiabilityValuation reads the block's recorded snapshot: the claimable
// aggregate, its completeness flag, and whether the block has valued at all.
func (k Keeper) loadLiabilityValuation(ctx context.Context) (math.LegacyDec, bool, bool, error) {
	store := k.transientStoreService.OpenTransientStore(ctx)
	bz, err := store.Get(liabilityValuationKey)
	if err != nil {
		return math.LegacyDec{}, false, false, fmt.Errorf("getting cached liability: %w", err)
	}
	if bz == nil {
		return math.LegacyDec{}, false, false, nil
	}
	if len(bz) < 2 {
		return math.LegacyDec{}, false, false, fmt.Errorf("cached liability is truncated")
	}
	var complete bool
	switch bz[0] {
	case liabilitySnapshotComplete:
		complete = true
	case liabilitySnapshotIncomplete:
		complete = false
	default:
		return math.LegacyDec{}, false, false, fmt.Errorf("cached liability flag %#x is invalid", bz[0])
	}

	var liability math.LegacyDec
	if err := liability.Unmarshal(bz[1:]); err != nil {
		return math.LegacyDec{}, false, false, fmt.Errorf("decoding cached liability: %w", err)
	}
	if liability.IsNil() || liability.IsNegative() || !liability.IsInValidRange() {
		return math.LegacyDec{}, false, false, fmt.Errorf("cached liability is invalid")
	}
	return liability, complete, true, nil
}

func (k Keeper) storeLiabilitySnapshot(ctx context.Context, liability math.LegacyDec, complete bool) error {
	if liability.IsNil() || liability.IsNegative() || !liability.IsInValidRange() {
		return fmt.Errorf("cannot cache invalid liability")
	}
	encoded, err := liability.Marshal()
	if err != nil {
		return fmt.Errorf("encoding cached liability: %w", err)
	}
	flag := liabilitySnapshotIncomplete
	if complete {
		flag = liabilitySnapshotComplete
	}
	value := make([]byte, 0, len(encoded)+1)
	value = append(value, flag)
	value = append(value, encoded...)

	store := k.transientStoreService.OpenTransientStore(ctx)
	if err := store.Set(liabilityValuationKey, value); err != nil {
		return fmt.Errorf("setting cached liability: %w", err)
	}
	return nil
}
