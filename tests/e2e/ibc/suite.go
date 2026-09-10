// Package ibc runs two ark chains under Hermes: the governance act that
// opens the hub, token transfers, interchain accounts, and a wasm light
// client stored through governance.
package ibc

import (
	"context"

	"github.com/cosmos/interchaintest/v10/ibc"
	"github.com/stretchr/testify/suite"

	"github.com/ararat-network/ark/tests/e2e/chainsuite"
)

const walletFunds = 1_000

// Suite is two ark chains, A and B, linked by Hermes over a transfer
// channel, with a funded wallet on each.
type Suite struct {
	suite.Suite
	Env     chainsuite.Environment
	ChainA  *chainsuite.Chain
	ChainB  *chainsuite.Chain
	Relayer *chainsuite.Relayer
	WalletA ibc.Wallet
	WalletB ibc.Wallet
	ctx     context.Context
}

func (s *Suite) SetupSuite() {
	s.Env = chainsuite.GetEnvironment()
	ctx, err := chainsuite.NewSuiteContext(&s.Suite)
	s.Require().NoError(err)
	s.ctx = ctx

	specA := chainsuite.DefaultChainSpec(s.Env)
	specA.ChainID = "ark-a"
	specB := chainsuite.DefaultChainSpec(s.Env)
	specB.Name, specB.ChainName, specB.ChainConfig.Name = "arkb", "arkb", "arkb"
	specB.ChainID = "ark-b"

	s.ChainA, s.ChainB, s.Relayer, err = chainsuite.CreateLinkedChains(ctx, s.T(), specA, specB,
		func(a, b *chainsuite.Chain) error {
			for _, chain := range []*chainsuite.Chain{a, b} {
				if err := chain.VerifyGasPrices(ctx); err != nil {
					return err
				}
				if err := chainsuite.OpenHub(ctx, chain); err != nil {
					return err
				}
			}
			return nil
		})
	s.Require().NoError(err)

	s.WalletA = s.fundedWallet(s.ChainA, "trader")
	s.WalletB = s.fundedWallet(s.ChainB, "trader")
}

func (s *Suite) GetContext() context.Context {
	s.Require().NotNil(s.ctx, "GetContext before SetupSuite ran")
	return s.ctx
}

func (s *Suite) fundedWallet(chain *chainsuite.Chain, name string) ibc.Wallet {
	wallet, err := chain.BuildWallet(s.GetContext(), name, "")
	s.Require().NoError(err)
	s.Require().NoError(chain.SendFunds(s.GetContext(), chainsuite.FaucetKeyName, ibc.WalletAmount{
		Address: wallet.FormattedAddress(),
		Denom:   chainsuite.Denom,
		Amount:  chainsuite.NOAH(walletFunds),
	}))
	return wallet
}
