package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"

	"ark/pkg/chain"
)

// NewGenesisState creates a Treasury genesis state.
func NewGenesisState(
	params Params,
	taxCaps []TaxCap,
	rewardFunding RewardFundingState,
	monetaryMandate MonetaryMandate,
	monetaryPolicy MonetaryPolicy,
	taxCapRefreshPending bool,
) *GenesisState {
	return &GenesisState{
		Params:               params,
		TaxCaps:              append([]TaxCap(nil), taxCaps...),
		RewardFunding:        rewardFunding,
		MonetaryMandate:      monetaryMandate,
		MonetaryPolicy:       monetaryPolicy,
		TaxCapRefreshPending: taxCapRefreshPending,
	}
}

// DefaultGenesisState returns the safe, unconfigured Treasury genesis state.
func DefaultGenesisState() *GenesisState {
	return NewGenesisState(
		DefaultParams(),
		[]TaxCap{},
		DefaultRewardFundingState(),
		DefaultMonetaryMandate(),
		DefaultMonetaryPolicy(),
		false,
	)
}

// DefaultRewardFundingState returns canonical empty reward-funding accounting.
// The keeper resets to it whenever a window settles, so the zero state has to
// mean the same thing at genesis and mid-chain.
func DefaultRewardFundingState() RewardFundingState {
	return RewardFundingState{
		ValidatorTarget:   math.ZeroInt(),
		OracleTarget:      math.ZeroInt(),
		ValidatorFeeValue: math.ZeroInt(),
	}
}

// Validate checks the context-free Treasury genesis invariants. Fund balances
// and Oracle-dependent tax-cap coverage are validated by the keeper.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	if err := gs.MonetaryPolicy.Validate(); err != nil {
		return err
	}

	for i, taxCap := range gs.TaxCaps {
		if err := chain.ValidatePricedDenom(taxCap.Denom); err != nil {
			return fmt.Errorf("tax cap denom %q is invalid: %w", taxCap.Denom, err)
		}
		if taxCap.TaxCap.IsNil() {
			return fmt.Errorf("tax cap for %s must be set", taxCap.Denom)
		}
		if taxCap.TaxCap.IsNegative() {
			return fmt.Errorf("tax cap for %s must be zero or positive", taxCap.Denom)
		}
		// A cap is deliberately not compared against the reference tax cap.
		// Only caps derived under the current reference agree with it: caps
		// kept after a denomination leaves the oracle-priced set are anchored to
		// whatever the reference was when they were last derived, so a
		// later policy move — including one to or from the zero uncapped
		// sentinel — leaves them disagreeing by design.
		if i > 0 && taxCap.Denom <= gs.TaxCaps[i-1].Denom {
			return fmt.Errorf("genesis tax caps must be sorted by unique denom")
		}
	}

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
			!gs.RewardFunding.ValidatorFeeValue.IsZero()) {
		return errors.New("empty reward funding window must use the default state")
	}
	// An imported window arrives mid-accrual rather than being reached one
	// block at a time, so the ceiling the accrual gets from MaxBlockRewardTarget
	// has to be imposed here directly. The bound is a whole window's worth of
	// either target, which is the most the running state could legitimately
	// hold.
	maxAccrued := MaxBlockRewardTarget.Mul(math.NewIntFromUint64(MaxRewardFundingWindow))
	for _, accrued := range []struct {
		name  string
		value math.Int
	}{
		{"validator target", gs.RewardFunding.ValidatorTarget},
		{"oracle target", gs.RewardFunding.OracleTarget},
		{"validator fee value", gs.RewardFunding.ValidatorFeeValue},
	} {
		if accrued.value.GT(maxAccrued) {
			return fmt.Errorf("%s must not exceed %s: %s", accrued.name, maxAccrued, accrued.value)
		}
	}
	if err := gs.MonetaryMandate.Validate(); err != nil {
		return err
	}

	return nil
}
