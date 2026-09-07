package sidecar

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The global binds package-level instruments to the first provider installed
// in the process, so this package gets one provider-installing test.
func TestUnaryMetricsPassesThroughAndRecords(t *testing.T) {
	registry := prometheus.NewRegistry()
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registry))
	require.NoError(t, err)
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
	})
	otel.SetMeterProvider(provider)

	info := &grpc.UnaryServerInfo{FullMethod: "/ark.pricefeed.v1.PriceFeed/Prices"}
	resp, err := unaryMetrics(context.Background(), "req", info, func(_ context.Context, req any) (any, error) {
		require.Equal(t, "req", req)
		return "resp", nil
	})
	require.NoError(t, err)
	require.Equal(t, "resp", resp)

	wantErr := status.Error(codes.Unavailable, "no snapshot")
	_, err = unaryMetrics(context.Background(), nil, info, func(context.Context, any) (any, error) {
		return nil, wantErr
	})
	require.ErrorIs(t, err, wantErr)

	// A nil info never panics; the method label is simply empty.
	_, err = unaryMetrics(context.Background(), nil, nil, func(context.Context, any) (any, error) {
		return nil, nil
	})
	require.NoError(t, err)

	families, err := registry.Gather()
	require.NoError(t, err)
	counts := map[string]float64{}
	for _, family := range families {
		if family.GetName() != "ark_pricefeed_rpc_requests_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := labelsOf(metric)
			counts[labels["method"]+"|"+labels["code"]] = metric.GetCounter().GetValue()
		}
	}
	require.Equal(t, map[string]float64{
		"/ark.pricefeed.v1.PriceFeed/Prices|OK":          1,
		"/ark.pricefeed.v1.PriceFeed/Prices|Unavailable": 1,
		"|OK": 1,
	}, counts)
}

func labelsOf(metric *dto.Metric) map[string]string {
	labels := make(map[string]string, len(metric.GetLabel()))
	for _, label := range metric.GetLabel() {
		labels[label.GetName()] = label.GetValue()
	}
	return labels
}
