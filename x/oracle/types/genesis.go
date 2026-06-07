package types

import (
	"encoding/json"
	"fmt"

	"github.com/cosmos/cosmos-sdk/codec"
)

// NewGenesisState creates a new GenesisState object
func NewGenesisState(
	params Params,
	exchangeRates []ExchangeRate,
	scoreWeights []ScoreWeight,
	missCounts []MissCount,
) *GenesisState {
	return &GenesisState{
		Params:        params,
		ExchangeRates: exchangeRates,
		ScoreWeights:  scoreWeights,
		MissCounts:    missCounts,
	}
}

// DefaultGenesisState - default GenesisState
func DefaultGenesisState() *GenesisState {
	return NewGenesisState(DefaultParams(),
		[]ExchangeRate{},
		[]ScoreWeight{},
		[]MissCount{},
	)
}

// Validate validates the oracle genesis state
func (gs GenesisState) Validate() error {
	// ExchangeRates: no duplicates, non-empty denom, positive rate
	seenDenoms := make(map[string]bool)
	for _, er := range gs.ExchangeRates {
		if len(er.Denom) == 0 {
			return fmt.Errorf("exchange rate denom must not be empty")
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
		if len(mc.ValidatorAddress) == 0 {
			return fmt.Errorf("score weight validator address must not be empty")
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
			return fmt.Errorf("miss count validator address must not be empty")
		}
		if seenValidators[mc.ValidatorAddress] {
			return fmt.Errorf("duplicate miss count for validator %s", mc.ValidatorAddress)
		}
		seenValidators[mc.ValidatorAddress] = true
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
