package chain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/chain"
)

func TestBootstrapNoahUSDPriceIsAPositiveDecimal(t *testing.T) {
	price, err := math.LegacyNewDecFromStr(chain.BootstrapNoahUSDPrice)
	require.NoError(t, err)
	require.True(t, price.IsPositive())
}
