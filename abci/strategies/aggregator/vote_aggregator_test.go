package aggregator

import (
	"testing"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"

	vetypes "noah/abci/ve/types"
	oracletypes "noah/x/oracle/types"
)

func TestAggregateOracleVotesPrunesConfiguredTargetWithNoVotes(t *testing.T) {
	votes := []Vote{
		newTestVote([]byte{1}, 10, []*vetypes.Rate{
			{Denom: "uusd", Rate: math.LegacyNewDec(100)},
		}),
		newTestVote([]byte{2}, 10, []*vetypes.Rate{
			{Denom: "uusd", Rate: math.LegacyNewDec(100)},
		}),
	}
	params := oracletypes.DefaultParams()
	params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
	voteTargets := map[string]math.LegacyDec{
		"uusd": math.LegacyZeroDec(),
		"ukrw": math.LegacyZeroDec(),
	}

	prices, scoreMap, err := NewVoteAggregator(log.NewNopLogger()).
		AggregateOracleVotes(sdk.Context{}, votes, params, voteTargets)

	require.NoError(t, err)
	require.Contains(t, prices, "uusd")
	require.NotContains(t, prices, "ukrw")
	require.Contains(t, voteTargets, "uusd")
	require.NotContains(t, voteTargets, "ukrw")
	require.Len(t, scoreMap, 2)
}

func TestAggregateOracleVotesPrunesConfiguredTargetThatFailsQuorum(t *testing.T) {
	votes := []Vote{
		newTestVote([]byte{1}, 10, []*vetypes.Rate{
			{Denom: "uusd", Rate: math.LegacyNewDec(100)},
			{Denom: "ukrw", Rate: math.LegacyNewDec(1000)},
		}),
		newTestVote([]byte{2}, 10, []*vetypes.Rate{
			{Denom: "uusd", Rate: math.LegacyNewDec(100)},
		}),
	}
	params := oracletypes.DefaultParams()
	params.VoteThreshold = math.LegacyNewDecWithPrec(75, 2)
	voteTargets := map[string]math.LegacyDec{
		"uusd": math.LegacyZeroDec(),
		"ukrw": math.LegacyZeroDec(),
	}

	prices, _, err := NewVoteAggregator(log.NewNopLogger()).
		AggregateOracleVotes(sdk.Context{}, votes, params, voteTargets)

	require.NoError(t, err)
	require.Contains(t, prices, "uusd")
	require.NotContains(t, prices, "ukrw")
	require.Contains(t, voteTargets, "uusd")
	require.NotContains(t, voteTargets, "ukrw")
}

func newTestVote(address []byte, power int64, rates []*vetypes.Rate) Vote {
	return Vote{
		Validator: cometabci.Validator{
			Address: address,
			Power:   power,
		},
		OracleVoteExtension: vetypes.OracleVoteExtension{
			Rates: rates,
		},
	}
}
