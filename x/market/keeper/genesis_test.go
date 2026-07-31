package keeper_test

import (
	"errors"

	"cosmossdk.io/math"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"ark/pkg/chain"
	"ark/x/market/types"
)

func (s *KeeperTestSuite) TestInitExportGenesis() {
	genesis := types.DefaultGenesisState()

	s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(authtypes.NewEmptyModuleAccount(types.ModuleName))
	s.oracleKeeper.EXPECT().GetReferenceDenom(s.ctx).
		Return(chain.SDRBaseDenom, nil)
	err := s.keeper.InitGenesis(s.ctx, genesis)
	s.Require().NoError(err)

	// Verify params
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(genesis.Params, params)

	// Verify pool delta
	delta, err := s.keeper.ArkPoolDelta.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().True(delta.Equal(genesis.ArkPoolDelta))

	// Overrides are governance judgment that cannot be re-derived, so genesis is
	// the only thing carrying them across an export and import cycle.
	s.Require().NotEmpty(genesis.TobinTaxOverrides)
	for _, override := range genesis.TobinTaxOverrides {
		stored, getErr := s.keeper.TobinTaxOverrides.Get(s.ctx, override.Denom)
		s.Require().NoError(getErr)
		s.Require().True(override.TobinTax.Equal(stored),
			"expected %s for %s, got %s", override.TobinTax, override.Denom, stored)
	}

	// Export and verify round-trip
	exported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().True(genesis.ArkPoolDelta.Equal(exported.ArkPoolDelta))
	s.Require().Equal(genesis.Params, exported.Params)
	s.Require().Len(exported.TobinTaxOverrides, len(genesis.TobinTaxOverrides))
	for i, override := range genesis.TobinTaxOverrides {
		s.Require().Equal(override.Denom, exported.TobinTaxOverrides[i].Denom)
		s.Require().True(override.TobinTax.Equal(exported.TobinTaxOverrides[i].TobinTax))
	}
}

func (s *KeeperTestSuite) TestInitGenesis_MissingModuleAccount() {
	genesis := types.DefaultGenesisState()

	s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(nil)
	err := s.keeper.InitGenesis(s.ctx, genesis)
	s.Require().Error(err)
	s.Require().ErrorContains(err, "module account has not been set")
}

// TestInitGenesis_ReferenceMismatchPreservesState covers the reference agreement
// InitGenesis insists on. The virtual pool prices conversion in the protocol
// reference unit, so a pool denominated in anything else is not a launchable
// configuration — and x/asset imports first, which is what makes the reference
// readable here at all.
func (s *KeeperTestSuite) TestInitGenesis_ReferenceMismatchPreservesState() {
	referenceErr := errors.New("asset reference unavailable")
	tests := []struct {
		name          string
		basePoolDenom string
		reference     string
		referenceErr  error
		expectErr     string
		errorIs       error
	}{
		{
			name:          "base pool disagrees with the reference",
			basePoolDenom: chain.USDBaseDenom,
			reference:     chain.SDRBaseDenom,
			expectErr:     "base pool denom ausd must be the protocol reference asdr",
		},
		{
			// An empty reference means no launch has configured one, so there is
			// nothing for the pool to agree with rather than a disagreement.
			name:      "reference has never been configured",
			reference: "",
			expectErr: "requires a configured protocol reference",
		},
		{
			name:         "reference lookup fails",
			referenceErr: referenceErr,
			expectErr:    "getting protocol reference",
			errorIs:      referenceErr,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			genesis := types.DefaultGenesisState()
			if tc.basePoolDenom != "" {
				genesis.ConversionPolicy.BasePool.Denom = tc.basePoolDenom
			}
			genesis.ArkPoolDelta = math.LegacyOneDec()

			s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).
				Return(authtypes.NewEmptyModuleAccount(types.ModuleName))
			s.oracleKeeper.EXPECT().GetReferenceDenom(s.ctx).Return(tc.reference, tc.referenceErr)

			err := s.keeper.InitGenesis(s.ctx, genesis)
			s.Require().ErrorContains(err, tc.expectErr)
			if tc.errorIs != nil {
				s.Require().ErrorIs(err, tc.errorIs)
			}

			// Genesis writes nothing until the reference agrees, so the state the
			// suite seeded is still intact.
			params, getErr := s.keeper.Params.Get(s.ctx)
			s.Require().NoError(getErr)
			s.Require().Equal(types.DefaultParams(), params)
			delta, getErr := s.keeper.ArkPoolDelta.Get(s.ctx)
			s.Require().NoError(getErr)
			s.Require().True(delta.IsZero())
			overrides, getErr := s.keeper.GetTobinTaxOverrides(s.ctx)
			s.Require().NoError(getErr)
			s.Require().Empty(overrides)
		})
	}
}
