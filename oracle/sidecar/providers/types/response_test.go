package types_test

import (
	. "ark/oracle/sidecar/providers/types"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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
