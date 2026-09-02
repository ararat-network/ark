package keeper_test

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	"github.com/ararat-network/ark/x/reserve/types"
)

// TestFeedReferents pins which Reserve state claims a feed and which does not.
// The guard is what forces the honest order of operations on governance: stop
// counting an asset, then stop pricing it.
func (s *KeeperTestSuite) TestFeedReferents() {
	committee := testAddress(1)
	destination := testAddress(2)

	s.Run("a credited entry claims the feed", func() {
		s.SetupTest()
		s.setPolicy(eligibility(testAsset, "0.5", "0.01"))

		referents, err := s.keeper.FeedReferents(s.ctx, testFeed)
		s.Require().NoError(err)
		s.Require().Len(referents, 1)
		s.Require().Equal(types.ModuleName, referents[0].Consumer)
		s.Require().Contains(referents[0].Referent, "recognition policy")
	})

	// Every stored entry grants credit — a zero factor is refused at the policy
	// write — so every entry on a series is its own claim, and two custodians
	// of one instrument are two blockers a removal proposal must answer.
	s.Run("each entry on a series claims the feed", func() {
		s.SetupTest()
		s.setPolicy(
			eligibility(testAsset, "0.5", "0.01"),
			eligibility(testFeed+"-cb", "1", "0.02"),
		)
		referents, err := s.keeper.FeedReferents(s.ctx, testFeed)
		s.Require().NoError(err)
		s.Require().Len(referents, 2)
	})

	// The guard walks the policy without testing GrantsCredit, so the state
	// validation forbids is pinned by seeding the collection directly. The
	// guard gates an irreversible-in-practice act, so it fails closed and
	// makes governance delist first.
	s.Run("an entry granting no credit still claims the feed", func() {
		s.SetupTest()
		for _, entry := range []types.EligibilityEntry{
			eligibility(testAsset, "0", "0.01"),
			eligibility(testAsset, "1", "0"),
		} {
			s.Require().Error(entry.Validate())
			s.Require().NoError(s.keeper.RecognitionPolicy.Set(s.ctx, entry.Denom, entry))

			referents, err := s.keeper.FeedReferents(s.ctx, testFeed)
			s.Require().NoError(err)
			s.Require().Len(referents, 1)
			s.Require().Contains(referents[0].Referent, "recognition policy")
		}
	})

	s.Run("an unlisted, unheld denom claims nothing", func() {
		s.SetupTest()

		referents, err := s.keeper.FeedReferents(s.ctx, chain.XDRBaseDenom)
		s.Require().NoError(err)
		s.Require().Empty(referents)
	})

	// An open position claims its series even with no eligibility entry: its
	// credit is zero either way, but removing the series forecloses ever listing
	// the exposure, since a policy entry requires an active feed.
	s.Run("an open external position claims the feed without any entry", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(1_000)
		s.deploy(committee, destination, 400, 4, 0)

		referents, err := s.keeper.FeedReferents(s.ctx, testFeed)
		s.Require().NoError(err)
		s.Require().Len(referents, 1)
		s.Require().Contains(referents[0].Referent, "1 open position(s)")
		s.Require().Contains(referents[0].Referent, "listed for recognition")
	})

	// Ark paper bought back through a position claims its own denomination
	// rather than a prefix, and for the other reason: paper can never be listed,
	// so what the series buys it is the sale path out of custody.
	s.Run("an open position in Ark paper claims its own denomination", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(1_000)
		s.registerAsset(chain.USDBaseDenom)
		s.expectDeploymentSend(destination, 400)
		_, err := s.msgServer.CommitteeDeploy(s.ctx, &types.MsgCommitteeDeploy{
			Committee:      committee,
			ExpectedTerm:   1,
			Destination:    destination,
			Amount:         noahCoin(400),
			Acquired:       sdk.NewInt64Coin(chain.USDBaseDenom, 4),
			VenueReference: "custodian-alpha",
			Reference:      "tx-0x01",
		})
		s.Require().NoError(err)

		referents, err := s.keeper.FeedReferents(s.ctx, chain.USDBaseDenom)
		s.Require().NoError(err)
		s.Require().Len(referents, 1)
		s.Require().Contains(referents[0].Referent, "1 open position(s)")
		s.Require().Contains(referents[0].Referent, "sold out through a deployment")
	})

	s.Run("both claims are reported together", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(1_000)
		s.deploy(committee, destination, 300, 3, 0)
		s.deploy(committee, destination, 200, 2, 0)
		s.setPolicy(eligibility(testAsset, "0.5", "0.01"))

		referents, err := s.keeper.FeedReferents(s.ctx, testFeed)
		s.Require().NoError(err)
		s.Require().Len(referents, 2)
		s.Require().Contains(referents[0].Referent, "recognition policy")
		s.Require().Contains(referents[1].Referent, "2 open position(s)")
	})

	// A closed position's recovery is already crystallised, so no later rate
	// read can change what the ledger says about it.
	s.Run("a closed position claims nothing", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(1_000)
		positionID := s.deploy(committee, destination, 400, 4, 0)
		_, err := s.msgServer.CommitteeClosePosition(s.ctx, &types.MsgCommitteeClosePosition{
			Committee: committee, ExpectedTerm: 1, PositionId: positionID,
		})
		s.Require().NoError(err)

		referents, err := s.keeper.FeedReferents(s.ctx, testFeed)
		s.Require().NoError(err)
		s.Require().Empty(referents)
	})

	// The guard answers for one denomination only: an open position in one
	// asset must not pin an unrelated feed.
	s.Run("claims are per denomination", func() {
		s.SetupTest()
		s.appointCommittee(committee, 1_000, 0, destination)
		s.setBlockHeight(20)
		s.fundReserve(1_000)
		s.deploy(committee, destination, 400, 4, 0)
		s.setPolicy(eligibility(testAsset, "0.5", "0.01"))

		referents, err := s.keeper.FeedReferents(s.ctx, chain.XDRBaseDenom)
		s.Require().NoError(err)
		s.Require().Empty(referents)
	})
}

// TestFeedReferentsSatisfiesTheGuard pins that the keeper still fits the
// interface app wiring registers it against, which no in-module call exercises.
func (s *KeeperTestSuite) TestFeedReferentsSatisfiesTheGuard() {
	s.SetupTest()

	var guard interface {
		FeedReferents(ctx context.Context, denom string) ([]oracletypes.FeedReferent, error)
	} = s.keeper
	referents, err := guard.FeedReferents(s.ctx, testAsset)
	s.Require().NoError(err)
	s.Require().Empty(referents)
}
