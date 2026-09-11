package chainsuite

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// TestOverlayArtefact checks the merge without a chain: interchaintest's
// accounts and gentxs survive, the bank ledger is the artefact's plus the
// generated accounts with supply recomputed, and every other module and the
// consensus block are the artefact's.
func TestOverlayArtefact(t *testing.T) {
	generated := []byte(`{
	  "chain_id": "ark-e2e",
	  "genesis_time": "2026-09-11T00:00:00Z",
	  "consensus": {"params": {"block": {"max_gas": "-1"}}},
	  "app_state": {
	    "auth": {"accounts": [{"@type": "/cosmos.auth.v1beta1.BaseAccount", "address": "ark1validator"}]},
	    "bank": {
	      "params": {"default_send_enabled": false},
	      "balances": [{"address": "ark1validator", "coins": [{"denom": "anoah", "amount": "10000000000000000000000"}]}],
	      "supply": [{"denom": "anoah", "amount": "10000000000000000000000"}],
	      "denom_metadata": [],
	      "send_enabled": []
	    },
	    "genutil": {"gen_txs": [{"body": {}}]},
	    "gov": {"params": {"voting_period": "172800s"}},
	    "treasury": {"params": {"transfer_tax_rate": "0"}}
	  }
	}`)

	merged, err := overlayArtefact(generated)
	require.NoError(t, err)
	out := gjson.ParseBytes(merged)

	require.Equal(t, "ark-e2e", out.Get("chain_id").String(), "the generated chain ID stays")
	require.Equal(t, "100000000", out.Get("consensus.params.block.max_gas").String(), "the consensus block is the artefact's")
	require.Equal(t, "ark1validator", out.Get("app_state.auth.accounts.0.address").String(), "generated accounts stay")
	require.Len(t, out.Get("app_state.genutil.gen_txs").Array(), 1, "generated gentxs stay")
	require.Equal(t, "0.005000000000000000", out.Get("app_state.treasury.params.transfer_tax_rate").String(), "other modules are the artefact's")
	require.Equal(t, "3600s", out.Get("app_state.gov.params.voting_period").String())

	bank := out.Get("app_state.bank")
	require.True(t, bank.Get("params.default_send_enabled").Bool(), "bank params are the artefact's")
	require.Len(t, bank.Get("denom_metadata").Array(), 1, "the NOAH metadata comes from the artefact")
	balances := bank.Get("balances").Array()
	require.Len(t, balances, 6, "five artefact balances plus the generated account")
	require.Equal(t, "1000010000000000000000000000", bank.Get("supply.0.amount").String(),
		"supply is the artefact's billion plus the generated 10,000 NOAH")

	var state map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(out.Get("app_state").Raw), &state))
	require.Contains(t, state, "wasm", "modules the generator did not write arrive from the artefact")
}

// TestOverlayArtefactRefusesCollidingAccount pins that a generated account
// on an artefact address is an error rather than a doubled balance.
func TestOverlayArtefactRefusesCollidingAccount(t *testing.T) {
	generated := []byte(`{"chain_id": "ark-e2e", "consensus": {}, "app_state": {
	  "auth": {"accounts": []}, "genutil": {"gen_txs": []},
	  "bank": {"balances": [{"address": "ark1jv65s3grqf6v6jl3dp4t6c9t9rk99cd8h9tdgl", "coins": [{"denom": "anoah", "amount": "1"}]}], "supply": []}
	}}`)
	_, err := overlayArtefact(generated)
	require.ErrorContains(t, err, "collides with an artefact balance")
}
