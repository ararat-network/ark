package metrics

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("ark/pricefeed/sidecar/providers/base/api/metrics")

	requestDuration metric.Float64Histogram
)

func init() {
	var err error

	requestDuration, err = meter.Float64Histogram(
		"ark.pricefeed.provider.api.request.duration",
		metric.WithDescription("Duration of API provider HTTP requests"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		panic(err)
	}
}

// RecordRequest records the latency and outcome of one API provider HTTP
// request. The status code is the one label: the HTTP status when a response
// arrived, "none" when the transport failed before one did.
func RecordRequest(
	ctx context.Context,
	provider string,
	duration time.Duration,
	resp *http.Response,
	_ error,
) {
	attrs := metric.WithAttributes(
		attribute.String("provider", provider),
		attribute.String("status_code", statusCode(resp)),
	)

	requestDuration.Record(ctx, durationMillis(duration), attrs)
}

func durationMillis(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}

func statusCode(resp *http.Response) string {
	if resp == nil {
		return "none"
	}
	return strconv.Itoa(resp.StatusCode)
}
