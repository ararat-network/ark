package base

import (
	"context"

	providermetrics "github.com/ararat-network/ark/pricefeed/sidecar/providers/base/metrics"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

// recv owns response-channel reads for one provider run and commits successful
// ticker results into the provider cache.
func (p *Provider) recv(ctx context.Context, responseCh <-chan types.Response) {
	p.logger.Debug("starting recv")

	for {
		select {
		case <-ctx.Done():
			p.logger.Debug(
				"finishing recv and closing with request context err",
				"error", ctx.Err(),
			)
			return
		case r, ok := <-responseCh:
			if !ok {
				p.logger.Debug("response channel closed; stopping recv")
				return
			}

			resolved, unresolved := r.Resolved, r.Unresolved

			for ticker, result := range resolved {
				p.logger.Debug(
					"successfully fetched data",
					"ticker", ticker,
					"result", result,
				)

				// A result the cache refuses is counted under its reason, so
				// a dropped stream never reads as a success.
				if code, ok := p.updateData(ctx, ticker, result); ok {
					providermetrics.RecordResponse(ctx, p.name, ticker, code)
				}
			}

			// Log and record all the unresolved data.
			for ticker, result := range unresolved {
				p.logger.Debug(
					"failed to fetch data",
					"ticker", ticker,
					"error", result,
				)

				providermetrics.RecordResponse(
					ctx,
					p.name,
					ticker,
					result.Code(),
				)
			}
		}
	}
}

// updateData caches result and returns the code to record for it: OK when
// stored, otherwise why not. It reports false for a ticker outside the
// configured markets or a cancelled run, which record nothing. An unchanged
// result refreshes only the current result's timestamp. Arrival order is the
// only order: each ticker has one in-order producer, and a wall-clock step
// back must not freeze the cache.
func (p *Provider) updateData(ctx context.Context, ticker types.Ticker, result types.Result) (types.ErrorCode, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if ctx.Err() != nil {
		return 0, false
	}

	pair, ok := p.markets.TickerToPair(ticker)
	if !ok {
		p.logger.Debug(
			"resolved result is not for a configured market",
			"ticker", ticker,
			"result", result,
		)
		return 0, false
	}

	if !result.Unchanged && result.Price == nil {
		p.logger.Debug(
			"resolved result has no price",
			"ticker", ticker,
			"result", result,
		)
		return types.ErrorInvalidResponse, true
	}

	current, ok := p.prices[pair]
	if result.Unchanged {
		if !ok {
			p.logger.Debug(
				"result is unchanged but no current data",
				"ticker", ticker,
				"result", result,
			)
			return types.ErrorNoExistingPrice, true
		}

		// Timestamp moves; LastObserved does not. The heartbeat says the
		// price still holds, not that it was seen again.
		current.Timestamp = result.Timestamp
		p.prices[pair] = current
		return types.OK, true
	}

	// A price is never observed after it arrived, and a handler that set no
	// observation time observed it on arrival.
	if result.LastObserved.IsZero() || result.LastObserved.After(result.Timestamp) {
		result.LastObserved = result.Timestamp
	}
	p.prices[pair] = result
	return types.OK, true
}
