package cmd

import (
	"context"
	"errors"
	"fmt"

	gometrics "github.com/hashicorp/go-metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/spf13/cast"
	"github.com/spf13/viper"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"golang.org/x/sync/errgroup"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdktelemetry "github.com/cosmos/cosmos-sdk/telemetry"
	"github.com/cosmos/cosmos-sdk/telemetry/registry"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/pkg/telemetry"
)

// prometheusEndpoint is the node's scrape endpoint: a registry of its own and
// the provider exporting into it.
type prometheusEndpoint struct {
	address  string
	registry *prometheus.Registry
	provider *sdkmetric.MeterProvider
	// hostBaseapp says the endpoint starts the SDK's baseapp instrument on
	// its provider: yes unless otel.yaml names the instrument, in which case
	// the SDK starts it in its own init, on this provider as well.
	hostBaseapp bool
}

// newPrometheusEndpoint returns the configured endpoint or nil when disabled. Its private registry
// prevents collisions and cross-posting with CometBFT and SDK legacy collectors.
func newPrometheusEndpoint(svrCtx *server.Context, cfg telemetry.PrometheusConfig, otelCfg otelFile) (*prometheusEndpoint, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	chainID, err := startChainID(svrCtx)
	if err != nil {
		return nil, err
	}

	registry := prometheus.NewRegistry()
	if err := errors.Join(
		registry.Register(collectors.NewGoCollector()),
		registry.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})),
	); err != nil {
		return nil, fmt.Errorf("registering process collectors: %w", err)
	}

	provider, err := telemetry.NewPrometheusProvider(
		serviceName,
		registry,
		attribute.String("service.instance.id", chainID+"/"+svrCtx.Config.Moniker),
		attribute.String("ark.chain.id", chainID),
	)
	if err != nil {
		return nil, err
	}

	return &prometheusEndpoint{
		address:     cfg.Address,
		registry:    registry,
		provider:    provider,
		hostBaseapp: !otelCfg.baseappInstrument,
	}, nil
}

// install sets the global provider before SDK telemetry binds package meters, then restores it
// during app creation before legacy bridge setup. OTel binds existing meters to the first provider;
// see cmd/arkd/README.md.
func (e *prometheusEndpoint) install() {
	if e == nil {
		return
	}
	otel.SetMeterProvider(e.provider)
}

// exposeWasmVMCache hands the endpoint's registry to the contract runtime
// through appOpts, so wasmvm's cache counters land on the scrape endpoint the
// way Gaia lands them on the default registry. Start passes its viper here;
// the commands that build an app without an endpoint have nothing to set.
func (e *prometheusEndpoint) exposeWasmVMCache(appOpts servertypes.AppOptions) {
	if e == nil {
		return
	}
	if v, ok := appOpts.(*viper.Viper); ok {
		v.Set(app.WasmVMCacheMetricsRegistererOpt, e.registry)
	}
}

// startBaseappInstrument enables SDK block/transaction timings after provider installation unless
// otel.yaml already delegates startup to the SDK. The acquired meter remains bound to that
// provider.
func (e *prometheusEndpoint) startBaseappInstrument() error {
	if e == nil || !e.hostBaseapp {
		return nil
	}
	inst := registry.Get(baseapp.InstrumentName)
	if inst == nil {
		return fmt.Errorf("SDK instrument %q is not registered", baseapp.InstrumentName)
	}
	return inst.Start(nil)
}

// installGoMetricsSink replaces the selected otel bridge in PostSetup, after SDK telemetry
// initialisation. It sanitises names without starting a second runtime collector or legacy
// Prometheus fan-out; see cmd/arkd/README.md.
func installGoMetricsSink(tel sdktelemetry.Config, queryRouter *baseapp.GRPCQueryRouter) error { //nolint:staticcheck // the legacy bridge is what this replaces
	if !tel.Enabled || tel.MetricsSink != otelSink {
		return nil
	}

	conf := gometrics.DefaultConfig(tel.ServiceName)
	conf.EnableHostname = tel.EnableHostname
	conf.EnableHostnameLabel = tel.EnableHostnameLabel
	conf.EnableRuntimeMetrics = false
	sink := telemetry.NewGoMetricsSink(context.Background(), otel.Meter("gometrics"), telemetry.GoMetricsConfig{
		ServiceName:  tel.ServiceName,
		IsQueryRoute: func(path string) bool { return queryRouter != nil && queryRouter.Route(path) != nil },
	})
	if _, err := gometrics.NewGlobal(conf, sink); err != nil {
		return fmt.Errorf("installing the go-metrics sink: %w", err)
	}
	return nil
}

// serve runs the scrape endpoint under the server's errgroup; a listener
// that cannot bind stops the node, as the SDK's own listeners do.
func (e *prometheusEndpoint) serve(ctx context.Context, g *errgroup.Group, logger log.Logger) {
	if e == nil {
		return
	}
	telemetry.ServeScrape(ctx, g, e.address, e.registry, e.provider, logger)
}

// startChainID resolves the chain ID before the app exists: the flag when
// given, otherwise the genesis file, the same source the node itself uses.
func startChainID(svrCtx *server.Context) (string, error) {
	if chainID := cast.ToString(svrCtx.Viper.Get(flags.FlagChainID)); chainID != "" {
		return chainID, nil
	}

	genesis, err := genutiltypes.AppGenesisFromFile(svrCtx.Config.GenesisFile())
	if err != nil {
		return "", fmt.Errorf("resolving chain ID from genesis: %w", err)
	}
	return genesis.ChainID, nil
}
