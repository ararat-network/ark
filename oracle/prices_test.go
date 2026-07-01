package oracle

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/log/v2"

	"noah/oracle/providers/base"
	providertypes "noah/oracle/providers/types"
	"noah/oracle/types"
)

func TestFetchPricesSkipsStoppedProvider(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := newMockProvider(t, ctrl, "unknown", testMarkets(), []string{"uusd", "ukrw"})
	aggregator := newRecordingPriceAggregator()
	oracle := &Oracle{
		logger:     log.NewNopLogger(),
		aggregator: aggregator,
	}

	oracle.fetchPrices(provider.provider, time.Minute)

	require.Empty(t, aggregator.providerPrices)
}

func TestFetchPricesFiltersStaleProviderPrices(t *testing.T) {
	ctrl := gomock.NewController(t)
	aggregator := newRecordingPriceAggregator()
	oracle := &Oracle{
		logger:     log.NewNopLogger(),
		aggregator: aggregator,
	}
	now := time.Now().UTC()
	provider := newMockProvider(t, ctrl, "unknown", testMarkets(), []string{"uusd", "ukrw"})
	errCh := startProviderWithResponse(t, provider, providertypes.NewResponse(
		map[providertypes.Ticker]providertypes.Result{
			"USDTUSD": providertypes.NewResult(big.NewFloat(1.25), now),
			"KRWUSD":  providertypes.NewResult(big.NewFloat(2.50), now.Add(-2*time.Minute)),
		},
		nil,
	))
	defer func() {
		provider.provider.Stop()
		requireProviderStopped(t, errCh)
	}()

	require.Eventually(t, func() bool {
		return len(provider.provider.GetPrices()) == 2
	}, time.Second, time.Millisecond)

	oracle.fetchPrices(provider.provider, time.Minute)

	require.Contains(t, aggregator.providerPrices, "unknown")
	require.Contains(t, aggregator.providerPrices["unknown"], "uusd")
	require.NotContains(t, aggregator.providerPrices["unknown"], "ukrw")
	require.Zero(t, aggregator.providerPrices["unknown"]["uusd"].Cmp(big.NewFloat(1.25)))
}

func TestFetchAllPricesResetsAggregatesAndRecordsSyncTime(t *testing.T) {
	aggregator := newRecordingPriceAggregator()
	oracle := &Oracle{
		logger:     log.NewNopLogger(),
		aggregator: aggregator,
		providers:  map[string]*base.Provider{},
		cfg: Config{
			MaxPriceAge: time.Minute,
		},
	}

	oracle.fetchAllPrices()

	require.Equal(t, 1, aggregator.resetCount)
	require.Equal(t, 1, aggregator.aggregateCount)
	require.False(t, oracle.GetLastSyncTime().IsZero())
}

func TestFetchAllPricesRecordsMissingPriceMetricsFromOracleDenoms(t *testing.T) {
	registry := prometheus.NewRegistry()
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registry))
	require.NoError(t, err)

	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
	})
	otel.SetMeterProvider(provider)

	aggregator := newRecordingPriceAggregator()
	aggregator.prices = types.Prices{
		"uusd": big.NewFloat(1.23),
	}
	oracle := &Oracle{
		logger:     log.NewNopLogger(),
		aggregator: aggregator,
		providers:  map[string]*base.Provider{},
		cfg: Config{
			MaxPriceAge: time.Minute,
			Denoms:      []string{"uusd", "ukrw"},
		},
	}

	oracle.fetchAllPrices()

	families, err := registry.Gather()
	require.NoError(t, err)
	missingPrices := oracleMetricFamily(t, families, "noah_oracle_missing_prices_total")
	require.Equal(t, float64(1), oracleCounterValue(t, missingPrices, map[string]string{
		"denom": "ukrw",
	}))
}

type recordingPriceAggregator struct {
	providerPrices map[string]types.Prices
	prices         types.Prices
	resetCount     int
	aggregateCount int
}

func newRecordingPriceAggregator() *recordingPriceAggregator {
	return &recordingPriceAggregator{
		providerPrices: make(map[string]types.Prices),
	}
}

func (r *recordingPriceAggregator) SetProviderPrices(provider string, prices types.Prices) {
	r.providerPrices[provider] = prices
}

func (r *recordingPriceAggregator) AggregatePrices() {
	r.aggregateCount++
}

func (r *recordingPriceAggregator) GetPrices() types.Prices {
	return r.prices
}

func (r *recordingPriceAggregator) Reset() {
	r.resetCount++
	r.providerPrices = make(map[string]types.Prices)
}

func oracleMetricFamily(t *testing.T, families []*dto.MetricFamily, name string) *dto.MetricFamily {
	t.Helper()

	names := make([]string, 0, len(families))
	for _, family := range families {
		names = append(names, family.GetName())
		if family.GetName() == name {
			return family
		}
	}

	t.Fatalf("metric family %q not found in %v", name, names)
	return nil
}

func oracleCounterValue(
	t *testing.T,
	family *dto.MetricFamily,
	labels map[string]string,
) float64 {
	t.Helper()

	return oracleMatchingMetric(t, family, labels).GetCounter().GetValue()
}

func oracleMatchingMetric(
	t *testing.T,
	family *dto.MetricFamily,
	labels map[string]string,
) *dto.Metric {
	t.Helper()

	for _, metric := range family.Metric {
		actual := make(map[string]string, len(metric.Label))
		for _, label := range metric.Label {
			actual[label.GetName()] = label.GetValue()
		}
		matches := true
		for name, value := range labels {
			if actual[name] != value {
				matches = false
				break
			}
		}
		if matches {
			return metric
		}
	}

	t.Fatalf("metric with labels %v not found", labels)
	return nil
}

func startProviderWithResponse(
	t *testing.T,
	provider mockProvider,
	response providertypes.Response,
) <-chan error {
	t.Helper()

	provider.fetcher.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ []providertypes.Ticker, responseCh chan<- providertypes.Response) error {
			responseCh <- response
			<-ctx.Done()
			return ctx.Err()
		})

	errCh := make(chan error, 1)
	go func() {
		errCh <- provider.provider.Start(context.Background())
	}()

	return errCh
}
