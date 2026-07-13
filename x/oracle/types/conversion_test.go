package types_test

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/x/oracle/types"
)

func TestRateSnapshotConvert(t *testing.T) {
	rates := types.RateSnapshot{
		"uusd": math.LegacyOneDec(),
		"ukrw": math.LegacyNewDec(1300),
	}

	tests := []struct {
		name      string
		offerCoin sdk.DecCoin
		askDenom  string
		expected  sdk.DecCoin
		expectErr error
	}{
		{
			name:      "converts through captured rates",
			offerCoin: sdk.NewDecCoinFromDec("uusd", math.LegacyNewDec(2)),
			askDenom:  "ukrw",
			expected:  sdk.NewDecCoinFromDec("ukrw", math.LegacyNewDec(2600)),
		},
		{
			name:      "same denom preserves zero amount",
			offerCoin: sdk.NewDecCoinFromDec("uusd", math.LegacyZeroDec()),
			askDenom:  "uusd",
			expected:  sdk.NewDecCoinFromDec("uusd", math.LegacyZeroDec()),
		},
		{
			name:      "missing offer rate",
			offerCoin: sdk.NewDecCoinFromDec("ufoo", math.LegacyOneDec()),
			askDenom:  "uusd",
			expectErr: types.ErrUnknownDenom,
		},
		{
			name:      "missing ask rate",
			offerCoin: sdk.NewDecCoinFromDec("uusd", math.LegacyOneDec()),
			askDenom:  "ufoo",
			expectErr: types.ErrUnknownDenom,
		},
		{
			name:      "non-positive rate",
			offerCoin: sdk.NewDecCoinFromDec("uzero", math.LegacyOneDec()),
			askDenom:  "uusd",
			expectErr: types.ErrInvalidExchangeRate,
		},
	}
	rates["uzero"] = math.LegacyZeroDec()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := rates.Convert(tc.offerCoin, tc.askDenom)
			if tc.expectErr != nil {
				require.ErrorIs(t, err, tc.expectErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.expected.Denom, actual.Denom)
			require.True(t, tc.expected.Amount.Equal(actual.Amount))
		})
	}
}

func TestRateSnapshotConvertLargeRepresentableAmount(t *testing.T) {
	largeAmount := new(big.Int).Lsh(big.NewInt(1), 200)
	offerCoin := sdk.NewDecCoinFromCoin(sdk.NewCoin("uusd", math.NewIntFromBigInt(largeAmount)))
	rates := types.RateSnapshot{
		"uusd": math.LegacyOneDec(),
		"ukrw": math.LegacyOneDec(),
	}

	actual, err := rates.Convert(offerCoin, "ukrw")
	require.NoError(t, err)
	require.True(t, offerCoin.Amount.Equal(actual.Amount))
}

func TestRateSnapshotConvertRangeErrors(t *testing.T) {
	max := maxLegacyDec()
	outOfRangeRaw := max.BigInt()
	outOfRangeRaw.Add(outOfRangeRaw, big.NewInt(1))
	outOfRange := math.LegacyNewDecFromBigIntWithPrec(outOfRangeRaw, math.LegacyPrecision)

	tests := []struct {
		name      string
		rates     types.RateSnapshot
		offerCoin sdk.DecCoin
		askDenom  string
	}{
		{
			name: "offer amount is nil",
			rates: types.RateSnapshot{
				"uusd": math.LegacyOneDec(),
			},
			offerCoin: sdk.DecCoin{Denom: "uusd", Amount: math.LegacyDec{}},
			askDenom:  "uusd",
		},
		{
			name: "offer amount is out of range",
			rates: types.RateSnapshot{
				"uusd": math.LegacyOneDec(),
			},
			offerCoin: sdk.NewDecCoinFromDec("uusd", outOfRange),
			askDenom:  "uusd",
		},
		{
			name: "intermediate multiplication overflows",
			rates: types.RateSnapshot{
				"uusd": math.LegacyNewDec(2),
				"ukrw": math.LegacyNewDec(2),
			},
			offerCoin: sdk.NewDecCoinFromDec("uusd", max),
			askDenom:  "ukrw",
		},
		{
			name: "quotient overflows",
			rates: types.RateSnapshot{
				"uusd": math.LegacySmallestDec(),
				"ukrw": math.LegacyOneDec(),
			},
			offerCoin: sdk.NewDecCoinFromDec("uusd", max),
			askDenom:  "ukrw",
		},
		{
			name: "conversion underflows to zero",
			rates: types.RateSnapshot{
				"uusd": math.LegacyOneDec(),
				"ukrw": math.LegacySmallestDec(),
			},
			offerCoin: sdk.NewDecCoinFromDec("uusd", math.LegacySmallestDec()),
			askDenom:  "ukrw",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.NotPanics(t, func() {
				_, err := tc.rates.Convert(tc.offerCoin, tc.askDenom)
				require.ErrorIs(t, err, types.ErrConversionOutOfRange)
			})
		})
	}
}

func maxLegacyDec() math.LegacyDec {
	precision := new(big.Int).Exp(big.NewInt(10), big.NewInt(math.LegacyPrecision), nil)
	raw := new(big.Int).Lsh(big.NewInt(1), 256)
	raw.Mul(raw, precision)
	raw.Sub(raw, big.NewInt(1))
	return math.LegacyNewDecFromBigIntWithPrec(raw, math.LegacyPrecision)
}
