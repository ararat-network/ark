package delegator_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/suite"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ararat-network/ark/tests/e2e/chainsuite"
	"github.com/ararat-network/ark/tests/e2e/delegator"
)

// DisbursementSuite opens a member tranche by governance, appoints a registrar through the
// committee, registers members, and rotates the registrar while preserving the permanent member
// records.
type DisbursementSuite struct {
	*delegator.Suite
}

const (
	// memberGrantNoah keeps the tranche and its payments small against the artefact's pool.
	memberGrantNoah = 100
	// memberHoldSeconds is the two-week floor every grant's first period keeps, longer than any suite
	// runs, so a chain here sees registration queue the tenth and never the block pay it (D126).
	memberHoldSeconds = 14 * 24 * 3600
)

func (s *DisbursementSuite) TestMemberGrantThroughTranche() {
	ctx := s.GetContext()
	members, err := sdk.Bech32ifyAddressBytes(chainsuite.Bech32Prefix, authtypes.NewModuleAddress("disbursement_members"))
	s.Require().NoError(err)
	gov, err := s.Chain.GovAuthority(ctx)
	s.Require().NoError(err)
	params, err := s.Chain.QueryJSON(ctx, "params", "disbursement", "params")
	s.Require().NoError(err)
	var settings map[string]any
	s.Require().NoError(json.Unmarshal([]byte(params.Raw), &settings))
	settings["member_amount"] = chainsuite.NOAH(memberGrantNoah).String()
	updateParams, err := json.Marshal(map[string]any{
		"@type": "/ark.disbursement.v1.MsgUpdateParams", "authority": gov, "params": settings,
	})
	s.Require().NoError(err)
	// appoint replaces the committee for the rest of the chain's life; every appointment advances
	// the term the committee's transactions must carry.
	appoint := func(committee string) json.RawMessage {
		message, err := json.Marshal(map[string]any{
			"@type": "/ark.disbursement.v1.MsgSetGrantsMandate", "authority": gov, "committee": committee,
			"activation_time": "1", "expiry_time": "4102444800",
		})
		s.Require().NoError(err)
		return message
	}

	// The tranche: three member grants opened from the artefact's member account by vote.
	tranche := chainsuite.NOAH(3 * memberGrantNoah)
	open, err := json.Marshal(map[string]any{
		"@type": "/ark.disbursement.v1.MsgOpenTranche", "authority": gov, "amount": tranche.String(),
	})
	s.Require().NoError(err)
	held := s.Balance(members)
	_, err = s.Chain.SubmitAndPassProposal(ctx, s.Wallet.KeyName(), "open the first tranche", open, updateParams, appoint(s.Wallet.FormattedAddress()))
	s.Require().NoError(err)
	s.Require().Equal(held, s.Balance(members), "opening a tranche moves no coins")
	opened, err := s.Chain.QueryJSON(ctx, "member_tranche", "disbursement", "balance", chainsuite.Denom)
	s.Require().NoError(err)
	s.Require().Equal(tranche.String(), opened.String())

	// The committee appoints the registrar, a hot key bounded by the open tranche alone; every
	// appointment advances the registrar term its batches must carry.
	registrar := s.FundedWallet("registrar", 10)
	_, err = s.Node().ExecTx(ctx, s.Wallet.KeyName(), "disbursement", "committee-registrar", "--expected-term", "1", "--registrar", registrar.FormattedAddress(), "--activation-time", "1", "--expiry-time", "4102444800")
	s.Require().NoError(err)

	// A member with no account yet, and one whose address was dusted.
	member, err := s.Chain.BuildWallet(ctx, "member", "")
	s.Require().NoError(err)
	dusted := s.FundedWallet("dusted-member", 1)
	_, err = s.Node().ExecTx(ctx, s.Wallet.KeyName(), "disbursement", "register-members", "--reference", "admission batch", "--expected-term", "1", "--addresses", member.FormattedAddress())
	s.Require().ErrorContains(err, notAppointed("registrar"), "the committee is not the registrar")
	_, err = s.Node().ExecTx(ctx, registrar.KeyName(), "disbursement", "register-members", "--reference", "admission batch", "--expected-term", "1", "--addresses", member.FormattedAddress(), "--addresses", dusted.FormattedAddress())
	s.Require().NoError(err)

	// Registration pays nothing: the whole grant waits in module custody and the tenth is queued for
	// the end of the two-week hold, into a fresh address or a dusted one alike (D126).
	s.Require().True(s.Balance(member.FormattedAddress()).IsZero(), "the tenth waits for the hold")
	s.Require().Equal(chainsuite.NOAH(1), s.Balance(dusted.FormattedAddress()), "an address holding coins waits like any other")
	status, err := s.Chain.QueryJSON(ctx, "grant", "disbursement", "member", member.FormattedAddress())
	s.Require().NoError(err)
	s.Require().Equal("0", status.Get("paid").String())
	s.Require().Equal(chainsuite.NOAH(memberGrantNoah).String(), status.Get("remaining").String())
	s.Require().Equal(status.Get("created_time").Uint()+memberHoldSeconds, status.Get("due_time").Uint(), "queued for the end of the hold")
	s.Require().Equal(held, s.Balance(members), "nothing has left the member account")

	// Governance replaces the committee, which clears the registrar it appointed; the new committee
	// names its own, and the old key and the old registrar terms are refused.
	_, err = s.Chain.SubmitAndPassProposal(ctx, s.Wallet.KeyName(), "replace the committee", appoint(s.Wallet2.FormattedAddress()))
	s.Require().NoError(err)
	another, err := s.Chain.BuildWallet(ctx, "member-2", "")
	s.Require().NoError(err)
	_, err = s.Node().ExecTx(ctx, s.Wallet.KeyName(), "disbursement", "committee-registrar", "--expected-term", "2", "--registrar", s.Wallet2.FormattedAddress(), "--activation-time", "1", "--expiry-time", "4102444800")
	s.Require().ErrorContains(err, notAppointed("grants"), "the replaced committee cannot rotate the registrar")
	another2, err := s.Chain.BuildWallet(ctx, "member-3", "")
	s.Require().NoError(err)
	_, err = s.Node().ExecTx(ctx, registrar.KeyName(), "disbursement", "register-members", "--reference", "admission batch", "--expected-term", "1", "--addresses", another2.FormattedAddress())
	s.Require().ErrorContains(err, notAppointed("registrar"), "the replacement cleared the registrar")
	// The registrar term is three: the first appointment, the clearing, and this appointment.
	_, err = s.Node().ExecTx(ctx, s.Wallet2.KeyName(), "disbursement", "committee-registrar", "--expected-term", "2", "--registrar", s.Wallet2.FormattedAddress(), "--activation-time", "1", "--expiry-time", "4102444800")
	s.Require().NoError(err)
	_, err = s.Node().ExecTx(ctx, registrar.KeyName(), "disbursement", "register-members", "--reference", "admission batch", "--expected-term", "2", "--addresses", another.FormattedAddress())
	s.Require().ErrorContains(err, notAppointed("registrar"), "the old registrar was replaced")
	_, err = s.Node().ExecTx(ctx, s.Wallet2.KeyName(), "disbursement", "register-members", "--reference", "admission batch", "--expected-term", "1", "--addresses", another.FormattedAddress())
	s.Require().ErrorContains(err, "registrar mandate: term mismatch", "the registrar term moved on")
	_, err = s.Node().ExecTx(ctx, s.Wallet2.KeyName(), "disbursement", "register-members", "--reference", "admission batch", "--expected-term", "3", "--addresses", another.FormattedAddress())
	s.Require().NoError(err)
	s.Require().True(s.Balance(another.FormattedAddress()).IsZero(), "the new registrar's member waits out the hold too")
}

// notAppointed is the refusal a mandate gives a signer it did not appoint.
func notAppointed(mandate string) string {
	return mandate + " mandate: signer is not the exact appointed account"
}

func TestDisbursement(t *testing.T) {
	s := &DisbursementSuite{Suite: &delegator.Suite{Suite: chainsuite.NewSuite(chainsuite.SuiteConfig{
		UpgradeOnSetup: true,
	})}}
	suite.Run(t, s)
}
