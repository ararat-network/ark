package ibc_test

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/ararat-network/ark/tests/e2e/chainsuite"
	"github.com/ararat-network/ark/tests/e2e/testdata"
)

// TestWasmLightClient stores a wasm light client through governance, reads
// its checksum back, and creates a client from it.
func (s *HubSuite) TestWasmLightClient() {
	ctx := s.GetContext()
	chain := s.ChainA
	gz, err := base64.StdEncoding.DecodeString(testdata.WasmDummyLightClient)
	s.Require().NoError(err)
	reader, err := gzip.NewReader(bytes.NewReader(gz))
	s.Require().NoError(err)
	code, err := io.ReadAll(reader)
	s.Require().NoError(err)
	checksum := sha256.Sum256(code)

	gov, err := chain.GovAuthority(ctx)
	s.Require().NoError(err)
	store := json.RawMessage(fmt.Sprintf(
		`{"@type":"/ibc.lightclients.wasm.v1.MsgStoreCode","signer":%q,"wasm_byte_code":%q}`,
		gov, base64.StdEncoding.EncodeToString(gz),
	))
	_, err = chain.SubmitAndPassProposal(ctx, chainsuite.FaucetKeyName, "store a wasm light client", store)
	s.Require().NoError(err)

	checksums, err := chain.QueryJSON(ctx, "checksums", "ibc-wasm", "checksums")
	s.Require().NoError(err)
	s.Require().Contains(checksums.Raw, hex.EncodeToString(checksum[:]))

	node := chain.GetNode()
	clientState := fmt.Sprintf(
		`{"@type":"/ibc.lightclients.wasm.v1.ClientState","data":"ZG9lc250IG1hdHRlcg==","checksum":%q,"latest_height":{"revision_number":"0","revision_height":"7795583"}}`,
		base64.StdEncoding.EncodeToString(checksum[:]),
	)
	consensusState := `{"@type":"/ibc.lightclients.wasm.v1.ConsensusState","data":"ZG9lc250IG1hdHRlcg=="}`
	s.Require().NoError(node.WriteFile(ctx, []byte(clientState), "wasm_client_state.json"))
	s.Require().NoError(node.WriteFile(ctx, []byte(consensusState), "wasm_consensus_state.json"))
	_, err = node.ExecTx(ctx, s.WalletA.KeyName(), "ibc", "client", "create",
		path.Join(node.HomeDir(), "wasm_client_state.json"),
		path.Join(node.HomeDir(), "wasm_consensus_state.json"),
	)
	s.Require().NoError(err)

	// Client IDs share one sequence across types, so the wasm client follows
	// the relayer's tendermint one.
	states, err := chain.QueryJSON(ctx, `client_states.#(client_id%"08-wasm-*").client_id`, "ibc", "client", "states")
	s.Require().NoError(err)
	s.Require().True(strings.HasPrefix(states.String(), "08-wasm-"), states.Raw)
}
