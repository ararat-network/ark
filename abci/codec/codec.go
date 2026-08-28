package codec

import (
	"errors"
	"fmt"

	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/encoding/protowire"

	cometabci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/types"

	vetypes "github.com/ararat-network/ark/abci/voteextension/types"
	chain "github.com/ararat-network/ark/pkg/chain"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// The vote-extension byte limits derive from the domain bounds, so the codec
// admits exactly the payloads validation could accept and padding of any kind
// gains nothing: duplicate-key wire entries no longer fit under the decoded
// limit, and raw-block padding no longer fits under the wire limit. Every
// constant below is consensus-relevant through VerifyVoteExtension acceptance
// and moves in lockstep with the domain bounds it derives from.
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

	// zstdWindowSize is the smallest power of two above the decoded limit. It
	// bounds encoder state, the window any accepted frame may declare, and the
	// raw-block payload cap in the wire-limit derivation below.
	zstdWindowSize = 1 << 15

	// The zstd frame envelope (RFC 8878) under this codec's encoder options:
	// frame magic number, frame-header descriptor plus window descriptor, the
	// content-size field (two bytes while the decoded limit stays within
	// [256, 65791]), and the xxhash64 content checksum.
	zstdMagicBytes       = 4
	zstdFrameHeaderBytes = 2
	zstdContentSizeBytes = 2
	zstdChecksumBytes    = 4
	zstdEnvelopeBytes    = zstdMagicBytes + zstdFrameHeaderBytes +
		zstdContentSizeBytes + zstdChecksumBytes

	// zstdRawBlockHeaderBytes is the header of one raw (uncompressed) block,
	// the least compact encoding zstd can legally emit. Blocks carry at most
	// min(zstdWindowSize, 128 KiB) payload bytes — the window size here.
	zstdRawBlockHeaderBytes = 3
	zstdMaxBlockBytes       = zstdWindowSize

	maxVoteExtensionWireBytes = maxVoteExtensionDecodedBytes + zstdEnvelopeBytes +
		zstdRawBlockHeaderBytes*(maxVoteExtensionDecodedBytes/zstdMaxBlockBytes+1)
)

// The encoder and decoder hold no per-call state: EncodeAll and DecodeAll are
// safe for concurrent use, so the codec needs no locking or pooling of its own.
var (
	zstdEncoder = newZstdEncoder()
	zstdDecoder = newZstdDecoder()
)

func newZstdEncoder() *zstd.Encoder {
	encoder, err := zstd.NewWriter(
		nil,
		zstd.WithEncoderConcurrency(1),
		zstd.WithWindowSize(zstdWindowSize),
	)
	if err != nil {
		panic(fmt.Sprintf("construct vote-extension zstd encoder: %v", err))
	}

	return encoder
}

func newZstdDecoder() *zstd.Decoder {
	decoder, err := zstd.NewReader(
		nil,
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderMaxMemory(maxVoteExtensionDecodedBytes),
		zstd.WithDecoderMaxWindow(zstdWindowSize),
	)
	if err != nil {
		panic(fmt.Sprintf("construct vote-extension zstd decoder: %v", err))
	}

	return decoder
}

// EncodeVoteExtension encodes an Ark oracle vote extension as bounded protobuf
// compressed with zstd.
func EncodeVoteExtension(voteExtension vetypes.OracleVoteExtension) ([]byte, error) {
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

	encoded := zstdEncoder.EncodeAll(decoded, nil)
	if len(encoded) > maxVoteExtensionWireBytes {
		return nil, fmt.Errorf(
			"compressed vote extension size %d exceeds maximum %d",
			len(encoded),
			maxVoteExtensionWireBytes,
		)
	}

	return encoded, nil
}

// DecodeVoteExtension decodes a bounded protobuf-plus-zstd oracle vote
// extension.
func DecodeVoteExtension(encoded []byte) (vetypes.OracleVoteExtension, error) {
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

	// The decoder rejects oversized frames from their declared content size,
	// and bounds unknown-size frames as it decompresses; the length check
	// below covers the decoded output either way.
	decoded, err := zstdDecoder.DecodeAll(encoded, nil)
	if err != nil {
		if errors.Is(err, zstd.ErrDecoderSizeExceeded) {
			return vetypes.OracleVoteExtension{}, fmt.Errorf(
				"decompressed output size exceeds maximum %d",
				maxVoteExtensionDecodedBytes,
			)
		}

		return vetypes.OracleVoteExtension{}, err
	}
	if len(decoded) > maxVoteExtensionDecodedBytes {
		return vetypes.OracleVoteExtension{}, fmt.Errorf(
			"decompressed output size %d exceeds maximum %d",
			len(decoded),
			maxVoteExtensionDecodedBytes,
		)
	}

	var voteExtension vetypes.OracleVoteExtension
	return voteExtension, voteExtension.Unmarshal(decoded)
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
