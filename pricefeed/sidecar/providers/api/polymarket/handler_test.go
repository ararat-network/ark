package polymarket_test

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/polymarket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

const (
	conditionID = "0x08f5fe8d0d29c08a96f0bc3dfb52f50e0caf470d94d133d95d38fa6c847e0925"
	yesTokenID  = "95128817762909535143571435260705470642391662537976312011260538371392879420759"
	noTokenID   = "50107902083284751016545440401692219408556171231461347396738260657226842527986"
	yesTicker   = types.Ticker(conditionID + "/" + yesTokenID)
	noTicker    = types.Ticker(conditionID + "/" + noTokenID)
)

// TestBatchTickersIsOnePerRequest records the endpoint shape: one market per
// request, whatever batch size the config carries.
func TestBatchTickersIsOnePerRequest(t *testing.T) {
	handler := NewHandler()

	for _, batchSize := range []int{0, 2} {
		got, err := handler.BatchTickers([]types.Ticker{yesTicker, noTicker}, batchSize)
		require.NoError(t, err)
		require.Equal(t, [][]types.Ticker{{yesTicker}, {noTicker}}, got)
	}
}

func TestCreateURL(t *testing.T) {
	handler := NewHandler()

	tests := []struct {
		name        string
		tickers     []types.Ticker
		errContains string
	}{
		{
			name:    "single token",
			tickers: []types.Ticker{yesTicker},
		},
		{
			name:        "empty tickers",
			errContains: "expected 1 ticker, got 0",
		},
		{
			name:        "more than one ticker",
			tickers:     []types.Ticker{yesTicker, noTicker},
			errContains: "expected 1 ticker, got 2",
		},
		{
			name:        "malformed ticker",
			tickers:     []types.Ticker{types.Ticker(conditionID)},
			errContains: "not in condition_id/token_id form",
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
			require.Equal(t, "clob.polymarket.com", parsed.Host)
			require.Equal(t, "/markets/"+conditionID, parsed.Path)
		})
	}
}

func TestParseResponse(t *testing.T) {
	handler := NewHandler()
	market := `{"condition_id":"` + conditionID + `","tokens":[
		{"token_id":"` + yesTokenID + `","outcome":"Yes","price":0.735},
		{"token_id":"` + noTokenID + `","outcome":"No","price":0}
	]}`

	tests := []struct {
		name      string
		tickers   []types.Ticker
		body      string
		wantPrice string
		wantCode  types.ErrorCode
	}{
		{
			name:      "resolves the requested outcome token",
			tickers:   []types.Ticker{yesTicker},
			body:      market,
			wantPrice: "0.735",
		},
		{
			// A settled outcome reports zero, which the resolver would drop;
			// the floor keeps it observable.
			name:      "floors a zero price",
			tickers:   []types.Ticker{noTicker},
			body:      market,
			wantPrice: MinPrice,
		},
		{
			name:     "missing token is no response",
			tickers:  []types.Ticker{types.Ticker(conditionID + "/1")},
			body:     market,
			wantCode: types.ErrorNoResponse,
		},
		{
			name:     "token without a price is invalid",
			tickers:  []types.Ticker{yesTicker},
			body:     `{"tokens":[{"token_id":"` + yesTokenID + `","outcome":"Yes"}]}`,
			wantCode: types.ErrorInvalidResponse,
		},
		{
			name:     "unparseable price",
			tickers:  []types.Ticker{yesTicker},
			body:     `{"tokens":[{"token_id":"` + yesTokenID + `","price":"x"}]}`,
			wantCode: types.ErrorFailedToDecode,
		},
		{
			name:     "malformed ticker",
			tickers:  []types.Ticker{types.Ticker(conditionID)},
			body:     market,
			wantCode: types.ErrorUnknownPair,
		},
		{
			name:     "malformed json",
			tickers:  []types.Ticker{yesTicker},
			body:     `{`,
			wantCode: types.ErrorFailedToDecode,
		},
		{
			name:     "more than one ticker is invalid",
			tickers:  []types.Ticker{yesTicker, noTicker},
			body:     market,
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
