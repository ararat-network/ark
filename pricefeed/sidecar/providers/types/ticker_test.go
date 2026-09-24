package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
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

func TestTickersAllReturnsEveryTicker(t *testing.T) {
	tickers := NewTickers("ATOMUSD", "btcusd")

	require.ElementsMatch(t, []Ticker{"ATOMUSD", "btcusd"}, tickers.All())

	empty := NewTickers()
	require.Empty(t, empty.All())
}
