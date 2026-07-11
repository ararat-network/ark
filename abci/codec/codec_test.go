package codec_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	cmtabci "github.com/cometbft/cometbft/abci/types"

	compression "ark/abci/codec"
	vetypes "ark/abci/ve/types"
)

func TestDefaultVoteExtensionCodec(t *testing.T) {
	t.Run("test encoding / decoding", func(t *testing.T) {
		// create a sample vote extension
		ve := vetypes.OracleVoteExtension{
			Rates: map[string][]byte{
				"uusd": []byte("1"),
				"ukrw": []byte("2"),
			},
		}
		// encode it
		codec := compression.NewDefaultVoteExtensionCodec()
		bz, err := codec.Encode(ve)
		require.NoError(t, err)

		// decode it
		decodedVe, err := codec.Decode(bz)
		require.NoError(t, err)

		// make sure it's the same
		require.Equal(t, ve.Rates, decodedVe.Rates)
	})

	t.Run("test decoding empty byte array", func(t *testing.T) {
		codec := compression.NewDefaultVoteExtensionCodec()
		_, err := codec.Decode([]byte{})
		require.Nil(t, err)
	})
}

func TestCompressionVoteExtensionCodec(t *testing.T) {
	t.Run("test encoding / decoding", func(t *testing.T) {
		// create a sample vote extension
		samplePrice := []byte("nocapongodskiptoonicewititshiiiiiiiii")
		ve := vetypes.OracleVoteExtension{
			Rates: make(map[string][]byte),
		}

		// add 200 prices
		for i := range uint64(200) {
			denom := fmt.Sprintf("u%d", i)
			ve.Rates[denom] = samplePrice
		}

		// create a codec
		defaultCodec := compression.NewDefaultVoteExtensionCodec()
		codec := compression.NewCompressionVoteExtensionCodec(
			defaultCodec,
			compression.NewZLibCompressor(compression.MaxVoteExtensionDecodedBytes),
		)

		// encode it
		bz, err := codec.Encode(ve)
		require.NoError(t, err)

		defaultBz, err := defaultCodec.Encode(ve)
		require.NoError(t, err)

		// make sure it's smaller
		require.True(t, len(bz) < len(defaultBz))

		// decode it
		decodedVe, err := codec.Decode(bz)
		require.NoError(t, err)

		// make sure it's the same
		require.Equal(t, ve.Rates, decodedVe.Rates)
	})

	t.Run("test decoding empty byte array", func(t *testing.T) {
		codec := compression.NewCompressionVoteExtensionCodec(
			compression.NewDefaultVoteExtensionCodec(),
			compression.NewZLibCompressor(compression.MaxVoteExtensionDecodedBytes),
		)
		_, err := codec.Decode([]byte{})
		require.Nil(t, err)
	})
}

func TestDefaultExtendedCommitCodec(t *testing.T) {
	t.Run("test encoding / decoding", func(t *testing.T) {
		// create a sample extended commit info
		eci := cmtabci.ExtendedCommitInfo{
			Round: 1,
			Votes: []cmtabci.ExtendedVoteInfo{
				{
					Validator: cmtabci.Validator{
						Address: []byte("1"),
						Power:   10,
					},
					VoteExtension:      []byte("1"),
					ExtensionSignature: []byte("1"),
				},
			},
		}

		// encode it
		codec := compression.NewDefaultExtendedCommitCodec()
		bz, err := codec.Encode(eci)
		require.NoError(t, err)

		// decode it
		decodedEci, err := codec.Decode(bz)
		require.NoError(t, err)

		// make sure it's the same
		require.Equal(t, eci, decodedEci)
	})

	t.Run("test decoding empty byte array", func(t *testing.T) {
		codec := compression.NewDefaultExtendedCommitCodec()
		_, err := codec.Decode([]byte{})
		require.Nil(t, err)
	})
}

func TestCompressionExtendedCommitCodec(t *testing.T) {
	t.Run("test encoding / decoding", func(t *testing.T) {
		// create a sample extended commit info
		eci := cmtabci.ExtendedCommitInfo{
			Round: 1,
			Votes: []cmtabci.ExtendedVoteInfo{
				{
					Validator: cmtabci.Validator{
						Address: []byte("1"),
						Power:   10,
					},
					VoteExtension:      []byte("1"),
					ExtensionSignature: []byte("1"),
				},
			},
		}

		// create a codec
		defaultCodec := compression.NewDefaultExtendedCommitCodec()
		codec := compression.NewCompressionExtendedCommitCodec(
			defaultCodec,
			compression.NewZStdCompressor(compression.MaxExtendedCommitDecodedBytes),
		)

		// encode it
		bz, err := codec.Encode(eci)
		require.NoError(t, err)

		// decode it
		decodedEci, err := codec.Decode(bz)
		require.NoError(t, err)

		// make sure it's the same
		require.Equal(t, eci, decodedEci)
	})

	t.Run("test decoding empty byte array", func(t *testing.T) {
		codec := compression.NewCompressionExtendedCommitCodec(
			compression.NewDefaultExtendedCommitCodec(),
			compression.NewZStdCompressor(compression.MaxExtendedCommitDecodedBytes),
		)
		_, err := codec.Decode([]byte{})
		require.NoError(t, err)
	})
}

func TestZLibCompressorBoundsDecompressedOutput(t *testing.T) {
	const maxOutputBytes = int64(1024)
	compressor := compression.NewZLibCompressor(maxOutputBytes)

	t.Run("accepts exact limit", func(t *testing.T) {
		want := bytes.Repeat([]byte("a"), int(maxOutputBytes))
		compressed, err := compressor.Compress(want)
		require.NoError(t, err)

		got, err := compressor.Decompress(compressed)
		require.NoError(t, err)
		require.Equal(t, want, got)
	})

	t.Run("rejects output above limit", func(t *testing.T) {
		compressed, err := compressor.Compress(bytes.Repeat([]byte("a"), int(maxOutputBytes)+1))
		require.NoError(t, err)

		_, err = compressor.Decompress(compressed)
		require.ErrorContains(t, err, "exceeds maximum")
	})

	t.Run("rejects malformed input", func(t *testing.T) {
		_, err := compressor.Decompress([]byte("not-zlib"))
		require.Error(t, err)
	})
}

func TestZStdCompressorBoundsDecompressedOutput(t *testing.T) {
	const maxOutputBytes = int64(64 << 10)
	compressor := compression.NewZStdCompressor(maxOutputBytes)

	t.Run("accepts exact limit", func(t *testing.T) {
		want := bytes.Repeat([]byte("a"), int(maxOutputBytes))
		compressed, err := compressor.Compress(want)
		require.NoError(t, err)

		got, err := compressor.Decompress(compressed)
		require.NoError(t, err)
		require.Equal(t, want, got)
	})

	t.Run("rejects output above limit", func(t *testing.T) {
		compressed, err := compressor.Compress(bytes.Repeat([]byte("a"), int(maxOutputBytes)+1))
		require.NoError(t, err)

		_, err = compressor.Decompress(compressed)
		require.Error(t, err)
	})

	t.Run("rejects malformed input", func(t *testing.T) {
		_, err := compressor.Decompress([]byte("not-zstd"))
		require.Error(t, err)
	})
}

func TestCompressionCodecsRejectOversizedWirePayloads(t *testing.T) {
	t.Run("vote extension", func(t *testing.T) {
		codec := compression.NewCompressionVoteExtensionCodec(
			compression.NewDefaultVoteExtensionCodec(),
			compression.NewZLibCompressor(compression.MaxVoteExtensionDecodedBytes),
		)

		_, err := codec.Decode(make([]byte, compression.MaxVoteExtensionWireBytes+1))
		require.ErrorContains(t, err, "compressed vote extension")
	})

	t.Run("extended commit", func(t *testing.T) {
		codec := compression.NewCompressionExtendedCommitCodec(
			compression.NewDefaultExtendedCommitCodec(),
			compression.NewZStdCompressor(compression.MaxExtendedCommitDecodedBytes),
		)

		_, err := codec.Decode(make([]byte, compression.MaxExtendedCommitWireBytes+1))
		require.ErrorContains(t, err, "compressed extended commit")
	})
}

func TestCompressionCodecsRejectOversizedEncodedPayloads(t *testing.T) {
	t.Run("vote extension", func(t *testing.T) {
		codec := compression.NewCompressionVoteExtensionCodec(
			compression.NewDefaultVoteExtensionCodec(),
			stubCompressor{compressed: make([]byte, compression.MaxVoteExtensionWireBytes+1)},
		)

		_, err := codec.Encode(vetypes.OracleVoteExtension{})
		require.ErrorContains(t, err, "compressed vote extension")
	})

	t.Run("extended commit", func(t *testing.T) {
		codec := compression.NewCompressionExtendedCommitCodec(
			compression.NewDefaultExtendedCommitCodec(),
			stubCompressor{compressed: make([]byte, compression.MaxExtendedCommitWireBytes+1)},
		)

		_, err := codec.Encode(cmtabci.ExtendedCommitInfo{})
		require.ErrorContains(t, err, "compressed extended commit")
	})
}

type stubCompressor struct {
	compressed []byte
}

func (s stubCompressor) Compress([]byte) ([]byte, error) {
	return s.compressed, nil
}

func (stubCompressor) Decompress([]byte) ([]byte, error) {
	return nil, nil
}
