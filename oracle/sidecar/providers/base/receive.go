package base

import (
	"context"

	providermetrics "noah/oracle/sidecar/providers/base/metrics"
	"noah/oracle/sidecar/providers/types"
)

// recv owns response-channel reads for one fetch cycle and commits successful
// ticker results into the provider cache.
func (p *Provider) recv(ctx context.Context) {
	p.logger.Debug("starting recv")

	for {
		select {
		case <-ctx.Done():
			p.logger.Debug(
				"finishing recv and closing with request context err",
				"error", ctx.Err(),
			)
			return
		case r, ok := <-p.responseCh:
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

				p.updateData(ctx, ticker, result)

				providermetrics.RecordResponse(
					ctx,
					p.name,
					ticker,
					string(p.transportType),
					types.OK,
				)
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
					string(p.transportType),
					result.Code(),
				)
			}
		}
	}
}

// updateData stores results that are not older than the current data. An unchanged
// result refreshes only the current result's timestamp.
func (p *Provider) updateData(ctx context.Context, ticker types.Ticker, result types.Result) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if ctx.Err() != nil {
		return
	}

	if !result.Unchanged && result.Price == nil {
		p.logger.Debug(
			"resolved result has no price",
			"ticker", ticker,
			"result", result,
		)
		return
	}

	current, ok := p.prices[ticker]
	if !ok {
		// Ignore an unchanged result when no previous value exists.
		if result.Unchanged {
			p.logger.Debug(
				"result is unchanged but no current data",
				"ticker", ticker,
				"result", result,
			)
			return
		}

		p.prices[ticker] = result
		return
	}

	// If the timestamp of the result is less than the current timestamp, then we do not update the data.
	if result.Timestamp.Before(current.Timestamp) {
		p.logger.Debug(
			"result timestamp is before current timestamp",
			"result_timestamp", result.Timestamp,
			"current_timestamp", p.prices[ticker].Timestamp,
			"ticker", ticker,
		)
		return
	}

	if result.Unchanged {
		p.logger.Debug(
			"result is unchanged",
			"ticker", ticker,
			"result", result,
			"updated_timestamp", result.Timestamp.String(),
		)

		current.Timestamp = result.Timestamp
		p.prices[ticker] = current
	} else {
		p.logger.Debug(
			"updating base provider data",
			"ticker", ticker,
			"result", result,
		)
		p.prices[ticker] = result
	}
}
