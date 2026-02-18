package types

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
)

func TestValidateGenesis(t *testing.T) {
	// Default genesis should be valid
	require.NoError(t, ValidateGenesis(DefaultGenesisState()))

	// Invalid params: negative base pool
	gen := DefaultGenesisState()
	gen.Params.BasePool = math.LegacyNewDec(-1)
	require.Error(t, ValidateGenesis(gen))

	// Invalid params: zero pool recovery period
	gen = DefaultGenesisState()
	gen.Params.PoolRecoveryPeriod = 0
	require.Error(t, ValidateGenesis(gen))
}

func TestNewGenesisState(t *testing.T) {
	delta := math.LegacyNewDec(12345)
	params := DefaultParams()

	gen := NewGenesisState(delta, params)
	require.True(t, delta.Equal(gen.NoahPoolDelta))
	require.Equal(t, params.BasePool, gen.Params.BasePool)
	require.Equal(t, params.PoolRecoveryPeriod, gen.Params.PoolRecoveryPeriod)
	require.Equal(t, params.MinStabilitySpread, gen.Params.MinStabilitySpread)
}
