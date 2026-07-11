package frankfurter_test

import (
	. "ark/oracle/sidecar/providers/api/frankfurter"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ark/oracle/sidecar/providers/types"
)

func TestBatchTickers(t *testing.T) {
	handler := NewHandler()

	tests := []struct {
		name        string
		tickers     []types.Ticker
		batchSize   int
		want        [][]types.Ticker
		errContains string
	}{
		{
			name:      "zero batch size groups all tickers by base",
			tickers:   []types.Ticker{"USD/KRW", "EUR/GBP", "USD/JPY", "EUR/CHF"},
			batchSize: 0,
			want: [][]types.Ticker{
				{"USD/KRW", "USD/JPY"},
				{"EUR/GBP", "EUR/CHF"},
			},
		},
		{
			name:      "batch size one keeps mixed bases as single pairs",
			tickers:   []types.Ticker{"USD/KRW", "EUR/GBP", "USD/JPY"},
			batchSize: 1,
			want: [][]types.Ticker{
				{"USD/KRW"},
				{"USD/JPY"},
				{"EUR/GBP"},
			},
		},
		{
			name:      "positive batch size chunks within each base",
			tickers:   []types.Ticker{"USD/KRW", "EUR/GBP", "USD/JPY", "USD/CNY", "EUR/CHF"},
			batchSize: 2,
			want: [][]types.Ticker{
				{"USD/KRW", "USD/JPY"},
				{"USD/CNY"},
				{"EUR/GBP", "EUR/CHF"},
			},
		},
		{
			name: "empty tickers",
		},
		{
			name:        "malformed ticker",
			tickers:     []types.Ticker{"USD/KRW", "EURGBP"},
			errContains: `expected ticker in "BASE/QUOTE" format`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := handler.BatchTickers(tt.tickers, tt.batchSize)
			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
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
			name:     "single currency pair",
			endpoint: types.Endpoint{URL: URL},
			tickers:  []types.Ticker{"EUR/USD"},
			wantURL:  "https://api.frankfurter.dev/v2/rates?base=EUR&quotes=USD",
		},
		{
			name:     "same base batch",
			endpoint: types.Endpoint{URL: URL},
			tickers:  []types.Ticker{"EUR/USD", "eur/gbp"},
			wantURL:  "https://api.frankfurter.dev/v2/rates?base=EUR&quotes=USD%2CGBP",
		},
		{
			name:     "preserves endpoint query parameters",
			endpoint: types.Endpoint{URL: URL + "?providers=ECB"},
			tickers:  []types.Ticker{"EUR/USD"},
			wantURL:  "https://api.frankfurter.dev/v2/rates?base=EUR&providers=ECB&quotes=USD",
		},
		{
			name:        "empty tickers",
			endpoint:    types.Endpoint{URL: URL},
			errContains: "tickers cannot be empty",
		},
		{
			name:        "mixed base batch",
			endpoint:    types.Endpoint{URL: URL},
			tickers:     []types.Ticker{"EUR/USD", "USD/GBP"},
			errContains: `ticker base "USD" does not match batch base "EUR"`,
		},
		{
			name:        "malformed ticker",
			endpoint:    types.Endpoint{URL: URL},
			tickers:     []types.Ticker{"EURUSD"},
			errContains: `expected ticker in "BASE/QUOTE" format`,
		},
		{
			name:        "empty base",
			endpoint:    types.Endpoint{URL: URL},
			tickers:     []types.Ticker{"/USD"},
			errContains: `expected ticker in "BASE/QUOTE" format`,
		},
		{
			name:        "empty quote",
			endpoint:    types.Endpoint{URL: URL},
			tickers:     []types.Ticker{"EUR/"},
			errContains: `expected ticker in "BASE/QUOTE" format`,
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
		})
	}
}

func TestDefaultMarketsCreateOneUSDRequest(t *testing.T) {
	handler := NewHandler()
	tickers := make([]types.Ticker, 0, len(DefaultMarkets))
	for _, market := range DefaultMarkets {
		tickers = append(tickers, market.Symbol)
	}

	batches, err := handler.BatchTickers(tickers, DefaultAPIConfig.BatchSize)
	require.NoError(t, err)
	require.Equal(t, [][]types.Ticker{tickers}, batches)

	gotURL, err := handler.CreateURL(types.Endpoint{URL: URL}, batches[0])
	require.NoError(t, err)
	parsed, err := url.Parse(gotURL)
	require.NoError(t, err)
	require.Equal(t, "USD", parsed.Query().Get("base"))
	require.Equal(t, "KRW,XDR,CNY,JPY,EUR,GBP,MNT", parsed.Query().Get("quotes"))
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
			name:    "resolves single rate array",
			tickers: []types.Ticker{"EUR/USD"},
			body:    `[{"date":"2026-03-25","base":"EUR","quote":"USD","rate":1.1568}]`,
			wantResolved: map[types.Ticker]string{
				"EUR/USD": "1.1568",
			},
		},
		{
			name:    "resolves batch out of order and case insensitively",
			tickers: []types.Ticker{"eur/usd", "eur/gbp"},
			body: `[
				{"date":"2026-03-25","base":"EUR","quote":"GBP","rate":0.8623},
				{"date":"2026-03-25","base":"EUR","quote":"JPY","rate":165.1},
				{"date":"2026-03-25","base":"EUR","quote":"USD","rate":1.1568}
			]`,
			wantResolved: map[types.Ticker]string{
				"eur/usd": "1.1568",
				"eur/gbp": "0.8623",
			},
		},
		{
			name:    "marks missing batch member as no response",
			tickers: []types.Ticker{"EUR/USD", "EUR/GBP"},
			body:    `[{"date":"2026-03-25","base":"EUR","quote":"USD","rate":1.1568}]`,
			wantResolved: map[types.Ticker]string{
				"EUR/USD": "1.1568",
			},
			wantErrors: map[types.Ticker]types.ErrorCode{
				"EUR/GBP": types.ErrorNoResponse,
			},
		},
		{
			name:    "isolates malformed rate within batch",
			tickers: []types.Ticker{"EUR/USD", "EUR/GBP"},
			body: `[
				{"date":"2026-03-25","base":"EUR","quote":"USD"},
				{"date":"2026-03-25","base":"EUR","quote":"GBP","rate":0.8623}
			]`,
			wantResolved: map[types.Ticker]string{
				"EUR/GBP": "0.8623",
			},
			wantErrors: map[types.Ticker]types.ErrorCode{
				"EUR/USD": types.ErrorFailedToParsePrice,
			},
		},
		{
			name:    "marks malformed json as decode failure",
			tickers: []types.Ticker{"EUR/USD", "EUR/GBP"},
			body:    `{`,
			wantErrors: map[types.Ticker]types.ErrorCode{
				"EUR/USD": types.ErrorFailedToDecode,
				"EUR/GBP": types.ErrorFailedToDecode,
			},
		},
		{
			name:    "marks unexpected pairs as no response",
			tickers: []types.Ticker{"EUR/USD"},
			body:    `[{"date":"2026-03-25","base":"EUR","quote":"GBP","rate":0.8623}]`,
			wantErrors: map[types.Ticker]types.ErrorCode{
				"EUR/USD": types.ErrorNoResponse,
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
