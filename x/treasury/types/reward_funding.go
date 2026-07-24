package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"
)

// DefaultRewardFundingState returns canonical empty reward-funding accounting.
func DefaultRewardFundingState() RewardFundingState {
	return RewardFundingState{
		ValidatorTarget:   math.ZeroInt(),
		OracleTarget:      math.ZeroInt(),
		ValidatorFeeValue: math.ZeroInt(),
		ValuationComplete: true,
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

func (gs GenesisState) validateRewardFunding() error {
	if gs.RewardFunding.ValidatorTarget.IsNil() {
		return errors.New("validator target must be set")
	}
	if gs.RewardFunding.ValidatorTarget.IsNegative() {
		return errors.New("validator target must be zero or positive")
	}
	if gs.RewardFunding.OracleTarget.IsNil() {
		return errors.New("oracle target must be set")
	}
	if gs.RewardFunding.OracleTarget.IsNegative() {
		return errors.New("oracle target must be zero or positive")
	}
	if gs.RewardFunding.ValidatorFeeValue.IsNil() {
		return errors.New("validator fee value must be set")
	}
	if gs.RewardFunding.ValidatorFeeValue.IsNegative() {
		return errors.New("validator fee value must be zero or positive")
	}
	if gs.RewardFunding.BlocksRemaining == 0 &&
		(!gs.RewardFunding.ValidatorTarget.IsZero() ||
			!gs.RewardFunding.OracleTarget.IsZero() ||
			!gs.RewardFunding.ValidatorFeeValue.IsZero() ||
			!gs.RewardFunding.ValuationComplete) {
		return errors.New("empty reward funding window must use the default state")
	}
	if err := ValidateRewardTargetCapacity(gs.Params, gs.RewardFunding, gs.MonetaryPolicy); err != nil {
		return err
	}

	return nil
}
