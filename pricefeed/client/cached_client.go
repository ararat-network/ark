package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pkg/encoding"
	"github.com/ararat-network/ark/pricefeed/api"
	clientmetrics "github.com/ararat-network/ark/pricefeed/client/metrics"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

const maxPriceSnapshotEntries = 2 * oracletypes.MaxFeeds

// pricesMethod is the RPC a sidecar has to serve to be one at all.
const pricesMethod = "/ark.pricefeed.v1.PriceFeed/Prices"

// Client polls the sidecar and serves its latest fresh price
// snapshot to node-side callers without performing network I/O on the request
// path.
type Client struct {
	logger log.Logger
	config Config

	// versions is the build version each sidecar last reported, by address.
	// Only the poll goroutine touches it.
	versions map[string]string

	respMu sync.RWMutex
	resp   *api.PricesResponse
}

// NewClient creates a cached node-side price client. The gRPC
// connection is created and owned by Run for each polling run.
func NewClient(logger log.Logger, cfg Config) (*Client, error) {
	if logger == nil {
		return nil, errors.New("logger cannot be nil")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &Client{
		logger:   logger.With(log.ModuleKey, "pricefeed-client"),
		config:   cfg,
		versions: make(map[string]string, 1),
	}, nil
}

// Run connects to the sidecar and polls prices until ctx is cancelled. A
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
	clientmetrics.SetEndpoints(ctx, []string{c.config.SidecarAddress})
	conn, err := grpc.NewClient(
		c.config.SidecarAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("dial oracle gRPC server: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close oracle gRPC connection: %w", closeErr))
		}
	}()

	return c.poll(ctx, api.NewPriceFeedClient(conn))
}

func (c *Client) poll(ctx context.Context, rpc api.PriceFeedClient) error {
	c.logger.Info("starting cached price client")
	c.fetchPrices(ctx, rpc)

	ticker := time.NewTicker(c.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("stopping cached price client from context")
			return ctx.Err()
		case <-ticker.C:
			c.fetchPrices(ctx, rpc)
		}
	}
}

func (c *Client) fetchPrices(ctx context.Context, rpc api.PriceFeedClient) {
	c.logger.Debug("fetching prices")

	fetchCtx, cancel := context.WithTimeout(ctx, c.config.ClientTimeout)
	defer cancel()

	start := time.Now()
	resp, err := rpc.Prices(fetchCtx, &api.PricesRequest{}, grpc.WaitForReady(true))
	if err == nil && resp == nil {
		err = errors.New("sidecar returned a nil price response")
	}
	if err == nil {
		err = validatePricesResponse(resp)
	}
	clientmetrics.RecordSidecarResponse(c.config.SidecarAddress, time.Since(start), err)
	if status.Code(err) == codes.Unimplemented {
		// The one status a sidecar of the wrong generation returns: it
		// answers, but not this service. See pricefeed/doc.go.
		err = fmt.Errorf("sidecar does not serve %s and is not compatible with this node: %w", pricesMethod, err)
	}
	if err != nil {
		if ctx.Err() == nil {
			c.logger.Error(
				"failed to fetch prices from sidecar",
				"err", err,
				"address", c.config.SidecarAddress,
			)
		}
		return
	}

	c.observeVersion(c.config.SidecarAddress, resp.Version)
	clientmetrics.RecordSnapshotTimestamp(c.config.SidecarAddress, resp.Timestamp)

	c.logger.Debug(
		"fetched prices",
		"timestamp", resp.Timestamp,
		"prices", resp.Prices,
	)

	c.respMu.Lock()
	c.resp = resp
	c.respMu.Unlock()
}

// observeVersion logs a sidecar's build version when it first answers and
// whenever it changes. The version is never a gate; see pricefeed/doc.go.
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

	age := time.Since(latest.Timestamp)
	if latest.Timestamp.IsZero() || age > c.config.PriceTTL {
		return nil, fmt.Errorf(
			"latest sidecar price snapshot is too stale; timestamp %s; age %s",
			latest.Timestamp.Format(time.RFC3339),
			age,
		)
	}

	return clonePricesResponse(latest), nil
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
