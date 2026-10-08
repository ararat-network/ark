package types_test

import (
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
		{"member upfront", types.GrantKind_GRANT_KIND_MEMBER, 100, false, 7, 120},
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
			Amount: sdk.NewInt64Coin("anoah", 30), Schedule: types.Schedule{FirstLength: week, FirstWeight: 1, PeriodLength: week, PeriodWeight: 1, Periods: 2},
			StartTime: 100, CreatedTime: 100, Paid: math.NewInt(10), Remaining: math.NewInt(20), Reference: "work", CreatedHeight: 1, DueTime: 100 + 2*week,
			AwardValue: math.NewInt(30),
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
	for _, tc := range []struct {
		name   string
		mutate func(*types.Grant)
		valid  bool
	}{
		{"live and queued", func(*types.Grant) {}, true},
		{"live and unqueued", func(g *types.Grant) { g.DueTime = 0 }, false},
		{"compensation without an award value", func(g *types.Grant) { g.AwardValue = math.ZeroInt() }, false},
		{"member with an award value", func(g *types.Grant) { member(g); g.AwardValue = math.NewInt(30) }, false},
		{"queued before it started", func(g *types.Grant) { g.DueTime = 99 }, false},
		// Nothing is due before one period after the award, wherever the clock starts.
		{"queued at its gate", func(g *types.Grant) { g.DueTime = 100 + week }, true},
		{"queued inside its first period", func(g *types.Grant) { g.DueTime = 100 + week - 1 }, false},
		{"backdated clock queued at its gate", func(g *types.Grant) { g.StartTime, g.DueTime = 50, 100+week }, true},
		{"backdated clock queued before its gate", func(g *types.Grant) { g.StartTime, g.DueTime = 50, 100+week-1 }, false},
		{"finished and unqueued", func(g *types.Grant) { g.Paid, g.Remaining, g.DueTime = math.NewInt(30), math.ZeroInt(), 0 }, true},
		{"finished and queued", func(g *types.Grant) { g.Paid, g.Remaining = math.NewInt(30), math.ZeroInt() }, false},
		{"cancelled debt queued", func(g *types.Grant) { g.CutoffTime, g.Remaining = 100+2*week, math.NewInt(10) }, true},
		{"cancelled debt unqueued", func(g *types.Grant) { g.CutoffTime, g.Remaining, g.DueTime = 100+2*week, math.NewInt(10), 0 }, false},
		{"cancelled holding past its debt", func(g *types.Grant) { g.CutoffTime = 100 + 2*week }, false},
		{"cancelled at its award", func(g *types.Grant) {
			g.CutoffTime, g.Paid, g.Remaining, g.DueTime = 100, math.ZeroInt(), math.ZeroInt(), 0
		}, true},
		{"cancelled before its award", func(g *types.Grant) {
			g.CutoffTime, g.Paid, g.Remaining, g.DueTime = 99, math.ZeroInt(), math.ZeroInt(), 0
		}, false},
		{"cancelled member", func(g *types.Grant) { member(g); g.CutoffTime, g.Remaining, g.DueTime = 115, math.ZeroInt(), 0 }, true},
		{"cancelled while suspended", func(g *types.Grant) {
			member(g)
			g.CutoffTime, g.SuspendedTime, g.Remaining, g.DueTime = 115, 115, math.ZeroInt(), 0
		}, false},
		{"suspended member unqueued", func(g *types.Grant) { member(g); g.SuspendedTime, g.DueTime = 115, 0 }, true},
		{"suspended member queued", func(g *types.Grant) { member(g); g.SuspendedTime = 115 }, false},
		{"suspended member paid past its suspension", func(g *types.Grant) {
			member(g)
			g.SuspendedTime, g.DueTime, g.Paid, g.Remaining = 115, 0, math.NewInt(20), math.NewInt(10)
		}, false},
		{"committee compensation", func(g *types.Grant) { g.MandateTerm = 1 }, true},
		{"member without a term", func(g *types.Grant) { member(g); g.MandateTerm = 0 }, false},
		{"governance ownership", ownership, true},
		{"ownership under a term", func(g *types.Grant) { ownership(g); g.MandateTerm = 1 }, false},
		{"registered member", member, true},
		{"member short of its first period", func(g *types.Grant) { member(g); g.Paid, g.Remaining = math.NewInt(9), math.NewInt(21) }, false},
		{"compensation in a stablecoin", func(g *types.Grant) { g.Amount = sdk.NewInt64Coin("ausd", 30) }, true},
		{"ownership in a stablecoin", func(g *types.Grant) { ownership(g); g.Amount = sdk.NewInt64Coin("ausd", 30) }, false},
		{"member without a reference", func(g *types.Grant) { member(g); g.Reference = " " }, false},
		{"member clock after its registration", func(g *types.Grant) { member(g); g.StartTime = 101 }, false},
		{"ownership clock after its award", func(g *types.Grant) { ownership(g); g.StartTime = 101 }, true},
		{"backdated ownership clock", func(g *types.Grant) { ownership(g); g.StartTime = 99 }, false},
		{"clock a century after its award", func(g *types.Grant) {
			g.StartTime = 100 + types.MaxStartOffset
			g.DueTime = g.StartTime + week
		}, true},
		{"clock past a century after its award", func(g *types.Grant) {
			g.StartTime = 101 + types.MaxStartOffset
			g.DueTime = g.StartTime + week
		}, false},
		{"compensation period under a week", stretch(week - 1), false},
		{"compensation with unequal periods", func(g *types.Grant) { g.Schedule.PeriodLength = 2 * week }, false},
		{"compensation with unequal weights", func(g *types.Grant) { g.Schedule.FirstWeight = 2 }, false},
		{"compensation of nine years", stretch(3 * 365 * 86_400), true},
		{"compensation past ten years", stretch(4 * 365 * 86_400), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := base()
			tc.mutate(&g)
			err := g.Validate()
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestValidateStartTime(t *testing.T) {
	now := uint64(1_800_000_000)
	for _, tc := range []struct {
		name  string
		start uint64
		valid bool
	}{
		{"now", now, true},
		{"a year back", now - 365*86_400, true},
		{"a year ahead", now + 365*86_400, true},
		{"a century ahead", now + types.MaxStartOffset, true},
		{"past a century ahead", now + types.MaxStartOffset + 1, false},
		{"past a century back", now - types.MaxStartOffset - 1, false},
		{"zero", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := types.ValidateStartTime(tc.start, now)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
