package types_test

import (
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

func TestNewResponseInitialisesNilMaps(t *testing.T) {
	response := NewResponse(nil, nil)

	require.NotNil(t, response.Resolved)
	require.NotNil(t, response.Unresolved)
	require.Empty(t, response.Resolved)
	require.Empty(t, response.Unresolved)
}

func TestNewResponsePreservesProvidedMaps(t *testing.T) {
	timestamp := time.Unix(10, 0).UTC()
	resolved := map[Ticker]Result{
		"ATOMUSD": NewResult(big.NewFloat(12.34), timestamp),
	}
	unresolved := map[Ticker]ErrorWithCode{
		"BTCUSD": NewErrorWithCode(errors.New("missing"), ErrorNoResponse),
	}

	response := NewResponse(resolved, unresolved)

	response.Resolved["ETHUSD"] = NewResult(big.NewFloat(23.45), timestamp)
	response.Unresolved["SOLUSD"] = NewErrorWithCode(errors.New("bad response"), ErrorInvalidResponse)

	require.Contains(t, resolved, Ticker("ETHUSD"))
	require.Contains(t, unresolved, Ticker("SOLUSD"))
}

func TestNewErrorResponse(t *testing.T) {
	tickers := []Ticker{"ATOMUSD", "BTCUSD"}
	err := NewErrorWithCode(errors.New("request failed"), ErrorAPIGeneral)

	response := NewErrorResponse(tickers, err)

	require.Empty(t, response.Resolved)
	require.Len(t, response.Unresolved, len(tickers))
	for _, ticker := range tickers {
		require.Equal(t, ErrorAPIGeneral, response.Unresolved[ticker].Code())
		require.Equal(t, "request failed", response.Unresolved[ticker].Error())
	}
}

func TestResultStringHandlesNilPrice(t *testing.T) {
	result := NewUnchangedResult(time.Unix(10, 0).UTC())

	require.Contains(t, result.String(), "price: <nil>")
	require.Contains(t, result.String(), "unchanged: true")
}

func TestNewResultRecordsLastObserved(t *testing.T) {
	observed := time.Unix(10, 0).UTC()

	result := NewResult(big.NewFloat(2), observed)

	require.True(t, result.Timestamp.Equal(observed))
	require.True(t, result.LastObserved.Equal(observed))
	require.False(t, result.Unchanged)
}

// An unchanged result carries no observation of its own: the LastObserved it
// leaves zero is the current result's to keep.
func TestNewUnchangedResultLeavesLastObservedZero(t *testing.T) {
	result := NewUnchangedResult(time.Unix(20, 0).UTC())

	require.True(t, result.Unchanged)
	require.Nil(t, result.Price)
	require.True(t, result.LastObserved.IsZero())
}
