package runtime_test

import (
	"context"
	"errors"
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

	providertypes "noah/oracle/sidecar/providers/types"
	resolverpkg "noah/oracle/sidecar/resolver"
	"noah/oracle/sidecar/runtime"
	oracletestutil "noah/oracle/sidecar/runtime/testutil"
	"noah/oracle/sidecar/types"
)

func TestStartFiltersStaleProviderPricesAndRecordsSyncTime(t *testing.T) {
	ctrl := gomock.NewController(t)
	now := time.Now().UTC()
	provider := newMockProvider(t, ctrl, "unknown", testMarkets())
	provider.fetcher.EXPECT().
		Run(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ []providertypes.Ticker, responseCh chan<- providertypes.Response) error {
			responseCh <- providertypes.NewResponse(
				map[providertypes.Ticker]providertypes.Result{
					"USDTUSD": providertypes.NewResult(big.NewFloat(1.25), now),
					"KRWUSD":  providertypes.NewResult(big.NewFloat(2.50), now.Add(-2*time.Minute)),
				},
				nil,
			)
			<-ctx.Done()
			return ctx.Err()
		})

	cfg := testOracleConfig(nil)
	cfg.UpdateInterval = 5 * time.Millisecond
	oracle, err := runtime.NewRuntime(cfg, runtime.WithProviders(provider.provider))
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()

	require.Eventually(t, func() bool {
		prices := oracle.GetPrices()
		if prices["uusd"] == nil || prices["uusd"].Cmp(big.NewFloat(1.25)) != 0 {
			return false
		}
		if _, ok := prices["ukrw"]; ok {
			return false
		}
		return !oracle.GetLastSyncTime().IsZero()
	}, time.Second, time.Millisecond)

	cancel()
	requireOracleStopped(t, errCh)
}

func TestStartRecordsMissingPriceMetricsFromFallbackDenoms(t *testing.T) {
	registry := prometheus.NewRegistry()
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registry))
	require.NoError(t, err)

	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
	})
	otel.SetMeterProvider(provider)

	cfg := testOracleConfig(nil)
	cfg.UpdateInterval = 5 * time.Millisecond
	oracle, err := runtime.NewRuntime(cfg)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()

	require.Eventually(t, func() bool {
		families, err := registry.Gather()
		if err != nil {
			return false
		}
		missingPrices := findOracleMetricFamily(families, "noah_oracle_missing_prices_total")
		if missingPrices == nil {
			return false
		}
		value, ok := oracleCounterValue(missingPrices, map[string]string{"denom": "ukrw"})
		return ok && value >= 1
	}, time.Second, time.Millisecond)

	cancel()
	requireOracleStopped(t, errCh)
}

func TestStartUsesVoteTargetsWhenRefreshSucceeds(t *testing.T) {
	ctrl := gomock.NewController(t)
	voteTargetsClient := oracletestutil.NewMockVoteTargetsClient(ctrl)
	voteTargetsClient.EXPECT().
		VoteTargets().
		Return([]string{"uusd", "ukrw"}, nil).
		AnyTimes()

	resolver := newRecordingResolver(nil)
	cfg := testOracleConfig(nil)
	cfg.UpdateInterval = 5 * time.Millisecond
	cfg.FallbackDenoms = []string{"ufallback"}
	oracle, err := runtime.NewRuntime(
		cfg,
		runtime.WithResolver(resolver),
		runtime.WithVoteTargetsClient(voteTargetsClient),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()

	require.Equal(t, []string{"uusd", "ukrw"}, requireResolvedDenoms(t, resolver))

	cancel()
	requireOracleStopped(t, errCh)
}

func TestStartUsesFallbackDenomsWhenVoteTargetsFailBeforeSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	voteTargetsClient := oracletestutil.NewMockVoteTargetsClient(ctrl)
	voteTargetsClient.EXPECT().
		VoteTargets().
		Return(nil, errors.New("node unavailable")).
		AnyTimes()

	resolver := newRecordingResolver(nil)
	cfg := testOracleConfig(nil)
	cfg.UpdateInterval = 5 * time.Millisecond
	cfg.FallbackDenoms = []string{"ufallback"}
	oracle, err := runtime.NewRuntime(
		cfg,
		runtime.WithResolver(resolver),
		runtime.WithVoteTargetsClient(voteTargetsClient),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()

	require.Equal(t, []string{"ufallback"}, requireResolvedDenoms(t, resolver))

	cancel()
	requireOracleStopped(t, errCh)
}

func TestStartKeepsLastVoteTargetsAfterRefreshFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	voteTargetsClient := oracletestutil.NewMockVoteTargetsClient(ctrl)
	calls := 0
	voteTargetsClient.EXPECT().
		VoteTargets().
		DoAndReturn(func() ([]string, error) {
			calls++
			if calls == 1 {
				return []string{"uusd"}, nil
			}
			return nil, errors.New("node unavailable")
		}).
		AnyTimes()

	resolver := newRecordingResolver(nil)
	cfg := testOracleConfig(nil)
	cfg.UpdateInterval = 5 * time.Millisecond
	cfg.FallbackDenoms = []string{"ufallback"}
	oracle, err := runtime.NewRuntime(
		cfg,
		runtime.WithResolver(resolver),
		runtime.WithVoteTargetsClient(voteTargetsClient),
	)
	require.NoError(t, err)

	errCh, cancel := startOracle(t, oracle)
	defer cancel()

	require.Equal(t, []string{"uusd"}, requireResolvedDenoms(t, resolver))
	require.Equal(t, []string{"uusd"}, requireResolvedDenoms(t, resolver))
	require.GreaterOrEqual(t, calls, 2)

	cancel()
	requireOracleStopped(t, errCh)
}

type recordingResolver struct {
	prices    types.Prices
	resolveCh chan []string
	updateErr error
}

func newRecordingResolver(prices types.Prices) *recordingResolver {
	return &recordingResolver{
		prices:    prices,
		resolveCh: make(chan []string, 10),
	}
}

func (r *recordingResolver) SetProviderPrices(string, types.Prices) {}

func (r *recordingResolver) ResolvePrices(denoms []string) {
	r.resolveCh <- append([]string(nil), denoms...)
}

func (r *recordingResolver) GetPrices() types.Prices {
	prices := make(types.Prices, len(r.prices))
	for pair, price := range r.prices {
		prices[pair] = new(big.Float).Copy(price)
	}
	return prices
}

func (r *recordingResolver) UpdateConfig(resolverpkg.Config) error { return r.updateErr }

func (r *recordingResolver) Reset() {}

func requireResolvedDenoms(t *testing.T, resolver *recordingResolver) []string {
	t.Helper()

	select {
	case denoms := <-resolver.resolveCh:
		return denoms
	case <-time.After(time.Second):
		t.Fatal("oracle did not resolve prices")
		return nil
	}
}

func startOracle(t *testing.T, oracle *runtime.Runtime) (<-chan error, context.CancelFunc) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- oracle.Start(ctx)
	}()
	requireOracleStarted(t, oracle)

	return errCh, cancel
}

func findOracleMetricFamily(families []*dto.MetricFamily, name string) *dto.MetricFamily {
	for _, family := range families {
		if family.GetName() == name {
			return family
		}
	}
	return nil
}

func oracleCounterValue(
	family *dto.MetricFamily,
	labels map[string]string,
) (float64, bool) {
	metric, ok := oracleMatchingMetric(family, labels)
	if !ok {
		return 0, false
	}
	return metric.GetCounter().GetValue(), true
}

func oracleMatchingMetric(
	family *dto.MetricFamily,
	labels map[string]string,
) (*dto.Metric, bool) {
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
			return metric, true
		}
	}

	return nil, false
}
