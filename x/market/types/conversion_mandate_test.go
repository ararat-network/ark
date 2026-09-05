package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/market/types"
)

func testCapacityCommittee() string {
	return authtypes.NewModuleAddress("capacity-committee").String()
}

// conversionBounds returns a corridor around the launch depth: half to double,
// with the recovery period allowed to shorten to a quarter day. Halving the
// period and deepening the pool together is the historical depeg response.
//
// The spread floor is bounded raise-only, which is how governance is expected
// to delegate it: the minimum is the live floor, so the committee can lift the
// floor up to four times launch but never below where it already stands.
func conversionBounds() (types.ConversionPolicy, types.ConversionPolicy) {
	launch := types.DefaultConversionPolicy()
	minimum := types.ConversionPolicy{
		BasePool: sdk.NewDecCoinFromDec(
			launch.BasePool.Denom,
			launch.BasePool.Amount.QuoInt64(2),
		),
		PoolRecoveryPeriod: launch.PoolRecoveryPeriod / 4,
		MinStabilitySpread: launch.MinStabilitySpread,
	}
	maximum := types.ConversionPolicy{
		BasePool: sdk.NewDecCoinFromDec(
			launch.BasePool.Denom,
			launch.BasePool.Amount.MulInt64(2),
		),
		PoolRecoveryPeriod: launch.PoolRecoveryPeriod,
		MinStabilitySpread: launch.MinStabilitySpread.MulInt64(4),
	}

	return minimum, maximum
}

func enabledConversionMandate() types.ConversionMandate {
	minimum, maximum := conversionBounds()
	appointment := types.NewDisabledConversionMandate(1)
	appointment.Committee = testCapacityCommittee()
	appointment.ActivationHeight = 10
	appointment.ExpiryHeight = 20
	appointment.MinimumPolicy = minimum
	appointment.MaximumPolicy = maximum

	return appointment
}

// TestConversionMandateIsActive covers both halves of the mandate's shadowed
// predicate: the envelope window, and the corridor sharing the live pool's
// unit. The second half is what a strand takes away and a re-point back
// restores, so the pure function is pinned at every boundary the keeper flows
// exercise.
func TestConversionMandateIsActive(t *testing.T) {
	appointment := enabledConversionMandate()
	live := types.DefaultConversionPolicy()
	stranded := types.DefaultConversionPolicy()
	stranded.BasePool = sdk.NewDecCoinFromDec("ausd", live.BasePool.Amount)

	tests := []struct {
		name       string
		mandate    types.ConversionMandate
		livePolicy types.ConversionPolicy
		height     uint64
		active     bool
	}{
		{
			name:       "inside the window with matching units",
			mandate:    appointment,
			livePolicy: live,
			height:     10,
			active:     true,
		},
		{
			name:       "last height of the half-open window",
			mandate:    appointment,
			livePolicy: live,
			height:     19,
			active:     true,
		},
		{
			name:       "before activation",
			mandate:    appointment,
			livePolicy: live,
			height:     9,
		},
		{
			name:       "at expiry",
			mandate:    appointment,
			livePolicy: live,
			height:     20,
		},
		{
			name:       "stranded by a pool in another unit",
			mandate:    appointment,
			livePolicy: stranded,
			height:     10,
		},
		{
			name:       "disabled",
			mandate:    types.DefaultConversionMandate(),
			livePolicy: live,
			height:     10,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.active, tc.mandate.IsActive(tc.livePolicy, tc.height))
		})
	}
}

func TestDefaultConversionMandateIsDisabled(t *testing.T) {
	appointment := types.DefaultConversionMandate()
	require.True(t, appointment.IsDisabled())
	require.Equal(t, uint64(0), appointment.Term)
	require.True(t, appointment.MinimumPolicy.IsZero())
	require.True(t, appointment.MaximumPolicy.IsZero())
	require.NoError(t, appointment.Validate())
}

func TestValidateConversionMandate(t *testing.T) {
	tests := []struct {
		name      string
		mandate   func() types.ConversionMandate
		mutate    func(*types.ConversionMandate)
		expectErr string
	}{
		{
			name:    "disabled default",
			mandate: types.DefaultConversionMandate,
			mutate:  func(*types.ConversionMandate) {},
		},
		{
			name:    "disabled retaining term",
			mandate: func() types.ConversionMandate { return types.NewDisabledConversionMandate(7) },
			mutate:  func(*types.ConversionMandate) {},
		},
		{
			name:    "configured",
			mandate: enabledConversionMandate,
			mutate:  func(*types.ConversionMandate) {},
		},
		{
			// A disabled mandate delegates nothing, so carrying live-looking
			// bounds would misreport the appointment's reach.
			name:    "disabled with bounds",
			mandate: types.DefaultConversionMandate,
			mutate: func(m *types.ConversionMandate) {
				_, maximum := conversionBounds()
				m.MaximumPolicy = maximum
			},
			expectErr: "disabled conversion mandate must use identical zero bounds",
		},
		{
			// A corridor bound is refused unset for the same reason the Tobin
			// cap is: the sentinel is one shape, and a policy that omits a
			// decimal is not it.
			name:    "unset depth on the disabled sentinel",
			mandate: types.DefaultConversionMandate,
			mutate: func(m *types.ConversionMandate) {
				m.MinimumPolicy.BasePool.Amount = math.LegacyDec{}
			},
			expectErr: "disabled conversion mandate must use identical zero bounds",
		},
		{
			name:    "unset spread on the disabled sentinel",
			mandate: types.DefaultConversionMandate,
			mutate: func(m *types.ConversionMandate) {
				m.MaximumPolicy.MinStabilitySpread = math.LegacyDec{}
			},
			expectErr: "disabled conversion mandate must use identical zero bounds",
		},
		{
			name:      "configured zero term",
			mandate:   enabledConversionMandate,
			mutate:    func(m *types.ConversionMandate) { m.Term = 0 },
			expectErr: "conversion mandate: configured term must be positive",
		},
		{
			name:      "non-canonical committee",
			mandate:   enabledConversionMandate,
			mutate:    func(m *types.ConversionMandate) { m.Committee = "invalid" },
			expectErr: "conversion mandate: committee is invalid",
		},
		{
			name:      "empty window",
			mandate:   enabledConversionMandate,
			mutate:    func(m *types.ConversionMandate) { m.ActivationHeight = m.ExpiryHeight },
			expectErr: "conversion mandate: activation height must precede expiry height",
		},
		{
			name:      "zero minimum depth",
			mandate:   enabledConversionMandate,
			mutate:    func(m *types.ConversionMandate) { m.MinimumPolicy.BasePool.Amount = math.LegacyZeroDec() },
			expectErr: "invalid conversion minimum: base pool must be positive",
		},
		{
			name:      "zero maximum recovery period",
			mandate:   enabledConversionMandate,
			mutate:    func(m *types.ConversionMandate) { m.MaximumPolicy.PoolRecoveryPeriod = 0 },
			expectErr: "invalid conversion maximum: pool recovery period must be between one and",
		},
		{
			// One corridor cannot span two units: the range would be
			// meaningless and no candidate could satisfy both ends.
			name:    "bounds in different denominations",
			mandate: enabledConversionMandate,
			mutate: func(m *types.ConversionMandate) {
				m.MaximumPolicy.BasePool.Denom = chain.USDBaseDenom
			},
			expectErr: "conversion bounds must share one denomination",
		},
		{
			name:    "inverted depth bounds",
			mandate: enabledConversionMandate,
			mutate: func(m *types.ConversionMandate) {
				m.MinimumPolicy.BasePool.Amount = m.MaximumPolicy.BasePool.Amount.MulInt64(2)
			},
			expectErr: "minimum base pool",
		},
		{
			name:    "inverted recovery period bounds",
			mandate: enabledConversionMandate,
			mutate: func(m *types.ConversionMandate) {
				m.MinimumPolicy.PoolRecoveryPeriod = m.MaximumPolicy.PoolRecoveryPeriod + 1
			},
			expectErr: "minimum pool recovery period",
		},
		{
			// A single-point corridor is a legitimate delegation: governance
			// may pin the exact value a committee is allowed to set.
			name:    "identical bounds",
			mandate: enabledConversionMandate,
			mutate: func(m *types.ConversionMandate) {
				m.MinimumPolicy = m.MaximumPolicy
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			appointment := tc.mandate()
			tc.mutate(&appointment)
			err := appointment.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.expectErr)
		})
	}
}

func TestValidateConversionMandateTobinCap(t *testing.T) {
	tests := []struct {
		name      string
		mandate   func() types.ConversionMandate
		mutate    func(*types.ConversionMandate)
		expectErr string
	}{
		{
			// A disabled mandate delegates nothing, Tobin power included.
			name:    "disabled with a Tobin cap",
			mandate: types.DefaultConversionMandate,
			mutate: func(m *types.ConversionMandate) {
				m.MaxTobinTax = math.LegacyNewDecWithPrec(2, 2)
			},
			expectErr: "disabled conversion mandate must carry a zero Tobin cap",
		},
		{
			// An enabled appointment states its whole delegation explicitly, so
			// an omitted cap is refused like an omitted corridor bound. A
			// capacity-only committee is spelled with an explicit zero.
			name:      "nil Tobin cap on an enabled mandate",
			mandate:   enabledConversionMandate,
			mutate:    func(m *types.ConversionMandate) { m.MaxTobinTax = math.LegacyDec{} },
			expectErr: "invalid conversion mandate Tobin cap: tobin tax must be set",
		},
		{
			// Omission is not a spelling of zero on either branch. The sentinel
			// stores an unset cap as zero, so nothing downstream would see the
			// difference — which is the reason to refuse it here, where the
			// difference between a delegation zeroed and a field never written
			// is still visible.
			name:      "nil Tobin cap on the disabled sentinel",
			mandate:   types.DefaultConversionMandate,
			mutate:    func(m *types.ConversionMandate) { m.MaxTobinTax = math.LegacyDec{} },
			expectErr: "disabled conversion mandate must carry a zero Tobin cap",
		},
		{
			name:    "zero Tobin cap",
			mandate: enabledConversionMandate,
			mutate:  func(m *types.ConversionMandate) { m.MaxTobinTax = math.LegacyZeroDec() },
		},
		{
			name:    "chargeable Tobin cap",
			mandate: enabledConversionMandate,
			mutate: func(m *types.ConversionMandate) {
				m.MaxTobinTax = math.LegacyNewDecWithPrec(5, 2)
			},
		},
		{
			name:    "negative Tobin cap",
			mandate: enabledConversionMandate,
			mutate: func(m *types.ConversionMandate) {
				m.MaxTobinTax = math.LegacyNewDecWithPrec(-1, 4)
			},
			expectErr: "invalid conversion mandate Tobin cap",
		},
		{
			// The cap bounds overrides, and no override may reach one: a cap
			// of one could authorize a raise ValidateTobinTax refuses.
			name:      "Tobin cap of one",
			mandate:   enabledConversionMandate,
			mutate:    func(m *types.ConversionMandate) { m.MaxTobinTax = math.LegacyOneDec() },
			expectErr: "invalid conversion mandate Tobin cap",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			appointment := tc.mandate()
			tc.mutate(&appointment)
			err := appointment.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.expectErr)
		})
	}
}

func TestConversionMandateValidateTobinCandidate(t *testing.T) {
	appointment := enabledConversionMandate()
	appointment.MaxTobinTax = math.LegacyNewDecWithPrec(5, 2)
	powerless := enabledConversionMandate()

	tests := []struct {
		name         string
		mandate      types.ConversionMandate
		defaultTobin math.LegacyDec
		candidate    math.LegacyDec
		expectErr    string
	}{
		{
			name:         "inside the band",
			mandate:      appointment,
			defaultTobin: types.DefaultTobinTax,
			candidate:    math.LegacyNewDecWithPrec(2, 2),
		},
		{
			// The floor itself is reachable, which is what lets a committee
			// retire its own emergency rate without waiting for a proposal.
			name:         "back down to the default",
			mandate:      appointment,
			defaultTobin: types.DefaultTobinTax,
			candidate:    types.DefaultTobinTax,
		},
		{
			name:         "exactly at the cap",
			mandate:      appointment,
			defaultTobin: types.DefaultTobinTax,
			candidate:    math.LegacyNewDecWithPrec(5, 2),
		},
		{
			name:         "capacity-only mandate",
			mandate:      powerless,
			defaultTobin: types.DefaultTobinTax,
			candidate:    math.LegacyNewDecWithPrec(2, 2),
			expectErr:    "conversion mandate delegates no Tobin power",
		},
		{
			// The corner that makes the zero-cap check authorization rather than
			// redundancy: with a zero default, a zero candidate satisfies both
			// band edges, so without the explicit check a capacity-only committee
			// could write a zero override and pin the denomination against a
			// later default raise.
			name:         "capacity-only mandate with a zero default",
			mandate:      powerless,
			defaultTobin: math.LegacyZeroDec(),
			candidate:    math.LegacyZeroDec(),
			expectErr:    "conversion mandate delegates no Tobin power",
		},
		{
			name:         "nil candidate",
			mandate:      appointment,
			defaultTobin: types.DefaultTobinTax,
			candidate:    math.LegacyDec{},
			expectErr:    "tobin tax must be set",
		},
		{
			name:         "candidate of one",
			mandate:      appointment,
			defaultTobin: types.DefaultTobinTax,
			candidate:    math.LegacyOneDec(),
			expectErr:    "tobin tax must be in [0, 1)",
		},
		{
			// Below the default is governance's alone: the committee may retire
			// its own rate but never take a denomination under the chain-wide
			// baseline.
			name:         "candidate below the default",
			mandate:      appointment,
			defaultTobin: math.LegacyNewDecWithPrec(3, 2),
			candidate:    math.LegacyNewDecWithPrec(2, 2),
			expectErr:    "below the default rate",
		},
		{
			name:         "candidate above the cap",
			mandate:      appointment,
			defaultTobin: types.DefaultTobinTax,
			candidate:    math.LegacyNewDecWithPrec(6, 2),
			expectErr:    "exceeds the mandate cap",
		},
		{
			// The band is bounded by the default and the cap alone, so where a
			// denomination currently sits never enters into it — including when
			// governance has parked one above the cap.
			name:         "cap below the default leaves an empty band",
			mandate:      appointment,
			defaultTobin: math.LegacyNewDecWithPrec(10, 2),
			candidate:    math.LegacyNewDecWithPrec(10, 2),
			expectErr:    "exceeds the mandate cap",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.mandate.ValidateTobinCandidate(tc.defaultTobin, tc.candidate)
			if tc.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.expectErr)
		})
	}
}

func TestConversionMandateValidatePolicy(t *testing.T) {
	appointment := enabledConversionMandate()
	minimum, maximum := conversionBounds()

	launch := types.DefaultConversionPolicy()
	inside := types.ConversionPolicy{
		BasePool:           launch.BasePool,
		PoolRecoveryPeriod: launch.PoolRecoveryPeriod / 2,
		MinStabilitySpread: launch.MinStabilitySpread.MulInt64(2),
	}

	// Each rejection case starts from a candidate the corridor accepts and moves
	// exactly one field out of range, so the error named is the only one the
	// candidate could have earned.
	outside := func(mutate func(*types.ConversionPolicy)) types.ConversionPolicy {
		policy := inside
		mutate(&policy)

		return policy
	}

	tests := []struct {
		name      string
		policy    types.ConversionPolicy
		expectErr string
	}{
		{name: "inside the corridor", policy: inside},
		{name: "at the minimum bound", policy: minimum},
		{name: "at the maximum bound", policy: maximum},
		{
			name: "below the minimum depth",
			policy: outside(func(p *types.ConversionPolicy) {
				p.BasePool = sdk.NewDecCoinFromDec(
					minimum.BasePool.Denom,
					minimum.BasePool.Amount.QuoInt64(2),
				)
			}),
			expectErr: "base pool",
		},
		{
			name: "above the maximum depth",
			policy: outside(func(p *types.ConversionPolicy) {
				p.BasePool = sdk.NewDecCoinFromDec(
					maximum.BasePool.Denom,
					maximum.BasePool.Amount.MulInt64(2),
				)
			}),
			expectErr: "base pool",
		},
		{
			name: "below the minimum recovery period",
			policy: outside(func(p *types.ConversionPolicy) {
				p.PoolRecoveryPeriod = minimum.PoolRecoveryPeriod - 1
			}),
			expectErr: "pool recovery period",
		},
		{
			name: "above the maximum recovery period",
			policy: outside(func(p *types.ConversionPolicy) {
				p.PoolRecoveryPeriod = maximum.PoolRecoveryPeriod + 1
			}),
			expectErr: "pool recovery period",
		},
		{
			// The corridor is bounded raise-only here, so dropping the floor below
			// where it stands is the move the delegation is meant to refuse.
			name: "below the minimum stability spread",
			policy: outside(func(p *types.ConversionPolicy) {
				p.MinStabilitySpread = minimum.MinStabilitySpread.QuoInt64(2)
			}),
			expectErr: "min stability spread",
		},
		{
			name: "above the maximum stability spread",
			policy: outside(func(p *types.ConversionPolicy) {
				p.MinStabilitySpread = maximum.MinStabilitySpread.Add(math.LegacySmallestDec())
			}),
			expectErr: "min stability spread",
		},
		{
			// The corridor keeps the unit governance approved it in, which is
			// what strands a live mandate across a reference re-pointing.
			name: "candidate in another denomination",
			policy: outside(func(p *types.ConversionPolicy) {
				p.BasePool = sdk.NewDecCoinFromDec(chain.USDBaseDenom, inside.BasePool.Amount)
			}),
			expectErr: "base pool denomination ausd is outside the mandate",
		},
		{
			// ValidatePolicy is exported and the comparisons are a nil decimal
			// away from a panic, so an unvalidated candidate must be refused
			// rather than halt.
			name: "unset stability spread is refused, not panicked on",
			policy: outside(func(p *types.ConversionPolicy) {
				p.MinStabilitySpread = math.LegacyDec{}
			}),
			expectErr: "min stability spread must be set",
		},
		{
			name: "unset depth is refused, not panicked on",
			policy: outside(func(p *types.ConversionPolicy) {
				p.BasePool = sdk.DecCoin{Denom: minimum.BasePool.Denom}
			}),
			expectErr: "base pool amount must be set",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := appointment.ValidatePolicy(tc.policy)
			if tc.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.expectErr)
		})
	}
}
