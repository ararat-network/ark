package chainsuite

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPinPrices checks the rewrite on the layout `pricefeed init` writes: endpoints point nowhere,
// every leg has a bootstrap price that outlives the run, and nothing else moves.
func TestPinPrices(t *testing.T) {
	generated := `[providers.frankfurter_api.api]
timeout = "3s"

[[providers.frankfurter_api.api.endpoints]]
url = "https://api.frankfurter.dev/v2/rates"
authentication = { api_key = "", api_key_header = "" }

# Last-resort prices.
[[resolver.bootstrap_prices]]
pair = "USD/NOAH"
price = "0.729394602479941648"
valid_until = "2027-10-29T00:00:00Z"

[[resolver.routes.axdr]]
name = "xdr-usd-noah"
pairs = ["XDR/USD", "USD/NOAH"]
`
	pinned, err := pinPrices([]byte(generated))
	require.NoError(t, err)
	out := string(pinned)

	require.NotContains(t, out, "frankfurter.dev")
	require.Contains(t, out, `url = "https://offline.invalid/"`)
	require.NotContains(t, out, "2027-10-29")
	require.Equal(t, len(PinnedPrices)+1, strings.Count(out, bootstrapTable))
	require.Equal(t, len(PinnedPrices)+1, strings.Count(out, `valid_until = "`+pinnedUntil+`"`))
	require.Contains(t, out, "pair = \"XDR/USD\"\nprice = \"1.371\"")
	require.Contains(t, out, `price = "0.729394602479941648"`)
	require.Contains(t, out, `timeout = "3s"`)
	require.Contains(t, out, `pairs = ["XDR/USD", "USD/NOAH"]`)

	_, err = pinPrices([]byte("[client]\naddresses = []\n"))
	require.ErrorContains(t, err, "no [[resolver.bootstrap_prices]] table")
}
