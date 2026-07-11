package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"

	"cosmossdk.io/log/v2"

	sidecarinternal "ark/oracle/sidecar/internal"
	"ark/oracle/sidecar/providers/base"
	apimetrics "ark/oracle/sidecar/providers/base/api/metrics"
	"ark/oracle/sidecar/providers/types"
)

// Fetcher polls an HTTP API for provider ticker prices and publishes responses
// to the provider receive loop.
type Fetcher struct {
	logger log.Logger
	config Config

	client           *http.Client
	method           string
	headers          map[string]string
	endpointSelector types.EndpointSelector

	dataHandler DataHandler
	limiter     *rate.Limiter
}

// NewFetcher returns an API fetcher using client for HTTP requests and
// dataHandler for provider-specific URL construction and response parsing.
func NewFetcher(cfg Config, client *http.Client, dataHandler DataHandler, opts ...Option) (*Fetcher, error) {
	f := &Fetcher{
		logger:           log.NewNopLogger(),
		config:           cfg,
		dataHandler:      dataHandler,
		client:           client,
		endpointSelector: types.FirstEndpoint,
		method:           http.MethodGet,
	}

	for _, opt := range opts {
		opt(f)
	}

	if f.logger == nil {
		return nil, errors.New("logger is nil")
	}
	if f.client == nil {
		return nil, errors.New("client is nil")
	}
	if f.dataHandler == nil {
		return nil, errors.New("data handler is nil")
	}
	if f.method == "" {
		return nil, errors.New("http request method is empty")
	}
	if f.endpointSelector == nil {
		return nil, errors.New("endpoint selector is nil")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if cfg.RequestsPerSecond > 0 {
		f.limiter = rate.NewLimiter(rate.Limit(cfg.RequestsPerSecond), 1)
	}
	f.logger = f.logger.With("api_fetcher", f.config.Name)

	return f, nil
}

// Run starts one polling loop per ticker batch and publishes batch responses
// until ctx is cancelled.
func (f *Fetcher) Run(ctx context.Context, tickers []types.Ticker, responseCh chan<- types.Response) error {
	if responseCh == nil {
		f.logger.Debug("response channel is nil")
		return errors.New("response channel is nil")
	}
	if len(tickers) == 0 {
		f.logger.Debug("no tickers to query; exiting")
		return nil
	}

	batches, err := f.dataHandler.BatchTickers(tickers, f.config.BatchSize)
	if err != nil {
		return fmt.Errorf("batching API tickers: %w", err)
	}
	if len(batches) == 0 {
		return errors.New("ticker batcher returned no batches")
	}

	f.logger.Debug(
		"starting API fetcher",
		"tickers", len(tickers),
		"batches", len(batches),
		"batch_size", f.config.BatchSize,
		"interval", f.config.Interval,
		"requests_per_second", f.config.RequestsPerSecond,
	)

	group, groupCtx := errgroup.WithContext(ctx)
	for _, subTickers := range batches {
		group.Go(func() error {
			return sidecarinternal.RunRecovering("api batch loop", func() error {
				return f.runBatchLoop(groupCtx, subTickers, responseCh)
			})
		})
	}

	return group.Wait()
}

// Type returns the fetcher type.
func (f *Fetcher) Type() base.TransportType {
	return base.API
}

// Name returns the provider name configured on the fetcher.
func (f *Fetcher) Name() string {
	return f.config.Name
}

// ResponseBufferSize returns one response slot per active API batch loop.
func (f *Fetcher) ResponseBufferSize(tickers []types.Ticker) int {
	if len(tickers) == 0 {
		return 1
	}

	batches, err := f.dataHandler.BatchTickers(tickers, f.config.BatchSize)
	if err != nil || len(batches) == 0 {
		return 1
	}

	return len(batches)
}

// runBatchLoop repeatedly queries one ticker batch until ctx is cancelled or a fatal query error occurs.
func (f *Fetcher) runBatchLoop(ctx context.Context, tickers []types.Ticker, responseCh chan<- types.Response) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		response, err := f.query(ctx, tickers)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			f.logger.Error(
				"API batch query failed",
				"error", err,
				"tickers", len(tickers),
			)

			return fmt.Errorf("api batch query failed: %w", err)
		}

		select {
		case <-ctx.Done():
			f.logger.Debug("context cancelled, stopping write response")
			return ctx.Err()
		case responseCh <- response:
			f.logger.Debug(
				"published API response",
				"resolved", len(response.Resolved),
				"unresolved", len(response.Unresolved),
			)
		}

		timer := time.NewTimer(f.config.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// query performs one HTTP polling attempt for tickers. Provider request failures
// are returned as unresolved responses; lifecycle, local setup, and fatal task
// errors are returned as errors.
func (f *Fetcher) query(ctx context.Context, tickers []types.Ticker) (types.Response, error) {
	endpoint, err := f.endpointSelector(f.config.Endpoints)
	if err != nil {
		return types.Response{}, ErrSelectEndpointWithErr(err)
	}

	// Create the URL for the request.
	url, err := f.dataHandler.CreateURL(endpoint, tickers)
	if err != nil {
		return types.Response{}, ErrCreateURLWithErr(err)
	}

	if f.limiter != nil {
		if err := f.limiter.Wait(ctx); err != nil {
			if ctx.Err() != nil {
				return types.Response{}, ctx.Err()
			}

			return types.Response{}, err
		}
	}

	requestCtx, cancel := context.WithTimeout(ctx, f.config.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, f.method, url, nil)
	if err != nil {
		return types.Response{}, err
	}

	for key, value := range f.headers {
		req.Header.Set(key, value)
	}
	if auth := endpoint.Authentication; auth.Enabled() {
		req.Header.Set(auth.APIKeyHeader, auth.APIKey)
	}

	// Measure only the HTTP request latency; limiter wait and parsing are excluded.
	start := time.Now()
	resp, err := f.client.Do(req)
	apimetrics.RecordRequest(ctx, f.config.Name, time.Since(start), resp, err)
	if err != nil {
		if ctx.Err() != nil {
			return types.Response{}, ctx.Err()
		}

		status := types.ErrorUnknown
		if resp != nil {
			status = types.ErrorCode(resp.StatusCode)
		}

		f.logger.Error(
			"API request failed",
			"error", err,
			"tickers", len(tickers),
		)

		return types.NewErrorResponse(
			tickers,
			types.NewErrorWithCode(
				ErrDoRequestWithErr(err),
				status,
			)), nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		if _, err := io.Copy(io.Discard, resp.Body); err != nil && ctx.Err() == nil {
			f.logger.Debug("failed to drain API response body", "error", err)
		}
	}

	f.logger.Debug(
		"received API response",
		"status_code", resp.StatusCode,
		"tickers", len(tickers),
	)

	var response types.Response
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		response = types.NewErrorResponse(
			tickers,
			types.NewErrorWithCode(
				ErrRateLimit,
				types.ErrorRateLimitExceeded,
			),
		)
	case resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices:
		response = types.NewErrorResponse(
			tickers,
			types.NewErrorWithCode(
				ErrUnexpectedStatusCodeWithCode(resp.StatusCode),
				types.ErrorCode(resp.StatusCode),
			),
		)
	default:
		response = f.dataHandler.ParseResponse(tickers, resp)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		f.logger.Error(
			"received non-success API response",
			"status_code", resp.StatusCode,
			"tickers", len(tickers),
		)
	}
	return response, nil
}
