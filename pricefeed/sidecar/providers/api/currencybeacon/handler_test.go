package currencybeacon_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/currencybeacon"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
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
			tickers:   []types.Ticker{"USD/KRW", "EUR/GBP", "USD/JPY"},
			batchSize: 0,
			want: [][]types.Ticker{
				{"USD/KRW", "USD/JPY"},
				{"EUR/GBP"},
			},
		},
		{
			name:      "positive batch size chunks within each base",
			tickers:   []types.Ticker{"USD/KRW", "USD/JPY", "USD/CNY"},
			batchSize: 2,
			want: [][]types.Ticker{
				{"USD/KRW", "USD/JPY"},
				{"USD/CNY"},
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
			tickers:  []types.Ticker{"USD/KRW"},
			wantURL:  "https://api.currencybeacon.com/v1/latest?base=USD&symbols=KRW",
		},
		{
			name:     "same base batch",
			endpoint: types.Endpoint{URL: URL},
			tickers:  []types.Ticker{"USD/KRW", "usd/xdr"},
			wantURL:  "https://api.currencybeacon.com/v1/latest?base=USD&symbols=KRW%2CXDR",
		},
		{
			name:     "preserves endpoint query parameters",
			endpoint: types.Endpoint{URL: URL + "?format=json"},
			tickers:  []types.Ticker{"USD/KRW"},
			wantURL:  "https://api.currencybeacon.com/v1/latest?base=USD&format=json&symbols=KRW",
		},
		{
			name:        "empty tickers",
			endpoint:    types.Endpoint{URL: URL},
			errContains: "tickers cannot be empty",
		},
		{
			name:        "mixed base batch",
			endpoint:    types.Endpoint{URL: URL},
			tickers:     []types.Ticker{"USD/KRW", "EUR/GBP"},
			errContains: `ticker base "EUR" does not match batch base "USD"`,
		},
		{
			name:        "malformed ticker",
			endpoint:    types.Endpoint{URL: URL},
			tickers:     []types.Ticker{"USDKRW"},
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
	require.Equal(t, "KRW,XDR,CNY,JPY,EUR,GBP,MNT", parsed.Query().Get("symbols"))
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
			name:    "resolves single rate",
			tickers: []types.Ticker{"USD/KRW"},
			body:    `{"meta":{"code":200},"response":{"date":"2026-08-28","base":"USD","rates":{"KRW":1355.25}}}`,
			wantResolved: map[types.Ticker]string{
				"USD/KRW": "1355.25",
			},
		},
		{
			name:    "resolves batch case insensitively and skips unrequested rates",
			tickers: []types.Ticker{"usd/krw", "usd/xdr"},
			body:    `{"meta":{"code":200},"response":{"date":"2026-08-28","base":"USD","rates":{"KRW":1355.25,"XDR":0.7332,"JPY":151.4}}}`,
			wantResolved: map[types.Ticker]string{
				"usd/krw": "1355.25",
				"usd/xdr": "0.7332",
			},
		},
		{
			name:    "marks missing batch member as no response",
			tickers: []types.Ticker{"USD/KRW", "USD/MNT"},
			body:    `{"meta":{"code":200},"response":{"date":"2026-08-28","base":"USD","rates":{"KRW":1355.25}}}`,
			wantResolved: map[types.Ticker]string{
				"USD/KRW": "1355.25",
			},
			wantErrors: map[types.Ticker]types.ErrorCode{
				"USD/MNT": types.ErrorNoResponse,
			},
		},
		{
			name:    "isolates malformed rate within batch",
			tickers: []types.Ticker{"USD/KRW", "USD/JPY"},
			body:    `{"meta":{"code":200},"response":{"date":"2026-08-28","base":"USD","rates":{"KRW":null,"JPY":151.4}}}`,
			wantResolved: map[types.Ticker]string{
				"USD/JPY": "151.4",
			},
			wantErrors: map[types.Ticker]types.ErrorCode{
				"USD/KRW": types.ErrorFailedToParsePrice,
			},
		},
		{
			name:    "marks base mismatch as no response",
			tickers: []types.Ticker{"USD/KRW"},
			body:    `{"meta":{"code":200},"response":{"date":"2026-08-28","base":"EUR","rates":{"KRW":1575.0}}}`,
			wantErrors: map[types.Ticker]types.ErrorCode{
				"USD/KRW": types.ErrorNoResponse,
			},
		},
		{
			name:    "marks non-success meta code as api error",
			tickers: []types.Ticker{"USD/KRW", "USD/JPY"},
			body:    `{"meta":{"code":401}}`,
			wantErrors: map[types.Ticker]types.ErrorCode{
				"USD/KRW": types.ErrorAPIGeneral,
				"USD/JPY": types.ErrorAPIGeneral,
			},
		},
		{
			name:    "marks malformed json as decode failure",
			tickers: []types.Ticker{"USD/KRW", "USD/JPY"},
			body:    `{`,
			wantErrors: map[types.Ticker]types.ErrorCode{
				"USD/KRW": types.ErrorFailedToDecode,
				"USD/JPY": types.ErrorFailedToDecode,
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
