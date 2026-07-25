package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"testing"

	cometabci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"

	"ark/abci/codec"
	vetypes "ark/abci/voteextension/types"
	arkencoding "ark/pkg/encoding"
)

func TestVoteExtensionsBlockHeight(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		queryHeight int64
		setHeight   bool
		want        *int64
		wantErr     string
	}{
		{
			name: "positional height",
			args: []string{"42"},
			want: heightPointer(42),
		},
		{
			name:        "query height flag",
			queryHeight: 43,
			setHeight:   true,
			want:        heightPointer(43),
		},
		{
			name: "latest height",
		},
		{
			name:    "invalid positional height",
			args:    []string{"invalid"},
			wantErr: "parse block height",
		},
		{
			name:    "zero positional height",
			args:    []string{"0"},
			wantErr: "block height must be positive",
		},
		{
			name:        "negative height",
			queryHeight: -1,
			setHeight:   true,
			wantErr:     "block height must be positive",
		},
		{
			name:        "ambiguous heights",
			args:        []string{"42"},
			queryHeight: 43,
			setHeight:   true,
			wantErr:     "not both",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := voteExtensionsCommand()
			if tc.setHeight {
				require.NoError(t, cmd.Flags().Set(flags.FlagHeight, stringHeight(tc.queryHeight)))
			}

			got, err := voteExtensionsBlockHeight(cmd, tc.args, tc.queryHeight)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestInspectVoteExtensions(t *testing.T) {
	validBlock := voteExtensionsTestBlock(t, 42)

	tests := []struct {
		name    string
		block   *cmttypes.Block
		check   func(*testing.T, voteExtensionsOutput)
		wantErr string
	}{
		{
			name:  "decoded extensions",
			block: validBlock,
			check: func(t *testing.T, got voteExtensionsOutput) {
				require.Equal(t, int64(42), got.SourceBlockHeight)
				require.Equal(t, int64(41), got.VoteHeight)
				require.Equal(t, int32(2), got.Round)
				require.Len(t, got.Votes, 2)
				require.Equal(t, "0102", got.Votes[0].ValidatorAddress)
				require.Equal(t, int64(100), got.Votes[0].ValidatorPower)
				require.Equal(t, "BLOCK_ID_FLAG_COMMIT", got.Votes[0].BlockIDFlag)
				require.Equal(t, "1.250000000000000000", got.Votes[0].Rates["ausd"])
				require.Empty(t, got.Votes[1].Rates)
			},
		},
		{
			name:    "invalid source height",
			block:   &cmttypes.Block{},
			wantErr: "source block height must be positive",
		},
		{
			name: "missing metadata",
			block: &cmttypes.Block{
				Header: cmttypes.Header{Height: 42},
			},
			wantErr: "contains no vote-extension metadata",
		},
		{
			name: "malformed metadata",
			block: &cmttypes.Block{
				Header: cmttypes.Header{Height: 42},
				Data:   cmttypes.Data{Txs: cmttypes.Txs{[]byte("invalid")}},
			},
			wantErr: "decode vote-extension metadata",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := inspectVoteExtensions(tc.block)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}

			require.NoError(t, err)
			tc.check(t, got)
		})
	}
}

func TestVoteExtensionsCommand(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantHeight *int64
	}{
		{
			name:       "explicit block",
			args:       []string{"42"},
			wantHeight: heightPointer(42),
		},
		{
			name: "latest block",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			node := voteExtensionsTestRPC{
				block: func(_ context.Context, got *int64) (*coretypes.ResultBlock, error) {
					calls++
					if tc.wantHeight == nil {
						require.Nil(t, got)
					} else {
						require.NotNil(t, got)
						require.Equal(t, *tc.wantHeight, *got)
					}

					return &coretypes.ResultBlock{Block: voteExtensionsTestBlock(t, 42)}, nil
				},
			}

			var output bytes.Buffer
			clientCtx := client.Context{}.
				WithClient(node).
				WithOutput(&output).
				WithOutputFormat(flags.OutputFormatJSON)
			cmd := voteExtensionsCommand()
			cmd.SetContext(context.Background())
			require.NoError(t, client.SetCmdClientContext(cmd, clientCtx))

			require.NoError(t, runVoteExtensions(cmd, tc.args))

			var decoded voteExtensionsOutput
			require.NoError(t, json.Unmarshal(output.Bytes(), &decoded))
			require.Equal(t, int64(42), decoded.SourceBlockHeight)
			require.Equal(t, 1, calls)
		})
	}
}

func TestVoteExtensionsCommandRegistration(t *testing.T) {
	tests := []struct {
		name string
		path []string
	}{
		{
			name: "query vote extensions",
			path: []string{"vote-extensions"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			found, args, err := queryCommand().Find(tc.path)
			require.NoError(t, err)
			require.Empty(t, args)
			require.Equal(t, "vote-extensions", found.Name())
		})
	}
}

func voteExtensionsTestBlock(t *testing.T, height int64) *cmttypes.Block {
	t.Helper()

	rate, err := arkencoding.EncodeLegacyDec(math.LegacyMustNewDecFromStr("1.25"))
	require.NoError(t, err)
	voteExtension, err := codec.NewVoteExtensionCodec().Encode(vetypes.OracleVoteExtension{
		Rates: map[string][]byte{"ausd": rate},
	})
	require.NoError(t, err)
	extendedCommit, err := codec.EncodeExtendedCommit(cometabci.ExtendedCommitInfo{
		Round: 2,
		Votes: []cometabci.ExtendedVoteInfo{
			{
				Validator:     cometabci.Validator{Address: []byte{0x01, 0x02}, Power: 100},
				VoteExtension: voteExtension,
				BlockIdFlag:   cmtproto.BlockIDFlagCommit,
			},
			{
				Validator:   cometabci.Validator{Address: []byte{0x03, 0x04}, Power: 50},
				BlockIdFlag: cmtproto.BlockIDFlagAbsent,
			},
		},
	})
	require.NoError(t, err)

	return &cmttypes.Block{
		Header: cmttypes.Header{Height: height},
		Data:   cmttypes.Data{Txs: cmttypes.Txs{extendedCommit}},
	}
}

func heightPointer(height int64) *int64 {
	return &height
}

func stringHeight(height int64) string {
	return strconv.FormatInt(height, 10)
}

type voteExtensionsTestRPC struct {
	client.CometRPC
	block func(context.Context, *int64) (*coretypes.ResultBlock, error)
}

func (rpc voteExtensionsTestRPC) Block(ctx context.Context, height *int64) (*coretypes.ResultBlock, error) {
	return rpc.block(ctx, height)
}
