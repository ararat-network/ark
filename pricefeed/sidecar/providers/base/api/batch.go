package api

import (
	"slices"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

// BatchTickers chunks tickers in input order. A non-positive batch size keeps
// all tickers in one batch.
func BatchTickers(tickers []types.Ticker, batchSize int) [][]types.Ticker {
	if len(tickers) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = len(tickers)
	}

	batches := make([][]types.Ticker, 0, (len(tickers)+batchSize-1)/batchSize)
	for batch := range slices.Chunk(tickers, batchSize) {
		batches = append(batches, batch)
	}

	return batches
}
