package types

import (
	"encoding/json"
	"errors"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
)

// NewGenesisState creates a new GenesisState object
func NewGenesisState(arkPoolDelta math.LegacyDec, params Params) *GenesisState {
	return &GenesisState{
		ArkPoolDelta: arkPoolDelta,
		Params:       params,
	}
}

// DefaultGenesisState returns raw genesis raw message for testing
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		ArkPoolDelta: math.LegacyZeroDec(),
		Params:       DefaultParams(),
	}
}

// Validate validates the provided market genesis state
func (gs GenesisState) Validate() error {
	if gs.ArkPoolDelta.IsNil() {
		return errors.New("ark pool delta must not be nil")
	}

	if err := gs.Params.Validate(); err != nil {
		return err
	}

	if _, err := NewEffectivePools(gs.Params.BasePool.Amount, gs.ArkPoolDelta); err != nil {
		return err
	}

	return nil
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
