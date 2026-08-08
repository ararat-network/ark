package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/math"

	"ark/x/treasury/types"
)

// RequiredReserveCapital reports the capital the strategic Reserve is owed
// against current exposure, satisfying x/reserve's TreasuryCapitalReader. It is
// the requirement side of §7.2: a number, never a decision, and only the fund
// acts on the difference.
//
// It requires a complete valuation, because every incompleteness that still
// yields a number can understate the aggregate — which understates the
// requirement and overstates the surplus a committee may burn.
func (k Keeper) RequiredReserveCapital(ctx context.Context) (math.Int, error) {
	targets, err := k.grossFundTargets(ctx, true)
	if err != nil {
		return math.Int{}, fmt.Errorf("sizing the Reserve capital requirement: %w", err)
	}
	return targets.Reserve, nil
}

// RedemptionBufferShortfall reports how far the Redemption Buffer falls below
// its target, bounding a committee transfer into it. The Buffer has no operator
// to ask, so Treasury reads its balance directly.
//
// It sizes on whatever the block could value, because the coverage draw spends
// the Buffer against this same aggregate however incomplete it is.
func (k Keeper) RedemptionBufferShortfall(ctx context.Context) (math.Int, error) {
	targets, err := k.grossFundTargets(ctx, false)
	if err != nil {
		return math.Int{}, fmt.Errorf("sizing the Redemption Buffer target: %w", err)
	}
	return shortfall(targets.Buffer, k.balance(ctx, types.RedemptionBufferName)), nil
}

// InsuranceShortfall reports how far Insurance falls below its target,
// bounding a committee transfer into it. Insurance reports its own recognised
// capital (§7.2), which an approved pending claim already encumbers, so sizing
// the gap against the raw balance would understate it by the claims Insurance
// has promised to pay.
func (k Keeper) InsuranceShortfall(ctx context.Context) (math.Int, error) {
	targets, err := k.grossFundTargets(ctx, false)
	if err != nil {
		return math.Int{}, fmt.Errorf("sizing the Insurance target: %w", err)
	}
	balance, err := k.claimsKeeper.RecognisedCapital(ctx)
	if err != nil {
		return math.Int{}, fmt.Errorf("getting Insurance recognised capital: %w", err)
	}
	return shortfall(targets.Insurance, balance), nil
}

// grossFundTargets sizes all three fund targets against the recognised basis a
// committee act is bounded by, folding the registry live and reading no fund
// balance. Every caller is a committee message, so the fold is metered against
// the gas that message pays.
//
// Whether an incomplete valuation is fatal belongs to the caller, because the
// two kinds are exposed in opposite directions: an incomplete aggregate can
// land either side of the truth, which a bound on destroying capital cannot
// survive in the low direction, while a bound on moving capital between
// protocol funds is exposed only in the high one, where the money stays
// protocol capital and the mandate floor still binds.
func (k Keeper) grossFundTargets(ctx context.Context, requireComplete bool) (types.FundTargetSet, error) {
	partition, err := k.liabilityPartitionValue(ctx, nil)
	if err != nil {
		return types.FundTargetSet{}, err
	}
	if !partition.complete {
		if requireComplete {
			return types.FundTargetSet{}, errors.New("aggregate liability valuation is incomplete")
		}
		if err := k.discloseIncompleteValuation(ctx, partition); err != nil {
			return types.FundTargetSet{}, err
		}
	}
	gross, err := partition.recognised()
	if err != nil {
		return types.FundTargetSet{}, err
	}
	policy, err := k.MonetaryPolicy.Get(ctx)
	if err != nil {
		return types.FundTargetSet{}, fmt.Errorf("getting monetary policy: %w", err)
	}
	return policy.FundTargets(gross), nil
}
