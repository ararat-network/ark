package coinbase_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/coinbase"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

// TestBatchTickersIsOnePerRequest records the endpoint shape: one product per
// request, whatever batch size the config carries.
func TestBatchTickersIsOnePerRequest(t *testing.T) {
	handler := NewHandler()
	tickers := []types.Ticker{"USDT-USD", "BTC-USD"}

	for _, batchSize := range []int{0, 2} {
		got, err := handler.BatchTickers(tickers, batchSize)
		require.NoError(t, err)
		require.Equal(t, [][]types.Ticker{{"USDT-USD"}, {"BTC-USD"}}, got)
	}

	got, err := handler.BatchTickers(nil, 0)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestCreateURL(t *testing.T) {
	handler := NewHandler()

	tests := []struct {
		name        string
		tickers     []types.Ticker
		wantPath    string
		errContains string
	}{
		{
			name:     "single product",
			tickers:  []types.Ticker{"USDT-USD"},
			wantPath: "/v2/prices/USDT-USD/spot",
		},
		{
			name:        "empty tickers",
			errContains: "expected 1 ticker, got 0",
		},
		{
			name:        "malformed product",
			tickers:     []types.Ticker{"USDTUSD"},
			errContains: "not in BASE-QUOTE form",
		},
		{
			name:        "more than one ticker",
			tickers:     []types.Ticker{"USDT-USD", "BTC-USD"},
			errContains: "expected 1 ticker, got 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, err := handler.CreateURL(types.Endpoint{URL: URL}, tt.tickers)
			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
				return
			}

			require.NoError(t, err)
			parsed, err := url.Parse(gotURL)
			require.NoError(t, err)
			require.Equal(t, "api.coinbase.com", parsed.Host)
			require.Equal(t, tt.wantPath, parsed.Path)
		})
	}
}

func TestParseResponse(t *testing.T) {
	handler := NewHandler()

	tests := []struct {
		name      string
		tickers   []types.Ticker
		body      string
		wantPrice string
		wantCode  types.ErrorCode
	}{
		{
			name:      "resolves the spot amount",
			tickers:   []types.Ticker{"USDT-USD"},
			body:      `{"data":{"amount":"1.0001","currency":"USD"}}`,
			wantPrice: "1.0001",
		},
		{
			// The endpoint answers in the product's quote; another currency
			// means the wrong product was priced.
			name:     "quoted in another currency is invalid",
			tickers:  []types.Ticker{"USDT-USD"},
			body:     `{"data":{"amount":"0.9","currency":"EUR"}}`,
			wantCode: types.ErrorInvalidResponse,
		},
		{
			name:     "malformed ticker",
			tickers:  []types.Ticker{"USDTUSD"},
			body:     `{"data":{"amount":"1.0001","currency":"USD"}}`,
			wantCode: types.ErrorUnknownPair,
		},
		{
			name:     "missing amount is no response",
			tickers:  []types.Ticker{"USDT-USD"},
			body:     `{"data":{"currency":"USD"}}`,
			wantCode: types.ErrorNoResponse,
		},
		{
			name:     "unparseable amount",
			tickers:  []types.Ticker{"USDT-USD"},
			body:     `{"data":{"amount":"not-a-price","currency":"USD"}}`,
			wantCode: types.ErrorFailedToParsePrice,
		},
		{
			name:     "malformed json",
			tickers:  []types.Ticker{"USDT-USD"},
			body:     `{`,
			wantCode: types.ErrorFailedToDecode,
		},
		{
			// A multi-ticker batch cannot be attributed to one product, so
			// the whole batch is refused rather than guessed at.
			name:     "more than one ticker is invalid",
			tickers:  []types.Ticker{"USDT-USD", "BTC-USD"},
			body:     `{"data":{"amount":"1.0001","currency":"USD"}}`,
			wantCode: types.ErrorInvalidResponse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := handler.ParseResponse(tt.tickers, httpResponse(tt.body))

			if tt.wantCode != 0 {
				require.Empty(t, response.Resolved)
				require.Len(t, response.Unresolved, len(tt.tickers))
				for _, ticker := range tt.tickers {
					require.Equal(t, tt.wantCode, response.Unresolved[ticker].Code())
				}
				return
			}

			require.Empty(t, response.Unresolved)
			result, ok := response.Resolved[tt.tickers[0]]
			require.True(t, ok)
			require.Equal(t, tt.wantPrice, result.Price.Text('f', -1))
			require.False(t, result.Timestamp.IsZero())
		})
	}
}

func httpResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
