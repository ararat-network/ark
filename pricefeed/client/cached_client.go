package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pkg/encoding"
	"github.com/ararat-network/ark/pkg/grpcconn"
	"github.com/ararat-network/ark/pkg/tlsconfig"
	"github.com/ararat-network/ark/pricefeed/api"
	clientmetrics "github.com/ararat-network/ark/pricefeed/client/metrics"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

const maxPriceSnapshotEntries = 2 * oracletypes.MaxFeeds

// pricesMethod is the RPC a sidecar has to serve to be one at all.
const pricesMethod = "/ark.pricefeed.v1.PriceFeed/Prices"

// maxFutureSkew bounds how far ahead of this node's clock a snapshot may be
// dated. A sidecar clock running fast would otherwise carry a stale snapshot
// past the TTL for as long as it ran fast. Five seconds, as the validate
// command allows.
const maxFutureSkew = 5 * time.Second

// endpoint is one dialled sidecar. Run builds one per configured address.
type endpoint struct {
	address string
	rpc     api.PriceFeedClient
}

// Client polls the sidecar and serves its latest fresh price
// snapshot to node-side callers without performing network I/O on the request
// path.
type Client struct {
	logger log.Logger
	config Config
	// dial is what every sidecar connection dials with: the TLS files, read
	// at construction so a bad file fails the start rather than the first
	// poll. Nil for a disabled client, which dials nothing.
	dial     []grpc.DialOption
	material *tlsconfig.Material

	// active indexes the endpoint that served last. Only the poll goroutine
	// touches it, which is why Run must not run concurrently.
	active int
	// versions is the build version each sidecar last reported, by address.
	// Poll goroutine only, like active.
	versions map[string]string

	respMu sync.RWMutex
	resp   *api.PricesResponse
}

// NewClient creates a cached node-side price client. The gRPC
// connections are created and owned by Run for each polling run.
func NewClient(logger log.Logger, cfg Config) (*Client, error) {
	if logger == nil {
		return nil, errors.New("logger cannot be nil")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg.SidecarAddresses = slices.Clone(cfg.SidecarAddresses)
	logger = logger.With(log.ModuleKey, "pricefeed-client")

	var dial []grpc.DialOption
	var material *tlsconfig.Material
	if cfg.Enabled {
		var err error
		material, err = grpcconn.LoadClient(cfg.TLS, cfg.SidecarAddresses...)
		if err != nil {
			return nil, fmt.Errorf("sidecar connection: %w", err)
		}
		dial = grpcconn.DialOptions(material.Config)
		if remote := grpcconn.RemotePlaintext(cfg.TLS, cfg.SidecarAddresses...); len(remote) > 0 {
			logger.Warn(
				"dialling sidecars in plaintext off loopback; explicit plaintext mode selected",
				"addresses", remote,
			)
		}
	}

	return &Client{
		logger:   logger,
		config:   cfg,
		dial:     dial,
		material: material,
		versions: make(map[string]string, len(cfg.SidecarAddresses)),
	}, nil
}

// Run connects to the sidecars and polls prices until ctx is cancelled. A
// disabled client parks until cancellation instead, dialling nothing, so both
// configurations return when ctx ends. The caller owns cancellation and must
// wait for Run to return before discarding c. Run must not be called
// concurrently on the same client.
func (c *Client) Run(ctx context.Context) (err error) {
	if ctx == nil {
		return errors.New("context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !c.config.Enabled {
		<-ctx.Done()
		return ctx.Err()
	}

	clientmetrics.SetEndpoints(ctx, c.config.SidecarAddresses)
	stopCertificates := c.material.Start(ctx, c.logger)
	defer stopCertificates()

	conns, err := grpcconn.Open(c.config.SidecarAddresses, c.dial...)
	if err != nil {
		return fmt.Errorf("dial sidecars: %w", err)
	}
	defer func() {
		err = errors.Join(err, grpcconn.Close(conns))
	}()
	endpoints := make([]endpoint, 0, len(conns))
	for i, conn := range conns {
		endpoints = append(endpoints, endpoint{
			address: c.config.SidecarAddresses[i],
			rpc:     api.NewPriceFeedClient(conn),
		})
	}

	return c.poll(ctx, endpoints)
}

func (c *Client) poll(ctx context.Context, endpoints []endpoint) error {
	c.logger.Info("starting cached price client")
	c.fetchPrices(ctx, endpoints)

	ticker := time.NewTicker(c.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("stopping cached price client from context")
			return ctx.Err()
		case <-ticker.C:
			c.fetchPrices(ctx, endpoints)
		}
	}
}

// fetchPrices tries the active endpoint, then the rest in order, and caches
// the first response that validates. The endpoint that answered becomes
// active and stays so until it fails: there is no fail-back, so a flapping
// preferred sidecar cannot bounce the client back and forth. Cancellation
// ends the sweep.
func (c *Client) fetchPrices(ctx context.Context, endpoints []endpoint) {
	c.logger.Debug("fetching prices")

	var errs []error
	for offset := range endpoints {
		if ctx.Err() != nil {
			return
		}
		i := (c.active + offset) % len(endpoints)
		resp, err := c.fetchFrom(ctx, endpoints[i])
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", endpoints[i].address, err))
			continue
		}

		if i != c.active {
			c.logger.Warn(
				"failing over to sidecar",
				"from", endpoints[c.active].address,
				"to", endpoints[i].address,
			)
			c.active = i
		}
		c.observeVersion(endpoints[i].address, resp.Version)
		clientmetrics.RecordSnapshotTimestamp(endpoints[i].address, resp.Timestamp)

		c.logger.Debug(
			"fetched prices",
			"address", endpoints[i].address,
			"timestamp", resp.Timestamp,
			"prices", resp.Prices,
		)

		c.respMu.Lock()
		c.resp = resp
		c.respMu.Unlock()
		return
	}

	if ctx.Err() == nil {
		c.logger.Error(
			"failed to fetch prices from every sidecar",
			"err", errors.Join(errs...),
		)
	}
}

// fetchFrom is one attempt against one sidecar under ClientTimeout. WaitForReady
// lets a restarting sidecar be waited on rather than failed at once.
func (c *Client) fetchFrom(ctx context.Context, ep endpoint) (*api.PricesResponse, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, c.config.ClientTimeout)
	defer cancel()

	start := time.Now()
	resp, err := ep.rpc.Prices(fetchCtx, &api.PricesRequest{}, grpc.WaitForReady(true))
	if err == nil && resp == nil {
		err = errors.New("sidecar returned a nil price response")
	}
	if err == nil {
		err = validatePricesResponse(resp)
	}
	if err == nil {
		err = c.validateTimestamp(resp.Timestamp)
	}
	clientmetrics.RecordSidecarResponse(ep.address, time.Since(start), err)
	if status.Code(err) == codes.Unimplemented {
		// The sidecar answers but does not implement the required service or RPC.
		// See docs/operations/PRICEFEED_OPERATIONS.md, "Node–sidecar compatibility".
		err = fmt.Errorf("sidecar does not serve %s and is not compatible with this node: %w", pricesMethod, err)
	}
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// observeVersion logs a sidecar's build version when it first answers and
// whenever it changes. The version is never a gate; see docs/operations/PRICEFEED_OPERATIONS.md,
// "Node–sidecar compatibility".
func (c *Client) observeVersion(address, version string) {
	previous, seen := c.versions[address]
	if seen && previous == version {
		return
	}
	c.versions[address] = version
	c.logger.Info("sidecar version", "address", address, "version", version, "previous", previous)
}

func validatePricesResponse(resp *api.PricesResponse) error {
	if len(resp.Prices) > maxPriceSnapshotEntries {
		return fmt.Errorf(
			"sidecar price count %d exceeds maximum %d",
			len(resp.Prices),
			maxPriceSnapshotEntries,
		)
	}
	for denom, rawPrice := range resp.Prices {
		if len(rawPrice) > encoding.MaxEncodedCompactLegacyDecBytes {
			return fmt.Errorf(
				"sidecar price %s length %d exceeds maximum %d",
				denom,
				len(rawPrice),
				encoding.MaxEncodedCompactLegacyDecBytes,
			)
		}
	}

	return nil
}

// Prices returns the latest cached price snapshot. The snapshot timestamp is
// supplied by the sidecar and must be fresh enough for node-side use.
func (c *Client) Prices(
	ctx context.Context,
	_ *api.PricesRequest,
	_ ...grpc.CallOption,
) (*api.PricesResponse, error) {
	if ctx == nil {
		return nil, errors.New("context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	c.respMu.RLock()
	latest := c.resp
	c.respMu.RUnlock()
	if latest == nil {
		return nil, errors.New("no prices fetched from the sidecar yet")
	}

	if err := c.validateTimestamp(latest.Timestamp); err != nil {
		return nil, err
	}

	return clonePricesResponse(latest), nil
}

// validateTimestamp checks freshness before accepting a sidecar response and
// again when serving it, since a cached snapshot can expire between polls.
func (c *Client) validateTimestamp(timestamp time.Time) error {
	age := time.Since(timestamp)
	if timestamp.IsZero() || age > c.config.PriceTTL {
		return fmt.Errorf(
			"latest sidecar price snapshot is too stale; timestamp %s; age %s",
			timestamp.Format(time.RFC3339),
			age,
		)
	}
	if age < -maxFutureSkew {
		return fmt.Errorf(
			"latest sidecar price snapshot is %s ahead of the clock; timestamp %s",
			-age,
			timestamp.Format(time.RFC3339),
		)
	}

	return nil
}

func clonePricesResponse(resp *api.PricesResponse) *api.PricesResponse {
	if resp == nil {
		return nil
	}

	var prices map[string][]byte
	if resp.Prices != nil {
		prices = make(map[string][]byte, len(resp.Prices))
		for denom, price := range resp.Prices {
			prices[denom] = bytes.Clone(price)
		}
	}

	return &api.PricesResponse{
		Prices:    prices,
		Timestamp: resp.Timestamp,
		Version:   resp.Version,
	}
}
