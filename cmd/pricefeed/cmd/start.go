package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pkg/grpcconn"
	"github.com/ararat-network/ark/pkg/telemetry"
	"github.com/ararat-network/ark/pkg/tlsconfig"
	"github.com/ararat-network/ark/pricefeed/config"
	"github.com/ararat-network/ark/pricefeed/sidecar"
)

const (
	// serviceName labels this process's logs and metrics alike, so the two
	// join on one name.
	serviceName = "pricefeed"

	// telemetryShutdownTimeout bounds the meter provider's final flush after
	// the process endpoints have stopped.
	telemetryShutdownTimeout = 5 * time.Second

	defaultAddress        = "127.0.0.1:8080"
	defaultAdminAddress   = "127.0.0.1:8081"
	defaultLogLevel       = "info"
	defaultMetrics        = false
	defaultMetricsAddress = "127.0.0.1:9091"
	defaultPprof          = false
	defaultPprofAddress   = "127.0.0.1:6060"
	defaultLogJSON        = false

	flagAddress        = "address"
	flagAdminAddress   = "admin-address"
	flagMetrics        = "metrics"
	flagMetricsAddress = "metrics-address"
	flagPprof          = "pprof"
	flagPprofAddress   = "pprof-address"
	flagLogLevel       = "log-level"
	flagLogJSON        = "log-json"
)

type startOptions struct {
	address        string
	tls            tlsconfig.Server
	adminAddress   string
	metrics        bool
	metricsAddress string
	pprof          bool
	pprofAddress   string
	logLevel       string
	logJSON        bool
}

func newStartCmd(configPath *string) *cobra.Command {
	options := startOptions{
		address:        defaultAddress,
		adminAddress:   defaultAdminAddress,
		metrics:        defaultMetrics,
		metricsAddress: defaultMetricsAddress,
		pprof:          defaultPprof,
		pprofAddress:   defaultPprofAddress,
		logLevel:       defaultLogLevel,
		logJSON:        defaultLogJSON,
	}

	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Run the price-feed sidecar.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runService(cmd.Context(), *configPath, options)
		},
	}

	flags := startCmd.Flags()
	flags.StringVar(&options.address, flagAddress, options.address, "Public gRPC and HTTP gateway listen address.")
	addServerTLSFlags(flags, &options.tls)
	flags.StringVar(
		&options.adminAddress,
		flagAdminAddress,
		options.adminAddress,
		"Loopback admin gRPC listen address; empty disables the admin service.",
	)
	flags.BoolVar(&options.metrics, flagMetrics, options.metrics, "Enable Prometheus metrics.")
	flags.StringVar(&options.metricsAddress, flagMetricsAddress, options.metricsAddress, "Prometheus metrics listen address.")
	flags.BoolVar(&options.pprof, flagPprof, options.pprof, "Enable pprof.")
	flags.StringVar(&options.pprofAddress, flagPprofAddress, options.pprofAddress, "Pprof listen address; loopback only.")
	flags.StringVar(&options.logLevel, flagLogLevel, options.logLevel, "Log level (debug, info, warn, error, disabled).")
	flags.BoolVar(&options.logJSON, flagLogJSON, options.logJSON, "Emit JSON logs.")

	return startCmd
}

// runService runs the sidecar and its optional process-owned HTTP endpoints in
// the foreground. It blocks until ctx ends or a component fails, then waits
// for every component to finish cleanup before returning.
func runService(ctx context.Context, configPath string, options startOptions) (err error) {
	logger, err := newLogger(options.logLevel, options.logJSON)
	if err != nil {
		return err
	}
	// The process endpoints are checked before the config is read or anything
	// listens. pprof exposes process internals, so it stays on loopback; the
	// admin listener applies the same rule where it is built.
	if options.pprof {
		if _, _, err := grpcconn.LoopbackListenAddress(options.pprofAddress); err != nil {
			return fmt.Errorf("pprof address: %w", err)
		}
	}
	if options.metrics {
		if _, _, err := grpcconn.ListenAddress(options.metricsAddress); err != nil {
			return fmt.Errorf("metrics address: %w", err)
		}
	}

	runtimeCfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	// Retain the source path so the admin service can reload the same config
	// file without accepting replacement runtime config over RPC.
	processCfg := sidecar.ProcessConfig{
		ServerAddress:     options.address,
		TLS:               options.tls,
		AdminAddress:      options.adminAddress,
		RuntimeConfigPath: configPath,
	}
	svc, err := sidecar.NewService(
		sidecar.Config{
			Runtime: runtimeCfg,
			Process: processCfg,
		},
		logger,
	)
	if err != nil {
		return fmt.Errorf("creating sidecar: %w", err)
	}

	group, groupCtx := errgroup.WithContext(ctx)
	if options.metrics {
		prometheus, err := initPrometheus()
		if err != nil {
			return fmt.Errorf("initialising prometheus telemetry: %w", err)
		}
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), telemetryShutdownTimeout)
			defer cancel()

			err = errors.Join(err, prometheus.Shutdown(shutdownCtx))
		}()

		group.Go(func() error {
			return telemetry.RunHTTPServer(
				groupCtx,
				options.metricsAddress,
				prometheus.Handler(),
				logger,
				"prometheus metrics",
			)
		})
	}

	if options.pprof {
		group.Go(func() error {
			return telemetry.RunHTTPServer(
				groupCtx,
				options.pprofAddress,
				newPprofHandler(),
				logger,
				"pprof",
			)
		})
	}

	group.Go(func() error {
		return svc.Run(groupCtx)
	})

	logger.Info("starting sidecar", "address", options.address, "tls", options.tls.Enabled())
	return group.Wait()
}

func newLogger(level string, jsonOutput bool) (log.Logger, error) {
	filter, err := log.ParseLogLevel(level)
	if err != nil {
		return nil, err
	}

	opts := []log.Option{
		log.FilterOption(filter),
		log.ColorOption(false),
	}
	if jsonOutput {
		opts = append(opts, log.OutputJSONOption())
	}

	return log.NewLogger(os.Stderr, opts...).With("service", serviceName), nil
}
