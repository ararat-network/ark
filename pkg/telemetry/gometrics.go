package telemetry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"

	gometrics "github.com/hashicorp/go-metrics"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
)

// maxInstrumentNameLen is OTel's cap on an instrument name.
const maxInstrumentNameLen = 255

// GoMetricsSink bounds legacy instruments and isolates their exported names.
// SDK query paths are untrusted, including paths that do not start with '/'.
// Their timers become fixed instruments with a bounded route classification.
type GoMetricsSink struct {
	ctx           context.Context
	meter         metric.Meter
	config        GoMetricsConfig
	mu            sync.Mutex
	instruments   map[string]any
	queryDuration metric.Float64Histogram
}

// GoMetricsConfig describes the SDK's key prefix and registered query routes.
// IsQueryRoute must be a read-only lookup over the app's fixed service router.
type GoMetricsConfig struct {
	ServiceName  string
	IsQueryRoute func(string) bool
}

// maxLegacyInstruments bounds the cache even if a future SDK call site uses
// dynamic keys. Queries never consume this allowance. Existing entries remain
// usable when the limit is reached; further names record nowhere.
const maxLegacyInstruments = 1024

var (
	_ gometrics.MetricSink               = (*GoMetricsSink)(nil)
	_ gometrics.PrecisionGaugeMetricSink = (*GoMetricsSink)(nil)
)

// NewGoMetricsSink returns a sink recording into meter under ctx.
func NewGoMetricsSink(ctx context.Context, meter metric.Meter, config ...GoMetricsConfig) *GoMetricsSink {
	s := &GoMetricsSink{ctx: ctx, meter: meter, instruments: make(map[string]any)}
	if len(config) != 0 {
		s.config = config[0]
	}
	var err error
	s.queryDuration, err = meter.Float64Histogram("ark.sdk.query.duration", metric.WithUnit("ms"), metric.WithDescription("Duration of completed ABCI queries"))
	if err != nil || s.queryDuration == nil {
		s.queryDuration = metricnoop.Float64Histogram{}
	}
	return s
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

// legacyName uses only Prometheus-safe characters. The digest preserves
// distinctions lost by sanitisation (dots, slashes, underscores, truncation),
// and the kind keeps a histogram's generated suffixes away from other types.
func legacyName(kind string, key []string) string {
	raw := strings.Join(key, "\x00")
	digest := sha256.Sum256([]byte(raw))
	readable := SanitiseInstrumentName(strings.Join(key, "."))
	readable = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, readable)
	if len(readable) > 120 {
		readable = readable[:120]
	}
	return "sdk_legacy_" + kind + "_" + readable + "_" + hex.EncodeToString(digest[:])
}

func legacyInstrument[I any](s *GoMetricsSink, kind string, key []string, create func(string) (I, error), fallback I) I {
	name := legacyName(kind, key)
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.instruments[name]; ok {
		return existing.(I)
	}
	if len(s.instruments) >= maxLegacyInstruments {
		return fallback
	}
	inst, err := create(name)
	if err != nil {
		inst = fallback
	}
	s.instruments[name] = inst
	return inst
}

func (s *GoMetricsSink) gauge(key []string) metric.Float64Gauge {
	return legacyInstrument(s, "gauge", key, func(name string) (metric.Float64Gauge, error) {
		inst, err := s.meter.Float64Gauge(name)
		if inst == nil {
			inst = metricnoop.Float64Gauge{}
		}
		return inst, err
	}, metric.Float64Gauge(metricnoop.Float64Gauge{}))
}

func (s *GoMetricsSink) counter(key []string) metric.Float64Counter {
	return legacyInstrument(s, "counter", key, func(name string) (metric.Float64Counter, error) {
		inst, err := s.meter.Float64Counter(name)
		if inst == nil {
			inst = metricnoop.Float64Counter{}
		}
		return inst, err
	}, metric.Float64Counter(metricnoop.Float64Counter{}))
}

func (s *GoMetricsSink) histogram(key []string) metric.Float64Histogram {
	return legacyInstrument(s, "histogram", key, func(name string) (metric.Float64Histogram, error) {
		inst, err := s.meter.Float64Histogram(name)
		if inst == nil {
			inst = metricnoop.Float64Histogram{}
		}
		return inst, err
	}, metric.Float64Histogram(metricnoop.Float64Histogram{}))
}

func (s *GoMetricsSink) unprefixed(key []string) []string {
	if s.config.ServiceName != "" && len(key) > 0 && key[0] == s.config.ServiceName {
		return key[1:]
	}
	return key
}

func (s *GoMetricsSink) queryRoute(path string) string {
	if s.config.IsQueryRoute != nil && s.config.IsQueryRoute(path) {
		return path
	}
	// Legacy handlers accept arbitrary suffixes. Export only their category.
	category, _, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	switch category {
	case "app":
		return "app"
	case "store":
		return "store"
	case "p2p":
		return "p2p"
	default:
		return "unknown"
	}
}

// SDK v0.54 query timers have one key, the raw path. Module lifecycle timers
// also have one key, but carry a module label and one of these three constants.
// Classify before sanitising so even malformed paths cannot allocate instruments.
func (s *GoMetricsSink) recordQuery(key []string, value float32, labels []gometrics.Label) bool {
	key = s.unprefixed(key)
	if len(key) != 1 {
		return false
	}
	if key[0] == "pre_blocker" || key[0] == "begin_blocker" || key[0] == "end_blocker" {
		for _, label := range labels {
			if label.Name == "module" {
				return false
			}
		}
	}
	route := metric.WithAttributes(attribute.String("route", s.queryRoute(key[0])))
	s.queryDuration.Record(s.ctx, float64(value), attrs(labels), route)
	return true
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
	s.IncrCounterWithLabels(key, val, nil)
}

// IncrCounterWithLabels implements gometrics.MetricSink.
func (s *GoMetricsSink) IncrCounterWithLabels(key []string, val float32, labels []gometrics.Label) {
	// The deferred query timer records duration and its sample count once, including
	// a query literally named "count", indistinguishable from the SDK's total.
	unprefixed := s.unprefixed(key)
	if len(unprefixed) == 2 && unprefixed[0] == "query" {
		return
	}
	s.counter(key).Add(s.ctx, float64(val), attrs(labels))
}

// AddSample implements gometrics.MetricSink.
func (s *GoMetricsSink) AddSample(key []string, val float32) {
	s.AddSampleWithLabels(key, val, nil)
}

// AddSampleWithLabels implements gometrics.MetricSink.
func (s *GoMetricsSink) AddSampleWithLabels(key []string, val float32, labels []gometrics.Label) {
	if s.recordQuery(key, val, labels) {
		return
	}
	s.histogram(key).Record(s.ctx, float64(val), attrs(labels))
}

func attrs(labels []gometrics.Label) metric.MeasurementOption {
	kvs := make([]attribute.KeyValue, len(labels))
	for i, l := range labels {
		kvs[i] = attribute.String(l.Name, l.Value)
	}
	return metric.WithAttributes(kvs...)
}
