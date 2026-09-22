package delegator_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/cosmos/interchaintest/v10/chain/cosmos"
	"github.com/cosmos/interchaintest/v10/testutil"
	"github.com/stretchr/testify/suite"
	"github.com/tidwall/sjson"

	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"github.com/ararat-network/ark/tests/e2e/chainsuite"
	"github.com/ararat-network/ark/tests/e2e/delegator"
)

// GovSuite drives proposals through deposit, vote, and tally on a live chain.
type GovSuite struct {
	*delegator.Suite
}

// TestParamChangeThroughDepositAndVote takes a proposal from an initial
// deposit through a top-up, the validators' votes, and the tally, and reads
// the changed parameter back.
func (s *GovSuite) TestParamChangeThroughDepositAndVote() {
	ctx := s.GetContext()
	gov, err := s.Chain.GovAuthority(ctx)
	s.Require().NoError(err)
	params, err := s.Chain.QueryJSON(ctx, "params", "gov", "params")
	s.Require().NoError(err)
	updated, err := sjson.Set(params.Raw, "voting_period", "45s")
	s.Require().NoError(err)
	msg := json.RawMessage(fmt.Sprintf(`{"@type":"/cosmos.gov.v1.MsgUpdateParams","authority":%q,"params":%s}`, gov, updated))

	// A tenth of the minimum deposit opens the deposit period.
	id, err := s.Chain.SubmitProposal(ctx, s.Wallet.KeyName(), "raise the voting period", chainsuite.NOAHCoin(chainsuite.GovDeposit/10), false, msg)
	s.Require().NoError(err)
	status, err := s.Chain.QueryJSON(ctx, "proposal.status", "gov", "proposal", id)
	s.Require().NoError(err)
	s.Require().Equal("PROPOSAL_STATUS_DEPOSIT_PERIOD", status.String())

	_, err = s.Node().ExecTx(ctx, s.Wallet2.KeyName(), "gov", "deposit", id, chainsuite.NOAHCoin(chainsuite.GovDeposit-chainsuite.GovDeposit/10))
	s.Require().NoError(err)
	s.Require().NoError(s.Chain.WaitForProposalStatus(ctx, id, govv1.StatusVotingPeriod))
	s.Require().NoError(s.Chain.PassProposal(ctx, id))

	period, err := s.Chain.QueryJSON(ctx, "params.voting_period", "gov", "params")
	s.Require().NoError(err)
	s.Require().Equal("45s", period.String())
}

// TestSoftwareUpgradeCancelled schedules an upgrade the binary does not
// carry, cancels it before its height, and sees the chain pass that height.
func (s *GovSuite) TestSoftwareUpgradeCancelled() {
	ctx := s.GetContext()
	height, err := s.Chain.Height(ctx)
	s.Require().NoError(err)
	// Two proposals have to pass before this height.
	haltHeight := height + 80

	upgrade, err := s.Chain.UpgradeProposal(ctx, s.Wallet.KeyName(), cosmos.SoftwareUpgradeProposal{
		Deposit:     chainsuite.NOAHCoin(chainsuite.GovDeposit),
		Title:       "an upgrade nobody ships",
		Name:        "e2e-cancelled",
		Description: "cancelled before its height",
		Height:      haltHeight,
	})
	s.Require().NoError(err)
	s.Require().NoError(s.Chain.PassProposal(ctx, upgrade.ProposalID))
	plan, err := s.Chain.UpgradeQueryPlan(ctx)
	s.Require().NoError(err)
	s.Require().NotNil(plan)
	s.Require().Equal("e2e-cancelled", plan.Name)

	gov, err := s.Chain.GovAuthority(ctx)
	s.Require().NoError(err)
	cancel := json.RawMessage(fmt.Sprintf(`{"@type":"/cosmos.upgrade.v1beta1.MsgCancelUpgrade","authority":%q}`, gov))
	_, err = s.Chain.SubmitAndPassProposal(ctx, s.Wallet.KeyName(), "cancel the upgrade", cancel)
	s.Require().NoError(err)
	plan, err = s.Chain.UpgradeQueryPlan(ctx)
	s.Require().NoError(err)
	s.Require().Nil(plan, "the plan survived its cancellation")

	current, err := s.Chain.Height(ctx)
	s.Require().NoError(err)
	s.Require().Less(current, haltHeight, "the proposals took longer than the upgrade delay; raise it")
	s.Require().NoError(testutil.WaitForBlocks(ctx, int(haltHeight-current)+2, s.Chain))
}

// TestExpeditedProposalFallsBack is the SDK rule that an expedited proposal
// failing its threshold becomes a regular one and finishes as that.
func (s *GovSuite) TestExpeditedProposalFallsBack() {
	ctx := s.GetContext()
	id, err := s.Chain.SubmitProposal(ctx, s.Wallet.KeyName(), "expedited text", chainsuite.NOAHCoin(chainsuite.GovExpeditedDeposit), true)
	s.Require().NoError(err)
	proposal, err := s.Chain.QueryJSON(ctx, "proposal", "gov", "proposal", id)
	s.Require().NoError(err)
	s.Require().True(proposal.Get("expedited").Bool())
	s.Require().Equal("PROPOSAL_STATUS_VOTING_PERIOD", proposal.Get("status").String())

	s.Require().NoError(s.Node().VoteOnProposal(ctx, chainsuite.ValidatorMoniker, mustUint(id), "no"))
	s.Require().NoError(testutil.WaitForCondition(chainsuite.GovExpeditedVotingPeriod+30*time.Second, chainsuite.BlockTime, func() (bool, error) {
		proposal, err := s.Chain.QueryJSON(ctx, "proposal", "gov", "proposal", id)
		if err != nil {
			return false, err
		}
		return !proposal.Get("expedited").Bool(), nil
	}), "the proposal stayed expedited past its window")
	s.Require().NoError(s.Chain.WaitForProposalStatus(ctx, id, govv1.StatusRejected))
}

func (s *GovSuite) TestCommunityPoolSpend() {
	ctx := s.GetContext()
	_, err := s.Node().ExecTx(ctx, s.Wallet.KeyName(), "distribution", "fund-community-pool", chainsuite.NOAHCoin(10))
	s.Require().NoError(err)

	gov, err := s.Chain.GovAuthority(ctx)
	s.Require().NoError(err)
	recipient := s.Wallet3.FormattedAddress()
	amount := chainsuite.NOAH(5)
	spend := json.RawMessage(fmt.Sprintf(
		`{"@type":"/cosmos.distribution.v1beta1.MsgCommunityPoolSpend","authority":%q,"recipient":%q,"amount":[{"denom":%q,"amount":%q}]}`,
		gov, recipient, chainsuite.Denom, amount.String(),
	))
	before := s.Balance(recipient)
	_, err = s.Chain.SubmitAndPassProposal(ctx, s.Wallet.KeyName(), "spend from the community pool", spend)
	s.Require().NoError(err)
	s.Require().Equal(before.Add(amount).String(), s.Balance(recipient).String())
}

func mustUint(id string) uint64 {
	var n uint64
	if _, err := fmt.Sscanf(id, "%d", &n); err != nil {
		panic(err)
	}
	return n
}

func TestGov(t *testing.T) {
	s := &GovSuite{Suite: &delegator.Suite{Suite: chainsuite.NewSuite(chainsuite.SuiteConfig{
		UpgradeOnSetup: true,
	})}}
	suite.Run(t, s)
}
