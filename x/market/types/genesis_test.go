package types

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"
)

func TestValidateGenesisState(t *testing.T) {
	genState := DefaultGenesisState()
	require.NoError(t, genState.Validate())

	genState.Params.BasePool = math.LegacyNewDec(-1)
	require.Error(t, genState.Validate())

	genState = DefaultGenesisState()
	genState.Params.PoolRecoveryPeriod = 0
	require.Error(t, genState.Validate())

	genState = DefaultGenesisState()
	genState.Params.MinStabilitySpread = math.LegacyNewDec(-1)
	require.Error(t, genState.Validate())
}
