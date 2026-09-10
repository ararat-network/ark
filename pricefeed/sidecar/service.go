package sidecar

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/api"
	oracleconfig "github.com/ararat-network/ark/pricefeed/config"
	sidecarinternal "github.com/ararat-network/ark/pricefeed/sidecar/internal"
	"github.com/ararat-network/ark/pricefeed/sidecar/runtime"
)

var _ api.PriceFeedServer = (*Service)(nil)

// Service owns sidecar lifecycle and implements the RPC interface. Runtime owns fetch/cache state;
// the private server owns transports.
type Service struct {
	api.UnimplementedPriceFeedServer

	// runtime owns providers, aggregation, updates, and cached price snapshots.
	runtime *runtime.Runtime

	// server owns the prepared gRPC/gateway transport stack.
	server *server
	// adminServer owns the optional process-local administration transport.
	adminServer *adminServer

	// runtimeConfigPath is immutable process state used by live reloads.
	runtimeConfigPath string
	// reloadMu serialises config file reads and runtime replacements.
	reloadMu sync.Mutex

	// logger is scoped once at construction and shared by transport/RPC paths.
	logger log.Logger
}

// NewService constructs a sidecar process around a validated runtime.
func NewService(cfg Config, logger log.Logger, opts ...Option) (*Service, error) {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	var svcOpts serviceOptions
	for _, opt := range opts {
		opt(&svcOpts)
	}
	runtimeOpts := []runtime.Option{runtime.WithLogger(logger)}
	if svcOpts.registry != nil {
		runtimeOpts = append(runtimeOpts, runtime.WithProviderRegistry(svcOpts.registry))
	}
	r, err := runtime.NewRuntime(cfg.Runtime, runtimeOpts...)
	if err != nil {
		return nil, err
	}

	process := cfg.Process
	if process.ServerAddress == "" {
		process.ServerAddress = defaultServerAddress
	}

	o := &Service{
		runtime:           r,
		runtimeConfigPath: process.RuntimeConfigPath,
		logger:            logger.With("component", "oracle"),
	}
	server, err := newServer(o, logger, process.ServerAddress, process.TLS)
	if err != nil {
		return nil, err
	}
	o.server = server
	if process.AdminAddress != "" {
		if strings.TrimSpace(process.RuntimeConfigPath) == "" {
			return nil, errors.New("oracle runtime config path is required when admin service is enabled")
		}

		adminServer, err := newAdminServer(o, logger, process.AdminAddress)
		if err != nil {
			return nil, err
		}
		o.adminServer = adminServer
	}

	return o, nil
}

// Run is blocking and single-use. Parent cancellation or child failure stops runtime and
// transports; it returns only after both finish cleanup.
func (o *Service) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}
	var adminRun func(context.Context) error
	if o.adminServer != nil {
		adminRun = o.adminServer.run
	}
	return runService(ctx, o.runtime.Run, o.server.run, adminRun)
}

// runService is the process failure boundary: any child failure cancels the
// others, and it returns only after every child has completed cleanup.
func runService(
	ctx context.Context,
	runtimeRun func(context.Context) error,
	transportRun func(context.Context) error,
	adminRun func(context.Context) error,
) error {
	eg, groupCtx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		return sidecarinternal.RunRecovering("oracle runtime", func() error {
			err := runtimeRun(groupCtx)
			if groupCtx.Err() == context.Canceled &&
				errors.Is(err, context.Cause(groupCtx)) {
				return nil
			}
			return err
		})
	})

	if adminRun != nil {
		eg.Go(func() error {
			return sidecarinternal.RunRecovering("oracle admin transport", func() error {
				return adminRun(groupCtx)
			})
		})
	}

	eg.Go(func() error {
		return sidecarinternal.RunRecovering("oracle transport", func() error {
			return transportRun(groupCtx)
		})
	})

	return eg.Wait()
}

// ReloadConfig reloads and applies the runtime config file fixed at construction.
func (o *Service) ReloadConfig(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}
	if strings.TrimSpace(o.runtimeConfigPath) == "" {
		return errors.New("oracle runtime config path is not configured")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	o.reloadMu.Lock()
	defer o.reloadMu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	cfg, err := oracleconfig.Load(o.runtimeConfigPath)
	if err != nil {
		return fmt.Errorf("loading oracle runtime config: %w", err)
	}
	if err := o.runtime.Update(cfg); err != nil {
		return fmt.Errorf("updating oracle runtime config: %w", err)
	}

	return nil
}
