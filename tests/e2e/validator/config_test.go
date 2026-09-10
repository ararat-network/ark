// Package validator_test holds the suites that change a validator's node
// configuration or break a validator, so each test gets its own chain.
package validator_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cosmos/interchaintest/v10"
	"github.com/cosmos/interchaintest/v10/chain/cosmos"
	"github.com/cosmos/interchaintest/v10/testutil"
	"github.com/gorilla/websocket"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"golang.org/x/sync/errgroup"

	"github.com/ararat-network/ark/tests/e2e/chainsuite"
)

const (
	cometMetricsPort = "26660"
	appMetricsPort   = "9464"
)

type ConfigSuite struct {
	*chainsuite.Suite
}

func (s *ConfigSuite) TestNoIndexingTransactions() {
	txIndex := make(testutil.Toml)
	txIndex["indexer"] = "null"
	configToml := make(testutil.Toml)
	configToml["tx_index"] = txIndex
	s.Require().NoError(s.Chain.ModifyConfig(
		s.GetContext(), s.T(),
		map[string]testutil.Toml{"config/config.toml": configToml},
		0,
	))

	amount := chainsuite.NOAH(1)
	before, err := s.Chain.GetBalance(s.GetContext(), s.Chain.ValidatorWallets[0].Address, chainsuite.Denom)
	s.Require().NoError(err)
	cmd := s.Chain.GetNode().TxCommand(
		chainsuite.FaucetKeyName, "bank", "send",
		chainsuite.FaucetKeyName, s.Chain.ValidatorWallets[0].Address, amount.String()+chainsuite.Denom,
	)
	stdout, _, err := s.Chain.GetNode().Exec(s.GetContext(), cmd, nil)
	s.Require().NoError(err)
	tx := cosmos.CosmosTx{}
	s.Require().NoError(json.Unmarshal(stdout, &tx))
	s.Require().Equal(0, tx.Code, tx.RawLog)
	s.Require().NoError(testutil.WaitForBlocks(s.GetContext(), 2, s.Chain))

	// The other validators indexed it; the one that did not still executed it.
	txResult, err := s.Chain.Validators[1].GetTransaction(s.Chain.Validators[1].CliContext(), tx.TxHash)
	s.Require().NoError(err)
	s.Require().Equal(uint32(0), txResult.Code, txResult.RawLog)
	after, err := s.Chain.GetBalance(s.GetContext(), s.Chain.ValidatorWallets[0].Address, chainsuite.Denom)
	s.Require().NoError(err)
	s.Require().Equal(before.Add(amount).String(), after.String())

	_, err = s.Chain.Validators[0].GetTransaction(s.Chain.Validators[0].CliContext(), tx.TxHash)
	s.Require().Error(err)
	s.Require().Contains(err.Error(), "transaction indexing is disabled")
}

// TestPrometheus scrapes both endpoints a validator serves: CometBFT's and
// the application's own, which app.toml's [prometheus] table owns.
func (s *ConfigSuite) TestPrometheus() {
	s.Require().NoError(s.enablePrometheus())
	comet, err := s.metrics(0, cometMetricsPort)
	s.Require().NoError(err)
	s.Require().Contains(comet, "cometbft_consensus_height")
	app, err := s.metrics(0, appMetricsPort)
	s.Require().NoError(err)
	s.Require().Contains(app, "ark_abci_requests_total")
}

func (s *ConfigSuite) TestPruningEverything() {
	appToml := make(testutil.Toml)
	appToml["pruning"] = "everything"
	statesync := make(testutil.Toml)
	statesync["snapshot-interval"] = 0
	appToml["state-sync"] = statesync
	s.Require().NoError(s.Chain.ModifyConfig(
		s.GetContext(), s.T(),
		map[string]testutil.Toml{"config/app.toml": appToml},
	))
	s.smokeTestTx()
}

// TestNodeMinGasPricesIgnored pins that the fee floor is Treasury's live
// price, not the node's minimum-gas-prices: a validator setting an absurd
// local floor still admits transactions at the chain's price.
func (s *ConfigSuite) TestNodeMinGasPricesIgnored() {
	appToml := make(testutil.Toml)
	appToml["minimum-gas-prices"] = "999999999999999999" + chainsuite.Denom
	s.Require().NoError(s.Chain.ModifyConfig(
		s.GetContext(), s.T(),
		map[string]testutil.Toml{"config/app.toml": appToml},
		0,
	))
	s.smokeTestTx()
}

func (s *ConfigSuite) TestPeerLimit() {
	s.Require().NoError(s.enablePrometheus())
	metrics, err := s.metrics(0, cometMetricsPort)
	s.Require().NoError(err)
	s.Require().Equal(float64(3), metrics["cometbft_p2p_peers"].GetMetric()[0].GetGauge().GetValue())

	peers := s.Chain.Nodes().PeerString(s.GetContext())
	peerList := strings.Split(peers, ",")
	for i, node := range s.Chain.Nodes() {
		if i > 0 {
			s.Require().NoError(node.SetPeers(s.GetContext(), peerList[0]))
		}
	}
	s.Require().NoError(s.Chain.Validators[0].SetPeers(s.GetContext(), ""))
	s.Require().NoError(s.Chain.StopAllNodes(s.GetContext()))
	s.Require().NoError(s.Chain.StartAllNodes(s.GetContext()))
	s.Require().NoError(testutil.WaitForBlocks(s.GetContext(), 2, s.Chain))

	p2p := make(testutil.Toml)
	p2p["max_num_inbound_peers"] = 2
	// Without PEX the peer count is what the test set.
	p2p["pex"] = false
	configToml := make(testutil.Toml)
	configToml["p2p"] = p2p
	s.Require().NoError(s.Chain.ModifyConfig(
		s.GetContext(), s.T(),
		map[string]testutil.Toml{"config/config.toml": configToml},
		0,
	))
	s.Require().NoError(testutil.WaitForBlocks(s.GetContext(), 4, s.Chain))

	s.Require().EventuallyWithT(func(c *assert.CollectT) {
		metrics, err = s.metrics(0, cometMetricsPort)
		assert.NoError(c, err)
		assert.Equal(c, float64(2), metrics["cometbft_p2p_peers"].GetMetric()[0].GetGauge().GetValue())
	}, 3*time.Minute, 10*time.Second)

	foundZero := false
	for i := 1; i < len(s.Chain.Validators); i++ {
		metrics, err = s.metrics(i, cometMetricsPort)
		s.Require().NoError(err)
		metric := metrics["cometbft_p2p_peers"].GetMetric()
		if (len(metric) == 0 || metric[0].GetGauge().GetValue() == float64(0)) && !foundZero {
			// The one validator 0 refused.
			foundZero = true
			continue
		}
		s.Require().GreaterOrEqual(len(metric), 1)
		s.Require().GreaterOrEqual(metric[0].GetGauge().GetValue(), float64(1))
	}
}

func (s *ConfigSuite) TestWSConnectionLimit() {
	const connectionCount = 20
	u, err := url.Parse(s.Chain.GetHostRPCAddress())
	s.Require().NoError(err)
	u.Scheme = "ws"
	u.Path = "/websocket"
	canConnect := func() error {
		var eg errgroup.Group
		tCtx, tCancel := context.WithTimeout(s.GetContext(), 80*time.Second)
		defer tCancel()
		for i := range connectionCount {
			eg.Go(func() error {
				c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
				if err != nil {
					return err
				}
				defer c.Close()
				err = c.WriteMessage(
					websocket.TextMessage,
					[]byte(fmt.Sprintf(`{"jsonrpc":"2.0","method":"subscribe","params":["tm.event='NewBlock'"],"id":%d}`, i)),
				)
				if err != nil {
					return err
				}
				for tCtx.Err() == nil {
					if _, _, err = c.ReadMessage(); err != nil {
						return err
					}
				}
				return c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			})
		}
		return eg.Wait()
	}
	s.Require().NoError(canConnect())

	rpc := make(testutil.Toml)
	rpc["max_open_connections"] = 10
	configToml := make(testutil.Toml)
	configToml["rpc"] = rpc
	s.Require().NoError(s.Chain.ModifyConfig(
		s.GetContext(), s.T(),
		map[string]testutil.Toml{"config/config.toml": configToml},
	))
	s.Require().Error(canConnect())
}

func (s *ConfigSuite) TestDisableAPI() {
	curl := []string{"curl", "-sf", s.Chain.GetAPIAddress() + "/cosmos/auth/v1beta1/accounts"}
	_, _, err := s.Chain.Validators[0].Exec(s.GetContext(), curl, nil)
	s.Require().NoError(err)

	apiToml := make(testutil.Toml)
	apiToml["enable"] = false
	appToml := make(testutil.Toml)
	appToml["api"] = apiToml
	s.Require().NoError(s.Chain.ModifyConfig(
		s.GetContext(), s.T(),
		map[string]testutil.Toml{"config/app.toml": appToml},
	))
	_, _, err = s.Chain.Validators[0].Exec(s.GetContext(), curl, nil)
	s.Require().Error(err)
}

func TestConfig(t *testing.T) {
	s := &ConfigSuite{
		Suite: chainsuite.NewSuite(chainsuite.SuiteConfig{
			UpgradeOnSetup: true,
			// Configuration changes must not leak between tests.
			Scope: chainsuite.ChainScopeTest,
			ChainSpec: &interchaintest.ChainSpec{
				NumValidators: &chainsuite.FourValidators,
			},
		}),
	}
	suite.Run(t, s)
}

func (s *ConfigSuite) enablePrometheus() error {
	instrumentation := make(testutil.Toml)
	instrumentation["prometheus"] = true
	configToml := make(testutil.Toml)
	configToml["instrumentation"] = instrumentation
	prometheus := make(testutil.Toml)
	prometheus["enabled"] = true
	prometheus["address"] = "0.0.0.0:" + appMetricsPort
	appToml := make(testutil.Toml)
	appToml["prometheus"] = prometheus
	return s.Chain.ModifyConfig(
		s.GetContext(), s.T(),
		map[string]testutil.Toml{
			"config/config.toml": configToml,
			"config/app.toml":    appToml,
		},
	)
}

// metrics scrapes nodeIdx's port from inside its container.
func (s *ConfigSuite) metrics(nodeIdx int, port string) (map[string]*dto.MetricFamily, error) {
	host := net.JoinHostPort(s.Chain.Validators[nodeIdx].HostName(), port)
	stdout, _, err := s.Chain.Validators[nodeIdx].Exec(s.GetContext(),
		[]string{"curl", "-sf", "http://" + host + "/metrics"}, nil)
	if err != nil {
		return nil, err
	}
	var parser expfmt.TextParser
	return parser.TextToMetricFamilies(bytes.NewBuffer(stdout))
}

// smokeTestTx is a bank send through validator 0 proving it still serves.
func (s *ConfigSuite) smokeTestTx() {
	txhash, err := s.Chain.GetNode().ExecTx(
		s.GetContext(), chainsuite.FaucetKeyName,
		"bank", "send", chainsuite.FaucetKeyName,
		s.Chain.ValidatorWallets[0].Address, chainsuite.NOAHCoin(1),
	)
	s.Require().NoError(err)
	tx, err := s.Chain.GetTransaction(txhash)
	s.Require().NoError(err)
	s.Require().Equal(uint32(0), tx.Code)
	s.Require().Greater(tx.Height, int64(1))
}
