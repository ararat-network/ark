package types

import (
	"errors"

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

	return nil
}
