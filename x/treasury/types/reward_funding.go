package types

import (
	"fmt"

	"cosmossdk.io/math"
)

// DefaultRewardFundingState returns canonical empty reward-funding accounting.
func DefaultRewardFundingState() RewardFundingState {
	return RewardFundingState{
		ValidatorTarget:   math.ZeroInt(),
		OracleTarget:      math.ZeroInt(),
		ValidatorFeeValue: math.ZeroInt(),
	}
}

// ValidateRewardTargetCapacity verifies that both the next complete funding
// window and the active partial window fit in Treasury's integer state.
func ValidateRewardTargetCapacity(params Params, funding RewardFundingState, policy MonetaryPolicy) error {
	blockTarget, err := policy.ValidatorBlockRewardTarget.SafeAdd(policy.OracleBlockRewardTarget)
	if err != nil {
		return fmt.Errorf("reward target capacity exceeded by per-block targets: %w", err)
	}
	if _, err := blockTarget.SafeMul(math.NewIntFromUint64(params.RewardFundingWindow)); err != nil {
		return fmt.Errorf(
			"reward target capacity exceeded over complete %d-block funding window: %w",
			params.RewardFundingWindow,
			err,
		)
	}

	accumulatedTarget, err := funding.ValidatorTarget.SafeAdd(funding.OracleTarget)
	if err != nil {
		return fmt.Errorf("reward target capacity exceeded by accumulated targets: %w", err)
	}
	remainingTarget, err := blockTarget.SafeMul(math.NewIntFromUint64(funding.BlocksRemaining))
	if err != nil {
		return fmt.Errorf(
			"reward target capacity exceeded over %d remaining blocks: %w",
			funding.BlocksRemaining,
			err,
		)
	}
	if _, err := accumulatedTarget.SafeAdd(remainingTarget); err != nil {
		return fmt.Errorf("reward target capacity exceeded by projected window total: %w", err)
	}
	return nil
}
