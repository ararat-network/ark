package keeper_test

import (
	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	chain "ark/pkg/chain"
	assettypes "ark/x/asset/types"
	"ark/x/market/types"
)

// seedTobinTaxOverride writes an override straight to the collection. Tests
// about conversion arrange rate policy this way so they do not depend on the
// governance entry point that owns validating it.
func (s *KeeperTestSuite) seedTobinTaxOverride(denom string, tobinTax math.LegacyDec) {
	s.Require().NoError(s.keeper.TobinTaxOverrides.Set(s.ctx, denom, tobinTax))
}

func (s *KeeperTestSuite) TestGetTobinTax() {
	// Every case uses its own denomination: overrides are suite state that
	// survives between subtests, and a shared denomination would let one case
	// decide the next one's answer.
	tests := []struct {
		name         string
		denom        string
		unregistered bool
		override     math.LegacyDec
		expected     math.LegacyDec
	}{
		{
			name:     "absent override falls back to the default",
			denom:    chain.USDBaseDenom,
			expected: types.DefaultTobinTax,
		},
		{
			name:     "override replaces the default",
			denom:    chain.KRWBaseDenom,
			override: math.LegacyNewDecWithPrec(1, 2),
			expected: math.LegacyNewDecWithPrec(1, 2),
		},
		{
			// A zero override is a policy statement — this market carries no
			// staleness risk worth charging for — so it cannot be conflated with
			// having no entry, which means the opposite.
			name:     "zero override is honoured rather than treated as absent",
			denom:    chain.CNYBaseDenom,
			override: math.LegacyZeroDec(),
			expected: math.LegacyZeroDec(),
		},
		{
			// Rates are registry-independent by design: eligibility is the
			// caller's question, so an unlisted denomination still has a rate.
			name:         "unregistered denomination still has a rate",
			denom:        chain.EURBaseDenom,
			unregistered: true,
			expected:     types.DefaultTobinTax,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			if tc.unregistered {
				s.assetStatuses[tc.denom] = assettypes.AssetStatus_ASSET_STATUS_UNSPECIFIED
			}
			if !tc.override.IsNil() {
				s.seedTobinTaxOverride(tc.denom, tc.override)
			}

			tobinTax, err := s.keeper.GetTobinTax(s.ctx, tc.denom)
			s.Require().NoError(err)
			s.Require().True(tc.expected.Equal(tobinTax), "expected %s, got %s", tc.expected, tobinTax)
		})
	}
}

func (s *KeeperTestSuite) TestGetTobinTaxRequiresParams() {
	s.Require().NoError(s.keeper.Params.Remove(s.ctx))

	_, err := s.keeper.GetTobinTax(s.ctx, chain.USDBaseDenom)
	s.Require().ErrorIs(err, collections.ErrNotFound)
	s.Require().ErrorContains(err, "getting params")
}

func (s *KeeperTestSuite) TestSetTobinTaxOverride() {
	tests := []struct {
		name        string
		denom       string
		statuses    map[string]assettypes.AssetStatus
		seed        math.LegacyDec
		tobinTax    math.LegacyDec
		expectErr   string
		expectErrIs error
	}{
		{
			name:     "records a rate for a live asset",
			denom:    chain.USDBaseDenom,
			tobinTax: math.LegacyNewDecWithPrec(1, 2),
		},
		{
			name:      "rejects a negative rate",
			denom:     chain.KRWBaseDenom,
			tobinTax:  math.LegacyNewDecWithPrec(-1, 4),
			expectErr: "tobin tax must be in [0, 1)",
		},
		{
			// The bound is exclusive: a rate of one consumes the whole output,
			// which is a refusal to convert dressed up as a fee.
			name:      "rejects a rate of one",
			denom:     chain.KRWBaseDenom,
			tobinTax:  math.LegacyOneDec(),
			expectErr: "tobin tax must be in [0, 1)",
		},
		{
			// A dangling override fails silently — the protection governance
			// wrote simply would not apply — so the registry check is what turns
			// a mistyped denomination into a failed proposal.
			name:        "rejects an unregistered denomination",
			denom:       chain.EURBaseDenom,
			statuses:    map[string]assettypes.AssetStatus{chain.EURBaseDenom: assettypes.AssetStatus_ASSET_STATUS_UNSPECIFIED},
			tobinTax:    math.LegacyNewDecWithPrec(1, 2),
			expectErrIs: assettypes.ErrAssetNotFound,
		},
		{
			// Status is not consulted, so one proposal can list an illiquid
			// asset and price its conversion together, rather than spending a
			// block live at a default rate chosen for liquid fiat.
			name:     "accepts a suspended listing",
			denom:    chain.CNYBaseDenom,
			statuses: map[string]assettypes.AssetStatus{chain.CNYBaseDenom: assettypes.AssetStatus_ASSET_STATUS_SUSPENDED},
			tobinTax: math.LegacyNewDecWithPrec(2, 2),
		},
		{
			name:     "replaces an existing entry",
			denom:    chain.GBPBaseDenom,
			seed:     math.LegacyNewDecWithPrec(1, 2),
			tobinTax: math.LegacyNewDecWithPrec(5, 3),
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.assetStatuses = tc.statuses
			if !tc.seed.IsNil() {
				s.seedTobinTaxOverride(tc.denom, tc.seed)
			}

			err := s.keeper.SetTobinTaxOverride(s.ctx, tc.denom, tc.tobinTax)
			if tc.expectErr != "" || tc.expectErrIs != nil {
				s.Require().Error(err)
				if tc.expectErr != "" {
					s.Require().ErrorContains(err, tc.expectErr)
				}
				if tc.expectErrIs != nil {
					s.Require().ErrorIs(err, tc.expectErrIs)
				}
				stored, storedErr := s.keeper.TobinTaxOverrides.Has(s.ctx, tc.denom)
				s.Require().NoError(storedErr)
				s.Require().False(stored)
				return
			}

			s.Require().NoError(err)
			stored, err := s.keeper.TobinTaxOverrides.Get(s.ctx, tc.denom)
			s.Require().NoError(err)
			s.Require().True(tc.tobinTax.Equal(stored), "expected %s, got %s", tc.tobinTax, stored)
		})
	}
}

// EventTobinTaxOverrideSet means a rate moved, not that a proposal ran.
// Governance and the committee both reach this through one keeper method, so
// they share the rule and the event.
func (s *KeeperTestSuite) TestSetTobinTaxOverrideAnnouncesOnlyRealChanges() {
	rate := math.LegacyNewDecWithPrec(1, 2)

	s.Require().NoError(s.keeper.SetTobinTaxOverride(s.ctx, chain.USDBaseDenom, rate))
	s.Require().Equal(1, s.countTypedEvents(&types.EventTobinTaxOverrideSet{}))
	s.requireTypedEvent(&types.EventTobinTaxOverrideSet{
		Denom:    chain.USDBaseDenom,
		TobinTax: rate,
	})

	// Restated identically: nothing moved, so nothing is announced.
	s.Require().NoError(s.keeper.SetTobinTaxOverride(s.ctx, chain.USDBaseDenom, rate))
	s.Require().Equal(1, s.countTypedEvents(&types.EventTobinTaxOverrideSet{}))

	// 0.0100 is the same rate as 0.01 held in a different big.Int. The guard
	// has to compare value rather than the pointer a LegacyDec carries, or
	// every restatement would read as a change.
	s.Require().NoError(s.keeper.SetTobinTaxOverride(
		s.ctx,
		chain.USDBaseDenom,
		math.LegacyNewDecWithPrec(100, 4),
	))
	s.Require().Equal(1, s.countTypedEvents(&types.EventTobinTaxOverrideSet{}))

	// A rate that actually moves is announced.
	raised := math.LegacyNewDecWithPrec(2, 2)
	s.Require().NoError(s.keeper.SetTobinTaxOverride(s.ctx, chain.USDBaseDenom, raised))
	s.Require().Equal(2, s.countTypedEvents(&types.EventTobinTaxOverrideSet{}))
	s.requireTypedEvent(&types.EventTobinTaxOverrideSet{
		Denom:    chain.USDBaseDenom,
		TobinTax: raised,
	})

	stored, err := s.keeper.TobinTaxOverrides.Get(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().True(raised.Equal(stored), "expected %s, got %s", raised, stored)
}

// The removal path needs no equivalent guard: an absent entry errors rather
// than announcing a deletion that did not happen.
func (s *KeeperTestSuite) TestRemoveTobinTaxOverrideAnnouncesTheDeletion() {
	s.Require().NoError(s.keeper.SetTobinTaxOverride(
		s.ctx,
		chain.KRWBaseDenom,
		math.LegacyNewDecWithPrec(1, 2),
	))

	s.Require().NoError(s.keeper.RemoveTobinTaxOverride(s.ctx, chain.KRWBaseDenom))
	s.Require().Equal(1, s.countTypedEvents(&types.EventTobinTaxOverrideRemoved{}))
	s.requireTypedEvent(&types.EventTobinTaxOverrideRemoved{Denom: chain.KRWBaseDenom})

	err := s.keeper.RemoveTobinTaxOverride(s.ctx, chain.KRWBaseDenom)
	s.Require().ErrorIs(err, types.ErrTobinOverrideMissing)
	s.Require().Equal(1, s.countTypedEvents(&types.EventTobinTaxOverrideRemoved{}))
}

func (s *KeeperTestSuite) TestRemoveTobinTaxOverride() {
	tests := []struct {
		name        string
		denom       string
		statuses    map[string]assettypes.AssetStatus
		seed        math.LegacyDec
		expectErrIs error
	}{
		{
			name:        "rejects a denomination with no entry",
			denom:       chain.USDBaseDenom,
			expectErrIs: types.ErrTobinOverrideMissing,
		},
		{
			name:  "removes a live asset's entry",
			denom: chain.KRWBaseDenom,
			seed:  math.LegacyNewDecWithPrec(1, 2),
		},
		{
			// Removal deliberately does not consult the registry: retirement
			// leaves overrides behind, and a cleanup hook would make asset
			// lifecycle write Market state. Cleanup must therefore stay possible
			// for a denomination no live asset is listed under.
			name:     "removes a retired asset's leftover entry",
			denom:    chain.MNTBaseDenom,
			statuses: map[string]assettypes.AssetStatus{chain.MNTBaseDenom: assettypes.AssetStatus_ASSET_STATUS_RETIRED},
			seed:     math.LegacyNewDecWithPrec(2, 2),
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.assetStatuses = tc.statuses
			if !tc.seed.IsNil() {
				s.seedTobinTaxOverride(tc.denom, tc.seed)
			}

			err := s.keeper.RemoveTobinTaxOverride(s.ctx, tc.denom)
			if tc.expectErrIs != nil {
				s.Require().ErrorIs(err, tc.expectErrIs)
				s.Require().ErrorContains(err, tc.denom)
				return
			}

			s.Require().NoError(err)
			found, err := s.keeper.TobinTaxOverrides.Has(s.ctx, tc.denom)
			s.Require().NoError(err)
			s.Require().False(found)
		})
	}
}

func (s *KeeperTestSuite) TestGetTobinTaxOverrides() {
	empty, err := s.keeper.GetTobinTaxOverrides(s.ctx)
	s.Require().NoError(err)
	s.Require().Empty(empty)
	s.Require().NotNil(empty)

	// Written out of order on purpose: genesis export and the query both need
	// the sparse map back in key order, not in the order governance wrote it.
	s.seedTobinTaxOverride(chain.USDBaseDenom, math.LegacyNewDecWithPrec(1, 2))
	s.seedTobinTaxOverride(chain.CNYBaseDenom, math.LegacyZeroDec())
	s.seedTobinTaxOverride(chain.MNTBaseDenom, math.LegacyNewDecWithPrec(2, 2))

	overrides, err := s.keeper.GetTobinTaxOverrides(s.ctx)
	s.Require().NoError(err)
	expected := []types.TobinTaxOverride{
		{Denom: chain.CNYBaseDenom, TobinTax: math.LegacyZeroDec()},
		{Denom: chain.MNTBaseDenom, TobinTax: math.LegacyNewDecWithPrec(2, 2)},
		{Denom: chain.USDBaseDenom, TobinTax: math.LegacyNewDecWithPrec(1, 2)},
	}
	s.Require().Len(overrides, len(expected))
	for i, want := range expected {
		s.Require().Equal(want.Denom, overrides[i].Denom)
		s.Require().True(want.TobinTax.Equal(overrides[i].TobinTax),
			"expected %s for %s, got %s", want.TobinTax, want.Denom, overrides[i].TobinTax)
	}
}
