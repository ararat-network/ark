package oracle

import (
	"testing"

	"github.com/stretchr/testify/require"

	cometabci "github.com/cometbft/cometbft/abci/types"

	oracletypes "ark/x/oracle/types"
)

func TestAggregateOracleVotesWithNoTargetsDoesNotRecordMisses(t *testing.T) {
	votes := []Vote{
		{Validator: cometabci.Validator{Address: []byte("validator1"), Power: 10}},
		{Validator: cometabci.Validator{Address: []byte("validator2"), Power: 20}},
	}

	result, err := aggregateOracleVotes(votes, oracletypes.DefaultParams(), []string{})

	require.NoError(t, err)
	require.Empty(t, result.Prices)
	require.Empty(t, result.VoteTargets)
	require.Len(t, result.scores, len(votes))
	for _, score := range result.scores {
		require.True(t, score.rewardWeight.IsZero())
		require.False(t, score.missed)
	}
}
