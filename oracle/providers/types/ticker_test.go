package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTickerKey(t *testing.T) {
	require.Equal(t, "ATOMUSD", Ticker("atomusd").Key())
	require.Equal(t, "ATOM-USD", Ticker("Atom-Usd").Key())
}

func TestTickersLookup(t *testing.T) {
	tickers := NewTickers("ATOMUSD", "btcusd")

	ticker, ok := tickers.Lookup("atomusd")
	require.True(t, ok)
	require.Equal(t, Ticker("ATOMUSD"), ticker)

	ticker, ok = tickers.Lookup("BTCUSD")
	require.True(t, ok)
	require.Equal(t, Ticker("btcusd"), ticker)

	_, ok = tickers.Lookup("ETHUSD")
	require.False(t, ok)
}

func TestTickersAddReplacesMatchingKey(t *testing.T) {
	tickers := NewTickers("ATOMUSD")

	tickers.Add("atomusd")

	ticker, ok := tickers.Lookup("ATOMUSD")
	require.True(t, ok)
	require.Equal(t, Ticker("atomusd"), ticker)
}

func TestTickersUnchangedResponse(t *testing.T) {
	tickers := NewTickers("ATOMUSD", "BTCUSD")

	response := tickers.UnchangedResponse()

	require.Empty(t, response.Unresolved)
	require.Len(t, response.Resolved, 2)
	for _, ticker := range []Ticker{"ATOMUSD", "BTCUSD"} {
		result, ok := response.Resolved[ticker]
		require.True(t, ok)
		require.True(t, result.Unchanged)
		require.Nil(t, result.Price)
		require.False(t, result.Timestamp.IsZero())
	}
}
