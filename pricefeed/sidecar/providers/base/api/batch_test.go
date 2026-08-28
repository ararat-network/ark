package api_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base/api"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

func TestBatchTickers(t *testing.T) {
	tickers := []types.Ticker{"BTCUSD", "ETHUSD", "SOLUSD"}
	tests := []struct {
		name      string
		tickers   []types.Ticker
		batchSize int
		want      [][]types.Ticker
	}{
		{
			name: "empty input",
		},
		{
			name:      "zero keeps one batch",
			tickers:   tickers,
			batchSize: 0,
			want:      [][]types.Ticker{tickers},
		},
		{
			name:      "negative keeps one batch",
			tickers:   tickers,
			batchSize: -1,
			want:      [][]types.Ticker{tickers},
		},
		{
			name:      "chunks in input order",
			tickers:   tickers,
			batchSize: 2,
			want: [][]types.Ticker{
				{"BTCUSD", "ETHUSD"},
				{"SOLUSD"},
			},
		},
		{
			name:      "oversized batch keeps one batch",
			tickers:   tickers,
			batchSize: 10,
			want:      [][]types.Ticker{tickers},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, api.BatchTickers(tt.tickers, tt.batchSize))
		})
	}
}
