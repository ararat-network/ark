package types

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/pkg/chain"
)

// NewGenesisState creates a new GenesisState object
func NewGenesisState(
	params Params,
	accounting Accounting,
	exchangeRates []ExchangeRate,
	rewardWeights []RewardWeight,
	missCounts []MissCount,
	voteTargets VoteTargets,
) *GenesisState {
	return &GenesisState{
		Params:        params,
		Accounting:    accounting,
		ExchangeRates: exchangeRates,
		RewardWeights: rewardWeights,
		MissCounts:    missCounts,
		VoteTargets:   voteTargets,
	}
}

// NewAccounting starts reward and slash accounting from genesis using
// the supplied active parameter windows.
func NewAccounting(params Params) Accounting {
	return Accounting{
		RewardWindow:             params.RewardWindow,
		RewardDistributionWindow: params.RewardDistributionWindow,
		SlashWindow:              params.SlashWindow,
	}
}

// DefaultGenesisState - default GenesisState
func DefaultGenesisState() *GenesisState {
	params := DefaultParams()
	return NewGenesisState(
		params,
		NewAccounting(params),
		[]ExchangeRate{},
		[]RewardWeight{},
		[]MissCount{},
		NewVoteTargets(params),
	)
}

// Validate validates the oracle genesis state
func (gs GenesisState) Validate() error {
	if gs.Accounting.RewardWindow == 0 {
		return errors.New("accounting reward window must be greater than zero")
	}
	if gs.Accounting.RewardDistributionWindow < gs.Accounting.RewardWindow {
		return errors.New("accounting reward distribution window must be greater than or equal to reward window")
	}
	if gs.Accounting.SlashWindow == 0 {
		return errors.New("accounting slash window must be greater than zero")
	}

	// ExchangeRates: ordered unique Ark-native base denoms and positive rates
	for i, er := range gs.ExchangeRates {
		if err := chain.ValidateNativeBaseDenom(er.Denom); err != nil {
			return fmt.Errorf("exchange rate %w", err)
		}
		if er.Rate.IsNil() {
			return fmt.Errorf("exchange rate for %s must be set", er.Denom)
		}
		if !er.Rate.IsInValidRange() {
			return fmt.Errorf("exchange rate for %s must be representable", er.Denom)
		}
		if !er.Rate.IsPositive() {
			return fmt.Errorf("exchange rate for %s must be positive: %s", er.Denom, er.Rate)
		}
		if i > 0 && er.Denom <= gs.ExchangeRates[i-1].Denom {
			return errors.New("genesis exchange rates must be sorted by unique denom")
		}
	}

	// RewardWeights: ordered unique validator-address storage keys
	var previousValidatorAddress sdk.ValAddress
	for i, rewardWeight := range gs.RewardWeights {
		if rewardWeight.RewardWeight.IsNil() {
			return errors.New("reward weight must be set")
		}
		if rewardWeight.RewardWeight.IsNegative() {
			return fmt.Errorf("reward weight must not be negative for validator %s", rewardWeight.ValidatorAddress)
		}
		if len(rewardWeight.ValidatorAddress) == 0 {
			return errors.New("reward weight validator address must not be empty")
		}
		validatorAddress, err := sdk.ValAddressFromBech32(rewardWeight.ValidatorAddress)
		if err != nil {
			return fmt.Errorf("reward weight validator address is invalid: %s", rewardWeight.ValidatorAddress)
		}
		if i > 0 && bytes.Compare(validatorAddress, previousValidatorAddress) <= 0 {
			return errors.New("genesis reward weights must be sorted by unique validator address")
		}
		previousValidatorAddress = validatorAddress
	}

	// MissCounts: ordered unique validator-address storage keys
	previousValidatorAddress = nil
	for i, mc := range gs.MissCounts {
		if len(mc.ValidatorAddress) == 0 {
			return errors.New("miss count validator address must not be empty")
		}
		validatorAddress, err := sdk.ValAddressFromBech32(mc.ValidatorAddress)
		if err != nil {
			return fmt.Errorf("miss count validator address is invalid: %s", mc.ValidatorAddress)
		}
		if i > 0 && bytes.Compare(validatorAddress, previousValidatorAddress) <= 0 {
			return errors.New("genesis miss counts must be sorted by unique validator address")
		}
		previousValidatorAddress = validatorAddress
	}

	if err := gs.VoteTargets.Validate(); err != nil {
		return err
	}
	voteTargets := gs.VoteTargets
	for _, er := range gs.ExchangeRates {
		if _, found := slices.BinarySearch(voteTargets.Denoms, er.Denom); !found {
			return fmt.Errorf("exchange rate denom %s is not a vote target", er.Denom)
		}
	}

	if err := gs.Params.Validate(); err != nil {
		return err
	}
	desiredDenoms := VoteTargetDenoms(gs.Params)
	stagedDenoms := voteTargets.Denoms
	if voteTargets.Pending != nil {
		stagedDenoms = voteTargets.Pending.Denoms
	}
	if !slices.Equal(desiredDenoms, stagedDenoms) {
		return errors.New("oracle params denoms must match the active or pending vote targets")
	}

	return nil
}

// GetGenesisStateFromAppState returns x/oracle GenesisState given raw application
// genesis state.
func GetGenesisStateFromAppState(cdc codec.JSONCodec, appState map[string]json.RawMessage) *GenesisState {
	var genesisState GenesisState

	if appState[ModuleName] != nil {
		cdc.MustUnmarshalJSON(appState[ModuleName], &genesisState)
	}

	return &genesisState
}
