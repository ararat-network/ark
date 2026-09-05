package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/abci/codec"
	vetypes "github.com/ararat-network/ark/abci/voteextension/types"
	"github.com/ararat-network/ark/pkg/encoding"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// NewSDKContext returns a minimal SDK context for ABCI tests.
func NewSDKContext(height int64, voteExtensionsEnableHeight int64, modes ...sdk.ExecMode) sdk.Context {
	ctx := sdk.Context{}.
		WithContext(context.Background()).
		WithBlockHeight(height).
		WithBlockTime(time.Unix(1, 0).UTC()).
		WithConsensusParams(cmtproto.ConsensusParams{
			Abci: &cmtproto.ABCIParams{
				VoteExtensionsEnableHeight: voteExtensionsEnableHeight,
			},
		})

	if len(modes) > 0 {
		ctx = ctx.WithExecMode(modes[0])
	}

	return ctx
}

func MustEncodeRate(t *testing.T, rate math.LegacyDec) []byte {
	t.Helper()

	encodedRate, err := encoding.EncodeCompactLegacyDec(rate)
	require.NoError(t, err)

	return encodedRate
}

// NewOracleVoteExtension builds the extension a conforming validator would
// submit for rates: non-positive rates are omitted, matching the production
// builder, because the compact encoding has no abstention form.
func NewOracleVoteExtension(t *testing.T, rates map[string]math.LegacyDec) vetypes.OracleVoteExtension {
	t.Helper()

	encodedRates := make(map[string][]byte, len(rates))
	for denom, rate := range rates {
		if !rate.IsPositive() {
			continue
		}
		encodedRates[denom] = MustEncodeRate(t, rate)
	}

	return vetypes.OracleVoteExtension{
		Rates:         encodedRates,
		TargetVersion: oracletypes.InitialFeedVersion,
	}
}

func MustEncodeVoteExtension(t *testing.T, voteExtension vetypes.OracleVoteExtension) []byte {
	t.Helper()

	encoded, err := codec.EncodeVoteExtension(voteExtension)
	require.NoError(t, err)

	return encoded
}

func MustEncodeExtendedCommit(t *testing.T, extendedCommit cmtabci.ExtendedCommitInfo) []byte {
	t.Helper()

	encoded, err := codec.EncodeExtendedCommit(extendedCommit)
	require.NoError(t, err)

	return encoded
}

func NewExtendedVoteInfo(validator sdk.ConsAddress, power int64, voteExtension []byte) cmtabci.ExtendedVoteInfo {
	return cmtabci.ExtendedVoteInfo{
		Validator: cmtabci.Validator{
			Address: validator,
			Power:   power,
		},
		VoteExtension: voteExtension,
	}
}

func NewCommitExtendedVoteInfo(validator sdk.ConsAddress, power int64, voteExtension []byte) cmtabci.ExtendedVoteInfo {
	vote := NewExtendedVoteInfo(validator, power, voteExtension)
	vote.BlockIdFlag = cmtproto.BlockIDFlagCommit

	return vote
}
