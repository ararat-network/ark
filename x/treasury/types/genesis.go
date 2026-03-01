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
	taxRate,
	rewardWeight math.LegacyDec,
	taxCaps []TaxCap,
	taxProceeds,
	epochInitialIssuance sdk.Coins,
	epochStates []EpochState,
) *GenesisState {
	return &GenesisState{
		Params:               params,
		TaxRate:              taxRate,
		RewardWeight:         rewardWeight,
		TaxCaps:              taxCaps,
		EpochTaxProceeds:     taxProceeds,
		EpochInitialIssuance: epochInitialIssuance,
		EpochStates:          epochStates,
	}
}

// DefaultGenesisState gets raw genesis raw message for testing
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		Params:               DefaultParams(),
		TaxRate:              DefaultTaxRate,
		RewardWeight:         DefaultRewardWeight,
		TaxCaps:              []TaxCap{},
		EpochTaxProceeds:     sdk.Coins{},
		EpochInitialIssuance: sdk.Coins{},
		EpochStates:          []EpochState{},
	}
}

// Validate validates the provided oracle genesis state to ensure the
// expected invariants holds. (i.e. params in correct bounds, no duplicate validators)
func (gs GenesisState) Validate() error {
	if gs.TaxRate.LT(gs.Params.TaxPolicy.RateMin) || gs.TaxRate.GT(gs.Params.TaxPolicy.RateMax) {
		return fmt.Errorf("tax_rate must less than RateMax(%s) and bigger than RateMin(%s)", gs.Params.TaxPolicy.RateMax, gs.Params.TaxPolicy.RateMin)
	}

	if gs.RewardWeight.LT(gs.Params.RewardPolicy.RateMin) || gs.RewardWeight.GT(gs.Params.RewardPolicy.RateMax) {
		return fmt.Errorf("reward_weight must less than WeightMax(%s) and bigger than RateMin(%s)", gs.Params.RewardPolicy.RateMax, gs.Params.RewardPolicy.RateMin)
	}

	return gs.Params.Validate()
}

// GetGenesisStateFromAppState returns x/treasury GenesisState given raw application
// genesis state.
func GetGenesisStateFromAppState(cdc codec.JSONCodec, appState map[string]json.RawMessage) *GenesisState {
	var genesisState GenesisState

	if appState[ModuleName] != nil {
		cdc.MustUnmarshalJSON(appState[ModuleName], &genesisState)
	}

	return &genesisState
}
