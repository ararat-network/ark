package codec

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"sync"

	"google.golang.org/protobuf/encoding/protowire"

	cometabci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/types"

	vetypes "ark/abci/voteextension/types"
	chain "ark/pkg/chain"
	oracletypes "ark/x/oracle/types"
)

// The vote-extension byte limits derive from the domain bounds, so the codec
// admits exactly the payloads validation could accept and padding of any kind
// gains nothing: duplicate-key wire entries no longer fit under the decoded
// limit, and deflate stored-block padding no longer fits under the wire limit.
// Every constant below is consensus-relevant through VerifyVoteExtension
// acceptance and moves in lockstep with the domain bounds it derives from.
const (
	// rateEntryFramingBytes is the protobuf framing around one Rates map
	// entry: entry tag+length, key tag+length, and value tag+length, each one
	// byte while entries stay under 128 bytes.
	rateEntryFramingBytes = 6
	// versionFieldMaxBytes is the target-version field tag plus a maximal
	// uvarint64.
	versionFieldMaxBytes = 11

	maxVoteExtensionDecodedBytes = oracletypes.MaxFeeds*
		(chain.MaxPricedDenomBytes+oracletypes.MaxEncodedVoteRateBytes+rateEntryFramingBytes) +
		versionFieldMaxBytes

	// zlibEnvelopeBytes is the zlib header plus the Adler-32 trailer;
	// zlibStoredBlockBytes is the header of one stored (uncompressed) deflate
	// block, the least compact encoding zlib can legally emit.
	zlibEnvelopeBytes    = 6
	zlibStoredBlockBytes = 5
	zlibStoredBlockLimit = 65535

	maxVoteExtensionWireBytes = maxVoteExtensionDecodedBytes + zlibEnvelopeBytes +
		zlibStoredBlockBytes*(maxVoteExtensionDecodedBytes/zlibStoredBlockLimit+1)
)

type resettableZlibReader interface {
	io.ReadCloser
	zlib.Resetter
}

// VoteExtensionCodec encodes and decodes Ark oracle vote extensions while
// retaining reusable compression state. It is safe for concurrent use but must
// not be copied after first use.
type VoteExtensionCodec struct {
	encodeMu sync.Mutex
	writer   *zlib.Writer

	readerPool sync.Pool
	bufferPool sync.Pool
}

// NewVoteExtensionCodec returns Ark's bounded protobuf-plus-zlib vote-extension
// codec.
func NewVoteExtensionCodec() *VoteExtensionCodec {
	return &VoteExtensionCodec{
		writer: zlib.NewWriter(io.Discard),
		bufferPool: sync.Pool{
			New: func() any {
				return new(bytes.Buffer)
			},
		},
	}
}

// Encode encodes an Ark oracle vote extension as bounded protobuf compressed
// with zlib.
func (c *VoteExtensionCodec) Encode(voteExtension vetypes.OracleVoteExtension) ([]byte, error) {
	decoded, err := voteExtension.Marshal()
	if err != nil {
		return nil, err
	}
	if len(decoded) > maxVoteExtensionDecodedBytes {
		return nil, fmt.Errorf(
			"decoded vote extension size %d exceeds maximum %d",
			len(decoded),
			maxVoteExtensionDecodedBytes,
		)
	}

	var encoded bytes.Buffer
	c.encodeMu.Lock()
	c.writer.Reset(&encoded)
	defer func() {
		c.writer.Reset(io.Discard)
		c.encodeMu.Unlock()
	}()
	if _, err := c.writer.Write(decoded); err != nil {
		return nil, err
	}
	if err := c.writer.Close(); err != nil {
		return nil, err
	}
	if encoded.Len() > maxVoteExtensionWireBytes {
		return nil, fmt.Errorf(
			"compressed vote extension size %d exceeds maximum %d",
			encoded.Len(),
			maxVoteExtensionWireBytes,
		)
	}

	return encoded.Bytes(), nil
}

// Decode decodes a bounded protobuf-plus-zlib oracle vote extension.
func (c *VoteExtensionCodec) Decode(encoded []byte) (vetypes.OracleVoteExtension, error) {
	if len(encoded) > maxVoteExtensionWireBytes {
		return vetypes.OracleVoteExtension{}, fmt.Errorf(
			"compressed vote extension size %d exceeds maximum %d",
			len(encoded),
			maxVoteExtensionWireBytes,
		)
	}
	if len(encoded) == 0 {
		return vetypes.OracleVoteExtension{}, nil
	}

	reader, err := c.acquireReader(encoded)
	if err != nil {
		return vetypes.OracleVoteExtension{}, err
	}

	decodedBuffer := c.bufferPool.Get().(*bytes.Buffer)
	decodedBuffer.Reset()
	defer c.releaseBuffer(decodedBuffer)
	limited := &io.LimitedReader{R: reader, N: maxVoteExtensionDecodedBytes + 1}
	if _, err := decodedBuffer.ReadFrom(limited); err != nil {
		_ = reader.Close()
		return vetypes.OracleVoteExtension{}, err
	}
	if decodedBuffer.Len() > maxVoteExtensionDecodedBytes {
		_ = reader.Close()
		return vetypes.OracleVoteExtension{}, fmt.Errorf(
			"decompressed output size %d exceeds maximum %d",
			decodedBuffer.Len(),
			maxVoteExtensionDecodedBytes,
		)
	}
	if err := reader.Close(); err != nil {
		return vetypes.OracleVoteExtension{}, err
	}
	c.readerPool.Put(reader)

	var voteExtension vetypes.OracleVoteExtension
	return voteExtension, voteExtension.Unmarshal(decodedBuffer.Bytes())
}

func (c *VoteExtensionCodec) acquireReader(encoded []byte) (resettableZlibReader, error) {
	source := bytes.NewReader(encoded)
	pooled := c.readerPool.Get()
	if pooled != nil {
		reader := pooled.(resettableZlibReader)
		if err := reader.Reset(source, nil); err != nil {
			_ = reader.Close()
			return nil, err
		}
		return reader, nil
	}

	reader, err := zlib.NewReader(source)
	if err != nil {
		return nil, err
	}
	return reader.(resettableZlibReader), nil
}

func (c *VoteExtensionCodec) releaseBuffer(buffer *bytes.Buffer) {
	if buffer.Cap() > maxVoteExtensionDecodedBytes {
		return
	}
	buffer.Reset()
	c.bufferPool.Put(buffer)
}

// EncodeExtendedCommit encodes CometBFT extended commit info as protobuf.
func EncodeExtendedCommit(extendedCommit cometabci.ExtendedCommitInfo) ([]byte, error) {
	if len(extendedCommit.Votes) > cmttypes.MaxVotesCount {
		return nil, fmt.Errorf(
			"extended commit vote count %d exceeds maximum %d",
			len(extendedCommit.Votes),
			cmttypes.MaxVotesCount,
		)
	}

	return extendedCommit.Marshal()
}

// DecodeExtendedCommit decodes protobuf extended commit info bounded by
// repeated vote cardinality. CometBFT bounds the containing proposal bytes.
func DecodeExtendedCommit(encoded []byte, maxVotes int) (cometabci.ExtendedCommitInfo, error) {
	if maxVotes < 0 || maxVotes > cmttypes.MaxVotesCount {
		return cometabci.ExtendedCommitInfo{}, fmt.Errorf(
			"extended commit vote limit %d is outside range [0, %d]",
			maxVotes,
			cmttypes.MaxVotesCount,
		)
	}
	if len(encoded) == 0 {
		return cometabci.ExtendedCommitInfo{}, nil
	}
	if err := preflightExtendedCommit(encoded, maxVotes); err != nil {
		return cometabci.ExtendedCommitInfo{}, err
	}

	var extendedCommit cometabci.ExtendedCommitInfo
	return extendedCommit, extendedCommit.Unmarshal(encoded)
}

func preflightExtendedCommit(encoded []byte, maxVotes int) error {
	voteCount := 0
	for len(encoded) > 0 {
		fieldNumber, wireType, tagBytes := protowire.ConsumeTag(encoded)
		if tagBytes < 0 {
			return fmt.Errorf("invalid extended commit protobuf tag: %w", protowire.ParseError(tagBytes))
		}

		switch fieldNumber {
		case 1:
			if wireType != protowire.VarintType {
				return fmt.Errorf("invalid extended commit round wire type %d", wireType)
			}
		case 2:
			if wireType != protowire.BytesType {
				return fmt.Errorf("invalid extended commit votes wire type %d", wireType)
			}
			voteCount++
			if voteCount > maxVotes {
				return fmt.Errorf(
					"extended commit vote count %d exceeds maximum %d",
					voteCount,
					maxVotes,
				)
			}
		}

		valueBytes := protowire.ConsumeFieldValue(fieldNumber, wireType, encoded[tagBytes:])
		if valueBytes < 0 {
			return fmt.Errorf("invalid extended commit protobuf field: %w", protowire.ParseError(valueBytes))
		}
		encoded = encoded[tagBytes+valueBytes:]
	}

	return nil
}
