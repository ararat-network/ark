package base

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	providertypes "github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	sidecartypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

func BenchmarkProviderGetPrices(b *testing.B) {
	for _, priceCount := range []int{8, 256} {
		b.Run(fmt.Sprintf("prices=%d", priceCount), func(b *testing.B) {
			provider := &Provider{
				prices: benchmarkProviderPrices(priceCount),
			}

			b.ReportAllocs()
			b.ResetTimer()

			var snapshot map[sidecartypes.Pair]providertypes.Result
			for i := 0; i < b.N; i++ {
				snapshot = provider.GetPrices()
			}

			if len(snapshot) != priceCount {
				b.Fatalf("got %d prices, want %d", len(snapshot), priceCount)
			}
		})
	}
}

func benchmarkProviderPrices(priceCount int) map[sidecartypes.Pair]providertypes.Result {
	prices := make(map[sidecartypes.Pair]providertypes.Result, priceCount)
	timestamp := time.Date(2026, time.July, 22, 0, 0, 0, 0, time.UTC)
	for i := 0; i < priceCount; i++ {
		pair := sidecartypes.Pair(fmt.Sprintf("ASSET%03d/USD", i))
		price := new(big.Float).
			SetPrec(sidecartypes.PricePrecisionBits).
			SetFloat64(float64(i + 1))
		prices[pair] = providertypes.NewResult(price, timestamp)
	}

	return prices
}
