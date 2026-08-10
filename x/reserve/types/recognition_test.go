package types_test

import (
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"ark/pkg/decimal"
	oracletypes "ark/x/oracle/types"
	"ark/x/reserve/types"
)

// The combined age bound asserts one message from both its lower and upper
// cases, so the string lives once.
const rateAgeOutOfRange = "max rate age must be greater than zero and at most"

// creditDenom is the asset every credit case is written against.
const creditDenom = "asdr"

// rateSet quotes creditDenom in units per one NOAH, the orientation every
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
			MaxRateAge:          72 * time.Hour,
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
			name:    "negative window",
			mutate:  func(e *types.EligibilityEntry) { e.MaxRateAge = -time.Second },
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
			mutate:  func(e *types.EligibilityEntry) { e.MaxRateAge = types.MaxRecognitionRateAge + time.Second },
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

func TestSolveRecognition(t *testing.T) {
	tests := []struct {
		name   string
		base   int64
		stakes []types.RecognitionStake
		want   []types.RecognitionResult
	}{
		{
			// The both-breach fixture from the design discussion: T = 5M ÷
			// (1 − 0.5) = 10M, and each clipped asset holds exactly its share.
			name:   "clips both stakes to shares of the joint answer",
			base:   5_000_000,
			stakes: []types.RecognitionStake{stake("0.3", 4_000_000), stake("0.2", 3_000_000)},
			want:   []types.RecognitionResult{result(3_000_000, 3_000_000), result(2_000_000, 2_000_000)},
		},
		{
			// The waterfall-correction fixture: assuming both breach puts the
			// smaller holding under its own ceiling, so it solves unclipped
			// and T = (5M + 1M) ÷ (1 − 0.3) = 8_571_428.
			name:   "moves an unbreached stake to the unclipped side",
			base:   5_000_000,
			stakes: []types.RecognitionStake{stake("0.3", 4_000_000), stake("0.2", 1_000_000)},
			want:   []types.RecognitionResult{result(2_571_428, 2_571_428), result(1_714_285, 1_000_000)},
		},
		{
			// Nothing breaches: T = 1_000 + 100 and the ceiling is reported
			// even though it does not bind.
			name:   "keeps raw credit below the ceiling",
			base:   1_000,
			stakes: []types.RecognitionStake{stake("0.5", 100)},
			want:   []types.RecognitionResult{result(550, 100)},
		},
		{
			// raw = ratio × T exactly: both sides of the split agree, to the
			// anoah, and the asset holds precisely its permitted share.
			name:   "solves the threshold boundary consistently",
			base:   900,
			stakes: []types.RecognitionStake{stake("0.1", 100)},
			want:   []types.RecognitionResult{result(100, 100)},
		},
		{
			// The property the denominator was chosen for: a corrupted input
			// levers only the provable base, T = 1_000 ÷ (1 − 0.5) = 2_000.
			name:   "bounds an absurd raw credit by the base",
			base:   1_000,
			stakes: []types.RecognitionStake{stake("0.5", 1_000_000_000_000_000)},
			want:   []types.RecognitionResult{result(1_000, 1_000)},
		},
		{
			name:   "counts nothing without a base",
			base:   0,
			stakes: []types.RecognitionStake{stake("0.5", 500)},
			want:   []types.RecognitionResult{result(0, 0)},
		},
		{
			// Order is the caller's: an unsorted input maps back untouched.
			name: "preserves input order across the internal sort",
			base: 1_000,
			stakes: []types.RecognitionStake{
				stake("0.2", 10_000),
				stake("0.1", 5),
				stake("0.3", 10_000),
			},
			// The two large stakes clip against T = 1_005 ÷ (1 − 0.5) =
			// 2_010; the small one keeps its raw 5.
			want: []types.RecognitionResult{
				result(402, 402),
				result(201, 5),
				result(603, 603),
			},
		},
		{
			// A zero ratio holds no share and reports no ceiling; a zero raw
			// holds a share but occupies none of it.
			name: "passes custody-only and unpriced stakes through",
			base: 1_000,
			stakes: []types.RecognitionStake{
				{Ratio: math.LegacyZeroDec(), Raw: math.LegacyNewDec(100)},
				{Ratio: math.LegacyMustNewDecFromStr("0.3"), Raw: math.LegacyDec{}},
			},
			want: []types.RecognitionResult{result(0, 0), result(300, 0)},
		},
		{
			// Unreachable under a validated policy; answered with zero credit
			// rather than an unbounded certificate.
			name:   "refuses ratios summing to one",
			base:   1_000,
			stakes: []types.RecognitionStake{stake("0.6", 5_000), stake("0.4", 5_000)},
			want:   []types.RecognitionResult{result(0, 0), result(0, 0)},
		},
		{
			// A one-quantum raw credit against a zero base puts the scan's
			// products on the rounding boundary at consecutive splits. The
			// honest total is zero, and the tie must not unclip its way past
			// the larger stake's cap to the all-unclipped fallback.
			name: "counts nothing without a base at a rounding tie",
			base: 0,
			stakes: []types.RecognitionStake{
				stake("0.488779746583308052", 3_852_830),
				{
					Ratio: math.LegacyMustNewDecFromStr("0.103527094374366716"),
					Raw:   math.LegacyNewDecWithPrec(1, 18),
				},
			},
			want: []types.RecognitionResult{result(0, 0), result(0, 0)},
		},
		{
			// A stake sitting within a quantum of exactly its share — the
			// last four rounding digits are what put raw × denominator and
			// ratio × numerator on opposite sides of half-even at consecutive
			// splits. The solve must stay inside base ÷ (1 − Σratios) = 10M
			// rather than running to the unclipped fallback.
			name: "holds the base bound at a sub-quantum tie",
			base: 1_000_000,
			stakes: []types.RecognitionStake{
				{
					Ratio: math.LegacyMustNewDecFromStr("0.1"),
					Raw:   math.LegacyMustNewDecFromStr("1000000.000000000000000004"),
				},
				stake("0.8", 1_000_000_000),
			},
			want: []types.RecognitionResult{
				result(1_000_000, 1_000_000),
				result(8_000_000, 8_000_000),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := types.SolveRecognition(math.NewInt(tc.base), tc.stakes)
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

// TestSolveRecognitionRefusesAnUnrepresentableStake pins the policy the
// checked arithmetic buys: a stake past LegacyDec's range is an error, not a
// saturated figure. Only a clerical attestation reaches this magnitude, and
// the caller that produced it is where it should be answered.
func TestSolveRecognitionRefusesAnUnrepresentableStake(t *testing.T) {
	absurd := math.LegacyNewDecFromBigInt(new(big.Int).Lsh(big.NewInt(1), 255))
	stakes := []types.RecognitionStake{
		{Ratio: math.LegacyMustNewDecFromStr("0.5"), Raw: absurd},
	}

	_, err := types.SolveRecognition(math.NewInt(1_000), stakes)
	require.ErrorIs(t, err, decimal.ErrOutOfRange)
}

// TestSolveRecognitionReportsSharesExactly pins the invariant the ratio
// denotes: at the solved total, no credit exceeds its ratio of that very
// total — the self-reference is what makes the statement checkable against
// the answer itself.
func TestSolveRecognitionReportsSharesExactly(t *testing.T) {
	base := math.NewInt(5_000_000)
	stakes := []types.RecognitionStake{stake("0.3", 4_000_000), stake("0.2", 3_000_000)}

	results, err := types.SolveRecognition(base, stakes)
	require.NoError(t, err)
	total := base
	for _, res := range results {
		total = total.Add(res.Credit)
	}
	require.Equal(t, math.NewInt(10_000_000), total)
	for i, res := range results {
		share := stakes[i].Ratio.MulInt(total).TruncateInt()
		require.True(t, res.Credit.LTE(share), "stake %d exceeds its share", i)
	}
}

func TestRawCredit(t *testing.T) {
	entry := types.EligibilityEntry{
		Denom:               creditDenom,
		HaircutFactor:       math.LegacyMustNewDecFromStr("0.333333333333333333"),
		RecognitionCapRatio: math.LegacyMustNewDecFromStr("0.5"),
	}

	// The credit stays un-rounded and un-clipped: the cap plays no part here,
	// and SolveRecognition truncates once at the end rather than at each row.
	credit, err := entry.RawCredit(rateSet("1"), math.NewInt(10))
	require.NoError(t, err)
	require.Equal(t, math.LegacyMustNewDecFromStr("3.33333333333333333"), credit)

	// The orientation, pinned as arithmetic rather than as prose: an oracle rate
	// quotes its asset per one NOAH, so an asset trading at two per NOAH is
	// worth half its quantity in NOAH — the credit falls as the rate rises.
	t.Run("rate above one shrinks the credit", func(t *testing.T) {
		entry := types.EligibilityEntry{
			Denom:         creditDenom,
			HaircutFactor: math.LegacyOneDec(),
		}
		credit, err := entry.RawCredit(rateSet("2"), math.NewInt(100))
		require.NoError(t, err)
		require.Equal(t, math.LegacyNewDec(50), credit)

		credit, err = entry.RawCredit(rateSet("0.5"), math.NewInt(100))
		require.NoError(t, err)
		require.Equal(t, math.LegacyNewDec(200), credit)
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

// TestRawCreditRefusesAnUnusableRate pins the boundary the fold owns. An absent
// or zero rate is not answered here — it is an error, deliberately, because
// this function converts and neither can be converted through. The recognition
// fold gates on a present, positive rate and answers both with zero credit,
// which is where the degrade-to-zero policy belongs.
func TestRawCreditRefusesAnUnusableRate(t *testing.T) {
	entry := types.EligibilityEntry{
		Denom:         creditDenom,
		HaircutFactor: math.LegacyOneDec(),
	}

	_, err := entry.RawCredit(oracletypes.NewRateSet(), math.NewInt(10))
	require.ErrorIs(t, err, oracletypes.ErrUnknownDenom)

	_, err = entry.RawCredit(rateSet("0"), math.NewInt(10))
	require.ErrorIs(t, err, oracletypes.ErrConversionOutOfRange)
}

// TestRawCreditRefusesAnAbsurdAttestation pins where a clerical error is
// answered: at the row that produced it, naming the asset, rather than being
// carried into the solve for the cap to neutralise.
func TestRawCreditRefusesAnAbsurdAttestation(t *testing.T) {
	entry := types.EligibilityEntry{
		Denom:               creditDenom,
		HaircutFactor:       math.LegacyOneDec(),
		RecognitionCapRatio: math.LegacyMustNewDecFromStr("0.5"),
	}

	// A quantity alone cannot overflow — math.Int stops just where LegacyDec
	// does — so it takes a quantity near that ceiling divided by a rate below
	// one, which is what a mis-keyed attestation looks like against an asset
	// worth more than a NOAH apiece.
	absurd := math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 255))
	_, err := entry.RawCredit(rateSet("0.01"), absurd)
	require.ErrorIs(t, err, oracletypes.ErrConversionOutOfRange)
	require.ErrorContains(t, err, "asdr")
}
