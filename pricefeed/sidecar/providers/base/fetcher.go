package base

import (
	"context"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

// Fetcher retrieves provider-specific ticker prices and publishes ticker-keyed responses.
type Fetcher interface {
	// Run fetches prices for tickers until ctx is cancelled or the fetcher stops.
	Run(ctx context.Context, tickers []types.Ticker, responseCh chan<- types.Response) error

	// Name returns the fetcher's provider identity.
	Name() string

	// ResponseBufferSize returns the response channel capacity this fetcher needs for tickers.
	ResponseBufferSize(tickers []types.Ticker) int

	// Type returns the fetcher's transport type.
	Type() TransportType
}
