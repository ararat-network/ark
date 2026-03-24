package types

import (
	"encoding/json"
	"fmt"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
)

// NewGenesisState creates a new GenesisState object
func NewGenesisState(
	params Params,
	rates []ExchangeRateTuple,
	feederDelegations []FeederDelegation,
	missCounters []MissCounter,
	aggregateExchangeRatePrevotes []AggregateExchangeRatePrevote,
	aggregateExchangeRateVotes []AggregateExchangeRateVote,
	tobinTaxes []TobinTax,
) *GenesisState {
	return &GenesisState{
		Params:                        params,
		ExchangeRates:                 rates,
		FeederDelegations:             feederDelegations,
		MissCounters:                  missCounters,
		AggregateExchangeRatePrevotes: aggregateExchangeRatePrevotes,
		AggregateExchangeRateVotes:    aggregateExchangeRateVotes,
		TobinTaxes:                    tobinTaxes,
	}
}

// DefaultGenesisState - default GenesisState used by columbus-2
func DefaultGenesisState() *GenesisState {
	return NewGenesisState(DefaultParams(),
		[]ExchangeRateTuple{},
		[]FeederDelegation{},
		[]MissCounter{},
		[]AggregateExchangeRatePrevote{},
		[]AggregateExchangeRateVote{},
		[]TobinTax{})
}

// Validate validates the oracle genesis state
func (gs GenesisState) Validate() error {
	// ExchangeRates: no duplicates, non-empty denom, positive rate
	seenDenoms := make(map[string]bool)
	for _, er := range gs.ExchangeRates {
		if len(er.Denom) == 0 {
			return fmt.Errorf("exchange rate denom must not be empty")
		}
		if !er.ExchangeRate.IsPositive() {
			return fmt.Errorf("exchange rate for %s must be positive: %s", er.Denom, er.ExchangeRate)
		}
		if seenDenoms[er.Denom] {
			return fmt.Errorf("duplicate exchange rate for denom %s", er.Denom)
		}
		seenDenoms[er.Denom] = true
	}

	// FeederDelegations: no duplicate validators, non-empty addresses
	seenValidators := make(map[string]bool)
	for _, fd := range gs.FeederDelegations {
		if len(fd.ValidatorAddress) == 0 {
			return fmt.Errorf("feeder delegation validator address must not be empty")
		}
		if len(fd.FeederAddress) == 0 {
			return fmt.Errorf("feeder delegation feeder address must not be empty")
		}
		if seenValidators[fd.ValidatorAddress] {
			return fmt.Errorf("duplicate feeder delegation for validator %s", fd.ValidatorAddress)
		}
		seenValidators[fd.ValidatorAddress] = true
	}

	// MissCounters: no duplicate validators
	seenValidators = make(map[string]bool)
	for _, mc := range gs.MissCounters {
		if len(mc.ValidatorAddress) == 0 {
			return fmt.Errorf("miss counter validator address must not be empty")
		}
		if seenValidators[mc.ValidatorAddress] {
			return fmt.Errorf("duplicate miss counter for validator %s", mc.ValidatorAddress)
		}
		seenValidators[mc.ValidatorAddress] = true
	}

	// AggregateExchangeRatePrevotes: no duplicate voters
	seenVoters := make(map[string]bool)
	for _, ap := range gs.AggregateExchangeRatePrevotes {
		if len(ap.Voter) == 0 {
			return fmt.Errorf("aggregate prevote voter must not be empty")
		}
		if seenVoters[ap.Voter] {
			return fmt.Errorf("duplicate aggregate prevote for voter %s", ap.Voter)
		}
		seenVoters[ap.Voter] = true
	}

	// AggregateExchangeRateVotes: no duplicate voters
	seenVoters = make(map[string]bool)
	for _, av := range gs.AggregateExchangeRateVotes {
		if len(av.Voter) == 0 {
			return fmt.Errorf("aggregate vote voter must not be empty")
		}
		if seenVoters[av.Voter] {
			return fmt.Errorf("duplicate aggregate vote for voter %s", av.Voter)
		}
		seenVoters[av.Voter] = true
	}

	// TobinTaxes: no duplicates, non-empty denom, tax in [0, 1]
	seenDenoms = make(map[string]bool)
	for _, tt := range gs.TobinTaxes {
		if len(tt.Denom) == 0 {
			return fmt.Errorf("tobin tax denom must not be empty")
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
