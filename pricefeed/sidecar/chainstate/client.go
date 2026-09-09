package chainstate

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"google.golang.org/grpc"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pkg/grpcconn"
	"github.com/ararat-network/ark/pkg/tlsconfig"
)

// Client polls the chain's oracle query service for the active feed set and
// serves the latest successful snapshot to the sidecar runtime.
type Client struct {
	logger log.Logger
	// dialOptions follow the transport credentials on every connection; tests
	// inject their dialer here.
	dialOptions []grpc.DialOption

	// active indexes the endpoint that served last. Only the poll goroutine
	// touches it, which is why Run must not run concurrently.
	active int
	// activeAddresses is the list active indexes, owned by the poll goroutine.
	activeAddresses []string

	mut sync.RWMutex

	// cfg and material change together through Update. Material is what cfg's TLS
	// files loaded to, read at construction and at Update so a bad file
	// fails the start or the reload rather than every poll after it.
	cfg      Config
	material *tlsconfig.Material

	// Cached chain state. feeds is the last successful snapshot, including an
	// authoritative empty snapshot. hasSnapshot distinguishes that state from a
	// client that has not completed any successful query yet.
	feeds       []string
	hasSnapshot bool
}

// NewClient validates cfg, applies opts, and returns a chainstate client ready
// to be run by the sidecar owner.
func NewClient(cfg Config, opts ...Option) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg.Addresses = slices.Clone(cfg.Addresses)

	c := &Client{
		logger: log.NewNopLogger(),
		cfg:    cfg,
	}
	for _, opt := range opts {
		opt(c)
	}

	if c.logger == nil {
		return nil, errors.New("logger is nil")
	}
	c.logger = c.logger.With("chain_state_client", "feeds")

	material, err := c.loadMaterial(cfg)
	if err != nil {
		return nil, err
	}
	c.material = material

	return c, nil
}

// Run blocks on the poll loop and cleans up lifecycle state before returning.
// Callers should run it in the owning lifecycle and use Feeds to read the
// cached snapshot. Run must not be called concurrently on the same client.
func (c *Client) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}

	c.logger.Info("starting chain state client")
	// A previous run may have used a different address list.
	c.active = 0

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if err := c.runOnce(ctx); err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				return ctx.Err()
			}

			c.logger.Error("chain state client poll failed", "error", err)

			cfg := c.getConfig()
			if err := waitForRetry(ctx, cfg.Interval); err != nil {
				return err
			}
		}
	}
}

// Update validates and replaces the client config. Address or TLS changes load
// replacement material before committing. Timing-only changes retain the active
// material and trust roots. Run observes address list, TLS, timeout, and interval
// changes on the next poll or retry cycle.
func (c *Client) Update(cfg Config) error {
	cfg.Addresses = slices.Clone(cfg.Addresses)
	c.mut.RLock()
	previous, material := c.cfg, c.material
	same := previous.Equal(cfg)
	c.mut.RUnlock()
	if same {
		return nil
	}

	if err := cfg.Validate(); err != nil {
		return err
	}
	if previous.TLS != cfg.TLS || !slices.Equal(previous.Addresses, cfg.Addresses) {
		var err error
		material, err = c.loadMaterial(cfg)
		if err != nil {
			return err
		}
	}

	c.mut.Lock()
	c.cfg = cfg
	c.material = material
	c.mut.Unlock()

	c.logger.Info("updated chain state feed client config")
	return nil
}

// loadMaterial loads cfg's TLS files and warns when the connections
// would reach a chain node in plaintext off this host.
func (c *Client) loadMaterial(cfg Config) (*tlsconfig.Material, error) {
	material, err := grpcconn.LoadClient(cfg.TLS, cfg.Addresses...)
	if err != nil {
		return nil, fmt.Errorf("oracle query connection: %w", err)
	}
	if remote := grpcconn.RemotePlaintext(cfg.TLS, cfg.Addresses...); len(remote) > 0 {
		c.logger.Warn(
			"dialling chain nodes in plaintext off loopback; explicit plaintext mode selected",
			"addresses", remote,
		)
	}
	return material, nil
}

// Feeds returns a copy of the latest successful feed snapshot. It returns an
// error until the first successful snapshot is cached; later refresh failures
// preserve the last successful snapshot, including an empty one.
func (c *Client) Feeds() ([]string, error) {
	c.mut.RLock()
	defer c.mut.RUnlock()

	if !c.hasSnapshot {
		return nil, errors.New("no feeds fetched yet")
	}

	return append([]string{}, c.feeds...), nil
}

// snapshot returns the current config and the material its TLS files
// loaded to, together under the client lock.
func (c *Client) snapshot() (Config, *tlsconfig.Material) {
	c.mut.RLock()
	defer c.mut.RUnlock()

	return c.cfg, c.material
}

// getConfig returns a copy of the current polling config under the client lock.
func (c *Client) getConfig() Config {
	c.mut.RLock()
	defer c.mut.RUnlock()

	return c.cfg
}
