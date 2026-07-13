package types_test

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"ark/x/market/types"
)

func TestNewEffectivePools(t *testing.T) {
	tests := []struct {
		name      string
		basePool  math.LegacyDec
		poolDelta math.LegacyDec
		expectErr string
	}{
		{
			name:      "balanced pools",
			basePool:  math.LegacyNewDec(400),
			poolDelta: math.LegacyZeroDec(),
		},
		{
			name:      "non-positive ark pool",
			basePool:  math.LegacyNewDec(400),
			poolDelta: math.LegacyNewDec(-400),
			expectErr: "effective ark pool must be positive",
		},
		{
			name:      "constant product overflow",
			basePool:  maxLegacyDec(),
			poolDelta: math.LegacyZeroDec(),
			expectErr: "constant product",
		},
		{
			name:      "ark pool addition overflow",
			basePool:  math.LegacyOneDec(),
			poolDelta: maxLegacyDec(),
			expectErr: "effective ark pool",
		},
		{
			name:     "noah pool quotient overflow",
			basePool: math.LegacyNewDecFromBigInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil)),
			poolDelta: math.LegacyNewDecFromBigInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil)).
				Neg().
				Add(math.LegacySmallestDec()),
			expectErr: "effective noah pool",
		},
		{
			name:      "noah pool underflows to zero",
			basePool:  math.LegacyOneDec(),
			poolDelta: math.LegacyNewDecFromBigInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil)).Sub(math.LegacyOneDec()),
			expectErr: "effective noah pool must be positive",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.NotPanics(t, func() {
				pools, err := types.NewEffectivePools(tc.basePool, tc.poolDelta)
				if tc.expectErr != "" {
					require.ErrorContains(t, err, tc.expectErr)
					return
				}

				require.NoError(t, err)
				require.True(t, math.LegacyNewDec(160000).Equal(pools.ConstantProduct))
				require.True(t, tc.basePool.Equal(pools.ArkPool))
				require.True(t, tc.basePool.Equal(pools.NoahPool))
			})
		})
	}
}

func maxLegacyDec() math.LegacyDec {
	precision := new(big.Int).Exp(big.NewInt(10), big.NewInt(math.LegacyPrecision), nil)
	raw := new(big.Int).Lsh(big.NewInt(1), 256)
	raw.Mul(raw, precision)
	raw.Sub(raw, big.NewInt(1))
	return math.LegacyNewDecFromBigIntWithPrec(raw, math.LegacyPrecision)
}
