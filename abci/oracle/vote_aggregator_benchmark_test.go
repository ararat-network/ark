package oracle

import (
	"encoding/binary"
	"fmt"
	"testing"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/math"

	oracleencoding "ark/abci/oracle/encoding"
	vetypes "ark/abci/ve/types"
	oracletypes "ark/x/oracle/types"
)

var (
	benchmarkResult       AggregationResult
	benchmarkQuotientSafe bool
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
			targetCount: oracletypes.MaxVoteTargets,
			reportCount: oracletypes.MaxVoteTargets,
		},
		{
			name:        "targets_256/reports_8",
			targetCount: oracletypes.MaxVoteTargets,
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
			result, err := aggregateOracleVotes(votes, params, voteTargets)
			if err != nil {
				b.Fatal(err)
			}
			if len(result.Prices) != bm.reportCount {
				b.Fatalf("got %d prices, want %d", len(result.Prices), bm.reportCount)
			}
			if len(result.scores) != len(votes) {
				b.Fatalf("got %d scores, want %d", len(result.scores), len(votes))
			}

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				result, err := aggregateOracleVotes(votes, params, voteTargets)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkResult = result
			}
		})
	}
}

func BenchmarkPositiveQuotientSafelyRepresentable(b *testing.B) {
	benchmarks := []struct {
		name        string
		numerator   math.LegacyDec
		denominator math.LegacyDec
		expected    bool
	}{
		{
			name:        "fast_path",
			numerator:   math.LegacyNewDec(100),
			denominator: math.LegacyNewDec(10),
			expected:    true,
		},
		{
			name:        "underflow_boundary",
			numerator:   math.LegacySmallestDec(),
			denominator: math.LegacyNewDec(2).Sub(math.LegacySmallestDec()),
			expected:    false,
		},
		{
			name:        "overflow_boundary",
			numerator:   math.LegacyMustNewDecFromStr("1000000000000000000000000000000000000000000000000000000000000"),
			denominator: math.LegacySmallestDec(),
			expected:    false,
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			if actual := positiveQuotientSafelyRepresentable(bm.numerator, bm.denominator); actual != bm.expected {
				b.Fatalf("got %t, want %t", actual, bm.expected)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				benchmarkQuotientSafe = positiveQuotientSafelyRepresentable(
					bm.numerator,
					bm.denominator,
				)
			}
		})
	}
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
		rates := make(map[string][]byte, reportCount)
		for targetIndex := range reportCount {
			rate := math.LegacyNewDec(
				int64(targetIndex+1)*100 + int64(validatorIndex%5),
			)
			bz, err := oracleencoding.EncodeRate(rate)
			if err != nil {
				b.Fatal(err)
			}
			rates[denoms[targetIndex]] = bz
		}

		address := make([]byte, 20)
		binary.BigEndian.PutUint64(address[12:], uint64(validatorIndex+1))
		votes[validatorIndex] = Vote{
			Validator: cometabci.Validator{
				Address: address,
				Power:   1,
			},
			OracleVoteExtension: vetypes.OracleVoteExtension{Rates: rates},
		}
	}

	return votes, oracletypes.DefaultParams(), voteTargets
}
