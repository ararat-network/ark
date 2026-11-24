package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/encoding"
	"github.com/ararat-network/ark/pricefeed/api"
)

func validConfigTOML() string {
	return `update_interval = "1500ms"
fallback_feeds = ["ausd"]

[client]
addresses = ["127.0.0.1:9090"]
timeout = "2s"
interval = "5s"

[providers.frankfurter_api]
name = "frankfurter_api"
transport_type = "api"
max_price_age = "90s"
markets = [{ pair = "NOAH/USD", symbol = "NOAHUSD" }]

[providers.frankfurter_api.api]
name = "frankfurter_api"
timeout = "3s"
interval = "1m"
batch_size = 1

[[providers.frankfurter_api.api.endpoints]]
url = "https://api.frankfurter.dev/v2/rates"
`
}

// offlineConfigTOML is validConfigTOML with every endpoint pointed somewhere
// unroutable and every interval pushed past the test's lifetime, so a sidecar
// started from it reaches its serving state without leaving the machine.
func offlineConfigTOML() string {
	return `update_interval = "1h"
fallback_feeds = ["ausd"]

[client]
addresses = ["passthrough:///localhost:19519"]
timeout = "1s"
interval = "1h"

[providers.frankfurter_api]
name = "frankfurter_api"
transport_type = "api"
max_price_age = "90s"
markets = [{ pair = "NOAH/USD", symbol = "NOAHUSD" }]

[providers.frankfurter_api.api]
name = "frankfurter_api"
timeout = "1s"
interval = "1h"
batch_size = 1

[[providers.frankfurter_api.api.endpoints]]
url = "https://localhost.invalid/rates"
`
}

func writeConfig(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "pricefeed.toml")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

	return path
}

func pricesResponse(t *testing.T) *api.PricesResponse {
	t.Helper()

	ausd, err := encoding.EncodeCompactLegacyDec(math.LegacyNewDec(1))
	require.NoError(t, err)
	akrw, err := encoding.EncodeCompactLegacyDec(math.LegacyMustNewDecFromStr("1234.5"))
	require.NoError(t, err)

	return &api.PricesResponse{
		Prices: map[string][]byte{
			"ausd": ausd,
			"akrw": akrw,
		},
		Timestamp: time.Date(2026, time.July, 10, 10, 11, 12, 0, time.UTC),
		Version:   "v1.2.3",
	}
}
