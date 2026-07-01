package frankfurter

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"noah/oracle/providers/types"
)

func TestCreateURL(t *testing.T) {
	handler := NewHandler()
	endpoint := types.Endpoint{URL: URL}

	tests := []struct {
		name        string
		tickers     []types.Ticker
		wantURL     string
		errContains string
	}{
		{
			name:    "single currency pair",
			tickers: []types.Ticker{"EUR/USD"},
			wantURL: "https://api.frankfurter.dev/v2/rate/EUR/USD",
		},
		{
			name:        "empty tickers",
			errContains: "expected exactly one ticker",
		},
		{
			name:        "multiple tickers",
			tickers:     []types.Ticker{"EUR/USD", "EUR/GBP"},
			errContains: "expected exactly one ticker",
		},
		{
			name:        "malformed ticker",
			tickers:     []types.Ticker{"EURUSD"},
			errContains: `expected ticker in "BASE/QUOTE" format`,
		},
		{
			name:        "empty base",
			tickers:     []types.Ticker{"/USD"},
			errContains: `expected ticker in "BASE/QUOTE" format`,
		},
		{
			name:        "empty quote",
			tickers:     []types.Ticker{"EUR/"},
			errContains: `expected ticker in "BASE/QUOTE" format`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, err := handler.CreateURL(endpoint, tt.tickers)
			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantURL, gotURL)
		})
	}
}

func TestParseResponse(t *testing.T) {
	handler := NewHandler()

	tests := []struct {
		name       string
		tickers    []types.Ticker
		body       string
		wantTicker types.Ticker
		wantPrice  string
		wantErr    types.ErrorCode
	}{
		{
			name:       "resolves matching rate",
			tickers:    []types.Ticker{"EUR/USD"},
			body:       `{"date":"2026-03-25","base":"EUR","quote":"USD","rate":1.1568}`,
			wantTicker: "EUR/USD",
			wantPrice:  "1.1568",
		},
		{
			name:       "matches pair case insensitively",
			tickers:    []types.Ticker{"eur/usd"},
			body:       `{"date":"2026-03-25","base":"EUR","quote":"USD","rate":1.1568}`,
			wantTicker: "eur/usd",
			wantPrice:  "1.1568",
		},
		{
			name:    "marks malformed json as decode failure",
			tickers: []types.Ticker{"EUR/USD"},
			body:    `{`,
			wantErr: types.ErrorFailedToDecode,
		},
		{
			name:    "marks malformed rate as parse failure",
			tickers: []types.Ticker{"EUR/USD"},
			body:    `{"date":"2026-03-25","base":"EUR","quote":"USD"}`,
			wantErr: types.ErrorFailedToParsePrice,
		},
		{
			name:    "marks unexpected pair as no response",
			tickers: []types.Ticker{"EUR/USD"},
			body:    `{"date":"2026-03-25","base":"EUR","quote":"GBP","rate":0.8623}`,
			wantErr: types.ErrorNoResponse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := handler.ParseResponse(tt.tickers, httpResponse(tt.body))
			if tt.wantErr != types.OK {
				require.Empty(t, response.Resolved)
				require.Equal(t, tt.wantErr, response.Unresolved[tt.tickers[0]].Code())
				return
			}

			require.Empty(t, response.Unresolved)
			result, ok := response.Resolved[tt.wantTicker]
			require.True(t, ok)
			require.NotNil(t, result.Price)
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
