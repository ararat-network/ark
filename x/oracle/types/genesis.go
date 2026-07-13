package types

import (
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
	accounting AccountingState,
	exchangeRates []ExchangeRate,
	scoreWeights []ScoreWeight,
	missCounts []MissCount,
	voteTargets VoteTargetState,
) *GenesisState {
	voteTargets.Denoms = slices.Clone(voteTargets.Denoms)
	slices.Sort(voteTargets.Denoms)

	return &GenesisState{
		Params:        params,
		Accounting:    accounting,
		ExchangeRates: exchangeRates,
		ScoreWeights:  scoreWeights,
		MissCounts:    missCounts,
		VoteTargets:   voteTargets,
	}
}

// NewVoteTargetState returns a canonical vote-target snapshot derived from params.
func NewVoteTargetState(params Params) VoteTargetState {
	denoms := make([]string, len(params.TobinTaxes))
	for i, tobinTax := range params.TobinTaxes {
		denoms[i] = tobinTax.Denom
	}
	slices.Sort(denoms)

	return VoteTargetState{Denoms: denoms}
}

// NewAccountingState starts reward and slash accounting from genesis using
// the supplied active parameter windows.
func NewAccountingState(params Params) AccountingState {
	return AccountingState{
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
		NewAccountingState(params),
		[]ExchangeRate{},
		[]ScoreWeight{},
		[]MissCount{},
		NewVoteTargetState(params),
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

	// ExchangeRates: no duplicates, micro denoms, positive rate
	seenDenoms := make(map[string]bool)
	for _, er := range gs.ExchangeRates {
		if err := chain.ValidateMicroDenom(er.Denom); err != nil {
			return fmt.Errorf("exchange rate %w", err)
		}
		if er.Rate.IsNil() {
			return fmt.Errorf("exchange rate for %s must be set", er.Denom)
		}
		if !er.Rate.IsPositive() {
			return fmt.Errorf("exchange rate for %s must be positive: %s", er.Denom, er.Rate)
		}
		if seenDenoms[er.Denom] {
			return fmt.Errorf("duplicate exchange rate for denom %s", er.Denom)
		}
		seenDenoms[er.Denom] = true
	}

	// ScoreWeights: no duplicate validators
	seenValidators := make(map[string]bool)
	for _, mc := range gs.ScoreWeights {
		if mc.ScoreWeight.IsNil() {
			return errors.New("score weight must be set")
		}
		if mc.ScoreWeight.IsNegative() {
			return fmt.Errorf("score weight must not be negative for validator %s", mc.ValidatorAddress)
		}
		if len(mc.ValidatorAddress) == 0 {
			return errors.New("score weight validator address must not be empty")
		}
		if _, err := sdk.ValAddressFromBech32(mc.ValidatorAddress); err != nil {
			return fmt.Errorf("score weight validator address is invalid: %s", mc.ValidatorAddress)
		}
		if seenValidators[mc.ValidatorAddress] {
			return fmt.Errorf("duplicate score weight for validator %s", mc.ValidatorAddress)
		}
		seenValidators[mc.ValidatorAddress] = true
	}

	// MissCounts: no duplicate validators
	seenValidators = make(map[string]bool)
	for _, mc := range gs.MissCounts {
		if len(mc.ValidatorAddress) == 0 {
			return errors.New("miss count validator address must not be empty")
		}
		if _, err := sdk.ValAddressFromBech32(mc.ValidatorAddress); err != nil {
			return fmt.Errorf("miss count validator address is invalid: %s", mc.ValidatorAddress)
		}
		if seenValidators[mc.ValidatorAddress] {
			return fmt.Errorf("duplicate miss count for validator %s", mc.ValidatorAddress)
		}
		seenValidators[mc.ValidatorAddress] = true
	}

	// VoteTargets: no duplicates and canonical micro denoms.
	if len(gs.VoteTargets.Denoms) > MaxVoteTargets {
		return fmt.Errorf(
			"vote targets count %d exceeds maximum vote targets %d",
			len(gs.VoteTargets.Denoms),
			MaxVoteTargets,
		)
	}
	seenDenoms = make(map[string]bool)
	for _, denom := range gs.VoteTargets.Denoms {
		if err := chain.ValidateMicroDenom(denom); err != nil {
			return fmt.Errorf("vote target %w", err)
		}
		if seenDenoms[denom] {
			return fmt.Errorf("duplicate vote target denom %s", denom)
		}
		seenDenoms[denom] = true
	}

	return gs.Params.Validate()
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
