package oracle

import (
	"fmt"

	cometabci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"ark/abci/codec"
	arkabci "ark/abci/types"
	vetypes "ark/abci/ve/types"
)

// Vote is the decoded oracle payload associated with one validator entry from
// the extended commit info injected into the proposal.
type Vote struct {
	// Validator is the CometBFT validator metadata from ExtendedVoteInfo.
	// Address is the consensus address and Power is the consensus voting power
	// for this commit.
	Validator cometabci.Validator

	// BlockIDFlag records whether the validator committed, voted nil, or was absent.
	BlockIDFlag cmtproto.BlockIDFlag

	// OracleVoteExtension is the decoded oracle vote-extension payload. It is
	// empty when the validator submitted no rates, was absent, or submitted an
	// invalid payload.
	OracleVoteExtension vetypes.OracleVoteExtension
}

// GetOracleVotes decodes the injected extended commit info from the proposal
// and returns one Vote per validator entry. Invalid individual payloads are
// classified as empty reports so they remain accountable without making block
// finalization fail. Extended-commit envelope errors remain fatal.
func GetOracleVotes(
	proposal [][]byte,
	veCodec codec.VoteExtensionCodec,
	extCommitCodec codec.ExtendedCommitCodec,
) ([]Vote, error) {
	if len(proposal) < arkabci.NumInjectedTxs {
		return nil, arkabci.MissingCommitInfoError{}
	}

	extendedCommitInfo, err := extCommitCodec.Decode(proposal[arkabci.OracleInfoIndex])
	if err != nil {
		return nil, arkabci.CodecError{
			Err: fmt.Errorf("error decoding extended-commit-info: %w", err),
		}
	}

	votes := make([]Vote, len(extendedCommitInfo.Votes))
	for i, voteInfo := range extendedCommitInfo.Votes {
		votes[i] = Vote{
			Validator:           voteInfo.Validator,
			BlockIDFlag:         voteInfo.BlockIdFlag,
			OracleVoteExtension: classifyVoteExtension(voteInfo.VoteExtension, veCodec),
		}
	}

	return votes, nil
}

func classifyVoteExtension(
	rawVoteExtension []byte,
	veCodec codec.VoteExtensionCodec,
) vetypes.OracleVoteExtension {
	if len(rawVoteExtension) == 0 {
		return vetypes.OracleVoteExtension{}
	}

	voteExtension, err := veCodec.Decode(rawVoteExtension)
	if err != nil {
		return vetypes.OracleVoteExtension{}
	}
	if err := ValidateVoteExtension(voteExtension); err != nil {
		return vetypes.OracleVoteExtension{}
	}

	return voteExtension
}
