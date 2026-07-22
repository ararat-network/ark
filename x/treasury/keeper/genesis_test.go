package keeper_test

import (
	"github.com/cosmos/gogoproto/proto"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	chain "ark/pkg/chain"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/types"
)

func (s *KeeperTestSuite) expectGenesisFundBalances(balances map[string]sdk.Coins) {
	for _, moduleName := range types.FundAccountNames() {
		account := authtypes.NewEmptyModuleAccount(moduleName)
		s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, moduleName).Return(account)
		s.bankKeeper.EXPECT().GetAllBalances(s.ctx, account.GetAddress()).Return(balances[moduleName])
	}
}

func (s *KeeperTestSuite) TestInitAndExportGenesis() {
	genesis := types.DefaultGenesisState()
	genesis.NextClaimId = 7
	genesis.Params.ReferenceTaxCap.Amount = math.ZeroInt()
	genesis.TaxCaps = []types.TaxCap{
		{Denom: chain.MicroSDRDenom, TaxCap: math.ZeroInt()},
	}
	s.oracleKeeper.EXPECT().GetTobinTaxes(s.ctx).Return([]oracletypes.TobinTax{
		{Denom: chain.MicroSDRDenom},
	}, nil)
	s.expectGenesisFundBalances(map[string]sdk.Coins{
		types.SubsidyPoolName:      sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 3)),
		types.RedemptionBufferName: sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 5)),
		types.StrategicReserveName: sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 7)),
		types.InsuranceName:        sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 11)),
	})

	s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))
	exported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(genesis.Params, exported.Params)
	s.Require().True(genesis.MonetaryPolicy.Equal(exported.MonetaryPolicy))
	s.Require().Equal(genesis.TaxCaps, exported.TaxCaps)
	s.Require().True(proto.Equal(&genesis.ClaimsMandate, &exported.ClaimsMandate))
	s.Require().Equal(genesis.ClaimsAllowanceUsed, exported.ClaimsAllowanceUsed)
	s.Require().Equal(genesis.InsuranceReserved, exported.InsuranceReserved)
	s.Require().Equal(genesis.NextClaimId, exported.NextClaimId)
	s.Require().Empty(exported.Claims)
	s.Require().Equal(genesis.RewardFunding, exported.RewardFunding)
	s.Require().Equal(genesis.MonetaryMandate, exported.MonetaryMandate)
}

func (s *KeeperTestSuite) TestInitGenesisBuildsUncappedSetWhenTaxIsDisabled() {
	genesis := types.DefaultGenesisState()
	s.oracleKeeper.EXPECT().GetTobinTaxes(s.ctx).Return([]oracletypes.TobinTax{
		{Denom: chain.MicroSDRDenom},
		{Denom: chain.MicroUSDDenom},
	}, nil)
	s.expectGenesisFundBalances(nil)

	s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))
	for _, denom := range []string{chain.MicroSDRDenom, chain.MicroUSDDenom} {
		cap, err := s.keeper.TaxCaps.Get(s.ctx, denom)
		s.Require().NoError(err)
		s.Require().True(cap.IsZero())
	}
}

func (s *KeeperTestSuite) TestInitGenesisBuildsPositiveCapsWhenTaxIsDisabled() {
	genesis := types.DefaultGenesisState()
	genesis.Params.ReferenceTaxCap.Amount = math.NewInt(100)
	s.oracleKeeper.EXPECT().GetTobinTaxes(s.ctx).Return([]oracletypes.TobinTax{
		{Denom: chain.MicroSDRDenom},
		{Denom: chain.MicroUSDDenom},
	}, nil)
	s.oracleKeeper.EXPECT().GetRateSnapshot(
		s.ctx,
		chain.MicroSDRDenom,
		chain.MicroUSDDenom,
	).Return(oracletypes.RateSnapshot{
		chain.MicroSDRDenom: math.LegacyOneDec(),
		chain.MicroUSDDenom: math.LegacyOneDec(),
	}, nil)
	s.expectGenesisFundBalances(nil)

	s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))
	for _, denom := range []string{chain.MicroSDRDenom, chain.MicroUSDDenom} {
		cap, err := s.keeper.TaxCaps.Get(s.ctx, denom)
		s.Require().NoError(err)
		s.Require().Equal(math.NewInt(100), cap)
	}
}

func (s *KeeperTestSuite) TestInitGenesisRejectsIncompleteCapsWhenTaxIsDisabled() {
	genesis := types.DefaultGenesisState()
	genesis.TaxCaps = []types.TaxCap{
		{Denom: chain.MicroSDRDenom, TaxCap: math.ZeroInt()},
	}
	s.oracleKeeper.EXPECT().GetTobinTaxes(s.ctx).Return([]oracletypes.TobinTax{
		{Denom: chain.MicroSDRDenom},
		{Denom: chain.MicroUSDDenom},
	}, nil)

	err := s.keeper.InitGenesis(s.ctx, genesis)
	s.Require().ErrorContains(err, "has 1 denoms; expected 2")
}

func (s *KeeperTestSuite) TestInitGenesisRejectsNonNoahFundBalance() {
	genesis := types.DefaultGenesisState()
	s.oracleKeeper.EXPECT().GetTobinTaxes(s.ctx).Return([]oracletypes.TobinTax{
		{Denom: chain.MicroSDRDenom},
	}, nil)
	s.accountKeeper.EXPECT().GetModuleAccount(s.ctx, types.SubsidyPoolName).
		Return(authtypes.NewEmptyModuleAccount(types.SubsidyPoolName))
	s.bankKeeper.EXPECT().GetAllBalances(s.ctx, gomock.Any()).Return(
		sdk.NewCoins(sdk.NewInt64Coin(chain.MicroUSDDenom, 1)),
	)

	err := s.keeper.InitGenesis(s.ctx, genesis)
	s.Require().ErrorContains(err, "unsupported genesis denom")
}

func (s *KeeperTestSuite) TestInitGenesisRejectsReservationAboveInsuranceBalance() {
	genesis := types.DefaultGenesisState()
	genesis.ClaimsMandate = types.ClaimsMandate{
		Term:                     1,
		Committee:                authtypes.NewModuleAddress("claims-committee").String(),
		ActivationHeight:         1,
		ExpiryHeight:             100,
		CancellationPeriodBlocks: 1,
		CommitteeClaimLimit:      math.NewInt(10),
	}
	genesis.ClaimsAllowanceUsed = math.NewInt(2)
	genesis.InsuranceReserved = math.NewInt(2)
	// The matching pending claim makes the pure genesis state internally
	// consistent; the keeper then rejects the insufficient Bank balance.
	genesis.Claims = []types.Claim{pendingClaimForGenesis(2)}
	genesis.NextClaimId = 2
	recipient, err := sdk.AccAddressFromBech32(genesis.Claims[0].Recipient)
	s.Require().NoError(err)
	s.bankKeeper.EXPECT().BlockedAddr(recipient).Return(false)
	s.oracleKeeper.EXPECT().GetTobinTaxes(s.ctx).Return([]oracletypes.TobinTax{
		{Denom: chain.MicroSDRDenom},
	}, nil)
	s.expectGenesisFundBalances(map[string]sdk.Coins{
		types.InsuranceName: sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 1)),
	})

	err = s.keeper.InitGenesis(s.ctx, genesis)
	s.Require().ErrorContains(err, "exceeds Insurance balance")
}

func (s *KeeperTestSuite) TestInitGenesisRejectsBlockedPendingClaimRecipient() {
	genesis := types.DefaultGenesisState()
	genesis.ClaimsMandate = types.ClaimsMandate{
		Term:                     1,
		Committee:                authtypes.NewModuleAddress("claims-committee").String(),
		ActivationHeight:         1,
		ExpiryHeight:             100,
		CancellationPeriodBlocks: 1,
		CommitteeClaimLimit:      math.NewInt(2),
	}
	genesis.ClaimsAllowanceUsed = math.NewInt(2)
	genesis.InsuranceReserved = math.NewInt(2)
	genesis.Claims = []types.Claim{pendingClaimForGenesis(2)}
	genesis.NextClaimId = 2
	recipient, err := sdk.AccAddressFromBech32(genesis.Claims[0].Recipient)
	s.Require().NoError(err)
	s.bankKeeper.EXPECT().BlockedAddr(recipient).Return(true)

	err = s.keeper.InitGenesis(s.ctx, genesis)
	s.Require().ErrorContains(err, "invalid pending claim")
	s.Require().ErrorContains(err, "blocked from receiving funds")
}

func (s *KeeperTestSuite) TestInitGenesisAllowsBlockedFinalizedClaimAuditRecord() {
	genesis := types.DefaultGenesisState()
	genesis.ClaimsMandate = types.ClaimsMandate{
		Term:                     1,
		Committee:                authtypes.NewModuleAddress("claims-committee").String(),
		ActivationHeight:         1,
		ExpiryHeight:             100,
		CancellationPeriodBlocks: 1,
		CommitteeClaimLimit:      math.NewInt(2),
	}
	claim := pendingClaimForGenesis(2)
	claim.Origin = types.ClaimOrigin_CLAIM_ORIGIN_GOVERNANCE
	claim.Status = types.ClaimStatus_CLAIM_STATUS_PAID
	claim.FinalizedHeight = claim.ExecutableHeight
	claim.FinalizedBy = authtypes.NewModuleAddress("claim-executor").String()
	genesis.Claims = []types.Claim{claim}
	genesis.NextClaimId = 2
	recipient, err := sdk.AccAddressFromBech32(claim.Recipient)
	s.Require().NoError(err)
	s.bankKeeper.EXPECT().BlockedAddr(recipient).Return(true).Times(0)
	s.oracleKeeper.EXPECT().GetTobinTaxes(s.ctx).Return([]oracletypes.TobinTax{
		{Denom: chain.MicroSDRDenom},
	}, nil)
	s.expectGenesisFundBalances(map[string]sdk.Coins{
		types.InsuranceName: sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 2)),
	})

	s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))
}

func pendingClaimForGenesis(amount int64) types.Claim {
	return types.Claim{
		ClaimId:           1,
		Submitter:         authtypes.NewModuleAddress("claims-committee").String(),
		Origin:            types.ClaimOrigin_CLAIM_ORIGIN_COMMITTEE,
		MandateTerm:       1,
		IncidentReference: "incident",
		Recipient:         authtypes.NewModuleAddress("claim-recipient").String(),
		Amount:            sdk.NewInt64Coin(chain.MicroNoahDenom, amount),
		EvidenceReference: "evidence",
		Status:            types.ClaimStatus_CLAIM_STATUS_PENDING,
		SubmittedHeight:   1,
		ExecutableHeight:  2,
	}
}
