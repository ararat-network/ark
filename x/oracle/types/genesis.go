package types

import (
	"encoding/json"
	"fmt"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// NewGenesisState creates a new GenesisState object
func NewGenesisState(
	params Params,
	exchangeRates []ExchangeRate,
	scoreWeights []ScoreWeight,
	missCounts []MissCount,
	tobinTaxes []TobinTax,
) *GenesisState {
	return &GenesisState{
		Params:        params,
		ExchangeRates: exchangeRates,
		ScoreWeights:  scoreWeights,
		MissCounts:    missCounts,
		TobinTaxes:    tobinTaxes,
	}
}

// DefaultGenesisState - default GenesisState
func DefaultGenesisState() *GenesisState {
	return NewGenesisState(DefaultParams(),
		[]ExchangeRate{},
		[]ScoreWeight{},
		[]MissCount{},
		[]TobinTax{},
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
		if len(mc.ValidatorAddress) == 0 {
			return fmt.Errorf("score weight validator address must not be empty")
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
			return fmt.Errorf("miss count validator address must not be empty")
		}
		if _, err := sdk.ValAddressFromBech32(mc.ValidatorAddress); err != nil {
			return fmt.Errorf("miss count validator address is invalid: %s", mc.ValidatorAddress)
		}
		if seenValidators[mc.ValidatorAddress] {
			return fmt.Errorf("duplicate miss count for validator %s", mc.ValidatorAddress)
		}
		seenValidators[mc.ValidatorAddress] = true
	}

	// TobinTaxes: no duplicates, micro denoms, tax in [0, 1]
	seenDenoms = make(map[string]bool)
	for _, tt := range gs.TobinTaxes {
		if len(tt.Denom) < 3 || tt.Denom[0] != 'u' {
			return fmt.Errorf("tobin tax denom must be a micro denom beginning with u: %s", tt.Denom)
		}
		if tt.TobinTax.IsNil() {
			return fmt.Errorf("tobin tax for %s must be set", tt.Denom)
		}
		if tt.TobinTax.IsNegative() || tt.TobinTax.GT(math.LegacyOneDec()) {
			return fmt.Errorf("tobin tax for %s must be between [0, 1]: %s", tt.Denom, tt.TobinTax)
		}
		if seenDenoms[tt.Denom] {
			return fmt.Errorf("duplicate tobin tax for denom %s", tt.Denom)
		}
		seenDenoms[tt.Denom] = true
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
