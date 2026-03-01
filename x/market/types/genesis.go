package types

import (
	"encoding/json"
	"fmt"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
)

// NewGenesisState creates a new GenesisState object
func NewGenesisState(noahPoolDelta math.LegacyDec, params Params) *GenesisState {
	return &GenesisState{
		NoahPoolDelta: noahPoolDelta,
		Params:        params,
	}
}

// DefaultGenesisState returns raw genesis raw message for testing
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		NoahPoolDelta: math.LegacyZeroDec(),
		Params:        DefaultParams(),
	}
}

// Validate validates the provided market genesis state
func (gs GenesisState) Validate() error {
	if gs.NoahPoolDelta.IsNil() {
		return fmt.Errorf("noah pool delta must not be nil")
	}

	return gs.Params.Validate()
}

// GetGenesisStateFromAppState returns x/market GenesisState given raw application
// genesis state.
func GetGenesisStateFromAppState(cdc codec.JSONCodec, appState map[string]json.RawMessage) *GenesisState {
	var genesisState GenesisState

	if appState[ModuleName] != nil {
		cdc.MustUnmarshalJSON(appState[ModuleName], &genesisState)
	}

	return &genesisState
}
