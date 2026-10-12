package types_test

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	"github.com/ararat-network/ark/x/reserve/types"
)

// The combined age bound asserts one message from both its lower and upper
// cases, so the string lives once.
const rateAgeOutOfRange = "max rate age must be greater than zero and at most"

// creditDenom is the asset every credit case is written against.
const creditDenom = "axdr"

// rateSet quotes creditDenom in NOAH per one unit, the orientation every
// oracle rate carries, and carries the NOAH identity with it.
func rateSet(rate string) oracletypes.RateSet {
	return oracletypes.NewRateSetFrom(map[string]math.LegacyDec{
		creditDenom: math.LegacyMustNewDecFromStr(rate),
	})
}

// TestEligibilityEntryValidate covers the rules an entry carries alone: the
// external shape, which is what makes an Ark-issued denomination unlistable
// without any registry read, the positivity of both credit factors, and the
// staleness window's domain cap.
func TestEligibilityEntryValidate(t *testing.T) {
	valid := func() types.EligibilityEntry {
		return types.EligibilityEntry{
			Denom:               "axau-lbma",
			HaircutFactor:       math.LegacyMustNewDecFromStr("0.8"),
			RecognitionCapRatio: math.LegacyMustNewDecFromStr("0.2"),
			MaxRateAge:          72 * 3_600,
		}
	}

	tests := []struct {
		name    string
		mutate  func(*types.EligibilityEntry)
		wantErr string
	}{
		{
			name:   "valid external symbol",
			mutate: func(*types.EligibilityEntry) {},
		},
		{
			// Two custodians of one instrument are two entries over one series,
			// which is the case the tag exists for.
			name:   "another external on the same series",
			mutate: func(e *types.EligibilityEntry) { e.Denom = "axau-cb" },
		},
		{
			// The partition: the name an asset could be registered under is
			// refused here, whether or not one currently is.
			name:    "bare denomination",
			mutate:  func(e *types.EligibilityEntry) { e.Denom = "ausd" },
			wantErr: "must name a feed and a tag",
		},
		{
			name:    "numeraire prefix",
			mutate:  func(e *types.EligibilityEntry) { e.Denom = "anoah-x" },
			wantErr: "never priced",
		},
		{
			// A nil Dec panics on comparison, so the unset guard is the gate:
			// RawCredit reads HaircutFactor without one of its own, trusting
			// that every stored entry came through here.
			name:    "unset haircut",
			mutate:  func(e *types.EligibilityEntry) { e.HaircutFactor = math.LegacyDec{} },
			wantErr: "haircut factor must be greater than zero and at most one",
		},
		{
			name:    "unset cap ratio",
			mutate:  func(e *types.EligibilityEntry) { e.RecognitionCapRatio = math.LegacyDec{} },
			wantErr: "recognition cap ratio must be greater than zero and at most one",
		},
		{
			// An entry granting no credit is indistinguishable from no entry at
			// all — it admits nothing, since custody admission never reads this
			// policy, and folds to zero exactly as an unlisted denomination
			// does — so a zero factor either side is refused rather than stored.
			name:    "zero haircut",
			mutate:  func(e *types.EligibilityEntry) { e.HaircutFactor = math.LegacyZeroDec() },
			wantErr: "haircut factor must be greater than zero and at most one",
		},
		{
			name:    "zero cap ratio",
			mutate:  func(e *types.EligibilityEntry) { e.RecognitionCapRatio = math.LegacyZeroDec() },
			wantErr: "recognition cap ratio must be greater than zero and at most one",
		},
		{
			// The smallest representable factor is admissible on both: the rule
			// is positivity, not a judgment about how small a credit is worth
			// granting.
			name: "smallest positive factors",
			mutate: func(e *types.EligibilityEntry) {
				e.HaircutFactor = math.LegacySmallestDec()
				e.RecognitionCapRatio = math.LegacySmallestDec()
			},
		},
		{
			name:    "unset window",
			mutate:  func(e *types.EligibilityEntry) { e.MaxRateAge = 0 },
			wantErr: rateAgeOutOfRange,
		},
		{
			// The cap itself is admissible: it bounds absurdity, it does not
			// express an opinion about sensible windows.
			name:   "window at the domain cap",
			mutate: func(e *types.EligibilityEntry) { e.MaxRateAge = types.MaxRecognitionRateAge },
		},
		{
			name:    "window past the domain cap",
			mutate:  func(e *types.EligibilityEntry) { e.MaxRateAge = types.MaxRecognitionRateAge + 1 },
			wantErr: rateAgeOutOfRange,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entry := valid()
			tc.mutate(&entry)
			err := entry.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func stake(ratio string, raw int64) types.RecognitionStake {
	return types.RecognitionStake{
		Ratio: math.LegacyMustNewDecFromStr(ratio),
		Raw:   math.LegacyNewDec(raw),
	}
}

func result(ceiling, credit int64) types.RecognitionResult {
	return types.RecognitionResult{
		Ceiling: math.NewInt(ceiling),
		Credit:  math.NewInt(credit),
	}
}

func TestRecognitionCredits(t *testing.T) {
	tests := []struct {
		name   string
		base   int64
		stakes []types.RecognitionStake
		want   []types.RecognitionResult
	}{
		{
			name:   "clips each stake at its ratio of the balance",
			base:   5_000_000,
			stakes: []types.RecognitionStake{stake("0.3", 4_000_000), stake("0.2", 3_000_000)},
			want:   []types.RecognitionResult{result(1_500_000, 1_500_000), result(1_000_000, 1_000_000)},
		},
		{
			// The ceiling is reported even when it does not bind.
			name:   "keeps raw credit below the ceiling",
			base:   1_000,
			stakes: []types.RecognitionStake{stake("0.5", 100)},
			want:   []types.RecognitionResult{result(500, 100)},
		},
		{
			name:   "credits a raw value at the ceiling exactly",
			base:   1_000,
			stakes: []types.RecognitionStake{stake("0.1", 100)},
			want:   []types.RecognitionResult{result(100, 100)},
		},
		{
			// A corrupted input earns its cap and nothing more.
			name:   "bounds an absurd raw credit by the balance",
			base:   1_000,
			stakes: []types.RecognitionStake{stake("0.5", 1_000_000_000_000_000)},
			want:   []types.RecognitionResult{result(500, 500)},
		},
		{
			name:   "counts nothing without a balance",
			base:   0,
			stakes: []types.RecognitionStake{stake("0.5", 500)},
			want:   []types.RecognitionResult{result(0, 0)},
		},
		{
			// 0.1 × 999 = 99.9: the ceiling floors, as recognition never rounds up.
			name:   "floors a fractional ceiling",
			base:   999,
			stakes: []types.RecognitionStake{stake("0.1", 1_000)},
			want:   []types.RecognitionResult{result(99, 99)},
		},
		{
			name: "preserves input order",
			base: 1_000,
			stakes: []types.RecognitionStake{
				stake("0.2", 10_000),
				stake("0.1", 5),
				stake("0.3", 10_000),
			},
			want: []types.RecognitionResult{
				result(200, 200),
				result(100, 5),
				result(300, 300),
			},
		},
		{
			// A zero ratio holds no cap and reports no ceiling; a zero raw
			// holds a cap but uses none of it.
			name: "passes custody-only and unpriced stakes through",
			base: 1_000,
			stakes: []types.RecognitionStake{
				{Ratio: math.LegacyZeroDec(), Raw: math.LegacyNewDec(100)},
				{Ratio: math.LegacyMustNewDecFromStr("0.3"), Raw: math.LegacyDec{}},
			},
			want: []types.RecognitionResult{result(0, 0), result(300, 0)},
		},
		{
			// The validated ceiling: external credit may equal the balance,
			// doubling it and no more.
			name:   "admits ratios summing to one",
			base:   1_000,
			stakes: []types.RecognitionStake{stake("0.6", 5_000), stake("0.4", 5_000)},
			want:   []types.RecognitionResult{result(600, 600), result(400, 400)},
		},
		{
			// Unreachable under a validated policy; answered with zero credit
			// rather than more than twice the balance.
			name:   "refuses ratios summing above one",
			base:   1_000,
			stakes: []types.RecognitionStake{stake("0.6", 5_000), stake("0.5", 5_000)},
			want:   []types.RecognitionResult{result(0, 0), result(0, 0)},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := types.RecognitionCredits(math.NewInt(tc.base), tc.stakes)
			require.NoError(t, err)
			// Element-wise Int.Equal rather than require.Equal: a zero Int
			// truncated out of a sub-anoah decimal carries a different big.Int
			// representation than NewInt(0), and only the value is the claim.
			require.Len(t, got, len(tc.want))
			for i := range tc.want {
				require.True(t, tc.want[i].Ceiling.Equal(got[i].Ceiling),
					"stake %d ceiling: want %s, got %s", i, tc.want[i].Ceiling, got[i].Ceiling)
				require.True(t, tc.want[i].Credit.Equal(got[i].Credit),
					"stake %d credit: want %s, got %s", i, tc.want[i].Credit, got[i].Credit)
			}
		})
	}
}

// TestRecognitionCreditsSurviveExtremeInputs pins that a credit near the
// decimal ceiling, or a ratio at the decimal floor, caps cleanly rather than
// failing the block that priced it.
func TestRecognitionCreditsSurviveExtremeInputs(t *testing.T) {
	absurd := math.LegacyNewDecFromBigInt(new(big.Int).Lsh(big.NewInt(1), 255))
	tests := []struct {
		name   string
		stakes []types.RecognitionStake
		credit math.Int
	}{
		{
			name:   "credit near the ceiling",
			stakes: []types.RecognitionStake{{Ratio: math.LegacyMustNewDecFromStr("0.5"), Raw: absurd}},
			credit: math.NewInt(500),
		},
		{
			name:   "ratio at the floor",
			stakes: []types.RecognitionStake{{Ratio: math.LegacySmallestDec(), Raw: absurd}},
			credit: math.ZeroInt(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			results, err := types.RecognitionCredits(math.NewInt(1_000), tc.stakes)
			require.NoError(t, err)
			require.True(t, tc.credit.Equal(results[0].Credit), "credit %s", results[0].Credit)
		})
	}
}

// TestRecognitionCreditsAreIndependent pins the property the cap exists for:
// a misreported holding earns its own cap and moves no other asset's credit.
// Measured against recognised capital instead, the fake entry below would
// have raised gold's ceiling with the total and carried 200 to 1,000.
func TestRecognitionCreditsAreIndependent(t *testing.T) {
	gold := stake("0.5", 1_000)
	fake := stake("0.4", 1_000_000_000)
	base := math.NewInt(100)

	honest, err := types.RecognitionCredits(base, []types.RecognitionStake{gold})
	require.NoError(t, err)
	attacked, err := types.RecognitionCredits(base, []types.RecognitionStake{gold, fake})
	require.NoError(t, err)

	require.Equal(t, math.NewInt(50), honest[0].Credit)
	require.Equal(t, honest[0].Credit, attacked[0].Credit, "the fake entry moved gold's credit")
	require.Equal(t, math.NewInt(40), attacked[1].Credit, "the fake entry earns exactly its cap")
}

func TestRawCredit(t *testing.T) {
	entry := types.EligibilityEntry{
		Denom:               creditDenom,
		HaircutFactor:       math.LegacyMustNewDecFromStr("0.333333333333333333"),
		RecognitionCapRatio: math.LegacyMustNewDecFromStr("0.5"),
	}

	// The credit stays un-rounded and un-clipped: the cap plays no part here,
	// and RecognitionCredits truncates once at the end rather than at each row.
	credit, err := entry.RawCredit(rateSet("1"), math.NewInt(10))
	require.NoError(t, err)
	require.Equal(t, math.LegacyMustNewDecFromStr("3.33333333333333333"), credit)

	// The orientation, pinned as arithmetic rather than as prose: an oracle rate
	// quotes NOAH per one unit of its asset, so an asset worth two NOAH apiece
	// is worth twice its quantity in NOAH — the credit rises with the rate.
	t.Run("rate above one grows the credit", func(t *testing.T) {
		entry := types.EligibilityEntry{
			Denom:         creditDenom,
			HaircutFactor: math.LegacyOneDec(),
		}
		credit, err := entry.RawCredit(rateSet("2"), math.NewInt(100))
		require.NoError(t, err)
		require.Equal(t, math.LegacyNewDec(200), credit)

		credit, err = entry.RawCredit(rateSet("0.5"), math.NewInt(100))
		require.NoError(t, err)
		require.Equal(t, math.LegacyNewDec(50), credit)
	})

	t.Run("no quantity", func(t *testing.T) {
		credit, err := entry.RawCredit(rateSet("1"), math.ZeroInt())
		require.NoError(t, err)
		require.True(t, credit.IsZero())
	})

	// Validate refuses a zero haircut, so this guard is unreachable through a
	// stored entry. It is exercised because RawCredit is a pure function that
	// does not get to assume its caller validated, and answering zero is the
	// only safe reading of a factor that credits nothing.
	t.Run("zero haircut credits nothing", func(t *testing.T) {
		entry.HaircutFactor = math.LegacyZeroDec()
		credit, err := entry.RawCredit(rateSet("1"), math.NewInt(10))
		require.NoError(t, err)
		require.True(t, credit.IsZero())
	})
}

// TestRawCreditRefusesAnAbsentRate checks that missing rates fail conversion, while a zero rate
// produces zero NOAH credit.
func TestRawCreditRefusesAnAbsentRate(t *testing.T) {
	entry := types.EligibilityEntry{
		Denom:         creditDenom,
		HaircutFactor: math.LegacyOneDec(),
	}

	_, err := entry.RawCredit(oracletypes.NewRateSet(), math.NewInt(10))
	require.ErrorIs(t, err, oracletypes.ErrUnknownDenom)

	credit, err := entry.RawCredit(rateSet("0"), math.NewInt(10))
	require.NoError(t, err)
	require.True(t, credit.IsZero())
}

// TestRawCreditRefusesAnAbsurdAttestation pins where a clerical error is
// answered: at the row that produced it, naming the asset, rather than being
// carried into the cap to neutralise.
func TestRawCreditRefusesAnAbsurdAttestation(t *testing.T) {
	entry := types.EligibilityEntry{
		Denom:               creditDenom,
		HaircutFactor:       math.LegacyOneDec(),
		RecognitionCapRatio: math.LegacyMustNewDecFromStr("0.5"),
	}

	// A quantity alone cannot overflow — math.Int stops just where LegacyDec
	// does — so it takes a quantity near that ceiling multiplied by a rate
	// above one, which is what a mis-keyed attestation looks like against an
	// asset worth more than a NOAH apiece.
	absurd := math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 255))
	_, err := entry.RawCredit(rateSet("100"), absurd)
	require.ErrorIs(t, err, oracletypes.ErrConversionOutOfRange)
	require.ErrorContains(t, err, "axdr")
}
