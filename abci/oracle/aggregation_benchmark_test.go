package oracle

import (
	"encoding/binary"
	"fmt"
	"testing"

	cmtabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/math"

	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

var (
	benchmarkResult  aggregationResult
	benchmarkTallies []pricedTally
)

func BenchmarkAggregateOracleVotes(b *testing.B) {
	benchmarks := []struct {
		name        string
		targetCount int
		reportCount int
	}{
		{name: "targets_8/reports_8", targetCount: 8, reportCount: 8},
		{
			name:        "targets_256/reports_256",
			targetCount: oracletypes.MaxFeeds,
			reportCount: oracletypes.MaxFeeds,
		},
		{
			name:        "targets_256/reports_8",
			targetCount: oracletypes.MaxFeeds,
			reportCount: 8,
		},
	}

	for _, bm := range benchmarks {
		b.Run("validators_100/"+bm.name, func(b *testing.B) {
			votes, params, voteTargets := benchmarkAggregationInput(
				b,
				100,
				bm.targetCount,
				bm.reportCount,
			)
			result := aggregateOracleVotes(votes, params, voteTargets)
			if len(result.prices) != bm.reportCount {
				b.Fatalf("got %d prices, want %d", len(result.prices), bm.reportCount)
			}
			if len(result.scores) != len(votes) {
				b.Fatalf("got %d scores, want %d", len(result.scores), len(votes))
			}

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				benchmarkResult = aggregateOracleVotes(votes, params, voteTargets)
			}
		})
	}
}

func BenchmarkSelectReference(b *testing.B) {
	cases := []struct {
		name            string
		targetCount     int
		supportPatterns int
	}{
		{name: "targets_8/identical_support", targetCount: 8, supportPatterns: 1},
		{
			name:            "targets_256/identical_support",
			targetCount:     oracletypes.MaxFeeds,
			supportPatterns: 1,
		},
		{
			name:            "targets_256/two_support_patterns",
			targetCount:     oracletypes.MaxFeeds,
			supportPatterns: 2,
		},
		{
			name:            "targets_256/distinct_support",
			targetCount:     oracletypes.MaxFeeds,
			supportPatterns: oracletypes.MaxFeeds,
		},
	}

	for _, benchmarkCase := range cases {
		b.Run("validators_100/"+benchmarkCase.name, func(b *testing.B) {
			passing, ballots := benchmarkReferenceInput(
				benchmarkCase.targetCount,
				100,
				benchmarkCase.supportPatterns,
			)
			tallies := selectReference(passing, ballots, 50)
			if len(tallies) != benchmarkCase.targetCount {
				b.Fatalf("got %d tallies, want %d", len(tallies), benchmarkCase.targetCount)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				benchmarkTallies = selectReference(passing, ballots, 50)
			}
		})
	}
}

func benchmarkReferenceInput(targetCount, validatorCount, supportPatterns int) ([]int, []ballot) {
	passing := make([]int, targetCount)
	ballots := make([]ballot, targetCount)
	for targetIndex := range targetCount {
		passing[targetIndex] = targetIndex
		ballots[targetIndex].rates = make([]math.LegacyDec, validatorCount)
		for validatorIndex := range validatorCount {
			if supportPatterns > 1 {
				pattern := targetIndex % supportPatterns
				switch supportPatterns {
				case 2:
					if pattern != 0 && validatorIndex >= validatorCount-10 {
						continue
					}
				default:
					const patternBits = 8
					bit := validatorIndex - (validatorCount - patternBits)
					if bit >= 0 && pattern&(1<<bit) != 0 {
						continue
					}
				}
			}
			ballots[targetIndex].add(tallyVote{
				validator: validatorIndex,
				rate: math.LegacyNewDec(
					int64(targetIndex+1)*1_000 + int64(validatorIndex+1),
				),
				power: 1,
			})
		}
	}

	return passing, ballots
}

func benchmarkAggregationInput(
	b *testing.B,
	validatorCount,
	targetCount,
	reportCount int,
) ([]Vote, oracletypes.Params, []string) {
	b.Helper()

	denoms := make([]string, targetCount)
	voteTargets := make([]string, targetCount)
	for targetIndex := range targetCount {
		denom := fmt.Sprintf("uasset%03d", targetIndex)
		denoms[targetIndex] = denom
		voteTargets[targetIndex] = denom
	}

	votes := make([]Vote, validatorCount)
	for validatorIndex := range validatorCount {
		rates := make([]VoteRate, reportCount)
		for targetIndex := range reportCount {
			rates[targetIndex] = VoteRate{
				TargetIndex: targetIndex,
				Value: math.LegacyNewDec(
					int64(targetIndex+1)*100 + int64(validatorIndex%5),
				),
			}
		}

		address := make([]byte, 20)
		binary.BigEndian.PutUint64(address[12:], uint64(validatorIndex+1))
		votes[validatorIndex] = Vote{
			Validator: cmtabci.Validator{
				Address: address,
				Power:   1,
			},
			Rates: rates,
		}
	}

	return votes, oracletypes.DefaultParams(), voteTargets
}
