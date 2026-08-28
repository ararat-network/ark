package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/claims/types"
)

// expectInsuranceBalances stubs the account and multi-denom balance reads
// InitGenesis makes, which the suite's single-denom GetBalance stub does not
// cover.
func (s *KeeperTestSuite) expectInsuranceBalances(balances sdk.Coins) {
	insuranceAddr := authtypes.NewModuleAddress(types.InsuranceName)
	moduleAccount := authtypes.NewEmptyModuleAccount(types.InsuranceName)
	s.accountKeeper.EXPECT().
		GetModuleAccount(gomock.Any(), types.InsuranceName).
		Return(moduleAccount).
		AnyTimes()
	s.bankKeeper.EXPECT().
		GetAllBalances(gomock.Any(), insuranceAddr).
		Return(balances).
		AnyTimes()
}

func (s *KeeperTestSuite) TestInitGenesisRoundTrip() {
	s.SetupTest()
	s.expectInsuranceBalances(sdk.NewCoins(noahCoin(5_000)))

	claim := types.Claim{
		ClaimId:         1,
		Submitter:       testAddress(1),
		Origin:          types.ClaimAuthority_CLAIM_AUTHORITY_COMMITTEE,
		MandateTerm:     1,
		Reference:       "incident",
		Recipient:       testAddress(3),
		Amount:          noahCoin(100),
		Status:          types.ClaimStatus_CLAIM_STATUS_PENDING,
		SubmittedHeight: 20,
		ClosingHeight:   25,
	}
	claimsMandate := s.appointCommittee(testAddress(1), 1, 10, 100, 1_000)

	genesis := types.NewGenesisState(
		types.Params{ClaimCancellationPeriodBlocks: 5},
		claimsMandate,
		math.NewInt(100),
		math.NewInt(100),
		2,
		[]types.Claim{claim},
	)
	s.Require().NoError(s.keeper.InitGenesis(s.ctx, genesis))

	exported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(genesis, exported)
}

func (s *KeeperTestSuite) TestInitGenesisDefault() {
	s.SetupTest()
	s.expectInsuranceBalances(sdk.NewCoins())

	expected := types.DefaultGenesisState()
	s.Require().NoError(s.keeper.InitGenesis(s.ctx, expected))

	exported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)

	// Claims is compared separately: DefaultGenesisState leaves it nil while
	// ExportGenesis builds an empty slice, and both mean "no claims".
	s.Require().Empty(exported.Claims)
	exported.Claims = expected.Claims
	s.Require().Equal(expected, exported)
}

func (s *KeeperTestSuite) TestInitGenesisRejections() {
	pendingClaim := func() types.Claim {
		return types.Claim{
			ClaimId:         1,
			Submitter:       testAddress(1),
			Origin:          types.ClaimAuthority_CLAIM_AUTHORITY_GOVERNANCE,
			Reference:       "incident",
			Recipient:       testAddress(3),
			Amount:          noahCoin(100),
			Status:          types.ClaimStatus_CLAIM_STATUS_PENDING,
			SubmittedHeight: 20,
			ClosingHeight:   25,
		}
	}
	valid := func() *types.GenesisState {
		return types.NewGenesisState(
			types.Params{ClaimCancellationPeriodBlocks: 5},
			types.DefaultClaimsMandate(),
			math.ZeroInt(),
			math.NewInt(100),
			2,
			[]types.Claim{pendingClaim()},
		)
	}

	tests := []struct {
		name      string
		balances  sdk.Coins
		blocked   bool
		mutate    func(*types.GenesisState)
		expectErr string
	}{
		{
			name:     "covered reservation",
			balances: sdk.NewCoins(noahCoin(5_000)),
			mutate:   func(*types.GenesisState) {},
		},
		{
			name:     "exactly covered reservation",
			balances: sdk.NewCoins(noahCoin(100)),
			mutate:   func(*types.GenesisState) {},
		},
		{
			name:      "reservation exceeds Insurance balance",
			balances:  sdk.NewCoins(noahCoin(99)),
			mutate:    func(*types.GenesisState) {},
			expectErr: "exceeds Insurance balance",
		},
		{
			name:      "non-NOAH Insurance balance",
			balances:  sdk.NewCoins(noahCoin(5_000), sdk.NewInt64Coin("usdr", 1)),
			mutate:    func(*types.GenesisState) {},
			expectErr: "unsupported genesis denom",
		},
		{
			// A recipient blocked while its claim was pending is a state the
			// import accepts and settlement ends, not a reason to refuse the
			// file. Blocking comes from app wiring, so refusing here would make
			// the same genesis load under one binary and halt under another.
			name:     "blocked pending recipient is imported",
			balances: sdk.NewCoins(noahCoin(5_000)),
			blocked:  true,
			mutate:   func(*types.GenesisState) {},
		},
		{
			name:      "invalid genesis is rejected before Bank is read",
			balances:  sdk.NewCoins(noahCoin(5_000)),
			mutate:    func(genesis *types.GenesisState) { genesis.NextClaimId = 0 },
			expectErr: "next claim ID must be positive",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.expectInsuranceBalances(tc.balances)
			if tc.blocked {
				s.blockedAddrs[testAddress(3)] = struct{}{}
			}
			genesis := valid()
			tc.mutate(genesis)

			err := s.keeper.InitGenesis(s.ctx, genesis)
			if tc.expectErr == "" {
				s.Require().NoError(err)
			} else {
				s.Require().ErrorContains(err, tc.expectErr)
			}
		})
	}
}

func (s *KeeperTestSuite) TestInitGenesisNil() {
	s.SetupTest()
	s.Require().ErrorContains(s.keeper.InitGenesis(s.ctx, nil), "genesis state is nil")
}

func (s *KeeperTestSuite) TestRecognisedCapital() {
	tests := []struct {
		name     string
		balance  int64
		reserved int64
		expect   int64
	}{
		{name: "unencumbered balance", balance: 500, reserved: 0, expect: 500},
		{name: "reservation is excluded", balance: 500, reserved: 200, expect: 300},
		{name: "fully encumbered", balance: 500, reserved: 500, expect: 0},
		{name: "empty fund", balance: 0, reserved: 0, expect: 0},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.fundInsurance(tc.balance)
			s.Require().NoError(s.keeper.InsuranceReserved.Set(s.ctx, math.NewInt(tc.reserved)))

			recognised, err := s.keeper.RecognisedCapital(s.ctx)
			s.Require().NoError(err)
			// Compared by value: a computed zero and math.NewInt(0) differ in
			// their internal big.Int representation, so require.Equal on the
			// struct is not a value comparison.
			s.Require().True(
				math.NewInt(tc.expect).Equal(recognised),
				"expected %s, got %s", math.NewInt(tc.expect), recognised,
			)
		})
	}
}

// TestRecognisedCapitalRefusesAnOverReservation pins the refusal standing
// between corrupt reservation accounting and the expansion waterfall. The state
// has to be written directly because no path through the module can reach it,
// which is the point: were it ever reached, nothing downstream would notice a
// fund reporting negative capital.
func (s *KeeperTestSuite) TestRecognisedCapitalRefusesAnOverReservation() {
	s.SetupTest()
	s.fundInsurance(100)
	s.Require().NoError(s.keeper.InsuranceReserved.Set(s.ctx, math.NewInt(101)))

	_, err := s.keeper.RecognisedCapital(s.ctx)
	s.Require().ErrorContains(err, "exceeds the Insurance balance")
}

// TestRecognisedCapitalTracksClaimLifecycle proves the value Treasury's
// waterfall consumes moves with the claim lifecycle, not just with the balance.
func (s *KeeperTestSuite) TestRecognisedCapitalTracksClaimLifecycle() {
	s.SetupTest()
	s.setCancellationPeriod(5)
	s.setBlockHeight(20)
	s.fundInsurance(500)

	recognised, err := s.keeper.RecognisedCapital(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(500), recognised)

	submitted, err := s.msgServer.SubmitClaim(s.ctx, &types.MsgSubmitClaim{
		Authority: s.authority,
		Reference: "incident",
		Recipient: testAddress(3),
		Amount:    noahCoin(100),
	})
	s.Require().NoError(err)

	// Submission encumbers without moving coins.
	recognised, err = s.keeper.RecognisedCapital(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(400), recognised)

	s.setBlockHeight(25)
	s.bankKeeper.EXPECT().
		SendCoinsFromModuleToAccount(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, _ string, _ sdk.AccAddress, coins sdk.Coins) error {
			// Bank moves the coins out, so the balance falls by the paid amount.
			s.insuranceBalance = s.insuranceBalance.Sub(coins.AmountOf(chain.NoahBaseDenom))
			return nil
		})
	s.Require().NoError(s.keeper.EndBlocker(s.ctx))
	s.requireStatus(submitted.ClaimId, types.ClaimStatus_CLAIM_STATUS_PAID)

	// Payment releases the reservation and reduces the balance by the same
	// amount, so recognised capital is unchanged by settlement itself.
	recognised, err = s.keeper.RecognisedCapital(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(400), recognised)
}
