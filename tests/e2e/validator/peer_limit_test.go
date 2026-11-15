// SPDX-License-Identifier: Apache-2.0
// Adapted from Gaia, tests/interchain/validator/config_test.go.
// Modified for Ark: the inbound peer limit, with every connection requested.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package validator_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/cosmos/interchaintest/v10"
	"github.com/cosmos/interchaintest/v10/ibc"
	"github.com/cosmos/interchaintest/v10/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"github.com/ararat-network/ark/tests/e2e/chainsuite"
)

const cometRPCPort = "26657"

type PeerLimitSuite struct {
	*chainsuite.Suite
}

func TestPeerLimit(t *testing.T) {
	// Unsafe RPC serves dial_peers: the test asks for each connection it
	// counts, where a restarted mesh re-forms on no schedule.
	configToml := chainsuite.DefaultConfigToml()
	configToml["rpc"] = testutil.Toml{"unsafe": true}
	s := &PeerLimitSuite{
		Suite: chainsuite.NewSuite(chainsuite.SuiteConfig{
			UpgradeOnSetup: true,
			ChainSpec: &interchaintest.ChainSpec{
				NumValidators: &chainsuite.FourValidators,
				ChainConfig: ibc.ChainConfig{ConfigFileOverrides: map[string]any{
					"config/config.toml": configToml,
					"config/app.toml":    chainsuite.DefaultAppToml(),
				}},
			},
		}),
	}
	suite.Run(t, s)
}

// TestInboundCap pins max_num_inbound_peers: capped at one with every other
// validator dialling it, validator 0 holds one inbound peer.
func (s *PeerLimitSuite) TestInboundCap() {
	addrs := strings.Split(s.Chain.Nodes().PeerString(s.GetContext()), ",")
	s.Require().Len(addrs, len(s.Chain.Validators))
	target := addrs[0]

	// Control: uncapped, validator 0 holds all three at once.
	s.Require().EventuallyWithT(func(c *assert.CollectT) {
		assert.NoError(c, s.dialAll(target))
		total, _, err := s.peerCounts(0)
		assert.NoError(c, err)
		assert.Equal(c, len(addrs)-1, total)
	}, time.Minute, 5*time.Second)

	// Validator 0 restarts alone and syncs through validator 1, the only peer
	// it dials. Without PEX it dials no other, so the rest reach it inbound
	// or not at all.
	p2p := testutil.Toml{
		"max_num_inbound_peers": 1,
		"pex":                   false,
		"persistent_peers":      addrs[1],
	}
	s.Require().NoError(s.Chain.ModifyConfig(
		s.GetContext(), s.T(),
		map[string]testutil.Toml{"config/config.toml": {"p2p": p2p}},
		0,
	))

	// Whoever dials in first takes the slot.
	s.Require().EventuallyWithT(func(c *assert.CollectT) {
		assert.NoError(c, s.dialAll(target))
		total, inbound, err := s.peerCounts(0)
		assert.NoError(c, err)
		assert.Equal(c, 1, inbound, "inbound of %d peers", total)
	}, 3*time.Minute, 10*time.Second)

	// The others keep asking and stay out. Each read trails its dials by half
	// the 20 s handshake timeout, so a dial in flight at the first two has
	// landed or failed by the last read.
	for range 3 {
		s.Require().NoError(s.dialAll(target))
		time.Sleep(10 * time.Second)
		total, inbound, err := s.peerCounts(0)
		s.Require().NoError(err)
		s.Require().LessOrEqual(inbound, 1, "inbound of %d peers", total)
	}
}

// dialAll has every validator but the first dial target once.
func (s *PeerLimitSuite) dialAll(target string) error {
	var errs []error
	for i := 1; i < len(s.Chain.Validators); i++ {
		errs = append(errs, s.dialPeer(i, target))
	}
	return errors.Join(errs...)
}

// dialPeer asks validator nodeIdx to dial peer once through the unsafe
// dial_peers route. The route only starts the dial, so a refusal shows in the
// peer counts, not here. JSON-RPC answers an unknown method with HTTP 200, so
// the body is checked: without rpc.unsafe this errors rather than dialling
// nothing.
func (s *PeerLimitSuite) dialPeer(nodeIdx int, peer string) error {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "dial_peers",
		"params": map[string]any{"peers": []string{peer}, "persistent": false},
	})
	if err != nil {
		return err
	}
	host := net.JoinHostPort(s.Chain.Validators[nodeIdx].HostName(), cometRPCPort)
	stdout, _, err := s.Chain.Validators[nodeIdx].Exec(s.GetContext(),
		[]string{"curl", "-sf", "-H", "Content-Type: application/json", "-d", string(body), "http://" + host}, nil)
	if err != nil {
		return err
	}
	var response struct {
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(stdout, &response); err != nil {
		return err
	}
	if len(response.Error) > 0 {
		return fmt.Errorf("dial_peers on validator %d: %s", nodeIdx, response.Error)
	}
	return nil
}

// peerCounts reads validator nodeIdx's net_info: the peers it holds, and how
// many of them dialled in.
func (s *PeerLimitSuite) peerCounts(nodeIdx int) (total, inbound int, err error) {
	host := net.JoinHostPort(s.Chain.Validators[nodeIdx].HostName(), cometRPCPort)
	stdout, _, err := s.Chain.Validators[nodeIdx].Exec(s.GetContext(),
		[]string{"curl", "-sf", "http://" + host + "/net_info"}, nil)
	if err != nil {
		return 0, 0, err
	}
	var response struct {
		Result *struct {
			Peers []struct {
				IsOutbound bool `json:"is_outbound"`
			} `json:"peers"`
		} `json:"result"`
	}
	if err := json.Unmarshal(stdout, &response); err != nil {
		return 0, 0, err
	}
	if response.Result == nil {
		return 0, 0, fmt.Errorf("net_info on validator %d: %s", nodeIdx, stdout)
	}
	for _, peer := range response.Result.Peers {
		if !peer.IsOutbound {
			inbound++
		}
	}
	return len(response.Result.Peers), inbound, nil
}
