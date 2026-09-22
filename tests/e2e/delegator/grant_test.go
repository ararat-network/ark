package delegator_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/ararat-network/ark/tests/e2e/chainsuite"
	"github.com/ararat-network/ark/tests/e2e/delegator"
)

// GrantSuite drives the grant contract the way the distribution plan's first
// tranche would: stored and instantiated with no admin, funded by a community
// pool spend proposal, a member registered by the registrar, and the
// registrar replaced by sudo.
type GrantSuite struct {
	*delegator.Suite
}

const (
	// memberGrantNoah is small so the delegator's wallet can fund the tranche.
	memberGrantNoah  = 100
	feeAllowanceNoah = 1
)

func (s *GrantSuite) TestMemberGrantThroughTranche() {
	ctx := s.GetContext()
	codeID, err := s.Chain.StoreContract(ctx, s.Wallet.KeyName(), "../../../app/testdata/grant.wasm")
	s.Require().NoError(err)

	schedule := []string{`{"length":31536000,"parts":12}`}
	for range 36 {
		schedule = append(schedule, `{"length":2628000,"parts":1}`)
	}
	init := fmt.Sprintf(
		`{"denom":%q,"registrar":%q,"issuance_limit":{"max_members":1000,"window_seconds":604800},`+
			`"member_grant":%q,"member_schedule":[%s],"fee_allowance":%q,`+
			`"founding_stake":%q,"seat_stake":%q,"cap":{"numerator":1,"denominator":5,"ceiling":%q,"unit":"1"}}`,
		chainsuite.Denom, s.Wallet.FormattedAddress(), chainsuite.NOAH(memberGrantNoah), strings.Join(schedule, ","),
		chainsuite.NOAH(feeAllowanceNoah), chainsuite.NOAH(50_000_000), chainsuite.NOAH(5_000_000), chainsuite.NOAH(60_000_000),
	)
	_, err = s.Node().ExecTx(ctx, s.Wallet.KeyName(), "wasm", "instantiate", codeID, init, "--label", "grant", "--no-admin")
	s.Require().NoError(err)
	contracts, err := s.Chain.QueryJSON(ctx, "contracts", "wasm", "list-contract-by-code", codeID)
	s.Require().NoError(err)
	s.Require().Len(contracts.Array(), 1, contracts.Raw)
	contract := contracts.Array()[0].String()

	// The tranche: three member grants, spent from the community pool by vote.
	tranche := chainsuite.NOAH(3 * memberGrantNoah)
	_, err = s.Node().ExecTx(ctx, s.Wallet.KeyName(), "distribution", "fund-community-pool", chainsuite.NOAHCoin(3*memberGrantNoah))
	s.Require().NoError(err)
	gov, err := s.Chain.GovAuthority(ctx)
	s.Require().NoError(err)
	spend := json.RawMessage(fmt.Sprintf(
		`{"@type":"/cosmos.distribution.v1beta1.MsgCommunityPoolSpend","authority":%q,"recipient":%q,"amount":[{"denom":%q,"amount":%q}]}`,
		gov, contract, chainsuite.Denom, tranche,
	))
	_, err = s.Chain.SubmitAndPassProposal(ctx, s.Wallet.KeyName(), "fund the first tranche", spend)
	s.Require().NoError(err)
	s.Require().Equal(tranche, s.Balance(contract))

	// A member with no account yet, and one whose address was dusted.
	member, err := s.Chain.BuildWallet(ctx, "member", "")
	s.Require().NoError(err)
	dusted := s.FundedWallet("dusted-member", 1)
	register := fmt.Sprintf(`{"register_members":{"addresses":[%q,%q]}}`, member.FormattedAddress(), dusted.FormattedAddress())
	_, err = s.Node().ExecTx(ctx, s.Wallet.KeyName(), "wasm", "execute", contract, register)
	s.Require().NoError(err)

	grant := chainsuite.NOAH(memberGrantNoah)
	account, err := s.Chain.QueryJSON(ctx, "account", "auth", "account", member.FormattedAddress())
	s.Require().NoError(err)
	s.Require().Equal("/cosmos.vesting.v1beta1.PeriodicVestingAccount", account.Get("type").String())
	s.Require().Equal(grant.String(), account.Get("value.base_vesting_account.original_vesting.0.amount").String())
	s.Require().Len(account.Get("value.vesting_periods").Array(), 37)
	s.Require().Equal(grant, s.Balance(member.FormattedAddress()))

	account, err = s.Chain.QueryJSON(ctx, "account", "auth", "account", dusted.FormattedAddress())
	s.Require().NoError(err)
	s.Require().Equal("/cosmos.auth.v1beta1.BaseAccount", account.Get("type").String(), "the dusted address was left alone")
	var status struct {
		Data struct {
			Status map[string]json.RawMessage `json:"status"`
		} `json:"data"`
	}
	s.Require().NoError(s.Chain.QueryContract(ctx, contract, fmt.Sprintf(`{"member":{"address":%q}}`, dusted.FormattedAddress()), &status))
	s.Require().Contains(status.Data.Status, "rejected")
	s.Require().Equal(chainsuite.NOAH(2*memberGrantNoah), s.Balance(contract), "the rejected grant stayed in the tranche")

	// The unvested grant delegates, with the contract's allowance paying the fee.
	val := s.Chain.ValidatorWallets[0].ValoperAddress
	_, err = s.Node().ExecTx(ctx, member.KeyName(), "staking", "delegate", val, chainsuite.NOAHCoin(memberGrantNoah/2), "--fee-granter", contract)
	s.Require().NoError(err)
	delegated, err := s.Chain.QueryJSON(ctx, "delegation_response.balance.amount", "staking", "delegation", member.FormattedAddress(), val)
	s.Require().NoError(err)
	s.Require().Equal(chainsuite.NOAH(memberGrantNoah/2).String(), delegated.String())

	// Governance replaces the registrar by sudo; the old key is refused.
	sudo := json.RawMessage(fmt.Sprintf(
		`{"@type":"/cosmwasm.wasm.v1.MsgSudoContract","authority":%q,"contract":%q,"msg":{"set_registrar":{"registrar":%q}}}`,
		gov, contract, s.Wallet2.FormattedAddress(),
	))
	_, err = s.Chain.SubmitAndPassProposal(ctx, s.Wallet.KeyName(), "replace the registrar", sudo)
	s.Require().NoError(err)
	another, err := s.Chain.BuildWallet(ctx, "member-2", "")
	s.Require().NoError(err)
	register = fmt.Sprintf(`{"register_members":{"addresses":[%q]}}`, another.FormattedAddress())
	_, err = s.Node().ExecTx(ctx, s.Wallet.KeyName(), "wasm", "execute", contract, register)
	s.Require().Error(err)
	_, err = s.Node().ExecTx(ctx, s.Wallet2.KeyName(), "wasm", "execute", contract, register)
	s.Require().NoError(err)
	s.Require().Equal(grant, s.Balance(another.FormattedAddress()))
}

func TestGrant(t *testing.T) {
	s := &GrantSuite{Suite: &delegator.Suite{Suite: chainsuite.NewSuite(chainsuite.SuiteConfig{
		UpgradeOnSetup: true,
	})}}
	suite.Run(t, s)
}
