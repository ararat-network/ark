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

// TestRateSetConvertAnswersUnconvertibleAsUnknown pins the consequence of
// letting membership vouch for a denomination instead of a regular expression:
// every denomination the set does not carry is answered the same way, whether it
// is well formed, malformed, or empty. Conversion has no opinion on the shape of
// a denomination it was never given a rate for.
//
// The identity case is included because it is the one that changed. An offer
// denomination absent from the set is unknown even when it is also the ask, so
// no denomination leaves Convert without having been proven a key.
func TestRateSetConvertAnswersUnconvertibleAsUnknown(t *testing.T) {
	rates := types.NewRateSetFrom(map[string]math.LegacyDec{
		chain.USDBaseDenom: math.LegacyNewDec(2),
	})

	tests := []struct {
		name       string
		offerDenom string
		askDenom   string
	}{
		{"malformed offer denom", "!!!", chain.USDBaseDenom},
		{"malformed ask denom", chain.USDBaseDenom, "!!!"},
		{"empty offer denom", "", chain.USDBaseDenom},
		{"empty ask denom", chain.USDBaseDenom, ""},
		{"well formed but unlisted", "afoo", chain.USDBaseDenom},
		{"identity of an absent denom", "afoo", "afoo"},
		{"identity of a malformed denom", "!!!", "!!!"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			offerCoin := sdk.DecCoin{Denom: tc.offerDenom, Amount: math.LegacyOneDec()}
			require.NotPanics(t, func() {
				_, err := rates.Convert(offerCoin, tc.askDenom)
				require.ErrorIs(t, err, types.ErrUnknownDenom)
			})
		})
	}
}

// TestRateSetConvertUnitRateSkipMatchesApplied pins the equivalence the
// unit-rate skip rests on. LegacyDec multiplies and divides by scaling to 10^18
// and chopping back, so either operation against one divides evenly and returns
// the operand untouched, which is what makes skipping it bit-identical to
// applying it rather than an approximation of it. The skip fires on the leg
// carrying the numeraire, so this reproduces the applied arithmetic in both
// directions over the values most likely to expose a rounding difference: a rate
// whose reciprocal does not terminate, an amount carrying all eighteen decimal
// places, an amount wide enough to stress the intermediate product, and one at
// the precision floor.
func TestRateSetConvertUnitRateSkipMatchesApplied(t *testing.T) {
	one := math.LegacyOneDec()

	amounts := map[string]math.LegacyDec{
		"precision floor": math.LegacySmallestDec(),
		"full precision":  math.LegacyMustNewDecFromStr("1.234567890123456789"),
		"whole units":     math.LegacyNewDecFromInt(chain.NativeBaseAmount(1)),
		"wide":            math.LegacyNewDecFromBigInt(new(big.Int).Lsh(big.NewInt(1), 150)),
	}
	rates := map[string]math.LegacyDec{
		"non-terminating reciprocal": math.LegacyNewDec(3),
		"large":                      math.LegacyNewDec(1300),
		"fractional":                 math.LegacyMustNewDecFromStr("0.000000000000000007"),
	}

	for amountName, amount := range amounts {
		for rateName, rate := range rates {
			set := types.RateSet{chain.NoahBaseDenom: one, "ausd": rate}

			t.Run(amountName+"/"+rateName+"/ask leg is one", func(t *testing.T) {
				actual, err := set.Convert(sdk.NewDecCoinFromDec("ausd", amount), chain.NoahBaseDenom)
				require.NoError(t, err)
				// The arithmetic the skip stands in for: multiply by the ask
				// rate of one, then divide by the offer rate.
				require.True(t, amount.Mul(one).Quo(rate).Equal(actual.Amount))
			})

			t.Run(amountName+"/"+rateName+"/offer leg is one", func(t *testing.T) {
				actual, err := set.Convert(sdk.NewDecCoinFromDec(chain.NoahBaseDenom, amount), "ausd")
				require.NoError(t, err)
				require.True(t, amount.Mul(rate).Quo(one).Equal(actual.Amount))
			})
		}
	}
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
			name: "ask rate is nil",
			rates: types.RateSet{
				"ausd": math.LegacyOneDec(),
				"akrw": math.LegacyDec{},
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
