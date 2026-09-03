package types

import (
	"fmt"
	"math/big"
	"time"
)

// Response contains resolved and unresolved ticker results.
type Response struct {
	// Resolved contains successful or unchanged results keyed by provider ticker.
	Resolved map[Ticker]Result
	// Unresolved contains classified failures keyed by provider ticker.
	Unresolved map[Ticker]ErrorWithCode
}

// NewResponse returns a response with non-nil result maps.
func NewResponse(resolved map[Ticker]Result, unresolved map[Ticker]ErrorWithCode) Response {
	if resolved == nil {
		resolved = make(map[Ticker]Result)
	}

	if unresolved == nil {
		unresolved = make(map[Ticker]ErrorWithCode)
	}

	return Response{
		Resolved:   resolved,
		Unresolved: unresolved,
	}
}

// NewErrorResponse returns a response marking each ticker as unresolved.
func NewErrorResponse(tickers []Ticker, err ErrorWithCode) Response {
	unresolved := make(map[Ticker]ErrorWithCode, len(tickers))
	for _, id := range tickers {
		unresolved[id] = err
	}

	return Response{
		Resolved:   make(map[Ticker]Result),
		Unresolved: unresolved,
	}
}

// Empty returns true if the response contains no resolved or unresolved tickers.
func (r Response) Empty() bool {
	return len(r.Resolved) == 0 && len(r.Unresolved) == 0
}

// String returns a human-readable representation of the response.
func (r Response) String() string {
	return fmt.Sprintf(
		"resolved: %v | unresolved: %v",
		r.Resolved,
		r.Unresolved,
	)
}

// Result contains the outcome for a resolved ticker.
type Result struct {
	// Price is the reported ticker price. It is nil when Unchanged is true.
	Price *big.Float
	// Timestamp is when the result was observed, or for an unchanged result,
	// when the provider last confirmed the price still held.
	Timestamp time.Time
	// LastObserved is when the price itself was last reported. An unchanged
	// result refreshes Timestamp and leaves this alone, which is what lets
	// freshness bound how long a heartbeat may carry a price.
	LastObserved time.Time
	// Unchanged indicates that the previously reported price remains valid.
	Unchanged bool
}

// NewResult returns a result containing a reported price.
func NewResult(price *big.Float, timestamp time.Time) Result {
	return Result{
		Price:        price,
		Timestamp:    timestamp,
		LastObserved: timestamp,
	}
}

// NewUnchangedResult returns a result indicating that the previous price remains valid.
func NewUnchangedResult(timestamp time.Time) Result {
	return Result{
		Timestamp: timestamp,
		Unchanged: true,
	}
}

// String returns a human-readable representation of the result.
func (r Result) String() string {
	price := "<nil>"
	if r.Price != nil {
		price = r.Price.String()
	}

	return fmt.Sprintf(
		"(price: %s, timestamp: %s, last observed: %s, unchanged: %t)",
		price,
		r.Timestamp.String(),
		r.LastObserved.String(),
		r.Unchanged,
	)
}
