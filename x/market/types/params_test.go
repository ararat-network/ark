package types

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"
)

func TestValidateParams(t *testing.T) {
	// Default params should be valid
	p := DefaultParams()
	require.NoError(t, p.Validate())

	// Negative base pool
	p = DefaultParams()
	p.BasePool = math.LegacyNewDec(-1)
	require.Error(t, p.Validate())

	// Zero pool recovery period
	p = DefaultParams()
	p.PoolRecoveryPeriod = 0
	require.Error(t, p.Validate())

	// Negative min stability spread
	p = DefaultParams()
	p.MinStabilitySpread = math.LegacyNewDec(-1)
	require.Error(t, p.Validate())

	// Min stability spread > 1
	p = DefaultParams()
	p.MinStabilitySpread = math.LegacyNewDecWithPrec(15, 1) // 1.5
	require.Error(t, p.Validate())
}
