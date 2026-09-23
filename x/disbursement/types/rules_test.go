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

func TestParamsValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*types.Params)
		valid  bool
	}{
		{"defaults", func(*types.Params) {}, true},
		{"zero members", func(p *types.Params) { p.MaxMembers = 0 }, false},
		{"maximum members", func(p *types.Params) { p.MaxMembers = types.MaxIssuanceMembers }, true},
		{"excess members", func(p *types.Params) { p.MaxMembers = types.MaxIssuanceMembers + 1 }, false},
		{"zero window", func(p *types.Params) { p.WindowSeconds = 0 }, false},
		{"zero amount", func(p *types.Params) { p.MemberAmount = math.ZeroInt() }, false},
		{"nil amount", func(p *types.Params) { p.MemberAmount = math.Int{} }, false},
		{"excess amount", func(p *types.Params) { p.MemberAmount = math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 128)) }, false},
		{"empty schedule", func(p *types.Params) { p.MemberSchedule = nil }, false},
		{"zero length", func(p *types.Params) { p.MemberSchedule[0].Length = 0 }, false},
		{"zero parts", func(p *types.Params) { p.MemberSchedule[0].Parts = 0 }, false},
		{"dust period", func(p *types.Params) { p.MemberAmount = math.OneInt() }, false},
		{"duplicate denom", func(p *types.Params) { p.CompensationDenoms = []string{"anoah", "anoah"} }, false},
		{"external denom", func(p *types.Params) { p.CompensationDenoms = []string{"ibc/ABC"} }, false},
		{"native denoms", func(p *types.Params) { p.CompensationDenoms = []string{"anoah", "ausd"} }, true},
		{"disable compensation", func(p *types.Params) { p.CompensationDenoms = nil }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := types.DefaultParams()
			tc.mutate(&p)
			err := p.Validate()
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestScheduleSplit(t *testing.T) {
	for _, tc := range []struct {
		name    string
		amount  int64
		periods []types.Period
		want    []int64
		bad     bool
	}{
		{"remainder", 23, []types.Period{{Length: 10, Parts: 1}, {Length: 10, Parts: 1}, {Length: 10, Parts: 1}}, []int64{7, 7, 9}, false},
		{"exact", 40, []types.Period{{Length: 1, Parts: 1}, {Length: 20, Parts: 3}}, []int64{10, 30}, false},
		{"too small", 1, []types.Period{{Length: 1, Parts: 1}, {Length: 1, Parts: 1}}, nil, true},
		{"parts overflow", 100, []types.Period{{Length: 1, Parts: ^uint64(0)}, {Length: 1, Parts: 1}}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := types.Split(math.NewInt(tc.amount), tc.periods)
			if tc.bad {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			for i, n := range tc.want {
				require.Equal(t, math.NewInt(n), got[i])
			}
		})
	}
}

func TestRandomisedScheduleConservation(t *testing.T) {
	for _, seed := range []int64{1, 17, 982} {
		t.Run(big.NewInt(seed).String(), func(t *testing.T) {
			r := rand.New(rand.NewSource(seed))
			for range 100 {
				periods := make([]types.Period, 1+r.Intn(30))
				for i := range periods {
					periods[i] = types.Period{Length: uint64(1 + r.Intn(100)), Parts: uint64(1 + r.Intn(100))}
				}
				amount := math.NewInt(1_000_000 + r.Int63n(1_000_000))
				parts, err := types.Split(amount, periods)
				require.NoError(t, err)
				total := math.ZeroInt()
				for _, part := range parts {
					require.True(t, part.IsPositive())
					total = total.Add(part)
				}
				require.Equal(t, amount, total)
			}
		})
	}
}

func TestGenesisValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*types.GenesisState)
		valid  bool
	}{
		{"empty", func(*types.GenesisState) {}, true},
		{"zero next grant", func(g *types.GenesisState) { g.NextGrantId = 0 }, false},
		{"missing journal", func(g *types.GenesisState) { g.NextJournalId = 2 }, false},
		{"changed ownership fraction", func(g *types.GenesisState) { g.OwnershipPolicy.Numerator = 5 }, false},
		{"nil ownership ceiling", func(g *types.GenesisState) { g.OwnershipPolicy.Ceiling = math.Int{} }, false},
		{"future epoch nil address", func(g *types.GenesisState) { g.RegistrarEpochs = []types.RegistrarEpoch{{Epoch: 1}} }, false},
		{"unordered issuance", func(g *types.GenesisState) {
			g.Issuance.Entries = []types.IssuanceEntry{{At: 2, Count: 1}, {At: 1, Count: 1}}
		}, false},
		{"excess issuance", func(g *types.GenesisState) {
			g.Issuance.Entries = []types.IssuanceEntry{{At: 1, Count: types.MaxIssuanceMembers + 1}}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := types.DefaultGenesisState()
			tc.mutate(g)
			err := g.Validate()
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

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
				Schedule:  []types.Period{{Length: 10, Parts: 1}, {Length: 10, Parts: 1}, {Length: 10, Parts: 1}},
				Cancelled: tc.cancelled, CutoffTime: 120,
			}
			earned, next, err := types.Accrued(g, tc.now)
			require.NoError(t, err)
			require.Equal(t, math.NewInt(tc.want), earned)
			require.Equal(t, tc.next, next)
		})
	}
}
