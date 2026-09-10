package keeper_test

import (
	"errors"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/ararat-network/ark/pkg/chain"
	claimstypes "github.com/ararat-network/ark/x/claims/types"
	"github.com/ararat-network/ark/x/reserve/types"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

func noahCoin(amount int64) sdk.Coin {
	return sdk.NewInt64Coin(chain.NoahBaseDenom, amount)
}

func (s *KeeperTestSuite) TestFundBufferRejectsInvalidAuthority() {
	s.SetupTest()

	_, err := s.msgServer.FundBuffer(s.ctx, &types.MsgFundBuffer{
		Authority:             "not-authority",
		Amount:                noahCoin(1),
		MinimumReserveBalance: noahCoin(0),
	})
	s.Require().ErrorIs(err, errortypes.ErrUnauthorized)
}

func (s *KeeperTestSuite) TestFundBufferHonoursFloor() {
	s.Run("floor blocks the transfer", func() {
		s.SetupTest()
		s.fundReserve(10)

		_, err := s.msgServer.FundBuffer(s.ctx, &types.MsgFundBuffer{
			Authority:             s.authority,
			Amount:                noahCoin(7),
			MinimumReserveBalance: noahCoin(4),
		})
		s.Require().ErrorContains(err, "cannot fund")
	})

	s.Run("landing exactly on the floor is permitted", func() {
		s.SetupTest()
		s.fundReserve(10)
		s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
			gomock.Any(),
			types.StrategicReserveName,
			treasurytypes.RedemptionBufferName,
			sdk.NewCoins(noahCoin(6)),
		).Return(nil)

		_, err := s.msgServer.FundBuffer(s.ctx, &types.MsgFundBuffer{
			Authority:             s.authority,
			Amount:                noahCoin(6),
			MinimumReserveBalance: noahCoin(4),
		})
		s.Require().NoError(err)
	})

	s.Run("a zero floor permits a full transfer", func() {
		s.SetupTest()
		s.fundReserve(10)
		s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
			gomock.Any(),
			types.StrategicReserveName,
			treasurytypes.RedemptionBufferName,
			sdk.NewCoins(noahCoin(10)),
		).Return(nil)

		_, err := s.msgServer.FundBuffer(s.ctx, &types.MsgFundBuffer{
			Authority:             s.authority,
			Amount:                noahCoin(10),
			MinimumReserveBalance: noahCoin(0),
		})
		s.Require().NoError(err)
	})

	s.Run("an amount above the balance cannot fund", func() {
		s.SetupTest()
		s.fundReserve(10)

		_, err := s.msgServer.FundBuffer(s.ctx, &types.MsgFundBuffer{
			Authority:             s.authority,
			Amount:                noahCoin(11),
			MinimumReserveBalance: noahCoin(0),
		})
		s.Require().ErrorContains(err, "cannot fund")
	})
}

func (s *KeeperTestSuite) TestFundBufferRejectsInvalidCoins() {
	tests := []struct {
		name    string
		amount  sdk.Coin
		minimum sdk.Coin
	}{
		{name: "zero amount", amount: noahCoin(0), minimum: noahCoin(0)},
		{
			name:    "negative amount",
			amount:  sdk.Coin{Denom: chain.NoahBaseDenom, Amount: math.NewInt(-1)},
			minimum: noahCoin(0),
		},
		{name: "wrong amount denom", amount: sdk.NewInt64Coin("axdr", 1), minimum: noahCoin(0)},
		{
			name:    "malformed amount denom",
			amount:  sdk.Coin{Denom: "!", Amount: math.OneInt()},
			minimum: noahCoin(0),
		},
		{
			name:    "negative minimum",
			amount:  noahCoin(1),
			minimum: sdk.Coin{Denom: chain.NoahBaseDenom, Amount: math.NewInt(-1)},
		},
		{name: "wrong minimum denom", amount: noahCoin(1), minimum: sdk.NewInt64Coin("axdr", 1)},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.fundReserve(1_000)

			_, err := s.msgServer.FundBuffer(s.ctx, &types.MsgFundBuffer{
				Authority:             s.authority,
				Amount:                tc.amount,
				MinimumReserveBalance: tc.minimum,
			})
			s.Require().Error(err)
		})
	}
}

func (s *KeeperTestSuite) TestFundBufferMovesFunds() {
	s.SetupTest()
	s.fundReserve(100)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(),
		types.StrategicReserveName,
		treasurytypes.RedemptionBufferName,
		sdk.NewCoins(noahCoin(40)),
	).Return(nil)

	_, err := s.msgServer.FundBuffer(s.ctx, &types.MsgFundBuffer{
		Authority:             s.authority,
		Amount:                noahCoin(40),
		MinimumReserveBalance: noahCoin(10),
	})
	s.Require().NoError(err)

	// No custom event: the signed message and the Bank event are the audit
	// trail.
	s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
}

func (s *KeeperTestSuite) TestFundBufferPropagatesBankFailure() {
	s.SetupTest()
	s.fundReserve(100)
	s.bankKeeper.EXPECT().
		SendCoinsFromModuleToModule(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(errors.New("bank rejected"))

	_, err := s.msgServer.FundBuffer(s.ctx, &types.MsgFundBuffer{
		Authority:             s.authority,
		Amount:                noahCoin(40),
		MinimumReserveBalance: noahCoin(10),
	})
	s.Require().ErrorContains(err, "bank rejected")
	s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
}

// TestFundInsuranceMovesFunds pins the one property that distinguishes
// the twin transfers: where the coins land. The destination is fixed in
// keeper code and unreachable from the request, so this is what a copy-paste
// between the two handlers would break and nothing else would catch.
func (s *KeeperTestSuite) TestFundInsuranceMovesFunds() {
	s.SetupTest()
	s.fundReserve(100)
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(),
		types.StrategicReserveName,
		claimstypes.InsuranceName,
		sdk.NewCoins(noahCoin(40)),
	).Return(nil)

	_, err := s.msgServer.FundInsurance(s.ctx, &types.MsgFundInsurance{
		Authority:             s.authority,
		Amount:                noahCoin(40),
		MinimumReserveBalance: noahCoin(10),
	})
	s.Require().NoError(err)

	// No custom event, exactly as the Buffer transfer.
	s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
}

func (s *KeeperTestSuite) TestFundInsuranceRejectsInvalidAuthority() {
	s.SetupTest()

	_, err := s.msgServer.FundInsurance(s.ctx, &types.MsgFundInsurance{
		Authority:             "not-authority",
		Amount:                noahCoin(1),
		MinimumReserveBalance: noahCoin(0),
	})
	s.Require().ErrorIs(err, errortypes.ErrUnauthorized)
}

// TestFundInsuranceHonoursGuard re-checks the shared guard on this path
// rather than trusting the Buffer's coverage, so a future change that stops
// routing both through transferToFund cannot quietly leave this one unguarded.
func (s *KeeperTestSuite) TestFundInsuranceHonoursGuard() {
	s.SetupTest()
	s.fundReserve(10)

	// No Bank expectation: the guard is checked before any coin moves.
	_, err := s.msgServer.FundInsurance(s.ctx, &types.MsgFundInsurance{
		Authority:             s.authority,
		Amount:                noahCoin(7),
		MinimumReserveBalance: noahCoin(5),
	})
	s.Require().ErrorContains(err, "cannot fund")

	// The exact remainder still fits.
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(),
		types.StrategicReserveName,
		claimstypes.InsuranceName,
		sdk.NewCoins(noahCoin(5)),
	).Return(nil)
	_, err = s.msgServer.FundInsurance(s.ctx, &types.MsgFundInsurance{
		Authority:             s.authority,
		Amount:                noahCoin(5),
		MinimumReserveBalance: noahCoin(5),
	})
	s.Require().NoError(err)
}

// TestCommitteeFundBuffer pins the bound that makes a committee transfer
// safe: the keeper derives it from the destination's shortfall, so the
// committee chooses timing and not size.
func (s *KeeperTestSuite) TestCommitteeFundBuffer() {
	committee := testAddress(1)
	destination := testAddress(2)

	// activeCommittee appoints a committee and states what the Buffer is short.
	activeCommittee := func(balance, floor, bufferGap int64) {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, floor, destination)
		s.setBlockHeight(20)
		s.fundReserve(balance)
		s.treasuryReader.bufferShortfall = math.NewInt(bufferGap)
	}

	// expectBufferSend stubs the module-to-module transfer.
	expectBufferSend := func(amount int64) {
		s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
			gomock.Any(),
			types.StrategicReserveName,
			treasurytypes.RedemptionBufferName,
			sdk.NewCoins(noahCoin(amount)),
		).Return(nil)
	}

	s.Run("fills up to the shortfall and reports the rest", func() {
		activeCommittee(1_000, 0, 400)
		expectBufferSend(250)

		resp, err := s.msgServer.CommitteeFundBuffer(s.ctx, &types.MsgCommitteeFundBuffer{
			Committee: committee, ExpectedTerm: 1, Amount: noahCoin(250),
		})
		s.Require().NoError(err)
		s.Require().Equal(noahCoin(150), resp.RemainingShortfall)
	})

	s.Run("fills the gap exactly", func() {
		activeCommittee(1_000, 0, 400)
		expectBufferSend(400)

		resp, err := s.msgServer.CommitteeFundBuffer(s.ctx, &types.MsgCommitteeFundBuffer{
			Committee: committee, ExpectedTerm: 1, Amount: noahCoin(400),
		})
		s.Require().NoError(err)
		s.Require().True(resp.RemainingShortfall.Amount.IsZero())
	})

	// The power exhausts itself at the target line: a committee cannot overfund
	// a fund however many times it tries.
	s.Run("refuses to fill past the target", func() {
		activeCommittee(1_000, 0, 400)

		_, err := s.msgServer.CommitteeFundBuffer(s.ctx, &types.MsgCommitteeFundBuffer{
			Committee: committee, ExpectedTerm: 1, Amount: noahCoin(401),
		})
		s.Require().ErrorContains(err, "exceeds the")
	})

	s.Run("refuses when the fund is at or above its target", func() {
		activeCommittee(1_000, 0, 0)

		_, err := s.msgServer.CommitteeFundBuffer(s.ctx, &types.MsgCommitteeFundBuffer{
			Committee: committee, ExpectedTerm: 1, Amount: noahCoin(1),
		})
		s.Require().ErrorContains(err, "shortfall 0anoah")
	})

	// The mandate floor binds separately from the shortfall: it is the
	// appointment's own operating rule about how far the fund may be drawn down.
	s.Run("the mandate floor bounds the transfer", func() {
		activeCommittee(500, 400, 1_000)
		expectBufferSend(100)

		_, err := s.msgServer.CommitteeFundBuffer(s.ctx, &types.MsgCommitteeFundBuffer{
			Committee: committee, ExpectedTerm: 1, Amount: noahCoin(100),
		})
		s.Require().NoError(err)

		_, err = s.msgServer.CommitteeFundBuffer(s.ctx, &types.MsgCommitteeFundBuffer{
			Committee: committee, ExpectedTerm: 1, Amount: noahCoin(101),
		})
		s.Require().ErrorContains(err, "cannot fund")
	})

	// Incomplete valuation still permits Buffer refill against the shortfall Treasury can value.
	// Only the Reserve disposal bound is unavailable.
	s.Run("fills while valuation is incomplete", func() {
		activeCommittee(1_000, 0, 400)
		s.treasuryReader.requiredErr = errors.New("aggregate liability valuation is incomplete")
		expectBufferSend(250)

		resp, err := s.msgServer.CommitteeFundBuffer(s.ctx, &types.MsgCommitteeFundBuffer{
			Committee: committee, ExpectedTerm: 1, Amount: noahCoin(250),
		})
		s.Require().NoError(err)
		s.Require().Equal(noahCoin(150), resp.RemainingShortfall)
	})

	// A valuation that faults outright is still a refusal: an unreadable
	// registry is not a qualified figure, and no bound may be sized on it.
	s.Run("refuses when the valuation faults", func() {
		activeCommittee(1_000, 0, 400)
		s.treasuryReader.err = errors.New("pricing aggregate liability: store fault")

		_, err := s.msgServer.CommitteeFundBuffer(s.ctx, &types.MsgCommitteeFundBuffer{
			Committee: committee, ExpectedTerm: 1, Amount: noahCoin(1),
		})
		s.Require().ErrorContains(err, "store fault")

		// The governance twin reads nothing but the balance, so it stays
		// available even here — the two are complements, not duplicates.
		s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
			gomock.Any(),
			types.StrategicReserveName,
			treasurytypes.RedemptionBufferName,
			sdk.NewCoins(noahCoin(1)),
		).Return(nil)
		_, err = s.msgServer.FundBuffer(s.ctx, &types.MsgFundBuffer{
			Authority:             s.authority,
			Amount:                noahCoin(1),
			MinimumReserveBalance: noahCoin(0),
		})
		s.Require().NoError(err)
	})

	s.Run("refuses when the requirement reader is unwired", func() {
		activeCommittee(1_000, 0, 400)
		s.keeper.SetTreasuryCapitalReader(nil)

		_, err := s.msgServer.CommitteeFundBuffer(s.ctx, &types.MsgCommitteeFundBuffer{
			Committee: committee, ExpectedTerm: 1, Amount: noahCoin(1),
		})
		s.Require().ErrorContains(err, "not wired")
	})

	// A transfer moves capital between protocol funds rather than converting it
	// into external exposure, so it neither consumes allowance nor records an entry.
	s.Run("consumes no allowance and opens no position", func() {
		activeCommittee(1_000, 0, 400)
		expectBufferSend(400)

		_, err := s.msgServer.CommitteeFundBuffer(s.ctx, &types.MsgCommitteeFundBuffer{
			Committee: committee, ExpectedTerm: 1, Amount: noahCoin(400),
		})
		s.Require().NoError(err)

		used, err := s.keeper.AllowanceUsed.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().True(used.IsZero())

		// No custom event and no ledger entry: the signed message and Bank's
		// canonical event are the audit trail, exactly as for the governance
		// twin.
		s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
		s.Require().Empty(s.wholeLedger())
	})

	s.Run("rejects invalid requests", func() {
		tests := []struct {
			name      string
			committee string
			term      uint64
			amount    sdk.Coin
			want      string
		}{
			{
				name: "zero amount", committee: committee, term: 1,
				amount: noahCoin(0), want: "transfer amount must be positive",
			},
			{
				name: "non-noah amount", committee: committee, term: 1,
				amount: assetCoin(1), want: "transfer amount must be denominated in anoah",
			},
			{
				name: "unauthorised signer", committee: testAddress(9), term: 1,
				amount: noahCoin(1), want: types.ReserveMandateLabel,
			},
			{
				name: "stale term", committee: committee, term: 2,
				amount: noahCoin(1), want: "term",
			},
		}

		for _, tc := range tests {
			s.Run(tc.name, func() {
				activeCommittee(1_000, 0, 400)
				_, err := s.msgServer.CommitteeFundBuffer(s.ctx, &types.MsgCommitteeFundBuffer{
					Committee: tc.committee, ExpectedTerm: tc.term, Amount: tc.amount,
				})
				s.Require().ErrorContains(err, tc.want)
			})
		}
	})
}

// TestCommitteeFundInsurance pins what distinguishes the twin: where the
// coins land, and which shortfall bounds them. Both are fixed in keeper code
// and unreachable from the request, so this is what a copy-paste between the
// two handlers would break and nothing else would catch.
func (s *KeeperTestSuite) TestCommitteeFundInsurance() {
	committee := testAddress(1)
	destination := testAddress(2)

	s.SetupTest()
	s.appointCommittee(committee, 1_000, 0, destination)
	s.setBlockHeight(20)
	s.fundReserve(1_000)
	s.treasuryReader.insuranceShortfall = math.NewInt(300)
	// The Buffer is full. A handler reading the wrong gap would refuse here.
	s.treasuryReader.bufferShortfall = math.ZeroInt()

	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(),
		types.StrategicReserveName,
		claimstypes.InsuranceName,
		sdk.NewCoins(noahCoin(300)),
	).Return(nil)

	resp, err := s.msgServer.CommitteeFundInsurance(s.ctx, &types.MsgCommitteeFundInsurance{
		Committee: committee, ExpectedTerm: 1, Amount: noahCoin(300),
	})
	s.Require().NoError(err)
	s.Require().True(resp.RemainingShortfall.Amount.IsZero())
	s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())

	// Treasury now sees a filled fund, which is what the transfer just caused:
	// the stub states the recomputed figure the real reader would return.
	s.treasuryReader.insuranceShortfall = math.ZeroInt()
	_, err = s.msgServer.CommitteeFundInsurance(s.ctx, &types.MsgCommitteeFundInsurance{
		Committee: committee, ExpectedTerm: 1, Amount: noahCoin(1),
	})
	s.Require().ErrorContains(err, "shortfall 0anoah")
}

// TestCommitteeTransferHonoursGuardOnBothPaths re-checks the shared bound on
// the Insurance path rather than trusting the Buffer's coverage, so a future
// change that stops routing both through committeeCommitToFund cannot quietly
// leave this one unguarded.
func (s *KeeperTestSuite) TestCommitteeTransferHonoursGuardOnBothPaths() {
	committee := testAddress(1)
	destination := testAddress(2)

	s.SetupTest()
	s.appointCommittee(committee, 1_000, 5, destination)
	s.setBlockHeight(20)
	s.fundReserve(10)
	s.treasuryReader.insuranceShortfall = math.NewInt(1_000)

	// No Bank expectation: the floor is checked before any coin moves.
	_, err := s.msgServer.CommitteeFundInsurance(s.ctx, &types.MsgCommitteeFundInsurance{
		Committee: committee, ExpectedTerm: 1, Amount: noahCoin(7),
	})
	s.Require().ErrorContains(err, "cannot fund")

	// The exact remainder still fits.
	s.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
		gomock.Any(),
		types.StrategicReserveName,
		claimstypes.InsuranceName,
		sdk.NewCoins(noahCoin(5)),
	).Return(nil)
	_, err = s.msgServer.CommitteeFundInsurance(s.ctx, &types.MsgCommitteeFundInsurance{
		Committee: committee, ExpectedTerm: 1, Amount: noahCoin(5),
	})
	s.Require().NoError(err)
}

// TestRecognisedCapital pins the answer Treasury's waterfall consumes under an
// empty recognition policy: exactly the NOAH balance, with non-NOAH custody
// invisible to it until governance lists a denomination. The recognition suite
// covers what listing grants.
func (s *KeeperTestSuite) TestRecognisedCapital() {
	tests := []struct {
		name    string
		balance int64
	}{
		{name: "empty fund", balance: 0},
		{name: "funded", balance: 500},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.fundReserve(tc.balance)
			s.fundReserveAsset(chain.USDBaseDenom, 25)

			recognised, err := s.keeper.RecognisedCapital(s.ctx)
			s.Require().NoError(err)
			s.Require().True(
				math.NewInt(tc.balance).Equal(recognised),
				"expected %d, got %s", tc.balance, recognised,
			)
		})
	}
}
