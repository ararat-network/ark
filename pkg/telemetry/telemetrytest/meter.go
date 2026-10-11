package telemetrytest

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/embedded"
)

// The OpenTelemetry global binds instruments created before its first
// SetMeterProvider to that provider for the life of the process. Subsystems
// create theirs at init, so a test installing a fresh provider would see only
// the first test's. The relay is installed once as that first provider and
// forwards to whichever provider the current test chose.
var (
	relay       = &relayProvider{}
	installOnce sync.Once
)

// RelayMeters installs the relay as the global meter provider. It must run
// before anything else in the process sets one; later calls do nothing.
func RelayMeters() {
	installOnce.Do(func() { otel.SetMeterProvider(relay) })
}

// SetMeterProvider points every meter in the process at mp until tb ends, then
// back at nothing.
func SetMeterProvider(tb testing.TB, mp metric.MeterProvider) {
	tb.Helper()

	RelayMeters()
	relay.retarget(mp)
	tb.Cleanup(func() { relay.retarget(nil) })
}

type relayProvider struct {
	embedded.MeterProvider

	mu     sync.Mutex
	target metric.MeterProvider
	meters []*relayMeter
}

func (p *relayProvider) Meter(name string, opts ...metric.MeterOption) metric.Meter {
	p.mu.Lock()
	defer p.mu.Unlock()

	m := &relayMeter{name: name, opts: opts}
	p.meters = append(p.meters, m)
	m.bind(p.target)
	return m
}

func (p *relayProvider) retarget(mp metric.MeterProvider) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if mp == p {
		mp = nil
	}
	p.target = mp
	for _, m := range p.meters {
		m.bind(mp)
	}
}

type relayMeter struct {
	embedded.Meter

	name string
	opts []metric.MeterOption

	mu          sync.Mutex
	target      metric.Meter
	instruments []relayed
	callbacks   []*relayRegistration
}

// relayed is an instrument that can rebind to another meter; a nil meter
// drops its measurements.
type relayed interface {
	bind(metric.Meter)
}

func (m *relayMeter) bind(mp metric.MeterProvider) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.target = nil
	if mp != nil {
		m.target = mp.Meter(m.name, m.opts...)
	}
	for _, inst := range m.instruments {
		inst.bind(m.target)
	}
	for _, reg := range m.callbacks {
		reg.bind(m.target)
	}
}

func (m *relayMeter) add(inst relayed) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.instruments = append(m.instruments, inst)
	inst.bind(m.target)
}

func (m *relayMeter) RegisterCallback(f metric.Callback, insts ...metric.Observable) (metric.Registration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	reg := &relayRegistration{meter: m, callback: f, instruments: insts}
	m.callbacks = append(m.callbacks, reg)
	reg.bind(m.target)
	return reg, nil
}

type relayRegistration struct {
	embedded.Registration

	meter       *relayMeter
	callback    metric.Callback
	instruments []metric.Observable

	mu      sync.Mutex
	current metric.Registration
}

func (r *relayRegistration) bind(target metric.Meter) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.current != nil {
		_ = r.current.Unregister()
		r.current = nil
	}
	if target == nil {
		return
	}
	insts := make([]metric.Observable, 0, len(r.instruments))
	for _, inst := range r.instruments {
		if t := observableTarget(inst); t != nil {
			insts = append(insts, t)
		}
	}
	callback := func(ctx context.Context, o metric.Observer) error {
		return r.callback(ctx, relayObserver{target: o})
	}
	reg, err := target.RegisterCallback(callback, insts...)
	if err != nil {
		otel.Handle(err)
		return
	}
	r.current = reg
}

func (r *relayRegistration) Unregister() error {
	m := r.meter
	m.mu.Lock()
	for i, reg := range m.callbacks {
		if reg == r {
			m.callbacks = append(m.callbacks[:i], m.callbacks[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
	r.bind(nil)
	return nil
}

// relayObserver translates relay observables in a callback into the current
// target's.
type relayObserver struct {
	embedded.Observer

	target metric.Observer
}

func (o relayObserver) ObserveFloat64(inst metric.Float64Observable, v float64, opts ...metric.ObserveOption) {
	if t, ok := observableTarget(inst).(metric.Float64Observable); ok {
		o.target.ObserveFloat64(t, v, opts...)
	}
}

func (o relayObserver) ObserveInt64(inst metric.Int64Observable, v int64, opts ...metric.ObserveOption) {
	if t, ok := observableTarget(inst).(metric.Int64Observable); ok {
		o.target.ObserveInt64(t, v, opts...)
	}
}

func observableTarget(inst metric.Observable) metric.Observable {
	if r, ok := inst.(interface{ observable() metric.Observable }); ok {
		return r.observable()
	}
	return nil
}

// slot holds an instrument's current target and how to create it on a meter.
type slot[T any] struct {
	create func(metric.Meter) (T, error)
	v      atomic.Pointer[T]
}

func (s *slot[T]) bind(m metric.Meter) {
	if m == nil {
		s.v.Store(nil)
		return
	}
	v, err := s.create(m)
	if err != nil {
		otel.Handle(err)
		s.v.Store(nil)
		return
	}
	s.v.Store(&v)
}

func (s *slot[T]) get() (T, bool) {
	if p := s.v.Load(); p != nil {
		return *p, true
	}
	var zero T
	return zero, false
}

func (m *relayMeter) Int64Counter(name string, opts ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	i := &int64Counter{}
	i.create = func(t metric.Meter) (metric.Int64Counter, error) { return t.Int64Counter(name, opts...) }
	m.add(i)
	return i, nil
}

func (m *relayMeter) Int64UpDownCounter(name string, opts ...metric.Int64UpDownCounterOption) (metric.Int64UpDownCounter, error) {
	i := &int64UpDownCounter{}
	i.create = func(t metric.Meter) (metric.Int64UpDownCounter, error) { return t.Int64UpDownCounter(name, opts...) }
	m.add(i)
	return i, nil
}

func (m *relayMeter) Int64Histogram(name string, opts ...metric.Int64HistogramOption) (metric.Int64Histogram, error) {
	i := &int64Histogram{}
	i.create = func(t metric.Meter) (metric.Int64Histogram, error) { return t.Int64Histogram(name, opts...) }
	m.add(i)
	return i, nil
}

func (m *relayMeter) Int64Gauge(name string, opts ...metric.Int64GaugeOption) (metric.Int64Gauge, error) {
	i := &int64Gauge{}
	i.create = func(t metric.Meter) (metric.Int64Gauge, error) { return t.Int64Gauge(name, opts...) }
	m.add(i)
	return i, nil
}

func (m *relayMeter) Int64ObservableCounter(name string, opts ...metric.Int64ObservableCounterOption) (metric.Int64ObservableCounter, error) {
	i := &int64ObservableCounter{}
	i.create = func(t metric.Meter) (metric.Int64ObservableCounter, error) {
		return t.Int64ObservableCounter(name, opts...)
	}
	m.add(i)
	return i, nil
}

func (m *relayMeter) Int64ObservableUpDownCounter(name string, opts ...metric.Int64ObservableUpDownCounterOption) (metric.Int64ObservableUpDownCounter, error) {
	i := &int64ObservableUpDownCounter{}
	i.create = func(t metric.Meter) (metric.Int64ObservableUpDownCounter, error) {
		return t.Int64ObservableUpDownCounter(name, opts...)
	}
	m.add(i)
	return i, nil
}

func (m *relayMeter) Int64ObservableGauge(name string, opts ...metric.Int64ObservableGaugeOption) (metric.Int64ObservableGauge, error) {
	i := &int64ObservableGauge{}
	i.create = func(t metric.Meter) (metric.Int64ObservableGauge, error) {
		return t.Int64ObservableGauge(name, opts...)
	}
	m.add(i)
	return i, nil
}

func (m *relayMeter) Float64Counter(name string, opts ...metric.Float64CounterOption) (metric.Float64Counter, error) {
	i := &float64Counter{}
	i.create = func(t metric.Meter) (metric.Float64Counter, error) { return t.Float64Counter(name, opts...) }
	m.add(i)
	return i, nil
}

func (m *relayMeter) Float64UpDownCounter(name string, opts ...metric.Float64UpDownCounterOption) (metric.Float64UpDownCounter, error) {
	i := &float64UpDownCounter{}
	i.create = func(t metric.Meter) (metric.Float64UpDownCounter, error) {
		return t.Float64UpDownCounter(name, opts...)
	}
	m.add(i)
	return i, nil
}

func (m *relayMeter) Float64Histogram(name string, opts ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	i := &float64Histogram{}
	i.create = func(t metric.Meter) (metric.Float64Histogram, error) { return t.Float64Histogram(name, opts...) }
	m.add(i)
	return i, nil
}

func (m *relayMeter) Float64Gauge(name string, opts ...metric.Float64GaugeOption) (metric.Float64Gauge, error) {
	i := &float64Gauge{}
	i.create = func(t metric.Meter) (metric.Float64Gauge, error) { return t.Float64Gauge(name, opts...) }
	m.add(i)
	return i, nil
}

func (m *relayMeter) Float64ObservableCounter(name string, opts ...metric.Float64ObservableCounterOption) (metric.Float64ObservableCounter, error) {
	i := &float64ObservableCounter{}
	i.create = func(t metric.Meter) (metric.Float64ObservableCounter, error) {
		return t.Float64ObservableCounter(name, opts...)
	}
	m.add(i)
	return i, nil
}

func (m *relayMeter) Float64ObservableUpDownCounter(name string, opts ...metric.Float64ObservableUpDownCounterOption) (metric.Float64ObservableUpDownCounter, error) {
	i := &float64ObservableUpDownCounter{}
	i.create = func(t metric.Meter) (metric.Float64ObservableUpDownCounter, error) {
		return t.Float64ObservableUpDownCounter(name, opts...)
	}
	m.add(i)
	return i, nil
}

func (m *relayMeter) Float64ObservableGauge(name string, opts ...metric.Float64ObservableGaugeOption) (metric.Float64ObservableGauge, error) {
	i := &float64ObservableGauge{}
	i.create = func(t metric.Meter) (metric.Float64ObservableGauge, error) {
		return t.Float64ObservableGauge(name, opts...)
	}
	m.add(i)
	return i, nil
}

type int64Counter struct {
	embedded.Int64Counter
	slot[metric.Int64Counter]
}

func (i *int64Counter) Add(ctx context.Context, v int64, opts ...metric.AddOption) {
	if t, ok := i.get(); ok {
		t.Add(ctx, v, opts...)
	}
}

func (i *int64Counter) Enabled(ctx context.Context) bool {
	t, ok := i.get()
	return ok && t.Enabled(ctx)
}

type int64UpDownCounter struct {
	embedded.Int64UpDownCounter
	slot[metric.Int64UpDownCounter]
}

func (i *int64UpDownCounter) Add(ctx context.Context, v int64, opts ...metric.AddOption) {
	if t, ok := i.get(); ok {
		t.Add(ctx, v, opts...)
	}
}

func (i *int64UpDownCounter) Enabled(ctx context.Context) bool {
	t, ok := i.get()
	return ok && t.Enabled(ctx)
}

type int64Histogram struct {
	embedded.Int64Histogram
	slot[metric.Int64Histogram]
}

func (i *int64Histogram) Record(ctx context.Context, v int64, opts ...metric.RecordOption) {
	if t, ok := i.get(); ok {
		t.Record(ctx, v, opts...)
	}
}

func (i *int64Histogram) Enabled(ctx context.Context) bool {
	t, ok := i.get()
	return ok && t.Enabled(ctx)
}

type int64Gauge struct {
	embedded.Int64Gauge
	slot[metric.Int64Gauge]
}

func (i *int64Gauge) Record(ctx context.Context, v int64, opts ...metric.RecordOption) {
	if t, ok := i.get(); ok {
		t.Record(ctx, v, opts...)
	}
}

func (i *int64Gauge) Enabled(ctx context.Context) bool {
	t, ok := i.get()
	return ok && t.Enabled(ctx)
}

type float64Counter struct {
	embedded.Float64Counter
	slot[metric.Float64Counter]
}

func (i *float64Counter) Add(ctx context.Context, v float64, opts ...metric.AddOption) {
	if t, ok := i.get(); ok {
		t.Add(ctx, v, opts...)
	}
}

func (i *float64Counter) Enabled(ctx context.Context) bool {
	t, ok := i.get()
	return ok && t.Enabled(ctx)
}

type float64UpDownCounter struct {
	embedded.Float64UpDownCounter
	slot[metric.Float64UpDownCounter]
}

func (i *float64UpDownCounter) Add(ctx context.Context, v float64, opts ...metric.AddOption) {
	if t, ok := i.get(); ok {
		t.Add(ctx, v, opts...)
	}
}

func (i *float64UpDownCounter) Enabled(ctx context.Context) bool {
	t, ok := i.get()
	return ok && t.Enabled(ctx)
}

type float64Histogram struct {
	embedded.Float64Histogram
	slot[metric.Float64Histogram]
}

func (i *float64Histogram) Record(ctx context.Context, v float64, opts ...metric.RecordOption) {
	if t, ok := i.get(); ok {
		t.Record(ctx, v, opts...)
	}
}

func (i *float64Histogram) Enabled(ctx context.Context) bool {
	t, ok := i.get()
	return ok && t.Enabled(ctx)
}

type float64Gauge struct {
	embedded.Float64Gauge
	slot[metric.Float64Gauge]
}

func (i *float64Gauge) Record(ctx context.Context, v float64, opts ...metric.RecordOption) {
	if t, ok := i.get(); ok {
		t.Record(ctx, v, opts...)
	}
}

func (i *float64Gauge) Enabled(ctx context.Context) bool {
	t, ok := i.get()
	return ok && t.Enabled(ctx)
}

// Observable instruments embed the API interface for its unexported marker
// methods, as the OpenTelemetry global does.

type int64ObservableCounter struct {
	embedded.Int64ObservableCounter
	metric.Int64Observable
	slot[metric.Int64ObservableCounter]
}

func (i *int64ObservableCounter) observable() metric.Observable {
	if t, ok := i.get(); ok {
		return t
	}
	return nil
}

type int64ObservableUpDownCounter struct {
	embedded.Int64ObservableUpDownCounter
	metric.Int64Observable
	slot[metric.Int64ObservableUpDownCounter]
}

func (i *int64ObservableUpDownCounter) observable() metric.Observable {
	if t, ok := i.get(); ok {
		return t
	}
	return nil
}

type int64ObservableGauge struct {
	embedded.Int64ObservableGauge
	metric.Int64Observable
	slot[metric.Int64ObservableGauge]
}

func (i *int64ObservableGauge) observable() metric.Observable {
	if t, ok := i.get(); ok {
		return t
	}
	return nil
}

type float64ObservableCounter struct {
	embedded.Float64ObservableCounter
	metric.Float64Observable
	slot[metric.Float64ObservableCounter]
}

func (i *float64ObservableCounter) observable() metric.Observable {
	if t, ok := i.get(); ok {
		return t
	}
	return nil
}

type float64ObservableUpDownCounter struct {
	embedded.Float64ObservableUpDownCounter
	metric.Float64Observable
	slot[metric.Float64ObservableUpDownCounter]
}

func (i *float64ObservableUpDownCounter) observable() metric.Observable {
	if t, ok := i.get(); ok {
		return t
	}
	return nil
}

type float64ObservableGauge struct {
	embedded.Float64ObservableGauge
	metric.Float64Observable
	slot[metric.Float64ObservableGauge]
}

func (i *float64ObservableGauge) observable() metric.Observable {
	if t, ok := i.get(); ok {
		return t
	}
	return nil
}
