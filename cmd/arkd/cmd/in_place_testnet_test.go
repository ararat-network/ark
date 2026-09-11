package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/cometbft/cometbft/privval"
	cmttypes "github.com/cometbft/cometbft/types"
	cmttime "github.com/cometbft/cometbft/types/time"

	"cosmossdk.io/log/v2"

	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
)

// TestForgeCommitVerifiesUnderTheNewValidatorSet pins what the node checks
// when it starts on the rewritten state: the commit verifies against the one
// validator under the new chain ID, and the extended commit rebuilds into a
// vote set with a majority. Both signatures bind the chain ID.
func TestForgeCommitVerifiesUnderTheNewValidatorSet(t *testing.T) {
	privKey := ed25519.GenPrivKey()
	blockID := cmttypes.BlockID{
		Hash:          bytes.Repeat([]byte{1}, 32),
		PartSetHeader: cmttypes.PartSetHeader{Total: 1, Hash: bytes.Repeat([]byte{2}, 32)},
	}
	commit, extended, err := forgeCommit(privKey, "ark-fork-1", 8, blockID, cmttime.Now())
	require.NoError(t, err)
	valSet := cmttypes.NewValidatorSet([]*cmttypes.Validator{
		{Address: privKey.PubKey().Address(), PubKey: privKey.PubKey(), VotingPower: 5_000_000},
	})

	require.NoError(t, valSet.VerifyCommit("ark-fork-1", blockID, 8, commit))
	require.Error(t, valSet.VerifyCommit("ark-1", blockID, 8, commit))

	require.NoError(t, extended.EnsureExtensions(true))
	var votes *cmttypes.VoteSet
	require.NotPanics(t, func() { votes = extended.ToExtendedVoteSet("ark-fork-1", valSet) })
	require.True(t, votes.HasTwoThirdsMajority())
	require.Panics(t, func() { extended.ToExtendedVoteSet("ark-1", valSet) })
}

// The signing state is moved back only when it is past the last block.
func TestRealignSignState(t *testing.T) {
	dir := t.TempDir()
	keyFile, stateFile := filepath.Join(dir, "key.json"), filepath.Join(dir, "state.json")
	pv := privval.GenFilePV(keyFile, stateFile)
	pv.LastSignState.Height = 9
	pv.LastSignState.Step = stepPrecommit
	pv.Save()

	realignSignState(log.NewNopLogger(), pv, 9)
	require.Equal(t, int64(9), privval.LoadFilePV(keyFile, stateFile).LastSignState.Height)

	realignSignState(log.NewNopLogger(), pv, 8)
	reloaded := privval.LoadFilePV(keyFile, stateFile)
	require.Equal(t, int64(8), reloaded.LastSignState.Height)
	require.Equal(t, int32(0), reloaded.LastSignState.Round)
	require.Equal(t, stepPrecommit, reloaded.LastSignState.Step)
	require.Empty(t, reloaded.LastSignState.SignBytes)
}

func TestRenameGenesis(t *testing.T) {
	path := filepath.Join(t.TempDir(), "genesis.json")
	base := &genutiltypes.AppGenesis{
		ChainID:   "ark-1",
		Consensus: &genutiltypes.ConsensusGenesis{Params: cmttypes.DefaultConsensusParams()},
	}
	require.NoError(t, base.SaveAs(path))

	require.NoError(t, renameGenesis(path, "ark-fork-1"))
	got, err := genutiltypes.AppGenesisFromFile(path)
	require.NoError(t, err)
	require.Equal(t, "ark-fork-1", got.ChainID)

	require.Error(t, renameGenesis(filepath.Join(t.TempDir(), "missing.json"), "x"))
}

func TestInPlaceTestnetConfirmation(t *testing.T) {
	for answer, wantErr := range map[string]bool{"y\n": false, "yes\n": false, "n\n": true, "\n": true} {
		cmd := &cobra.Command{}
		cmd.SetIn(bytes.NewBufferString(answer))
		cmd.SetOut(os.NewFile(0, os.DevNull))
		err := confirmInPlaceTestnet(cmd)
		if wantErr {
			require.Error(t, err, answer)
		} else {
			require.NoError(t, err, answer)
		}
	}
}

// in-place-testnet is the start command with two arguments and its own
// flags; start's PreRunE checks run on it as well.
func TestInPlaceTestnetCommandShape(t *testing.T) {
	rootCmd := NewRootCmd()
	cmd, _, err := rootCmd.Find([]string{"in-place-testnet"})
	require.NoError(t, err)
	require.NotNil(t, cmd.Flags().Lookup(flagTriggerTestnetUpgrade))
	require.NotNil(t, cmd.Flags().Lookup(flagSkipConfirmation))
	require.NotNil(t, cmd.Flags().Lookup("with-comet"))
	require.NotNil(t, cmd.PreRunE)
	require.Error(t, cmd.Args(cmd, []string{"one"}))
	require.NoError(t, cmd.Args(cmd, []string{"chain", "operator"}))
}
