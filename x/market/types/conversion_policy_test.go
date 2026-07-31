package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	"ark/x/market/types"
)

func TestValidateConversionPolicy(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.ConversionPolicy)
		expectErr string
	}{
		{
			name:   "default conversion policy",
			mutate: func(*types.ConversionPolicy) {},
		},
		{
			name: "nil base pool",
			mutate: func(policy *types.ConversionPolicy) {
				policy.BasePool = sdk.DecCoin{}
			},
			expectErr: "base pool amount must be set",
		},
		{
			name: "nil base pool amount with valid denom",
			mutate: func(policy *types.ConversionPolicy) {
				policy.BasePool = sdk.DecCoin{Denom: chain.SDRBaseDenom}
			},
			expectErr: "base pool amount must be set",
		},
		{
			// A zero depth cannot be scaled from when depth changes, and it
			// makes the constant product degenerate.
			name: "zero base pool",
			mutate: func(policy *types.ConversionPolicy) {
				policy.BasePool = sdk.NewDecCoin(chain.SDRBaseDenom, math.ZeroInt())
			},
			expectErr: "base pool must be positive",
		},
		{
			name: "negative base pool",
			mutate: func(policy *types.ConversionPolicy) {
				policy.BasePool = sdk.DecCoin{
					Denom:  chain.SDRBaseDenom,
					Amount: math.LegacyNewDec(-1),
				}
			},
			expectErr: "invalid base pool",
		},
		{
			name: "empty base pool denom",
			mutate: func(policy *types.ConversionPolicy) {
				policy.BasePool.Denom = ""
			},
			expectErr: "invalid base pool",
		},
		{
			name: "base pool square is out of range",
			mutate: func(policy *types.ConversionPolicy) {
				policy.BasePool = sdk.NewDecCoinFromDec(chain.SDRBaseDenom, maxLegacyDec())
			},
			expectErr: "base pool square must be representable",
		},
		{
			name: "zero pool recovery period",
			mutate: func(policy *types.ConversionPolicy) {
				policy.PoolRecoveryPeriod = 0
			},
			expectErr: "pool recovery period must be positive",
		},
		{
			name: "single-block recovery period is valid",
			mutate: func(policy *types.ConversionPolicy) {
				policy.PoolRecoveryPeriod = 1
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			policy := types.DefaultConversionPolicy()
			tc.mutate(&policy)
			err := policy.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.expectErr)
		})
	}
}

func TestDefaultConversionPolicyValues(t *testing.T) {
	policy := types.DefaultConversionPolicy()
	require.Equal(t, chain.SDRBaseDenom, policy.BasePool.Denom)
	require.True(
		t,
		math.LegacyNewDecFromInt(chain.NativeBaseAmount(1_000_000)).Equal(policy.BasePool.Amount),
	)
	require.Equal(t, uint64(chain.BlocksPerDay), policy.PoolRecoveryPeriod)
}

// TestZeroConversionPolicyIsNotLaunchable pins why the disabled mandate sentinel
// cannot be DefaultConversionPolicy: zero is the one pair Validate rejects, which
// is what makes it unmistakably "no delegation".
func TestZeroConversionPolicyIsNotLaunchable(t *testing.T) {
	zero := types.ZeroConversionPolicy()
	require.True(t, zero.IsZero())
	require.Error(t, zero.Validate())
	require.False(t, types.DefaultConversionPolicy().IsZero())
}

func TestConversionPolicyIsZero(t *testing.T) {
	tests := []struct {
		name   string
		policy types.ConversionPolicy
		want   bool
	}{
		{
			name:   "canonical zero",
			policy: types.ZeroConversionPolicy(),
			want:   true,
		},
		{
			// A bound supplied as an empty JSON object arrives with a nil
			// amount, and must still read as the disabled sentinel rather than
			// panicking on a nil decimal.
			name:   "unset amount counts as zero",
			policy: types.ConversionPolicy{},
			want:   true,
		},
		{
			name:   "depth only",
			policy: types.ConversionPolicy{BasePool: sdk.NewDecCoin(chain.SDRBaseDenom, math.OneInt())},
		},
		{
			name:   "recovery period only",
			policy: types.ConversionPolicy{PoolRecoveryPeriod: 1},
		},
		{
			name:   "default",
			policy: types.DefaultConversionPolicy(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.policy.IsZero())
		})
	}
}

func TestConversionPolicyEqual(t *testing.T) {
	base := types.DefaultConversionPolicy()

	deeper := base
	deeper.BasePool = sdk.NewDecCoinFromDec(base.BasePool.Denom, base.BasePool.Amount.MulInt64(2))

	relabelled := base
	relabelled.BasePool = sdk.NewDecCoinFromDec(chain.USDBaseDenom, base.BasePool.Amount)

	slower := base
	slower.PoolRecoveryPeriod = base.PoolRecoveryPeriod + 1

	require.True(t, base.Equal(types.DefaultConversionPolicy()))
	require.False(t, base.Equal(deeper))
	require.False(t, base.Equal(relabelled))
	require.False(t, base.Equal(slower))
	// Nil amounts compare without panicking, in both directions.
	require.True(t, types.ConversionPolicy{}.Equal(types.ConversionPolicy{}))
	require.False(t, types.ConversionPolicy{}.Equal(types.ZeroConversionPolicy()))
	require.False(t, types.ZeroConversionPolicy().Equal(types.ConversionPolicy{}))
}
