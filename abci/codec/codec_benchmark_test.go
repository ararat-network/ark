package codec

import (
	"fmt"
	"testing"

	"cosmossdk.io/math"

	vetypes "github.com/ararat-network/ark/abci/voteextension/types"
	"github.com/ararat-network/ark/pkg/encoding"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

var (
	benchmarkEncodedVoteExtension  []byte
	benchmarkDecodedVoteExtensions []vetypes.OracleVoteExtension
)

func BenchmarkVoteExtensionCodec(b *testing.B) {
	const validatorCount = 100

	targetCounts := []int{len(oracletypes.DefaultFeedDenoms), oracletypes.MaxFeeds}
	fixtures := make(map[int]vetypes.OracleVoteExtension, len(targetCounts))
	encodedFixtures := make(map[int][]byte, len(targetCounts))
	for _, targetCount := range targetCounts {
		fixture := benchmarkCodecVoteExtension(b, targetCount)
		encoded, err := EncodeVoteExtension(fixture)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := DecodeVoteExtension(encoded); err != nil {
			b.Fatal(err)
		}
		fixtures[targetCount] = fixture
		encodedFixtures[targetCount] = encoded
	}

	b.Run("encode", func(b *testing.B) {
		for _, targetCount := range targetCounts {
			b.Run(fmt.Sprintf("targets_%d", targetCount), func(b *testing.B) {
				fixture := fixtures[targetCount]
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					encoded, err := EncodeVoteExtension(fixture)
					if err != nil {
						b.Fatal(err)
					}
					benchmarkEncodedVoteExtension = encoded
				}
				b.ReportMetric(float64(len(encodedFixtures[targetCount])), "extension_B/op")
			})
		}
	})

	b.Run("decode/validators_100", func(b *testing.B) {
		for _, targetCount := range targetCounts {
			b.Run(fmt.Sprintf("targets_%d", targetCount), func(b *testing.B) {
				encoded := encodedFixtures[targetCount]
				decoded := make([]vetypes.OracleVoteExtension, validatorCount)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					for i := range validatorCount {
						voteExtension, err := DecodeVoteExtension(encoded)
						if err != nil {
							b.Fatal(err)
						}
						decoded[i] = voteExtension
					}
				}
				benchmarkDecodedVoteExtensions = decoded
				b.ReportMetric(float64(len(encoded)), "extension_B/validator")
				b.ReportMetric(validatorCount, "validators/op")
			})
		}
	})
}

func benchmarkCodecVoteExtension(b *testing.B, targetCount int) vetypes.OracleVoteExtension {
	b.Helper()

	rates := make(map[string][]byte, targetCount)
	for i := range targetCount {
		rate, err := encoding.EncodeCompactLegacyDec(math.LegacyNewDec(int64(i + 1)))
		if err != nil {
			b.Fatal(err)
		}
		rates[fmt.Sprintf("uasset%03d", i)] = rate
	}

	return vetypes.OracleVoteExtension{
		Rates:         rates,
		TargetVersion: oracletypes.InitialFeedVersion,
	}
}
