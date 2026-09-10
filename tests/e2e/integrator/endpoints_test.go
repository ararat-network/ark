// Package integrator_test holds what explorers, wallets, and relaunch
// operators depend on: the endpoints a node serves and an export that boots.
package integrator_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/ararat-network/ark/tests/e2e/chainsuite"
)

type EndpointsSuite struct {
	*chainsuite.Suite
}

// TestAPIEndpoints is the REST surface integrators read: the SDK routes
// operators and explorers depend on, and every route Ark's modules register
// whose argument exists at genesis. The settlement plan and the tax cap by
// denomination are left out: no launch genesis holds an asset in settlement
// or a tax cap, and both answer not found until one does.
func (s *EndpointsSuite) TestAPIEndpoints() {
	wallets := s.Chain.ValidatorWallets
	tests := []struct {
		name string
		path string
		key  string
	}{
		{name: "node_info", path: "/cosmos/base/tendermint/v1beta1/node_info", key: "default_node_info"},
		{name: "syncing", path: "/cosmos/base/tendermint/v1beta1/syncing", key: "syncing"},
		{name: "latest_block", path: "/cosmos/base/tendermint/v1beta1/blocks/latest", key: "block"},
		{name: "block_1", path: "/cosmos/base/tendermint/v1beta1/blocks/1", key: "block"},
		{name: "validatorsets_latest", path: "/cosmos/base/tendermint/v1beta1/validatorsets/latest", key: "validators"},
		{name: "node_config", path: "/cosmos/base/node/v1beta1/config", key: "minimum_gas_price"},
		{name: "txs", path: "/cosmos/tx/v1beta1/txs?query=tx.height=9999999999", key: "txs"},
		{name: "auth_accounts", path: "/cosmos/auth/v1beta1/accounts", key: "accounts"},
		{name: "auth_params", path: "/cosmos/auth/v1beta1/params", key: "params"},
		{name: "bank_balances", path: "/cosmos/bank/v1beta1/balances/" + wallets[0].Address, key: "balances"},
		{name: "bank_denoms_metadata", path: "/cosmos/bank/v1beta1/denoms_metadata", key: "metadatas"},
		{name: "bank_supply", path: "/cosmos/bank/v1beta1/supply", key: "supply"},
		{name: "distribution_community_pool", path: "/cosmos/distribution/v1beta1/community_pool", key: "pool"},
		{name: "distribution_slashes", path: "/cosmos/distribution/v1beta1/validators/" + wallets[0].ValoperAddress + "/slashes", key: "slashes"},
		{name: "evidence", path: "/cosmos/evidence/v1beta1/evidence", key: "evidence"},
		{name: "gov_proposals", path: "/cosmos/gov/v1/proposals", key: "proposals"},
		{name: "gov_params", path: "/cosmos/gov/v1/params/voting", key: "params"},
		{name: "slashing_params", path: "/cosmos/slashing/v1beta1/params", key: "params"},
		{name: "slashing_signing_infos", path: "/cosmos/slashing/v1beta1/signing_infos", key: "info"},
		{name: "staking_params", path: "/cosmos/staking/v1beta1/params", key: "params"},
		{name: "staking_delegations", path: "/cosmos/staking/v1beta1/delegations/" + wallets[0].Address, key: "delegation_responses"},
		{name: "staking_redelegations", path: "/cosmos/staking/v1beta1/delegators/" + wallets[0].Address + "/redelegations", key: "redelegation_responses"},
		{name: "staking_unbonding", path: "/cosmos/staking/v1beta1/delegators/" + wallets[0].Address + "/unbonding_delegations", key: "unbonding_responses"},
		{name: "staking_delegator_validators", path: "/cosmos/staking/v1beta1/delegators/" + wallets[0].Address + "/validators", key: "validators"},
		{name: "staking_validators", path: "/cosmos/staking/v1beta1/validators", key: "validators"},
		{name: "staking_validator_delegations", path: "/cosmos/staking/v1beta1/validators/" + wallets[0].ValoperAddress + "/delegations", key: "delegation_responses"},
		{name: "staking_validator_unbonding", path: "/cosmos/staking/v1beta1/validators/" + wallets[0].ValoperAddress + "/unbonding_delegations", key: "unbonding_responses"},
		{name: "upgrade_plan", path: "/cosmos/upgrade/v1beta1/current_plan", key: "plan"},
		{name: "ibc_client_params", path: "/ibc/core/client/v1/params", key: "params"},
		{name: "ibc_transfer_params", path: "/ibc/apps/transfer/v1/params", key: "params"},
		{name: "wasm_codes", path: "/cosmwasm/wasm/v1/code", key: "code_infos"},

		{name: "asset_assets", path: "/ark/asset/v1/assets", key: "priced_assets"},
		{name: "asset_asset", path: "/ark/asset/v1/assets/ausd", key: "priced_asset"},
		{name: "asset_resolutions", path: "/ark/asset/v1/assets/ausd/resolutions", key: "resolution_records"},
		{name: "asset_emergency_mandate", path: "/ark/asset/v1/emergency_mandate", key: "mandate"},
		{name: "asset_params", path: "/ark/asset/v1/params", key: "params"},
		{name: "claims_balance", path: "/ark/claims/v1/balance", key: "balance"},
		{name: "claims_claims", path: "/ark/claims/v1/claims", key: "claims"},
		{name: "claims_mandate", path: "/ark/claims/v1/mandate", key: "mandate"},
		{name: "claims_params", path: "/ark/claims/v1/params", key: "params"},
		{name: "market_conversion_mandate", path: "/ark/market/v1/conversion_mandate", key: "mandate"},
		{name: "market_conversion_policy", path: "/ark/market/v1/conversion_policy", key: "conversion_policy"},
		{name: "market_params", path: "/ark/market/v1/params", key: "params"},
		{name: "market_pool", path: "/ark/market/v1/pool", key: "base_pool"},
		{name: "market_tobin_tax", path: "/ark/market/v1/tobin_tax/ausd", key: "tobin_tax"},
		{name: "market_tobin_tax_overrides", path: "/ark/market/v1/tobin_tax_overrides", key: "tobin_tax_overrides"},
		{name: "oracle_exchange_rates", path: "/ark/oracle/v1/denoms/exchange_rates", key: "exchange_rates"},
		{name: "oracle_feeds", path: "/ark/oracle/v1/feeds", key: "feeds"},
		{name: "oracle_feed_referents", path: "/ark/oracle/v1/feeds/ausd/referents", key: "referents"},
		{name: "oracle_params", path: "/ark/oracle/v1/params", key: "params"},
		{name: "oracle_reference_denom", path: "/ark/oracle/v1/reference_denom", key: "reference_denom"},
		{name: "oracle_attendance", path: "/ark/oracle/v1/validators/" + wallets[0].ValoperAddress + "/attendance", key: "attendance"},
		{name: "oracle_reward_weight", path: "/ark/oracle/v1/validators/" + wallets[0].ValoperAddress + "/reward_weight", key: "reward_weight"},
		{name: "reserve_balance", path: "/ark/reserve/v1/balance", key: "balance"},
		{name: "reserve_closed_positions", path: "/ark/reserve/v1/closed_positions", key: "positions"},
		{name: "reserve_ledger", path: "/ark/reserve/v1/ledger", key: "ledger"},
		{name: "reserve_mandate", path: "/ark/reserve/v1/mandate", key: "mandate"},
		{name: "reserve_open_positions", path: "/ark/reserve/v1/open_positions", key: "positions"},
		{name: "reserve_recognised_capital", path: "/ark/reserve/v1/recognised_capital", key: "recognised"},
		{name: "reserve_recognition_policy", path: "/ark/reserve/v1/recognition_policy", key: "entries"},
		{name: "security_committee_plan", path: "/ark/security/v1/committee_plan", key: "committee_plan"},
		{name: "security_mandate", path: "/ark/security/v1/security_mandate", key: "mandate"},
		{name: "treasury_conversion_factors", path: "/ark/treasury/v1/conversion_factors", key: "conversion_factors"},
		{name: "treasury_conversion_factor", path: "/ark/treasury/v1/conversion_factors/anoah", key: "conversion_factor"},
		{name: "treasury_economic_mandate", path: "/ark/treasury/v1/economic_mandate", key: "mandate"},
		{name: "treasury_economic_policy", path: "/ark/treasury/v1/economic_policy", key: "policy"},
		{name: "treasury_exposure_status", path: "/ark/treasury/v1/exposure_status", key: ""},
		{name: "treasury_fund_status", path: "/ark/treasury/v1/fund_status", key: ""},
		{name: "treasury_gas_prices", path: "/ark/treasury/v1/gas_prices", key: "gas_prices"},
		{name: "treasury_gas_price", path: "/ark/treasury/v1/gas_prices/anoah", key: "gas_price"},
		{name: "treasury_params", path: "/ark/treasury/v1/params", key: "params"},
		{name: "treasury_reward_funding", path: "/ark/treasury/v1/reward_funding", key: ""},
		{name: "treasury_tax_caps", path: "/ark/treasury/v1/tax_caps", key: "tax_caps"},
	}
	for _, tt := range tests {
		s.Run("API "+tt.name, func() {
			body, err := s.Chain.RestGet(s.GetContext(), tt.path)
			s.Require().NoError(err)
			if tt.key != "" {
				s.Require().True(body.Get(tt.key).Exists(), "%s: no %q in %s", tt.path, tt.key, body.Raw)
			}
		})
	}

	s.Run("API missing route", func() {
		_, status, err := chainsuite.Get(s.GetContext(), s.Chain.HostAPI()+"/missing_endpoint")
		s.Require().NoError(err)
		s.Require().Equal(http.StatusNotImplemented, status)
	})
}

func (s *EndpointsSuite) TestRPCEndpoints() {
	tests := []struct {
		name string
		path string
		key  string
	}{
		{name: "abci_info", path: "/abci_info", key: "response"},
		{name: "block", path: "/block", key: "block"},
		{name: "block_results", path: "/block_results", key: "finalize_block_events"}, //nolint:misspell // CometBFT's key
		{name: "blockchain", path: "/blockchain", key: "block_metas"},
		{name: "commit", path: "/commit", key: "signed_header"},
		{name: "consensus_params", path: "/consensus_params", key: "consensus_params"},
		{name: "consensus_state", path: "/consensus_state", key: "round_state"},
		{name: "dump_consensus_state", path: "/dump_consensus_state", key: "round_state"},
		{name: "genesis_chunked", path: "/genesis_chunked", key: "chunk"},
		{name: "net_info", path: "/net_info", key: "peers"},
		{name: "num_unconfirmed_txs", path: "/num_unconfirmed_txs", key: "n_txs"},
		{name: "unconfirmed_txs", path: "/unconfirmed_txs", key: "n_txs"},
		{name: "status", path: "/status", key: "node_info"},
		{name: "validators", path: "/validators", key: "validators"},
	}
	for _, tt := range tests {
		s.Run("RPC "+tt.name, func() {
			body, err := chainsuite.GetJSON(s.GetContext(), s.Chain.HostRPC()+tt.path)
			s.Require().NoError(err)
			s.Require().True(body.Get("result").Exists(), body.Raw)
			s.Require().True(body.Get("result."+tt.key).Exists(), "%s: no %q in %s", tt.path, tt.key, body.Raw)
		})
	}
}

// TestVoteExtensionsInBlock pins the block shape integrators decode: the
// first transaction of every block past the first is the extended commit.
func (s *EndpointsSuite) TestVoteExtensionsInBlock() {
	block, err := chainsuite.GetJSON(s.GetContext(), s.Chain.HostRPC()+"/block")
	s.Require().NoError(err)
	txs := block.Get("result.block.data.txs")
	s.Require().True(txs.IsArray(), block.Raw)
	s.Require().NotEmpty(txs.Array(), "a block past the first carries the extended commit as its first transaction")

	votes, err := s.Chain.VoteExtensions(s.GetContext(), 0)
	s.Require().NoError(err)
	s.Require().Len(votes, len(s.Chain.Validators))
}

func TestEndpoints(t *testing.T) {
	s := &EndpointsSuite{
		Suite: chainsuite.NewSuite(chainsuite.SuiteConfig{UpgradeOnSetup: true}),
	}
	suite.Run(t, s)
}
