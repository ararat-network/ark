package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"noah/x/oracle/types"
)

func TestParseExchangeRateTuples(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectedTuples types.ExchangeRates
		expectErr      string
	}{
		{
			name:  "single denom",
			input: "123.0uluna",
			expectedTuples: types.ExchangeRates{
				{Denom: "uluna", Rate: math.LegacyNewDecWithPrec(1230, 1)},
			},
		},
		{
			name:  "multiple denoms",
			input: "123.0uluna,123.123ukrw",
			expectedTuples: types.ExchangeRates{
				{Denom: "uluna", Rate: math.LegacyNewDecWithPrec(1230, 1)},
				{Denom: "ukrw", Rate: math.LegacyMustNewDecFromStr("123.123")},
			},
		},
		{
			name:  "abstain vote (zero rate)",
			input: "0.0uluna,123.1ukrw",
			expectedTuples: types.ExchangeRates{
				{Denom: "uluna", Rate: math.LegacyZeroDec()},
				{Denom: "ukrw", Rate: math.LegacyMustNewDecFromStr("123.1")},
			},
		},
		{
			name:           "empty string",
			input:          "",
			expectedTuples: nil,
		},
		{
			name:           "whitespace only",
			input:          "   ",
			expectedTuples: nil,
		},
		{
			name:      "duplicate denom",
			input:     "100.0uluna,123.123ukrw,121233.123ukrw",
			expectErr: "duplicated denom",
		},
		{
			name:      "missing denom",
			input:     "123.123",
			expectErr: "invalid decimal coin",
		},
		{
			name:      "valid then missing denom",
			input:     "123.0uluna,123.1",
			expectErr: "invalid decimal coin",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tuples, err := types.ParseExchangeRates(tc.input)
			if tc.expectErr != "" {
				require.Error(t, err)
				require.ErrorContains(t, err, tc.expectErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expectedTuples, tuples)
			}
		})
	}
}
