package coinmarketcap_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/coinmarketcap"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

func TestBatchTickers(t *testing.T) {
	handler := NewHandler()
	tickers := []types.Ticker{"825", "1", "1027"}

	got, err := handler.BatchTickers(tickers, 0)
	require.NoError(t, err)
	require.Equal(t, [][]types.Ticker{tickers}, got)

	got, err = handler.BatchTickers(tickers, 2)
	require.NoError(t, err)
	require.Equal(t, [][]types.Ticker{{"825", "1"}, {"1027"}}, got)
}

func TestCreateURL(t *testing.T) {
	handler := NewHandler()

	tests := []struct {
		name        string
		tickers     []types.Ticker
		wantID      string
		errContains string
	}{
		{
			name:    "single id",
			tickers: []types.Ticker{"825"},
			wantID:  "825",
		},
		{
			name:    "batch keeps order",
			tickers: []types.Ticker{"825", "1"},
			wantID:  "825,1",
		},
		{
			name:        "empty tickers",
			errContains: "no tickers provided",
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
			require.Equal(t, "pro-api.coinmarketcap.com", parsed.Host)
			require.Equal(t, "/v2/cryptocurrency/quotes/latest", parsed.Path)
			require.Equal(t, tt.wantID, parsed.Query().Get("id"))
		})
	}
}

// TestDefaultAPIConfigCarriesTheKeyHeader pins the credential path: the key
// travels in the documented header, and the placeholder must be replaced.
func TestDefaultAPIConfigCarriesTheKeyHeader(t *testing.T) {
	require.NoError(t, DefaultAPIConfig.Validate())
	auth := DefaultAPIConfig.Endpoints[0].Authentication
	require.True(t, auth.Enabled())
	require.Equal(t, APIKeyHeader, auth.APIKeyHeader)
	require.Equal(t, PlaceholderAPIKey, auth.APIKey)
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
			name:    "resolves the USD quote without float rounding",
			tickers: []types.Ticker{"825"},
			body: `{"data":{"825":{"id":825,"symbol":"USDT","quote":{"USD":{"price":1.000123456789012345}}}},
				"status":{"error_code":0,"error_message":""}}`,
			wantResolved: map[types.Ticker]string{"825": "1.000123456789012345"},
		},
		{
			name:    "resolves a batch and ignores ids that were not requested",
			tickers: []types.Ticker{"825", "1"},
			body: `{"data":{
				"825":{"quote":{"USD":{"price":0.9999}}},
				"1":{"quote":{"USD":{"price":67734.5}}},
				"1027":{"quote":{"USD":{"price":3000}}}
			},"status":{"error_code":0}}`,
			wantResolved: map[types.Ticker]string{"825": "0.9999", "1": "67734.5"},
		},
		{
			name:       "missing USD quote is no response",
			tickers:    []types.Ticker{"825"},
			body:       `{"data":{"825":{"quote":{"EUR":{"price":0.92}}}},"status":{"error_code":0}}`,
			wantErrors: map[types.Ticker]types.ErrorCode{"825": types.ErrorNoResponse},
		},
		{
			name:         "marks a missing id as no response",
			tickers:      []types.Ticker{"825", "1"},
			body:         `{"data":{"825":{"quote":{"USD":{"price":1.0}}}},"status":{"error_code":0}}`,
			wantResolved: map[types.Ticker]string{"825": "1"},
			wantErrors:   map[types.Ticker]types.ErrorCode{"1": types.ErrorNoResponse},
		},
		{
			// A status error fails every ticker with the venue's message; the
			// body typically carries no data at all in that case.
			name:    "status error fails every ticker",
			tickers: []types.Ticker{"825", "1"},
			body:    `{"status":{"error_code":1002,"error_message":"API key missing."}}`,
			wantErrors: map[types.Ticker]types.ErrorCode{
				"825": types.ErrorAPIGeneral,
				"1":   types.ErrorAPIGeneral,
			},
		},
		{
			name:       "marks malformed json as a decode failure",
			tickers:    []types.Ticker{"825"},
			body:       `{`,
			wantErrors: map[types.Ticker]types.ErrorCode{"825": types.ErrorFailedToDecode},
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
