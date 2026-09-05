package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/market/types"
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
				policy.BasePool = sdk.DecCoin{Denom: chain.XDRBaseDenom}
			},
			expectErr: "base pool amount must be set",
		},
		{
			// A zero depth cannot be scaled from when depth changes, and it
			// makes the constant product degenerate.
			name: "zero base pool",
			mutate: func(policy *types.ConversionPolicy) {
				policy.BasePool = sdk.NewDecCoin(chain.XDRBaseDenom, math.ZeroInt())
			},
			expectErr: "base pool must be positive",
		},
		{
			name: "negative base pool",
			mutate: func(policy *types.ConversionPolicy) {
				policy.BasePool = sdk.DecCoin{
					Denom:  chain.XDRBaseDenom,
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
				policy.BasePool = sdk.NewDecCoinFromDec(chain.XDRBaseDenom, maxLegacyDec())
			},
			expectErr: "base pool square must be representable",
		},
		{
			name: "zero pool recovery period",
			mutate: func(policy *types.ConversionPolicy) {
				policy.PoolRecoveryPeriod = 0
			},
			expectErr: "pool recovery period must be between one and",
		},
		{
			name: "pool recovery period at the domain cap",
			mutate: func(policy *types.ConversionPolicy) {
				policy.PoolRecoveryPeriod = types.MaxPoolRecoveryPeriod
			},
		},
		{
			// The period is a divisor: long enough and the per-block quotient
			// truncates to nothing, so the pool keeps its imbalance for good.
			name: "pool recovery period above the domain cap",
			mutate: func(policy *types.ConversionPolicy) {
				policy.PoolRecoveryPeriod = types.MaxPoolRecoveryPeriod + 1
			},
			expectErr: "pool recovery period must be between one and",
		},
		{
			name: "single-block recovery period is valid",
			mutate: func(policy *types.ConversionPolicy) {
				policy.PoolRecoveryPeriod = 1
			},
		},
		{
			name: "nil min stability spread",
			mutate: func(policy *types.ConversionPolicy) {
				policy.MinStabilitySpread = math.LegacyDec{}
			},
			expectErr: "min stability spread must be set",
		},
		{
			name: "negative min stability spread",
			mutate: func(policy *types.ConversionPolicy) {
				policy.MinStabilitySpread = math.LegacyNewDec(-1)
			},
			expectErr: "min stability spread must be in [0, 1]",
		},
		{
			name: "min stability spread greater than 1",
			mutate: func(policy *types.ConversionPolicy) {
				policy.MinStabilitySpread = math.LegacyNewDecWithPrec(101, 2)
			},
			expectErr: "min stability spread must be in [0, 1]",
		},
		{
			// Zero leaves the constant product alone to price every conversion.
			name: "zero min stability spread is valid",
			mutate: func(policy *types.ConversionPolicy) {
				policy.MinStabilitySpread = math.LegacyZeroDec()
			},
		},
		{
			// Unlike a Tobin rate, a floor of one is a deliberate halt on NOAH-pair
			// conversion rather than a refusal dressed as a fee.
			name: "min stability spread of one is valid",
			mutate: func(policy *types.ConversionPolicy) {
				policy.MinStabilitySpread = math.LegacyOneDec()
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
	require.Equal(t, chain.XDRBaseDenom, policy.BasePool.Denom)
	require.True(
		t,
		math.LegacyNewDecFromInt(chain.NativeBaseAmount(1_000_000)).Equal(policy.BasePool.Amount),
	)
	require.Equal(t, chain.BlocksPerDay, policy.PoolRecoveryPeriod)
	require.True(t, math.LegacyNewDecWithPrec(2, 2).Equal(policy.MinStabilitySpread))
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
			// amount. Nil is not a zero anybody wrote, and the sentinel has one
			// spelling, so it does not read as the disabled payload — the answer
			// is false rather than a panic.
			name:   "unset amount is not zero",
			policy: types.ConversionPolicy{},
		},
		{
			name:   "unset spread is not zero",
			policy: types.ConversionPolicy{BasePool: sdk.DecCoin{Amount: math.LegacyZeroDec()}},
		},
		{
			name:   "depth only",
			policy: types.ConversionPolicy{BasePool: sdk.NewDecCoin(chain.XDRBaseDenom, math.OneInt())},
		},
		{
			name:   "recovery period only",
			policy: types.ConversionPolicy{PoolRecoveryPeriod: 1},
		},
		{
			// A floor alone is still a delegation, so it must not read as the
			// disabled sentinel.
			name:   "spread floor only",
			policy: types.ConversionPolicy{MinStabilitySpread: math.LegacyNewDecWithPrec(1, 2)},
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

	pricier := base
	pricier.MinStabilitySpread = base.MinStabilitySpread.Add(math.LegacyNewDecWithPrec(1, 2))

	require.True(t, base.Equal(types.DefaultConversionPolicy()))
	require.False(t, base.Equal(deeper))
	require.False(t, base.Equal(relabelled))
	require.False(t, base.Equal(slower))
	require.False(t, base.Equal(pricier))
	// Nil amounts compare without panicking, in both directions.
	require.True(t, types.ConversionPolicy{}.Equal(types.ConversionPolicy{})) //nolint:gocritic // distinct zero values; nil-amount comparison is the case under test
	require.False(t, types.ConversionPolicy{}.Equal(types.ZeroConversionPolicy()))
	require.False(t, types.ZeroConversionPolicy().Equal(types.ConversionPolicy{}))
}
