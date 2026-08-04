package types

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cosmos/cosmos-sdk/codec"

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
	if err := ValidateRewardTargetCapacity(gs.Params, gs.RewardFunding, gs.MonetaryPolicy); err != nil {
		return err
	}
	if err := gs.MonetaryMandate.Validate(); err != nil {
		return err
	}

	return nil
}

// GetGenesisStateFromAppState returns the Treasury genesis state from raw app
// genesis state.
func GetGenesisStateFromAppState(cdc codec.JSONCodec, appState map[string]json.RawMessage) *GenesisState {
	var genesisState GenesisState
	if appState[ModuleName] != nil {
		cdc.MustUnmarshalJSON(appState[ModuleName], &genesisState)
	}
	return &genesisState
}
