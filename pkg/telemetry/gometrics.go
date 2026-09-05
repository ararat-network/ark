package telemetry

import (
	"context"
	"strings"
	"sync"

	gometrics "github.com/hashicorp/go-metrics"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

// maxInstrumentNameLen is OTel's cap on an instrument name.
const maxInstrumentNameLen = 255

// histogramBoundaries are the buckets the SDK's own bridge uses, kept so the
// exported series do not change with the sink.
var histogramBoundaries = metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10)

// GoMetricsSink bridges the SDK's legacy go-metrics into an OTel meter the
// way cosmos-sdk's "otel" sink does, with two differences: instrument names
// are sanitised, and nothing panics. The SDK's sink hands raw keys to the
// meter and panics when OTel rejects one, and baseapp records every ABCI
// query under its request path — "/cosmos.auth.v1beta1.Query/Account" —
// which OTel refuses because a name must start with a letter, so under that
// sink every CLI query over CometBFT RPC fails. A name OTel still refuses
// after sanitising records nowhere.
type GoMetricsSink struct {
	ctx        context.Context
	meter      metric.Meter
	counters   sync.Map
	gauges     sync.Map
	histograms sync.Map
}

var (
	_ gometrics.MetricSink               = (*GoMetricsSink)(nil)
	_ gometrics.PrecisionGaugeMetricSink = (*GoMetricsSink)(nil)
)

// NewGoMetricsSink returns a sink recording into meter under ctx.
func NewGoMetricsSink(ctx context.Context, meter metric.Meter) *GoMetricsSink {
	return &GoMetricsSink{ctx: ctx, meter: meter}
}

// SanitiseInstrumentName maps a go-metrics key onto a valid OTel instrument
// name: an ASCII letter first, then ASCII letters, digits, '_', '.', '-'
// and '/', at most 255 characters. Characters that cannot open a name are
// dropped until one can, every later invalid byte becomes '_', and a key
// holding no letter at all becomes "unnamed".
func SanitiseInstrumentName(key string) string {
	var b strings.Builder
	for i := 0; i < len(key) && b.Len() < maxInstrumentNameLen; i++ {
		c := key[i]
		switch {
		case isASCIILetter(c):
			b.WriteByte(c)
		case b.Len() == 0:
			// Still looking for the letter that opens the name.
		case (c >= '0' && c <= '9') || c == '_' || c == '.' || c == '-' || c == '/':
			b.WriteByte(c)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "unnamed"
	}
	return b.String()
}

func isASCIILetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// instrumentName flattens a go-metrics key as the SDK's bridge does, joined
// with dots, then sanitises it.
func instrumentName(key []string) string {
	return SanitiseInstrumentName(strings.Join(key, "."))
}

func (s *GoMetricsSink) gauge(key []string) metric.Float64Gauge {
	name := instrumentName(key)
	if entry, ok := s.gauges.Load(name); ok {
		return entry.(metric.Float64Gauge)
	}
	inst, err := s.meter.Float64Gauge(name)
	if err != nil || inst == nil {
		inst = noop.Float64Gauge{}
	}
	entry, _ := s.gauges.LoadOrStore(name, inst)
	return entry.(metric.Float64Gauge)
}

func (s *GoMetricsSink) counter(key []string) metric.Float64Counter {
	name := instrumentName(key)
	if entry, ok := s.counters.Load(name); ok {
		return entry.(metric.Float64Counter)
	}
	inst, err := s.meter.Float64Counter(name)
	if err != nil || inst == nil {
		inst = noop.Float64Counter{}
	}
	entry, _ := s.counters.LoadOrStore(name, inst)
	return entry.(metric.Float64Counter)
}

func (s *GoMetricsSink) histogram(key []string) metric.Float64Histogram {
	name := instrumentName(key)
	if entry, ok := s.histograms.Load(name); ok {
		return entry.(metric.Float64Histogram)
	}
	inst, err := s.meter.Float64Histogram(name, histogramBoundaries)
	if err != nil || inst == nil {
		inst = noop.Float64Histogram{}
	}
	entry, _ := s.histograms.LoadOrStore(name, inst)
	return entry.(metric.Float64Histogram)
}

// SetGauge implements gometrics.MetricSink.
func (s *GoMetricsSink) SetGauge(key []string, val float32) {
	s.gauge(key).Record(s.ctx, float64(val))
}

// SetGaugeWithLabels implements gometrics.MetricSink.
func (s *GoMetricsSink) SetGaugeWithLabels(key []string, val float32, labels []gometrics.Label) {
	s.gauge(key).Record(s.ctx, float64(val), attrs(labels))
}

// SetPrecisionGauge implements gometrics.PrecisionGaugeMetricSink.
func (s *GoMetricsSink) SetPrecisionGauge(key []string, val float64) {
	s.gauge(key).Record(s.ctx, val)
}

// SetPrecisionGaugeWithLabels implements gometrics.PrecisionGaugeMetricSink.
func (s *GoMetricsSink) SetPrecisionGaugeWithLabels(key []string, val float64, labels []gometrics.Label) {
	s.gauge(key).Record(s.ctx, val, attrs(labels))
}

// EmitKey implements gometrics.MetricSink.
func (s *GoMetricsSink) EmitKey(key []string, val float32) {
	s.histogram(key).Record(s.ctx, float64(val))
}

// IncrCounter implements gometrics.MetricSink.
func (s *GoMetricsSink) IncrCounter(key []string, val float32) {
	s.counter(key).Add(s.ctx, float64(val))
}

// IncrCounterWithLabels implements gometrics.MetricSink.
func (s *GoMetricsSink) IncrCounterWithLabels(key []string, val float32, labels []gometrics.Label) {
	s.counter(key).Add(s.ctx, float64(val), attrs(labels))
}

// AddSample implements gometrics.MetricSink.
func (s *GoMetricsSink) AddSample(key []string, val float32) {
	s.histogram(key).Record(s.ctx, float64(val))
}

// AddSampleWithLabels implements gometrics.MetricSink.
func (s *GoMetricsSink) AddSampleWithLabels(key []string, val float32, labels []gometrics.Label) {
	s.histogram(key).Record(s.ctx, float64(val), attrs(labels))
}

func attrs(labels []gometrics.Label) metric.MeasurementOption {
	kvs := make([]attribute.KeyValue, len(labels))
	for i, l := range labels {
		kvs[i] = attribute.String(l.Name, l.Value)
	}
	return metric.WithAttributes(kvs...)
}
