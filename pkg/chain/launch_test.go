package chain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/chain"
)

func TestBootstrapNoahPerUSDIsAPositiveDecimal(t *testing.T) {
	price, err := math.LegacyNewDecFromStr(chain.BootstrapNoahPerUSD)
	require.NoError(t, err)
	require.True(t, price.IsPositive())
}

func TestFounderSeat(t *testing.T) {
	seat := chain.SeatGrant
	for _, tc := range []struct {
		name  string
		count int
		want  math.Int
	}{
		{"no seats", 0, seat},
		{"alone", 1, seat},
		{"two", 2, seat},
		{"three", 3, seat},
		{"four", 4, seat.MulRaw(3).QuoRaw(2)},
		{"seven", 7, seat.MulRaw(3)},
		{"ten", 10, seat.MulRaw(9).QuoRaw(2)},
		{"thirteen", 13, seat.MulRaw(6)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := chain.FounderSeat(tc.count)
			require.Equal(t, tc.want, got)
			if tc.count > 3 {
				// Half the others' sum is a third of the whole.
				others := seat.MulRaw(int64(tc.count - 1))
				require.Equal(t, others.Add(got), got.MulRaw(3), "the founder holds a third")
			}
		})
	}
}
