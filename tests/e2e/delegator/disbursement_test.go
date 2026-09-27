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

// DisbursementSuite opens a member tranche by governance, registers members, and replaces the
// committee while preserving the permanent member records.
type DisbursementSuite struct {
	*delegator.Suite
}

const (
	// memberGrantNoah keeps the tranche and its payments small against the artefact's pool.
	memberGrantNoah = 100
)

func (s *DisbursementSuite) TestMemberGrantThroughTranche() {
	ctx := s.GetContext()
	custody, err := sdk.Bech32ifyAddressBytes(chainsuite.Bech32Prefix, authtypes.NewModuleAddress("disbursement"))
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
			"activation_height": "1", "expiry_height": "1000000000",
		})
		s.Require().NoError(err)
		return message
	}

	// The tranche: three member grants opened from the artefact's member pool by vote.
	tranche := chainsuite.NOAH(3 * memberGrantNoah)
	open, err := json.Marshal(map[string]any{
		"@type": "/ark.disbursement.v1.MsgOpenTranche", "authority": gov, "pool": "POOL_MEMBERS", "amount": tranche.String(),
	})
	s.Require().NoError(err)
	held := s.Balance(custody)
	_, err = s.Chain.SubmitAndPassProposal(ctx, s.Wallet.KeyName(), "open the first tranche", open, updateParams, appoint(s.Wallet.FormattedAddress()))
	s.Require().NoError(err)
	s.Require().Equal(held, s.Balance(custody), "opening a tranche moves no coins")
	opened, err := s.Chain.QueryJSON(ctx, "member_pool.open", "disbursement", "balance", chainsuite.Denom)
	s.Require().NoError(err)
	s.Require().Equal(tranche.String(), opened.String())

	// A member with no account yet, and one whose address was dusted.
	member, err := s.Chain.BuildWallet(ctx, "member", "")
	s.Require().NoError(err)
	dusted := s.FundedWallet("dusted-member", 1)
	_, err = s.Node().ExecTx(ctx, s.Wallet.KeyName(), "disbursement", "committee-register", "--expected-term", "1", "--addresses", member.FormattedAddress(), "--addresses", dusted.FormattedAddress())
	s.Require().NoError(err)

	// The first period, a tenth, is sent at once into an ordinary account;
	// the rest waits in module custody.
	tenth := chainsuite.NOAH(memberGrantNoah / 10)
	account, err := s.Chain.QueryJSON(ctx, "account", "auth", "account", member.FormattedAddress())
	s.Require().NoError(err)
	s.Require().Equal("/cosmos.auth.v1beta1.BaseAccount", account.Get("type").String())
	s.Require().Equal(tenth, s.Balance(member.FormattedAddress()))
	s.Require().Equal(tenth.Add(chainsuite.NOAH(1)), s.Balance(dusted.FormattedAddress()), "an address holding coins is paid like any other")
	status, err := s.Chain.QueryJSON(ctx, "grant", "disbursement", "member", member.FormattedAddress())
	s.Require().NoError(err)
	s.Require().Equal(tenth.String(), status.Get("paid").String())
	s.Require().Equal(chainsuite.NOAH(memberGrantNoah).Sub(tenth).String(), status.Get("remaining").String())
	payable, err := s.Chain.QueryJSON(ctx, "amount.amount", "disbursement", "releasable", status.Get("id").String())
	s.Require().NoError(err)
	s.Require().Equal("0", payable.String(), "nothing has elapsed")
	s.Require().Equal(held.Sub(tenth.MulRaw(2)), s.Balance(custody), "only the first periods left")

	// The tenth is stake like any other, and pays its own fee.
	val := s.Chain.ValidatorWallets[0].ValoperAddress
	_, err = s.Node().ExecTx(ctx, member.KeyName(), "staking", "delegate", val, chainsuite.NOAHCoin(memberGrantNoah/20))
	s.Require().NoError(err)
	delegated, err := s.Chain.QueryJSON(ctx, "delegation_response.balance.amount", "staking", "delegation", member.FormattedAddress(), val)
	s.Require().NoError(err)
	s.Require().Equal(chainsuite.NOAH(memberGrantNoah/20).String(), delegated.String())

	// Governance replaces the committee; the old key and the old term are refused.
	_, err = s.Chain.SubmitAndPassProposal(ctx, s.Wallet.KeyName(), "replace the committee", appoint(s.Wallet2.FormattedAddress()))
	s.Require().NoError(err)
	another, err := s.Chain.BuildWallet(ctx, "member-2", "")
	s.Require().NoError(err)
	_, err = s.Node().ExecTx(ctx, s.Wallet.KeyName(), "disbursement", "committee-register", "--expected-term", "2", "--addresses", another.FormattedAddress())
	s.Require().Error(err)
	_, err = s.Node().ExecTx(ctx, s.Wallet2.KeyName(), "disbursement", "committee-register", "--expected-term", "1", "--addresses", another.FormattedAddress())
	s.Require().Error(err)
	_, err = s.Node().ExecTx(ctx, s.Wallet2.KeyName(), "disbursement", "committee-register", "--expected-term", "2", "--addresses", another.FormattedAddress())
	s.Require().NoError(err)
	s.Require().Equal(tenth, s.Balance(another.FormattedAddress()))
}

func TestDisbursement(t *testing.T) {
	s := &DisbursementSuite{Suite: &delegator.Suite{Suite: chainsuite.NewSuite(chainsuite.SuiteConfig{
		UpgradeOnSetup: true,
	})}}
	suite.Run(t, s)
}
