package sidecar_test

import (
	"math/big"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	server "noah/oracle/sidecar"
	oracletypes "noah/oracle/sidecar/types"
	"noah/pkg/encoding"
)

func TestToReqPrices(t *testing.T) {
	tests := []struct {
		name   string
		prices oracletypes.DenomPrices
		want   map[string]math.LegacyDec
	}{
		{
			name:   "empty prices",
			prices: oracletypes.DenomPrices{},
			want:   map[string]math.LegacyDec{},
		},
		{
			name: "multiple prices",
			prices: oracletypes.DenomPrices{
				"uusd": mustParseBigFloat(t, "123.456"),
				"ukrw": mustParseBigFloat(t, "42.25"),
			},
			want: map[string]math.LegacyDec{
				"uusd": math.LegacyMustNewDecFromStr("123.456"),
				"ukrw": math.LegacyMustNewDecFromStr("42.25"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := server.ToReqPrices(tt.prices)

			require.NoError(t, err)
			require.Len(t, got, len(tt.want))
			for ticker, want := range tt.want {
				rate, err := encoding.DecodeLegacyDec(got[ticker])
				require.NoError(t, err)
				require.Equal(t, want, rate)
			}
		})
	}
}

func TestToReqPricesRejectsNilPrice(t *testing.T) {
	got, err := server.ToReqPrices(oracletypes.DenomPrices{
		"uusd": nil,
	})

	require.Nil(t, got)
	require.EqualError(t, err, "nil price for uusd")
}

func mustParseBigFloat(t *testing.T, value string) *big.Float {
	t.Helper()

	price, _, err := big.ParseFloat(value, 10, 256, big.ToNearestEven)
	require.NoError(t, err)
	return price
}
