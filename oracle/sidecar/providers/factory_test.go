package providers_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"noah/oracle/sidecar/providers"
	frankfurterapi "noah/oracle/sidecar/providers/api/frankfurter"
	"noah/oracle/sidecar/providers/base"
	"noah/oracle/sidecar/providers/base/api"
	"noah/oracle/sidecar/providers/types"
)

func TestNewProviderAllowsEmptyActiveMarkets(t *testing.T) {
	cfg := providers.Config{
		Name:          frankfurterapi.Name,
		TransportType: base.API,
		Markets:       types.Markets{{Pair: "ARK/USD", Symbol: "ARKUSD"}},
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
