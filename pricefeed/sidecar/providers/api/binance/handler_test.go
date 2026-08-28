package binance

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

func TestBatchTickers(t *testing.T) {
	tickers := []types.Ticker{"ATOMUSDT", "BTCUSDT", "ETHUSDT"}

	tests := []struct {
		name      string
		batchSize int
		want      [][]types.Ticker
	}{
		{
			name:      "zero keeps all tickers together",
			batchSize: 0,
			want:      [][]types.Ticker{{"ATOMUSDT", "BTCUSDT", "ETHUSDT"}},
		},
		{
			name:      "one creates one request per ticker",
			batchSize: 1,
			want:      [][]types.Ticker{{"ATOMUSDT"}, {"BTCUSDT"}, {"ETHUSDT"}},
		},
		{
			name:      "positive size chunks tickers",
			batchSize: 2,
			want:      [][]types.Ticker{{"ATOMUSDT", "BTCUSDT"}, {"ETHUSDT"}},
		},
	}

	handler := &Handler{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := handler.BatchTickers(tickers, tt.batchSize)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}

	got, err := handler.BatchTickers(nil, 2)
	require.NoError(t, err)
	require.Nil(t, got)
}
