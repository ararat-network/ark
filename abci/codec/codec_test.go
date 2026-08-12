package codec

import (
	"bytes"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/types"

	"cosmossdk.io/math"

	vetypes "ark/abci/voteextension/types"
	chain "ark/pkg/chain"
	arkencoding "ark/pkg/encoding"
	oracletypes "ark/x/oracle/types"
)

func TestVoteExtensionCodec(t *testing.T) {
	voteExtension := vetypes.OracleVoteExtension{
		Rates: map[string][]byte{
			"akrw": []byte("2"),
			"ausd": []byte("1"),
		},
		TargetVersion: oracletypes.InitialFeedVersion,
	}

	encoded, err := EncodeVoteExtension(voteExtension)
	require.NoError(t, err)

	decoded, err := DecodeVoteExtension(encoded)
	require.NoError(t, err)
	require.Equal(t, voteExtension, decoded)

	decoded, err = DecodeVoteExtension(nil)
	require.NoError(t, err)
	require.Empty(t, decoded.Rates)
}

// The wire limit is derived from the zstd frame format; the encoder derives
// the same bound from its own options. Cross-checking the two catches both a
// domain bound leaving the ranges the derivation assumes and a dependency
// upgrade changing the worst case, either of which moves consensus acceptance.
func TestVoteExtensionWireLimitMatchesEncoderWorstCase(t *testing.T) {
	require.Equal(
		t,
		maxVoteExtensionWireBytes,
		zstdEncoder.MaxEncodedSize(maxVoteExtensionDecodedBytes),
	)
}

func TestExtendedCommitCodec(t *testing.T) {
	extendedCommit := cmtabci.ExtendedCommitInfo{
		Round: 1,
		Votes: []cmtabci.ExtendedVoteInfo{
			{
				Validator:          cmtabci.Validator{Address: []byte("1"), Power: 10},
				VoteExtension:      []byte("1"),
				ExtensionSignature: []byte("1"),
			},
		},
	}

	encoded, err := EncodeExtendedCommit(extendedCommit)
	require.NoError(t, err)
	expectedEncoded, err := extendedCommit.Marshal()
	require.NoError(t, err)
	require.Equal(t, expectedEncoded, encoded)

	decoded, err := DecodeExtendedCommit(encoded, len(extendedCommit.Votes))
	require.NoError(t, err)
	require.Equal(t, extendedCommit, decoded)

	decoded, err = DecodeExtendedCommit(nil, 0)
	require.NoError(t, err)
	require.Empty(t, decoded.Votes)
}

func TestExtendedCommitCodecBoundsVoteCount(t *testing.T) {
	const maxVotes = 3

	atLimit := bytes.Repeat([]byte{0x12, 0x00}, maxVotes)
	decoded, err := DecodeExtendedCommit(atLimit, maxVotes)
	require.NoError(t, err)
	require.Len(t, decoded.Votes, maxVotes)

	aboveLimit := append(bytes.Clone(atLimit), 0x12, 0x00)
	decoded, err = DecodeExtendedCommit(aboveLimit, maxVotes)
	require.ErrorContains(t, err, "extended commit vote count 4 exceeds maximum 3")
	require.Empty(t, decoded.Votes)
}

func TestExtendedCommitCodecRejectsInvalidVoteLimits(t *testing.T) {
	testCases := []int{-1, cmttypes.MaxVotesCount + 1}
	for _, maxVotes := range testCases {
		t.Run(fmt.Sprintf("max_votes_%d", maxVotes), func(t *testing.T) {
			_, err := DecodeExtendedCommit(nil, maxVotes)
			require.ErrorContains(t, err, "extended commit vote limit")
		})
	}
}

func TestExtendedCommitCodecRejectsMalformedWireBeforeUnmarshal(t *testing.T) {
	testCases := []struct {
		name    string
		encoded []byte
	}{
		{name: "missing vote length", encoded: []byte{0x12}},
		{name: "truncated vote length", encoded: []byte{0x12, 0x80}},
		{name: "vote length exceeds input", encoded: []byte{0x12, 0x02, 0x00}},
		{name: "vote has wrong wire type", encoded: []byte{0x10, 0x00}},
		{name: "round has wrong wire type", encoded: []byte{0x0a, 0x00}},
		{name: "overflowing tag", encoded: bytes.Repeat([]byte{0x80}, 10)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeExtendedCommit(tc.encoded, cmttypes.MaxVotesCount)
			require.Error(t, err)
		})
	}

	decoded, err := DecodeExtendedCommit([]byte{0x18, 0x01}, 0)
	require.NoError(t, err)
	require.Empty(t, decoded.Votes)
}

func TestExtendedCommitCodecRejectsExcessVotesWithBoundedAllocation(t *testing.T) {
	const attackPayloadBytes = 8 << 20

	encoded := bytes.Repeat([]byte{0x12, 0x00}, attackPayloadBytes/2)
	result := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, err := DecodeExtendedCommit(encoded, 100)
			if err == nil {
				b.Fatal("expected vote-count error")
			}
		}
	})

	require.Less(t, result.AllocedBytesPerOp(), int64(4<<10))
}

func TestExtendedCommitCodecRejectsExcessVotesOnEncode(t *testing.T) {
	_, err := EncodeExtendedCommit(cmtabci.ExtendedCommitInfo{
		Votes: make([]cmtabci.ExtendedVoteInfo, cmttypes.MaxVotesCount+1),
	})
	require.ErrorContains(t, err, "extended commit vote count")
}

func TestCodecsAccommodateMaximumOracleCapacity(t *testing.T) {
	t.Run("256-target vote extension", func(t *testing.T) {
		// The widest rate a vote may carry: MaxEncodedVoteRateBytes decimal
		// digits of raw price*10^18.
		maxRate, err := arkencoding.EncodeLegacyDec(
			math.LegacyMustNewDecFromStr("1" + strings.Repeat("0", 21)),
		)
		require.NoError(t, err)
		require.Len(t, maxRate, oracletypes.MaxEncodedVoteRateBytes)

		voteExtension := vetypes.OracleVoteExtension{
			Rates:         make(map[string][]byte, oracletypes.MaxFeeds),
			TargetVersion: oracletypes.InitialFeedVersion,
		}
		for targetIndex := range oracletypes.MaxFeeds {
			denom := fmt.Sprintf("a%015d", targetIndex)
			require.Len(t, denom, chain.MaxPricedDenomBytes)
			require.NoError(t, chain.ValidatePricedDenom(denom))
			voteExtension.Rates[denom] = maxRate
		}

		decoded, err := voteExtension.Marshal()
		require.NoError(t, err)
		require.LessOrEqual(t, len(decoded), maxVoteExtensionDecodedBytes)
		// The derived limit is tight: the maximal valid payload leaves
		// headroom only for the version varint width.
		require.Greater(t, len(decoded), maxVoteExtensionDecodedBytes-versionFieldMaxBytes)

		encoded, err := EncodeVoteExtension(voteExtension)
		require.NoError(t, err)
		require.LessOrEqual(t, len(encoded), maxVoteExtensionWireBytes)

		// The maximal payload must survive the round trip: it exercises the
		// encoder window against the decoder's window and memory bounds at the
		// exact size where those limits meet.
		roundTripped, err := DecodeVoteExtension(encoded)
		require.NoError(t, err)
		require.Equal(t, voteExtension, roundTripped)
	})

	t.Run("130-validator extended commit fits default block budget", func(t *testing.T) {
		// Terra operated with a governance-controlled 130-validator active set.
		const validatorCount = 130

		voteExtensions := make([]byte, validatorCount*maxVoteExtensionWireBytes)
		_, err := rand.New(rand.NewSource(1)).Read(voteExtensions)
		require.NoError(t, err)

		extendedCommit := cmtabci.ExtendedCommitInfo{
			Votes: make([]cmtabci.ExtendedVoteInfo, validatorCount),
		}
		for validatorIndex := range validatorCount {
			start := validatorIndex * maxVoteExtensionWireBytes
			extendedCommit.Votes[validatorIndex] = cmtabci.ExtendedVoteInfo{
				Validator: cmtabci.Validator{
					Address: bytes.Repeat([]byte{byte(validatorIndex)}, 20),
					Power:   1,
				},
				VoteExtension: voteExtensions[start : start+maxVoteExtensionWireBytes],
				// MaxDataBytes reserves MaxCommitSigBytes per validator, sized
				// from this same maximum; a smaller signature would understate
				// the payload the budget has to hold.
				ExtensionSignature: bytes.Repeat([]byte{byte(validatorIndex)}, cmttypes.MaxSignatureSize),
			}
		}

		encoded, err := EncodeExtendedCommit(extendedCommit)
		require.NoError(t, err)

		consensusParams := cmttypes.DefaultConsensusParams()
		maxDataBytes := cmttypes.MaxDataBytes(
			consensusParams.Block.MaxBytes,
			consensusParams.Evidence.MaxBytes,
			validatorCount,
		)
		require.LessOrEqual(
			t,
			cmttypes.ComputeProtoSizeForTxs([]cmttypes.Tx{encoded}),
			maxDataBytes,
		)

		decoded, err := DecodeExtendedCommit(encoded, validatorCount)
		require.NoError(t, err)
		require.Len(t, decoded.Votes, validatorCount)
	})
}

func TestVoteExtensionCodecRejectsOversizedWirePayload(t *testing.T) {
	_, err := DecodeVoteExtension(make([]byte, maxVoteExtensionWireBytes+1))
	require.ErrorContains(t, err, "compressed vote extension")
}

func TestVoteExtensionCodecRejectsDuplicateKeyPadding(t *testing.T) {
	entry, err := (&vetypes.OracleVoteExtension{
		Rates: map[string][]byte{"ausd": []byte("1")},
	}).Marshal()
	require.NoError(t, err)

	// Concatenated map entries decode to one small valid map, so duplicate
	// keys are pure wire padding; the decoded limit must stop them.
	padded := bytes.Repeat(entry, maxVoteExtensionDecodedBytes/len(entry)+1)

	_, err = DecodeVoteExtension(zstdEncoder.EncodeAll(padded, nil))
	require.ErrorContains(t, err, "decompressed output size")
}

func TestVoteExtensionCodecBoundsDecompressedOutput(t *testing.T) {
	oversized := bytes.Repeat([]byte("a"), maxVoteExtensionDecodedBytes+1)

	_, err := DecodeVoteExtension(zstdEncoder.EncodeAll(oversized, nil))
	require.ErrorContains(t, err, "decompressed output size")
}

func TestVoteExtensionCodecRejectsOversizedPayloadOnEncode(t *testing.T) {
	_, err := EncodeVoteExtension(vetypes.OracleVoteExtension{
		Rates: map[string][]byte{"ausd": bytes.Repeat([]byte("1"), maxVoteExtensionDecodedBytes)},
	})
	require.ErrorContains(t, err, "decoded vote extension")
}

func TestCodecsRejectMalformedPayloads(t *testing.T) {
	_, err := DecodeVoteExtension([]byte("not-zstd"))
	require.Error(t, err)

	_, err = DecodeExtendedCommit([]byte("not-protobuf"), cmttypes.MaxVotesCount)
	require.Error(t, err)
}

func FuzzDecodeExtendedCommitVoteLimit(f *testing.F) {
	f.Add([]byte(nil))
	f.Add([]byte{0x12, 0x00})
	f.Add(bytes.Repeat([]byte{0x12, 0x00}, 5))
	f.Add([]byte{0x12, 0x80})

	f.Fuzz(func(t *testing.T, encoded []byte) {
		decoded, err := DecodeExtendedCommit(encoded, 4)
		if err == nil && len(decoded.Votes) > 4 {
			t.Fatalf("decoded %d votes above limit 4", len(decoded.Votes))
		}
	})
}
