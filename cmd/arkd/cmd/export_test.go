package cmd

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	cmttypes "github.com/cometbft/cometbft/types"

	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
)

// TestContinuationGenesisKeepsEveryConsensusParam pins what the SDK's own
// export loses: the ABCI and version groups survive the trip through the
// genesis document, so a relaunch keeps vote extensions on.
func TestContinuationGenesisKeepsEveryConsensusParam(t *testing.T) {
	params := cmttypes.DefaultConsensusParams().ToProto()
	params.Abci.VoteExtensionsEnableHeight = 1
	params.Version.App = 7
	base := &genutiltypes.AppGenesis{
		ChainID:   "ark-export-test",
		Consensus: &genutiltypes.ConsensusGenesis{Params: cmttypes.DefaultConsensusParams()},
	}
	exported := servertypes.ExportedApp{
		AppState:        json.RawMessage(`{"bank":{}}`),
		Height:          42,
		ConsensusParams: params,
	}

	out, err := json.Marshal(continuationGenesis(base, exported))
	require.NoError(t, err)
	got, err := genutiltypes.AppGenesisFromReader(bytes.NewReader(out))
	require.NoError(t, err)

	require.Equal(t, int64(42), got.InitialHeight)
	require.Equal(t, int64(1), got.Consensus.Params.ABCI.VoteExtensionsEnableHeight)
	require.Equal(t, uint64(7), got.Consensus.Params.Version.App)
	require.JSONEq(t, `{"bank":{}}`, string(got.AppState))
}
