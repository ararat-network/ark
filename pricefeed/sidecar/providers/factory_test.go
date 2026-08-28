package providers_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers"
	frankfurterapi "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/frankfurter"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

func TestNewProviderAllowsEmptyActiveMarkets(t *testing.T) {
	cfg := providers.Config{
		Name:          frankfurterapi.Name,
		TransportType: base.API,
		Markets:       types.Markets{{Pair: "NOAH/USD", Symbol: "NOAHUSD"}},
		MaxPriceAge:   time.Minute,
		API: api.Config{
			Name:      frankfurterapi.Name,
			Timeout:   time.Second,
			Interval:  time.Second,
			Endpoints: []types.Endpoint{{URL: "https://example.invalid/prices"}},
		},
	}

	provider, err := providers.NewProvider(cfg, nil, log.NewNopLogger())

	require.NoError(t, err)
	require.Empty(t, provider.GetTickers())
}
