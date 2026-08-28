package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "github.com/ararat-network/ark/pkg/chain"
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

// recognised is the claimable aggregate — every liability the block could put
// a number against — and the denominator redemption coverage divides by. A
// member with an unavailable feed counts at its last known rate, because
// dropping it would pay its holders' coverage share away during the outage.
// Write-offs and untrusted suspensions are genuinely outside: the obligation
// or the protocol's rate is withdrawn, not merely unevidenced.
func (p liabilityPartition) recognised() (math.LegacyDec, error) {
	valued, err := decimal.Add(p.priced, p.settlement)
	if err != nil {
		return math.LegacyDec{}, err
	}
	return decimal.Add(valued, p.stale)
}

// net is recognised less what the strategic Reserve holds of it: the claims
// that can actually arrive, since Reserve-held paper has no holder and only a
// committee act returns it to circulation. Committee bounds keep recognised —
// the committee they constrain can re-issue that paper by deploying it, so a
// bound on net would move on state its own subject controls. The subtraction
// cannot go negative — selfHeld accrues only in branches that counted the same
// supply at the same rate, and a balance never exceeds its supply — so the
// check is a backstop.
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

// discloseIncompleteValuation announces a valuation that could not cover every
// recognised liability, naming the supply behind the qualification.
//
// Its consumers call it rather than the fold, because the fold cannot know
// whether a caller will spend against the figure or merely look at it.
// Settlement announces the block's own valuation; a committee bound announces
// the one its ceiling was sized on, inside the transaction that acted on it and
// discarded with it if the act then fails. The event therefore means "capital
// moved against a qualified figure", never "someone asked".
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

// accrueSelfHeld records what the strategic Reserve holds of a member the
// caller's branch just counted, valued through that branch's own conversion —
// called per counted branch so the netting can never disagree with the
// counting in membership or rate. A zero balance records nothing, so the list
// names only what the Reserve holds.
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
