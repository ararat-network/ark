package codec

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	vetypes "ark/abci/voteextension/types"
)

func TestVoteExtensionCodecReusePreservesWireEncoding(t *testing.T) {
	codec := NewVoteExtensionCodec()
	first := vetypes.OracleVoteExtension{
		Rates:         map[string][]byte{"uone": []byte("first-rate")},
		TargetVersion: 1,
	}
	second := vetypes.OracleVoteExtension{
		Rates:         map[string][]byte{"utwo": []byte("second-rate")},
		TargetVersion: 2,
	}

	expectedFirst := encodeVoteExtensionWithoutReuse(t, first)
	encodedFirst, err := codec.Encode(first)
	require.NoError(t, err)
	require.Equal(t, expectedFirst, encodedFirst)

	encodedSecond, err := codec.Encode(second)
	require.NoError(t, err)
	decodedSecond, err := codec.Decode(encodedSecond)
	require.NoError(t, err)
	require.Equal(t, second, decodedSecond)

	encodedFirst, err = codec.Encode(first)
	require.NoError(t, err)
	require.Equal(t, expectedFirst, encodedFirst)
	decodedFirst, err := codec.Decode(encodedFirst)
	require.NoError(t, err)
	require.Equal(t, first, decodedFirst)
}

func TestDecodeVoteExtensionOwnsRateBytesAfterBufferReuse(t *testing.T) {
	codec := NewVoteExtensionCodec()
	first := vetypes.OracleVoteExtension{
		Rates: map[string][]byte{"uone": []byte("first-rate")},
	}
	encodedFirst, err := codec.Encode(first)
	require.NoError(t, err)
	decodedFirst, err := codec.Decode(encodedFirst)
	require.NoError(t, err)

	for i := range 10 {
		second := vetypes.OracleVoteExtension{
			Rates: map[string][]byte{
				"utwo": bytes.Repeat([]byte{byte(i + 1)}, 1_024),
			},
		}
		encodedSecond, err := codec.Encode(second)
		require.NoError(t, err)
		decodedSecond, err := codec.Decode(encodedSecond)
		require.NoError(t, err)
		require.Equal(t, second, decodedSecond)
	}

	require.Equal(t, []byte("first-rate"), decodedFirst.Rates["uone"])
}

func TestVoteExtensionCodecReuseAfterMalformedInput(t *testing.T) {
	codec := NewVoteExtensionCodec()
	voteExtension := vetypes.OracleVoteExtension{
		Rates:         map[string][]byte{"uone": []byte("rate")},
		TargetVersion: 1,
	}
	encoded, err := codec.Encode(voteExtension)
	require.NoError(t, err)
	decoded, err := codec.Decode(encoded)
	require.NoError(t, err)
	require.Equal(t, voteExtension, decoded)

	_, err = codec.Decode([]byte("not-zlib"))
	require.Error(t, err)

	var oversized bytes.Buffer
	writer := zlib.NewWriter(&oversized)
	_, err = writer.Write(bytes.Repeat([]byte("a"), maxVoteExtensionDecodedBytes+1))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	_, err = codec.Decode(oversized.Bytes())
	require.ErrorContains(t, err, "decompressed output size")

	decoded, err = codec.Decode(encoded)
	require.NoError(t, err)
	require.Equal(t, voteExtension, decoded)
}

func TestVoteExtensionCodecReuseConcurrent(t *testing.T) {
	const (
		goroutineCount = 16
		iterationCount = 50
	)
	codec := NewVoteExtensionCodec()

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
				encoded, err := codec.Encode(want)
				if err != nil {
					errors <- fmt.Errorf("encode: %w", err)
					return
				}
				got, err := codec.Decode(encoded)
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

func encodeVoteExtensionWithoutReuse(t *testing.T, voteExtension vetypes.OracleVoteExtension) []byte {
	t.Helper()

	decoded, err := voteExtension.Marshal()
	require.NoError(t, err)

	var encoded bytes.Buffer
	writer := zlib.NewWriter(&encoded)
	_, err = writer.Write(decoded)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return encoded.Bytes()
}
