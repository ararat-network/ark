package api_test

import (
	"context"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

// listHandler requests a batch as one comma-separated s parameter and
// resolves every requested ticker from a successful answer.
type listHandler struct{}

func (listHandler) BatchTickers(tickers []types.Ticker, _ int) ([][]types.Ticker, error) {
	return [][]types.Ticker{tickers}, nil
}

func (listHandler) CreateURL(endpoint types.Endpoint, tickers []types.Ticker) (string, error) {
	symbols := make([]string, 0, len(tickers))
	for _, ticker := range tickers {
		symbols = append(symbols, string(ticker))
	}
	return endpoint.URL + "?s=" + strings.Join(symbols, ","), nil
}

func (listHandler) ParseResponse(tickers []types.Ticker, _ *http.Response) types.Response {
	resolved := make(map[types.Ticker]types.Result, len(tickers))
	for _, ticker := range tickers {
		resolved[ticker] = types.NewResult(big.NewFloat(1), time.Now().UTC())
	}
	return types.NewResponse(resolved, nil)
}

// venue answers 400 to any request naming a refused symbol, as Binance does,
// and records each request as host and symbol list.
type venue struct {
	mu       sync.Mutex
	refused  map[string]bool
	status   map[string]int
	requests []string
}

func (v *venue) refuse(symbols ...string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.refused = make(map[string]bool, len(symbols))
	for _, symbol := range symbols {
		v.refused[symbol] = true
	}
}

func (v *venue) RoundTrip(req *http.Request) (*http.Response, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	symbols := req.URL.Query().Get("s")
	v.requests = append(v.requests, req.URL.Host+" "+symbols)

	status := http.StatusOK
	if code, ok := v.status[req.URL.Host]; ok {
		status = code
	}
	for _, symbol := range strings.Split(symbols, ",") {
		if v.refused[symbol] {
			status = http.StatusBadRequest
		}
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
}

func (v *venue) recorded() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]string(nil), v.requests...)
}

func refusalConfig() Config {
	cfg := apiConfig()
	cfg.BatchSize = 0
	cfg.Interval = 10 * time.Millisecond
	return cfg
}

// TestRunSetsRefusedTickersAside pins that one symbol the venue refuses no
// longer takes the batch down with it: each ticker is asked for alone once,
// and the batch then runs without the refused one, which stays unresolved.
func TestRunSetsRefusedTickersAside(t *testing.T) {
	v := &venue{}
	v.refuse("BAD")
	fetcher, err := NewFetcher(refusalConfig(), &http.Client{Transport: v}, listHandler{},
		WithRefusedTickerRetry(time.Hour))
	require.NoError(t, err)

	responses, stop := runAPI(t, fetcher, []types.Ticker{"A", "BAD", "C"})
	for range 2 {
		response := receiveResponse(t, responses)
		require.Contains(t, response.Resolved, types.Ticker("A"))
		require.Contains(t, response.Resolved, types.Ticker("C"))
		require.Contains(t, response.Unresolved, types.Ticker("BAD"))
	}
	require.Equal(t, types.ErrorUnknownPair, receiveResponse(t, responses).Unresolved["BAD"].Code())
	require.ErrorIs(t, stop(), context.Canceled)

	want := []string{"provider.test A,BAD,C", "provider.test A", "provider.test BAD", "provider.test C", "provider.test A,C"}
	got := v.recorded()
	require.GreaterOrEqual(t, len(got), len(want))
	require.Equal(t, want, got[:len(want)])
	for _, request := range got[len(want):] {
		require.Equal(t, "provider.test A,C", request)
	}
}

// TestRunRetriesRefusedTickersAlone pins that a set-aside ticker is asked for
// alone again after the retry period and rejoins the batch once answered.
func TestRunRetriesRefusedTickersAlone(t *testing.T) {
	v := &venue{}
	v.refuse("BAD")
	fetcher, err := NewFetcher(refusalConfig(), &http.Client{Transport: v}, listHandler{},
		WithRefusedTickerRetry(30*time.Millisecond))
	require.NoError(t, err)

	responses, stop := runAPI(t, fetcher, []types.Ticker{"A", "BAD", "C"})
	require.Contains(t, receiveResponse(t, responses).Unresolved, types.Ticker("BAD"))
	v.refuse()

	deadline := time.Now().Add(5 * time.Second)
	for receiveResponse(t, responses).Resolved["BAD"].Price == nil {
		require.True(t, time.Now().Before(deadline), "refused ticker never retried")
	}
	receiveResponse(t, responses)
	require.ErrorIs(t, stop(), context.Canceled)
	require.Contains(t, v.recorded(), "provider.test BAD")
	require.Contains(t, v.recorded(), "provider.test A,C,BAD")
}

// TestRunSetsNothingAsideWhenEveryTickerIsRefused pins that a venue refusing
// every ticker alone, which is not one symbol's doing, keeps the batch whole
// and is not isolated again before the retry period.
func TestRunSetsNothingAsideWhenEveryTickerIsRefused(t *testing.T) {
	v := &venue{}
	v.refuse("A", "B")
	fetcher, err := NewFetcher(refusalConfig(), &http.Client{Transport: v}, listHandler{},
		WithRefusedTickerRetry(time.Hour))
	require.NoError(t, err)

	responses, stop := runAPI(t, fetcher, []types.Ticker{"A", "B"})
	for range 2 {
		response := receiveResponse(t, responses)
		require.Empty(t, response.Resolved)
		require.Equal(t, types.ErrorCode(http.StatusBadRequest), response.Unresolved["A"].Code())
	}
	require.ErrorIs(t, stop(), context.Canceled)

	want := []string{"provider.test A,B", "provider.test A", "provider.test B", "provider.test A,B"}
	got := v.recorded()
	require.GreaterOrEqual(t, len(got), len(want))
	require.Equal(t, want, got[:len(want)])
	for _, request := range got[len(want):] {
		require.Equal(t, "provider.test A,B", request)
	}
}

// TestRunFailsOverToTheNextEndpoint pins that a batch stays on an endpoint
// until a request to it fails, and that a refused request is not the
// endpoint's failure.
func TestRunFailsOverToTheNextEndpoint(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   []string
	}{
		{
			name:   "unavailable",
			status: http.StatusServiceUnavailable,
			want:   []string{"first.test A", "second.test A", "second.test A"},
		},
		{
			name:   "refused",
			status: http.StatusBadRequest,
			want:   []string{"first.test A", "first.test A", "first.test A"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &venue{status: map[string]int{"first.test": tt.status}}
			cfg := refusalConfig()
			cfg.Endpoints = []types.Endpoint{{URL: "https://first.test"}, {URL: "https://second.test"}}
			fetcher, err := NewFetcher(cfg, &http.Client{Transport: v}, listHandler{})
			require.NoError(t, err)

			responses, stop := runAPI(t, fetcher, []types.Ticker{"A"})
			for range len(tt.want) {
				receiveResponse(t, responses)
			}
			require.ErrorIs(t, stop(), context.Canceled)
			require.Equal(t, tt.want, v.recorded()[:len(tt.want)])
		})
	}
}
