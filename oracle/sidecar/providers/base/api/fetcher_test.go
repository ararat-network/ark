package api_test

import (
	"context"
	"errors"
	"io"
	"math/big"
	"net/http"
	. "noah/oracle/sidecar/providers/base/api"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	sidecarinternal "noah/oracle/sidecar/internal"
	"noah/oracle/sidecar/providers/base"
	apitestutil "noah/oracle/sidecar/providers/base/api/testutil"
	"noah/oracle/sidecar/providers/types"
)

const testURL = "https://provider.test/prices"

func TestNewFetcherValidatesInputs(t *testing.T) {
	handler := newMockDataHandler(t)
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
	handler := newMockDataHandler(t)

	fetcher, err := NewFetcher(apiConfig(), &http.Client{}, handler)
	require.NoError(t, err)

	require.ErrorContains(t, fetcher.Run(context.Background(), []types.Ticker{"ATOMUSD"}, nil), "response channel is nil")
	require.NoError(t, fetcher.Run(context.Background(), nil, make(chan types.Response)))
}

func TestRunReturnsErrorWhenEndpointSelectionFails(t *testing.T) {
	tickers := []types.Ticker{"ATOMUSD", "BTCUSD"}

	fetcher, err := NewFetcher(
		apiConfig(),
		&http.Client{},
		newMockDataHandler(t),
		WithEndpointSelector(func([]types.Endpoint) (types.Endpoint, error) {
			return types.Endpoint{}, errors.New("no endpoint available")
		}),
	)
	require.NoError(t, err)

	err = fetcher.Run(context.Background(), tickers, make(chan types.Response, 1))
	require.ErrorIs(t, err, ErrSelectEndpoint)
	require.ErrorContains(t, err, "no endpoint available")
}

func TestRunReturnsErrorWhenCreateURLFails(t *testing.T) {
	tickers := []types.Ticker{"ATOMUSD", "BTCUSD"}
	cfg := apiConfig()
	cfg.BatchSize = len(tickers)
	handler := newMockDataHandler(t)
	handler.EXPECT().
		CreateURL(cfg.Endpoints[0], tickers).
		Return("", errors.New("missing ticker"))

	fetcher, err := NewFetcher(cfg, &http.Client{}, handler)
	require.NoError(t, err)

	err = fetcher.Run(context.Background(), tickers, make(chan types.Response, 1))
	require.ErrorIs(t, err, ErrCreateURL)
	require.ErrorContains(t, err, "missing ticker")
}

func TestRunReturnsErrorWhenBatchLoopPanics(t *testing.T) {
	tickers := []types.Ticker{"ATOMUSD"}
	cfg := apiConfig()
	handler := newMockDataHandler(t)
	handler.EXPECT().
		CreateURL(cfg.Endpoints[0], tickers).
		DoAndReturn(func(types.Endpoint, []types.Ticker) (string, error) {
			panic("boom")
		})

	fetcher, err := NewFetcher(cfg, &http.Client{}, handler)
	require.NoError(t, err)

	err = fetcher.Run(context.Background(), tickers, make(chan types.Response, 1))
	require.ErrorContains(t, err, "api batch loop panicked")
	require.ErrorContains(t, err, "boom")
	require.True(t, sidecarinternal.IsPanic(err))
}

func TestRunSendsMethodHeadersAndParsesSuccessfulResponse(t *testing.T) {
	tickers := []types.Ticker{"ATOMUSD"}
	expected := types.NewResponse(map[types.Ticker]types.Result{
		"ATOMUSD": types.NewResult(big.NewFloat(12.34), time.Unix(10, 0).UTC()),
	}, nil)

	cfg := apiConfig()
	handler := newMockDataHandler(t)
	handler.EXPECT().
		CreateURL(cfg.Endpoints[0], tickers).
		Return(testURL, nil)
	handler.EXPECT().
		ParseResponse(tickers, gomock.Any()).
		DoAndReturn(func(gotTickers []types.Ticker, response *http.Response) types.Response {
			require.Equal(t, tickers, gotTickers)
			require.NotNil(t, response)
			return expected
		})

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

	response, err := runAPIOnce(fetcher, tickers)
	require.NoError(t, err)
	require.Equal(t, expected, response)
}

func TestRunAppliesSelectedEndpointAuthenticationAtRequestTime(t *testing.T) {
	tickers := []types.Ticker{"ATOMUSD"}
	expected := types.NewResponse(map[types.Ticker]types.Result{
		"ATOMUSD": types.NewResult(big.NewFloat(12.34), time.Unix(10, 0).UTC()),
	}, nil)

	cfg := apiConfig()
	cfg.Endpoints = []types.Endpoint{
		{
			URL: "https://first.provider.test",
			Authentication: types.Authentication{
				APIKeyHeader: "X-First-Key",
				APIKey:       "first-secret",
			},
		},
		{
			URL: "https://second.provider.test",
			Authentication: types.Authentication{
				APIKeyHeader: "X-Second-Key",
				APIKey:       "second-secret",
			},
		},
	}
	selectedEndpoint := cfg.Endpoints[1]

	handler := newMockDataHandler(t)
	handler.EXPECT().
		CreateURL(selectedEndpoint, tickers).
		Return(testURL, nil)
	handler.EXPECT().
		ParseResponse(tickers, gomock.Any()).
		Return(expected)

	fetcher, err := NewFetcher(
		cfg,
		&http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				require.Empty(t, req.Header.Get("X-First-Key"))
				require.Equal(t, "second-secret", req.Header.Get("X-Second-Key"))
				return httpResponse(http.StatusOK, `{"ok":true}`), nil
			}),
		},
		handler,
		WithEndpointSelector(func(endpoints []types.Endpoint) (types.Endpoint, error) {
			return endpoints[1], nil
		}),
	)
	require.NoError(t, err)

	response, err := runAPIOnce(fetcher, tickers)
	require.NoError(t, err)
	require.Equal(t, expected, response)
}

func TestRunMapsHTTPFailuresToUnresolvedResponses(t *testing.T) {
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

			handler := newMockDataHandler(t)
			handler.EXPECT().
				CreateURL(cfg.Endpoints[0], tickers).
				Return(testURL, nil)

			fetcher, err := NewFetcher(cfg, &http.Client{
				Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return httpResponse(tt.code, ""), nil
				}),
			}, handler)
			require.NoError(t, err)

			response, err := runAPIOnce(fetcher, tickers)
			require.NoError(t, err)
			require.Empty(t, response.Resolved)
			require.Equal(t, tt.want, response.Unresolved["ATOMUSD"].Code())
		})
	}
}

func TestRunMapsClientErrorToUnresolvedResponse(t *testing.T) {
	tickers := []types.Ticker{"ATOMUSD"}
	cfg := apiConfig()
	handler := newMockDataHandler(t)
	handler.EXPECT().
		CreateURL(cfg.Endpoints[0], tickers).
		Return("https://example.invalid", nil)

	fetcher, err := NewFetcher(cfg, &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network unavailable")
		}),
	}, handler)
	require.NoError(t, err)

	response, err := runAPIOnce(fetcher, tickers)
	require.NoError(t, err)
	require.Equal(t, types.ErrorUnknown, response.Unresolved["ATOMUSD"].Code())
}

func TestRunReturnsContextErrorOnCancellation(t *testing.T) {
	tickers := []types.Ticker{"ATOMUSD"}
	cfg := apiConfig()
	cfg.Timeout = time.Hour
	handler := newMockDataHandler(t)
	handler.EXPECT().
		CreateURL(cfg.Endpoints[0], tickers).
		Return("https://example.invalid", nil)

	requestStarted := make(chan struct{})

	fetcher, err := NewFetcher(cfg, &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			close(requestStarted)
			<-req.Context().Done()
			return nil, req.Context().Err()
		}),
	}, handler)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- fetcher.Run(ctx, tickers, make(chan types.Response, 1))
	}()
	requireSignal(t, requestStarted, "request did not start")
	cancel()
	require.ErrorIs(t, <-errCh, context.Canceled)
}

func TestRunPublishesBatchedResponsesAndStopsOnContextCancellation(t *testing.T) {
	tickers := []types.Ticker{"ATOMUSD", "BTCUSD", "ETHUSD"}

	cfg := apiConfig()
	handler := newMockDataHandler(t)
	handler.EXPECT().
		CreateURL(cfg.Endpoints[0], gomock.Any()).
		DoAndReturn(func(endpoint types.Endpoint, gotTickers []types.Ticker) (string, error) {
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
		}).
		Times(2)
	handler.EXPECT().
		ParseResponse(gomock.Any(), gomock.Any()).
		DoAndReturn(func(gotTickers []types.Ticker, _ *http.Response) types.Response {
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
		}).
		Times(2)

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
	handler := newMockDataHandler(t)
	handler.EXPECT().
		CreateURL(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ types.Endpoint, gotTickers []types.Ticker) (string, error) {
			batches <- append([]types.Ticker(nil), gotTickers...)
			return testURL, nil
		}).
		Times(2)
	handler.EXPECT().
		ParseResponse(gomock.Any(), gomock.Any()).
		DoAndReturn(func(gotTickers []types.Ticker, _ *http.Response) types.Response {
			return types.NewResponse(map[types.Ticker]types.Result{
				gotTickers[0]: types.NewResult(big.NewFloat(1), time.Now()),
			}, nil)
		}).
		Times(2)

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
	fetcher, err := NewFetcher(cfg, &http.Client{}, newMockDataHandler(t))
	require.NoError(t, err)

	require.Equal(t, 2, fetcher.ResponseBufferSize([]types.Ticker{"ATOMUSD", "BTCUSD", "ETHUSD"}))
	require.Equal(t, 1, fetcher.ResponseBufferSize(nil))
}

func runAPIOnce(fetcher interface {
	Run(context.Context, []types.Ticker, chan<- types.Response) error
}, tickers []types.Ticker) (types.Response, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	responseCh := make(chan types.Response, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- fetcher.Run(ctx, tickers, responseCh)
	}()

	select {
	case response := <-responseCh:
		cancel()
		select {
		case err := <-errCh:
			if err != nil && !errors.Is(err, context.Canceled) {
				return types.Response{}, err
			}
		case <-time.After(time.Second):
			return types.Response{}, errors.New("timed out waiting for API fetcher shutdown")
		}
		return response, nil
	case err := <-errCh:
		return types.Response{}, err
	case <-time.After(time.Second):
		return types.Response{}, errors.New("timed out waiting for API response")
	}
}

func requireSignal(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(time.Second):
		require.Fail(t, message)
	}
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

func newMockDataHandler(t *testing.T) *apitestutil.MockDataHandler {
	t.Helper()

	return apitestutil.NewMockDataHandler(gomock.NewController(t))
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
