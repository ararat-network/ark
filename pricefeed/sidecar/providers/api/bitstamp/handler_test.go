package bitstamp_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/bitstamp"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

// TestBatchTickersIgnoresBatchSize records the endpoint shape: one response
// carries every market, so splitting would only repeat the request.
func TestBatchTickersIgnoresBatchSize(t *testing.T) {
	handler := NewHandler()
	tickers := []types.Ticker{"USDT/USD", "BTC/USD", "ETH/USD"}

	for _, batchSize := range []int{0, 1, 2} {
		got, err := handler.BatchTickers(tickers, batchSize)
		require.NoError(t, err)
		require.Equal(t, [][]types.Ticker{tickers}, got)
	}

	got, err := handler.BatchTickers(nil, 1)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestCreateURL(t *testing.T) {
	handler := NewHandler()

	gotURL, err := handler.CreateURL(types.Endpoint{URL: URL}, []types.Ticker{"USDT/USD"})
	require.NoError(t, err)
	require.Equal(t, URL, gotURL)

	_, err = handler.CreateURL(types.Endpoint{URL: URL}, nil)
	require.ErrorContains(t, err, "no tickers provided")
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
			name:         "resolves the last price",
			tickers:      []types.Ticker{"USDT/USD"},
			body:         `[{"pair":"USDT/USD","last":"1.00010","bid":"1.0000"}]`,
			wantResolved: map[types.Ticker]string{"USDT/USD": "1.0001"},
		},
		{
			// The endpoint returns every market; only requested pairs
			// resolve, matched case-insensitively.
			name:    "filters to the requested pairs",
			tickers: []types.Ticker{"usdt/usd", "BTC/USD"},
			body: `[
				{"pair":"ETH/USD","last":"2253.7"},
				{"pair":"BTC/USD","last":"67734.5"},
				{"pair":"USDT/USD","last":"0.9999"}
			]`,
			wantResolved: map[types.Ticker]string{"usdt/usd": "0.9999", "BTC/USD": "67734.5"},
		},
		{
			name:         "marks a missing pair as no response",
			tickers:      []types.Ticker{"USDT/USD", "BTC/USD"},
			body:         `[{"pair":"USDT/USD","last":"1.0001"}]`,
			wantResolved: map[types.Ticker]string{"USDT/USD": "1.0001"},
			wantErrors:   map[types.Ticker]types.ErrorCode{"BTC/USD": types.ErrorNoResponse},
		},
		{
			name:         "isolates an unparseable price",
			tickers:      []types.Ticker{"USDT/USD", "BTC/USD"},
			body:         `[{"pair":"USDT/USD","last":"not-a-price"},{"pair":"BTC/USD","last":"67734.5"}]`,
			wantResolved: map[types.Ticker]string{"BTC/USD": "67734.5"},
			wantErrors:   map[types.Ticker]types.ErrorCode{"USDT/USD": types.ErrorFailedToParsePrice},
		},
		{
			name:       "marks malformed json as a decode failure",
			tickers:    []types.Ticker{"USDT/USD"},
			body:       `{`,
			wantErrors: map[types.Ticker]types.ErrorCode{"USDT/USD": types.ErrorFailedToDecode},
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
