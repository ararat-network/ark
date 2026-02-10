package types

import (
	"encoding/json"

	marketv1 "noah/api/noah/market/v1"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
)

// NewGenesisState creates a new GenesisState object
func NewGenesisState(noahPoolDelta math.LegacyDec, params *marketv1.Params) *marketv1.GenesisState {
	return &marketv1.GenesisState{
		NoahPoolDelta: noahPoolDelta.String(),
		Params:        params,
	}
}

// DefaultGenesisState returns raw genesis raw message for testing
func DefaultGenesisState() *marketv1.GenesisState {
	return &marketv1.GenesisState{
		NoahPoolDelta: math.LegacyZeroDec().String(),
		Params:        DefaultParams(),
	}
}

// ValidateGenesis validates the provided market genesis state
func ValidateGenesis(data *marketv1.GenesisState) error {
	return ValidateParams(data.Params)
}

// GetGenesisStateFromAppState returns x/market GenesisState given raw application
// genesis state.
func GetGenesisStateFromAppState(cdc codec.JSONCodec, appState map[string]json.RawMessage) *marketv1.GenesisState {
	var genesisState marketv1.GenesisState

	if appState[ModuleName] != nil {
		cdc.MustUnmarshalJSON(appState[ModuleName], &genesisState)
	}

	return &genesisState
}
