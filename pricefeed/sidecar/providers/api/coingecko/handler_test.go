package coingecko_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/coingecko"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

func TestBatchTickers(t *testing.T) {
	handler := NewHandler()
	tickers := []types.Ticker{"tether/usd", "bitcoin/usd", "ethereum/usd"}

	got, err := handler.BatchTickers(tickers, 0)
	require.NoError(t, err)
	require.Equal(t, [][]types.Ticker{tickers}, got)

	got, err = handler.BatchTickers(tickers, 2)
	require.NoError(t, err)
	require.Equal(t, [][]types.Ticker{{"tether/usd", "bitcoin/usd"}, {"ethereum/usd"}}, got)
}

func TestCreateURL(t *testing.T) {
	handler := NewHandler()

	tests := []struct {
		name        string
		tickers     []types.Ticker
		wantIDs     string
		wantQuotes  string
		errContains string
	}{
		{
			name:       "single ticker",
			tickers:    []types.Ticker{"tether/usd"},
			wantIDs:    "tether",
			wantQuotes: "usd",
		},
		{
			// Ids and quotes are sent once each, in first-seen order; the
			// parser drops the cross-product entries nobody asked for.
			name:       "deduplicates ids and quotes",
			tickers:    []types.Ticker{"tether/usd", "bitcoin/usd", "tether/eur"},
			wantIDs:    "tether,bitcoin",
			wantQuotes: "usd,eur",
		},
		{
			// CoinGecko keys ids and quotes lower-case; a capitalised symbol
			// would otherwise be sent verbatim and never resolve.
			name:       "lower-cases ids and quotes",
			tickers:    []types.Ticker{"Tether/USD"},
			wantIDs:    "tether",
			wantQuotes: "usd",
		},
		{
			name:        "empty tickers",
			errContains: "no tickers provided",
		},
		{
			name:        "malformed ticker",
			tickers:     []types.Ticker{"tether"},
			errContains: "not in id/quote form",
		},
		{
			name:        "empty quote",
			tickers:     []types.Ticker{"tether/"},
			errContains: "not in id/quote form",
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
			require.Equal(t, "api.coingecko.com", parsed.Host)
			require.Equal(t, "/api/v3/simple/price", parsed.Path)
			require.Equal(t, tt.wantIDs, parsed.Query().Get("ids"))
			require.Equal(t, tt.wantQuotes, parsed.Query().Get("vs_currencies"))
			require.Equal(t, Precision, parsed.Query().Get("precision"))
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
			// The price arrives as a JSON number; it is kept as text so the
			// eighteen requested decimals survive intact.
			name:         "resolves a price without float rounding",
			tickers:      []types.Ticker{"tether/usd"},
			body:         `{"tether":{"usd":0.999123456789012345}}`,
			wantResolved: map[types.Ticker]string{"tether/usd": "0.999123456789012345"},
		},
		{
			name:    "drops cross-product entries that were not requested",
			tickers: []types.Ticker{"tether/usd", "bitcoin/eur"},
			body: `{
				"tether":{"usd":1.0,"eur":0.92},
				"bitcoin":{"usd":67734.5,"eur":62000.25}
			}`,
			wantResolved: map[types.Ticker]string{"tether/usd": "1", "bitcoin/eur": "62000.25"},
		},
		{
			name:         "marks a missing ticker as no response",
			tickers:      []types.Ticker{"tether/usd", "bitcoin/usd"},
			body:         `{"tether":{"usd":1.0}}`,
			wantResolved: map[types.Ticker]string{"tether/usd": "1"},
			wantErrors:   map[types.Ticker]types.ErrorCode{"bitcoin/usd": types.ErrorNoResponse},
		},
		{
			name:       "marks malformed json as a decode failure",
			tickers:    []types.Ticker{"tether/usd"},
			body:       `{`,
			wantErrors: map[types.Ticker]types.ErrorCode{"tether/usd": types.ErrorFailedToDecode},
		},
		{
			name:       "marks a non-numeric price as a decode failure",
			tickers:    []types.Ticker{"tether/usd"},
			body:       `{"tether":{"usd":"x"}}`,
			wantErrors: map[types.Ticker]types.ErrorCode{"tether/usd": types.ErrorFailedToDecode},
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
