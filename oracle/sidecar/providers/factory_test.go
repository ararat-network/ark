package providers_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	"ark/oracle/sidecar/providers"
	frankfurterapi "ark/oracle/sidecar/providers/api/frankfurter"
	"ark/oracle/sidecar/providers/base"
	"ark/oracle/sidecar/providers/base/api"
	"ark/oracle/sidecar/providers/types"
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
