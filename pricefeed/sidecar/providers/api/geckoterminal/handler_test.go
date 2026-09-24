package geckoterminal_test

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/api/geckoterminal"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

const (
	usdtAddress = "0xdac17f958d2ee523a2206206994597c13d831ec7"
	usdcAddress = "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48"
)

func TestBatchTickers(t *testing.T) {
	handler := NewHandler()
	tickers := []types.Ticker{usdtAddress, usdcAddress}

	got, err := handler.BatchTickers(tickers, 0)
	require.NoError(t, err)
	require.Equal(t, [][]types.Ticker{tickers}, got)

	got, err = handler.BatchTickers(tickers, 1)
	require.NoError(t, err)
	require.Equal(t, [][]types.Ticker{{usdtAddress}, {usdcAddress}}, got)
}

// TestBatchTickersCapsAtTheEndpointLimit pins the venue limit: a zero or
// oversized batch size still yields requests the endpoint accepts.
func TestBatchTickersCapsAtTheEndpointLimit(t *testing.T) {
	handler := NewHandler()
	tickers := make([]types.Ticker, MaxAddressesPerRequest+1)
	for i := range tickers {
		tickers[i] = types.Ticker(fmt.Sprintf("0x%040x", i))
	}

	for _, batchSize := range []int{0, MaxAddressesPerRequest + 1} {
		got, err := handler.BatchTickers(tickers, batchSize)
		require.NoError(t, err)
		require.Len(t, got, 2)
		require.Len(t, got[0], MaxAddressesPerRequest)
		require.Len(t, got[1], 1)
	}
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
			name:     "single address",
			tickers:  []types.Ticker{usdtAddress},
			wantPath: "/api/v2/simple/networks/eth/token_price/" + usdtAddress,
		},
		{
			name:     "batch joins addresses in the last path segment",
			tickers:  []types.Ticker{usdtAddress, usdcAddress},
			wantPath: "/api/v2/simple/networks/eth/token_price/" + usdtAddress + "," + usdcAddress,
		},
		{
			name:        "empty tickers",
			errContains: "no tickers provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, err := handler.CreateURL(types.Endpoint{URL: ETHURL}, tt.tickers)
			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
				return
			}

			require.NoError(t, err)
			parsed, err := url.Parse(gotURL)
			require.NoError(t, err)
			require.Equal(t, "api.geckoterminal.com", parsed.Host)
			require.Equal(t, tt.wantPath, parsed.Path)
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
			name:    "resolves a token price",
			tickers: []types.Ticker{usdtAddress},
			body: `{"data":{"id":"1","type":"simple_token_price","attributes":{"token_prices":{
				"` + usdtAddress + `":"0.999806870339427"}}}}`,
			wantResolved: map[types.Ticker]string{usdtAddress: "0.999806870339427"},
		},
		{
			// Addresses come back lower-cased whatever the request sent, so
			// the match is case-insensitive.
			name:    "matches addresses case insensitively and ignores extras",
			tickers: []types.Ticker{types.Ticker(strings.ToUpper(usdtAddress))},
			body: `{"data":{"type":"simple_token_price","attributes":{"token_prices":{
				"` + usdtAddress + `":"1.0",
				"` + usdcAddress + `":"0.9999"}}}}`,
			wantResolved: map[types.Ticker]string{types.Ticker(strings.ToUpper(usdtAddress)): "1"},
		},
		{
			name:         "marks a missing address as no response",
			tickers:      []types.Ticker{usdtAddress, usdcAddress},
			body:         `{"data":{"type":"simple_token_price","attributes":{"token_prices":{"` + usdtAddress + `":"1.0"}}}}`,
			wantResolved: map[types.Ticker]string{usdtAddress: "1"},
			wantErrors:   map[types.Ticker]types.ErrorCode{usdcAddress: types.ErrorNoResponse},
		},
		{
			name:         "isolates an unparseable price",
			tickers:      []types.Ticker{usdtAddress, usdcAddress},
			body:         `{"data":{"type":"simple_token_price","attributes":{"token_prices":{"` + usdtAddress + `":"x","` + usdcAddress + `":"1"}}}}`,
			wantResolved: map[types.Ticker]string{usdcAddress: "1"},
			wantErrors:   map[types.Ticker]types.ErrorCode{usdtAddress: types.ErrorFailedToParsePrice},
		},
		{
			name:    "unexpected data type fails every ticker",
			tickers: []types.Ticker{usdtAddress, usdcAddress},
			body:    `{"data":{"type":"pool","attributes":{}}}`,
			wantErrors: map[types.Ticker]types.ErrorCode{
				usdtAddress: types.ErrorInvalidResponse,
				usdcAddress: types.ErrorInvalidResponse,
			},
		},
		{
			name:       "marks malformed json as a decode failure",
			tickers:    []types.Ticker{usdtAddress},
			body:       `{`,
			wantErrors: map[types.Ticker]types.ErrorCode{usdtAddress: types.ErrorFailedToDecode},
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
