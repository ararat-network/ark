package app

import (
	"encoding/json"
	"os"
	"testing"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	ibcwasmtypes "github.com/cosmos/ibc-go/modules/light-clients/08-wasm/v11/types"
	"github.com/stretchr/testify/require"
)

// The contract runtime must ship shut. Wasmd's default genesis opens upload
// and instantiation to everybody and Ark keeps that default in code, so the
// launch posture holds in exactly one place: the curated genesis under
// app/genesis. This pins it there.
func TestWasmGenesisShipsDisabled(t *testing.T) {
	raw, err := os.ReadFile("genesis/genesis.json")
	require.NoError(t, err)

	var genesis struct {
		AppState map[string]json.RawMessage `json:"app_state"`
	}
	require.NoError(t, json.Unmarshal(raw, &genesis))
	require.NotNil(t, genesis.AppState[wasmtypes.ModuleName], "wasm genesis must be present")

	var state wasmtypes.GenesisState
	require.NoError(t, json.Unmarshal(genesis.AppState[wasmtypes.ModuleName], &state))

	require.Equal(t, wasmtypes.AccessTypeNobody, state.Params.CodeUploadAccess.Permission,
		"nobody may upload contract code at launch")
	require.Equal(t, wasmtypes.AccessTypeNobody, state.Params.InstantiateDefaultPermission,
		"nobody may instantiate contracts at launch")
	require.Empty(t, state.Codes, "launch genesis carries no contract code")
	require.Empty(t, state.Contracts, "launch genesis carries no contracts")
}

// The 08-wasm light-client host ships the same way: present, with no client
// code. Uploading a light client is a governance action after launch.
func TestWasmLightClientGenesisShipsEmpty(t *testing.T) {
	raw, err := os.ReadFile("genesis/genesis.json")
	require.NoError(t, err)

	var genesis struct {
		AppState map[string]json.RawMessage `json:"app_state"`
	}
	require.NoError(t, json.Unmarshal(raw, &genesis))
	require.NotNil(t, genesis.AppState[ibcwasmtypes.ModuleName], "08-wasm genesis must be present")

	var state ibcwasmtypes.GenesisState
	require.NoError(t, json.Unmarshal(genesis.AppState[ibcwasmtypes.ModuleName], &state))
	require.Empty(t, state.Contracts, "launch genesis carries no light-client code")
}
