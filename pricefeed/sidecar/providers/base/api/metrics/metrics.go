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
	requests        metric.Int64Counter
	cycleDuration   metric.Float64Histogram
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

	requests, err = meter.Int64Counter(
		"ark.pricefeed.provider.api.requests",
		metric.WithDescription("Number of API provider HTTP requests"),
	)
	if err != nil {
		panic(err)
	}

	cycleDuration, err = meter.Float64Histogram(
		"ark.pricefeed.provider.api.cycle.duration",
		metric.WithDescription("Duration of API provider polling cycles"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		panic(err)
	}
}

// RecordRequest records the latency and outcome of one API provider HTTP request.
func RecordRequest(
	ctx context.Context,
	provider string,
	duration time.Duration,
	resp *http.Response,
	err error,
) {
	attrs := metric.WithAttributes(
		attribute.String("provider", provider),
		attribute.String("status", requestStatus(resp, err)),
		attribute.String("status_code", statusCode(resp)),
		attribute.String("status_class", statusClass(resp)),
	)

	requestDuration.Record(ctx, durationMillis(duration), attrs)
	requests.Add(ctx, 1, attrs)
}

// RecordCycle records the duration and outcome of one API provider polling cycle.
func RecordCycle(ctx context.Context, provider string, duration time.Duration, err error) {
	cycleDuration.Record(
		ctx,
		durationMillis(duration),
		metric.WithAttributes(
			attribute.String("provider", provider),
			attribute.String("status", errorStatus(err)),
		),
	)
}

func durationMillis(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}

func requestStatus(resp *http.Response, err error) string {
	if err != nil {
		return "failure"
	}
	if resp == nil {
		return "success"
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "failure"
	}
	return "success"
}

func errorStatus(err error) string {
	if err != nil {
		return "failure"
	}
	return "success"
}

func statusCode(resp *http.Response) string {
	if resp == nil {
		return "none"
	}
	return strconv.Itoa(resp.StatusCode)
}

func statusClass(resp *http.Response) string {
	if resp == nil {
		return "none"
	}
	return strconv.Itoa(resp.StatusCode/100) + "xx"
}
