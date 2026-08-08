package types_test

import (
	"testing"

	"cosmossdk.io/math"

	"github.com/stretchr/testify/require"

	"ark/x/treasury/types"
)

func TestMonetaryPolicyValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.MonetaryPolicy)
		expectErr string
	}{
		{name: "default is valid", mutate: func(*types.MonetaryPolicy) {}},
		{
			name: "independent ratios may sum above one",
			mutate: func(p *types.MonetaryPolicy) {
				p.RedemptionBufferTargetRatio = math.LegacyOneDec()
				p.StrategicReserveTargetRatio = math.LegacyOneDec()
				p.InsuranceTargetRatio = math.LegacyOneDec()
			},
		},
		{name: "nil tax rate", mutate: func(p *types.MonetaryPolicy) { p.StabilityTaxRate = math.LegacyDec{} }, expectErr: "StabilityTaxRate must be set"},
		{name: "negative tax rate", mutate: func(p *types.MonetaryPolicy) { p.StabilityTaxRate = math.LegacyNewDec(-1) }, expectErr: "StabilityTaxRate must be between zero and one"},
		{name: "tax rate above one", mutate: func(p *types.MonetaryPolicy) {
			p.StabilityTaxRate = math.LegacyNewDecWithPrec(1001, 3)
		}, expectErr: "StabilityTaxRate must be between zero and one"},
		{name: "nil validator target", mutate: func(p *types.MonetaryPolicy) { p.ValidatorBlockRewardTarget = math.Int{} }, expectErr: "ValidatorBlockRewardTarget must be set"},
		{name: "negative validator target", mutate: func(p *types.MonetaryPolicy) { p.ValidatorBlockRewardTarget = math.NewInt(-1) }, expectErr: "ValidatorBlockRewardTarget must be zero or positive"},
		{name: "nil Oracle target", mutate: func(p *types.MonetaryPolicy) { p.OracleBlockRewardTarget = math.Int{} }, expectErr: "OracleBlockRewardTarget must be set"},
		{name: "negative Oracle target", mutate: func(p *types.MonetaryPolicy) { p.OracleBlockRewardTarget = math.NewInt(-1) }, expectErr: "OracleBlockRewardTarget must be zero or positive"},
		{
			name:   "validator target at the domain cap",
			mutate: func(p *types.MonetaryPolicy) { p.ValidatorBlockRewardTarget = types.MaxBlockRewardTarget },
		},
		{
			name: "validator target above the domain cap",
			mutate: func(p *types.MonetaryPolicy) {
				p.ValidatorBlockRewardTarget = types.MaxBlockRewardTarget.Add(math.OneInt())
			},
			expectErr: "ValidatorBlockRewardTarget must not exceed",
		},
		{
			name: "Oracle target above the domain cap",
			mutate: func(p *types.MonetaryPolicy) {
				p.OracleBlockRewardTarget = types.MaxBlockRewardTarget.Add(math.OneInt())
			},
			expectErr: "OracleBlockRewardTarget must not exceed",
		},
		{
			// The worst case the accrual can be handed, and admissible: the
			// headroom lives in the window multiplication rather than in
			// forbidding the pair, which the test below proves.
			name: "both targets at the domain cap",
			mutate: func(p *types.MonetaryPolicy) {
				p.ValidatorBlockRewardTarget = types.MaxBlockRewardTarget
				p.OracleBlockRewardTarget = types.MaxBlockRewardTarget
			},
		},
		{name: "nil Buffer ratio", mutate: func(p *types.MonetaryPolicy) { p.RedemptionBufferTargetRatio = math.LegacyDec{} }, expectErr: "RedemptionBufferTargetRatio must be set"},
		{name: "negative Reserve ratio", mutate: func(p *types.MonetaryPolicy) { p.StrategicReserveTargetRatio = math.LegacyNewDec(-1) }, expectErr: "StrategicReserveTargetRatio must be between zero and one"},
		{name: "Insurance ratio above one", mutate: func(p *types.MonetaryPolicy) {
			p.InsuranceTargetRatio = math.LegacyNewDecWithPrec(1001, 3)
		}, expectErr: "InsuranceTargetRatio must be between zero and one"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			policy := types.DefaultMonetaryPolicy()
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

// TestRewardTargetCapsLeaveWindowHeadroom is why the two caps exist in the
// shape they do. They replaced a validation that projected the whole window's
// arithmetic at every write of params or policy: a projection has to be
// re-derived by every future writer of every input it reads, and its verdict
// moves with live state, where a cap is a fact about one field checked where
// that field is validated. That trade only holds if the caps' product clears
// the integer ceiling with room to spare — both targets, every block of the
// longest admissible window.
func TestRewardTargetCapsLeaveWindowHeadroom(t *testing.T) {
	perBlock, err := types.MaxBlockRewardTarget.SafeAdd(types.MaxBlockRewardTarget)
	require.NoError(t, err)
	whole, err := perBlock.SafeMul(math.NewIntFromUint64(types.MaxRewardFundingWindow))
	require.NoError(t, err)
	require.Less(t, whole.BigInt().BitLen(), math.MaxBitLen-64,
		"a whole window must stay far under the integer limit")
}

// TestFundTargetsRoundUp pins the rounding direction, which is a consensus rule
// rather than a preference: a target sizes a requirement, so any fraction of a
// base unit becomes a whole one. Rounded down, a fund would report itself full
// while sitting a base unit short of its own policy.
func TestFundTargetsRoundUp(t *testing.T) {
	policy := types.DefaultMonetaryPolicy()
	policy.RedemptionBufferTargetRatio = math.LegacyNewDecWithPrec(5, 1)
	policy.StrategicReserveTargetRatio = math.LegacyNewDecWithPrec(25, 2)

	t.Run("an exact product keeps its value", func(t *testing.T) {
		targets := policy.FundTargets(math.LegacyNewDec(100))
		require.True(t, math.NewInt(50).Equal(targets.Buffer), "buffer: %s", targets.Buffer)
		require.True(t, math.NewInt(25).Equal(targets.Reserve), "reserve: %s", targets.Reserve)
	})

	t.Run("a fractional product takes the whole unit", func(t *testing.T) {
		targets := policy.FundTargets(math.LegacyNewDec(101))
		require.True(t, math.NewInt(51).Equal(targets.Buffer), "buffer: %s", targets.Buffer)
		require.True(t, math.NewInt(26).Equal(targets.Reserve), "reserve: %s", targets.Reserve)
	})

	t.Run("a requirement under one base unit still requires one", func(t *testing.T) {
		targets := policy.FundTargets(math.LegacyOneDec())
		require.True(t, math.OneInt().Equal(targets.Buffer), "buffer: %s", targets.Buffer)
		require.True(t, math.OneInt().Equal(targets.Reserve), "reserve: %s", targets.Reserve)
	})

	t.Run("a zero ratio requires nothing", func(t *testing.T) {
		targets := policy.FundTargets(math.LegacyNewDec(1_000_000))
		require.True(t, targets.Insurance.IsZero(), "insurance: %s", targets.Insurance)
	})
}
