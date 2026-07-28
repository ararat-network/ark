package oracle

import (
	"errors"
	"fmt"
	"slices"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/math"

	"ark/abci/codec"
	arkabci "ark/abci/types"
	vetypes "ark/abci/voteextension/types"
	arkencoding "ark/pkg/encoding"
	oracletypes "ark/x/oracle/types"
)

// VoteRate is a decoded oracle rate keyed by its canonical target index.
type VoteRate struct {
	TargetIndex int
	Value       math.LegacyDec
}

// Vote is the decoded oracle payload associated with one validator entry from
// the extended commit info injected into the proposal.
type Vote struct {
	// Validator is the CometBFT validator metadata from ExtendedVoteInfo.
	// Address is the consensus address and Power is the consensus voting power
	// for this commit.
	Validator cometabci.Validator

	// Rates contains validated domain rates keyed by canonical target index.
	// Non-positive values are retained as abstentions: they never enter ballots
	// and do not count as participation. It is nil when the validator was
	// absent or submitted an invalid payload.
	Rates []VoteRate
}

// ParseVoteExtension converts transport rate bytes into domain rates.
func ParseVoteExtension(voteExtension vetypes.OracleVoteExtension) (map[string]math.LegacyDec, error) {
	if len(voteExtension.Rates) > oracletypes.MaxVoteTargets {
		return nil, fmt.Errorf(
			"number of oracle vote extension rates %d exceeds maximum %d",
			len(voteExtension.Rates),
			oracletypes.MaxVoteTargets,
		)
	}

	rates := make(map[string]math.LegacyDec, len(voteExtension.Rates))
	for denom, rawRate := range voteExtension.Rates {
		rate, err := arkencoding.DecodeLegacyDec(rawRate)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid oracle vote extension rate for denom %s: %w",
				denom,
				err,
			)
		}
		rates[denom] = rate
	}

	return rates, nil
}

// ValidateVoteExtension parses a transport vote extension, checks that it
// belongs to the target epoch required for its vote height, and returns rates
// keyed by canonical target index.
func ValidateVoteExtension(
	voteExtension vetypes.OracleVoteExtension,
	targets oracletypes.VoteTargetSet,
) ([]VoteRate, error) {
	if targets.Version == 0 {
		return nil, errors.New("expected vote-target version must be positive")
	}
	if voteExtension.TargetVersion != targets.Version {
		return nil, fmt.Errorf(
			"oracle vote extension target version %d does not match expected version %d",
			voteExtension.TargetVersion,
			targets.Version,
		)
	}
	if len(voteExtension.Rates) > oracletypes.MaxVoteTargets {
		return nil, fmt.Errorf(
			"number of oracle vote extension rates %d exceeds maximum %d",
			len(voteExtension.Rates),
			oracletypes.MaxVoteTargets,
		)
	}

	rates := make([]VoteRate, 0, len(voteExtension.Rates))
	for denom, rawRate := range voteExtension.Rates {
		targetIndex, found := slices.BinarySearch(targets.Denoms, denom)
		if !found {
			return nil, fmt.Errorf(
				"oracle vote extension target %s is not in expected targets %v",
				denom,
				targets.Denoms,
			)
		}
		rate, err := arkencoding.DecodeLegacyDec(rawRate)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid oracle vote extension rate for denom %s: %w",
				denom,
				err,
			)
		}
		rates = append(rates, VoteRate{TargetIndex: targetIndex, Value: rate})
	}

	return rates, nil
}

// GetOracleVotes decodes the injected extended commit info from the proposal
// and returns one Vote per validator entry. Invalid individual payloads yield
// nil Rates and are treated like empty reports: the validator still counts
// toward total commit power and accrues attendance eligibility on functioning
// blocks, without making block finalisation fail. Extended-commit envelope
// errors remain fatal.
func GetOracleVotes(
	voteExtensionCodec *codec.VoteExtensionCodec,
	proposal [][]byte,
	targets oracletypes.VoteTargetSet,
	maxVotes int,
) ([]Vote, error) {
	if len(proposal) < arkabci.NumInjectedTxs {
		return nil, arkabci.ErrMissingCommitInfo
	}

	extendedCommitInfo, err := codec.DecodeExtendedCommit(proposal[arkabci.OracleInfoIndex], maxVotes)
	if err != nil {
		return nil, fmt.Errorf("%w: decode extended commit info: %w", arkabci.ErrCodec, err)
	}

	votes := make([]Vote, len(extendedCommitInfo.Votes))
	for i, voteInfo := range extendedCommitInfo.Votes {
		votes[i].Validator = voteInfo.Validator
		if len(voteInfo.VoteExtension) == 0 {
			continue
		}

		voteExtension, err := voteExtensionCodec.Decode(voteInfo.VoteExtension)
		if err != nil {
			continue
		}
		rates, err := ValidateVoteExtension(voteExtension, targets)
		if err != nil {
			continue
		}
		votes[i].Rates = rates
	}

	return votes, nil
}
