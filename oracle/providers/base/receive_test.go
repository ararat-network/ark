package base

import (
	"math/big"
	"testing"
	"time"

	"cosmossdk.io/log/v2"
	"github.com/stretchr/testify/require"

	"noah/oracle/providers/types"
)

func TestUpdateDataStoresResultAndGetPricesReturnsDeepCopy(t *testing.T) {
	provider := newTestProvider()
	ticker := types.Ticker("ATOMUSD")

	provider.updateData(ticker, types.NewResult(big.NewFloat(12.34), time.Unix(10, 0).UTC()))

	prices := provider.GetPrices()
	require.Len(t, prices, 1)
	require.Equal(t, 0, prices["uatom"].Price.Cmp(big.NewFloat(12.34)))

	prices["uatom"].Price.SetFloat64(99)

	prices = provider.GetPrices()
	require.Equal(t, 0, prices["uatom"].Price.Cmp(big.NewFloat(12.34)))
}

func TestUpdateDataIgnoresOlderResults(t *testing.T) {
	provider := newTestProvider()
	ticker := types.Ticker("ATOMUSD")
	currentTime := time.Unix(20, 0).UTC()

	provider.updateData(ticker, types.NewResult(big.NewFloat(2), currentTime))
	provider.updateData(ticker, types.NewResult(big.NewFloat(1), time.Unix(10, 0).UTC()))

	prices := provider.GetPrices()
	require.Equal(t, currentTime, prices["uatom"].Timestamp)
	require.Equal(t, 0, prices["uatom"].Price.Cmp(big.NewFloat(2)))
}

func TestUpdateDataIgnoresUnchangedResultWithoutExistingPrice(t *testing.T) {
	provider := newTestProvider()

	provider.updateData(types.Ticker("ATOMUSD"), types.NewUnchangedResult(time.Unix(10, 0).UTC()))

	require.Empty(t, provider.GetPrices())
}

func TestGetPricesSkipsTickersWithoutMarketMapping(t *testing.T) {
	provider := newTestProvider()

	provider.updateData(types.Ticker("BTCUSD"), types.NewResult(big.NewFloat(2), time.Unix(10, 0).UTC()))

	require.Empty(t, provider.GetPrices())
}

func TestUpdateDataRefreshesTimestampForUnchangedResult(t *testing.T) {
	provider := newTestProvider()
	ticker := types.Ticker("ATOMUSD")
	currentTime := time.Unix(10, 0).UTC()
	updatedTime := time.Unix(20, 0).UTC()

	provider.updateData(ticker, types.NewResult(big.NewFloat(2), currentTime))
	provider.updateData(ticker, types.NewUnchangedResult(updatedTime))

	prices := provider.GetPrices()
	require.Equal(t, updatedTime, prices["uatom"].Timestamp)
	require.False(t, prices["uatom"].Unchanged)
	require.Equal(t, 0, prices["uatom"].Price.Cmp(big.NewFloat(2)))
}

func newTestProvider() *Provider {
	return &Provider{
		logger: log.NewNopLogger(),
		config: Config{
			Name: "test",
			Type: API,
			Markets: types.Markets{
				{Denom: "uatom", Symbol: "ATOMUSD"},
			},
		},
		prices: make(map[types.Ticker]types.Result),
	}
}
