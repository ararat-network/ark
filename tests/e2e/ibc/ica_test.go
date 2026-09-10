package ibc_test

import (
	"fmt"
	"time"

	"github.com/cosmos/interchaintest/v10/ibc"
	"github.com/cosmos/interchaintest/v10/testutil"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/ararat-network/ark/tests/e2e/chainsuite"
)

// TestInterchainAccountBankSend registers an interchain account on B from
// A, funds it, and has it send on B. The host allows only the bank send the
// opening proposal named.
func (s *HubSuite) TestInterchainAccountBankSend() {
	ctx := s.GetContext()
	channel, err := s.Relayer.GetTransferChannel(ctx, s.ChainA, s.ChainB)
	s.Require().NoError(err)
	connection := channel.ConnectionHops[0]
	owner := s.WalletA

	// The channel version is the ICS-27 metadata, spelled out: the register
	// command's empty default is refused on ark as unparseable metadata.
	end, err := s.ChainA.QueryJSON(ctx, "connection.counterparty.connection_id", "ibc", "connection", "end", connection)
	s.Require().NoError(err)
	version := fmt.Sprintf(
		`{"version":"ics27-1","controller_connection_id":%q,"host_connection_id":%q,"address":"","encoding":"proto3","tx_type":"sdk_multi_msg"}`,
		connection, end.String(),
	)
	_, err = s.ChainA.GetNode().ExecTx(ctx, owner.KeyName(),
		"interchain-accounts", "controller", "register", connection,
		"--ordering", "ORDER_ORDERED", "--version", version,
	)
	s.Require().NoError(err)

	var icaAddress string
	s.Require().NoError(testutil.WaitForCondition(3*time.Minute, 5*time.Second, func() (bool, error) {
		address, err := s.ChainA.QueryJSON(ctx, "address",
			"interchain-accounts", "controller", "interchain-account", owner.FormattedAddress(), connection)
		if err != nil {
			return false, nil //nolint:nilerr // the channel is still opening
		}
		icaAddress = address.String()
		return icaAddress != "", nil
	}), "the interchain account never opened")

	s.Require().NoError(s.ChainB.SendFunds(ctx, chainsuite.FaucetKeyName, ibc.WalletAmount{
		Address: icaAddress, Denom: chainsuite.Denom, Amount: chainsuite.NOAH(10),
	}))

	recipient := s.WalletB.FormattedAddress()
	before, err := s.ChainB.GetBalance(ctx, recipient, chainsuite.Denom)
	s.Require().NoError(err)
	amount := chainsuite.NOAH(1)
	msg := &banktypes.MsgSend{
		FromAddress: icaAddress,
		ToAddress:   recipient,
		Amount:      sdk.NewCoins(sdk.NewCoin(chainsuite.Denom, amount)),
	}
	registry := s.ChainA.Config().EncodingConfig.InterfaceRegistry
	_, err = s.ChainA.GetNode().SendICATx(ctx, owner.KeyName(), connection, registry, []sdk.Msg{msg}, "e2e", "proto3")
	s.Require().NoError(err)
	s.Require().NoError(chainsuite.WaitForBalance(ctx, s.ChainB, recipient, chainsuite.Denom, before.Add(amount), 2*time.Minute))

	// NOAH itself crosses untaxed: the execution tax the host's policy router
	// charges falls on the stable assets, and the relayer paid the gas. The
	// account is down by exactly what it sent.
	icaBalance, err := s.ChainB.GetBalance(ctx, icaAddress, chainsuite.Denom)
	s.Require().NoError(err)
	s.Require().Equal(chainsuite.NOAH(10).Sub(amount).String(), icaBalance.String())
}
