package delegator_test

import (
	"path"
	"testing"
	"time"

	"github.com/cosmos/interchaintest/v10"
	"github.com/cosmos/interchaintest/v10/testutil"
	"github.com/stretchr/testify/suite"

	"github.com/ararat-network/ark/tests/e2e/chainsuite"
	"github.com/ararat-network/ark/tests/e2e/delegator"
)

// StakingSuite delegates on a four-validator chain with a short unbonding
// time, and checks that a power shift keeps every validator's oracle vote.
type StakingSuite struct {
	*delegator.Suite
}

func (s *StakingSuite) TestDelegateWithdrawUnbond() {
	ctx := s.GetContext()
	node := s.Node()
	val := s.Chain.ValidatorWallets[1].ValoperAddress
	wallet := s.Wallet

	_, err := node.ExecTx(ctx, wallet.KeyName(), "staking", "delegate", val, chainsuite.NOAHCoin(50))
	s.Require().NoError(err)
	delegation, err := s.Chain.StakingQueryDelegation(ctx, val, wallet.FormattedAddress())
	s.Require().NoError(err)
	s.Require().Equal(chainsuite.NOAH(50).String(), delegation.Balance.Amount.String())

	s.Require().NoError(testutil.WaitForBlocks(ctx, 3, s.Chain))
	_, err = node.ExecTx(ctx, wallet.KeyName(), "distribution", "withdraw-rewards", val)
	s.Require().NoError(err)

	_, err = node.ExecTx(ctx, wallet.KeyName(), "staking", "unbond", val, chainsuite.NOAHCoin(20))
	s.Require().NoError(err)
	unbonding, err := s.Chain.StakingQueryUnbondingDelegation(ctx, wallet.FormattedAddress(), val)
	s.Require().NoError(err)
	s.Require().Len(unbonding.Entries, 1)
	s.Require().Equal(chainsuite.NOAH(20).String(), unbonding.Entries[0].Balance.String())

	balanceBefore := s.Balance(wallet.FormattedAddress())
	s.Require().NoError(testutil.WaitForCondition(chainsuite.ShortUnbondingTime+30*time.Second, chainsuite.BlockTime, func() (bool, error) {
		_, err := s.Chain.StakingQueryUnbondingDelegation(ctx, wallet.FormattedAddress(), val)
		return err != nil, nil
	}), "unbonding never completed")
	s.Require().Equal(balanceBefore.Add(chainsuite.NOAH(20)).String(), s.Balance(wallet.FormattedAddress()).String())

	delegation, err = s.Chain.StakingQueryDelegation(ctx, val, wallet.FormattedAddress())
	s.Require().NoError(err)
	s.Require().Equal(chainsuite.NOAH(30).String(), delegation.Balance.Amount.String())
}

func (s *StakingSuite) TestRedelegate() {
	ctx := s.GetContext()
	src := s.Chain.ValidatorWallets[1].ValoperAddress
	dst := s.Chain.ValidatorWallets[2].ValoperAddress
	wallet := s.Wallet2

	_, err := s.Node().ExecTx(ctx, wallet.KeyName(), "staking", "delegate", src, chainsuite.NOAHCoin(30))
	s.Require().NoError(err)
	_, err = s.Node().ExecTx(ctx, wallet.KeyName(), "staking", "redelegate", src, dst, chainsuite.NOAHCoin(10))
	s.Require().NoError(err)

	delegation, err := s.Chain.StakingQueryDelegation(ctx, dst, wallet.FormattedAddress())
	s.Require().NoError(err)
	s.Require().Equal(chainsuite.NOAH(10).String(), delegation.Balance.Amount.String())
	delegation, err = s.Chain.StakingQueryDelegation(ctx, src, wallet.FormattedAddress())
	s.Require().NoError(err)
	s.Require().Equal(chainsuite.NOAH(20).String(), delegation.Balance.Amount.String())
}

func (s *StakingSuite) TestAuthzDelegate() {
	ctx := s.GetContext()
	node := s.Node()
	val := s.Chain.ValidatorWallets[1].ValoperAddress
	granter, grantee := s.Wallet3, s.Wallet2

	_, err := node.ExecTx(ctx, granter.KeyName(),
		"authz", "grant", grantee.FormattedAddress(), "delegate",
		"--allowed-validators", val, "--spend-limit", chainsuite.NOAHCoin(10),
	)
	s.Require().NoError(err)
	nested, err := s.Chain.GenerateTx(ctx, 0,
		"staking", "delegate", val, chainsuite.NOAHCoin(5), "--from", granter.FormattedAddress(),
		"--gas", "200000", "--fees", chainsuite.NOAHCoin(1),
	)
	s.Require().NoError(err)
	s.Require().NoError(node.WriteFile(ctx, []byte(nested), "authz-delegate.json"))
	_, err = node.ExecTx(ctx, grantee.KeyName(), "authz", "exec", path.Join(node.HomeDir(), "authz-delegate.json"))
	s.Require().NoError(err)

	delegation, err := s.Chain.StakingQueryDelegation(ctx, val, granter.FormattedAddress())
	s.Require().NoError(err)
	s.Require().Equal(chainsuite.NOAH(5).String(), delegation.Balance.Amount.String())
}

// TestPowerShiftKeepsOracleVotes delegates past the largest validator and
// checks the consensus set follows and every validator's rates still land in
// the extended commit, at the new power.
func (s *StakingSuite) TestPowerShiftKeepsOracleVotes() {
	if !s.Env.PriceFeed {
		s.T().Skip("TEST_PRICEFEED is off; validators vote no rates")
	}
	ctx := s.GetContext()
	s.Require().NoError(s.Chain.WaitForExchangeRate(ctx, "ausd", 4*time.Minute))

	votes, err := s.Chain.VoteExtensions(ctx, 0)
	s.Require().NoError(err)
	s.Require().Len(votes, len(s.Chain.Validators))
	for _, vote := range votes {
		s.Require().NotEmpty(vote.Rates, "validator %s reported no rates", vote.ValidatorAddress)
	}

	hexAddr, err := s.Chain.GetValidatorHex(ctx, 3)
	s.Require().NoError(err)
	before, err := s.Chain.GetValidatorPower(ctx, hexAddr)
	s.Require().NoError(err)

	// Past validator 0's 3000 NOAH self-delegation.
	_, err = s.Node().ExecTx(ctx, s.Wallet.KeyName(), "staking", "delegate", s.Chain.ValidatorWallets[3].ValoperAddress, chainsuite.NOAHCoin(2600))
	s.Require().NoError(err)
	s.Require().NoError(testutil.WaitForBlocks(ctx, 3, s.Chain))

	after, err := s.Chain.GetValidatorPower(ctx, hexAddr)
	s.Require().NoError(err)
	s.Require().Greater(after, before)

	votes, err = s.Chain.VoteExtensions(ctx, 0)
	s.Require().NoError(err)
	s.Require().Len(votes, len(s.Chain.Validators))
	var top int64
	for _, vote := range votes {
		s.Require().NotEmpty(vote.Rates, "validator %s reported no rates after the shift", vote.ValidatorAddress)
		top = max(top, vote.ValidatorPower)
	}
	s.Require().Equal(after, top, "the extended commit does not carry the new power")
}

func TestStaking(t *testing.T) {
	s := &StakingSuite{Suite: &delegator.Suite{Suite: chainsuite.NewSuite(chainsuite.SuiteConfig{
		UpgradeOnSetup:   true,
		GenesisOverrides: chainsuite.ShortUnbondingGenesis(),
		ChainSpec: &interchaintest.ChainSpec{
			NumValidators: &chainsuite.FourValidators,
		},
	})}}
	suite.Run(t, s)
}
