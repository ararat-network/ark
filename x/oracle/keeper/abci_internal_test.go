package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"noah/x/oracle/types"
)

func TestSameTobinTaxes(t *testing.T) {
	tobinTax := math.LegacyNewDecWithPrec(25, 4)

	tests := []struct {
		name     string
		stored   map[string]math.LegacyDec
		params   types.TobinTaxes
		expected bool
	}{
		{
			name: "same tobin taxes",
			stored: map[string]math.LegacyDec{
				"uusd": tobinTax,
				"ukrw": tobinTax,
			},
			params: types.TobinTaxes{
				{Denom: "uusd", TobinTax: tobinTax},
				{Denom: "ukrw", TobinTax: tobinTax},
			},
			expected: true,
		},
		{
			name: "missing denom",
			stored: map[string]math.LegacyDec{
				"uusd": tobinTax,
			},
			params: types.TobinTaxes{
				{Denom: "uusd", TobinTax: tobinTax},
				{Denom: "ukrw", TobinTax: tobinTax},
			},
		},
		{
			name: "extra denom",
			stored: map[string]math.LegacyDec{
				"uusd": tobinTax,
				"ukrw": tobinTax,
			},
			params: types.TobinTaxes{
				{Denom: "uusd", TobinTax: tobinTax},
			},
		},
		{
			name: "changed tax",
			stored: map[string]math.LegacyDec{
				"uusd": tobinTax,
			},
			params: types.TobinTaxes{
				{Denom: "uusd", TobinTax: math.LegacyNewDecWithPrec(50, 4)},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, sameTobinTaxes(tc.stored, tc.params))
		})
	}
}
