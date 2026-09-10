// Package delegator holds the suites a token holder's transactions run
// through the built arkd against a live network: bank, staking, governance,
// fee grants, authorisations, vesting, multisig, contracts, and the oracle.
package delegator

import (
	"github.com/cosmos/interchaintest/v10/chain/cosmos"
	"github.com/cosmos/interchaintest/v10/ibc"

	sdkmath "cosmossdk.io/math"

	"github.com/ararat-network/ark/tests/e2e/chainsuite"
)

// WalletFunds is each delegator wallet's balance in NOAH: enough to delegate
// past a validator's power and still pay for every transaction.
const WalletFunds = 5_000

// Suite is a chain with three funded wallets on its first node.
type Suite struct {
	*chainsuite.Suite
	Wallet  ibc.Wallet
	Wallet2 ibc.Wallet
	Wallet3 ibc.Wallet
}

func (s *Suite) SetupSuite() {
	s.Suite.SetupSuite()
	s.Wallet = s.FundedWallet("delegator", WalletFunds)
	s.Wallet2 = s.FundedWallet("delegator2", WalletFunds)
	s.Wallet3 = s.FundedWallet("delegator3", WalletFunds)
}

// FundedWallet creates a key on the first node and funds it from the faucet.
func (s *Suite) FundedWallet(name string, noah int64) ibc.Wallet {
	ctx := s.GetContext()
	wallet, err := s.Chain.BuildWallet(ctx, name, "")
	s.Require().NoError(err)
	s.Require().NoError(s.Chain.SendFunds(ctx, chainsuite.FaucetKeyName, ibc.WalletAmount{
		Address: wallet.FormattedAddress(),
		Denom:   chainsuite.Denom,
		Amount:  chainsuite.NOAH(noah),
	}))
	return wallet
}

// Node is the node every wallet's key lives on.
func (s *Suite) Node() *cosmos.ChainNode {
	return s.Chain.GetNode()
}

// Balance is address's NOAH balance.
func (s *Suite) Balance(address string) sdkmath.Int {
	balance, err := s.Chain.GetBalance(s.GetContext(), address, chainsuite.Denom)
	s.Require().NoError(err)
	return balance
}

// Send moves amount NOAH between wallets through the CLI and checks the
// recipient got exactly that: the fee and transfer tax ride on the sender.
func (s *Suite) Send(from ibc.Wallet, to string, amount sdkmath.Int, extraFlags ...string) {
	before := s.Balance(to)
	cmd := append([]string{"bank", "send", from.FormattedAddress(), to, amount.String() + chainsuite.Denom}, extraFlags...)
	_, err := s.Node().ExecTx(s.GetContext(), from.KeyName(), cmd...)
	s.Require().NoError(err)
	s.Require().Equal(before.Add(amount).String(), s.Balance(to).String(), "recipient balance")
}
