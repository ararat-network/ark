package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	cmtcfg "github.com/cometbft/cometbft/config"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdktelemetry "github.com/cosmos/cosmos-sdk/telemetry"

	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/app/mempool"
)

// startRun shares validated config, app, and endpoint across startup hooks. PostSetup runs services
// in the server errgroup so cancellation precedes app cleanup. Rollback may construct the app
// without preRun or an endpoint; see cmd/arkd/README.md.
type startRun struct {
	cfg      arkAppConfig
	endpoint *prometheusEndpoint
	app      *app.ArkApp
	svrCtx   *server.Context
	// testnet is set by in-place-testnet for its app creator.
	testnet *inPlaceTestnetArgs
}

// adjustStartCommand runs preRun after the SDK start command's own PreRunE.
func adjustStartCommand(rootCmd *cobra.Command, run *startRun) {
	startCmd, _, err := rootCmd.Find([]string{"start"})
	if err != nil {
		panic(err)
	}
	run.wrapPreRun(startCmd)
}

// wrapPreRun runs preRun after cmd's own PreRunE.
func (r *startRun) wrapPreRun(cmd *cobra.Command) {
	prev := cmd.PreRunE
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if prev != nil {
			if err := prev(cmd, args); err != nil {
				return err
			}
		}
		return r.preRun(cmd)
	}
}

func (r *startRun) preRun(cmd *cobra.Command) error {
	var err error
	r.svrCtx = server.GetServerContextFromCmd(cmd)
	r.cfg, r.endpoint, err = prepareStart(r.svrCtx)
	if err != nil {
		return err
	}
	r.endpoint.install()
	return r.endpoint.startBaseappInstrument()
}

func (r *startRun) createApp(logger log.Logger, db dbm.DB, appOpts servertypes.AppOptions) servertypes.Application {
	r.endpoint.install()
	r.endpoint.exposeWasmVMCache(appOpts)
	r.app = newApp(logger, db, appOpts).(*app.ArkApp)
	return r.app
}

func (r *startRun) postSetup(svrCtx *server.Context, _ client.Context, ctx context.Context, g *errgroup.Group) error { //nolint:revive // the SDK's PostSetup signature
	if err := installGoMetricsSink(r.cfg.Telemetry, r.app.GRPCQueryRouter()); err != nil { //nolint:staticcheck // the legacy bridge is what this replaces
		return err
	}
	r.endpoint.serve(ctx, g, svrCtx.Logger)
	g.Go(func() error {
		// A dead price client must not stop the node: the protocol
		// tolerates a validator abstaining from oracle votes.
		if err := r.app.RunPriceFeed(ctx); err != nil {
			svrCtx.Logger.Error("price-feed client stopped unexpectedly", "err", err)
		}
		return nil
	})
	return nil
}

// prepareStart is the start command's PreRunE body: the one decode and
// validation of app.toml and the read of otel.yaml, before the SDK re-reads
// both and before the app exists.
func prepareStart(svrCtx *server.Context) (arkAppConfig, *prometheusEndpoint, error) {
	// The app pool decides admission; CometBFT's flood list mirrors it for
	// gossip and rechecks and must never be the binding gate.
	if svrCtx.Config.Mempool.Type != cmtcfg.MempoolTypeFlood {
		return arkAppConfig{}, nil, errors.New("ark requires config.toml [mempool] type = \"flood\"; run: arkd config set config mempool.type flood")
	}
	if !svrCtx.Config.Mempool.Recheck {
		return arkAppConfig{}, nil, errors.New("ark requires config.toml [mempool] recheck = true: CometBFT learns of pool evictions only through recheck")
	}
	bindMempoolByteLimits(svrCtx)
	cfg, err := readAppConfig(svrCtx.Viper)
	if err != nil {
		return arkAppConfig{}, nil, err
	}
	if size, count := svrCtx.Config.Mempool.Size, cfg.Mempool.Count(); size < count {
		return arkAppConfig{}, nil, fmt.Errorf("config.toml [mempool] size = %d is below app.toml [mempool] max-txs = %d; CometBFT's list must never refuse what the pool reserves", size, count)
	}
	var otelCfg otelFile
	if os.Getenv(otelConfigFileEnv) != "" {
		// The variable makes the SDK build OpenTelemetry from that file at
		// package load and ignore config/otel.yaml, so there is nothing to
		// read here. The node's package-level meters are already bound to
		// the file's provider for good: the endpoint would serve without them.
		if cfg.Prometheus.Enabled {
			return arkAppConfig{}, nil, fmt.Errorf("%s is set while [prometheus] is enabled; the SDK binds the node's meters to that file's provider before the scrape endpoint exists. Unset it or disable [prometheus]", otelConfigFileEnv)
		}
	} else {
		otelCfg, err = readOtelFile(filepath.Join(svrCtx.Config.RootDir, "config", sdktelemetry.OtelFileName))
		if err != nil {
			return arkAppConfig{}, nil, err
		}
		if otelCfg.pullReader {
			return arkAppConfig{}, nil, errors.New("otel.yaml configures a pull metric reader, which this build's OpenTelemetry cannot serve; scrape the node through [prometheus] in app.toml")
		}
	}
	for _, section := range absentSections(svrCtx.Viper) {
		svrCtx.Logger.Info("app.toml has no section; its defaults apply", "section", section)
	}
	if !cfg.PriceFeed.Enabled {
		svrCtx.Logger.Warn("app.toml [pricefeed] is disabled; a validator on this node abstains from every oracle vote")
	}
	if cfg.unroutedGoMetrics() {
		svrCtx.Logger.Warn("telemetry.metrics-sink is otel but [prometheus] is disabled; go-metrics series reach only what otel.yaml exports")
	}
	if cfg.deadPrometheusRetention() {
		svrCtx.Logger.Warn("telemetry.prometheus-retention-time is set but the legacy fan-out it sizes is replaced by the go-metrics sink; the series reach [prometheus] instead")
	}
	if otelCfg.meterProvider && cfg.Prometheus.Enabled {
		svrCtx.Logger.Warn("otel.yaml meter_provider is shadowed by [prometheus]; the node's meters bind to the scrape endpoint and its readers export nothing")
	}
	endpoint, err := newPrometheusEndpoint(svrCtx, cfg.Prometheus, otelCfg)
	return cfg, endpoint, err
}

// bindMempoolByteLimits passes the exact CometBFT values to app construction.
// The SDK merges app.toml into Viper after decoding config.toml; copying the
// resolved values prevents a stray app.toml key from splitting the two limits.
func bindMempoolByteLimits(svrCtx *server.Context) {
	svrCtx.Viper.Set(mempool.MaxTransactionBytesKey, svrCtx.Config.Mempool.MaxTxBytes)
	svrCtx.Viper.Set(mempool.MaxPoolBytesKey, svrCtx.Config.Mempool.MaxTxsBytes)
}
