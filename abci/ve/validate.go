package ve

import (
	"bytes"
	"errors"
	"fmt"
	"slices"

	cometabci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/core/comet"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// VoteExtensionsEnabled determines if vote extensions are enabled for the current block. If
// vote extensions are enabled at height h, then a proposer will receive vote extensions
// in height h+1. This is primarily used by modules that need to make state changes
// based on whether vote extensions were included in a proposal.
func VoteExtensionsEnabled(ctx sdk.Context) bool {
	cp := ctx.ConsensusParams()
	if cp.Abci == nil || cp.Abci.VoteExtensionsEnableHeight == 0 {
		return false
	}

	// Per the Cosmos SDK, the first block should not use the latest finalised block state. This means
	// vote extensions should NOT be making state changes.
	//
	// Ref: https://github.com/cosmos/cosmos-sdk/blob/2100a73dcea634ce914977dbddb4991a020ee345/baseapp/baseapp.go#L488-L495
	if ctx.BlockHeight() <= 1 {
		return false
	}

	return cp.Abci.VoteExtensionsEnableHeight < ctx.BlockHeight()
}

// ValidateVoteExtensionsFn defines the function for validating vote extensions. This
// function is not explicitly used to validate the oracle data but rather that
// the signed vote extensions included in the proposal are valid and provide
// a super-majority of vote extensions for the current block. This method is
// expected to be used in PrepareProposal and ProcessProposal.
type ValidateVoteExtensionsFn func(
	ctx sdk.Context,
	extInfo cometabci.ExtendedCommitInfo,
) error

// NewDefaultValidateVoteExtensionsFn returns a new DefaultValidateVoteExtensionsFn.
func NewDefaultValidateVoteExtensionsFn(validatorStore baseapp.ValidatorStore) ValidateVoteExtensionsFn {
	return func(ctx sdk.Context, info cometabci.ExtendedCommitInfo) error {
		if VoteExtensionsEnabled(ctx) {
			return ValidateVoteExtensions(ctx, validatorStore, info)
		}

		return nil
	}
}

// NoOpValidateVoteExtensions is a no-op validation method (purely used for testing).
func NoOpValidateVoteExtensions(
	_ sdk.Context,
	_ cometabci.ExtendedCommitInfo,
) error {
	return nil
}

// ValidateVoteExtensions defines a helper function for verifying vote extension
// signatures that may be passed or manually injected into a block proposal from
// a proposer in PrepareProposal. It returns an error if any signature is invalid
// or if unexpected vote extensions and/or signatures are found or less than 2/3
// power is received.
func ValidateVoteExtensions(
	ctx sdk.Context,
	valStore baseapp.ValidatorStore,
	extCommit cometabci.ExtendedCommitInfo,
) error {
	commitInfo := ctx.CometInfo().GetLastCommit()

	// Ark requires the injected commit to match consensus exactly, including
	// BlockID flags. The SDK helper intentionally does not enforce that last
	// invariant, so this check must run first.
	if err := ValidateExtendedCommitAgainstLastCommit(extCommit, commitInfo); err != nil {
		return err
	}

	// Keep Ark's stricter structural rule for non-commit entries. Signature,
	// public-key, and quorum validation are delegated to the SDK below.
	for _, vote := range extCommit.Votes {
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

	// SDK v0.54.2 reads height and chain ID from ctx; the two legacy arguments
	// are ignored. It owns canonical sign bytes, signature checks, and quorum.
	return baseapp.ValidateVoteExtensions(ctx, valStore, 0, "", extCommit)
}

// ValidateExtendedCommitAgainstLastCommit validates an ExtendedCommitInfo against a LastCommit. Specifically,
// it checks that the ExtendedCommit and LastCommit for the same height are consistent with each other and
// ordered correctly by voting power in accordance with
// [comet](https://github.com/cometbft/cometbft/blob/4ce0277b35f31985bbf2c25d3806a184a4510010/types/validator_set.go#L784).
func ValidateExtendedCommitAgainstLastCommit(ec cometabci.ExtendedCommitInfo, lc comet.CommitInfo) error {
	// Check that the rounds are the same.
	if ec.Round != lc.Round() {
		return fmt.Errorf("extended commit round %d does not match last commit round %d", ec.Round, lc.Round())
	}

	// Check that the number of votes is the same.
	if len(ec.Votes) != lc.Votes().Len() {
		return fmt.Errorf("extended commit votes length %d does not match last commit votes length %d", len(ec.Votes), lc.Votes().Len())
	}

	// Check sort order of extended commit votes.
	if !slices.IsSortedFunc(ec.Votes, func(vote1, vote2 cometabci.ExtendedVoteInfo) int {
		if vote1.Validator.Power == vote2.Validator.Power {
			return bytes.Compare(vote1.Validator.Address, vote2.Validator.Address) // addresses sorted in ascending order (used to break vp conflicts)
		}
		if vote1.Validator.Power > vote2.Validator.Power {
			return -1
		}
		return 1
	}) {
		return errors.New("extended commit votes are not sorted by voting power")
	}

	addressCache := make(map[string]struct{}, len(ec.Votes))
	// Check consistency between LastCommit and ExtendedCommit.
	for i, vote := range ec.Votes {
		// Cache addresses to check for duplicates.
		if _, ok := addressCache[string(vote.Validator.Address)]; ok {
			return fmt.Errorf("extended commit vote address %X is duplicated", vote.Validator.Address)
		}
		addressCache[string(vote.Validator.Address)] = struct{}{}

		lcVote := lc.Votes().Get(i)
		if !bytes.Equal(vote.Validator.Address, lcVote.Validator().Address()) {
			return fmt.Errorf("extended commit vote address %X does not match last commit vote address %X", vote.Validator.Address, lcVote.Validator().Address())
		}
		if vote.Validator.Power != lcVote.Validator().Power() {
			return fmt.Errorf("extended commit vote power %d does not match last commit vote power %d", vote.Validator.Power, lcVote.Validator().Power())
		}

		if int32(vote.BlockIdFlag) != int32(lcVote.GetBlockIDFlag()) {
			return fmt.Errorf("mismatched block ID flag between extended commit vote %d and last proposed commit %d", int32(vote.BlockIdFlag), int32(lcVote.GetBlockIDFlag()))
		}
	}

	return nil
}
