package kraken_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/kraken"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

func TestBatchTickers(t *testing.T) {
	handler := NewHandler()
	tickers := []types.Ticker{"USDTZUSD", "XXBTZUSD", "XETHZUSD"}

	tests := []struct {
		name      string
		batchSize int
		want      [][]types.Ticker
	}{
		{
			name:      "zero keeps all tickers together",
			batchSize: 0,
			want:      [][]types.Ticker{{"USDTZUSD", "XXBTZUSD", "XETHZUSD"}},
		},
		{
			name:      "positive size chunks tickers",
			batchSize: 2,
			want:      [][]types.Ticker{{"USDTZUSD", "XXBTZUSD"}, {"XETHZUSD"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := handler.BatchTickers(tickers, tt.batchSize)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestCreateURL(t *testing.T) {
	handler := NewHandler()

	tests := []struct {
		name        string
		tickers     []types.Ticker
		wantPair    string
		errContains string
	}{
		{
			name:     "single pair",
			tickers:  []types.Ticker{"USDTZUSD"},
			wantPair: "USDTZUSD",
		},
		{
			name:     "batch keeps order",
			tickers:  []types.Ticker{"USDTZUSD", "XXBTZUSD"},
			wantPair: "USDTZUSD,XXBTZUSD",
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
			require.Equal(t, "https", parsed.Scheme)
			require.Equal(t, "api.kraken.com", parsed.Host)
			require.Equal(t, "/0/public/Ticker", parsed.Path)
			require.Equal(t, tt.wantPair, parsed.Query().Get("pair"))
		})
	}
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
			name:         "resolves the close price",
			tickers:      []types.Ticker{"USDTZUSD"},
			body:         `{"error":[],"result":{"USDTZUSD":{"a":["1.00010000","1","1.000"],"c":["1.00000000","100.5"]}}}`,
			wantResolved: map[types.Ticker]string{"USDTZUSD": "1"},
		},
		{
			// Kraken keys results by full pair name whatever the request
			// used, so the match is case-insensitive on the full name.
			name:    "resolves a batch case insensitively",
			tickers: []types.Ticker{"usdtzusd", "XXBTZUSD"},
			body: `{"error":[],"result":{
				"XXBTZUSD":{"c":["67734.50000","0.001"]},
				"USDTZUSD":{"c":["0.99990000","10"]}
			}}`,
			wantResolved: map[types.Ticker]string{"usdtzusd": "0.9999", "XXBTZUSD": "67734.5"},
		},
		{
			name:         "ignores pairs that were not requested",
			tickers:      []types.Ticker{"USDTZUSD"},
			body:         `{"error":[],"result":{"XXBTZUSD":{"c":["67734.5","0.001"]}}}`,
			wantResolved: map[types.Ticker]string{},
			wantErrors:   map[types.Ticker]types.ErrorCode{"USDTZUSD": types.ErrorNoResponse},
		},
		{
			name:         "marks an empty close array as invalid",
			tickers:      []types.Ticker{"USDTZUSD", "XXBTZUSD"},
			body:         `{"error":[],"result":{"USDTZUSD":{"c":[]},"XXBTZUSD":{"c":["67734.5","0.001"]}}}`,
			wantResolved: map[types.Ticker]string{"XXBTZUSD": "67734.5"},
			wantErrors:   map[types.Ticker]types.ErrorCode{"USDTZUSD": types.ErrorInvalidResponse},
		},
		{
			name:         "isolates an unparseable price within a batch",
			tickers:      []types.Ticker{"USDTZUSD", "XXBTZUSD"},
			body:         `{"error":[],"result":{"USDTZUSD":{"c":["not-a-price","1"]},"XXBTZUSD":{"c":["67734.5","0.001"]}}}`,
			wantResolved: map[types.Ticker]string{"XXBTZUSD": "67734.5"},
			wantErrors:   map[types.Ticker]types.ErrorCode{"USDTZUSD": types.ErrorFailedToParsePrice},
		},
		{
			// A request-level error empties the result, so every ticker fails
			// with the venue's message rather than a bare no-response.
			name:    "request level error fails every ticker",
			tickers: []types.Ticker{"USDTZUSD", "XXBTZUSD"},
			body:    `{"error":["EQuery:Unknown asset pair"],"result":{}}`,
			wantErrors: map[types.Ticker]types.ErrorCode{
				"USDTZUSD": types.ErrorAPIGeneral,
				"XXBTZUSD": types.ErrorAPIGeneral,
			},
		},
		{
			name:       "marks malformed json as a decode failure",
			tickers:    []types.Ticker{"USDTZUSD"},
			body:       `{`,
			wantErrors: map[types.Ticker]types.ErrorCode{"USDTZUSD": types.ErrorFailedToDecode},
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
