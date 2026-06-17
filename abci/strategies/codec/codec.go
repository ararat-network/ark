package codec

import (
	"bytes"
	"compress/zlib"
	"io"

	"github.com/klauspost/compress/zstd"

	cometabci "github.com/cometbft/cometbft/abci/types"

	vetypes "noah/abci/ve/types"
)

var (
	enc, _ = zstd.NewWriter(nil)
	dec, _ = zstd.NewReader(nil)
)

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
	return ve.Marshal()
}

func (codec *DefaultVoteExtensionCodec) Decode(bz []byte) (vetypes.OracleVoteExtension, error) {
	var ve vetypes.OracleVoteExtension
	return ve, ve.Unmarshal(bz)
}

type Compressor interface {
	Compress([]byte) ([]byte, error)
	Decompress([]byte) ([]byte, error)
}

// ZLibCompressor is a Compressor that uses zlib to compress and decompress byte arrays.
// This object is not thread-safe.
type ZLibCompressor struct{}

// NewZLibCompressor returns a new ZLibCompressor.
func NewZLibCompressor() *ZLibCompressor {
	return &ZLibCompressor{}
}

// Compress compresses the given byte array using zlib. It returns an error if the compression fails.
// This function is not thread-safe.
func (c *ZLibCompressor) Compress(bz []byte) ([]byte, error) {
	var b bytes.Buffer

	w := zlib.NewWriter(&b)
	defer w.Close()

	// write and flush the buffer
	if _, err := w.Write(bz); err != nil {
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
	r, err := zlib.NewReader(bytes.NewReader(bz))
	if err != nil {
		return nil, err
	}
	defer r.Close()

	return io.ReadAll(r)
}

// ZStdCompressor is a Compressor that uses zstd to compress and decompress byte arrays.
// This object is thread-safe.
type ZStdCompressor struct{}

func NewZStdCompressor() *ZStdCompressor {
	return &ZStdCompressor{}
}

func (c *ZStdCompressor) Compress(bz []byte) ([]byte, error) {
	return enc.EncodeAll(bz, nil), nil
}

func (c *ZStdCompressor) Decompress(bz []byte) ([]byte, error) {
	return dec.DecodeAll(bz, nil)
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

	return codec.compressor.Compress(bz)
}

// Decode decompresses the vote extension and then decodes the result using the
// underlying codec.
func (codec *CompressionVoteExtensionCodec) Decode(bz []byte) (vetypes.OracleVoteExtension, error) {
	// Decompress first.
	bz, err := codec.compressor.Decompress(bz)
	if err != nil {
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
	return extendedCommitInfo.Marshal()
}

func (codec *DefaultExtendedCommitCodec) Decode(bz []byte) (cometabci.ExtendedCommitInfo, error) {
	if len(bz) == 0 {
		return cometabci.ExtendedCommitInfo{}, nil
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

	return codec.compressor.Compress(bz)
}

// Decode decompresses the extended commit info and then decodes the result using
// the underlying codec.
func (codec *CompressionExtendedCommitCodec) Decode(bz []byte) (cometabci.ExtendedCommitInfo, error) {
	// Decompress first.
	bz, err := codec.compressor.Decompress(bz)
	if err != nil {
		return cometabci.ExtendedCommitInfo{}, err
	}

	return codec.codec.Decode(bz)
}
