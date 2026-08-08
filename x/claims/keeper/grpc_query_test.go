package keeper_test

import (
	"cosmossdk.io/math"

	sdkquery "github.com/cosmos/cosmos-sdk/types/query"

	"ark/x/claims/types"
)

func (s *KeeperTestSuite) TestQueryParams() {
	s.SetupTest()
	s.setCancellationPeriod(42)

	resp, err := s.queryClient.Params(s.ctx, &types.QueryParamsRequest{})
	s.Require().NoError(err)
	s.Require().Equal(uint64(42), resp.Params.ClaimCancellationPeriodBlocks)
}

func (s *KeeperTestSuite) TestQueryClaimsMandate() {
	committee := testAddress(1)

	s.Run("disabled mandate reports inactive", func() {
		s.SetupTest()
		s.setBlockHeight(20)

		resp, err := s.queryServer.ClaimsMandate(s.ctx, &types.QueryClaimsMandateRequest{})
		s.Require().NoError(err)
		s.Require().False(resp.Active)
		s.Require().Equal(noahCoin(0), resp.AllowanceUsed)
		s.Require().Equal(noahCoin(0), resp.AllowanceRemaining)
	})

	s.Run("active mandate reports remaining allowance", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1, 10, 100, 1_000)
		s.Require().NoError(s.keeper.ClaimsAllowanceUsed.Set(s.ctx, math.NewInt(400)))
		s.setBlockHeight(20)

		resp, err := s.queryServer.ClaimsMandate(s.ctx, &types.QueryClaimsMandateRequest{})
		s.Require().NoError(err)
		s.Require().True(resp.Active)
		s.Require().Equal(committee, resp.Mandate.Committee)
		s.Require().Equal(noahCoin(400), resp.AllowanceUsed)
		s.Require().Equal(noahCoin(600), resp.AllowanceRemaining)
	})

	s.Run("activity is derived from the current height", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1, 10, 100, 1_000)

		// Half-open window: inactive one block before activation and again at
		// expiry itself.
		s.setBlockHeight(9)
		resp, err := s.queryServer.ClaimsMandate(s.ctx, &types.QueryClaimsMandateRequest{})
		s.Require().NoError(err)
		s.Require().False(resp.Active)

		s.setBlockHeight(10)
		resp, err = s.queryServer.ClaimsMandate(s.ctx, &types.QueryClaimsMandateRequest{})
		s.Require().NoError(err)
		s.Require().True(resp.Active)

		s.setBlockHeight(99)
		resp, err = s.queryServer.ClaimsMandate(s.ctx, &types.QueryClaimsMandateRequest{})
		s.Require().NoError(err)
		s.Require().True(resp.Active)

		s.setBlockHeight(100)
		resp, err = s.queryServer.ClaimsMandate(s.ctx, &types.QueryClaimsMandateRequest{})
		s.Require().NoError(err)
		s.Require().False(resp.Active)
	})
}

func (s *KeeperTestSuite) TestQueryBalance() {
	s.Run("empty fund", func() {
		s.SetupTest()

		resp, err := s.queryServer.Balance(s.ctx, &types.QueryBalanceRequest{})
		s.Require().NoError(err)
		s.Require().Equal(noahCoin(0), resp.Balance)
		s.Require().Equal(noahCoin(0), resp.Reserved)
	})

	s.Run("reports custody and encumbrance", func() {
		s.SetupTest()
		s.fundInsurance(5_000)
		s.Require().NoError(s.keeper.InsuranceReserved.Set(s.ctx, math.NewInt(250)))

		resp, err := s.queryServer.Balance(s.ctx, &types.QueryBalanceRequest{})
		s.Require().NoError(err)
		s.Require().Equal(noahCoin(5_000), resp.Balance)
		s.Require().Equal(noahCoin(250), resp.Reserved)

		// The difference is what Treasury sizes against, so the two must agree
		// with the operator interface rather than merely resemble it.
		recognised, err := s.keeper.RecognisedCapital(s.ctx)
		s.Require().NoError(err)
		s.Require().Equal(resp.Balance.Amount.Sub(resp.Reserved.Amount), recognised)
	})

	// A replacement resets the committee allowance; the encumbrance it does not
	// touch is exactly why this figure answers here and not on the mandate.
	s.Run("survives mandate replacement", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		s.appointCommittee(testAddress(1), 1, 10, 100, 1_000)
		s.setBlockHeight(20)
		s.fundInsurance(5_000)

		_, err := s.msgServer.CommitteeSubmitClaim(s.ctx, &types.MsgCommitteeSubmitClaim{
			Committee:    testAddress(1),
			ExpectedTerm: 1,
			Reference:    "incident",
			Recipient:    testAddress(3),
			Amount:       noahCoin(100),
		})
		s.Require().NoError(err)

		_, err = s.msgServer.SetClaimsMandate(s.ctx, &types.MsgSetClaimsMandate{
			Authority:           s.authority,
			Committee:           testAddress(2),
			ActivationHeight:    10,
			ExpiryHeight:        100,
			CommitteeClaimLimit: noahCoin(1_000),
		})
		s.Require().NoError(err)

		mandate, err := s.queryServer.ClaimsMandate(s.ctx, &types.QueryClaimsMandateRequest{})
		s.Require().NoError(err)
		s.Require().Equal(noahCoin(0), mandate.AllowanceUsed)

		resp, err := s.queryServer.Balance(s.ctx, &types.QueryBalanceRequest{})
		s.Require().NoError(err)
		s.Require().Equal(noahCoin(100), resp.Reserved)
	})
}

func (s *KeeperTestSuite) TestQueryClaim() {
	s.Run("returns a stored claim", func() {
		s.SetupTest()
		s.setCancellationPeriod(5)
		s.setBlockHeight(20)
		s.fundInsurance(5_000)

		submitted, err := s.msgServer.SubmitClaim(s.ctx, &types.MsgSubmitClaim{
			Authority: s.authority,
			Reference: "incident",
			Recipient: testAddress(3),
			Amount:    noahCoin(100),
		})
		s.Require().NoError(err)

		resp, err := s.queryClient.Claim(s.ctx, &types.QueryClaimRequest{ClaimId: submitted.ClaimId})
		s.Require().NoError(err)
		s.Require().Equal(submitted.ClaimId, resp.Claim.ClaimId)
		s.Require().Equal(types.ClaimStatus_CLAIM_STATUS_PENDING, resp.Claim.Status)
	})

	s.Run("zero claim ID is rejected", func() {
		s.SetupTest()
		_, err := s.queryClient.Claim(s.ctx, &types.QueryClaimRequest{ClaimId: 0})
		s.Require().ErrorContains(err, "claim ID must be positive")
	})

	s.Run("unknown claim ID is not found", func() {
		s.SetupTest()
		_, err := s.queryClient.Claim(s.ctx, &types.QueryClaimRequest{ClaimId: 99})
		s.Require().ErrorContains(err, "not found")
	})
}

func (s *KeeperTestSuite) TestQueryClaims() {
	seed := func(count int) {
		s.setCancellationPeriod(5)
		s.setBlockHeight(20)
		s.fundInsurance(50_000)
		for range count {
			_, err := s.msgServer.SubmitClaim(s.ctx, &types.MsgSubmitClaim{
				Authority: s.authority,
				Reference: "incident",
				Recipient: testAddress(3),
				Amount:    noahCoin(10),
			})
			s.Require().NoError(err)
		}
	}

	s.Run("lists every claim in ID order", func() {
		s.SetupTest()
		seed(3)

		resp, err := s.queryClient.Claims(s.ctx, &types.QueryClaimsRequest{})
		s.Require().NoError(err)
		s.Require().Len(resp.Claims, 3)
		for i, claim := range resp.Claims {
			s.Require().Equal(uint64(i+1), claim.ClaimId)
		}
	})

	s.Run("paginates", func() {
		s.SetupTest()
		seed(3)

		resp, err := s.queryClient.Claims(s.ctx, &types.QueryClaimsRequest{
			Pagination: &sdkquery.PageRequest{Limit: 2},
		})
		s.Require().NoError(err)
		s.Require().Len(resp.Claims, 2)
		s.Require().NotNil(resp.Pagination.NextKey)
	})

	s.Run("key and offset together are rejected", func() {
		s.SetupTest()
		seed(1)

		_, err := s.queryClient.Claims(s.ctx, &types.QueryClaimsRequest{
			Pagination: &sdkquery.PageRequest{Offset: 1, Key: []byte{0x01}},
		})
		s.Require().ErrorContains(err, "must not specify both key and offset")
	})

	s.Run("empty store lists nothing", func() {
		s.SetupTest()

		resp, err := s.queryClient.Claims(s.ctx, &types.QueryClaimsRequest{})
		s.Require().NoError(err)
		s.Require().Empty(resp.Claims)
	})
}
