package codec

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"math/big"
	"math/rand"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	cmtabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	oracleencoding "ark/abci/oracle/encoding"
	vetypes "ark/abci/ve/types"
	oracletypes "ark/x/oracle/types"
)

func TestVoteExtensionCodec(t *testing.T) {
	c := NewVoteExtensionCodec()
	voteExtension := vetypes.OracleVoteExtension{
		Rates: map[string][]byte{
			"ukrw": []byte("2"),
			"uusd": []byte("1"),
		},
	}

	encoded, err := c.Encode(voteExtension)
	require.NoError(t, err)

	decoded, err := c.Decode(encoded)
	require.NoError(t, err)
	require.Equal(t, voteExtension.Rates, decoded.Rates)

	decoded, err = c.Decode(nil)
	require.NoError(t, err)
	require.Empty(t, decoded.Rates)
}

func TestExtendedCommitCodec(t *testing.T) {
	c := NewExtendedCommitCodec()
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

	encoded, err := c.Encode(extendedCommit)
	require.NoError(t, err)
	decoded, err := c.Decode(encoded)
	require.NoError(t, err)
	require.Equal(t, extendedCommit, decoded)

	decoded, err = c.Decode(nil)
	require.NoError(t, err)
	require.Empty(t, decoded.Votes)
}

func TestCodecsAccommodateMaximumOracleCapacity(t *testing.T) {
	t.Run("256-target vote extension", func(t *testing.T) {
		maxRateRaw := new(big.Int).Lsh(big.NewInt(1), 256)
		maxRateRaw.Mul(maxRateRaw, big.NewInt(1_000_000_000_000_000_000))
		maxRateRaw.Sub(maxRateRaw, big.NewInt(1))
		rate, err := oracleencoding.EncodeRate(
			math.LegacyNewDecFromBigIntWithPrec(maxRateRaw, math.LegacyPrecision),
		)
		require.NoError(t, err)

		voteExtension := vetypes.OracleVoteExtension{
			Rates: make(map[string][]byte, oracletypes.MaxVoteTargets),
		}
		for targetIndex := range oracletypes.MaxVoteTargets {
			denom := fmt.Sprintf("u%03d%s", targetIndex, strings.Repeat("a", 124))
			require.NoError(t, sdk.ValidateDenom(denom))
			voteExtension.Rates[denom] = rate
		}

		decoded, err := voteExtension.Marshal()
		require.NoError(t, err)
		require.LessOrEqual(t, len(decoded), maxVoteExtensionDecodedBytes)

		encoded, err := NewVoteExtensionCodec().Encode(voteExtension)
		require.NoError(t, err)
		require.LessOrEqual(t, len(encoded), maxVoteExtensionWireBytes)
	})

	t.Run("100-validator extended commit", func(t *testing.T) {
		const validatorCount = 100

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
				VoteExtension:      voteExtensions[start : start+maxVoteExtensionWireBytes],
				ExtensionSignature: bytes.Repeat([]byte{byte(validatorIndex)}, 64),
			}
		}

		require.LessOrEqual(t, extendedCommit.Size(), maxExtendedCommitDecodedBytes)

		encoded, err := NewExtendedCommitCodec().Encode(extendedCommit)
		require.NoError(t, err)
		require.LessOrEqual(t, len(encoded), maxExtendedCommitWireBytes)

		decoded, err := NewExtendedCommitCodec().Decode(encoded)
		require.NoError(t, err)
		require.Len(t, decoded.Votes, validatorCount)
	})
}

func TestCodecsRejectOversizedWirePayloads(t *testing.T) {
	t.Run("vote extension", func(t *testing.T) {
		_, err := NewVoteExtensionCodec().Decode(make([]byte, maxVoteExtensionWireBytes+1))
		require.ErrorContains(t, err, "compressed vote extension")
	})

	t.Run("extended commit", func(t *testing.T) {
		_, err := NewExtendedCommitCodec().Decode(make([]byte, maxExtendedCommitWireBytes+1))
		require.ErrorContains(t, err, "compressed extended commit")
	})
}

func TestCodecsBoundDecompressedOutput(t *testing.T) {
	t.Run("vote extension", func(t *testing.T) {
		var compressed bytes.Buffer
		writer := zlib.NewWriter(&compressed)
		_, err := writer.Write(bytes.Repeat([]byte("a"), maxVoteExtensionDecodedBytes+1))
		require.NoError(t, err)
		require.NoError(t, writer.Close())

		_, err = NewVoteExtensionCodec().Decode(compressed.Bytes())
		require.ErrorContains(t, err, "decompressed output size")
	})

	t.Run("extended commit", func(t *testing.T) {
		encoder, err := zstd.NewWriter(nil)
		require.NoError(t, err)
		t.Cleanup(func() { encoder.Close() })
		compressed := encoder.EncodeAll(bytes.Repeat([]byte("a"), maxExtendedCommitDecodedBytes+1), nil)

		_, err = NewExtendedCommitCodec().Decode(compressed)
		require.Error(t, err)
	})
}

func TestCodecsRejectOversizedDecodedPayloads(t *testing.T) {
	t.Run("vote extension", func(t *testing.T) {
		_, err := NewVoteExtensionCodec().Encode(vetypes.OracleVoteExtension{
			Rates: map[string][]byte{"uusd": bytes.Repeat([]byte("1"), maxVoteExtensionDecodedBytes)},
		})
		require.ErrorContains(t, err, "decoded vote extension")
	})

	t.Run("extended commit", func(t *testing.T) {
		_, err := NewExtendedCommitCodec().Encode(cmtabci.ExtendedCommitInfo{
			Votes: []cmtabci.ExtendedVoteInfo{{
				VoteExtension: bytes.Repeat([]byte("1"), maxExtendedCommitDecodedBytes),
			}},
		})
		require.ErrorContains(t, err, "decoded extended commit")
	})
}

func TestCodecsRejectMalformedCompressedPayloads(t *testing.T) {
	_, err := NewVoteExtensionCodec().Decode([]byte("not-zlib"))
	require.Error(t, err)

	_, err = NewExtendedCommitCodec().Decode([]byte("not-zstd"))
	require.Error(t, err)
}
