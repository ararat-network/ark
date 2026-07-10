package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"cosmossdk.io/log/v2"

	oracleconfig "ark/oracle/config"
	"ark/oracle/sidecar"
)

const (
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
		Short: "Run the price oracle.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runOracle(cmd.Context(), *configPath, options)
		},
	}

	flags := startCmd.Flags()
	flags.StringVar(&options.address, flagAddress, options.address, "Oracle gRPC listen address.")
	flags.StringVar(&options.adminAddress, flagAdminAddress, options.adminAddress, "Loopback admin gRPC listen address.")
	flags.BoolVar(&options.metrics, flagMetrics, options.metrics, "Enable Prometheus metrics.")
	flags.StringVar(&options.metricsAddress, flagMetricsAddress, options.metricsAddress, "Prometheus metrics listen address.")
	flags.BoolVar(&options.pprof, flagPprof, options.pprof, "Enable pprof.")
	flags.StringVar(&options.pprofAddress, flagPprofAddress, options.pprofAddress, "Pprof listen address.")
	flags.StringVar(&options.logLevel, flagLogLevel, options.logLevel, "Log level (debug, info, warn, error, disabled).")
	flags.BoolVar(&options.logJSON, flagLogJSON, options.logJSON, "Emit JSON logs.")

	return startCmd
}

// runOracle runs the sidecar and its optional process-owned HTTP endpoints in
// the foreground. It blocks until cancellation or a component failure, then
// waits for every component to finish cleanup before returning.
func runOracle(parentCtx context.Context, configPath string, options startOptions) (err error) {
	if parentCtx == nil {
		parentCtx = context.Background()
	}

	logger, err := newLogger(options.logLevel, options.logJSON)
	if err != nil {
		return err
	}

	runtimeCfg, err := oracleconfig.Load(configPath)
	if err != nil {
		return err
	}

	signalCtx, stopSignals := signal.NotifyContext(parentCtx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	// Retain the source path so the admin service can reload the same config
	// file without accepting replacement runtime config over RPC.
	processCfg := sidecar.ProcessConfig{
		ServerAddress:     options.address,
		AdminAddress:      options.adminAddress,
		RuntimeConfigPath: configPath,
	}
	oracle, err := sidecar.NewOracle(
		sidecar.Config{
			Runtime: runtimeCfg,
			Process: processCfg,
		},
		logger,
	)
	if err != nil {
		return fmt.Errorf("creating oracle sidecar: %w", err)
	}

	group, groupCtx := errgroup.WithContext(signalCtx)
	if options.metrics {
		prometheus, err := initPrometheus("oracle")
		if err != nil {
			return fmt.Errorf("initialising oracle prometheus telemetry: %w", err)
		}
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(
				context.Background(),
				sidecar.DefaultServerReadHeaderTimeout,
			)
			defer cancel()

			err = errors.Join(err, prometheus.Shutdown(shutdownCtx))
		}()

		group.Go(func() error {
			return runHTTPServer(
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
			return runHTTPServer(
				groupCtx,
				options.pprofAddress,
				newPprofHandler(),
				logger,
				"pprof",
			)
		})
	}

	group.Go(func() error {
		return oracle.Run(groupCtx)
	})

	logger.Info("starting oracle sidecar", "address", options.address)
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

	return log.NewLogger(os.Stderr, opts...).With("service", "oracle_sidecar"), nil
}
