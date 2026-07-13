package codec

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"

	cometabci "github.com/cometbft/cometbft/abci/types"

	vetypes "ark/abci/ve/types"
)

var zstdEncoder = func() *zstd.Encoder {
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		panic(fmt.Errorf("create zstd encoder: %w", err))
	}
	return encoder
}()

// VoteExtensionCodec encodes and decodes Ark oracle vote extensions.
type VoteExtensionCodec interface {
	Encode(vetypes.OracleVoteExtension) ([]byte, error)
	Decode([]byte) (vetypes.OracleVoteExtension, error)
}

// ExtendedCommitCodec encodes and decodes CometBFT extended commit info.
type ExtendedCommitCodec interface {
	Encode(cometabci.ExtendedCommitInfo) ([]byte, error)
	Decode([]byte) (cometabci.ExtendedCommitInfo, error)
}

// NewVoteExtensionCodec returns Ark's bounded protobuf-plus-zlib vote-extension codec.
func NewVoteExtensionCodec() VoteExtensionCodec {
	return voteExtensionCodec{}
}

// NewExtendedCommitCodec returns Ark's bounded protobuf-plus-zstd extended-commit codec.
func NewExtendedCommitCodec() ExtendedCommitCodec {
	return extendedCommitCodec{}
}

type voteExtensionCodec struct{}

func (voteExtensionCodec) Encode(voteExtension vetypes.OracleVoteExtension) ([]byte, error) {
	decoded, err := voteExtension.Marshal()
	if err != nil {
		return nil, err
	}
	if err := validatePayloadSize("decoded vote extension", len(decoded), maxVoteExtensionDecodedBytes); err != nil {
		return nil, err
	}

	encoded, err := compressZlib(decoded)
	if err != nil {
		return nil, err
	}
	if err := validatePayloadSize("compressed vote extension", len(encoded), maxVoteExtensionWireBytes); err != nil {
		return nil, err
	}

	return encoded, nil
}

func (voteExtensionCodec) Decode(encoded []byte) (vetypes.OracleVoteExtension, error) {
	if err := validatePayloadSize("compressed vote extension", len(encoded), maxVoteExtensionWireBytes); err != nil {
		return vetypes.OracleVoteExtension{}, err
	}

	decoded, err := decompressZlib(encoded, maxVoteExtensionDecodedBytes)
	if err != nil {
		return vetypes.OracleVoteExtension{}, err
	}

	var voteExtension vetypes.OracleVoteExtension
	return voteExtension, voteExtension.Unmarshal(decoded)
}

type extendedCommitCodec struct{}

func (extendedCommitCodec) Encode(extendedCommit cometabci.ExtendedCommitInfo) ([]byte, error) {
	decoded, err := extendedCommit.Marshal()
	if err != nil {
		return nil, err
	}
	if err := validatePayloadSize("decoded extended commit", len(decoded), maxExtendedCommitDecodedBytes); err != nil {
		return nil, err
	}

	encoded := zstdEncoder.EncodeAll(decoded, nil)
	if err := validatePayloadSize("compressed extended commit", len(encoded), maxExtendedCommitWireBytes); err != nil {
		return nil, err
	}

	return encoded, nil
}

func (extendedCommitCodec) Decode(encoded []byte) (cometabci.ExtendedCommitInfo, error) {
	if err := validatePayloadSize("compressed extended commit", len(encoded), maxExtendedCommitWireBytes); err != nil {
		return cometabci.ExtendedCommitInfo{}, err
	}

	decoded, err := decompressZstd(encoded, maxExtendedCommitDecodedBytes)
	if err != nil {
		return cometabci.ExtendedCommitInfo{}, err
	}
	if len(decoded) == 0 {
		return cometabci.ExtendedCommitInfo{}, nil
	}

	var extendedCommit cometabci.ExtendedCommitInfo
	return extendedCommit, extendedCommit.Unmarshal(decoded)
}

func compressZlib(decoded []byte) ([]byte, error) {
	var encoded bytes.Buffer
	writer := zlib.NewWriter(&encoded)
	if _, err := writer.Write(decoded); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	return encoded.Bytes(), nil
}

func decompressZlib(encoded []byte, maxOutputBytes int64) ([]byte, error) {
	if len(encoded) == 0 {
		return nil, nil
	}

	reader, err := zlib.NewReader(bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return readLimited(reader, maxOutputBytes)
}

func decompressZstd(encoded []byte, maxOutputBytes int64) ([]byte, error) {
	if len(encoded) == 0 {
		return nil, nil
	}

	reader, err := zstd.NewReader(
		bytes.NewReader(encoded),
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderLowmem(true),
		zstd.WithDecoderMaxMemory(uint64(maxOutputBytes)),
		zstd.WithDecoderMaxWindow(uint64(maxOutputBytes)),
	)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return readLimited(reader, maxOutputBytes)
}

func validatePayloadSize(name string, size, maximum int) error {
	if size > maximum {
		return fmt.Errorf("%s size %d exceeds maximum %d", name, size, maximum)
	}

	return nil
}

func readLimited(reader io.Reader, maxOutputBytes int64) ([]byte, error) {
	limited := &io.LimitedReader{R: reader, N: maxOutputBytes + 1}
	decoded, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(decoded)) > maxOutputBytes {
		return nil, fmt.Errorf("decompressed output size %d exceeds maximum %d", len(decoded), maxOutputBytes)
	}

	return decoded, nil
}
