package ve

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"

	protoio "github.com/cosmos/gogoproto/io"
	"github.com/cosmos/gogoproto/proto"

	cometabci "github.com/cometbft/cometbft/abci/types"
	cryptoenc "github.com/cometbft/cometbft/crypto/encoding"
	cmtprotocrypto "github.com/cometbft/cometbft/proto/tendermint/crypto"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/core/comet"

	sdk "github.com/cosmos/cosmos-sdk/types"

	oracleencoding "ark/abci/oracle/encoding"
	vetypes "ark/abci/ve/types"
)

const MaxVoteExtensionRates = 1024

// ValidateOracleVoteExtension validates the vote extension provided by a validator.
func ValidateOracleVoteExtension(
	ctx sdk.Context,
	ve vetypes.OracleVoteExtension,
) error {
	if len(ve.Rates) > MaxVoteExtensionRates {
		return fmt.Errorf("number of oracle vote extension rates %d exceeds maximum %d", len(ve.Rates), MaxVoteExtensionRates)
	}

	for denom, rawRate := range ve.Rates {
		if rawRate == nil {
			return errors.New("nil oracle vote extension rate")
		}
		rate, err := oracleencoding.DecodeRate(rawRate)
		if err != nil {
			return fmt.Errorf("invalid oracle vote extension rate for denom %s: %w", denom, err)
		}
		canonical, err := oracleencoding.EncodeRate(rate)
		if err != nil {
			return fmt.Errorf("canonicalize oracle vote extension rate for denom %s: %w", denom, err)
		}
		if !bytes.Equal(rawRate, canonical) {
			return fmt.Errorf("non-canonical oracle vote extension rate for denom %s", denom)
		}
		if err := sdk.ValidateDenom(denom); err != nil {
			return fmt.Errorf("invalid oracle vote extension denom %s: %w", denom, err)
		}
	}

	return nil
}

// VoteExtensionsEnabled determines if vote extensions are enabled for the current block. If
// vote extensions are enabled at height h, then a proposer will receive vote extensions
// in height h+1. This is primarily used by modules that need to make state changes
// based on whether vote extensions were included in a proposal.
func VoteExtensionsEnabled(ctx sdk.Context) bool {
	cp := ctx.ConsensusParams()
	if cp.Abci == nil || cp.Abci.VoteExtensionsEnableHeight == 0 {
		return false
	}

	// Per the Cosmos SDK, the first block should not use the latest finalized block state. This means
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
func NewDefaultValidateVoteExtensionsFn(validatorStore ValidatorStore) ValidateVoteExtensionsFn {
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

// ValidatorStore defines the interface required for verifying vote
// extension signatures. Typically, this will be implemented by the x/staking
// module, which has knowledge of the CometBFT public key.
type ValidatorStore interface {
	GetPubKeyByConsAddr(context.Context, sdk.ConsAddress) (cmtprotocrypto.PublicKey, error)
}

// ValidateVoteExtensions defines a helper function for verifying vote extension
// signatures that may be passed or manually injected into a block proposal from
// a proposer in PrepareProposal. It returns an error if any signature is invalid
// or if unexpected vote extensions and/or signatures are found or less than 2/3
// power is received.
func ValidateVoteExtensions(
	ctx sdk.Context,
	valStore ValidatorStore,
	extCommit cometabci.ExtendedCommitInfo,
) error {
	currentHeight := ctx.HeaderInfo().Height
	chainID := ctx.HeaderInfo().ChainID
	commitInfo := ctx.CometInfo().GetLastCommit()

	// Check that both extCommit + commit are ordered in accordance with vp/address.
	if err := ValidateExtendedCommitAgainstLastCommit(extCommit, commitInfo); err != nil {
		return err
	}

	// Start checking vote extensions only **after** the vote extensions enable
	// height, because when `currentHeight == VoteExtensionsEnableHeight`
	// PrepareProposal doesn't get any vote extensions in its request.
	extensionsEnabled := VoteExtensionsEnabled(ctx)
	marshalDelimitedFn := func(msg proto.Message) ([]byte, error) {
		var buf bytes.Buffer
		if err := protoio.NewDelimitedWriter(&buf).WriteMsg(msg); err != nil {
			return nil, err
		}

		return buf.Bytes(), nil
	}

	var (
		// Total voting power of all validators in the extended commit.
		totalVP int64
		// Total voting power of commit votes with vote extension signatures.
		sumVP int64
	)

	for _, vote := range extCommit.Votes {
		totalVP += vote.Validator.Power

		if extensionsEnabled {
			if vote.BlockIdFlag == cmtproto.BlockIDFlagCommit && len(vote.ExtensionSignature) == 0 {
				return fmt.Errorf("vote extension signature is missing; validator addr %s",
					vote.Validator.String(),
				)
			}
			if vote.BlockIdFlag != cmtproto.BlockIDFlagCommit && len(vote.VoteExtension) != 0 {
				return fmt.Errorf("non-commit vote extension present; validator addr %s",
					vote.Validator.String(),
				)
			}
			if vote.BlockIdFlag != cmtproto.BlockIDFlagCommit && len(vote.ExtensionSignature) != 0 {
				return fmt.Errorf("non-commit vote extension signature present; validator addr %s",
					vote.Validator.String(),
				)
			}
		} else { // vote extensions disabled
			if len(vote.VoteExtension) != 0 {
				return fmt.Errorf("vote extension present but extensions disabled; validator addr %s",
					vote.Validator.String(),
				)
			}
			if len(vote.ExtensionSignature) != 0 {
				return fmt.Errorf("vote extension signature present but extensions disabled; validator addr %s",
					vote.Validator.String(),
				)
			}

			continue
		}

		// Only verify signatures for commit votes. There must be a super-majority,
		// otherwise the previous block could not have been committed.
		if vote.BlockIdFlag != cmtproto.BlockIDFlagCommit {
			continue
		}

		// If the validator does not have a valid public key, skip signature
		// verification but still include its voting power. The app may have pruned
		// the validator's public key from the store, but CometBFT considered the
		// validator active when it included the validator in the commit.
		sumVP += vote.Validator.Power
		valConsAddr := sdk.ConsAddress(vote.Validator.Address)
		pubKeyProto, err := valStore.GetPubKeyByConsAddr(ctx, valConsAddr)
		if err != nil {
			continue
		}

		cmtPubKey, err := cryptoenc.PubKeyFromProto(pubKeyProto)
		if err != nil {
			return fmt.Errorf("failed to convert validator %X public key: %w", valConsAddr, err)
		}

		cve := cmtproto.CanonicalVoteExtension{
			Extension: vote.VoteExtension,
			Height:    currentHeight - 1, // the vote extension was signed in the previous height
			Round:     int64(extCommit.Round),
			ChainId:   chainID,
		}

		extSignBytes, err := marshalDelimitedFn(&cve)
		if err != nil {
			return fmt.Errorf("failed to encode CanonicalVoteExtension: %w", err)
		}

		if !cmtPubKey.VerifySignature(extSignBytes, vote.ExtensionSignature) {
			return fmt.Errorf("failed to verify validator %X vote extension signature", valConsAddr)
		}
	}

	// This check is probably unnecessary, but better safe than sorry.
	if totalVP <= 0 {
		return fmt.Errorf("total voting power must be positive, got: %d", totalVP)
	}

	if extensionsEnabled {
		// Require at least 2/3 + 1 voting power with valid vote extensions.
		if requiredVP := ((totalVP * 2) / 3) + 1; sumVP < requiredVP {
			return fmt.Errorf(
				"insufficient cumulative voting power received to verify vote extensions; got: %d, expected: >=%d",
				sumVP, requiredVP,
			)
		}
	}

	return nil
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
		return -int(vote1.Validator.Power - vote2.Validator.Power) // vp sorted in descending order
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

		// Only check non-absent votes, which may have been modified by PrepareProposal pruning.
		if !(vote.BlockIdFlag == cmtproto.BlockIDFlagAbsent && len(vote.VoteExtension) == 0 && len(vote.ExtensionSignature) == 0) {
			if int32(vote.BlockIdFlag) != int32(lcVote.GetBlockIDFlag()) {
				return fmt.Errorf("mismatched block ID flag between extended commit vote %d and last proposed commit %d", int32(vote.BlockIdFlag), int32(lcVote.GetBlockIDFlag()))
			}
		}
	}

	return nil
}
