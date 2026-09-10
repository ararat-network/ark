package ibc_test

import (
	"github.com/ararat-network/ark/tests/e2e/chainsuite"
)

// TestTransferRoundTrip sends NOAH from A to B and part of it back. The
// voucher arrives whole: the fee and the transfer tax come out of the
// sender beside the escrow, and the way back unescrows exactly what went.
func (s *HubSuite) TestTransferRoundTrip() {
	ctx := s.GetContext()
	senderBefore, err := s.ChainA.GetBalance(ctx, s.WalletA.FormattedAddress(), chainsuite.Denom)
	s.Require().NoError(err)

	amount := chainsuite.NOAH(5)
	voucher, err := chainsuite.Transfer(ctx, s.Relayer, s.ChainA, s.ChainB,
		s.WalletA.KeyName(), s.WalletB.FormattedAddress(), chainsuite.Denom, amount)
	s.Require().NoError(err)
	s.Require().NotEqual(chainsuite.Denom, voucher)

	senderAfter, err := s.ChainA.GetBalance(ctx, s.WalletA.FormattedAddress(), chainsuite.Denom)
	s.Require().NoError(err)
	s.Require().True(senderAfter.LT(senderBefore.Sub(amount)), "the sender paid no fee on the transfer")

	back := chainsuite.NOAH(2)
	arrivedAs, err := chainsuite.Transfer(ctx, s.Relayer, s.ChainB, s.ChainA,
		s.WalletB.KeyName(), s.WalletA.FormattedAddress(), voucher, back)
	s.Require().NoError(err)
	s.Require().Equal(chainsuite.Denom, arrivedAs)
	remaining, err := s.ChainB.GetBalance(ctx, s.WalletB.FormattedAddress(), voucher)
	s.Require().NoError(err)
	s.Require().Equal(amount.Sub(back).String(), remaining.String())
}
