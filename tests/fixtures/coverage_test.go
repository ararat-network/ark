package fixtures

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/baseapp"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/app"
)

// knownGaps lists the registered messages no fixture pins. The list only
// shrinks: a message that gains a fixture must leave it, and a new message
// must either get a fixture or be added here with its reason.
var knownGaps = []string{
	// Needs two conflicting signed votes from a real validator; nothing a
	// fixture can forge honestly.
	"/cosmos.evidence.v1beta1.MsgSubmitEvidence",
	// Emitted by a contract over an IBC channel, which the chain does not have.
	"/cosmwasm.wasm.v1.MsgIBCCloseChannel",
	"/cosmwasm.wasm.v1.MsgIBCSend",
}

// TestMessageCoverage requires every message type the application registers to
// appear in a fixture, at the top level of a transaction or inside a proposal,
// unless it is a known gap.
func TestMessageCoverage(t *testing.T) {
	arkApp := app.NewArkApp(log.NewNopLogger(), dbm.NewMemDB(), true, simtestutil.NewAppOptionsWithFlagHome(t.TempDir()), baseapp.SetChainID(chainID))
	t.Cleanup(func() { require.NoError(t, arkApp.Close()) })
	registered := arkApp.InterfaceRegistry().ListImplementations(sdk.MsgInterfaceProtoName)
	sort.Strings(registered)
	require.NotEmpty(t, registered)

	covered := map[string]bool{}
	entries, err := os.ReadDir(fixtureDir)
	require.NoError(t, err)
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(fixtureDir, e.Name()))
		require.NoError(t, err)
		var v any
		require.NoError(t, json.Unmarshal(raw, &v))
		collectTypeURLs(v, covered)
	}

	var missing []string
	for _, url := range registered {
		gap := slices.Contains(knownGaps, url)
		switch {
		case covered[url] && gap:
			t.Errorf("%s has a fixture; remove it from knownGaps", url)
		case !covered[url] && !gap:
			missing = append(missing, url)
		}
	}
	if len(missing) > 0 {
		t.Errorf("registered messages without a fixture or a knownGaps entry:\n  %s", joinLines(missing))
	}
	for _, url := range knownGaps {
		if !slices.Contains(registered, url) {
			t.Errorf("%s in knownGaps is not a registered message", url)
		}
	}
	require.True(t, slices.IsSorted(knownGaps), "knownGaps must be sorted")
}

// collectTypeURLs records every "@type" value in a decoded JSON document, which
// is how proto JSON names a message at any depth: a transaction's messages, a
// proposal's, an authz exec's.
func collectTypeURLs(v any, into map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		if url, ok := x["@type"].(string); ok {
			into[url] = true
		}
		for _, child := range x {
			collectTypeURLs(child, into)
		}
	case []any:
		for _, child := range x {
			collectTypeURLs(child, into)
		}
	}
}

func joinLines(items []string) string {
	out := ""
	for i, item := range items {
		if i > 0 {
			out += "\n  "
		}
		out += item
	}
	return out
}
