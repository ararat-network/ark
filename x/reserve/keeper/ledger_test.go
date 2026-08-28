package keeper_test

import (
	"math/big"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/mandate"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	"github.com/ararat-network/ark/x/reserve/types"
)

// testFeed is the series the test claim prices through; testAsset is the claim
// symbol itself. They are spelled apart because the partition is the thing
// under test: an eligibility entry and a position name the claim, while every
// Oracle read names the feed. Ark-issued paper is spelled chain.USDBaseDenom,
// a bare priced denomination admitted by registry membership.
const (
	testFeed  = "abill"
	testAsset = testFeed + "-x"
)

func assetCoin(amount int64) sdk.Coin {
	return sdk.NewInt64Coin(testAsset, amount)
}

// paperCoin builds Ark-issued paper, which is what an on-chain movement of
// non-NOAH custody actually moves: an in-kind outflow sells paper the account
// holds, and an in-kind return delivers paper into it. External custody can do
// neither — it is off-chain, and its sale comes back as NOAH or paper.
func paperCoin(amount int64) sdk.Coin {
	return sdk.NewInt64Coin(chain.USDBaseDenom, amount)
}

// appointCommittee stores an active mandate and resets allowance usage,
// mirroring a successful MsgSetReserveMandate.
func (s *KeeperTestSuite) appointCommittee(committee string, allowance, floor int64, destinations ...string) {
	reserveMandate := types.ReserveMandate{
		Envelope: mandate.Envelope{
			Term:             1,
			Committee:        committee,
			ActivationHeight: 10,
			ExpiryHeight:     1_000,
		},
		DeploymentAllowance: noahCoin(allowance),
		MinimumNoahBalance:  noahCoin(floor),
		Destinations:        destinations,
	}
	s.Require().NoError(s.keeper.Mandate.Set(s.ctx, reserveMandate))
	s.Require().NoError(s.keeper.AllowanceUsed.Set(s.ctx, math.ZeroInt()))
}

func (s *KeeperTestSuite) setBlockHeight(height int64) {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(height)
}

func testAddress(seed byte) string {
	bz := make([]byte, 20)
	for i := range bz {
		bz[i] = seed
	}
	return sdk.AccAddress(bz).String()
}

// expectDeploymentSend stubs the account-facing send a deployment performs.
func (s *KeeperTestSuite) expectDeploymentSend(destination string, amount int64) {
	s.bankKeeper.EXPECT().SendCoinsFromModuleToAccount(
		gomock.Any(),
		types.StrategicReserveName,
		sdk.MustAccAddressFromBech32(destination),
		sdk.NewCoins(noahCoin(amount)),
	).Return(nil)
}

// deploy runs one authorised deployment and returns the position it funded.
// The venue travels only on an opening: funding inherits the position's, and
// the handler refuses a restated one.
func (s *KeeperTestSuite) deploy(committee, destination string, amount, acquired int64, positionID uint64) uint64 {
	s.expectDeploymentSend(destination, amount)
	venue := "custodian-alpha"
	if positionID != 0 {
		venue = ""
	}
	resp, err := s.msgServer.CommitteeDeploy(s.ctx, &types.MsgCommitteeDeploy{
		Committee:      committee,
		ExpectedTerm:   1,
		PositionId:     positionID,
		Destination:    destination,
		Amount:         noahCoin(amount),
		Acquired:       assetCoin(acquired),
		VenueReference: venue,
		Reference:      "tx-0x01",
	})
	s.Require().NoError(err)
	return resp.PositionId
}

func (s *KeeperTestSuite) TestSetReserveMandate() {
	committee := testAddress(1)
	destination := testAddress(2)

	s.Run("appoints and derives the next term", func() {
		s.SetupTest()
		_, err := s.msgServer.SetReserveMandate(s.ctx, &types.MsgSetReserveMandate{
			Authority:           s.authority,
			Committee:           committee,
			ActivationHeight:    10,
			ExpiryHeight:        1_000,
			DeploymentAllowance: noahCoin(1_000),
			MinimumNoahBalance:  noahCoin(100),
			Destinations:        []string{destination},
		})
		s.Require().NoError(err)

		stored, err := s.keeper.Mandate.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().Equal(uint64(1), stored.Term)
		s.Require().Equal(committee, stored.Committee)
		s.Require().Equal([]string{destination}, stored.Destinations)
	})

	s.Run("replacement resets allowance and clears destinations", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.Require().NoError(s.keeper.AllowanceUsed.Set(s.ctx, math.NewInt(400)))

		_, err := s.msgServer.SetReserveMandate(s.ctx, &types.MsgSetReserveMandate{
			Authority:           s.authority,
			Committee:           testAddress(3),
			ActivationHeight:    10,
			ExpiryHeight:        1_000,
			DeploymentAllowance: noahCoin(500),
			MinimumNoahBalance:  noahCoin(0),
			Destinations:        []string{testAddress(4)},
		})
		s.Require().NoError(err)

		used, err := s.keeper.AllowanceUsed.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().True(used.IsZero())
		stored, err := s.keeper.Mandate.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().Equal([]string{testAddress(4)}, stored.Destinations)
	})

	s.Run("empty committee disables", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)

		_, err := s.msgServer.SetReserveMandate(s.ctx, &types.MsgSetReserveMandate{
			Authority:           s.authority,
			DeploymentAllowance: noahCoin(0),
			MinimumNoahBalance:  noahCoin(0),
		})
		s.Require().NoError(err)

		stored, err := s.keeper.Mandate.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().True(stored.IsDisabled())
		s.Require().Empty(stored.Destinations)
	})

	tests := []struct {
		name      string
		msg       *types.MsgSetReserveMandate
		expectErr string
	}{
		{
			name: "wrong authority",
			msg: &types.MsgSetReserveMandate{
				Authority:           testAddress(9),
				Committee:           committee,
				ActivationHeight:    10,
				ExpiryHeight:        1_000,
				DeploymentAllowance: noahCoin(1_000),
				MinimumNoahBalance:  noahCoin(0),
				Destinations:        []string{destination},
			},
			expectErr: "invalid authority",
		},
		{
			name: "committee equals authority",
			msg: &types.MsgSetReserveMandate{
				Authority:           s.authority,
				Committee:           s.authority,
				ActivationHeight:    10,
				ExpiryHeight:        1_000,
				DeploymentAllowance: noahCoin(1_000),
				MinimumNoahBalance:  noahCoin(0),
				Destinations:        []string{destination},
			},
			expectErr: "must be distinct from the Reserve authority",
		},
		{
			name: "configured mandate needs a destination",
			msg: &types.MsgSetReserveMandate{
				Authority:           s.authority,
				Committee:           committee,
				ActivationHeight:    10,
				ExpiryHeight:        1_000,
				DeploymentAllowance: noahCoin(1_000),
				MinimumNoahBalance:  noahCoin(0),
			},
			expectErr: "must name at least one destination",
		},
		{
			name: "non-positive allowance",
			msg: &types.MsgSetReserveMandate{
				Authority:           s.authority,
				Committee:           committee,
				ActivationHeight:    10,
				ExpiryHeight:        1_000,
				DeploymentAllowance: noahCoin(0),
				MinimumNoahBalance:  noahCoin(0),
				Destinations:        []string{destination},
			},
			expectErr: "allowance must be positive",
		},
		{
			name: "duplicate destination",
			msg: &types.MsgSetReserveMandate{
				Authority:           s.authority,
				Committee:           committee,
				ActivationHeight:    10,
				ExpiryHeight:        1_000,
				DeploymentAllowance: noahCoin(1_000),
				MinimumNoahBalance:  noahCoin(0),
				Destinations:        []string{destination, destination},
			},
			expectErr: "duplicate Reserve destination",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.SetupTest()
			_, err := s.msgServer.SetReserveMandate(s.ctx, tc.msg)
			s.Require().ErrorContains(err, tc.expectErr)
		})
	}
}

func (s *KeeperTestSuite) TestCommitteeDeploy() {
	committee := testAddress(1)
	destination := testAddress(2)

	s.Run("records both legs and consumes allowance", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 100, destination)
		s.setBlockHeight(20)
		s.fundReserve(500)

		positionID := s.deploy(committee, destination, 300, 12, 0)

		position, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().Equal(noahCoin(300), position.Deployed)
		s.Require().Equal(assetCoin(12), position.Quantity)
		s.Require().True(position.Returned.Amount.IsZero())
		s.Require().False(position.IsClosed())

		used, err := s.keeper.AllowanceUsed.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().Equal(math.NewInt(300), used)
	})

	// An in-kind outflow is how on-chain custody leaves by sale rather than by
	// burn: the peg-defence round trip buys a stable off-chain, takes delivery,
	// and later sells it back. These cases cover that outbound leg.
	s.Run("deploys held custody in kind at its booked valuation", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 100, destination)
		s.setBlockHeight(20)
		s.fundReserve(150)
		s.registerAsset(chain.USDBaseDenom)
		s.fundReserveAsset(chain.USDBaseDenom, 10)
		s.stubRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyMustNewDecFromStr("0.05")})

		s.bankKeeper.EXPECT().SendCoinsFromModuleToAccount(
			gomock.Any(),
			types.StrategicReserveName,
			sdk.MustAccAddressFromBech32(destination),
			sdk.NewCoins(paperCoin(4)),
		).Return(nil)
		resp, err := s.msgServer.CommitteeDeploy(s.ctx, &types.MsgCommitteeDeploy{
			Committee: committee, ExpectedTerm: 1, Destination: destination,
			Amount:         paperCoin(4),
			Acquired:       assetCoin(4),
			VenueReference: "desk-beta",
			Reference:      "tx-0x09",
		})
		s.Require().NoError(err)

		// Cost basis is the anoah valuation, not the coin, so Realised stays
		// comparable with a NOAH-funded position. The rate is quoted in asset
		// units per one NOAH, so a twentieth of a unit to the NOAH values the
		// four deployed units at eighty anoah.
		position, err := s.keeper.OpenPositions.Get(s.ctx, resp.PositionId)
		s.Require().NoError(err)
		s.Require().Equal(noahCoin(80), position.Deployed)

		// No allowance is consumed: the envelope meters NOAH leaving, and this
		// capital was charged when it first deployed. A spent committee can
		// therefore still unwind — the sale path never jams shut.
		used, err := s.keeper.AllowanceUsed.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().True(used.IsZero())

		entry := s.ledgerFor(resp.PositionId)[0]
		s.Require().Equal(paperCoin(4), entry.MovedCoin)
		s.Require().Equal(noahCoin(80), entry.MovedNoahValue)
	})

	s.Run("a spent allowance does not block an in-kind sale", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(5_000)
		s.registerAsset(chain.USDBaseDenom)
		s.fundReserveAsset(chain.USDBaseDenom, 10)
		s.stubRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyMustNewDecFromStr("0.05")})
		positionID := s.deploy(committee, destination, 1_000, 10, 0)

		// The whole envelope is gone; unwinding must still be possible, or the
		// meter would block risk reduction exactly when it matters.
		s.bankKeeper.EXPECT().SendCoinsFromModuleToAccount(
			gomock.Any(),
			types.StrategicReserveName,
			sdk.MustAccAddressFromBech32(destination),
			sdk.NewCoins(paperCoin(10)),
		).Return(nil)
		_, err := s.msgServer.CommitteeDeploy(s.ctx, &types.MsgCommitteeDeploy{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			Destination: destination,
			Amount:      paperCoin(10), Acquired: assetCoin(10),
			Reference: "tx-0x09",
		})
		s.Require().NoError(err)
	})

	s.Run("refuses an in-kind deployment the account does not hold", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.registerAsset(chain.USDBaseDenom)
		s.fundReserveAsset(chain.USDBaseDenom, 3)
		s.stubRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyMustNewDecFromStr("0.05")})

		_, err := s.msgServer.CommitteeDeploy(s.ctx, &types.MsgCommitteeDeploy{
			Committee: committee, ExpectedTerm: 1, Destination: destination,
			Amount: paperCoin(4), Acquired: assetCoin(4),
			VenueReference: "desk-beta", Reference: "tx-0x09",
		})
		s.Require().ErrorContains(err, "exceeds the Reserve's")
	})

	s.Run("refuses an in-kind deployment under a dark feed", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.registerAsset(chain.USDBaseDenom)
		s.fundReserveAsset(chain.USDBaseDenom, 10)
		// No rate for the paper going out: unlike a return, an outflow can wait
		// for a price rather than book a permanently false zero cost basis.
		s.stubRates(oracletypes.RateSet{})

		_, err := s.msgServer.CommitteeDeploy(s.ctx, &types.MsgCommitteeDeploy{
			Committee: committee, ExpectedTerm: 1, Destination: destination,
			Amount: paperCoin(4), Acquired: assetCoin(4),
			VenueReference: "desk-beta", Reference: "tx-0x09",
		})
		s.Require().ErrorContains(err, "no fresh price feed")
	})

	s.Run("the NOAH floor does not bind an in-kind outflow", func() {
		s.SetupTest()
		// The floor exceeds the NOAH balance, so a NOAH deployment of any size
		// is refused; the in-kind one is unaffected because the floor is a
		// NOAH liquidity bridge.
		s.appointCommittee(committee, 10_000, 500, destination)
		s.setBlockHeight(20)
		s.fundReserve(400)
		s.registerAsset(chain.USDBaseDenom)
		s.fundReserveAsset(chain.USDBaseDenom, 10)
		s.stubRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyMustNewDecFromStr("0.05")})

		s.bankKeeper.EXPECT().SendCoinsFromModuleToAccount(
			gomock.Any(),
			types.StrategicReserveName,
			sdk.MustAccAddressFromBech32(destination),
			sdk.NewCoins(paperCoin(2)),
		).Return(nil)
		_, err := s.msgServer.CommitteeDeploy(s.ctx, &types.MsgCommitteeDeploy{
			Committee: committee, ExpectedTerm: 1, Destination: destination,
			Amount: paperCoin(2), Acquired: assetCoin(2),
			VenueReference: "desk-beta", Reference: "tx-0x09",
		})
		s.Require().NoError(err)
	})

	// The attested leg is the one field a committee writes that no balance
	// bounds, and it feeds the fold Treasury settles against every block. Both
	// paths that set it must refuse an absurd one here, where a refusal costs a
	// message, rather than at the block where it would cost liveness.
	s.Run("refuses an attestation past the cap", func() {
		s.SetupTest()
		s.appointCommittee(committee, 10_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(5_000)

		// No send is stubbed on either refusal: the attestation is judged
		// before any coin moves.
		_, err := s.msgServer.CommitteeDeploy(s.ctx, &types.MsgCommitteeDeploy{
			Committee: committee, ExpectedTerm: 1, Destination: destination,
			Amount:         noahCoin(100),
			Acquired:       sdk.NewCoin(testAsset, types.MaxAttestedQuantity.AddRaw(1)),
			VenueReference: "custodian-alpha", Reference: "tx-0x09",
		})
		s.Require().ErrorContains(err, "position quantity must not exceed")

		// The funding path adds to a stored quantity before it is judged, so an
		// attestation near the Int ceiling has to be answered by the checked
		// addition rather than by a panic on the way to that judgment.
		positionID := s.deploy(committee, destination, 100, 10, 0)
		_, err = s.msgServer.CommitteeDeploy(s.ctx, &types.MsgCommitteeDeploy{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			Destination: destination,
			Amount:      noahCoin(100),
			Acquired: sdk.NewCoin(testAsset, math.NewIntFromBigInt(
				new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)),
			)),
			Reference: "tx-0x0a",
		})
		s.Require().ErrorContains(err, "funding the attested quantity of position")

		// The position kept exactly what the successful deployment attested.
		position, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().Equal(assetCoin(10), position.Quantity)
	})

	s.Run("allowance is consumed permanently", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(5_000)

		s.deploy(committee, destination, 600, 6, 0)

		// No send is stubbed: the allowance is checked before any coin moves,
		// so a refused deployment must never reach Bank.
		_, err := s.msgServer.CommitteeDeploy(s.ctx, &types.MsgCommitteeDeploy{
			Committee: committee, ExpectedTerm: 1, Destination: destination,
			Amount: noahCoin(500), Acquired: assetCoin(5),
			VenueReference: "custodian-alpha",
		})
		s.Require().ErrorContains(err, "exceeds remaining Reserve allowance")

		// The exact remainder still fits, and the refusal consumed nothing.
		s.deploy(committee, destination, 400, 4, 0)
		used, err := s.keeper.AllowanceUsed.Get(s.ctx)
		s.Require().NoError(err)
		s.Require().Equal(math.NewInt(1_000), used)
	})

	s.Run("the mandate NOAH floor blocks a deployment", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 300, destination)
		s.setBlockHeight(20)
		s.fundReserve(500)

		_, err := s.msgServer.CommitteeDeploy(s.ctx, &types.MsgCommitteeDeploy{
			Committee: committee, ExpectedTerm: 1, Destination: destination,
			Amount: noahCoin(250), Acquired: assetCoin(2),
			VenueReference: "custodian-alpha",
		})
		s.Require().ErrorContains(err, "mandate floor")
	})

	s.Run("an unnamed destination is refused", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(500)

		_, err := s.msgServer.CommitteeDeploy(s.ctx, &types.MsgCommitteeDeploy{
			Committee: committee, ExpectedTerm: 1, Destination: testAddress(8),
			Amount: noahCoin(100), Acquired: assetCoin(1),
			VenueReference: "custodian-alpha",
		})
		s.Require().ErrorContains(err, "not named by the Reserve mandate")
	})

	s.Run("funding an open position accumulates both legs", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(5_000)

		positionID := s.deploy(committee, destination, 300, 3, 0)
		s.deploy(committee, destination, 200, 2, positionID)

		position, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().Equal(noahCoin(500), position.Deployed)
		s.Require().Equal(assetCoin(5), position.Quantity)
	})

	s.Run("a position cannot be funded with a different asset", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(5_000)
		positionID := s.deploy(committee, destination, 300, 3, 0)

		_, err := s.msgServer.CommitteeDeploy(s.ctx, &types.MsgCommitteeDeploy{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			Destination: destination, Amount: noahCoin(100),
			Acquired: sdk.NewInt64Coin("anote", 1),
		})
		s.Require().ErrorContains(err, "cannot be funded with")
	})

	// Restating the venue is a correction's work, and the refusal says so.
	s.Run("funding refuses a restated venue reference", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(5_000)
		positionID := s.deploy(committee, destination, 300, 3, 0)

		_, err := s.msgServer.CommitteeDeploy(s.ctx, &types.MsgCommitteeDeploy{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			Destination: destination, Amount: noahCoin(100),
			Acquired: assetCoin(1), VenueReference: "desk-beta",
			Reference: "tx-0x02",
		})
		s.Require().ErrorContains(err, "cannot restate its venue reference")
	})

	mandateTests := []struct {
		name      string
		committee string
		term      uint64
		height    int64
	}{
		{name: "wrong committee", committee: testAddress(8), term: 1, height: 20},
		{name: "stale expected term", committee: committee, term: 0, height: 20},
		{name: "before activation", committee: committee, term: 1, height: 9},
		{name: "at expiry", committee: committee, term: 1, height: 1_000},
	}
	for _, tc := range mandateTests {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.appointCommittee(committee, 1_000, 0, destination)
			s.setBlockHeight(tc.height)
			s.fundReserve(5_000)

			_, err := s.msgServer.CommitteeDeploy(s.ctx, &types.MsgCommitteeDeploy{
				Committee: tc.committee, ExpectedTerm: tc.term, Destination: destination,
				Amount: noahCoin(100), Acquired: assetCoin(1),
				VenueReference: "custodian-alpha",
			})
			s.Require().ErrorContains(err, types.ReserveMandateLabel)
		})
	}
}

// TestLedgerLifecycle walks one position from deployment through a partial
// return to closure, checking that the ledger and the derived aggregates stay
// in agreement at every step. This is the invariant genesis re-derivation
// depends on.
func (s *KeeperTestSuite) TestLedgerLifecycle() {
	committee := testAddress(1)
	destination := testAddress(2)

	s.SetupTest()
	s.appointCommittee(committee, 1_000, 0, destination)
	s.setBlockHeight(20)
	s.fundReserve(1_000)

	positionID := s.deploy(committee, destination, 400, 4, 0)

	// A holding update restates the attested quantity and moves no coin.
	_, err := s.msgServer.CommitteeRecordUpdate(s.ctx, &types.MsgCommitteeRecordUpdate{
		Committee: committee, ExpectedTerm: 1, PositionId: positionID,
		Quantity:  assetCoin(5),
		Reference: "statement-2026-08",
	})
	s.Require().NoError(err)

	// A NOAH return is recovery the chain can value; the asset leg shrinks.
	_, err = s.msgServer.CommitteeAttributeReturn(s.ctx, &types.MsgCommitteeAttributeReturn{
		Committee: committee, ExpectedTerm: 1, PositionId: positionID,
		ReturnedCoin:      noahCoin(250),
		RemainingQuantity: assetCoin(2),
		Reference:         "tx-0x02",
	})
	s.Require().NoError(err)

	position, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
	s.Require().NoError(err)
	s.Require().Equal(noahCoin(400), position.Deployed)
	s.Require().Equal(noahCoin(250), position.Returned)
	s.Require().Equal(assetCoin(2), position.Quantity)

	_, err = s.msgServer.CommitteeClosePosition(s.ctx, &types.MsgCommitteeClosePosition{
		Committee: committee, ExpectedTerm: 1, PositionId: positionID,
		Reference: "closure-memo",
	})
	s.Require().NoError(err)

	// Closing moved the record: it is out of the open store and in the closed
	// one, which is what takes it out of every open-set walk.
	held, err := s.keeper.OpenPositions.Has(s.ctx, positionID)
	s.Require().NoError(err)
	s.Require().False(held)
	position, err = s.keeper.ClosedPositions.Get(s.ctx, positionID)
	s.Require().NoError(err)
	s.Require().True(position.IsClosed())
	// Realised is a loss: 250 back against 400 out.
	s.Require().Equal(math.NewInt(-150), position.Realised())

	// The ledger recorded every act, and the lifetime folds agree with it.
	entries := s.ledgerFor(positionID)
	s.Require().Len(entries, 4)
	s.Require().Equal(types.EntryKind_ENTRY_KIND_DEPLOYMENT, entries[0].Kind)
	s.Require().Equal(types.EntryKind_ENTRY_KIND_QUANTITY_UPDATE, entries[1].Kind)
	s.Require().Equal(types.EntryKind_ENTRY_KIND_RETURN_ATTRIBUTION, entries[2].Kind)
	s.Require().Equal(types.EntryKind_ENTRY_KIND_CLOSURE, entries[3].Kind)

	// The proven legs live on the position, which is the only place they are
	// stored and the only place genesis re-derives them.
	closed, err := s.keeper.ClosedPositions.Get(s.ctx, positionID)
	s.Require().NoError(err)
	s.Require().Equal(noahCoin(400), closed.Deployed)
	s.Require().Equal(noahCoin(250), closed.Returned)

	// A closed position accepts no further committee bookkeeping: closure moved
	// the record out of the open store, which is the only store the act reads.
	_, err = s.msgServer.CommitteeRecordUpdate(s.ctx, &types.MsgCommitteeRecordUpdate{
		Committee: committee, ExpectedTerm: 1, PositionId: positionID,
		Quantity: assetCoin(9),
	})
	s.Require().ErrorContains(err, "getting open position")
}

// TestReturnAttributionValuation pins how recovery is crystallised: NOAH at
// par, a priced in-kind return at the attribution-time feed rate, and a
// feed-dark return refused outright rather than booked at a zero no correction
// could later repair. The valuation is the keeper's arithmetic in every case,
// never the committee's claim.
func (s *KeeperTestSuite) TestReturnAttributionValuation() {
	committee := testAddress(1)
	destination := testAddress(2)

	s.Run("priced in-kind return values at the feed rate", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(1_000)
		s.registerAsset(chain.USDBaseDenom)
		s.fundReserveAsset(chain.USDBaseDenom, 4)
		s.stubRates(oracletypes.RateSet{chain.USDBaseDenom: math.LegacyMustNewDecFromStr("0.05")})
		positionID := s.deploy(committee, destination, 400, 4, 0)

		// The position holds external custody; the return arrives as paper, which
		// is what an off-chain sale can actually deliver on-chain.
		_, err := s.msgServer.CommitteeAttributeReturn(s.ctx, &types.MsgCommitteeAttributeReturn{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			ReturnedCoin:      paperCoin(4),
			RemainingQuantity: assetCoin(0),
			Reference:         "tx-0x03",
		})
		s.Require().NoError(err)

		position, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().Equal(noahCoin(80), position.Returned)
		entries := s.ledgerFor(positionID)
		s.Require().Equal(noahCoin(80), entries[len(entries)-1].MovedNoahValue)
		s.Require().Equal(paperCoin(4), entries[len(entries)-1].MovedCoin)
	})

	// The feed that matters is the returned coin's, not the position's: an
	// inflow is priced by what it is, and the external custody the position
	// holds never crosses the bank boundary at all.
	s.Run("feed-dark return is refused", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(1_000)
		s.registerAsset(chain.USDBaseDenom)
		s.fundReserveAsset(chain.USDBaseDenom, 4)
		// stubRates registers one AnyTimes expectation closing over this map, so
		// the feed is moved by mutating it rather than by re-stubbing.
		rates := oracletypes.RateSet{chain.USDBaseDenom: math.LegacyMustNewDecFromStr("0.05")}
		s.stubRates(rates)
		positionID := s.deploy(committee, destination, 400, 4, 0)
		delete(rates, chain.USDBaseDenom)

		_, err := s.msgServer.CommitteeAttributeReturn(s.ctx, &types.MsgCommitteeAttributeReturn{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			ReturnedCoin:      paperCoin(4),
			RemainingQuantity: assetCoin(0),
			Reference:         "tx-0x03",
		})
		s.Require().ErrorContains(err, "no fresh price feed")

		// Nothing was written: the position is untouched and the committee is
		// free to attribute once the feed returns.
		position, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().True(position.Returned.Amount.IsZero())
		s.Require().Len(s.ledgerFor(positionID), 1)
	})

	// Dust is where the mirror with a deployment stops. A live feed pricing the
	// return to zero is a real valuation, and the coins are already in custody,
	// so refusing would leave them unattributable to any position.
	s.Run("dust return under a live feed values at zero", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(1_000)
		s.registerAsset(chain.USDBaseDenom)
		s.fundReserveAsset(chain.USDBaseDenom, 4)
		rates := oracletypes.RateSet{chain.USDBaseDenom: math.LegacyMustNewDecFromStr("0.05")}
		s.stubRates(rates)
		positionID := s.deploy(committee, destination, 400, 4, 0)
		// The feed collapses: forty units to the NOAH makes the four returned
		// units a tenth of an anoah, which truncates to zero. Dust now comes
		// from a rate far above one, the opposite end from where a rate quoted
		// the other way round would have put it.
		rates[chain.USDBaseDenom] = math.LegacyNewDec(40)

		_, err := s.msgServer.CommitteeAttributeReturn(s.ctx, &types.MsgCommitteeAttributeReturn{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			ReturnedCoin:      paperCoin(4),
			RemainingQuantity: assetCoin(0),
			Reference:         "tx-0x03",
		})
		s.Require().NoError(err)

		position, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().True(position.Returned.Amount.IsZero())
		entries := s.ledgerFor(positionID)
		s.Require().Equal(noahCoin(0), entries[len(entries)-1].MovedNoahValue)
	})

	s.Run("attribution must match custody", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(1_000)
		positionID := s.deploy(committee, destination, 400, 4, 0)

		// The account holds none of the asset, so the claimed inflow is a
		// fiction the handler refuses.
		_, err := s.msgServer.CommitteeAttributeReturn(s.ctx, &types.MsgCommitteeAttributeReturn{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			ReturnedCoin:      assetCoin(4),
			RemainingQuantity: assetCoin(0),
			Reference:         "tx-0x03",
		})
		s.Require().ErrorContains(err, "exceeds the Reserve's")

		// A NOAH attribution beyond the balance is refused identically.
		_, err = s.msgServer.CommitteeAttributeReturn(s.ctx, &types.MsgCommitteeAttributeReturn{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			ReturnedCoin:      noahCoin(1_001),
			RemainingQuantity: assetCoin(0),
			Reference:         "tx-0x03",
		})
		s.Require().ErrorContains(err, "exceeds the Reserve's")
	})
}

// TestImpairmentAuthority pins that impairment is a round trip either
// authority may complete, and that the ledger says which one did.
func (s *KeeperTestSuite) TestImpairmentAuthority() {
	committee := testAddress(1)
	destination := testAddress(2)

	// impairedPosition returns a freshly impaired position and the entry count
	// that leaves behind.
	impairedPosition := func() uint64 {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(1_000)
		positionID := s.deploy(committee, destination, 400, 4, 0)

		_, err := s.msgServer.CommitteeMarkImpaired(s.ctx, &types.MsgCommitteeMarkImpaired{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			Reference: "custodian-default-notice",
		})
		s.Require().NoError(err)
		position, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().True(position.Impaired)
		return positionID
	}

	// Marking down is the conservative direction, and governance holds it for
	// the state the committee cannot cover: after a revoked mandate, a position
	// known dead would otherwise keep counting until a successor is appointed.
	s.Run("governance marks down with no mandate live", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(1_000)
		positionID := s.deploy(committee, destination, 400, 4, 0)

		// Governance revokes the mandate, leaving nobody appointed.
		_, err := s.msgServer.SetReserveMandate(s.ctx, &types.MsgSetReserveMandate{
			Authority: s.authority, Committee: "",
		})
		s.Require().NoError(err)

		_, err = s.msgServer.MarkImpaired(s.ctx, &types.MsgMarkImpaired{
			Authority: s.authority, PositionId: positionID,
			Reference: "custodian-insolvent",
		})
		s.Require().NoError(err)
		position, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().True(position.Impaired)

		entries := s.ledgerFor(positionID)
		s.Require().Len(entries, 2)
		s.Require().Equal(types.EntryKind_ENTRY_KIND_IMPAIRMENT, entries[1].Kind)
		s.Require().Equal(s.authority, entries[1].RecordedBy)
		s.Require().Zero(entries[1].Term)

		// And governance can undo its own mark, still with nobody appointed.
		_, err = s.msgServer.ClearImpairment(s.ctx, &types.MsgClearImpairment{
			Authority: s.authority, PositionId: positionID,
			Reference: "evidence-withdrawn",
		})
		s.Require().NoError(err)
		position, err = s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().False(position.Impaired)
	})

	s.Run("governance marking rejects an unauthorised signer", func() {
		positionID := impairedPosition()

		_, err := s.msgServer.MarkImpaired(s.ctx, &types.MsgMarkImpaired{
			Authority: committee, PositionId: positionID,
		})
		s.Require().ErrorIs(err, errortypes.ErrUnauthorized)
	})

	s.Run("the committee restores what it marked down", func() {
		positionID := impairedPosition()

		_, err := s.msgServer.CommitteeClearImpairment(s.ctx, &types.MsgCommitteeClearImpairment{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			Reference: "custodian-cured",
		})
		s.Require().NoError(err)
		position, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().False(position.Impaired)

		// The reversal is attributable to the appointment that made it, which is
		// what distinguishes it from governance's own.
		entries := s.ledgerFor(positionID)
		s.Require().Len(entries, 3)
		clearing := entries[2]
		s.Require().Equal(types.EntryKind_ENTRY_KIND_IMPAIRMENT, clearing.Kind)
		s.Require().Equal(committee, clearing.RecordedBy)
		s.Require().Equal(uint64(1), clearing.Term)
	})

	// Governance keeps the same power, and reaches a position when no mandate
	// would authorise the committee's message at all.
	s.Run("governance restores at term zero", func() {
		positionID := impairedPosition()

		_, err := s.msgServer.ClearImpairment(s.ctx, &types.MsgClearImpairment{
			Authority: s.authority, PositionId: positionID,
			Reference: "governance-review",
		})
		s.Require().NoError(err)
		position, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().False(position.Impaired)

		entries := s.ledgerFor(positionID)
		s.Require().Len(entries, 3)
		s.Require().Equal(s.authority, entries[2].RecordedBy)
		s.Require().Zero(entries[2].Term)
	})

	// Clearing restores the credit the impairment removed. This is the property
	// the governance gate was protecting, and it is now the committee's to
	// exercise, so it is pinned on the committee's path.
	s.Run("clearing restores recognition credit", func() {
		positionID := impairedPosition()
		s.setPolicy(eligibility(testAsset, "1", "0.5"))
		s.stubRates(oracletypes.RateSet{testAsset: math.LegacyOneDec()})
		s.Require().Equal(math.NewInt(1_000), s.recognised())

		_, err := s.msgServer.CommitteeClearImpairment(s.ctx, &types.MsgCommitteeClearImpairment{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			Reference: "custodian-cured",
		})
		s.Require().NoError(err)
		s.Require().Equal(math.NewInt(1_004), s.recognised())
	})

	s.Run("rejects an unauthorised signer on either path", func() {
		positionID := impairedPosition()
		stranger := testAddress(9)

		_, err := s.msgServer.ClearImpairment(s.ctx, &types.MsgClearImpairment{
			Authority: committee, PositionId: positionID,
		})
		s.Require().ErrorIs(err, errortypes.ErrUnauthorized)

		_, err = s.msgServer.CommitteeClearImpairment(s.ctx, &types.MsgCommitteeClearImpairment{
			Committee: stranger, ExpectedTerm: 1, PositionId: positionID,
		})
		s.Require().ErrorContains(err, types.ReserveMandateLabel)

		// Still impaired: neither refusal left a partial change behind.
		position, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().True(position.Impaired)
	})

	s.Run("rejects a stale term", func() {
		positionID := impairedPosition()

		_, err := s.msgServer.CommitteeClearImpairment(s.ctx, &types.MsgCommitteeClearImpairment{
			Committee: committee, ExpectedTerm: 2, PositionId: positionID,
		})
		s.Require().ErrorContains(err, "term")
	})

	// Clearing an unimpaired position is refused rather than treated as a
	// no-op, so a ledger entry never records a change that did not happen.
	s.Run("refuses to clear what is not impaired", func() {
		positionID := impairedPosition()

		_, err := s.msgServer.CommitteeClearImpairment(s.ctx, &types.MsgCommitteeClearImpairment{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
		})
		s.Require().NoError(err)

		_, err = s.msgServer.CommitteeClearImpairment(s.ctx, &types.MsgCommitteeClearImpairment{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
		})
		s.Require().ErrorContains(err, "already in the requested impairment state")
	})
}

// TestCorrectionRestatesWithoutErasing pins that a governance correction
// changes the position and leaves the mistake in the ledger.
func (s *KeeperTestSuite) TestCorrectionRestatesWithoutErasing() {
	committee := testAddress(1)
	destination := testAddress(2)

	s.SetupTest()
	s.appointCommittee(committee, 1_000, 0, destination)
	s.setBlockHeight(20)
	s.fundReserve(1_000)
	positionID := s.deploy(committee, destination, 400, 4, 0)

	entries := s.ledgerFor(positionID)
	s.Require().Len(entries, 1)
	deploymentEntry := entries[0]

	_, err := s.msgServer.CorrectPosition(s.ctx, &types.MsgCorrectPosition{
		Authority:  s.authority,
		PositionId: positionID,
		Corrects:   deploymentEntry.EntryId,
		Quantity:   assetCoin(40),
		Reference:  "decimal-error",
	})
	s.Require().NoError(err)

	position, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
	s.Require().NoError(err)
	s.Require().Equal(assetCoin(40), position.Quantity)

	entries = s.ledgerFor(positionID)
	s.Require().Len(entries, 2)
	// The original entry still states what was first recorded.
	s.Require().Equal(assetCoin(4), entries[0].Quantity)
	s.Require().Equal(types.EntryKind_ENTRY_KIND_CORRECTION, entries[1].Kind)
	s.Require().Equal(deploymentEntry.EntryId, entries[1].Corrects)

	// The proven leg is untouched: a correction restates the attested holding,
	// never what the chain moved.
	corrected, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
	s.Require().NoError(err)
	s.Require().Equal(noahCoin(400), corrected.Deployed)
}

// TestClosePositionAuthority pins that either authority may close, and that
// the ledger records which one did. Closure is safe for both because the
// realised figure comes from proven coin movements rather than an attestation,
// so no authority can shade it.
func (s *KeeperTestSuite) TestClosePositionAuthority() {
	committee := testAddress(1)
	destination := testAddress(2)

	// openPosition deploys one position against a live mandate.
	openPosition := func() uint64 {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(1_000)
		return s.deploy(committee, destination, 400, 4, 0)
	}

	s.Run("governance closes at term zero and crystallises the realised figure", func() {
		positionID := openPosition()
		_, err := s.msgServer.CommitteeAttributeReturn(s.ctx, &types.MsgCommitteeAttributeReturn{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			ReturnedCoin:      noahCoin(500),
			RemainingQuantity: assetCoin(0),
			Reference:         "tx-0x07",
		})
		s.Require().NoError(err)

		_, err = s.msgServer.ClosePosition(s.ctx, &types.MsgClosePosition{
			Authority: s.authority, PositionId: positionID,
			Reference: "wind-down",
		})
		s.Require().NoError(err)

		position, err := s.keeper.ClosedPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().True(position.IsClosed())
		s.Require().Equal(uint64(20), position.ClosedHeight)
		// 500 back against 400 out: the profit is arithmetic over proven legs,
		// identical whichever authority closed.
		s.Require().Equal(math.NewInt(100), position.Realised())

		entries := s.ledgerFor(positionID)
		closure := entries[len(entries)-1]
		s.Require().Equal(types.EntryKind_ENTRY_KIND_CLOSURE, closure.Kind)
		s.Require().Equal(s.authority, closure.RecordedBy)
		s.Require().Zero(closure.Term)

		s.requireClosureEvent(positionID)
	})

	s.Run("the committee closes under its term", func() {
		positionID := openPosition()

		_, err := s.msgServer.CommitteeClosePosition(s.ctx, &types.MsgCommitteeClosePosition{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
		})
		s.Require().NoError(err)

		entries := s.ledgerFor(positionID)
		closure := entries[len(entries)-1]
		s.Require().Equal(committee, closure.RecordedBy)
		s.Require().Equal(uint64(1), closure.Term)

		s.requireClosureEvent(positionID)
	})

	// Closing an already-closed position is refused rather than re-stamping the
	// height and emitting a second closure event for one closure.
	s.Run("refuses to close twice on either path", func() {
		positionID := openPosition()
		_, err := s.msgServer.ClosePosition(s.ctx, &types.MsgClosePosition{
			Authority: s.authority, PositionId: positionID,
		})
		s.Require().NoError(err)

		_, err = s.msgServer.ClosePosition(s.ctx, &types.MsgClosePosition{
			Authority: s.authority, PositionId: positionID,
		})
		s.Require().ErrorContains(err, "getting open position")

		_, err = s.msgServer.CommitteeClosePosition(s.ctx, &types.MsgCommitteeClosePosition{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
		})
		s.Require().ErrorContains(err, "getting open position")
	})

	s.Run("rejects an unauthorised signer", func() {
		positionID := openPosition()

		_, err := s.msgServer.ClosePosition(s.ctx, &types.MsgClosePosition{
			Authority: committee, PositionId: positionID,
		})
		s.Require().ErrorIs(err, errortypes.ErrUnauthorized)

		position, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().False(position.IsClosed())
	})

	// The deadlock this message exists to break. An open position blocks
	// removal of its denomination's feed, so without a governance closure an
	// asset wind-down would need a committee appointed purely to close the dead
	// positions first.
	s.Run("governance closure releases the feed guard with no mandate live", func() {
		positionID := openPosition()

		referents, err := s.keeper.FeedReferents(s.ctx, testFeed)
		s.Require().NoError(err)
		s.Require().Len(referents, 1)
		s.Require().Contains(referents[0].Referent, "open position")

		_, err = s.msgServer.SetReserveMandate(s.ctx, &types.MsgSetReserveMandate{
			Authority: s.authority, Committee: "",
		})
		s.Require().NoError(err)

		_, err = s.msgServer.ClosePosition(s.ctx, &types.MsgClosePosition{
			Authority: s.authority, PositionId: positionID,
			Reference: "asset-retirement",
		})
		s.Require().NoError(err)

		referents, err = s.keeper.FeedReferents(s.ctx, testAsset)
		s.Require().NoError(err)
		s.Require().Empty(referents)
	})
}

// requireClosureEvent asserts one closure event was emitted for the position.
func (s *KeeperTestSuite) requireClosureEvent(positionID uint64) {
	position, err := s.keeper.ClosedPositions.Get(s.ctx, positionID)
	s.Require().NoError(err)
	expected, err := sdk.TypedEventToEvent(&types.EventPositionClosed{
		PositionId: position.PositionId,
		Deployed:   position.Deployed,
		Returned:   position.Returned,
		Realised:   position.Realised(),
	})
	s.Require().NoError(err)
	s.Require().Contains(sdk.UnwrapSDKContext(s.ctx).EventManager().Events(), expected)
}

// TestCommitteeCorrectPosition pins the committee's correction against the
// governance one: the same reach, a different attribution.
func (s *KeeperTestSuite) TestCommitteeCorrectPosition() {
	committee := testAddress(1)
	destination := testAddress(2)

	// correctable opens one position and returns it with its deployment entry.
	correctable := func() (uint64, uint64) {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(1_000)
		positionID := s.deploy(committee, destination, 400, 4, 0)
		entries := s.ledgerFor(positionID)
		s.Require().Len(entries, 1)
		return positionID, entries[0].EntryId
	}

	s.Run("restates and attributes to the acting term", func() {
		positionID, entryID := correctable()

		_, err := s.msgServer.CommitteeCorrectPosition(s.ctx, &types.MsgCommitteeCorrectPosition{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			Corrects:       entryID,
			Quantity:       assetCoin(40),
			VenueReference: "custodian-beta",
			Reference:      "decimal-error",
		})
		s.Require().NoError(err)

		position, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().Equal(assetCoin(40), position.Quantity)
		s.Require().Equal("custodian-beta", position.VenueReference)

		entries := s.ledgerFor(positionID)
		s.Require().Len(entries, 2)
		// The mistake survives beside its correction, as under governance.
		s.Require().Equal(assetCoin(4), entries[0].Quantity)
		s.Require().Equal(types.EntryKind_ENTRY_KIND_CORRECTION, entries[1].Kind)
		s.Require().Equal(entryID, entries[1].Corrects)
		s.Require().Equal(committee, entries[1].RecordedBy)
		s.Require().Equal(uint64(1), entries[1].Term)

		// The proven leg is untouched: a correction restates the attestation,
		// never what the chain moved.
		s.Require().Equal(noahCoin(400), position.Deployed)
	})

	// Booking to the wrong instrument is a real error, so the committee may
	// restate the asset itself — the same reach governance has.
	s.Run("restates the held asset", func() {
		positionID, entryID := correctable()

		_, err := s.msgServer.CommitteeCorrectPosition(s.ctx, &types.MsgCommitteeCorrectPosition{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			Corrects:  entryID,
			Quantity:  sdk.NewInt64Coin(sdrExternal, 4),
			Reference: "booked-to-the-wrong-instrument",
		})
		s.Require().NoError(err)

		position, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().Equal(sdk.NewInt64Coin(sdrExternal, 4), position.Quantity)
	})

	// A closed position is history that should still read true. Recognition
	// already skips closed positions, so this moves no capital.
	s.Run("corrects a closed position", func() {
		positionID, entryID := correctable()
		_, err := s.msgServer.CommitteeClosePosition(s.ctx, &types.MsgCommitteeClosePosition{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
		})
		s.Require().NoError(err)

		_, err = s.msgServer.CommitteeCorrectPosition(s.ctx, &types.MsgCommitteeCorrectPosition{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			Corrects:  entryID,
			Quantity:  assetCoin(40),
			Reference: "post-closure-restatement",
		})
		s.Require().NoError(err)

		// The restatement went back to the closed store rather than reopening
		// the position.
		position, err := s.keeper.ClosedPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().True(position.IsClosed())
		s.Require().Equal(assetCoin(40), position.Quantity)
		held, err := s.keeper.OpenPositions.Has(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().False(held)

		// The books still balance. A correction moves no coin, so it folds into
		// neither the deployed nor the returned sum, and the invariant that makes
		// the ledger authoritative is untouched by restating a quantity.
		exported, err := s.keeper.ExportGenesis(s.ctx)
		s.Require().NoError(err)
		s.Require().NoError(exported.Validate())
	})

	// The correcting entry must name an entry of the position it corrects, or
	// the ledger stops being a chain of restatements.
	s.Run("refuses an entry belonging to another position", func() {
		positionID, entryID := correctable()
		other := s.deploy(committee, destination, 100, 1, 0)

		_, err := s.msgServer.CommitteeCorrectPosition(s.ctx, &types.MsgCommitteeCorrectPosition{
			Committee: committee, ExpectedTerm: 1, PositionId: other,
			Corrects: entryID, Quantity: assetCoin(10),
		})
		s.Require().ErrorContains(err, "belongs to position")

		// And an entry that does not exist at all.
		_, err = s.msgServer.CommitteeCorrectPosition(s.ctx, &types.MsgCommitteeCorrectPosition{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
			Corrects: 9_999, Quantity: assetCoin(10),
		})
		s.Require().ErrorContains(err, "getting corrected entry")
	})

	s.Run("refuses an unauthorised signer", func() {
		positionID, entryID := correctable()

		_, err := s.msgServer.CommitteeCorrectPosition(s.ctx, &types.MsgCommitteeCorrectPosition{
			Committee: testAddress(9), ExpectedTerm: 1, PositionId: positionID,
			Corrects: entryID, Quantity: assetCoin(40),
		})
		s.Require().ErrorContains(err, types.ReserveMandateLabel)

		position, err := s.keeper.OpenPositions.Get(s.ctx, positionID)
		s.Require().NoError(err)
		s.Require().Equal(assetCoin(4), position.Quantity)
	})

	s.Run("refuses a stale term", func() {
		positionID, entryID := correctable()

		_, err := s.msgServer.CommitteeCorrectPosition(s.ctx, &types.MsgCommitteeCorrectPosition{
			Committee: committee, ExpectedTerm: 2, PositionId: positionID,
			Corrects: entryID, Quantity: assetCoin(40),
		})
		s.Require().ErrorContains(err, "term")
	})
}

// TestGenesisRoundTripRederivesAggregates proves the exported books re-derive
// to the same figures, which is what makes the ledger the authoritative
// record rather than a parallel log.
func (s *KeeperTestSuite) TestGenesisRoundTripRederivesAggregates() {
	committee := testAddress(1)
	destination := testAddress(2)

	s.SetupTest()
	s.appointCommittee(committee, 1_000, 0, destination)
	s.setBlockHeight(20)
	s.fundReserve(1_000)
	positionID := s.deploy(committee, destination, 400, 4, 0)
	_, err := s.msgServer.CommitteeAttributeReturn(s.ctx, &types.MsgCommitteeAttributeReturn{
		Committee: committee, ExpectedTerm: 1, PositionId: positionID,
		ReturnedCoin:      noahCoin(150),
		RemainingQuantity: assetCoin(2),
		Reference:         "tx-0x04",
	})
	s.Require().NoError(err)

	s.setPolicy(
		eligibility(sdrExternal, "1", "0.5"),
		eligibility(testAsset, "0.5", "0.2"),
	)

	exported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().NoError(exported.Validate())
	s.Require().Len(exported.OpenPositions, 1)
	s.Require().Empty(exported.ClosedPositions)
	s.Require().Len(exported.Ledger, 2)
	// The policy exports in stored order, which is already the sorted order
	// genesis validation demands.
	s.Require().Equal([]types.EligibilityEntry{
		eligibility(testAsset, "0.5", "0.2"),
		eligibility(sdrExternal, "1", "0.5"),
	}, exported.RecognitionPolicy)

	// A tampered position no longer agrees with the ledger it folds. This is
	// the whole books-must-balance invariant: module-wide totals were only ever
	// sums of these, so checking them proved nothing this does not.
	tampered := *exported
	tampered.OpenPositions[0].Deployed = noahCoin(999)
	s.Require().ErrorContains(tampered.Validate(), "does not equal its ledger sum")
}

// TestGenesisRoundTripPreservesPositionStatus proves an export and reimport
// puts every position back in the store it came from. Status is location now,
// so an import that misrouted a closed position would not merely mislabel it —
// recognition would credit history, and outstanding deployment would count a
// position that has already settled.
func (s *KeeperTestSuite) TestGenesisRoundTripPreservesPositionStatus() {
	committee := testAddress(1)
	destination := testAddress(2)

	s.SetupTest()
	open, closedID := s.openAndClosed(committee, destination)
	s.setPolicy(eligibility(testAsset, "1", "0.5"))
	s.stubRates(oracletypes.RateSet{testAsset: math.LegacyOneDec()})

	exported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().NoError(exported.Validate())
	s.Require().Equal(open, s.positionIDs(exported.OpenPositions))
	s.Require().Equal([]uint64{closedID}, s.positionIDs(exported.ClosedPositions))

	recognisedBefore := s.recognised()
	outstandingBefore, err := s.queryServer.Balance(s.ctx, &types.QueryBalanceRequest{})
	s.Require().NoError(err)

	// Reimport into a clean chain and ask the same questions. The custody
	// balance is Bank genesis rather than this module's, so it is restored
	// alongside rather than by the import.
	s.SetupTest()
	s.stubRates(oracletypes.RateSet{testAsset: math.LegacyOneDec()})
	s.fundReserve(10_000)
	s.Require().NoError(s.keeper.InitGenesis(s.ctx, exported))

	reexported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(open, s.positionIDs(reexported.OpenPositions))
	s.Require().Equal([]uint64{closedID}, s.positionIDs(reexported.ClosedPositions))

	// The figures the split exists to bound agree across the round trip.
	s.Require().Equal(recognisedBefore, s.recognised())
	outstandingAfter, err := s.queryServer.Balance(s.ctx, &types.QueryBalanceRequest{})
	s.Require().NoError(err)
	s.Require().Equal(outstandingBefore.OutstandingDeployed, outstandingAfter.OutstandingDeployed)
}

// TestGenesisRoundTripContinuesIdentifiers pins what the identifier sequences
// carry across an export: the next ID to issue, not the last one issued. Both
// neighbours export a genesis whose records are individually valid — an import
// restoring one short reissues a live identifier onto a position that already
// exists, and one long leaves a permanent gap. Neither is visible in the
// exported records themselves, only in the first act after the import, so the
// assertion has to be an act rather than a comparison of the two dumps.
func (s *KeeperTestSuite) TestGenesisRoundTripContinuesIdentifiers() {
	committee := testAddress(1)
	destination := testAddress(2)

	s.SetupTest()
	open, closed := s.openAndClosed(committee, destination)

	exported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().NoError(exported.Validate())

	highestPosition := closed
	for _, id := range open {
		if id > highestPosition {
			highestPosition = id
		}
	}
	// The ledger walks in key order, so the last entry carries the high ID.
	highestEntry := exported.Ledger[len(exported.Ledger)-1].EntryId
	s.Require().Equal(highestPosition+1, exported.NextPositionId)
	s.Require().Equal(highestEntry+1, exported.NextEntryId)

	s.SetupTest()
	s.fundReserve(10_000)
	s.Require().NoError(s.keeper.InitGenesis(s.ctx, exported))
	s.setBlockHeight(20)

	// The deployment the imported chain performs next continues both sequences
	// from where the exporting chain left them.
	s.Require().Equal(highestPosition+1, s.deploy(committee, destination, 100, 1, 0))

	reexported, err := s.keeper.ExportGenesis(s.ctx)
	s.Require().NoError(err)
	s.Require().NoError(reexported.Validate())
	s.Require().Equal(highestEntry+1, reexported.Ledger[len(exported.Ledger)].EntryId)
}

// wholeLedger collects every entry, for asserting that an act wrote none.
func (s *KeeperTestSuite) wholeLedger() []types.AccountingEntry {
	entries := make([]types.AccountingEntry, 0)
	s.Require().NoError(s.keeper.Ledger.Walk(s.ctx, nil, func(_ uint64, entry types.AccountingEntry) (bool, error) {
		entries = append(entries, entry)
		return false, nil
	}))
	return entries
}

// ledgerFor collects one position's entries in entry-ID order.
func (s *KeeperTestSuite) ledgerFor(positionID uint64) []types.AccountingEntry {
	entries := make([]types.AccountingEntry, 0)
	s.Require().NoError(s.keeper.Ledger.Walk(s.ctx, nil, func(_ uint64, entry types.AccountingEntry) (bool, error) {
		if entry.PositionId == positionID {
			entries = append(entries, entry)
		}
		return false, nil
	}))
	return entries
}
