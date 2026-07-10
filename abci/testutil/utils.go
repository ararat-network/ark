package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	cometabci "github.com/cometbft/cometbft/abci/types"
	cometproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	oracleencoding "ark/abci/oracle/encoding"
	vetypes "ark/abci/ve/types"
)

// NewSDKContext returns a minimal SDK context for ABCI tests.
func NewSDKContext(height int64, voteExtensionsEnableHeight int64, modes ...sdk.ExecMode) sdk.Context {
	ctx := sdk.Context{}.
		WithContext(context.Background()).
		WithBlockHeight(height).
		WithBlockTime(time.Unix(1, 0).UTC()).
		WithConsensusParams(cometproto.ConsensusParams{
			Abci: &cometproto.ABCIParams{
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

	encodedRate, err := oracleencoding.EncodeRate(rate)
	require.NoError(t, err)

	return encodedRate
}

func NewOracleVoteExtension(t *testing.T, rates map[string]math.LegacyDec) vetypes.OracleVoteExtension {
	t.Helper()

	encodedRates := make(map[string][]byte, len(rates))
	for denom, rate := range rates {
		encodedRates[denom] = MustEncodeRate(t, rate)
	}

	return vetypes.OracleVoteExtension{Rates: encodedRates}
}

func NewExtendedVoteInfo(validator sdk.ConsAddress, power int64, voteExtension []byte) cometabci.ExtendedVoteInfo {
	return cometabci.ExtendedVoteInfo{
		Validator: cometabci.Validator{
			Address: validator,
			Power:   power,
		},
		VoteExtension: voteExtension,
	}
}

func NewCommitExtendedVoteInfo(validator sdk.ConsAddress, power int64, voteExtension []byte) cometabci.ExtendedVoteInfo {
	vote := NewExtendedVoteInfo(validator, power, voteExtension)
	vote.BlockIdFlag = cometproto.BlockIDFlagCommit

	return vote
}
