package api

import (
	"context"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"noah/oracle/providers/base"
	"noah/oracle/providers/types"
)

const testURL = "https://provider.test/prices"

func TestNewFetcherValidatesInputs(t *testing.T) {
	handler := &stubDataHandler{}
	client := &http.Client{}

	tests := []struct {
		name        string
		client      *http.Client
		handler     DataHandler
		opts        []Option
		errContains string
	}{
		{
			name:        "nil client",
			handler:     handler,
			errContains: "client is nil",
		},
		{
			name:        "nil data handler",
			client:      client,
			errContains: "data handler is nil",
		},
		{
			name:        "empty http method",
			client:      client,
			handler:     handler,
			opts:        []Option{WithHTTPMethod("")},
			errContains: "http request method is empty",
		},
		{
			name:    "valid",
			client:  client,
			handler: handler,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fetcher, err := NewFetcher(apiConfig(), tt.client, tt.handler, tt.opts...)
			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
				return
			}

			require.NoError(t, err)
			require.Equal(t, apiConfig().Name, fetcher.Name())
			require.Equal(t, base.API, fetcher.Type())
		})
	}
}

func TestRunRejectsNilResponseChannelAndEmptyTickers(t *testing.T) {
	handler := &stubDataHandler{}

	fetcher, err := NewFetcher(apiConfig(), &http.Client{}, handler)
	require.NoError(t, err)

	require.ErrorContains(t, fetcher.Run(context.Background(), []types.Ticker{"ATOMUSD"}, nil), "response channel is nil")
	require.NoError(t, fetcher.Run(context.Background(), nil, make(chan types.Response)))
}

func TestQueryReturnsErrorWhenEndpointSelectionFails(t *testing.T) {
	tickers := []types.Ticker{"ATOMUSD", "BTCUSD"}

	fetcher, err := NewFetcher(
		apiConfig(),
		&http.Client{},
		&stubDataHandler{},
		WithEndpointSelector(func([]types.Endpoint) (types.Endpoint, error) {
			return types.Endpoint{}, errors.New("no endpoint available")
		}),
	)
	require.NoError(t, err)

	response, err := fetcher.query(context.Background(), tickers)
	require.ErrorIs(t, err, ErrSelectEndpoint)
	require.ErrorContains(t, err, "no endpoint available")
	require.Empty(t, response.Resolved)
	require.Empty(t, response.Unresolved)
}

func TestQueryReturnsErrorWhenCreateURLFails(t *testing.T) {
	tickers := []types.Ticker{"ATOMUSD", "BTCUSD"}
	cfg := apiConfig()
	handler := &stubDataHandler{
		createURL: func(endpoint types.Endpoint, gotTickers []types.Ticker) (string, error) {
			require.Equal(t, cfg.Endpoints[0], endpoint)
			require.Equal(t, tickers, gotTickers)
			return "", errors.New("missing ticker")
		},
	}

	fetcher, err := NewFetcher(cfg, &http.Client{}, handler)
	require.NoError(t, err)

	response, err := fetcher.query(context.Background(), tickers)
	require.ErrorIs(t, err, ErrCreateURL)
	require.ErrorContains(t, err, "missing ticker")
	require.Empty(t, response.Resolved)
	require.Empty(t, response.Unresolved)
}

func TestQuerySendsMethodHeadersAndParsesSuccessfulResponse(t *testing.T) {
	tickers := []types.Ticker{"ATOMUSD"}
	expected := types.NewResponse(map[types.Ticker]types.Result{
		"ATOMUSD": types.NewResult(big.NewFloat(12.34), time.Unix(10, 0).UTC()),
	}, nil)

	cfg := apiConfig()
	handler := &stubDataHandler{
		createURL: func(endpoint types.Endpoint, gotTickers []types.Ticker) (string, error) {
			require.Equal(t, cfg.Endpoints[0], endpoint)
			require.Equal(t, tickers, gotTickers)
			return testURL, nil
		},
		parseResponse: func(gotTickers []types.Ticker, response *http.Response) types.Response {
			require.Equal(t, tickers, gotTickers)
			require.NotNil(t, response)
			return expected
		},
	}

	fetcher, err := NewFetcher(
		cfg,
		&http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				require.Equal(t, http.MethodPost, req.Method)
				require.Equal(t, "secret", req.Header.Get("X-API-Key"))
				return httpResponse(http.StatusOK, `{"ok":true}`), nil
			}),
		},
		handler,
		WithHTTPMethod(http.MethodPost),
		WithHTTPHeaders(map[string]string{"X-API-Key": "secret"}),
	)
	require.NoError(t, err)

	response, err := fetcher.query(context.Background(), tickers)
	require.NoError(t, err)
	require.Equal(t, expected, response)
}

func TestQueryMapsHTTPFailuresToUnresolvedResponses(t *testing.T) {
	tests := []struct {
		name string
		code int
		want types.ErrorCode
	}{
		{
			name: "rate limited",
			code: http.StatusTooManyRequests,
			want: types.ErrorRateLimitExceeded,
		},
		{
			name: "server error",
			code: http.StatusInternalServerError,
			want: types.ErrorCode(http.StatusInternalServerError),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tickers := []types.Ticker{"ATOMUSD"}
			cfg := apiConfig()

			handler := &stubDataHandler{
				createURL: func(endpoint types.Endpoint, gotTickers []types.Ticker) (string, error) {
					require.Equal(t, cfg.Endpoints[0], endpoint)
					require.Equal(t, tickers, gotTickers)
					return testURL, nil
				},
			}

			fetcher, err := NewFetcher(cfg, &http.Client{
				Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return httpResponse(tt.code, ""), nil
				}),
			}, handler)
			require.NoError(t, err)

			response, err := fetcher.query(context.Background(), tickers)
			require.NoError(t, err)
			require.Empty(t, response.Resolved)
			require.Equal(t, tt.want, response.Unresolved["ATOMUSD"].Code())
		})
	}
}

func TestQueryMapsClientErrorToUnresolvedResponse(t *testing.T) {
	tickers := []types.Ticker{"ATOMUSD"}
	cfg := apiConfig()
	handler := &stubDataHandler{
		createURL: func(endpoint types.Endpoint, gotTickers []types.Ticker) (string, error) {
			require.Equal(t, cfg.Endpoints[0], endpoint)
			require.Equal(t, tickers, gotTickers)
			return "https://example.invalid", nil
		},
	}

	fetcher, err := NewFetcher(cfg, &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network unavailable")
		}),
	}, handler)
	require.NoError(t, err)

	response, err := fetcher.query(context.Background(), tickers)
	require.NoError(t, err)
	require.Equal(t, types.ErrorUnknown, response.Unresolved["ATOMUSD"].Code())
}

func TestQueryReturnsContextErrorOnCancellation(t *testing.T) {
	tickers := []types.Ticker{"ATOMUSD"}
	cfg := apiConfig()
	handler := &stubDataHandler{
		createURL: func(endpoint types.Endpoint, gotTickers []types.Ticker) (string, error) {
			require.Equal(t, cfg.Endpoints[0], endpoint)
			require.Equal(t, tickers, gotTickers)
			return "https://example.invalid", nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	fetcher, err := NewFetcher(cfg, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()
			return nil, req.Context().Err()
		}),
	}, handler)
	require.NoError(t, err)

	_, err = fetcher.query(ctx, tickers)
	require.ErrorIs(t, err, context.Canceled)
}

func TestRunPublishesBatchedResponsesAndStopsOnContextCancellation(t *testing.T) {
	tickers := []types.Ticker{"ATOMUSD", "BTCUSD", "ETHUSD"}

	cfg := apiConfig()
	handler := &stubDataHandler{
		createURL: func(endpoint types.Endpoint, gotTickers []types.Ticker) (string, error) {
			require.Equal(t, cfg.Endpoints[0], endpoint)
			switch {
			case len(gotTickers) == 2:
				require.Equal(t, []types.Ticker{"ATOMUSD", "BTCUSD"}, gotTickers)
				return "https://provider.test/batch-1", nil
			case len(gotTickers) == 1:
				require.Equal(t, []types.Ticker{"ETHUSD"}, gotTickers)
				return "https://provider.test/batch-2", nil
			default:
				t.Fatalf("unexpected tickers: %v", gotTickers)
				return "", nil
			}
		},
		parseResponse: func(gotTickers []types.Ticker, _ *http.Response) types.Response {
			switch {
			case len(gotTickers) == 2:
				require.Equal(t, []types.Ticker{"ATOMUSD", "BTCUSD"}, gotTickers)
				return types.NewResponse(map[types.Ticker]types.Result{"ATOMUSD": types.NewResult(big.NewFloat(1), time.Now())}, nil)
			case len(gotTickers) == 1:
				require.Equal(t, []types.Ticker{"ETHUSD"}, gotTickers)
				return types.NewResponse(map[types.Ticker]types.Result{"ETHUSD": types.NewResult(big.NewFloat(2), time.Now())}, nil)
			default:
				t.Fatalf("unexpected tickers: %v", gotTickers)
				return types.Response{}
			}
		},
	}

	cfg.BatchSize = 2
	cfg.Interval = time.Hour

	fetcher, err := NewFetcher(cfg, &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return httpResponse(http.StatusOK, `{}`), nil
		}),
	}, handler)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	responseCh := make(chan types.Response, 2)
	errCh := make(chan error, 1)
	go func() {
		errCh <- fetcher.Run(ctx, tickers, responseCh)
	}()

	require.NotEmpty(t, (<-responseCh).Resolved)
	require.NotEmpty(t, (<-responseCh).Resolved)
	cancel()
	require.ErrorIs(t, <-errCh, context.Canceled)
}

func TestRunUsesConfiguredBatchSize(t *testing.T) {
	tickers := []types.Ticker{"ATOMUSD", "BTCUSD", "ETHUSD"}
	cfg := apiConfig()
	cfg.BatchSize = 2
	cfg.Interval = time.Hour

	batches := make(chan []types.Ticker, 2)
	handler := &stubDataHandler{
		createURL: func(_ types.Endpoint, gotTickers []types.Ticker) (string, error) {
			batches <- append([]types.Ticker(nil), gotTickers...)
			return testURL, nil
		},
		parseResponse: func(gotTickers []types.Ticker, _ *http.Response) types.Response {
			return types.NewResponse(map[types.Ticker]types.Result{
				gotTickers[0]: types.NewResult(big.NewFloat(1), time.Now()),
			}, nil)
		},
	}

	fetcher, err := NewFetcher(cfg, &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return httpResponse(http.StatusOK, `{}`), nil
		}),
	}, handler)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	responseCh := make(chan types.Response, 2)
	errCh := make(chan error, 1)
	go func() {
		errCh <- fetcher.Run(ctx, tickers, responseCh)
	}()

	got := []([]types.Ticker){<-batches, <-batches}
	cancel()
	require.ErrorIs(t, <-errCh, context.Canceled)

	require.ElementsMatch(t, []([]types.Ticker){
		{"ATOMUSD", "BTCUSD"},
		{"ETHUSD"},
	}, got)
}

func TestResponseBufferSizeReturnsBatchCount(t *testing.T) {
	cfg := apiConfig()
	cfg.BatchSize = 2
	fetcher, err := NewFetcher(cfg, &http.Client{}, &stubDataHandler{})
	require.NoError(t, err)

	require.Equal(t, 2, fetcher.ResponseBufferSize([]types.Ticker{"ATOMUSD", "BTCUSD", "ETHUSD"}))
	require.Equal(t, 1, fetcher.ResponseBufferSize(nil))
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func httpResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type stubDataHandler struct {
	createURL     func(types.Endpoint, []types.Ticker) (string, error)
	parseResponse func([]types.Ticker, *http.Response) types.Response
}

func (h *stubDataHandler) CreateURL(endpoint types.Endpoint, tickers []types.Ticker) (string, error) {
	if h.createURL == nil {
		return "", errors.New("unexpected CreateURL call")
	}
	return h.createURL(endpoint, tickers)
}

func (h *stubDataHandler) ParseResponse(tickers []types.Ticker, response *http.Response) types.Response {
	if h.parseResponse == nil {
		return types.Response{}
	}
	return h.parseResponse(tickers, response)
}

func apiConfig() Config {
	return Config{
		Name:      "test",
		BatchSize: 1,
		Interval:  time.Hour,
		Timeout:   time.Second,
		Endpoints: []types.Endpoint{{URL: "https://provider.test"}},
	}
}
