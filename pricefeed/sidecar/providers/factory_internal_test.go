package providers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log/v2"

	frankfurterapi "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/frankfurter"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

func TestBuildAPIFetcherUsesFrankfurterHandlerBatching(t *testing.T) {
	fetcher, err := buildAPIFetcher(Config{
		Name:          frankfurterapi.Name,
		TransportType: base.API,
		API:           frankfurterapi.DefaultAPIConfig,
	}, log.NewNopLogger())
	require.NoError(t, err)

	require.Equal(t, 2, fetcher.ResponseBufferSize([]types.Ticker{
		"USD/KRW",
		"EUR/GBP",
		"USD/JPY",
		"EUR/CHF",
	}))
}
