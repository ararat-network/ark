package types_test

import (
	"bytes"
	"cmp"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/disbursement/types"
)

func TestDefaultGenesisState(t *testing.T) {
	gs := types.DefaultGenesisState()
	require.NoError(t, gs.Validate())
	require.True(t, gs.CompensationUsed.IsZero())
	require.True(t, gs.RegistrarMandate.IsDisabled())
	for _, next := range []uint64{gs.NextGrantId, gs.NextGrantEntryId, gs.NextBeneficiaryEntryId} {
		require.Equal(t, uint64(1), next)
	}
}

func testAddress(b byte) string { return sdk.AccAddress(bytes.Repeat([]byte{b}, 20)).String() }

// hold is the shortest first period, the two-hold floor every grant waits out (D126).
const hold = types.MinFirstPeriod

// grantEntry is a grant journal entry the fixture's authority records at the fixture's creation.
func grantEntry(id, grant uint64, action types.GrantAction, amount int64, subject string) types.GrantEntry {
	return types.GrantEntry{Id: id, GrantId: grant, Action: action, Actor: testAddress(9), Amount: chain.NoahCoin(math.NewInt(amount)), Subject: subject, Offer: math.ZeroInt(), Height: 1, Time: 100}
}

// validGenesis holds a member paid its first period, a compensation award one payment in, and a
// beneficiary without grants, every list in storage-key order.
func validGenesis() *types.GenesisState {
	a, b, c, authority := testAddress(1), testAddress(2), testAddress(3), testAddress(9)
	gs := types.DefaultGenesisState()
	gs.Beneficiaries = []types.Beneficiary{
		{Address: b, Controller: b, OwnershipCommitted: math.ZeroInt(), Seat: math.ZeroInt()},
		{Address: c, Controller: c, OwnershipCommitted: math.ZeroInt(), Seat: math.ZeroInt()},
	}
	gs.Grants = []types.Grant{
		{
			Id: 1, Kind: types.GrantKind_GRANT_KIND_MEMBER, Beneficiary: a, Payee: a, CreatedBy: authority,
			Amount: chain.NoahCoin(math.NewInt(10_000)), Schedule: types.Schedule{FirstLength: hold, FirstWeight: 1, PeriodLength: 10, PeriodWeight: 3, Periods: 3},
			StartTime: 100, CreatedTime: 100, CreatedHeight: 1, Paid: math.NewInt(1_000), Remaining: math.NewInt(9_000), Reference: "admission batch", AwardValue: math.ZeroInt(), DueTime: 110 + hold,
			MandateTerm: 1,
		},
		{
			Id: 2, Kind: types.GrantKind_GRANT_KIND_COMPENSATION, Beneficiary: b, Payee: b, CreatedBy: authority,
			Amount: chain.NoahCoin(math.NewInt(30)), Schedule: types.Schedule{FirstLength: hold, FirstWeight: 1, PeriodLength: hold, PeriodWeight: 1, Periods: 2},
			StartTime: 100, CreatedTime: 100, CreatedHeight: 1, Paid: math.NewInt(10), Remaining: math.NewInt(20), Reference: "work", AwardValue: math.NewInt(30), DueTime: 100 + 2*hold,
		},
	}
	gs.NextGrantId = 3
	// The member was registered under the live registrar term, appointed inside the committee's window.
	gs.GrantsMandate = types.NewDisabledGrantsMandate(1)
	gs.GrantsMandate.Committee, gs.GrantsMandate.ActivationTime, gs.GrantsMandate.ExpiryTime = testAddress(7), 1, 1_000
	gs.RegistrarMandate = types.NewDisabledRegistrarMandate(1)
	gs.RegistrarMandate.Committee, gs.RegistrarMandate.ActivationTime, gs.RegistrarMandate.ExpiryTime = testAddress(8), 1, 1_000
	members, contributors := types.EmptyBook(types.MembersName), types.EmptyBook(types.ContributorsName)
	members.Reserved = sdk.NewCoins(chain.NoahCoin(math.NewInt(9_000)))
	contributors.Reserved = sdk.NewCoins(chain.NoahCoin(math.NewInt(20)))
	gs.Books = []types.AccountBook{contributors, members}
	gs.GrantJournal = []types.GrantEntry{
		grantEntry(1, 1, types.GrantAction_GRANT_ACTION_CREATED, 10_000, ""),
		grantEntry(2, 1, types.GrantAction_GRANT_ACTION_PAID, 1_000, a),
		grantEntry(3, 2, types.GrantAction_GRANT_ACTION_CREATED, 30, ""),
		grantEntry(4, 2, types.GrantAction_GRANT_ACTION_PAID, 10, b),
	}
	gs.NextGrantEntryId = 5
	return gs
}

func TestGenesisValidation(t *testing.T) {
	a, b := testAddress(1), testAddress(2)
	const (
		cancelled  = types.GrantAction_GRANT_ACTION_CANCELLED
		suspended  = types.GrantAction_GRANT_ACTION_SUSPENDED
		reinstated = types.GrantAction_GRANT_ACTION_REINSTATED
	)
	// record adds entries to the grant journal in storage-key order.
	record := func(g *types.GenesisState, entries ...types.GrantEntry) {
		g.GrantJournal = append(g.GrantJournal, entries...)
		slices.SortFunc(g.GrantJournal, func(x, y types.GrantEntry) int {
			return cmp.Or(cmp.Compare(x.GrantId, y.GrantId), cmp.Compare(x.Id, y.Id))
		})
		g.NextGrantEntryId = uint64(len(g.GrantJournal)) + 1
	}
	// cut cancels the compensation award after its third period, fully earned: it still owes 20 and frees nothing.
	cut := func(g *types.GenesisState) { g.Grants[1].CutoffTime, g.Grants[1].DueTime = 105+3*hold, 105+3*hold }
	// suspend suspends the member five seconds past its hold, its first period paid.
	suspend := func(g *types.GenesisState) { g.Grants[0].SuspendedTime, g.Grants[0].DueTime = 105+hold, 0 }
	// at stamps an entry with its block time.
	at := func(e types.GrantEntry, time uint64) types.GrantEntry {
		e.Time = time
		return e
	}
	// stablecoin makes the compensation award ausd, funded by a conversion recorded as entry 4 in its
	// creation's write; its payment moves to entry 5.
	stablecoin := func(g *types.GenesisState) {
		usd := func(n int64) sdk.Coin { return sdk.NewInt64Coin("ausd", n) }
		g.Grants[1].Amount = usd(30)
		g.Books[0].Reserved = sdk.NewCoins(usd(20))
		created, converted, paid := g.GrantJournal[2], grantEntry(4, 2, types.GrantAction_GRANT_ACTION_CONVERTED, 0, ""), g.GrantJournal[3]
		created.Amount, converted.Amount, converted.Offer, paid.Amount, paid.Id = usd(30), usd(30), math.NewInt(31), usd(10), 5
		g.GrantJournal = append(g.GrantJournal[:2], created, converted, paid)
		g.NextGrantEntryId = 6
	}
	// rotate gives b and a 32-byte contract beneficiary new controllers. The contract sorts first by
	// bytes, but the store walks b's entry first, its address being shorter.
	rotate := func(byteOrder bool) func(*types.GenesisState) {
		return func(g *types.GenesisState) {
			contract := sdk.AccAddress(bytes.Repeat([]byte{1}, 32)).String()
			g.Beneficiaries = append([]types.Beneficiary{{Address: contract, Controller: a, OwnershipCommitted: math.ZeroInt(), Seat: math.ZeroInt()}}, g.Beneficiaries...)
			g.Beneficiaries[1].Controller = a
			g.BeneficiaryJournal = []types.BeneficiaryEntry{
				{Id: 1, Beneficiary: b, Actor: b, Controller: a, Height: 1, Time: 100},
				{Id: 2, Beneficiary: contract, Actor: contract, Controller: a, Height: 1, Time: 100},
			}
			if byteOrder {
				g.BeneficiaryJournal[0], g.BeneficiaryJournal[1] = g.BeneficiaryJournal[1], g.BeneficiaryJournal[0]
			}
			g.NextBeneficiaryEntryId = 3
		}
	}
	// owned gives c an ownership award of four shares of 10, one paid as its first period ends, cancelled
	// fifteen seconds later owing the second: the record commits the 20 it earned (D121).
	owned := func(g *types.GenesisState) {
		c := testAddress(3)
		g.Grants = append(g.Grants, types.Grant{
			Id: 3, Kind: types.GrantKind_GRANT_KIND_OWNERSHIP, Beneficiary: c, Payee: c, CreatedBy: testAddress(9),
			Amount: chain.NoahCoin(math.NewInt(40)), Schedule: types.Schedule{FirstLength: hold, FirstWeight: 1, PeriodLength: 10, PeriodWeight: 1, Periods: 3},
			StartTime: 100, CreatedTime: 100, CreatedHeight: 1, Paid: math.NewInt(10), Remaining: math.NewInt(10), Reference: "agreement",
			AwardValue: math.ZeroInt(), CutoffTime: 115 + hold, DueTime: 115 + hold,
		})
		g.NextGrantId = 4
		g.Beneficiaries[1].OwnershipCommitted = math.NewInt(20)
		g.Books[0].Reserved = sdk.NewCoins(chain.NoahCoin(math.NewInt(30)))
		record(g,
			grantEntry(5, 3, types.GrantAction_GRANT_ACTION_CREATED, 40, ""),
			at(grantEntry(6, 3, types.GrantAction_GRANT_ACTION_PAID, 10, c), 100+hold),
			at(grantEntry(7, 3, cancelled, 20, ""), 115+hold),
		)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*types.GenesisState)
		err    string
	}{
		{"valid", func(*types.GenesisState) {}, ""},
		{"books out of order", func(g *types.GenesisState) { g.Books[0], g.Books[1] = g.Books[1], g.Books[0] }, "sorted by unique account"},
		{"duplicate book", func(g *types.GenesisState) { g.Books[1] = g.Books[0] }, "sorted by unique account"},
		{"beneficiaries out of order", func(g *types.GenesisState) {
			g.Beneficiaries[0], g.Beneficiaries[1] = g.Beneficiaries[1], g.Beneficiaries[0]
		}, "sorted by unique address"},
		{"duplicate beneficiary", func(g *types.GenesisState) { g.Beneficiaries[1] = g.Beneficiaries[0] }, "sorted by unique address"},
		{"grants out of order", func(g *types.GenesisState) { g.Grants[0], g.Grants[1] = g.Grants[1], g.Grants[0] }, "without a gap"},
		{"grant ID gap", func(g *types.GenesisState) { g.Grants[1].Id = 3 }, "without a gap"},
		{"next grant ID behind the records", func(g *types.GenesisState) { g.NextGrantId = 2 }, "does not follow"},
		{"grant journal out of order", func(g *types.GenesisState) {
			g.GrantJournal[0], g.GrantJournal[1] = g.GrantJournal[1], g.GrantJournal[0]
		}, "sorted by unique grant and entry ID"},
		{"grant journal interleaved", func(g *types.GenesisState) {
			g.GrantJournal[1], g.GrantJournal[2] = g.GrantJournal[2], g.GrantJournal[1]
		}, "sorted by unique grant and entry ID"},
		{"grant entry ID past the history", func(g *types.GenesisState) { g.GrantJournal[3].Id = 5 }, "run from one without a gap"},
		{"grant entry ID reused across grants", func(g *types.GenesisState) { g.GrantJournal[2].Id = 2 }, "run from one without a gap"},
		{"next grant entry ID behind the history", func(g *types.GenesisState) { g.NextGrantEntryId = 4 }, "does not follow"},
		{"reservations differ from grants", func(g *types.GenesisState) { g.Books[1].Reserved = sdk.NewCoins(chain.NoahCoin(math.NewInt(8_999))) }, "reservations"},
		{"term usage differs from awards", func(g *types.GenesisState) { g.CompensationUsed = math.OneInt() }, "term usage"},
		{"negative member tranche", func(g *types.GenesisState) { g.MemberTranche = math.NewInt(-1) }, "member tranche"},
		{"open member tranche", func(g *types.GenesisState) { g.MemberTranche = math.NewInt(1_000) }, ""},
		{"ownership differs from grants", func(g *types.GenesisState) { g.Beneficiaries[0].OwnershipCommitted = math.OneInt() }, "ownership committed"},
		{"cancelled ownership commits what it earned", owned, ""},
		{"ownership committed as paid alone", func(g *types.GenesisState) { owned(g); g.Beneficiaries[1].OwnershipCommitted = math.NewInt(10) }, "ownership committed"},
		{"ownership committed as awarded", func(g *types.GenesisState) { owned(g); g.Beneficiaries[1].OwnershipCommitted = math.NewInt(40) }, "ownership committed"},
		{"payments differ from the journal", func(g *types.GenesisState) { g.GrantJournal[1].Amount = chain.NoahCoin(math.NewInt(999)) }, "differs from its journal"},
		{"grant without a beneficiary record", func(g *types.GenesisState) { g.Grants[1].Beneficiary = a }, "no beneficiary record"},
		{"entry naming an unknown grant", func(g *types.GenesisState) { g.GrantJournal[3].GrantId = 9 }, "unknown grant 9"},
		{"creation entry differs from its grant", func(g *types.GenesisState) { g.GrantJournal[0].Amount = chain.NoahCoin(math.NewInt(9_999)) }, "creation"},
		{"payee differs from the journal", func(g *types.GenesisState) { g.Grants[0].Payee = b }, "journal leads to"},
		{"journal opening with a payment", func(g *types.GenesisState) {
			paid, created := g.GrantJournal[1], g.GrantJournal[0]
			paid.Id, created.Id = 1, 2
			g.GrantJournal[0], g.GrantJournal[1] = paid, created
		}, "must open with its creation"},
		{"creation recorded twice", func(g *types.GenesisState) {
			record(g, grantEntry(5, 1, types.GrantAction_GRANT_ACTION_CREATED, 10_000, ""))
		}, "must open with its creation, once"},
		{"grant without a journal", func(g *types.GenesisState) { g.GrantJournal, g.NextGrantEntryId = g.GrantJournal[:2], 3 }, "grant 2 has no journal"},
		{"entry in another denomination", func(g *types.GenesisState) { g.GrantJournal[3].Amount = sdk.NewInt64Coin("ausd", 10) }, "moves ausd; grant 2 pays"},
		{"conversion for a NOAH grant", func(g *types.GenesisState) {
			converted := grantEntry(5, 2, types.GrantAction_GRANT_ACTION_CONVERTED, 30, "")
			converted.Offer = math.NewInt(31)
			record(g, converted)
		}, "converts for NOAH grant 2"},
		{"payee change to the current payee", func(g *types.GenesisState) {
			record(g, grantEntry(5, 2, types.GrantAction_GRANT_ACTION_PAYEE_CHANGED, 0, b))
		}, "already pays"},
		{"two grants registering one member", func(g *types.GenesisState) {
			g.Grants[1].Kind, g.Grants[1].Beneficiary, g.Grants[1].Payee = types.GrantKind_GRANT_KIND_MEMBER, a, a
			g.Grants[1].AwardValue, g.Grants[1].MandateTerm = math.ZeroInt(), 1
		}, "both register member"},
		{"committee term beyond the live term", func(g *types.GenesisState) { g.Grants[1].MandateTerm = 2 }, "committee term 2 beyond"},
		{"registrar term beyond the live term", func(g *types.GenesisState) { g.Grants[0].MandateTerm = 2 }, "registrar term 2 beyond"},
		{"member without a term", func(g *types.GenesisState) { g.Grants[0].MandateTerm = 0 }, "registrar term"},
		{"registrar under a disabled committee", func(g *types.GenesisState) { g.GrantsMandate = types.NewDisabledGrantsMandate(1) }, "disabled grants mandate"},
		{"registrar before the committee's window", func(g *types.GenesisState) { g.RegistrarMandate.ActivationTime = 0 }, "inside the committee's"},
		{"registrar past the committee's window", func(g *types.GenesisState) { g.RegistrarMandate.ExpiryTime = 1_001 }, "inside the committee's"},
		{"disabled registrar under a disabled committee", func(g *types.GenesisState) {
			g.GrantsMandate, g.RegistrarMandate = types.NewDisabledGrantsMandate(1), types.NewDisabledRegistrarMandate(2)
		}, ""},
		{"disabled registrar with a window", func(g *types.GenesisState) {
			g.RegistrarMandate = types.NewDisabledRegistrarMandate(2)
			g.RegistrarMandate.ExpiryTime = 10
		}, "registrar mandate"},
		{"registrar term advanced past its registrations", func(g *types.GenesisState) {
			g.RegistrarMandate = types.NewDisabledRegistrarMandate(2)
		}, ""},
		{"payment to a replaced payee", func(g *types.GenesisState) { g.GrantJournal[3].Subject = a }, "was paying"},
		{"cancelled grant records its cancellation", func(g *types.GenesisState) { cut(g); record(g, at(grantEntry(5, 2, cancelled, 0, ""), 105+3*hold)) }, ""},
		{"cancelled grant missing its cancellation", cut, "cancelled false"},
		{"live grant recording a cancellation", func(g *types.GenesisState) { record(g, grantEntry(5, 2, cancelled, 0, "")) }, "cancelled true"},
		{"cancellation recorded twice", func(g *types.GenesisState) {
			cut(g)
			record(g, at(grantEntry(5, 2, cancelled, 0, ""), 105+3*hold), at(grantEntry(6, 2, cancelled, 0, ""), 105+3*hold))
		}, "cancels grant 2 again"},
		{"suspended member records its suspension", func(g *types.GenesisState) { suspend(g); record(g, at(grantEntry(5, 1, suspended, 0, ""), 105+hold)) }, ""},
		{"suspended member missing its suspension", suspend, "suspended false"},
		{"cancellation clears a suspension", func(g *types.GenesisState) {
			// Cancelled at its suspension, the member keeps the first period it was paid.
			g.Grants[0].CutoffTime, g.Grants[0].Remaining, g.Grants[0].DueTime = 105+hold, math.ZeroInt(), 0
			g.Books[1].Reserved = sdk.NewCoins()
			record(g, at(grantEntry(5, 1, suspended, 0, ""), 105+hold), at(grantEntry(6, 1, cancelled, 9_000, ""), 110+hold))
		}, ""},
		{"reinstatement out of turn", func(g *types.GenesisState) { record(g, grantEntry(5, 1, reinstated, 0, "")) }, "out of turn"},
		{"suspension of a contributor grant", func(g *types.GenesisState) { record(g, grantEntry(5, 2, suspended, 0, "")) }, "out of turn"},
		{"journal time running back", func(g *types.GenesisState) { g.GrantJournal[1].Time = 101 }, "grant entry 3 is recorded before entry 2"},
		{"journal height running back", func(g *types.GenesisState) { g.GrantJournal[1].Height = 2 }, "grant entry 3 is recorded before entry 2"},
		{"suspension at another time", func(g *types.GenesisState) { suspend(g); record(g, at(grantEntry(5, 1, suspended, 0, ""), 106+hold)) }, fmt.Sprintf("suspended at %d", 105+hold)},
		{"cancellation at another time", func(g *types.GenesisState) { cut(g); record(g, at(grantEntry(5, 2, cancelled, 0, ""), 106+3*hold)) }, "cut off at"},
		{"conversion funds its award", stablecoin, ""},
		{"conversion after a payment", func(g *types.GenesisState) {
			stablecoin(g)
			g.GrantJournal[3], g.GrantJournal[4] = g.GrantJournal[4], g.GrantJournal[3]
			g.GrantJournal[3].Id, g.GrantJournal[4].Id = 4, 5
		}, "does not fund"},
		{"second conversion", func(g *types.GenesisState) {
			stablecoin(g)
			second := g.GrantJournal[3]
			second.Id = 6
			record(g, second)
		}, "does not fund"},
		{"conversion in a later block", func(g *types.GenesisState) {
			stablecoin(g)
			g.GrantJournal[3].Time, g.GrantJournal[4].Time = 101, 101
		}, "does not fund"},
		{"beneficiary journal orders addresses by length first", rotate(false), ""},
		{"beneficiary journal in byte order", rotate(true), "sorted by unique beneficiary"},
		{"beneficiary journal time running back", func(g *types.GenesisState) {
			rotate(false)(g)
			g.BeneficiaryJournal[0].Time = 101
		}, "beneficiary entry 2 is recorded before entry 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := validGenesis()
			tc.mutate(g)
			err := g.Validate()
			if tc.err == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.err)
			}
		})
	}
}

func TestGenesisBeneficiaryJournal(t *testing.T) {
	a, b, c := sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String(), sdk.AccAddress(bytes.Repeat([]byte{2}, 20)).String(), sdk.AccAddress(bytes.Repeat([]byte{3}, 20)).String()
	entry := func(id uint64, controller string) types.BeneficiaryEntry {
		return types.BeneficiaryEntry{Id: id, Beneficiary: a, Actor: a, Height: 1, Time: 1, Controller: controller}
	}
	for _, tc := range []struct {
		name   string
		mutate func(*types.GenesisState)
		valid  bool
	}{
		{"no entries", func(*types.GenesisState) {}, true},
		{"one entry", func(g *types.GenesisState) {
			g.Beneficiaries[0].Controller = b
			g.BeneficiaryJournal, g.NextBeneficiaryEntryId = []types.BeneficiaryEntry{entry(1, b)}, 2
		}, true},
		{"two entries", func(g *types.GenesisState) {
			g.Beneficiaries[0].Controller = c
			g.BeneficiaryJournal, g.NextBeneficiaryEntryId = []types.BeneficiaryEntry{entry(1, b), entry(2, c)}, 3
		}, true},
		{"entries out of order", func(g *types.GenesisState) {
			g.Beneficiaries[0].Controller = c
			g.BeneficiaryJournal, g.NextBeneficiaryEntryId = []types.BeneficiaryEntry{entry(2, c), entry(1, b)}, 3
		}, false},
		{"back to the address", func(g *types.GenesisState) {
			g.BeneficiaryJournal, g.NextBeneficiaryEntryId = []types.BeneficiaryEntry{entry(1, b), entry(2, a)}, 3
		}, true},
		{"controller without an entry", func(g *types.GenesisState) { g.Beneficiaries[0].Controller = b }, false},
		{"entry without the controller", func(g *types.GenesisState) {
			g.BeneficiaryJournal, g.NextBeneficiaryEntryId = []types.BeneficiaryEntry{entry(1, b)}, 2
		}, false},
		{"entry that changes nothing", func(g *types.GenesisState) {
			g.BeneficiaryJournal, g.NextBeneficiaryEntryId = []types.BeneficiaryEntry{entry(1, a)}, 2
		}, false},
		{"unknown beneficiary", func(g *types.GenesisState) {
			e := entry(1, c)
			e.Beneficiary = b
			g.BeneficiaryJournal, g.NextBeneficiaryEntryId = []types.BeneficiaryEntry{e}, 2
		}, false},
		{"gap in IDs", func(g *types.GenesisState) {
			g.Beneficiaries[0].Controller = b
			g.BeneficiaryJournal, g.NextBeneficiaryEntryId = []types.BeneficiaryEntry{entry(2, b)}, 3
		}, false},
		{"gap inside the history", func(g *types.GenesisState) {
			g.Beneficiaries[0].Controller = c
			g.BeneficiaryJournal, g.NextBeneficiaryEntryId = []types.BeneficiaryEntry{entry(1, b), entry(3, c)}, 3
		}, false},
		{"next ID behind history", func(g *types.GenesisState) {
			g.Beneficiaries[0].Controller = b
			g.BeneficiaryJournal, g.NextBeneficiaryEntryId = []types.BeneficiaryEntry{entry(1, b)}, 1
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := types.DefaultGenesisState()
			g.Beneficiaries = []types.Beneficiary{{Address: a, Controller: a, OwnershipCommitted: math.ZeroInt(), Seat: math.ZeroInt()}}
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
