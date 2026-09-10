package ibc_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/ararat-network/ark/tests/e2e/chainsuite"
	"github.com/ararat-network/ark/tests/e2e/ibc"
)

// HubSuite is every IBC scenario on one pair of chains.
type HubSuite struct {
	ibc.Suite
}

// TestHubOpenedThroughGovernance reads back the parameters the setup's
// proposal set, and the client, connection, and channel Hermes could only
// build once they were.
func (s *HubSuite) TestHubOpenedThroughGovernance() {
	ctx := s.GetContext()
	for _, chain := range []*chainsuite.Chain{s.ChainA, s.ChainB} {
		allowed, err := chain.QueryJSON(ctx, "allowed_clients", "ibc", "client", "params")
		s.Require().NoError(err)
		s.Require().Contains(allowed.Raw, "07-tendermint")
		s.Require().Contains(allowed.Raw, "08-wasm")

		transfer, err := chain.QueryJSON(ctx, "@this", "ibc-transfer", "params")
		s.Require().NoError(err)
		s.Require().True(transfer.Get("send_enabled").Bool())
		s.Require().True(transfer.Get("receive_enabled").Bool())

		host, err := chain.QueryJSON(ctx, "@this", "interchain-accounts", "host", "params")
		s.Require().NoError(err)
		s.Require().True(host.Get("host_enabled").Bool())
		s.Require().Contains(host.Get("allow_messages").Raw, "/cosmos.bank.v1beta1.MsgSend")

		clients, err := chain.QueryJSON(ctx, "client_states", "ibc", "client", "states")
		s.Require().NoError(err)
		s.Require().NotEmpty(clients.Array(), "no client on %s", chain.Config().ChainID)
	}
	channel, err := s.Relayer.GetTransferChannel(ctx, s.ChainA, s.ChainB)
	s.Require().NoError(err)
	s.Require().Equal("STATE_OPEN", channel.State)
}

func TestHub(t *testing.T) {
	suite.Run(t, new(HubSuite))
}
