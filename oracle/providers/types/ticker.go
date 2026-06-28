package types

import (
	"strings"
	"sync"
	"time"
)

// Ticker identifies a provider-specific market symbol.
type Ticker string

// Key returns the ticker's case-insensitive lookup key.
func (t Ticker) Key() string {
	return strings.ToUpper(string(t))
}

// Tickers is a concurrency-safe collection of provider tickers indexed by key.
type Tickers struct {
	mu    sync.RWMutex
	cache map[string]Ticker
}

// NewTickers returns a collection containing the provided tickers.
func NewTickers(tickers ...Ticker) Tickers {
	cache := make(map[string]Ticker)
	for _, ticker := range tickers {
		cache[ticker.Key()] = ticker
	}
	return Tickers{
		cache: cache,
	}
}

// Add inserts or replaces a ticker with the same key.
func (t *Tickers) Add(ticker Ticker) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.cache[ticker.Key()] = ticker
}

// Lookup returns the ticker matching raw, ignoring case.
func (t *Tickers) Lookup(raw string) (Ticker, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	ticker, ok := t.cache[Ticker(raw).Key()]
	return ticker, ok
}

// UnchangedResponse returns an unchanged result for every registered ticker.
func (t *Tickers) UnchangedResponse() Response {
	t.mu.RLock()
	defer t.mu.RUnlock()

	resolved := make(map[Ticker]Result)
	for _, ticker := range t.cache {
		resolved[ticker] = NewUnchangedResult(time.Now().UTC())
	}

	return NewResponse(resolved, nil)
}
