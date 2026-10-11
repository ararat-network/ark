package keeper_test

import (
	"errors"

	"cosmossdk.io/math"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/mandate"
	"github.com/ararat-network/ark/x/market/types"
)

func (s *KeeperTestSuite) TestInitExportGenesis() {
	minimum, maximum := capacityCorridor()
	genesis := &types.GenesisState{
		Params:       types.Params{DefaultTobinTax: math.LegacyNewDecWithPrec(5, 3)},
		ArkPoolDelta: math.LegacyNewDec(-7),
		TobinTaxOverrides: []types.TobinTaxOverride{
			{Denom: chain.KRWBaseDenom, TobinTax: math.LegacyNewDecWithPrec(2, 2)},
			{Denom: chain.MXNBaseDenom, TobinTax: math.LegacyNewDecWithPrec(1, 2)},
		},
		ConversionPolicy: candidateInsideCorridor(),
		ConversionMandate: types.ConversionMandate{
			Envelope: mandate.Envelope{
				Term:           3,
				Committee:      capacityCommittee(),
				ActivationTime: capacityActivation,
				ExpiryTime:     capacityExpiry,
				CommitteeShape: mandate.CommitteeShape{
					AccountType: "/cosmos.auth.v1beta1.BaseAccount",
					KeyKind:     mandate.CommitteeKeyKind_COMMITTEE_KEY_KIND_MULTISIG,
					Threshold:   2,
					MemberCount: 3,
				},
			},
			MinimumPolicy: minimum,
			MaximumPolicy: maximum,
			MaxTobinTax:   tobinCapForTest(),
		},
	}
	// Every field departs from the suite's seed, so one the import skipped cannot match by accident.
	seeded, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().NotEqual(seeded.Params, genesis.Params)
	s.Require().False(seeded.ArkPoolDelta.Equal(genesis.ArkPoolDelta))
	s.Require().NotEqual(seeded.TobinTaxOverrides, genesis.TobinTaxOverrides)
	s.Require().False(seeded.ConversionPolicy.Equal(genesis.ConversionPolicy))
	s.Require().NotEqual(seeded.ConversionMandate, genesis.ConversionMandate)

	s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(authtypes.NewEmptyModuleAccount(types.ModuleName))
	s.oracleKeeper.EXPECT().GetReferenceDenom(s.ctx).
		Return(chain.XDRBaseDenom, nil)
	s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))

	exported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(genesis, exported)
}

func (s *KeeperTestSuite) TestInitGenesis_MissingModuleAccount() {
	genesis := types.DefaultGenesisState()

	s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.ModuleName).Return(nil)
	err := s.keeper.InitGenesis(s.ctx, genesis)
	s.Require().Error(err)
	s.Require().ErrorContains(err, "module account has not been set")
}

// TestInitGenesis_RefusalPreservesState checks the refusals genesis makes against live state: the
// pool denomination must match the protocol reference imported by Oracle before Market
// initialisation, and the committee must not be the authority.
func (s *KeeperTestSuite) TestInitGenesis_RefusalPreservesState() {
	referenceErr := errors.New("asset reference unavailable")
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	tests := []struct {
		name          string
		basePoolDenom string
		reference     string
		referenceErr  error
		// committee, when set, appoints it over the standard corridor.
		committee string
		expectErr string
		errorIs   error
	}{
		{
			name:          "base pool disagrees with the reference",
			basePoolDenom: chain.USDBaseDenom,
			reference:     chain.XDRBaseDenom,
			expectErr:     "base pool denom ausd must be the protocol reference axdr",
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
		{
			// The message path refuses this appointment too; genesis is the other way in.
			name:      "committee is the authority",
			reference: chain.XDRBaseDenom,
			committee: authority,
			expectErr: "conversion committee must be distinct from the Market authority",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			genesis := types.DefaultGenesisState()
			if tc.basePoolDenom != "" {
				genesis.ConversionPolicy.BasePool.Denom = tc.basePoolDenom
			}
			if tc.committee != "" {
				minimum, maximum := capacityCorridor()
				genesis.ConversionMandate = types.ConversionMandate{
					Envelope: mandate.Envelope{
						Term:           1,
						Committee:      tc.committee,
						ActivationTime: capacityActivation,
						ExpiryTime:     capacityExpiry,
					},
					MinimumPolicy: minimum,
					MaximumPolicy: maximum,
					MaxTobinTax:   math.LegacyZeroDec(),
				}
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

			// Genesis writes nothing until every check passes, so the state the
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
			appointment, getErr := s.keeper.ConversionMandate.Get(s.ctx)
			s.Require().NoError(getErr)
			s.Require().Equal(types.DefaultConversionMandate(), appointment)
		})
	}
}
