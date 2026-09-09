package client

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ararat-network/ark/pricefeed/api"
)

// A snapshot dated ahead of this node's clock is served only within the skew
// bound: past it, a fast sidecar clock would otherwise keep a stale snapshot
// alive for as long as it ran fast.
func TestCachedPriceClientBoundsSnapshotsAheadOfTheClock(t *testing.T) {
	tests := []struct {
		name    string
		ahead   time.Duration
		wantErr string
	}{
		{name: "within skew", ahead: maxFutureSkew - time.Second},
		{name: "past skew", ahead: maxFutureSkew + time.Second, wantErr: "ahead of the clock"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestCachedPriceClient(t, validClientConfig())
			resp := freshResponse()
			resp.Timestamp = time.Now().Add(tt.ahead)
			client.resp = resp

			_, err := client.Prices(context.Background(), &api.PricesRequest{})
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
