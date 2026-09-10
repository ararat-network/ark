package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/x/treasury/types"
)

// RequiredReserveCapital sizes the nominal exposure requirement for Reserve burns. Incomplete
// valuation fails because an understated requirement would overstate disposable surplus.
func (k Keeper) RequiredReserveCapital(ctx context.Context) (math.Int, error) {
	targets, err := k.grossFundTargets(ctx, true)
	if err != nil {
		return math.Int{}, fmt.Errorf("sizing the Reserve capital requirement: %w", err)
	}
	return targets.Reserve, nil
}

// RedemptionBufferShortfall bounds committee refill using available nominal liability and the
// Buffer's direct custody balance. Incomplete valuation does not disable refill.
func (k Keeper) RedemptionBufferShortfall(ctx context.Context) (math.Int, error) {
	targets, err := k.grossFundTargets(ctx, false)
	if err != nil {
		return math.Int{}, fmt.Errorf("sizing the Redemption Buffer target: %w", err)
	}
	return shortfall(targets.Buffer, k.getBalance(ctx, types.RedemptionBufferName)), nil
}

// InsuranceShortfall bounds committee refill against recognised Insurance capital, which excludes
// pending claims, rather than raw custody.
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

// grossFundTargets folds live nominal liability without reading fund balances. Callers choose
// whether incompleteness is fatal: disposal requires completeness, while bounded transfers between
// protocol funds may proceed with disclosure.
func (k Keeper) grossFundTargets(ctx context.Context, requireComplete bool) (types.FundTargetSet, error) {
	partition, err := k.liabilityPartitionValue(ctx)
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
	policy, err := k.EconomicPolicy.Get(ctx)
	if err != nil {
		return types.FundTargetSet{}, fmt.Errorf("getting economic policy: %w", err)
	}
	// Apply the same exposure multiplier as expansion routing. Higher requirements reduce burnable
	// surplus and widen fund-refill gaps; nominal liability prevents self-held paper from loosening
	// committee bounds.
	basis, err := k.exposureAdjusted(ctx, gross)
	if err != nil {
		return types.FundTargetSet{}, err
	}
	return policy.FundTargets(basis), nil
}
