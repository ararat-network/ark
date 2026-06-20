package oracle

import (
	"fmt"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"noah/abci/codec"
	noahabci "noah/abci/types"
	vetypes "noah/abci/ve/types"
)

// Vote is the decoded oracle payload associated with one validator entry from
// the extended commit info injected into the proposal.
type Vote struct {
	// Validator is the CometBFT validator metadata from ExtendedVoteInfo.
	// Address is the consensus address and Power is the consensus voting power
	// for this commit.
	Validator cometabci.Validator

	// OracleVoteExtension is the decoded oracle vote-extension payload. It may
	// be empty when the validator submitted no rates or its extension was
	// absent or pruned before proposal injection.
	OracleVoteExtension vetypes.OracleVoteExtension
}

// GetOracleVotes decodes the injected extended commit info from the proposal
// and returns one Vote per validator entry. It preserves entries with empty
// vote extensions so their voting power can still count toward quorum
// denominators.
func GetOracleVotes(
	proposal [][]byte,
	veCodec codec.VoteExtensionCodec,
	extCommitCodec codec.ExtendedCommitCodec,
) ([]Vote, error) {
	if len(proposal) < noahabci.NumInjectedTxs {
		return nil, noahabci.MissingCommitInfoError{}
	}

	extendedCommitInfo, err := extCommitCodec.Decode(proposal[noahabci.OracleInfoIndex])
	if err != nil {
		return nil, noahabci.CodecError{
			Err: fmt.Errorf("error decoding extended-commit-info: %w", err),
		}
	}

	votes := make([]Vote, len(extendedCommitInfo.Votes))
	for i, voteInfo := range extendedCommitInfo.Votes {
		if len(voteInfo.VoteExtension) == 0 {
			votes[i] = Vote{
				Validator:           voteInfo.Validator,
				OracleVoteExtension: vetypes.OracleVoteExtension{},
			}
			continue
		}

		voteExtension, err := veCodec.Decode(voteInfo.VoteExtension)
		if err != nil {
			return nil, noahabci.CodecError{
				Err: fmt.Errorf("error decoding vote-extension: %w", err),
			}
		}

		votes[i] = Vote{
			Validator:           voteInfo.Validator,
			OracleVoteExtension: voteExtension,
		}
	}

	return votes, nil
}
