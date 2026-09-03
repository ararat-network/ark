package binance

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

func TestBatchTickers(t *testing.T) {
	tickers := []types.Ticker{"ATOMUSDT", "BTCUSDT", "ETHUSDT"}

	tests := []struct {
		name      string
		batchSize int
		want      [][]types.Ticker
	}{
		{
			name:      "zero keeps all tickers together",
			batchSize: 0,
			want:      [][]types.Ticker{{"ATOMUSDT", "BTCUSDT", "ETHUSDT"}},
		},
		{
			name:      "one creates one request per ticker",
			batchSize: 1,
			want:      [][]types.Ticker{{"ATOMUSDT"}, {"BTCUSDT"}, {"ETHUSDT"}},
		},
		{
			name:      "positive size chunks tickers",
			batchSize: 2,
			want:      [][]types.Ticker{{"ATOMUSDT", "BTCUSDT"}, {"ETHUSDT"}},
		},
	}

	handler := NewHandler()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := handler.BatchTickers(tickers, tt.batchSize)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}

	got, err := handler.BatchTickers(nil, 2)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestCreateURL(t *testing.T) {
	handler := NewHandler()

	tests := []struct {
		name        string
		endpoint    types.Endpoint
		tickers     []types.Ticker
		wantURL     string
		errContains string
	}{
		{
			name:     "single symbol",
			endpoint: types.Endpoint{URL: URL},
			tickers:  []types.Ticker{"BTCUSDT"},
			wantURL:  `https://api.binance.com/api/v3/ticker/price?symbols=%5B%22BTCUSDT%22%5D`,
		},
		{
			// The separator lands between symbols and never trails the list:
			// a trailing comma inside the bracket makes the query malformed.
			name:     "batch keeps order and drops the trailing separator",
			endpoint: types.Endpoint{URL: URL},
			tickers:  []types.Ticker{"BTCUSDT", "ETHUSDT", "ATOMUSDT"},
			wantURL: `https://api.binance.com/api/v3/ticker/price?symbols=` +
				`%5B%22BTCUSDT%22,%22ETHUSDT%22,%22ATOMUSDT%22%5D`,
		},
		{
			name:        "empty tickers",
			endpoint:    types.Endpoint{URL: URL},
			errContains: "invalid or no ticker were provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, err := handler.CreateURL(tt.endpoint, tt.tickers)
			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantURL, gotURL)

			// The percent-encoding has to survive a parse, since the fetcher
			// hands this string straight to net/http.
			parsed, err := url.Parse(gotURL)
			require.NoError(t, err)
			require.Equal(t, encodedSymbolList(tt.tickers), parsed.Query().Get("symbols"))
		})
	}
}

// TestDefaultMarketsCreateOneUSDRequest walks the built-in market set through
// the same path the sidecar does, so a mapping that no longer batches or
// encodes cleanly fails here rather than at runtime.
func TestDefaultMarketsCreateOneUSDRequest(t *testing.T) {
	handler := NewHandler()
	tickers := make([]types.Ticker, 0, len(DefaultMarkets))
	for _, market := range DefaultMarkets {
		tickers = append(tickers, market.Symbol)
	}

	batches, err := handler.BatchTickers(tickers, DefaultNonUSAPIConfig.BatchSize)
	require.NoError(t, err)
	require.Equal(t, [][]types.Ticker{tickers}, batches)

	gotURL, err := handler.CreateURL(types.Endpoint{URL: URL}, batches[0])
	require.NoError(t, err)
	parsed, err := url.Parse(gotURL)
	require.NoError(t, err)
	require.Equal(t, `["USDTUSD"]`, parsed.Query().Get("symbols"))
}

// encodedSymbolList is what the query parameter decodes to once net/url has
// undone the percent-encoding CreateURL builds by hand.
func encodedSymbolList(tickers []types.Ticker) string {
	quoted := make([]string, 0, len(tickers))
	for _, ticker := range tickers {
		quoted = append(quoted, `"`+string(ticker)+`"`)
	}
	return "[" + strings.Join(quoted, Separator) + "]"
}

func TestParseResponse(t *testing.T) {
	handler := NewHandler()

	tests := []struct {
		name         string
		tickers      []types.Ticker
		body         string
		wantResolved map[types.Ticker]string
		wantErrors   map[types.Ticker]types.ErrorCode
	}{
		{
			name:         "resolves a single symbol",
			tickers:      []types.Ticker{"BTCUSDT"},
			body:         `[{"symbol":"BTCUSDT","price":"4.00000200"}]`,
			wantResolved: map[types.Ticker]string{"BTCUSDT": "4.000002"},
		},
		{
			// Binance does not promise request order, and the lookup is
			// case-insensitive, so neither may be relied on by the caller.
			name:    "resolves a batch out of order and case insensitively",
			tickers: []types.Ticker{"btcusdt", "ethusdt"},
			body: `[
				{"symbol":"ETHUSDT","price":"0.07946600"},
				{"symbol":"BTCUSDT","price":"4.00000200"}
			]`,
			wantResolved: map[types.Ticker]string{
				"btcusdt": "4.000002",
				"ethusdt": "0.079466",
			},
		},
		{
			// A symbol nobody asked for is dropped rather than resolved
			// against some other ticker.
			name:         "ignores symbols that were not requested",
			tickers:      []types.Ticker{"BTCUSDT"},
			body:         `[{"symbol":"LTCBTC","price":"4.00000200"}]`,
			wantErrors:   map[types.Ticker]types.ErrorCode{"BTCUSDT": types.ErrorNoResponse},
			wantResolved: map[types.Ticker]string{},
		},
		{
			name:         "marks a missing batch member as no response",
			tickers:      []types.Ticker{"BTCUSDT", "ETHUSDT"},
			body:         `[{"symbol":"BTCUSDT","price":"4.00000200"}]`,
			wantResolved: map[types.Ticker]string{"BTCUSDT": "4.000002"},
			wantErrors:   map[types.Ticker]types.ErrorCode{"ETHUSDT": types.ErrorNoResponse},
		},
		{
			// One unparseable price must not cost the rest of the batch.
			name:    "isolates an unparseable price within a batch",
			tickers: []types.Ticker{"BTCUSDT", "ETHUSDT"},
			body: `[
				{"symbol":"BTCUSDT","price":"not-a-price"},
				{"symbol":"ETHUSDT","price":"0.07946600"}
			]`,
			wantResolved: map[types.Ticker]string{"ETHUSDT": "0.079466"},
			wantErrors:   map[types.Ticker]types.ErrorCode{"BTCUSDT": types.ErrorFailedToParsePrice},
		},
		{
			name:    "marks malformed json as a decode failure",
			tickers: []types.Ticker{"BTCUSDT", "ETHUSDT"},
			body:    `{`,
			wantErrors: map[types.Ticker]types.ErrorCode{
				"BTCUSDT": types.ErrorFailedToDecode,
				"ETHUSDT": types.ErrorFailedToDecode,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := handler.ParseResponse(tt.tickers, httpResponse(tt.body))
			require.Len(t, response.Resolved, len(tt.wantResolved))
			require.Len(t, response.Unresolved, len(tt.wantErrors))

			for ticker, wantPrice := range tt.wantResolved {
				result, ok := response.Resolved[ticker]
				require.True(t, ok)
				require.NotNil(t, result.Price)
				require.Equal(t, wantPrice, result.Price.Text('f', -1))
				require.False(t, result.Timestamp.IsZero())
			}
			for ticker, wantCode := range tt.wantErrors {
				require.Equal(t, wantCode, response.Unresolved[ticker].Code())
			}
		})
	}
}

func httpResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
