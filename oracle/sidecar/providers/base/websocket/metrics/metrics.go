package metrics

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	// ConnectionEventDialError records a websocket dial failure.
	ConnectionEventDialError = "dial_error"
	// ConnectionEventSubscribeError records a subscription setup failure.
	ConnectionEventSubscribeError = "subscribe_error"
	// ConnectionEventHealthy records a successfully established websocket connection.
	ConnectionEventHealthy = "healthy"
	// ConnectionEventReadError records a websocket read failure.
	ConnectionEventReadError = "read_error"

	// WriteOperationSubscribe labels subscription message write errors.
	WriteOperationSubscribe = "subscribe"
	// WriteOperationUpdate labels provider update message write errors.
	WriteOperationUpdate = "update"
	// WriteOperationHeartbeat labels heartbeat message write errors.
	WriteOperationHeartbeat = "heartbeat"
)

var (
	meter = otel.Meter("noah/oracle/sidecar/providers/base/websocket/metrics")

	connectionEvents metric.Int64Counter
	reconnects       metric.Int64Counter
	parseErrors      metric.Int64Counter
	writeErrors      metric.Int64Counter
)

func init() {
	var err error

	connectionEvents, err = meter.Int64Counter(
		"noah.oracle.provider.websocket.connection.events",
		metric.WithDescription("Number of websocket provider connection lifecycle events"),
	)
	if err != nil {
		panic(err)
	}

	reconnects, err = meter.Int64Counter(
		"noah.oracle.provider.websocket.reconnects",
		metric.WithDescription("Number of websocket provider reconnect attempts"),
	)
	if err != nil {
		panic(err)
	}

	parseErrors, err = meter.Int64Counter(
		"noah.oracle.provider.websocket.parse_errors",
		metric.WithDescription("Number of websocket provider message parse errors"),
	)
	if err != nil {
		panic(err)
	}

	writeErrors, err = meter.Int64Counter(
		"noah.oracle.provider.websocket.write_errors",
		metric.WithDescription("Number of websocket provider write errors"),
	)
	if err != nil {
		panic(err)
	}
}

// RecordConnectionEvent records a websocket connection lifecycle event.
func RecordConnectionEvent(ctx context.Context, provider string, event string) {
	connectionEvents.Add(
		ctx,
		1,
		metric.WithAttributes(
			attribute.String("provider", provider),
			attribute.String("event", event),
		),
	)
}

// RecordReconnect records a websocket reconnect attempt.
func RecordReconnect(ctx context.Context, provider string) {
	reconnects.Add(
		ctx,
		1,
		metric.WithAttributes(attribute.String("provider", provider)),
	)
}

// RecordParseError records a websocket message parse failure.
func RecordParseError(ctx context.Context, provider string) {
	parseErrors.Add(
		ctx,
		1,
		metric.WithAttributes(attribute.String("provider", provider)),
	)
}

// RecordWriteError records a websocket write failure for an operation.
func RecordWriteError(ctx context.Context, provider string, operation string) {
	writeErrors.Add(
		ctx,
		1,
		metric.WithAttributes(
			attribute.String("provider", provider),
			attribute.String("operation", operation),
		),
	)
}
