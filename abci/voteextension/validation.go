package voteextension

import (
	"fmt"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// VoteExtensionsAvailable reports whether the current block has a previous
// commit whose vote extensions should be processed.
func VoteExtensionsAvailable(ctx sdk.Context) bool {
	cp := ctx.ConsensusParams()
	if cp.Abci == nil ||
		cp.Abci.VoteExtensionsEnableHeight == 0 ||
		cp.Abci.VoteExtensionsEnableHeight >= ctx.BlockHeight() {
		return false
	}

	info := ctx.CometInfo()
	if info == nil {
		return false
	}

	commit := info.GetLastCommit()
	return commit != nil && commit.Votes().Len() > 0
}

// ValidateExtendedCommit verifies the extended commit injected into a block
// proposal. It returns an error if the commit does not match consensus, contains
// invalid or unexpected vote extensions, or represents less than 2/3 power.
func ValidateExtendedCommit(
	ctx sdk.Context,
	valStore baseapp.ValidatorStore,
	extCommit cmtabci.ExtendedCommitInfo,
) error {
	// The SDK owns structural consistency with LastCommit, canonical sign bytes,
	// signature checks, and quorum. In SDK v0.54 the legacy height and chain ID
	// arguments are ignored in favour of values read from ctx.
	if err := baseapp.ValidateVoteExtensions(ctx, valStore, 0, "", extCommit); err != nil {
		return err
	}

	// The SDK does not compare BlockID flags or inspect extension fields on
	// non-commit entries. Ark requires the injected commit to match consensus
	// exactly and forbids unauthenticated payloads on nil or absent votes.
	commitInfo := ctx.CometInfo().GetLastCommit()
	for i, vote := range extCommit.Votes {
		lastVote := commitInfo.Votes().Get(i)
		if int32(vote.BlockIdFlag) != int32(lastVote.GetBlockIDFlag()) {
			return fmt.Errorf("mismatched block ID flag between extended commit vote %d and last proposed commit %d", int32(vote.BlockIdFlag), int32(lastVote.GetBlockIDFlag()))
		}
		if vote.BlockIdFlag == cmtproto.BlockIDFlagCommit {
			continue
		}
		if len(vote.VoteExtension) != 0 {
			return fmt.Errorf("non-commit vote extension present; validator addr %s", vote.Validator.String())
		}
		if len(vote.ExtensionSignature) != 0 {
			return fmt.Errorf("non-commit vote extension signature present; validator addr %s", vote.Validator.String())
		}
	}

	return nil
}
