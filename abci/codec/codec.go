package codec

import (
	"fmt"

	"google.golang.org/protobuf/encoding/protowire"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/types"

	vetypes "github.com/ararat-network/ark/abci/voteextension/types"
	"github.com/ararat-network/ark/pkg/chain"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// Vote-extension wire limits derive from the accepted rate and target bounds.
// These constants affect consensus acceptance; see abci/oracle/README.md,
// "Encoding boundary", for their relationship to domain validation.
const (
	// rateEntryFramingBytes is the protobuf framing around one Rates map
	// entry: entry tag+length, key tag+length, and value tag+length, each one
	// byte while entries stay under 128 bytes.
	rateEntryFramingBytes = 6
	// versionFieldMaxBytes is the target-version field tag plus a maximal
	// uvarint64.
	versionFieldMaxBytes = 11

	maxVoteExtensionBytes = oracletypes.MaxFeeds*
		(chain.MaxPricedDenomBytes+oracletypes.MaxEncodedVoteRateBytes+rateEntryFramingBytes) +
		versionFieldMaxBytes
)

// EncodeVoteExtension encodes an Ark oracle vote extension as bounded
// protobuf.
func EncodeVoteExtension(voteExtension vetypes.OracleVoteExtension) ([]byte, error) {
	encoded, err := voteExtension.Marshal()
	if err != nil {
		return nil, err
	}
	if len(encoded) > maxVoteExtensionBytes {
		return nil, fmt.Errorf(
			"encoded vote extension size %d exceeds maximum %d",
			len(encoded),
			maxVoteExtensionBytes,
		)
	}

	return encoded, nil
}

// DecodeVoteExtension decodes a bounded protobuf oracle vote extension.
func DecodeVoteExtension(encoded []byte) (vetypes.OracleVoteExtension, error) {
	if len(encoded) > maxVoteExtensionBytes {
		return vetypes.OracleVoteExtension{}, fmt.Errorf(
			"encoded vote extension size %d exceeds maximum %d",
			len(encoded),
			maxVoteExtensionBytes,
		)
	}
	if len(encoded) == 0 {
		return vetypes.OracleVoteExtension{}, nil
	}

	var voteExtension vetypes.OracleVoteExtension
	return voteExtension, voteExtension.Unmarshal(encoded)
}

// EncodeExtendedCommit encodes CometBFT extended commit info as protobuf.
func EncodeExtendedCommit(extendedCommit cmtabci.ExtendedCommitInfo) ([]byte, error) {
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
func DecodeExtendedCommit(encoded []byte, maxVotes int) (cmtabci.ExtendedCommitInfo, error) {
	if maxVotes < 0 || maxVotes > cmttypes.MaxVotesCount {
		return cmtabci.ExtendedCommitInfo{}, fmt.Errorf(
			"extended commit vote limit %d is outside range [0, %d]",
			maxVotes,
			cmttypes.MaxVotesCount,
		)
	}
	if len(encoded) == 0 {
		return cmtabci.ExtendedCommitInfo{}, nil
	}
	if err := preflightExtendedCommit(encoded, maxVotes); err != nil {
		return cmtabci.ExtendedCommitInfo{}, err
	}

	var extendedCommit cmtabci.ExtendedCommitInfo
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
