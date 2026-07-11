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

var enc, _ = zstd.NewWriter(nil)

// VoteExtensionCodec is the interface for encoding and decoding vote extensions.
type VoteExtensionCodec interface {
	// Encode encodes the vote extension into a byte array.
	Encode(ve vetypes.OracleVoteExtension) ([]byte, error)

	// Decode decodes the vote extension from a byte array.
	Decode([]byte) (vetypes.OracleVoteExtension, error)
}

// ExtendedCommitCodec is the interface for encoding and decoding extended commit info.
type ExtendedCommitCodec interface {
	// Encode encodes the extended commit info into a byte array.
	Encode(cometabci.ExtendedCommitInfo) ([]byte, error)

	// Decode decodes the extended commit info from a byte array.
	Decode([]byte) (cometabci.ExtendedCommitInfo, error)
}

// NewDefaultVoteExtensionCodec returns a new DefaultVoteExtensionCodec.

func NewDefaultVoteExtensionCodec() *DefaultVoteExtensionCodec {
	return &DefaultVoteExtensionCodec{}
}

// DefaultVoteExtensionCodec is the default implementation of VoteExtensionCodec.
// It uses the generated Marshal and Unmarshal methods.
type DefaultVoteExtensionCodec struct{}

func (codec *DefaultVoteExtensionCodec) Encode(ve vetypes.OracleVoteExtension) ([]byte, error) {
	bz, err := ve.Marshal()
	if err != nil {
		return nil, err
	}
	if err := validatePayloadSize("decoded vote extension", len(bz), MaxVoteExtensionDecodedBytes); err != nil {
		return nil, err
	}

	return bz, nil
}

func (codec *DefaultVoteExtensionCodec) Decode(bz []byte) (vetypes.OracleVoteExtension, error) {
	if err := validatePayloadSize("decoded vote extension", len(bz), MaxVoteExtensionDecodedBytes); err != nil {
		return vetypes.OracleVoteExtension{}, err
	}

	var ve vetypes.OracleVoteExtension
	return ve, ve.Unmarshal(bz)
}

type Compressor interface {
	Compress([]byte) ([]byte, error)
	Decompress([]byte) ([]byte, error)
}

// ZLibCompressor uses zlib and bounds decompressed output to protect callers
// from compressed payloads that expand beyond their resource envelope.
type ZLibCompressor struct {
	maxOutputBytes int64
}

// NewZLibCompressor returns a new ZLibCompressor.
func NewZLibCompressor(maxOutputBytes int64) *ZLibCompressor {
	return &ZLibCompressor{maxOutputBytes: maxOutputBytes}
}

// Compress compresses the given byte array using zlib. It returns an error if the compression fails.
func (c *ZLibCompressor) Compress(bz []byte) ([]byte, error) {
	var b bytes.Buffer

	w := zlib.NewWriter(&b)

	// write and flush the buffer
	if _, err := w.Write(bz); err != nil {
		_ = w.Close()
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

// Decompress decompresses the given byte array using zlib. It returns an error if the decompression fails.
func (c *ZLibCompressor) Decompress(bz []byte) ([]byte, error) {
	if len(bz) == 0 {
		return nil, nil
	}
	if err := validateMaxOutputBytes(c.maxOutputBytes); err != nil {
		return nil, err
	}
	r, err := zlib.NewReader(bytes.NewReader(bz))
	if err != nil {
		return nil, err
	}
	defer r.Close()

	return readLimited(r, c.maxOutputBytes)
}

// ZStdCompressor uses zstd and bounds both the decoder window and decompressed
// output. Separate instances should be used for payloads with different limits.
type ZStdCompressor struct {
	maxOutputBytes int64
}

func NewZStdCompressor(maxOutputBytes int64) *ZStdCompressor {
	return &ZStdCompressor{maxOutputBytes: maxOutputBytes}
}

func (c *ZStdCompressor) Compress(bz []byte) ([]byte, error) {
	return enc.EncodeAll(bz, nil), nil
}

func (c *ZStdCompressor) Decompress(bz []byte) ([]byte, error) {
	if len(bz) == 0 {
		return nil, nil
	}
	if err := validateMaxOutputBytes(c.maxOutputBytes); err != nil {
		return nil, err
	}

	r, err := zstd.NewReader(
		bytes.NewReader(bz),
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderLowmem(true),
		zstd.WithDecoderMaxMemory(uint64(c.maxOutputBytes)),
		zstd.WithDecoderMaxWindow(uint64(c.maxOutputBytes)),
	)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	return readLimited(r, c.maxOutputBytes)
}

// CompressionVoteExtensionCodec compresses encoded vote extensions and
// decompresses them before decoding.
type CompressionVoteExtensionCodec struct {
	codec      VoteExtensionCodec
	compressor Compressor
}

// NewCompressionVoteExtensionCodec returns a new CompressionVoteExtensionCodec given an underlying codec.
func NewCompressionVoteExtensionCodec(codec VoteExtensionCodec, compressor Compressor) *CompressionVoteExtensionCodec {
	return &CompressionVoteExtensionCodec{
		codec:      codec,
		compressor: compressor,
	}
}

// Encode returns the encoded vote extension using the underlying codec and then
// compresses the result.
func (codec *CompressionVoteExtensionCodec) Encode(ve vetypes.OracleVoteExtension) ([]byte, error) {
	bz, err := codec.codec.Encode(ve)
	if err != nil {
		return nil, err
	}
	if err := validatePayloadSize("decoded vote extension", len(bz), MaxVoteExtensionDecodedBytes); err != nil {
		return nil, err
	}

	bz, err = codec.compressor.Compress(bz)
	if err != nil {
		return nil, err
	}
	if err := validatePayloadSize("compressed vote extension", len(bz), MaxVoteExtensionWireBytes); err != nil {
		return nil, err
	}

	return bz, nil
}

// Decode decompresses the vote extension and then decodes the result using the
// underlying codec.
func (codec *CompressionVoteExtensionCodec) Decode(bz []byte) (vetypes.OracleVoteExtension, error) {
	if err := validatePayloadSize("compressed vote extension", len(bz), MaxVoteExtensionWireBytes); err != nil {
		return vetypes.OracleVoteExtension{}, err
	}

	// Decompress first.
	bz, err := codec.compressor.Decompress(bz)
	if err != nil {
		return vetypes.OracleVoteExtension{}, err
	}
	if err := validatePayloadSize("decoded vote extension", len(bz), MaxVoteExtensionDecodedBytes); err != nil {
		return vetypes.OracleVoteExtension{}, err
	}

	return codec.codec.Decode(bz)
}

// DefaultExtendedCommitCodec is the default implementation of ExtendedCommitCodec.
// It uses the generated Marshal and Unmarshal methods.
type DefaultExtendedCommitCodec struct{}

// NewDefaultExtendedCommitCodec returns a new DefaultExtendedCommitCodec.
func NewDefaultExtendedCommitCodec() *DefaultExtendedCommitCodec {
	return &DefaultExtendedCommitCodec{}
}

func (codec *DefaultExtendedCommitCodec) Encode(extendedCommitInfo cometabci.ExtendedCommitInfo) ([]byte, error) {
	bz, err := extendedCommitInfo.Marshal()
	if err != nil {
		return nil, err
	}
	if err := validatePayloadSize("decoded extended commit", len(bz), MaxExtendedCommitDecodedBytes); err != nil {
		return nil, err
	}

	return bz, nil
}

func (codec *DefaultExtendedCommitCodec) Decode(bz []byte) (cometabci.ExtendedCommitInfo, error) {
	if len(bz) == 0 {
		return cometabci.ExtendedCommitInfo{}, nil
	}
	if err := validatePayloadSize("decoded extended commit", len(bz), MaxExtendedCommitDecodedBytes); err != nil {
		return cometabci.ExtendedCommitInfo{}, err
	}

	var extendedCommitInfo cometabci.ExtendedCommitInfo
	return extendedCommitInfo, extendedCommitInfo.Unmarshal(bz)
}

// CompressionExtendedCommitCodec compresses encoded extended commit info and
// decompresses it before decoding.
type CompressionExtendedCommitCodec struct {
	codec      ExtendedCommitCodec
	compressor Compressor
}

// NewCompressionExtendedCommitCodec returns a new CompressionExtendedCommitCodec given an underlying codec.
func NewCompressionExtendedCommitCodec(codec ExtendedCommitCodec, compressor Compressor) *CompressionExtendedCommitCodec {
	return &CompressionExtendedCommitCodec{
		codec:      codec,
		compressor: compressor,
	}
}

// Encode returns the encoded extended commit info using the underlying codec and
// then compresses the result.
func (codec *CompressionExtendedCommitCodec) Encode(extendedCommitInfo cometabci.ExtendedCommitInfo) ([]byte, error) {
	bz, err := codec.codec.Encode(extendedCommitInfo)
	if err != nil {
		return nil, err
	}
	if err := validatePayloadSize("decoded extended commit", len(bz), MaxExtendedCommitDecodedBytes); err != nil {
		return nil, err
	}

	bz, err = codec.compressor.Compress(bz)
	if err != nil {
		return nil, err
	}
	if err := validatePayloadSize("compressed extended commit", len(bz), MaxExtendedCommitWireBytes); err != nil {
		return nil, err
	}

	return bz, nil
}

// Decode decompresses the extended commit info and then decodes the result using
// the underlying codec.
func (codec *CompressionExtendedCommitCodec) Decode(bz []byte) (cometabci.ExtendedCommitInfo, error) {
	if err := validatePayloadSize("compressed extended commit", len(bz), MaxExtendedCommitWireBytes); err != nil {
		return cometabci.ExtendedCommitInfo{}, err
	}

	// Decompress first.
	bz, err := codec.compressor.Decompress(bz)
	if err != nil {
		return cometabci.ExtendedCommitInfo{}, err
	}
	if err := validatePayloadSize("decoded extended commit", len(bz), MaxExtendedCommitDecodedBytes); err != nil {
		return cometabci.ExtendedCommitInfo{}, err
	}

	return codec.codec.Decode(bz)
}

func validatePayloadSize(name string, size, maximum int) error {
	if size > maximum {
		return fmt.Errorf("%s size %d exceeds maximum %d", name, size, maximum)
	}

	return nil
}

func validateMaxOutputBytes(maxOutputBytes int64) error {
	if maxOutputBytes <= 0 {
		return fmt.Errorf("maximum decompressed output must be positive, got %d", maxOutputBytes)
	}

	return nil
}

func readLimited(r io.Reader, maxOutputBytes int64) ([]byte, error) {
	limited := &io.LimitedReader{R: r, N: maxOutputBytes + 1}
	bz, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(bz)) > maxOutputBytes {
		return nil, fmt.Errorf("decompressed output size %d exceeds maximum %d", len(bz), maxOutputBytes)
	}

	return bz, nil
}
