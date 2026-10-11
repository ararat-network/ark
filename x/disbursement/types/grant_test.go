package types_test

import (
	stdmath "math"
	"math/big"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/disbursement/types"
)

func TestAccrualBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name      string
		kind      types.GrantKind
		now       uint64
		cancelled bool
		want      int64
		next      uint64
	}{
		{"member held to its first boundary", types.GrantKind_GRANT_KIND_MEMBER, 100, false, 0, 110},
		{"member at its first boundary", types.GrantKind_GRANT_KIND_MEMBER, 110, false, 7, 120},
		{"contributor starts unpaid", types.GrantKind_GRANT_KIND_COMPENSATION, 100, false, 0, 110},
		{"before boundary", types.GrantKind_GRANT_KIND_OWNERSHIP, 109, false, 0, 110},
		{"at boundary", types.GrantKind_GRANT_KIND_OWNERSHIP, 110, false, 7, 120},
		{"catch up", types.GrantKind_GRANT_KIND_COMPENSATION, 129, false, 14, 130},
		{"final remainder", types.GrantKind_GRANT_KIND_MEMBER, 130, false, 23, 0},
		{"cancelled clock", types.GrantKind_GRANT_KIND_COMPENSATION, 1000, true, 14, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := types.Grant{
				Kind: tc.kind, Amount: sdk.NewInt64Coin("anoah", 23), StartTime: 100,
				Schedule: types.Schedule{FirstLength: 10, FirstWeight: 1, PeriodLength: 10, PeriodWeight: 1, Periods: 2},
			}
			if tc.cancelled {
				g.CutoffTime = 120
			}
			earned, next, err := g.Accrued(tc.now)
			require.NoError(t, err)
			require.Equal(t, math.NewInt(tc.want), earned)
			require.Equal(t, tc.next, next)
		})
	}
}

// TestAccrualGate: nothing accrues before one period after the award, wherever the clock starts,
// so a cancel before then returns everything.
func TestAccrualGate(t *testing.T) {
	grant := func(start uint64) types.Grant {
		return types.Grant{
			Kind: types.GrantKind_GRANT_KIND_COMPENSATION, Amount: sdk.NewInt64Coin("anoah", 20), StartTime: start, CreatedTime: 1_000,
			Schedule: types.Schedule{FirstLength: 100, FirstWeight: 1, PeriodLength: 100, PeriodWeight: 1, Periods: 1},
		}
	}
	for _, tc := range []struct {
		name   string
		start  uint64
		now    uint64
		cutoff uint64
		want   int64
		next   uint64
	}{
		{"backdated, before the gate", 500, 1_050, 0, 0, 1_100},
		{"backdated, at the gate", 500, 1_100, 0, 20, 0},
		{"backdated, cancelled before the gate", 500, 2_000, 1_050, 0, 0},
		{"backdated, cancelled at the gate", 500, 2_000, 1_100, 20, 0},
		{"backdated, between its first boundary and the gate", 950, 1_060, 0, 0, 1_100},
		{"backdated, at the gate mid-period", 950, 1_100, 0, 10, 1_150},
		{"now, before the gate", 1_000, 1_050, 0, 0, 1_100},
		{"now, at the first boundary", 1_000, 1_100, 0, 10, 1_200},
		{"forward, before the gate", 2_000, 1_050, 0, 0, 2_100},
		{"forward, past the gate", 2_000, 1_500, 0, 0, 2_100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := grant(tc.start)
			g.CutoffTime = tc.cutoff
			earned, next, err := g.Accrued(tc.now)
			require.NoError(t, err)
			require.Equal(t, math.NewInt(tc.want), earned)
			require.Equal(t, tc.next, next)
		})
	}
}

// TestRandomisedAccrualNext: next is exactly the first time earned changes, and zero when it never
// does, for any shape, a clock either side of the award, and cancellation.
func TestRandomisedAccrualNext(t *testing.T) {
	kinds := []types.GrantKind{types.GrantKind_GRANT_KIND_MEMBER, types.GrantKind_GRANT_KIND_OWNERSHIP, types.GrantKind_GRANT_KIND_COMPENSATION}
	const created = uint64(100)
	for _, seed := range []int64{1, 17, 982} {
		t.Run(big.NewInt(seed).String(), func(t *testing.T) {
			r := rand.New(rand.NewSource(seed))
			for range 300 {
				g := types.Grant{
					Kind: kinds[r.Intn(len(kinds))], Amount: sdk.NewInt64Coin("anoah", int64(50+r.Intn(500))),
					StartTime: uint64(40 + r.Intn(120)), CreatedTime: created,
					Schedule: types.Schedule{
						FirstLength: uint64(1 + r.Intn(20)), FirstWeight: uint64(1 + r.Intn(4)),
						PeriodLength: uint64(1 + r.Intn(20)), PeriodWeight: uint64(1 + r.Intn(4)), Periods: uint32(r.Intn(5)),
					},
				}
				if g.Kind == types.GrantKind_GRANT_KIND_MEMBER {
					g.StartTime = created
				}
				if r.Intn(4) == 0 {
					g.CutoffTime = created + uint64(r.Intn(150))
				}
				// Nothing asks about a time before the award, and the gate falls within a period of it.
				now := created + uint64(r.Intn(40))
				// Nothing changes past the end or the gate.
				last := max(g.StartTime+g.Schedule.Duration(), created+g.Schedule.FirstLength)
				before, next, err := g.Accrued(now)
				require.NoError(t, err)
				require.True(t, next == 0 || next > now, "%+v at %d: next %d is not ahead", g, now, next)
				for at := now + 1; at <= last+1; at++ {
					earned, _, err := g.Accrued(at)
					require.NoError(t, err)
					if next != 0 && at == next {
						require.False(t, earned.Equal(before), "%+v at %d: nothing changes at next %d", g, now, next)
						break
					}
					require.True(t, earned.Equal(before), "%+v at %d: changes at %d before next %d", g, now, at, next)
				}
			}
		})
	}
}

func TestRetryAt(t *testing.T) {
	const now, retry = uint64(1_000), types.PaymentRetryInterval
	for _, tc := range []struct {
		name string
		next uint64
		want uint64
	}{
		{"a near boundary looks there", now + 10, now + 10},
		{"a boundary at the retry looks at the retry", now + retry, now + retry},
		{"a far boundary retries first", now + 5_000_000, now + retry},
		{"past the schedule retries", 0, now + retry},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, types.RetryAt(now, tc.next))
		})
	}
}

func TestGrantValidation(t *testing.T) {
	address := sdk.AccAddress(make([]byte, 20)).String()
	base := func() types.Grant {
		return types.Grant{
			Id: 1, Kind: types.GrantKind_GRANT_KIND_COMPENSATION, Beneficiary: address, Payee: address, CreatedBy: address,
			Amount: sdk.NewInt64Coin("anoah", 30), Schedule: types.Schedule{FirstLength: hold, FirstWeight: 1, PeriodLength: hold, PeriodWeight: 1, Periods: 2},
			StartTime: 100, CreatedTime: 100, Paid: math.NewInt(10), Remaining: math.NewInt(20), Reference: "work", CreatedHeight: 1, DueTime: 100 + 2*hold,
			AwardValue: math.NewInt(30), Charge: math.ZeroInt(),
		}
	}
	member := func(g *types.Grant) {
		g.Kind, g.AwardValue, g.MandateTerm = types.GrantKind_GRANT_KIND_MEMBER, math.ZeroInt(), 1
	}
	ownership := func(g *types.Grant) { g.Kind, g.AwardValue = types.GrantKind_GRANT_KIND_OWNERSHIP, math.ZeroInt() }
	// stretch gives the compensation award three periods of the given length, queued for its second.
	stretch := func(period uint64) func(*types.Grant) {
		return func(g *types.Grant) {
			g.Schedule.FirstLength, g.Schedule.PeriodLength, g.DueTime = period, period, 100+2*period
		}
	}
	const (
		dueTime      = "invalid grant due time"
		dueEarly     = "grant is due before one period after its award"
		creation     = "invalid grant creation time or height"
		suspension   = "invalid member suspension"
		cutoff       = "invalid cancellation cutoff"
		entitlement  = "grant exceeds its earned entitlement"
		terms        = "a member carries its registrar term and ownership none"
		clock        = "grant clock does not fit its kind"
		floor        = "a grant's first period must be at least two weeks"
		compensation = "compensation is equal periods of at least two weeks, at most ten years in all"
	)
	for _, tc := range []struct {
		name   string
		mutate func(*types.Grant)
		err    string
	}{
		{"live and queued", func(*types.Grant) {}, ""},
		{"live and unqueued", func(g *types.Grant) { g.DueTime = 0 }, dueTime},
		{"zero ID", func(g *types.Grant) { g.Id = 0 }, "invalid grant ID"},
		{"unknown kind", func(g *types.Grant) { g.Kind = types.GrantKind(7) }, "unknown grant kind"},
		{"created at no height", func(g *types.Grant) { g.CreatedHeight = 0 }, creation},
		{"created at no time", func(g *types.Grant) { g.CreatedTime = 0 }, creation},
		{"suspended contributor", func(g *types.Grant) { g.SuspendedTime, g.DueTime = 115, 0 }, suspension},
		{"member suspended before its registration", func(g *types.Grant) { member(g); g.SuspendedTime, g.DueTime = 99, 0 }, suspension},
		{"compensation without an award value", func(g *types.Grant) { g.AwardValue = math.ZeroInt() }, "award value: amount must be positive"},
		{"member with an award value", func(g *types.Grant) { member(g); g.AwardValue = math.NewInt(30) }, "only compensation carries an award value"},
		{"queued before it started", func(g *types.Grant) { g.DueTime = 99 }, dueTime},
		// Nothing is due before one period after the award, wherever the clock starts.
		{"queued at its gate", func(g *types.Grant) { g.DueTime = 100 + hold }, ""},
		{"queued inside its first period", func(g *types.Grant) { g.DueTime = 100 + hold - 1 }, dueEarly},
		{"backdated clock queued at its gate", func(g *types.Grant) { g.StartTime, g.DueTime = 50, 100+hold }, ""},
		{"backdated clock queued before its gate", func(g *types.Grant) { g.StartTime, g.DueTime = 50, 100+hold-1 }, dueEarly},
		{"finished and unqueued", func(g *types.Grant) { g.Paid, g.Remaining, g.DueTime = math.NewInt(30), math.ZeroInt(), 0 }, ""},
		{"finished and queued", func(g *types.Grant) { g.Paid, g.Remaining = math.NewInt(30), math.ZeroInt() }, dueTime},
		{"cancelled debt queued", func(g *types.Grant) { g.CutoffTime, g.Remaining = 100+2*hold, math.NewInt(10) }, ""},
		{"cancelled debt unqueued", func(g *types.Grant) { g.CutoffTime, g.Remaining, g.DueTime = 100+2*hold, math.NewInt(10), 0 }, dueTime},
		{"cancelled holding past its debt", func(g *types.Grant) { g.CutoffTime = 100 + 2*hold }, entitlement},
		{"cancelled at its award", func(g *types.Grant) {
			g.CutoffTime, g.Paid, g.Remaining, g.DueTime = 100, math.ZeroInt(), math.ZeroInt(), 0
		}, ""},
		{"cancelled before its award", func(g *types.Grant) {
			g.CutoffTime, g.Paid, g.Remaining, g.DueTime = 99, math.ZeroInt(), math.ZeroInt(), 0
		}, cutoff},
		{"member cancelled inside its hold", func(g *types.Grant) {
			member(g)
			g.CutoffTime, g.Paid, g.Remaining, g.DueTime = 115, math.ZeroInt(), math.ZeroInt(), 0
		}, ""},
		{"member cancelled after its tenth", func(g *types.Grant) { member(g); g.CutoffTime, g.Remaining, g.DueTime = 100+hold, math.ZeroInt(), 0 }, ""},
		{"cancelled while suspended", func(g *types.Grant) {
			member(g)
			g.CutoffTime, g.SuspendedTime, g.Remaining, g.DueTime = 115, 115, math.ZeroInt(), 0
		}, cutoff},
		{"suspended member unqueued", func(g *types.Grant) { member(g); g.SuspendedTime, g.DueTime = 100+hold, 0 }, ""},
		{"member suspended inside its hold", func(g *types.Grant) {
			member(g)
			g.SuspendedTime, g.DueTime, g.Paid, g.Remaining = 115, 0, math.ZeroInt(), math.NewInt(30)
		}, ""},
		{"suspended member queued", func(g *types.Grant) {
			member(g)
			g.SuspendedTime, g.Paid, g.Remaining = 115, math.ZeroInt(), math.NewInt(30)
		}, dueTime},
		{"suspended member paid past its suspension", func(g *types.Grant) {
			member(g)
			g.SuspendedTime, g.DueTime, g.Paid, g.Remaining = 115, 0, math.NewInt(20), math.NewInt(10)
		}, "suspended member paid past its suspension"},
		{"committee compensation", func(g *types.Grant) { g.MandateTerm, g.Charge = 1, math.NewInt(35) }, ""},
		{"committee compensation without a charge", func(g *types.Grant) { g.MandateTerm = 1 }, "charge: amount must be positive"},
		{"committee compensation with an unset charge", func(g *types.Grant) { g.MandateTerm, g.Charge = 1, math.Int{} }, "charge: amount must be nonnegative"},
		{"governance compensation with a charge", func(g *types.Grant) { g.Charge = math.NewInt(35) }, "only a committee award carries a charge"},
		{"member with a charge", func(g *types.Grant) { member(g); g.Charge = math.NewInt(35) }, "only a committee award carries a charge"},
		{"member without a term", func(g *types.Grant) { member(g); g.MandateTerm = 0 }, terms},
		{"governance ownership", ownership, ""},
		{"ownership under a term", func(g *types.Grant) { ownership(g); g.MandateTerm = 1 }, terms},
		{"registered member", member, ""},
		// A member's tenth waits a period like every first payment (D126).
		{"member registered and unpaid", func(g *types.Grant) {
			member(g)
			g.Paid, g.Remaining, g.DueTime = math.ZeroInt(), math.NewInt(30), 100+hold
		}, ""},
		{"member queued inside its hold", func(g *types.Grant) {
			member(g)
			g.Paid, g.Remaining, g.DueTime = math.ZeroInt(), math.NewInt(30), 100+hold-1
		}, dueEarly},
		{"compensation in a stablecoin", func(g *types.Grant) { g.Amount = sdk.NewInt64Coin("ausd", 30) }, ""},
		{"ownership in a stablecoin", func(g *types.Grant) { ownership(g); g.Amount = sdk.NewInt64Coin("ausd", 30) }, "distribution grants require NOAH"},
		{"member without a reference", func(g *types.Grant) { member(g); g.Reference = " " }, "grant reference must not be empty"},
		{"member clock after its registration", func(g *types.Grant) { member(g); g.StartTime = 101 }, clock},
		{"ownership clock after its award", func(g *types.Grant) { ownership(g); g.StartTime = 101 }, ""},
		{"backdated ownership clock", func(g *types.Grant) { ownership(g); g.StartTime = 99 }, clock},
		{"clock a century after its award", func(g *types.Grant) {
			g.StartTime = 100 + types.MaxStartOffset
			g.DueTime = g.StartTime + hold
		}, ""},
		{"clock past a century after its award", func(g *types.Grant) {
			g.StartTime = 101 + types.MaxStartOffset
			g.DueTime = g.StartTime + hold
		}, clock},
		{"compensation period under the floor", stretch(hold - 1), floor},
		{"ownership first period under the floor", func(g *types.Grant) { ownership(g); g.Schedule.FirstLength = hold - 1 }, floor},
		{"member first period under the floor", func(g *types.Grant) { member(g); g.Schedule.FirstLength = hold - 1 }, floor},
		{"compensation with unequal periods", func(g *types.Grant) { g.Schedule.PeriodLength = 2 * hold }, compensation},
		{"compensation with unequal weights", func(g *types.Grant) { g.Schedule.FirstWeight = 2 }, compensation},
		{"compensation of nine years", stretch(3 * 365 * 86_400), ""},
		{"compensation past ten years", stretch(4 * 365 * 86_400), compensation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := base()
			tc.mutate(&g)
			err := g.Validate()
			if tc.err == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.err)
		})
	}
}

func TestValidateStartTime(t *testing.T) {
	// More than a century past the epoch, so a century back is still a positive time.
	now := 2*types.MaxStartOffset + 1_800_000_000
	const (
		century  = "start time must be within a century of the block"
		positive = "start time must be a positive Unix time"
	)
	for _, tc := range []struct {
		name  string
		start uint64
		now   uint64
		err   string
	}{
		{"now", now, now, ""},
		{"a year back", now - 365*86_400, now, ""},
		{"a year ahead", now + 365*86_400, now, ""},
		{"a century ahead", now + types.MaxStartOffset, now, ""},
		{"past a century ahead", now + types.MaxStartOffset + 1, now, century},
		{"a century back", now - types.MaxStartOffset, now, ""},
		{"past a century back", now - types.MaxStartOffset - 1, now, century},
		{"zero", 0, now, positive},
		{"past the header's int64", uint64(stdmath.MaxInt64) + 1, now, positive},
		{"a block past the header's int64", now, uint64(stdmath.MaxInt64) + 1, positive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := types.ValidateStartTime(tc.start, tc.now)
			if tc.err == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.err)
		})
	}
}

// TestAccrualAtEveryBound builds a grant at every accepted maximum at once: the longest, heaviest
// shape, a 128-bit amount, and a clock a century ahead of the latest award whose schedule still ends
// within the header's int64. The shares conserve and accrual runs to the whole amount without error;
// one past any bound is refused.
func TestAccrualAtEveryBound(t *testing.T) {
	address := sdk.AccAddress(make([]byte, 20)).String()
	amount := math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 128)).SubRaw(1)
	shape := types.Schedule{
		FirstLength: types.MaxPeriodLength, FirstWeight: types.MaxWeight,
		PeriodLength: types.MaxPeriodLength, PeriodWeight: types.MaxWeight, Periods: types.MaxSchedulePeriods,
	}
	created := uint64(stdmath.MaxInt64) - types.MaxStartOffset - shape.Duration()
	base := func() types.Grant {
		start := created + types.MaxStartOffset
		return types.Grant{
			Id: 1, Kind: types.GrantKind_GRANT_KIND_OWNERSHIP, Beneficiary: address, Payee: address, CreatedBy: address,
			Amount: sdk.NewCoin("anoah", amount), Schedule: shape, StartTime: start, CreatedTime: created, CreatedHeight: 1,
			Paid: math.ZeroInt(), Remaining: amount, Reference: "agreement", AwardValue: math.ZeroInt(), Charge: math.ZeroInt(),
			DueTime: start + shape.FirstLength,
		}
	}
	g := base()
	require.NoError(t, g.Validate())
	first, each, last, err := shape.Shares(amount)
	require.NoError(t, err)
	require.Equal(t, amount, first.Add(each.MulRaw(types.MaxSchedulePeriods-1)).Add(last))
	firstEnd, end := g.StartTime+shape.FirstLength, g.StartTime+shape.Duration()
	require.Equal(t, uint64(stdmath.MaxInt64), end, "the schedule ends at the last valid time")
	for _, tc := range []struct {
		name string
		at   uint64
		want math.Int
		next uint64
	}{
		{"before the first boundary", firstEnd - 1, math.ZeroInt(), firstEnd},
		{"at the first boundary", firstEnd, first, firstEnd + shape.PeriodLength},
		{"midway", firstEnd + 600*shape.PeriodLength, first.Add(each.MulRaw(600)), firstEnd + 601*shape.PeriodLength},
		{"just before the end", end - 1, amount.Sub(last), end},
		{"at the end", end, amount, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			earned, next, err := g.Accrued(tc.at)
			require.NoError(t, err)
			require.Equal(t, tc.want, earned)
			require.Equal(t, tc.next, next)
		})
	}
	const (
		firstBound   = "first period requires a bounded positive length and weight"
		furtherBound = "further periods require a bounded positive length and weight"
	)
	for _, tc := range []struct {
		name   string
		mutate func(*types.Grant)
		err    string
	}{
		{"a further period too many", func(g *types.Grant) { g.Schedule.Periods++ }, "schedule has at most 1200 further periods"},
		{"a first period past a century", func(g *types.Grant) { g.Schedule.FirstLength++ }, firstBound},
		{"a first weight past the bound", func(g *types.Grant) { g.Schedule.FirstWeight++ }, firstBound},
		{"a further period past a century", func(g *types.Grant) { g.Schedule.PeriodLength++ }, furtherBound},
		{"a further weight past the bound", func(g *types.Grant) { g.Schedule.PeriodWeight++ }, furtherBound},
		{"an amount past 128 bits", func(g *types.Grant) {
			g.Amount.Amount, g.Remaining = g.Amount.Amount.AddRaw(1), g.Remaining.AddRaw(1)
		}, "amount must be nonnegative and at most 128 bits"},
		{"a clock past a century ahead", func(g *types.Grant) { g.StartTime++; g.DueTime++ }, "grant clock does not fit its kind"},
		{"an award a second later", func(g *types.Grant) { g.CreatedTime++; g.StartTime++; g.DueTime++ }, "schedule end overflows time"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := base()
			tc.mutate(&g)
			require.ErrorContains(t, g.Validate(), tc.err)
		})
	}
}
