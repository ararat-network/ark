package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/decimal"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	reservetypes "github.com/ararat-network/ark/x/reserve/types"
	"github.com/ararat-network/ark/x/treasury/types"
)

// liabilityPartition is one block's liability report, partitioned by asset
// lifecycle status. Which aggregate a consumer divides by is not a preference:
// a flow takes net, a bound on a committee act takes recognised.
type liabilityPartition struct {
	priced     math.LegacyDec
	settlement math.LegacyDec
	stale      math.LegacyDec
	selfHeld   math.LegacyDec

	staleSupply     []sdk.Coin
	untrustedSupply []sdk.Coin
	selfHeldSupply  []sdk.Coin
	writtenOff      []types.WrittenOffExposure

	complete bool
}

// recognised sums priced, settlement-priced, and last-known-rate liability before self-held
// netting. Written-off and untrusted suspended exposure stays outside; stale obligations remain
// counted where evidence exists.
func (p liabilityPartition) recognised() (math.LegacyDec, error) {
	valued, err := decimal.Add(p.priced, p.settlement)
	if err != nil {
		return math.LegacyDec{}, err
	}
	return decimal.Add(valued, p.stale)
}

// net subtracts Reserve-held liability for flow calculations; committee bounds retain recognised
// liability. Matching membership and rates plus balance <= supply make a negative result
// unreachable; the checked error remains a backstop.
func (p liabilityPartition) net() (math.LegacyDec, error) {
	recognised, err := p.recognised()
	if err != nil {
		return math.LegacyDec{}, err
	}
	net, err := decimal.Sub(recognised, p.selfHeld)
	if err != nil {
		return math.LegacyDec{}, fmt.Errorf("netting self-held supply from claimable liability: %w", err)
	}
	if net.IsNegative() {
		return math.LegacyDec{}, fmt.Errorf(
			"self-held liability %s exceeds the claimable aggregate %s",
			p.selfHeld,
			recognised,
		)
	}
	return net, nil
}

// liabilityPartitionValue folds the asset registry into the block's liability
// partition, classifying each holding by its pricing verdict.
func (k Keeper) liabilityPartitionValue(ctx context.Context) (liabilityPartition, error) {
	denoms, pricings, err := k.assetKeeper.PricedAssets(ctx)
	if err != nil {
		return liabilityPartition{}, fmt.Errorf("pricing aggregate liability: %w", err)
	}

	// Only the decimals need seeding — LegacyDec's zero value is nil, while nil
	// lists append and marshal as empty.
	partition := liabilityPartition{
		priced:     math.LegacyZeroDec(),
		settlement: math.LegacyZeroDec(),
		stale:      math.LegacyZeroDec(),
		selfHeld:   math.LegacyZeroDec(),
		complete:   true,
	}

	// Supply is filtered after pricing so the registry is read once; pricing a
	// zero-supply member costs nothing.
	for _, denom := range denoms {
		supply := k.bankKeeper.GetSupply(ctx, denom)
		if !supply.Amount.IsPositive() {
			continue
		}
		verdict := pricings[denom]
		switch {
		case verdict.IsPriced() && verdict.Source == assettypes.PriceSource_PRICE_SOURCE_SETTLEMENT:
			partition.settlement, err = accrueLiability(
				partition.settlement,
				supply,
				pricings.Convert,
			)
			if err != nil {
				return liabilityPartition{}, err
			}
			if err := k.accrueSelfHeld(ctx, &partition, denom, pricings.Convert); err != nil {
				return liabilityPartition{}, err
			}
		case verdict.IsPriced():
			partition.priced, err = accrueLiability(
				partition.priced,
				supply,
				pricings.Convert,
			)
			if err != nil {
				return liabilityPartition{}, err
			}
			if err := k.accrueSelfHeld(ctx, &partition, denom, pricings.Convert); err != nil {
				return liabilityPartition{}, err
			}
		case verdict.Reason == assettypes.UnpricedReason_UNPRICED_REASON_UNTRUSTED:
			partition.untrustedSupply = append(partition.untrustedSupply, supply)
			partition.complete = false
		case verdict.Reason == assettypes.UnpricedReason_UNPRICED_REASON_WRITTEN_OFF:
			partition.writtenOff = append(partition.writtenOff, types.WrittenOffExposure{
				OutstandingSupply: supply,
				WriteOffVersion:   verdict.Asset.Version,
			})
		case verdict.Reason == assettypes.UnpricedReason_UNPRICED_REASON_FEED_UNAVAILABLE:
			// Incomplete whether or not the last known rate keeps counting this
			// supply: no fresh rate stood behind part of the total, which fund
			// targets must not be sized on.
			partition.staleSupply = append(partition.staleSupply, supply)
			partition.complete = false
			if verdict.LastRate == nil {
				// Never priced, so there is no evidence to count it by.
				continue
			}
			partition.stale, err = accrueLiability(
				partition.stale,
				supply,
				pricings.ConvertLastKnown,
			)
			if err != nil {
				return liabilityPartition{}, err
			}
			if err := k.accrueSelfHeld(ctx, &partition, denom, pricings.ConvertLastKnown); err != nil {
				return liabilityPartition{}, err
			}
		default:
			// Retired and unrecognised supply is invisible to the liability
			// report.
		}
	}

	return partition, nil
}

// discloseIncompleteValuation records excluded supply for settlement or a committee action using
// degraded bounds. Queries report the partition without emitting events; failed transactions
// discard their disclosure.
func (k Keeper) discloseIncompleteValuation(ctx context.Context, partition liabilityPartition) error {
	claimable, err := partition.recognised()
	if err != nil {
		return err
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventLiabilityIncomplete{
		ClaimableLiability:       chain.NoahDecCoin(claimable),
		StaleMemberSupply:        partition.staleSupply,
		UntrustedSuspendedSupply: partition.untrustedSupply,
	}); err != nil {
		return fmt.Errorf("emitting Treasury incomplete liability event: %w", err)
	}

	return nil
}

// accrueSelfHeld values Reserve custody using the same branch, membership, and rate as counted
// liability. Zero holdings create no disclosure row.
func (k Keeper) accrueSelfHeld(
	ctx context.Context,
	partition *liabilityPartition,
	denom string,
	convert func(sdk.DecCoin, string) (sdk.DecCoin, error),
) error {
	held := k.bankKeeper.GetBalance(
		ctx,
		k.accountKeeper.GetModuleAddress(reservetypes.StrategicReserveName),
		denom,
	)
	if !held.Amount.IsPositive() {
		return nil
	}

	accrued, err := accrueLiability(partition.selfHeld, held, convert)
	if err != nil {
		return fmt.Errorf("valuing self-held %s: %w", denom, err)
	}
	partition.selfHeld = accrued
	partition.selfHeldSupply = append(partition.selfHeldSupply, held)

	return nil
}

// accrueLiability adds the NOAH value of supply to a liability bucket. The
// conversion is a parameter so each call site names its rate; ConvertLastKnown
// is legitimate in exactly one branch.
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
