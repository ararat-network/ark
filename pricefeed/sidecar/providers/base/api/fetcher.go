package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"

	"cosmossdk.io/log/v2"

	sidecarinternal "github.com/ararat-network/ark/pricefeed/sidecar/internal"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base"
	apimetrics "github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api/metrics"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

// MaxResponseBodyBytes bounds one provider HTTP response before JSON decoding.
// A 1 MiB ceiling retains at least 5x headroom over the maximum supported
// 512-ticker Binance and Frankfurter response shapes.
const MaxResponseBodyBytes int64 = 1 << 20

// RefusedTickerRetry is how often a batch that set refused tickers aside asks
// for each of them alone again, and the least time between isolations.
const RefusedTickerRetry = 5 * time.Minute

// Fetcher polls an HTTP API for provider ticker prices and publishes responses
// to the provider receive loop.
type Fetcher struct {
	logger log.Logger
	config Config

	client           *http.Client
	method           string
	headers          map[string]string
	endpointSelector types.EndpointSelector
	// refusedRetry is how long a batch waits before it isolates refused
	// tickers again or retries the ones it set aside.
	refusedRetry time.Duration

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
		endpointSelector: types.RotateEndpoints,
		refusedRetry:     RefusedTickerRetry,
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
	if f.refusedRetry <= 0 {
		return nil, errors.New("refused ticker retry must be greater than 0")
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

// runBatchLoop repeatedly queries one ticker batch until ctx is cancelled. A batch that cannot be
// queried stays unresolved and is retried next interval, so the provider's other batches keep
// running and a cause that clears lets this one recover.
func (f *Fetcher) runBatchLoop(ctx context.Context, tickers []types.Ticker, responseCh chan<- types.Response) error {
	b := &batch{active: tickers}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		response := f.poll(ctx, b)
		if ctx.Err() != nil {
			return ctx.Err()
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

// batch is one batch loop's polling state.
type batch struct {
	// active is what each interval requests together.
	active []types.Ticker
	// refused is what the venue refused alone while it answered the rest;
	// it is published unresolved without being requested.
	refused []types.Ticker
	// failures counts requests the endpoint failed, which selects the
	// endpoint: a batch stays on one until it fails.
	failures int
	// nextIsolation is the earliest time to isolate again or retry refused.
	nextIsolation time.Time
}

// poll requests the active tickers. When a venue refuses a whole batch, as
// Binance and Kraken do over one unknown symbol, each ticker is asked for
// alone, and those refused alone are set aside so the rest resolve. Set-aside
// tickers are retried alone every refusedRetry and rejoin once answered.
func (f *Fetcher) poll(ctx context.Context, b *batch) types.Response {
	now := time.Now()
	response := f.fetch(ctx, b, b.active)
	isolate := !now.Before(b.nextIsolation)

	switch {
	case ctx.Err() != nil:
		return response
	case isolate && len(b.active) > 1 && refusedWhole(response, b.active):
		b.nextIsolation = now.Add(f.refusedRetry)
		kept, refused, isolated := f.isolate(ctx, b, b.active)
		response = isolated
		// Refused alone as well as together is not one symbol's doing.
		if len(refused) > 0 && len(kept) > 0 {
			b.active = kept
			b.refused = append(b.refused, refused...)
			f.logger.Warn("venue refused tickers alone; requesting the batch without them", "tickers", refused)
		}
	case isolate && len(b.refused) > 0:
		b.nextIsolation = now.Add(f.refusedRetry)
		answered, refused, retried := f.isolate(ctx, b, b.refused)
		mergeResponse(response, retried)
		b.refused = refused
		if len(answered) > 0 {
			b.active = append(b.active, answered...)
			f.logger.Info("venue answers tickers it refused; requesting them again", "tickers", answered)
		}
	}

	for _, ticker := range b.refused {
		if _, ok := response.Unresolved[ticker]; !ok {
			response.Unresolved[ticker] = types.NewErrorWithCode(
				errors.New("refused by the venue; retried alone periodically"),
				types.ErrorUnknownPair,
			)
		}
	}

	return response
}

// isolate requests each ticker alone. A ticker the venue refuses alone is
// refused; any other outcome, a failure included, keeps it with the batch.
func (f *Fetcher) isolate(
	ctx context.Context,
	b *batch,
	tickers []types.Ticker,
) (kept, refused []types.Ticker, response types.Response) {
	response = types.NewResponse(nil, nil)
	for _, ticker := range tickers {
		if ctx.Err() != nil {
			return kept, refused, response
		}
		alone := []types.Ticker{ticker}
		single := f.fetch(ctx, b, alone)
		mergeResponse(response, single)
		if refusedWhole(single, alone) {
			refused = append(refused, ticker)
		} else {
			kept = append(kept, ticker)
		}
	}

	return kept, refused, response
}

// fetch performs one request for tickers and always returns a response: a
// request that cannot be made, or that fails, publishes them unresolved. A
// request the endpoint failed moves the batch to the next endpoint.
func (f *Fetcher) fetch(ctx context.Context, b *batch, tickers []types.Ticker) types.Response {
	response, endpointFailed, err := f.query(ctx, b.failures, tickers)
	if endpointFailed {
		b.failures++
	}
	if err == nil {
		// poll adds to the maps, which a handler may have left nil.
		return types.NewResponse(response.Resolved, response.Unresolved)
	}
	if ctx.Err() == nil {
		f.logger.Error(
			"API batch query failed",
			"error", err,
			"tickers", len(tickers),
		)
	}

	code := types.ErrorAPIGeneral
	if errors.Is(err, ErrCreateURL) {
		code = types.ErrorUnableToCreateURL
	}
	return types.NewErrorResponse(tickers, types.NewErrorWithCode(err, code))
}

// refusedWhole reports whether the venue refused every ticker as a request it
// will not answer, rather than failing to answer it: nothing resolved, and
// every ticker carries a refusal.
func refusedWhole(response types.Response, tickers []types.Ticker) bool {
	if len(response.Resolved) != 0 || len(tickers) == 0 {
		return false
	}
	for _, ticker := range tickers {
		result, ok := response.Unresolved[ticker]
		if !ok || !isRefusal(result.Code()) {
			return false
		}
	}

	return true
}

// isRefusal reports whether code says the venue refused the request itself:
// an unknown symbol reported in the body, or the HTTP statuses venues answer
// one with. A failed endpoint is not a refusal.
func isRefusal(code types.ErrorCode) bool {
	switch code {
	case types.ErrorUnknownPair,
		types.ErrorCode(http.StatusBadRequest),
		types.ErrorCode(http.StatusNotFound),
		types.ErrorCode(http.StatusUnprocessableEntity):
		return true
	default:
		return false
	}
}

// mergeResponse adds from's results to into.
func mergeResponse(into, from types.Response) {
	for ticker, result := range from.Resolved {
		into.Resolved[ticker] = result
		delete(into.Unresolved, ticker)
	}
	for ticker, result := range from.Unresolved {
		if _, ok := into.Resolved[ticker]; !ok {
			into.Unresolved[ticker] = result
		}
	}
}

// query performs one HTTP polling attempt for tickers on the endpoint selected for attempt.
// Provider request failures are returned as unresolved responses, with endpointFailed set when the
// endpoint rather than the request was at fault; cancellation and local setup failures are
// returned as errors.
func (f *Fetcher) query(
	ctx context.Context,
	attempt int,
	tickers []types.Ticker,
) (_ types.Response, endpointFailed bool, _ error) {
	endpoint, err := f.endpointSelector(f.config.Endpoints, attempt)
	if err != nil {
		return types.Response{}, false, ErrSelectEndpointWithErr(err)
	}

	// Create the URL for the request.
	url, err := f.dataHandler.CreateURL(endpoint, tickers)
	if err != nil {
		return types.Response{}, false, ErrCreateURLWithErr(err)
	}

	if f.limiter != nil {
		if err := f.limiter.Wait(ctx); err != nil {
			if ctx.Err() != nil {
				return types.Response{}, false, ctx.Err()
			}

			return types.Response{}, false, err
		}
	}

	requestCtx, cancel := context.WithTimeout(ctx, f.config.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, f.method, url, nil)
	if err != nil {
		return types.Response{}, false, err
	}

	// A handler may add path segments and query parameters, but must not
	// change the destination before endpoint credentials are attached.
	origin, err := neturl.Parse(endpoint.URL)
	if err != nil || req.URL.Scheme != "https" || origin.Scheme != req.URL.Scheme ||
		!strings.EqualFold(req.URL.Host, origin.Host) || req.URL.User != nil || req.URL.Fragment != "" {
		return types.Response{}, false, errors.New("provider request must retain its configured HTTPS origin")
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
			return types.Response{}, false, ctx.Err()
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
			)), true, nil
	}
	responseBody := resp.Body
	defer responseBody.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		if _, err := io.CopyN(io.Discard, responseBody, MaxResponseBodyBytes+1); err != nil &&
			!errors.Is(err, io.EOF) && ctx.Err() == nil {
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
		body, readErr := io.ReadAll(io.LimitReader(responseBody, MaxResponseBodyBytes+1))
		switch {
		case readErr != nil:
			response = types.NewErrorResponse(
				tickers,
				types.NewErrorWithCode(
					fmt.Errorf("reading API response body: %w", readErr),
					types.ErrorFailedToDecode,
				),
			)
		case int64(len(body)) > MaxResponseBodyBytes:
			response = types.NewErrorResponse(
				tickers,
				types.NewErrorWithCode(
					fmt.Errorf(
						"API response body size %d exceeds maximum %d",
						len(body),
						MaxResponseBodyBytes,
					),
					types.ErrorFailedToDecode,
				),
			)
		default:
			resp.Body = io.NopCloser(bytes.NewReader(body))
			response = f.dataHandler.ParseResponse(tickers, resp)
		}
	}

	failed := resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices
	if failed {
		f.logger.Error(
			"received non-success API response",
			"status_code", resp.StatusCode,
			"tickers", len(tickers),
		)
	}
	return response, failed && !isRefusal(types.ErrorCode(resp.StatusCode)), nil
}
