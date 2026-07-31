package codec

import (
	"bytes"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	vetypes "ark/abci/voteextension/types"
)

func TestVoteExtensionCodecReusePreservesWireEncoding(t *testing.T) {
	first := vetypes.OracleVoteExtension{
		Rates:         map[string][]byte{"uone": []byte("first-rate")},
		TargetVersion: 1,
	}
	second := vetypes.OracleVoteExtension{
		Rates:         map[string][]byte{"utwo": []byte("second-rate")},
		TargetVersion: 2,
	}

	expectedFirst := encodeVoteExtensionWithFreshEncoder(t, first)
	encodedFirst, err := EncodeVoteExtension(first)
	require.NoError(t, err)
	require.Equal(t, expectedFirst, encodedFirst)

	encodedSecond, err := EncodeVoteExtension(second)
	require.NoError(t, err)
	decodedSecond, err := DecodeVoteExtension(encodedSecond)
	require.NoError(t, err)
	require.Equal(t, second, decodedSecond)

	encodedFirst, err = EncodeVoteExtension(first)
	require.NoError(t, err)
	require.Equal(t, expectedFirst, encodedFirst)
	decodedFirst, err := DecodeVoteExtension(encodedFirst)
	require.NoError(t, err)
	require.Equal(t, first, decodedFirst)
}

// Finalisation decodes one vote extension per validator and retains every
// result, so decoded rate bytes must not alias storage that a later decode can
// overwrite.
func TestDecodeVoteExtensionOwnsReturnedRateBytes(t *testing.T) {
	first := vetypes.OracleVoteExtension{
		Rates: map[string][]byte{"uone": []byte("first-rate")},
	}
	encodedFirst, err := EncodeVoteExtension(first)
	require.NoError(t, err)
	decodedFirst, err := DecodeVoteExtension(encodedFirst)
	require.NoError(t, err)

	for i := range 10 {
		second := vetypes.OracleVoteExtension{
			Rates: map[string][]byte{
				"utwo": bytes.Repeat([]byte{byte(i + 1)}, 1_024),
			},
		}
		encodedSecond, err := EncodeVoteExtension(second)
		require.NoError(t, err)
		decodedSecond, err := DecodeVoteExtension(encodedSecond)
		require.NoError(t, err)
		require.Equal(t, second, decodedSecond)
	}

	require.Equal(t, []byte("first-rate"), decodedFirst.Rates["uone"])
}

func TestVoteExtensionCodecReuseAfterMalformedInput(t *testing.T) {
	voteExtension := vetypes.OracleVoteExtension{
		Rates:         map[string][]byte{"uone": []byte("rate")},
		TargetVersion: 1,
	}
	encoded, err := EncodeVoteExtension(voteExtension)
	require.NoError(t, err)
	decoded, err := DecodeVoteExtension(encoded)
	require.NoError(t, err)
	require.Equal(t, voteExtension, decoded)

	_, err = DecodeVoteExtension([]byte("not-zstd"))
	require.Error(t, err)

	oversized := bytes.Repeat([]byte("a"), maxVoteExtensionDecodedBytes+1)
	_, err = DecodeVoteExtension(zstdEncoder.EncodeAll(oversized, nil))
	require.ErrorContains(t, err, "decompressed output size")

	decoded, err = DecodeVoteExtension(encoded)
	require.NoError(t, err)
	require.Equal(t, voteExtension, decoded)
}

func TestVoteExtensionCodecReuseConcurrent(t *testing.T) {
	const (
		goroutineCount = 16
		iterationCount = 50
	)

	fixtures := []vetypes.OracleVoteExtension{
		{
			Rates:         map[string][]byte{"uone": []byte("first-rate")},
			TargetVersion: 1,
		},
		{
			Rates:         map[string][]byte{"utwo": bytes.Repeat([]byte("second-rate"), 32)},
			TargetVersion: 2,
		},
	}

	errors := make(chan error, goroutineCount)
	var waitGroup sync.WaitGroup
	for goroutineIndex := range goroutineCount {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for iteration := range iterationCount {
				want := fixtures[(goroutineIndex+iteration)%len(fixtures)]
				encoded, err := EncodeVoteExtension(want)
				if err != nil {
					errors <- fmt.Errorf("encode: %w", err)
					return
				}
				got, err := DecodeVoteExtension(encoded)
				if err != nil {
					errors <- fmt.Errorf("decode: %w", err)
					return
				}
				if !reflect.DeepEqual(want, got) {
					errors <- fmt.Errorf("decoded vote extension differs: want %v, got %v", want, got)
					return
				}
			}
		}()
	}
	waitGroup.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}
}

// encodeVoteExtensionWithFreshEncoder mirrors the codec's encoder options on a
// throwaway encoder, so the package-level encoder is held to byte-identical
// output regardless of how many extensions it has already encoded.
func encodeVoteExtensionWithFreshEncoder(t *testing.T, voteExtension vetypes.OracleVoteExtension) []byte {
	t.Helper()

	decoded, err := voteExtension.Marshal()
	require.NoError(t, err)

	encoder, err := zstd.NewWriter(
		nil,
		zstd.WithEncoderConcurrency(1),
		zstd.WithWindowSize(zstdWindowSize),
	)
	require.NoError(t, err)
	defer encoder.Close()

	return encoder.EncodeAll(decoded, nil)
}
