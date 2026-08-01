package types_test

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/pkg/chain"
	"ark/x/oracle/types"
)

func TestRateSetConvert(t *testing.T) {
	rates := types.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom:  math.LegacyNewDec(2),
		chain.KRWBaseDenom:  math.LegacyNewDec(1300),
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
			offerCoin: sdk.NewDecCoinFromDec(chain.USDBaseDenom, math.LegacyNewDec(2)),
			askDenom:  chain.KRWBaseDenom,
			expected:  sdk.NewDecCoinFromDec(chain.KRWBaseDenom, math.LegacyNewDec(1300)),
		},
		{
			name: "equal exponents preserve the display price in base units",
			offerCoin: sdk.NewDecCoinFromDec(
				chain.NoahBaseDenom,
				math.LegacyNewDecFromInt(chain.NativeBaseAmount(1)),
			),
			askDenom: chain.USDBaseDenom,
			expected: sdk.NewDecCoinFromDec(
				chain.USDBaseDenom,
				math.LegacyNewDecFromInt(chain.NativeBaseAmount(2)),
			),
		},
		{
			name:      "same denom preserves zero amount",
			offerCoin: sdk.NewDecCoinFromDec(chain.USDBaseDenom, math.LegacyZeroDec()),
			askDenom:  chain.USDBaseDenom,
			expected:  sdk.NewDecCoinFromDec(chain.USDBaseDenom, math.LegacyZeroDec()),
		},
		{
			name:      "missing offer rate",
			offerCoin: sdk.NewDecCoinFromDec("afoo", math.LegacyOneDec()),
			askDenom:  chain.USDBaseDenom,
			expectErr: types.ErrUnknownDenom,
		},
		{
			name:      "missing ask rate",
			offerCoin: sdk.NewDecCoinFromDec(chain.USDBaseDenom, math.LegacyOneDec()),
			askDenom:  "afoo",
			expectErr: types.ErrUnknownDenom,
		},
	}

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

func TestRateSetConvertLargeRepresentableAmount(t *testing.T) {
	largeAmount := new(big.Int).Lsh(big.NewInt(1), 200)
	offerCoin := sdk.NewDecCoinFromCoin(sdk.NewCoin("ausd", math.NewIntFromBigInt(largeAmount)))
	rates := types.RateSet{
		"ausd": math.LegacyOneDec(),
		"akrw": math.LegacyOneDec(),
	}

	actual, err := rates.Convert(offerCoin, "akrw")
	require.NoError(t, err)
	require.True(t, offerCoin.Amount.Equal(actual.Amount))
}

// TestRateSetConvertUnderflowTruncatesToZero pins measurement semantics: a
// positive offer whose converted value sits below Dec precision returns zero
// rather than an error. Entitlement callers enforce their own positivity bar
// after truncating to whole units.
func TestRateSetConvertUnderflowTruncatesToZero(t *testing.T) {
	rates := types.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		// One base unit of a denomination this hyperinflated is worth less
		// than Dec precision can carry.
		"ausd": math.LegacyNewDec(10).Power(19),
	}

	actual, err := rates.Convert(sdk.NewDecCoinFromDec("ausd", math.LegacyOneDec()), chain.NoahBaseDenom)
	require.NoError(t, err)
	require.Equal(t, chain.NoahBaseDenom, actual.Denom)
	require.True(t, actual.Amount.IsZero())
}

func TestRateSetConvertRangeErrors(t *testing.T) {
	max := maxLegacyDec()
	outOfRangeRaw := max.BigInt()
	outOfRangeRaw.Add(outOfRangeRaw, big.NewInt(1))
	outOfRange := math.LegacyNewDecFromBigIntWithPrec(outOfRangeRaw, math.LegacyPrecision)

	tests := []struct {
		name      string
		rates     types.RateSet
		offerCoin sdk.DecCoin
		askDenom  string
	}{
		{
			name: "offer amount is nil",
			rates: types.RateSet{
				"ausd": math.LegacyOneDec(),
			},
			offerCoin: sdk.DecCoin{Denom: "ausd", Amount: math.LegacyDec{}},
			askDenom:  "ausd",
		},
		{
			name: "offer amount is out of range",
			rates: types.RateSet{
				"ausd": math.LegacyOneDec(),
			},
			offerCoin: sdk.NewDecCoinFromDec("ausd", outOfRange),
			askDenom:  "ausd",
		},
		{
			name: "offer rate is out of range",
			rates: types.RateSet{
				"ausd": outOfRange,
				"akrw": math.LegacyOneDec(),
			},
			offerCoin: sdk.NewDecCoinFromDec("ausd", math.LegacyOneDec()),
			askDenom:  "akrw",
		},
		{
			name: "offer rate is nil",
			rates: types.RateSet{
				"ausd": math.LegacyDec{},
				"akrw": math.LegacyOneDec(),
			},
			offerCoin: sdk.NewDecCoinFromDec("ausd", math.LegacyOneDec()),
			askDenom:  "akrw",
		},
		{
			name: "offer rate is zero",
			rates: types.RateSet{
				"ausd": math.LegacyZeroDec(),
				"akrw": math.LegacyOneDec(),
			},
			offerCoin: sdk.NewDecCoinFromDec("ausd", math.LegacyOneDec()),
			askDenom:  "akrw",
		},
		{
			name: "ask rate is out of range",
			rates: types.RateSet{
				"ausd": math.LegacyOneDec(),
				"akrw": outOfRange,
			},
			offerCoin: sdk.NewDecCoinFromDec("ausd", math.LegacyOneDec()),
			askDenom:  "akrw",
		},
		{
			name: "intermediate multiplication overflows",
			rates: types.RateSet{
				"ausd": math.LegacyNewDec(2),
				"akrw": math.LegacyNewDec(2),
			},
			offerCoin: sdk.NewDecCoinFromDec("ausd", max),
			askDenom:  "akrw",
		},
		{
			name: "quotient overflows",
			rates: types.RateSet{
				"ausd": math.LegacySmallestDec(),
				"akrw": math.LegacyOneDec(),
			},
			offerCoin: sdk.NewDecCoinFromDec("ausd", max),
			askDenom:  "akrw",
		},
		{
			name: "negative rate produces a negative conversion",
			rates: types.RateSet{
				"ausd": math.LegacyOneDec(),
				"akrw": math.LegacyNewDec(-1),
			},
			offerCoin: sdk.NewDecCoinFromDec("ausd", math.LegacyOneDec()),
			askDenom:  "akrw",
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
