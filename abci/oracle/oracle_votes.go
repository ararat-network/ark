package oracle

import (
	"errors"
	"fmt"
	"slices"

	cmtabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/abci/codec"
	arkabci "github.com/ararat-network/ark/abci/types"
	vetypes "github.com/ararat-network/ark/abci/voteextension/types"
	arkencoding "github.com/ararat-network/ark/pkg/encoding"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
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
	Validator cmtabci.Validator

	// Rates contains validated domain rates keyed by canonical target index.
	// Decoded rates are strictly positive; abstention is omission, so an
	// unpriced target is simply absent. It is nil when the validator was
	// absent or submitted an invalid payload.
	Rates []VoteRate

	// Invalid is true when the validator attached a payload that failed
	// decoding or domain validation. Aggregation grades an invalid report
	// exactly like an absent one; the flag feeds telemetry only and must never
	// influence consensus state.
	Invalid bool
}

// DecodeVoteRate decodes one vote-extension rate under the oracle's vote-rate
// size policy. Vote extensions carry prices, so a much shorter encoding than
// the full LegacyDec range is accepted; the tighter bound is what keeps the
// derived vote-extension capacity limits small.
func DecodeVoteRate(bz []byte) (math.LegacyDec, error) {
	if len(bz) > oracletypes.MaxEncodedVoteRateBytes {
		return math.LegacyDec{}, fmt.Errorf(
			"encoded oracle vote rate length %d exceeds maximum %d",
			len(bz),
			oracletypes.MaxEncodedVoteRateBytes,
		)
	}

	return arkencoding.DecodeCompactLegacyDec(bz)
}

// ParseVoteExtension converts transport rate bytes into domain rates.
func ParseVoteExtension(voteExtension vetypes.OracleVoteExtension) (map[string]math.LegacyDec, error) {
	if len(voteExtension.Rates) > oracletypes.MaxFeeds {
		return nil, fmt.Errorf(
			"number of oracle vote extension rates %d exceeds maximum %d",
			len(voteExtension.Rates),
			oracletypes.MaxFeeds,
		)
	}

	rates := make(map[string]math.LegacyDec, len(voteExtension.Rates))
	for denom, rawRate := range voteExtension.Rates {
		rate, err := DecodeVoteRate(rawRate)
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
	feeds oracletypes.FeedSet,
) ([]VoteRate, error) {
	if feeds.Version == 0 {
		return nil, errors.New("expected vote-target version must be positive")
	}
	if voteExtension.TargetVersion != feeds.Version {
		return nil, fmt.Errorf(
			"oracle vote extension target version %d does not match expected version %d",
			voteExtension.TargetVersion,
			feeds.Version,
		)
	}
	if len(voteExtension.Rates) > oracletypes.MaxFeeds {
		return nil, fmt.Errorf(
			"number of oracle vote extension rates %d exceeds maximum %d",
			len(voteExtension.Rates),
			oracletypes.MaxFeeds,
		)
	}

	rates := make([]VoteRate, 0, len(voteExtension.Rates))
	for denom, rawRate := range voteExtension.Rates {
		targetIndex, found := slices.BinarySearch(feeds.Denoms, denom)
		if !found {
			return nil, fmt.Errorf(
				"oracle vote extension target %s is not in expected targets %v",
				denom,
				feeds.Denoms,
			)
		}
		rate, err := DecodeVoteRate(rawRate)
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
// blocks, without making block finalisation fail. They are flagged Invalid so
// telemetry can separate them from plain absences. Extended-commit envelope
// errors remain fatal.
func GetOracleVotes(
	proposal [][]byte,
	feeds oracletypes.FeedSet,
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

		voteExtension, err := codec.DecodeVoteExtension(voteInfo.VoteExtension)
		if err != nil {
			votes[i].Invalid = true
			continue
		}
		rates, err := ValidateVoteExtension(voteExtension, feeds)
		if err != nil {
			votes[i].Invalid = true
			continue
		}
		votes[i].Rates = rates
	}

	return votes, nil
}
