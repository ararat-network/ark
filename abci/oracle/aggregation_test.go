package oracle_test

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/abci/codec"
	"ark/abci/oracle"
	abcitestutil "ark/abci/testutil"
	oracletypes "ark/x/oracle/types"
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

	keeper, prices, err := applyOracleVoteExtensions(t, votes, params, voteTargets)

	require.NoError(t, err)
	require.Contains(t, prices, "uusd")
	require.NotContains(t, prices, "ukrw")
	require.Len(t, keeper.scoreWeights, 2)

	for _, vote := range votes {
		require.Equal(t, uint64(1), keeper.missCounts[vote.validator.String()])
	}
}

func TestAggregateOracleVotesKeepsFailedQuorumTargetAccountable(t *testing.T) {
	testCases := []struct {
		name       string
		rate       math.LegacyDec
		expectMiss bool
	}{
		{
			name: "positive rate",
			rate: math.LegacyNewDec(1000),
		},
		{
			name:       "zero rate",
			rate:       math.LegacyZeroDec(),
			expectMiss: true,
		},
		{
			name:       "negative rate",
			rate:       math.LegacyNewDec(-1),
			expectMiss: true,
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

			keeper, prices, err := applyOracleVoteExtensions(t, votes, params, voteTargets)

			require.NoError(t, err)
			require.Contains(t, prices, "uusd")
			require.NotContains(t, prices, "ukrw")
			if tc.expectMiss {
				require.Equal(t, uint64(1), keeper.missCounts[sdk.ConsAddress(voterWithFailedQuorumDenom).String()])
			} else {
				require.Zero(t, keeper.missCounts[sdk.ConsAddress(voterWithFailedQuorumDenom).String()])
			}
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

	keeper, prices, err := applyOracleVoteExtensions(t, votes, params, voteTargets)

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

	_, prices, err := applyOracleVoteExtensions(t, votes, params, voteTargets)

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

	_, prices, err := applyOracleVoteExtensions(t, votes, params, voteTargets)

	require.NoError(t, err)
	require.Contains(t, prices, "uusd")
	require.NotContains(t, prices, "ukrw")
}

func TestAggregateOracleVotesPenalizesNonPositiveTargetRates(t *testing.T) {
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

			keeper, prices, err := applyOracleVoteExtensions(t, votes, params, voteTargets)

			require.NoError(t, err)
			require.Contains(t, prices, "uusd")
			require.True(t, math.NewInt(10).Equal(keeper.scoreWeights[sdk.ConsAddress(positiveVoter).String()]))
			require.True(t, keeper.scoreWeights[sdk.ConsAddress(nonPositiveVoter).String()].IsZero())
			require.Equal(t, uint64(1), keeper.missCounts[sdk.ConsAddress(nonPositiveVoter).String()])
		})
	}
}

func TestAggregateOracleVotesCountsAtMostOneMissForMultipleNonPositiveTargets(t *testing.T) {
	honestVoter := []byte{1}
	nonPositiveVoter := []byte{2}
	votes := []testVote{
		newTestVote(honestVoter, 10, map[string]math.LegacyDec{
			"ukrw": math.LegacyNewDec(1000),
			"usdr": math.LegacyNewDec(2),
			"uusd": math.LegacyNewDec(100),
		}),
		newTestVote(nonPositiveVoter, 10, map[string]math.LegacyDec{
			"ukrw": math.LegacyZeroDec(),
			"usdr": math.LegacyNewDec(-1),
			"uusd": math.LegacyNewDec(100),
		}),
	}
	params := oracletypes.DefaultParams()
	params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
	voteTargets := map[string]math.LegacyDec{
		"ukrw": math.LegacyZeroDec(),
		"usdr": math.LegacyZeroDec(),
		"uusd": math.LegacyZeroDec(),
	}

	keeper, _, err := applyOracleVoteExtensions(t, votes, params, voteTargets)

	require.NoError(t, err)
	require.Equal(t, uint64(1), keeper.missCounts[sdk.ConsAddress(nonPositiveVoter).String()])
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

			keeper, prices, err := applyOracleVoteExtensions(t, votes, params, voteTargets)

			require.NoError(t, err)
			require.True(t, math.LegacyNewDec(100).Equal(prices["uusd"]))
			require.True(t, math.NewInt(10).Equal(keeper.scoreWeights[sdk.ConsAddress(inBandVoter1).String()]))
			require.True(t, math.NewInt(10).Equal(keeper.scoreWeights[sdk.ConsAddress(inBandVoter2).String()]))
			require.True(t, keeper.scoreWeights[sdk.ConsAddress(outOfBandVoter).String()].IsZero())
			require.Equal(t, uint64(1), keeper.missCounts[sdk.ConsAddress(outOfBandVoter).String()])
		})
	}
}

func TestAggregateOracleVotesUsesCeilingForVoteThreshold(t *testing.T) {
	testCases := []struct {
		name         string
		reportPower  int64
		absentPower  int64
		expectsPrice bool
	}{
		{
			name:         "rounded support below threshold fails",
			reportPower:  3,
			absentPower:  7,
			expectsPrice: false,
		},
		{
			name:         "support at ceiling threshold passes",
			reportPower:  4,
			absentPower:  6,
			expectsPrice: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			votes := []testVote{
				newTestVote([]byte{1}, tc.reportPower, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
				}),
				newTestVote([]byte{2}, tc.absentPower, map[string]math.LegacyDec{}),
			}
			params := oracletypes.DefaultParams()
			params.VoteThreshold = math.LegacyNewDecWithPrec(34, 2)
			voteTargets := map[string]math.LegacyDec{
				"uusd": math.LegacyZeroDec(),
			}

			_, prices, err := applyOracleVoteExtensions(t, votes, params, voteTargets)

			require.NoError(t, err)
			if tc.expectsPrice {
				require.Contains(t, prices, "uusd")
			} else {
				require.NotContains(t, prices, "uusd")
			}
		})
	}
}

func TestAggregateOracleVotesUsesCeilingForOverlapThreshold(t *testing.T) {
	testCases := []struct {
		name          string
		overlapPower  int64
		referenceOnly int64
		targetOnly    int64
		absentPower   int64
		expectsTarget bool
	}{
		{
			name:          "rounded overlap below threshold fails",
			overlapPower:  3,
			referenceOnly: 1,
			targetOnly:    1,
			absentPower:   5,
			expectsTarget: false,
		},
		{
			name:          "overlap at ceiling threshold passes",
			overlapPower:  4,
			referenceOnly: 0,
			targetOnly:    0,
			absentPower:   6,
			expectsTarget: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			votes := []testVote{
				newTestVote([]byte{1}, tc.overlapPower, map[string]math.LegacyDec{
					"uaaa": math.LegacyNewDec(100),
					"uzzz": math.LegacyNewDec(10),
				}),
				newTestVote([]byte{2}, tc.referenceOnly, map[string]math.LegacyDec{
					"uaaa": math.LegacyNewDec(100),
				}),
				newTestVote([]byte{3}, tc.targetOnly, map[string]math.LegacyDec{
					"uzzz": math.LegacyNewDec(10),
				}),
				newTestVote([]byte{4}, tc.absentPower, map[string]math.LegacyDec{}),
			}
			params := oracletypes.DefaultParams()
			params.VoteThreshold = math.LegacyNewDecWithPrec(34, 2)
			voteTargets := map[string]math.LegacyDec{
				"uaaa": math.LegacyZeroDec(),
				"uzzz": math.LegacyZeroDec(),
			}

			_, prices, err := applyOracleVoteExtensions(t, votes, params, voteTargets)

			require.NoError(t, err)
			require.Contains(t, prices, "uaaa")
			if tc.expectsTarget {
				require.Contains(t, prices, "uzzz")
			} else {
				require.NotContains(t, prices, "uzzz")
			}
		})
	}
}

func TestAggregateOracleVotesUsesMedianOfValidatorCrossRates(t *testing.T) {
	votes := []testVote{
		newTestVote([]byte{1}, 1, map[string]math.LegacyDec{
			"uaaa": math.LegacyNewDec(1),
			"uzzz": math.LegacyNewDec(1),
		}),
		newTestVote([]byte{2}, 1, map[string]math.LegacyDec{
			"uaaa": math.LegacyNewDec(100),
			"uzzz": math.LegacyNewDec(10),
		}),
		newTestVote([]byte{3}, 1, map[string]math.LegacyDec{
			"uaaa": math.LegacyNewDec(101),
			"uzzz": math.LegacyNewDec(101),
		}),
	}
	voteTargets := map[string]math.LegacyDec{
		"uaaa": math.LegacyZeroDec(),
		"uzzz": math.LegacyZeroDec(),
	}

	keeper, prices, err := applyOracleVoteExtensions(t, votes, oracletypes.DefaultParams(), voteTargets)

	require.NoError(t, err)
	require.True(t, math.LegacyNewDec(100).Equal(prices["uaaa"]))
	// Per-validator uaaa/uzzz ratios are 1, 10, and 1, so their median is 1.
	// This deliberately differs from median(uaaa)/median(uzzz) = 100/10.
	require.True(t, math.LegacyNewDec(100).Equal(prices["uzzz"]))
	require.True(t, math.NewInt(1).Equal(keeper.scoreWeights[sdk.ConsAddress([]byte{1}).String()]))
	require.True(t, math.NewInt(1).Equal(keeper.scoreWeights[sdk.ConsAddress([]byte{2}).String()]))
	require.True(t, math.NewInt(2).Equal(keeper.scoreWeights[sdk.ConsAddress([]byte{3}).String()]))
	require.Equal(t, uint64(1), keeper.missCounts[sdk.ConsAddress([]byte{1}).String()])
	require.Equal(t, uint64(1), keeper.missCounts[sdk.ConsAddress([]byte{2}).String()])
	require.Zero(t, keeper.missCounts[sdk.ConsAddress([]byte{3}).String()])
}

func TestAggregateOracleVotesSkipsUnrepresentableCrossRateObservation(t *testing.T) {
	honestVoter := []byte{1}
	extremeVoter := []byte{2}
	votes := []testVote{
		newTestVote(honestVoter, 90, map[string]math.LegacyDec{
			"uaaa": math.LegacyNewDec(100),
			"uzzz": math.LegacyNewDec(10),
		}),
		newTestVote(extremeVoter, 10, map[string]math.LegacyDec{
			"uaaa": math.LegacyMustNewDecFromStr("1000000000000000000000000000000000000000000000000000000000000"),
			"uzzz": math.LegacySmallestDec(),
		}),
	}
	voteTargets := map[string]math.LegacyDec{
		"uaaa": math.LegacyZeroDec(),
		"uzzz": math.LegacyZeroDec(),
	}

	keeper, prices, err := applyOracleVoteExtensions(t, votes, oracletypes.DefaultParams(), voteTargets)

	require.NoError(t, err)
	require.True(t, math.LegacyNewDec(100).Equal(prices["uaaa"]))
	require.True(t, math.LegacyNewDec(10).Equal(prices["uzzz"]))
	require.True(t, math.NewInt(180).Equal(keeper.scoreWeights[sdk.ConsAddress(honestVoter).String()]))
	require.True(t, keeper.scoreWeights[sdk.ConsAddress(extremeVoter).String()].IsZero())
	require.Equal(t, uint64(1), keeper.missCounts[sdk.ConsAddress(extremeVoter).String()])
}

func TestApplyOracleVoteExtensionsUsesDeterministicWriteOrder(t *testing.T) {
	votes := []testVote{
		newTestVote([]byte{3}, 1, map[string]math.LegacyDec{
			"uzzz": math.LegacyNewDec(10),
			"uaaa": math.LegacyNewDec(100),
			"ummm": math.LegacyNewDec(50),
		}),
		newTestVote([]byte{1}, 1, map[string]math.LegacyDec{
			"ummm": math.LegacyNewDec(50),
			"uzzz": math.LegacyNewDec(10),
			"uaaa": math.LegacyNewDec(100),
		}),
		newTestVote([]byte{2}, 1, map[string]math.LegacyDec{
			"uaaa": math.LegacyNewDec(100),
			"ummm": math.LegacyNewDec(50),
			"uzzz": math.LegacyNewDec(10),
		}),
	}
	voteTargets := map[string]math.LegacyDec{
		"uzzz": math.LegacyZeroDec(),
		"uaaa": math.LegacyZeroDec(),
		"ummm": math.LegacyZeroDec(),
	}

	keeper, _, err := applyOracleVoteExtensions(t, votes, oracletypes.DefaultParams(), voteTargets)

	require.NoError(t, err)
	require.Equal(t, []string{"uaaa", "ummm", "uzzz"}, keeper.exchangeRateOrder)
	require.Equal(t, []string{
		sdk.ConsAddress([]byte{3}).String(),
		sdk.ConsAddress([]byte{1}).String(),
		sdk.ConsAddress([]byte{2}).String(),
	}, keeper.scoreOrder)
}

func TestAggregateOracleVotesFixedBandSkipsExtremeNonPositiveRate(t *testing.T) {
	inBandVoter1 := []byte{1}
	inBandVoter2 := []byte{2}
	outOfBandVoter := []byte{3}
	nonPositiveVoter := []byte{4}
	votes := []testVote{
		newTestVote(inBandVoter1, 40, map[string]math.LegacyDec{
			"uusd": math.LegacyNewDec(100),
		}),
		newTestVote(inBandVoter2, 40, map[string]math.LegacyDec{
			"uusd": math.LegacyNewDec(100),
		}),
		newTestVote(outOfBandVoter, 10, map[string]math.LegacyDec{
			"uusd": math.LegacyNewDec(120),
		}),
		newTestVote(nonPositiveVoter, 10, map[string]math.LegacyDec{
			"uusd": math.LegacyNewDec(-1_000_000),
		}),
	}
	params := oracletypes.DefaultParams()
	voteTargets := map[string]math.LegacyDec{
		"uusd": math.LegacyZeroDec(),
	}

	keeper, prices, err := applyOracleVoteExtensions(t, votes, params, voteTargets)

	require.NoError(t, err)
	require.True(t, math.LegacyNewDec(100).Equal(prices["uusd"]))
	require.True(t, math.NewInt(40).Equal(keeper.scoreWeights[sdk.ConsAddress(inBandVoter1).String()]))
	require.True(t, math.NewInt(40).Equal(keeper.scoreWeights[sdk.ConsAddress(inBandVoter2).String()]))
	require.True(t, keeper.scoreWeights[sdk.ConsAddress(outOfBandVoter).String()].IsZero())
	require.Equal(t, uint64(1), keeper.missCounts[sdk.ConsAddress(outOfBandVoter).String()])
	require.True(t, keeper.scoreWeights[sdk.ConsAddress(nonPositiveVoter).String()].IsZero())
	require.Equal(t, uint64(1), keeper.missCounts[sdk.ConsAddress(nonPositiveVoter).String()])
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
) (*recordingOracleKeeper, map[string]math.LegacyDec, error) {
	t.Helper()
	voteExtensionCodec := codec.NewVoteExtensionCodec()

	extendedVotes := make([]cometabci.ExtendedVoteInfo, 0, len(votes))
	for _, vote := range votes {
		voteExtension, err := voteExtensionCodec.Encode(abcitestutil.NewOracleVoteExtension(t, vote.rates))
		require.NoError(t, err)
		extendedVotes = append(extendedVotes, abcitestutil.NewExtendedVoteInfo(vote.validator, vote.power, voteExtension))
	}

	extendedCommit, err := codec.EncodeExtendedCommit(cometabci.ExtendedCommitInfo{Votes: extendedVotes})
	require.NoError(t, err)

	keeper := newRecordingOracleKeeper(params, voteTargets)
	prices, err := oracle.ProcessVoteExtensions(
		abcitestutil.NewSDKContext(3, 0),
		keeper,
		voteExtensionCodec,
		&cometabci.RequestFinalizeBlock{
			Height:            3,
			Txs:               [][]byte{extendedCommit},
			DecidedLastCommit: cometabci.CommitInfo{Votes: make([]cometabci.VoteInfo, len(extendedVotes))},
		},
	)

	return keeper, prices, err
}

type recordingOracleKeeper struct {
	params            oracletypes.Params
	voteTargets       []string
	exchangeRate      map[string]math.LegacyDec
	exchangeRateOrder []string
	scoreWeights      map[string]math.Int
	scoreOrder        []string
	missCounts        map[string]uint64
}

func newRecordingOracleKeeper(params oracletypes.Params, voteTargets map[string]math.LegacyDec) *recordingOracleKeeper {
	denoms := make([]string, 0, len(voteTargets))
	for denom := range voteTargets {
		denoms = append(denoms, denom)
	}
	slices.Sort(denoms)

	return &recordingOracleKeeper{
		params:       params,
		voteTargets:  denoms,
		exchangeRate: make(map[string]math.LegacyDec),
		scoreWeights: make(map[string]math.Int),
		missCounts:   make(map[string]uint64),
	}
}

func (k *recordingOracleKeeper) GetParams(context.Context) (oracletypes.Params, error) {
	return k.params, nil
}

func (k *recordingOracleKeeper) SetExchangeRateWithEvent(_ context.Context, exchangeRate oracletypes.ExchangeRate) error {
	k.exchangeRate[exchangeRate.Denom] = exchangeRate.Rate
	k.exchangeRateOrder = append(k.exchangeRateOrder, exchangeRate.Denom)
	return nil
}

func (k *recordingOracleKeeper) RecordVoteAccounting(
	_ context.Context,
	validator sdk.ConsAddress,
	scoreWeight math.Int,
	missed bool,
) error {
	validatorKey := validator.String()
	currentWeight, ok := k.scoreWeights[validatorKey]
	if !ok {
		currentWeight = math.ZeroInt()
	}
	k.scoreWeights[validatorKey] = currentWeight.Add(scoreWeight)
	k.scoreOrder = append(k.scoreOrder, validatorKey)
	if missed {
		k.missCounts[validator.String()]++
	}
	return nil
}

func (k *recordingOracleKeeper) GetVoteTargets(context.Context, int64) (oracletypes.VoteTargetSet, error) {
	return oracletypes.VoteTargetSet{
		Version: oracletypes.InitialVoteTargetVersion,
		Denoms:  slices.Clone(k.voteTargets),
	}, nil
}

func (k *recordingOracleKeeper) AdvanceVoteTargets(context.Context) error {
	return nil
}
