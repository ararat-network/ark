package resolver_test

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/ararat-network/ark/pricefeed/sidecar/resolver"
	"github.com/ararat-network/ark/pricefeed/sidecar/types"
)

func BenchmarkResolvePrices(b *testing.B) {
	benchmarkResolvePrices(b)
}

func BenchmarkResolvePricesWithPrometheus(b *testing.B) {
	registry := prometheus.NewRegistry()
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registry))
	if err != nil {
		b.Fatal(err)
	}

	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	otel.SetMeterProvider(provider)
	b.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			b.Error(err)
		}
	})

	benchmarkResolvePrices(b)

	families, err := registry.Gather()
	if err != nil {
		b.Fatal(err)
	}
	if len(families) == 0 {
		b.Fatal("no Prometheus metrics gathered")
	}
}

func benchmarkResolvePrices(b *testing.B) {
	testCases := []struct {
		name          string
		feedCount     int
		providerCount int
		bootstrapLeg  bool
	}{
		{
			name:          "direct/feeds=8/providers=1",
			feedCount:     8,
			providerCount: 1,
		},
		{
			name:          "bootstrap_route/feeds=8/providers=1",
			feedCount:     8,
			providerCount: 1,
			bootstrapLeg:  true,
		},
		{
			name:          "bootstrap_route/feeds=256/providers=1",
			feedCount:     256,
			providerCount: 1,
			bootstrapLeg:  true,
		},
		{
			name:          "bootstrap_route/feeds=256/providers=16",
			feedCount:     256,
			providerCount: 16,
			bootstrapLeg:  true,
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			cfg, providerPrices, feeds, now := benchmarkResolverInput(
				tc.feedCount,
				tc.providerCount,
				tc.bootstrapLeg,
			)

			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()

			var prices types.Prices
			for i := 0; i < b.N; i++ {
				prices = resolver.ResolvePrices(ctx, cfg, providerPrices, feeds, now)
			}

			if len(prices) != tc.feedCount {
				b.Fatalf("got %d prices, want %d", len(prices), tc.feedCount)
			}
		})
	}
}

func benchmarkResolverInput(
	feedCount int,
	providerCount int,
	bootstrapLeg bool,
) (resolver.Config, map[string]types.Prices, []string, time.Time) {
	now := time.Date(2026, time.July, 22, 0, 0, 0, 0, time.UTC)
	cfg := resolver.Config{
		Routes: make(map[string][]resolver.Route, feedCount),
	}
	if bootstrapLeg {
		cfg.BootstrapPrices = []resolver.BootstrapPrice{
			{
				Pair:       "NOAH/USD",
				Price:      "1",
				ValidUntil: now.Add(time.Hour).Format(time.RFC3339),
			},
		}
	}

	feeds := make([]string, feedCount)
	pairs := make([]types.Pair, feedCount)
	for i := 0; i < feedCount; i++ {
		denom := fmt.Sprintf("uasset%03d", i)
		quote := fmt.Sprintf("ASSET%03d", i)
		feeds[i] = denom

		if bootstrapLeg {
			pair := types.Pair("USD/" + quote)
			pairs[i] = pair
			cfg.Routes[denom] = []resolver.Route{
				{
					Name:  "noah-usd-" + quote,
					Pairs: []types.Pair{"NOAH/USD", pair},
				},
			}
			continue
		}

		pairs[i] = types.Pair("NOAH/" + quote)
	}

	providerPrices := make(map[string]types.Prices, providerCount)
	for providerIndex := 0; providerIndex < providerCount; providerIndex++ {
		prices := make(types.Prices, feedCount)
		for targetIndex, pair := range pairs {
			price := new(big.Float).
				SetPrec(types.PricePrecisionBits).
				SetFloat64(float64(targetIndex+1) + float64(providerIndex)/100)
			prices[pair] = price
		}
		providerPrices[fmt.Sprintf("provider-%02d", providerIndex)] = prices
	}

	return cfg, providerPrices, feeds, now
}
