package delegator_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/ararat-network/ark/tests/e2e/chainsuite"
	"github.com/ararat-network/ark/tests/e2e/delegator"
)

// CosmWasmSuite stores, instantiates, executes, and queries a contract
// through the CLI. The runtime ships open, so no proposal is needed.
type CosmWasmSuite struct {
	*delegator.Suite
}

func (s *CosmWasmSuite) TestCounterContract() {
	ctx := s.GetContext()
	codeID, err := s.Chain.StoreContract(ctx, s.Wallet.KeyName(), "../testdata/counter.wasm")
	s.Require().NoError(err)
	_, err = s.Node().ExecTx(ctx, s.Wallet.KeyName(),
		"wasm", "instantiate", codeID, `{"count":0}`, "--label", "counter", "--no-admin",
	)
	s.Require().NoError(err)
	contracts, err := s.Chain.QueryJSON(ctx, "contracts", "wasm", "list-contract-by-code", codeID)
	s.Require().NoError(err)
	s.Require().Len(contracts.Array(), 1, contracts.Raw)
	contract := contracts.Array()[0].String()

	_, err = s.Node().ExecTx(ctx, s.Wallet.KeyName(), "wasm", "execute", contract, `{"increment":{}}`)
	s.Require().NoError(err)

	var resp struct {
		Data struct {
			Count int64 `json:"count"`
		} `json:"data"`
	}
	s.Require().NoError(s.Chain.QueryContract(ctx, contract, `{"get_count":{}}`, &resp))
	s.Require().EqualValues(1, resp.Data.Count)
}

func TestCosmWasm(t *testing.T) {
	s := &CosmWasmSuite{Suite: &delegator.Suite{Suite: chainsuite.NewSuite(chainsuite.SuiteConfig{
		UpgradeOnSetup: true,
	})}}
	suite.Run(t, s)
}
