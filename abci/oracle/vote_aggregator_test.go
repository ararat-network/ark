package oracle_test

import (
	"context"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"noah/abci/codec"
	"noah/abci/oracle"
	abcitestutil "noah/abci/testutil"
	oracletypes "noah/x/oracle/types"
)

func TestAggregateOracleVotesKeepsNoVoteTargetAccountable(t *testing.T) {
	votes := []testVote{
		newTestVote([]byte{1}, 1, map[string]math.LegacyDec{
			"uusd": math.LegacyNewDec(100),
		}),
		newTestVote([]byte{2}, 10, map[string]math.LegacyDec{
			"uusd": math.LegacyNewDec(100),
		}),
	}
	params := oracletypes.DefaultParams()
	params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
	voteTargets := map[string]math.LegacyDec{
		"uusd": math.LegacyZeroDec(),
		"ukrw": math.LegacyZeroDec(),
	}

	keeper, prices, returnedTargets, err := applyOracleVoteExtensions(t, votes, params, voteTargets)

	require.NoError(t, err)
	require.Contains(t, prices, "uusd")
	require.NotContains(t, prices, "ukrw")
	require.Equal(t, voteTargets, returnedTargets)
	require.Len(t, keeper.scoreWeights, 2)

	for _, vote := range votes {
		require.Equal(t, uint64(1), keeper.missCounts[vote.validator.String()])
	}
}

func TestAggregateOracleVotesKeepsFailedQuorumTargetAccountable(t *testing.T) {
	testCases := []struct {
		name string
		rate math.LegacyDec
	}{
		{
			name: "positive rate",
			rate: math.LegacyNewDec(1000),
		},
		{
			name: "zero rate",
			rate: math.LegacyZeroDec(),
		},
		{
			name: "negative rate",
			rate: math.LegacyNewDec(-1),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			voterWithFailedQuorumDenom := []byte{1}
			voterMissingFailedQuorumDenom := []byte{2}
			votes := []testVote{
				newTestVote(voterWithFailedQuorumDenom, 10, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
					"ukrw": tc.rate,
				}),
				newTestVote(voterMissingFailedQuorumDenom, 10, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
				}),
			}
			params := oracletypes.DefaultParams()
			params.VoteThreshold = math.LegacyNewDecWithPrec(75, 2)
			voteTargets := map[string]math.LegacyDec{
				"uusd": math.LegacyZeroDec(),
				"ukrw": math.LegacyZeroDec(),
			}

			keeper, prices, returnedTargets, err := applyOracleVoteExtensions(t, votes, params, voteTargets)

			require.NoError(t, err)
			require.Contains(t, prices, "uusd")
			require.NotContains(t, prices, "ukrw")
			require.Equal(t, voteTargets, returnedTargets)
			require.Zero(t, keeper.missCounts[sdk.ConsAddress(voterWithFailedQuorumDenom).String()])
			require.Equal(t, uint64(1), keeper.missCounts[sdk.ConsAddress(voterMissingFailedQuorumDenom).String()])
		})
	}
}

func TestAggregateOracleVotesSkipsCrossRateDenomWithoutReferenceOverlap(t *testing.T) {
	referenceOnlyVoter := []byte{1}
	crossOnlyVoter := []byte{2}
	votes := []testVote{
		newTestVote(referenceOnlyVoter, 10, map[string]math.LegacyDec{
			"ukrw": math.LegacyNewDec(1000),
		}),
		newTestVote(crossOnlyVoter, 10, map[string]math.LegacyDec{
			"uusd": math.LegacyNewDec(100),
		}),
	}
	params := oracletypes.DefaultParams()
	params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
	voteTargets := map[string]math.LegacyDec{
		"ukrw": math.LegacyZeroDec(),
		"uusd": math.LegacyZeroDec(),
	}

	keeper, prices, _, err := applyOracleVoteExtensions(t, votes, params, voteTargets)

	require.NoError(t, err)
	require.Contains(t, prices, "ukrw")
	require.NotContains(t, prices, "uusd")
	require.Len(t, keeper.scoreWeights, 2)
}

func TestAggregateOracleVotesChoosesReferenceWithBestOverlapCoverage(t *testing.T) {
	votes := []testVote{
		newTestVote([]byte{1}, 40, map[string]math.LegacyDec{
			"uusd": math.LegacyNewDec(100),
		}),
		newTestVote([]byte{2}, 20, map[string]math.LegacyDec{
			"uusd": math.LegacyNewDec(100),
			"ukrw": math.LegacyNewDec(1000),
			"usdr": math.LegacyNewDec(2),
		}),
		newTestVote([]byte{3}, 30, map[string]math.LegacyDec{
			"ukrw": math.LegacyNewDec(900),
			"usdr": math.LegacyNewDec(3),
		}),
		newTestVote([]byte{4}, 10, map[string]math.LegacyDec{}),
	}
	params := oracletypes.DefaultParams()
	params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
	voteTargets := map[string]math.LegacyDec{
		"uusd": math.LegacyZeroDec(),
		"ukrw": math.LegacyZeroDec(),
		"usdr": math.LegacyZeroDec(),
	}

	_, prices, _, err := applyOracleVoteExtensions(t, votes, params, voteTargets)

	require.NoError(t, err)
	require.NotContains(t, prices, "uusd")
	require.True(t, math.LegacyNewDec(900).Equal(prices["ukrw"]))
	require.True(t, math.LegacyNewDec(3).Equal(prices["usdr"]))
}

func TestAggregateOracleVotesSkipsCrossRateDenomBelowOverlapQuorum(t *testing.T) {
	votes := []testVote{
		newTestVote([]byte{1}, 40, map[string]math.LegacyDec{
			"uusd": math.LegacyNewDec(100),
			"ukrw": math.LegacyNewDec(1000),
		}),
		newTestVote([]byte{2}, 40, map[string]math.LegacyDec{
			"uusd": math.LegacyNewDec(100),
		}),
		newTestVote([]byte{3}, 20, map[string]math.LegacyDec{
			"ukrw": math.LegacyNewDec(1000),
		}),
	}
	params := oracletypes.DefaultParams()
	params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
	voteTargets := map[string]math.LegacyDec{
		"uusd": math.LegacyZeroDec(),
		"ukrw": math.LegacyZeroDec(),
	}

	_, prices, _, err := applyOracleVoteExtensions(t, votes, params, voteTargets)

	require.NoError(t, err)
	require.Contains(t, prices, "uusd")
	require.NotContains(t, prices, "ukrw")
}

func TestAggregateOracleVotesCountsNonPositiveTargetRatesAsSubmitted(t *testing.T) {
	testCases := []struct {
		name string
		rate math.LegacyDec
	}{
		{
			name: "zero rate",
			rate: math.LegacyZeroDec(),
		},
		{
			name: "negative rate",
			rate: math.LegacyNewDec(-1),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			positiveVoter := []byte{1}
			nonPositiveVoter := []byte{2}
			votes := []testVote{
				newTestVote(positiveVoter, 10, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
				}),
				newTestVote(nonPositiveVoter, 10, map[string]math.LegacyDec{
					"uusd": tc.rate,
				}),
			}
			params := oracletypes.DefaultParams()
			params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
			voteTargets := map[string]math.LegacyDec{
				"uusd": math.LegacyZeroDec(),
			}

			keeper, prices, _, err := applyOracleVoteExtensions(t, votes, params, voteTargets)

			require.NoError(t, err)
			require.Contains(t, prices, "uusd")
			require.Equal(t, uint64(10), keeper.scoreWeights[sdk.ConsAddress(positiveVoter).String()])
			require.Zero(t, keeper.scoreWeights[sdk.ConsAddress(nonPositiveVoter).String()])
			require.Empty(t, keeper.missCounts)
		})
	}
}

func TestAggregateOracleVotesPenalizesPositiveOutOfBandTargetRates(t *testing.T) {
	testCases := []struct {
		name      string
		outOfBand math.LegacyDec
	}{
		{
			name:      "positive out of band rate",
			outOfBand: math.LegacyNewDec(1000),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			inBandVoter1 := []byte{1}
			inBandVoter2 := []byte{2}
			outOfBandVoter := []byte{3}
			votes := []testVote{
				newTestVote(inBandVoter1, 10, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
				}),
				newTestVote(inBandVoter2, 10, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
				}),
				newTestVote(outOfBandVoter, 10, map[string]math.LegacyDec{
					"uusd": tc.outOfBand,
				}),
			}
			params := oracletypes.DefaultParams()
			params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
			voteTargets := map[string]math.LegacyDec{
				"uusd": math.LegacyZeroDec(),
			}

			keeper, prices, _, err := applyOracleVoteExtensions(t, votes, params, voteTargets)

			require.NoError(t, err)
			require.True(t, math.LegacyNewDec(100).Equal(prices["uusd"]))
			require.Equal(t, uint64(10), keeper.scoreWeights[sdk.ConsAddress(inBandVoter1).String()])
			require.Equal(t, uint64(10), keeper.scoreWeights[sdk.ConsAddress(inBandVoter2).String()])
			require.Zero(t, keeper.scoreWeights[sdk.ConsAddress(outOfBandVoter).String()])
			require.Equal(t, uint64(1), keeper.missCounts[sdk.ConsAddress(outOfBandVoter).String()])
		})
	}
}

type testVote struct {
	validator sdk.ConsAddress
	power     int64
	rates     map[string]math.LegacyDec
}

func newTestVote(address []byte, power int64, rates map[string]math.LegacyDec) testVote {
	return testVote{
		validator: sdk.ConsAddress(address),
		power:     power,
		rates:     rates,
	}
}

func applyOracleVoteExtensions(
	t *testing.T,
	votes []testVote,
	params oracletypes.Params,
	voteTargets map[string]math.LegacyDec,
) (*recordingOracleKeeper, map[string]math.LegacyDec, map[string]math.LegacyDec, error) {
	t.Helper()

	veCodec := codec.NewDefaultVoteExtensionCodec()
	extCommitCodec := codec.NewDefaultExtendedCommitCodec()
	extendedVotes := make([]cometabci.ExtendedVoteInfo, 0, len(votes))
	for _, vote := range votes {
		voteExtension, err := veCodec.Encode(abcitestutil.NewOracleVoteExtension(t, vote.rates))
		require.NoError(t, err)
		extendedVotes = append(extendedVotes, abcitestutil.NewExtendedVoteInfo(vote.validator, vote.power, voteExtension))
	}

	extendedCommit, err := extCommitCodec.Encode(cometabci.ExtendedCommitInfo{Votes: extendedVotes})
	require.NoError(t, err)

	keeper := newRecordingOracleKeeper(params, voteTargets)
	priceApplier := oracle.NewPriceApplier(
		oracle.NewVoteAggregator(log.NewNopLogger()),
		keeper,
		veCodec,
		extCommitCodec,
		log.NewNopLogger(),
	)
	prices, returnedTargets, err := priceApplier.ApplyPricesFromVoteExtensions(
		abcitestutil.NewSDKContext(3, 0),
		&cometabci.RequestFinalizeBlock{
			Height: 3,
			Txs:    [][]byte{extendedCommit},
		},
	)

	return keeper, prices, returnedTargets, err
}

type recordingOracleKeeper struct {
	params       oracletypes.Params
	voteTargets  map[string]math.LegacyDec
	exchangeRate map[string]math.LegacyDec
	scoreWeights map[string]uint64
	missCounts   map[string]uint64
}

func newRecordingOracleKeeper(params oracletypes.Params, voteTargets map[string]math.LegacyDec) *recordingOracleKeeper {
	return &recordingOracleKeeper{
		params:       params,
		voteTargets:  cloneDecMap(voteTargets),
		exchangeRate: make(map[string]math.LegacyDec),
		scoreWeights: make(map[string]uint64),
		missCounts:   make(map[string]uint64),
	}
}

func (k *recordingOracleKeeper) GetParams(context.Context) (oracletypes.Params, error) {
	return k.params, nil
}

func (k *recordingOracleKeeper) SetExchangeRateWithEvent(_ context.Context, exchangeRate oracletypes.ExchangeRate) error {
	k.exchangeRate[exchangeRate.Denom] = exchangeRate.Rate
	return nil
}

func (k *recordingOracleKeeper) AddScoreWeight(_ context.Context, validator sdk.ConsAddress, scoreWeight uint64) error {
	k.scoreWeights[validator.String()] += scoreWeight
	return nil
}

func (k *recordingOracleKeeper) IncrementMissCount(_ context.Context, validator sdk.ConsAddress) error {
	k.missCounts[validator.String()]++
	return nil
}

func (k *recordingOracleKeeper) GetVoteTargets(context.Context) (map[string]math.LegacyDec, error) {
	return cloneDecMap(k.voteTargets), nil
}

func (k *recordingOracleKeeper) SyncTobinTax(context.Context, map[string]math.LegacyDec) error {
	return nil
}

func cloneDecMap(in map[string]math.LegacyDec) map[string]math.LegacyDec {
	out := make(map[string]math.LegacyDec, len(in))
	maps.Copy(out, in)

	return out
}
