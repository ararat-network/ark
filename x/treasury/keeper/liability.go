package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	"ark/pkg/decimal"
	assettypes "ark/x/asset/types"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/types"
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

// PrimeLiabilitySnapshot values the claimable liability once for the block,
// called from Treasury's BeginBlocker after every PreBlocker has applied oracle
// prices, so transaction-time callers never rescan. Reusing that snapshot is
// sound only because its four inputs are either tracked — supply, via
// RecordSupplyChange — or cannot move mid-block: lifecycle transitions and
// settlement plans are governance messages run in the EndBlocker, the emergency
// committee calls InvalidateRegistryCache, and only the preblock vote processor
// writes rates. Any new path that moves supply across the partition mid-block
// reintroduces that hazard; hard state errors fail the block, while an
// incomplete valuation is an expected degraded mode disclosed by
// EventLiabilityIncomplete.
func (k Keeper) PrimeLiabilitySnapshot(ctx context.Context) error {
	partition, err := k.liabilityPartitionValue(ctx, nil)
	if err != nil {
		return err
	}

	recognised, err := partition.recognizedNoah()
	if err != nil {
		return err
	}
	if err := k.storeLiabilitySnapshot(ctx, recognised, partition.complete); err != nil {
		return err
	}

	// A complete valuation discloses nothing: an event every block would bury
	// the blocks that matter. cachedLiabilityValue repeats this on the same
	// terms — see the note there.
	if partition.complete {
		return nil
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventLiabilityIncomplete{
		ClaimableLiability:       chain.NoahDecCoin(recognised),
		StaleMemberSupply:        partition.stale,
		UntrustedSuspendedSupply: partition.untrusted,
	}); err != nil {
		return fmt.Errorf("emitting Treasury incomplete liability event: %w", err)
	}
	return nil
}

// InvalidateRegistryCache implements assettypes.RegistryCacheInvalidator by
// dropping the block's primed valuation. The snapshot tracks mint and burn but
// not lifecycle status, so a suspension landing inside the block would leave
// RouteExpansion sizing targets on the pre-failure figure — burning NOAH and
// committing principal to the Buffer, both irreversible, in the block an asset
// just failed. Dropping the entry is enough: cachedLiabilityValue rebuilds on a
// miss, the same path a block that never primed takes.
func (k Keeper) InvalidateRegistryCache(ctx context.Context) error {
	if err := k.transientStoreService.OpenTransientStore(ctx).
		Delete(liabilityValuationKey); err != nil {
		return fmt.Errorf("invalidating cached liability: %w", err)
	}

	return nil
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

// liabilityPartition is one block's liability report, partitioned by asset
// lifecycle status.
type liabilityPartition struct {
	pricedNoah     math.LegacyDec
	settlementNoah math.LegacyDec
	staleNoah      math.LegacyDec
	stale          []sdk.Coin
	untrusted      []sdk.Coin
	writtenOff     []types.WrittenOffExposure
	complete       bool
}

// recognizedNoah is the claimable aggregate: every liability this block could
// put a number against, and the denominator redemption coverage divides by,
// meaningful whether or not the partition is complete. A member whose feed is
// unavailable is counted at its last known rate rather than dropped, because
// leaving it out would pay its holders' coverage share away to whoever happens
// to be transacting during the outage. A write-off extinguishes the obligation
// and an untrusted suspension withdraws the protocol's rate, so both are
// genuinely outside the aggregate; an absent feed is only absent evidence.
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
	denoms, pricings, err := k.assetKeeper.PricedAssets(ctx, rates)
	if err != nil {
		return liabilityPartition{}, fmt.Errorf("pricing aggregate liability: %w", err)
	}

	partition := liabilityPartition{
		pricedNoah:     math.LegacyZeroDec(),
		settlementNoah: math.LegacyZeroDec(),
		staleNoah:      math.LegacyZeroDec(),
		stale:          []sdk.Coin{},
		untrusted:      []sdk.Coin{},
		writtenOff:     []types.WrittenOffExposure{},
		complete:       true,
	}

	// Supply is filtered after pricing rather than before so the registry is
	// read once: naming denominations back to the fold would read every record
	// a second time each block. Pricing a zero-supply member costs nothing, and
	// the partition ignores it either way.
	for _, denom := range denoms {
		supply := k.bankKeeper.GetSupply(ctx, denom)
		if !supply.Amount.IsPositive() {
			continue
		}
		verdict := pricings[denom]
		switch {
		case verdict.IsPriced() && verdict.Source == assettypes.PriceSource_PRICE_SOURCE_SETTLEMENT:
			partition.settlementNoah, err = accrueLiability(
				partition.settlementNoah,
				supply,
				pricings.Convert,
			)
			if err != nil {
				return liabilityPartition{}, err
			}
		case verdict.IsPriced():
			partition.pricedNoah, err = accrueLiability(
				partition.pricedNoah,
				supply,
				pricings.Convert,
			)
			if err != nil {
				return liabilityPartition{}, err
			}
		case verdict.Reason == assettypes.UnpricedReason_UNPRICED_REASON_UNTRUSTED:
			partition.untrusted = append(partition.untrusted, supply)
			partition.complete = false
		case verdict.Reason == assettypes.UnpricedReason_UNPRICED_REASON_WRITTEN_OFF:
			partition.writtenOff = append(partition.writtenOff, types.WrittenOffExposure{
				OutstandingSupply: supply,
				WriteOffVersion:   verdict.Asset.Version,
			})
		case verdict.Reason == assettypes.UnpricedReason_UNPRICED_REASON_FEED_UNAVAILABLE:
			// Marked incomplete either way: no fresh rate stood behind part of
			// the total, which is what fund targets must not be sized on,
			// whether or not the last known rate kept counting this supply.
			partition.stale = append(partition.stale, supply)
			partition.complete = false
			if verdict.LastRate == nil {
				// Never priced, so there is no evidence to count it by.
				continue
			}
			partition.staleNoah, err = accrueLiability(
				partition.staleNoah,
				supply,
				pricings.ConvertLastKnown,
			)
			if err != nil {
				return liabilityPartition{}, err
			}
		default:
			// Retired and unrecognised supply is invisible to the liability
			// report.
		}
	}

	return partition, nil
}

// accrueLiability adds the NOAH value of supply to a liability bucket. The
// conversion is a parameter so each call site names the rate it values at:
// ConvertLastKnown reads a rate the freshness gate rejected, legitimate in
// exactly one branch.
func accrueLiability(total math.LegacyDec, supply sdk.Coin, convert func(sdk.DecCoin, string) (sdk.DecCoin, error)) (math.LegacyDec, error) {
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
