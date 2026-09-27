package app_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	query "github.com/cosmos/cosmos-sdk/types/query"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/ararat-network/ark/app"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	disbursementkeeper "github.com/ararat-network/ark/x/disbursement/keeper"
	disbursementtypes "github.com/ararat-network/ark/x/disbursement/types"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

type nativeDisbursementFixture struct {
	app                  *app.ArkApp
	ctx                  sdk.Context
	committee, authority sdk.AccAddress
	term                 uint64
	query                disbursementtypes.QueryServer
}

func nativeAddress(name string) sdk.AccAddress {
	return authtypes.NewModuleAddress("native-disbursement-" + name)
}
func nativeNoah(n int64) math.Int { return chain.NativeBaseAmount(n) }

func newNativeDisbursement(t *testing.T, founders ...sdk.AccAddress) *nativeDisbursementFixture {
	t.Helper()
	a := apptestutil.Setup(t, false)
	f := &nativeDisbursementFixture{app: a, ctx: a.NewContextLegacy(false, cmtproto.Header{Height: 10, Time: time.Unix(1_800_000_000, 0)}), committee: nativeAddress("committee"), authority: authtypes.NewModuleAddress(govtypes.ModuleName), query: disbursementkeeper.NewQueryServerImpl(a.DisbursementKeeper)}
	g := disbursementtypes.DefaultGenesisState()
	for _, founder := range founders {
		g.Beneficiaries = append(g.Beneficiaries, disbursementtypes.Beneficiary{Address: founder.String(), Controller: founder.String(), OwnershipPaid: math.ZeroInt(), Seat: nativeNoah(chain.SeatGrantNoah)})
	}
	require.NoError(t, a.DisbursementKeeper.InitGenesis(f.ctx, g))
	p := g.Params
	p.MemberAmount = math.NewInt(10_000)
	p.MemberSchedule = []disbursementtypes.Period{{Length: 1, Parts: 1}, {Length: 10, Parts: 3}, {Length: 10, Parts: 6}}
	require.NoError(t, f.exec(&disbursementtypes.MsgUpdateParams{Authority: f.authority.String(), Params: p}))
	f.appoint(t, f.committee)
	return f
}

// appoint replaces the committee for a long window; every call advances the term.
func (f *nativeDisbursementFixture) appoint(t *testing.T, committee sdk.AccAddress) {
	t.Helper()
	f.appointWith(t, committee, nil, 0)
}

// appointWith appoints the committee with a compensation allowance and minimum first period.
func (f *nativeDisbursementFixture) appointWith(t *testing.T, committee sdk.AccAddress, allowance sdk.Coins, minFirstPeriod uint64) {
	t.Helper()
	height := uint64(f.ctx.BlockHeight())
	require.NoError(t, f.exec(&disbursementtypes.MsgSetGrantsMandate{
		Authority: f.authority.String(), Committee: committee.String(), ActivationHeight: height, ExpiryHeight: height + 1_000_000,
		CompensationAllowance: allowance, MinFirstPeriod: minFirstPeriod,
	}))
	f.term++
}

func (f *nativeDisbursementFixture) exec(msgs ...sdk.Msg) error {
	ctx, write := f.ctx.CacheContext()
	for _, msg := range msgs {
		h := f.app.MsgServiceRouter().Handler(msg)
		if h == nil {
			return errors.New("unregistered grant message")
		}
		if _, err := h(ctx, msg); err != nil {
			return err
		}
	}
	write()
	return nil
}

// fund deposits into custody outside both pools, as a community pool spend does.
func (f *nativeDisbursementFixture) fund(t *testing.T, coin sdk.Coin) {
	t.Helper()
	funder := nativeAddress("funder")
	coins := sdk.NewCoins(coin)
	apptestutil.FundAccount(t, f.app, f.ctx, funder, coins)
	require.NoError(t, f.app.DistrKeeper.FundCommunityPool(f.ctx, coins, funder))
	require.NoError(t, f.exec(&distrtypes.MsgCommunityPoolSpend{Authority: f.authority.String(), Recipient: authtypes.NewModuleAddress(disbursementtypes.ModuleName).String(), Amount: coins}))
}

func (f *nativeDisbursementFixture) poolItem(pool disbursementtypes.Pool) collections.Item[disbursementtypes.PoolBalance] {
	if pool == disbursementtypes.Pool_POOL_CONTRIBUTORS {
		return f.app.DisbursementKeeper.ContributorPool
	}
	return f.app.DisbursementKeeper.MemberPool
}

func (f *nativeDisbursementFixture) poolBalance(t *testing.T, pool disbursementtypes.Pool) disbursementtypes.PoolBalance {
	t.Helper()
	p, err := f.poolItem(pool).Get(f.ctx)
	require.NoError(t, err)
	return p
}

// seed stands in for genesis funding: only genesis grows a pool.
func (f *nativeDisbursementFixture) seed(t *testing.T, pool disbursementtypes.Pool, amount math.Int) {
	t.Helper()
	apptestutil.FundModule(t, f.app, f.ctx, disbursementtypes.ModuleName, sdk.NewCoins(chain.NoahCoin(amount)))
	p := f.poolBalance(t, pool)
	p.Unallocated = p.Unallocated.Add(amount)
	require.NoError(t, f.poolItem(pool).Set(f.ctx, p))
}

// open seeds a pool and opens the same amount as governance would for a step.
func (f *nativeDisbursementFixture) open(t *testing.T, pool disbursementtypes.Pool, amount math.Int) {
	t.Helper()
	f.seed(t, pool, amount)
	require.NoError(t, f.exec(&disbursementtypes.MsgOpenTranche{Authority: f.authority.String(), Pool: pool, Amount: amount}))
}

func (f *nativeDisbursementFixture) advance(seconds int64) {
	f.ctx = f.ctx.WithBlockTime(f.ctx.BlockTime().Add(time.Duration(seconds) * time.Second)).WithBlockHeight(f.ctx.BlockHeight() + 1)
}

func (f *nativeDisbursementFixture) grant(t *testing.T, id uint64) disbursementtypes.Grant {
	t.Helper()
	g, err := f.app.DisbursementKeeper.Grants.Get(f.ctx, id)
	require.NoError(t, err)
	return g
}

func (f *nativeDisbursementFixture) register(t *testing.T, address sdk.AccAddress) uint64 {
	t.Helper()
	id, err := f.app.DisbursementKeeper.NextGrantID.Peek(f.ctx)
	require.NoError(t, err)
	require.NoError(t, f.exec(&disbursementtypes.MsgCommitteeRegister{Committee: f.committee.String(), ExpectedTerm: f.term, Addresses: []string{address.String()}}))
	return id
}

func (f *nativeDisbursementFixture) create(t *testing.T, kind disbursementtypes.GrantKind, address sdk.AccAddress, coin sdk.Coin, schedule []disbursementtypes.Period) uint64 {
	t.Helper()
	id, err := f.app.DisbursementKeeper.NextGrantID.Peek(f.ctx)
	require.NoError(t, err)
	require.NoError(t, f.exec(&disbursementtypes.MsgCreateGrant{Authority: f.authority.String(), Kind: kind, Beneficiary: address.String(), Amount: coin, Schedule: schedule, Reference: "Public contributor agreement"}))
	return id
}

func (f *nativeDisbursementFixture) release(id uint64) error {
	return f.exec(&disbursementtypes.MsgRelease{Sender: nativeAddress("caller").String(), GrantIds: []uint64{id}})
}

func (f *nativeDisbursementFixture) check(t *testing.T) *disbursementtypes.GenesisState {
	t.Helper()
	g, err := f.app.DisbursementKeeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, g.Validate())
	for _, totals := range g.Totals {
		balance, err := f.query.Balance(f.ctx, &disbursementtypes.QueryBalanceRequest{Denom: totals.Denom})
		require.NoError(t, err)
		held := totals.Reserved.Add(balance.MemberPool.Unallocated).Add(balance.ContributorPool.Unallocated).Add(balance.Converting)
		require.Equal(t, balance.Balance.Amount, balance.Unallocated.Add(held))
	}
	return g
}

func TestNativeDisbursementMemberCancellation(t *testing.T) {
	for _, suspended := range []bool{false, true} {
		t.Run(fmt.Sprintf("suspended_%t", suspended), func(t *testing.T) {
			f := newNativeDisbursement(t)
			member := nativeAddress("member")
			payee := nativeAddress("new-payee")
			f.open(t, disbursementtypes.Pool_POOL_MEMBERS, math.NewInt(10_000))
			id := f.register(t, member)
			require.Equal(t, math.NewInt(1_000), f.app.BankKeeper.GetBalance(f.ctx, member, chain.NoahBaseDenom).Amount)
			f.advance(11)
			if suspended {
				require.NoError(t, f.exec(&disbursementtypes.MsgCommitteeSuspend{Committee: f.committee.String(), ExpectedTerm: f.term, Addresses: []string{member.String()}}))
				f.advance(10)
			}
			// A Bank refusal cannot veto cancellation or free the retained debt.
			f.app.BankKeeper.AppendSendRestriction(func(_ context.Context, _ sdk.AccAddress, to sdk.AccAddress, _ sdk.Coins) (sdk.AccAddress, error) {
				if to.Equals(member) {
					return nil, errors.New("blocked destination")
				}
				return to, nil
			})
			require.NoError(t, f.exec(&disbursementtypes.MsgCancelGrants{Authority: f.authority.String(), GrantIds: []uint64{id}}))
			g := f.grant(t, id)
			require.True(t, g.Cancelled)
			require.Equal(t, math.NewInt(3_000), g.Remaining)
			require.Equal(t, math.NewInt(6_000), g.CancelledAmount)
			snapshot := f.check(t)
			require.ErrorContains(t, f.release(id), "blocked destination")
			require.Equal(t, snapshot, f.check(t))
			require.Error(t, f.exec(&disbursementtypes.MsgSetPayee{Sender: f.committee.String(), GrantId: id, Payee: payee.String()}))
			require.NoError(t, f.exec(&disbursementtypes.MsgSetPayee{Sender: member.String(), GrantId: id, Payee: payee.String()}))
			f.advance(100)
			require.NoError(t, f.release(id))
			require.Equal(t, math.NewInt(3_000), f.app.BankKeeper.GetBalance(f.ctx, payee, chain.NoahBaseDenom).Amount)
			require.Error(t, f.exec(&disbursementtypes.MsgCommitteeRegister{Committee: f.committee.String(), ExpectedTerm: f.term, Addresses: []string{member.String()}}))
			require.Error(t, f.exec(&disbursementtypes.MsgCommitteeReinstate{Committee: f.committee.String(), ExpectedTerm: f.term, Addresses: []string{member.String()}}))
			f.check(t)
		})
	}
}

func TestNativeDisbursementSuspensionRecovery(t *testing.T) {
	for _, mode := range []string{"committee", "governance", "void"} {
		t.Run(mode, func(t *testing.T) {
			f := newNativeDisbursement(t)
			member := nativeAddress("member")
			f.open(t, disbursementtypes.Pool_POOL_MEMBERS, math.NewInt(10_000))
			id := f.register(t, member)
			require.NoError(t, f.exec(&disbursementtypes.MsgCommitteeSuspend{Committee: f.committee.String(), ExpectedTerm: f.term, Addresses: []string{member.String()}}))
			f.advance(11)
			require.ErrorContains(t, f.release(id), "nothing is payable")
			switch mode {
			case "void":
				// The live term is never voided; re-appointing the same account advances it.
				require.Error(t, f.exec(&disbursementtypes.MsgVoidSuspensions{Authority: f.authority.String(), Term: f.term}))
				previous := f.term
				f.appoint(t, f.committee)
				require.NoError(t, f.exec(&disbursementtypes.MsgVoidSuspensions{Authority: f.authority.String(), Term: previous}))
				require.Error(t, f.exec(&disbursementtypes.MsgVoidSuspensions{Authority: f.authority.String(), Term: previous}))
				// A suspension under the new term, even in the same block, is untouched by the void.
				require.NoError(t, f.exec(&disbursementtypes.MsgCommitteeSuspend{Committee: f.committee.String(), ExpectedTerm: f.term, Addresses: []string{member.String()}}))
				require.Error(t, f.release(id))
				previous = f.term
				f.appoint(t, f.committee)
				require.NoError(t, f.exec(&disbursementtypes.MsgVoidSuspensions{Authority: f.authority.String(), Term: previous}))
			case "governance":
				require.NoError(t, f.exec(&disbursementtypes.MsgReinstateMembers{Authority: f.authority.String(), Addresses: []string{member.String()}}))
			default:
				require.NoError(t, f.exec(&disbursementtypes.MsgCommitteeReinstate{Committee: f.committee.String(), ExpectedTerm: f.term, Addresses: []string{member.String()}}))
			}
			require.NoError(t, f.release(id))
			require.Equal(t, math.NewInt(4_000), f.grant(t, id).Paid)
			f.check(t)
		})
	}
}

func TestNativeDisbursementOwnershipAndFounders(t *testing.T) {
	for _, founder := range []bool{false, true} {
		t.Run(fmt.Sprintf("founder_%t", founder), func(t *testing.T) {
			beneficiary := nativeAddress("owner")
			var founders []sdk.AccAddress
			if founder {
				founders = []sdk.AccAddress{beneficiary}
			}
			f := newNativeDisbursement(t, founders...)
			f.open(t, disbursementtypes.Pool_POOL_CONTRIBUTORS, nativeNoah(100_000_000))
			apptestutil.FundModule(t, f.app, f.ctx, stakingtypes.BondedPoolName, sdk.NewCoins(chain.NoahCoin(nativeNoah(50_000_000))))
			id := f.create(t, disbursementtypes.GrantKind_GRANT_KIND_OWNERSHIP, beneficiary, chain.NoahCoin(nativeNoah(80_000_000)), []disbursementtypes.Period{{Length: 10, Parts: 1}, {Length: 10, Parts: 3}})
			require.Error(t, f.release(id))
			f.advance(10)
			quote, err := f.query.Releasable(f.ctx, &disbursementtypes.QueryReleasableRequest{Id: id})
			require.NoError(t, err)
			require.True(t, quote.Amount.Amount.IsPositive())
			require.True(t, quote.Amount.Amount.LT(quote.Accrued))
			require.NoError(t, f.release(id))
			paid := f.grant(t, id).Paid
			controller, payee := nativeAddress("controller"), nativeAddress("payee")
			require.NoError(t, f.exec(&disbursementtypes.MsgSetController{Sender: beneficiary.String(), Beneficiary: beneficiary.String(), Controller: controller.String()}))
			require.NoError(t, f.exec(&disbursementtypes.MsgSetPayee{Sender: controller.String(), GrantId: id, Payee: payee.String()}))
			// A new award uses the same beneficiary without a founder classification input.
			other := f.create(t, disbursementtypes.GrantKind_GRANT_KIND_OWNERSHIP, beneficiary, chain.NoahCoin(nativeNoah(10_000_000)), []disbursementtypes.Period{{Length: 100, Parts: 1}})
			person, err := f.query.Beneficiary(f.ctx, &disbursementtypes.QueryBeneficiaryRequest{Address: beneficiary.String()})
			require.NoError(t, err)
			require.Equal(t, paid, person.Beneficiary.OwnershipPaid)
			require.Equal(t, controller.String(), person.Beneficiary.Controller)
			if founder {
				require.Equal(t, nativeNoah(chain.SeatGrantNoah), person.Beneficiary.Seat)
			} else {
				require.True(t, person.Beneficiary.Seat.IsZero())
			}
			require.NoError(t, f.exec(&disbursementtypes.MsgCancelGrants{Authority: f.authority.String(), GrantIds: []uint64{other}}))
			apptestutil.FundModule(t, f.app, f.ctx, stakingtypes.BondedPoolName, sdk.NewCoins(chain.NoahCoin(nativeNoah(400_000_000))))
			f.advance(10)
			// The 60M ceiling counts the seat.
			ceiling := nativeNoah(60_000_000)
			if founder {
				ceiling = ceiling.Sub(nativeNoah(chain.SeatGrantNoah))
			}
			require.NoError(t, f.release(id))
			require.Equal(t, ceiling, f.grant(t, id).Paid)
			require.ErrorContains(t, f.release(id), "nothing is payable")
			f.check(t)
		})
	}
}

func TestNativeDisbursementStablecoinCompensation(t *testing.T) {
	t.Run("funded_untaxed_and_grandfathered", func(t *testing.T) {
		f := newNativeDisbursement(t)
		beneficiary := nativeAddress("worker")
		taxParams, err := f.app.TreasuryKeeper.Params.Get(f.ctx)
		require.NoError(t, err)
		taxParams.TransferTaxRate = math.LegacyMustNewDecFromStr("0.1")
		require.NoError(t, f.app.TreasuryKeeper.Params.Set(f.ctx, taxParams))
		asset, err := f.app.AssetKeeper.Assets.Get(f.ctx, "ausd")
		require.NoError(t, err)
		p, err := f.app.DisbursementKeeper.Params.Get(f.ctx)
		require.NoError(t, err)
		p.CompensationDenoms = []string{"anoah", "ausd"}
		require.NoError(t, f.exec(&disbursementtypes.MsgUpdateParams{Authority: f.authority.String(), Params: p}))
		f.fund(t, sdk.NewInt64Coin("ausd", 12_000))
		id := f.create(t, disbursementtypes.GrantKind_GRANT_KIND_COMPENSATION, beneficiary, sdk.NewInt64Coin("ausd", 12_000), []disbursementtypes.Period{{Length: 10, Parts: 1}, {Length: 10, Parts: 1}, {Length: 10, Parts: 1}})
		require.Error(t, f.release(id))
		collector := authtypes.NewModuleAddress(treasurytypes.TransferTaxCollectorName)
		before := f.app.BankKeeper.GetBalance(f.ctx, collector, "ausd")
		asset.Status = assettypes.AssetStatus_ASSET_STATUS_SUSPENDED
		require.NoError(t, f.app.AssetKeeper.Assets.Set(f.ctx, "ausd", asset))
		require.ErrorContains(t, f.exec(&disbursementtypes.MsgCreateGrant{Authority: f.authority.String(), Kind: disbursementtypes.GrantKind_GRANT_KIND_COMPENSATION, Beneficiary: beneficiary.String(), Amount: sdk.NewInt64Coin("ausd", 1), Schedule: []disbursementtypes.Period{{Length: 1, Parts: 1}}, Reference: "new work"}), "active asset")
		f.advance(10)
		require.NoError(t, f.release(id))
		require.Equal(t, math.NewInt(4_000), f.app.BankKeeper.GetBalance(f.ctx, beneficiary, "ausd").Amount)
		require.Equal(t, before, f.app.BankKeeper.GetBalance(f.ctx, collector, "ausd"))
		person, err := f.query.Beneficiary(f.ctx, &disbursementtypes.QueryBeneficiaryRequest{Address: beneficiary.String()})
		require.NoError(t, err)
		require.True(t, person.Beneficiary.OwnershipPaid.IsZero())
		f.advance(10)
		require.NoError(t, f.exec(&disbursementtypes.MsgCancelGrants{Authority: f.authority.String(), GrantIds: []uint64{id}}))
		g := f.grant(t, id)
		require.Equal(t, math.NewInt(4_000), g.Remaining)
		require.Equal(t, math.NewInt(4_000), g.CancelledAmount)
		custody, err := f.query.Balance(f.ctx, &disbursementtypes.QueryBalanceRequest{Denom: "ausd"})
		require.NoError(t, err)
		require.Equal(t, math.NewInt(4_000), custody.Unallocated, "unearned stablecoins stay for the next award")
		pool, err := f.app.DistrKeeper.FeePool.Get(f.ctx)
		require.NoError(t, err)
		require.True(t, pool.CommunityPool.AmountOf("ausd").IsZero())
		require.NoError(t, f.release(id))
		totals, err := f.query.Balance(f.ctx, &disbursementtypes.QueryBalanceRequest{Denom: "ausd"})
		require.NoError(t, err)
		require.Equal(t, math.NewInt(8_000), totals.Totals.CompensationPaid)
		require.True(t, totals.Totals.OwnershipPaid.IsZero())
		f.check(t)
	})
}

func TestNativeDisbursementAtomicRefusals(t *testing.T) {
	for _, tc := range []string{"wrong_committee", "duplicate_members", "batch_overflow", "issuance_window", "unfunded", "wrong_asset", "reserved_return", "blocked_batch"} {
		t.Run(tc, func(t *testing.T) {
			f := newNativeDisbursement(t)
			member := nativeAddress("member")
			f.open(t, disbursementtypes.Pool_POOL_MEMBERS, math.NewInt(30_000))
			f.register(t, member)
			p, err := f.app.DisbursementKeeper.Params.Get(f.ctx)
			require.NoError(t, err)
			var msg sdk.Msg
			switch tc {
			case "wrong_committee":
				msg = &disbursementtypes.MsgCommitteeRegister{Committee: member.String(), ExpectedTerm: f.term, Addresses: []string{nativeAddress("other").String()}}
			case "duplicate_members":
				msg = &disbursementtypes.MsgCommitteeRegister{Committee: f.committee.String(), ExpectedTerm: f.term, Addresses: []string{member.String()}}
			case "batch_overflow":
				msg = &disbursementtypes.MsgRelease{Sender: member.String(), GrantIds: make([]uint64, disbursementtypes.MaxBatch+1)}
			case "issuance_window":
				p.MaxMembers = 1
				require.NoError(t, f.exec(&disbursementtypes.MsgUpdateParams{Authority: f.authority.String(), Params: p}))
				msg = &disbursementtypes.MsgCommitteeRegister{Committee: f.committee.String(), ExpectedTerm: f.term, Addresses: []string{nativeAddress("other").String()}}
			case "unfunded":
				msg = &disbursementtypes.MsgCreateGrant{Authority: f.authority.String(), Kind: disbursementtypes.GrantKind_GRANT_KIND_COMPENSATION, Beneficiary: member.String(), Amount: chain.NoahCoin(math.NewInt(40_000)), Schedule: []disbursementtypes.Period{{Length: 1, Parts: 1}}, Reference: "work"}
			case "wrong_asset":
				msg = &disbursementtypes.MsgCreateGrant{Authority: f.authority.String(), Kind: disbursementtypes.GrantKind_GRANT_KIND_OWNERSHIP, Beneficiary: member.String(), Amount: sdk.NewInt64Coin("ausd", 1), Schedule: []disbursementtypes.Period{{Length: 1, Parts: 1}}, Reference: "work"}
			case "reserved_return":
				msg = &disbursementtypes.MsgReturnUnallocated{Authority: f.authority.String(), Amount: chain.NoahCoin(math.NewInt(20_001))}
			case "blocked_batch":
				msg = &disbursementtypes.MsgCommitteeRegister{Committee: f.committee.String(), ExpectedTerm: f.term, Addresses: []string{nativeAddress("other").String(), authtypes.NewModuleAddress(authtypes.FeeCollectorName).String()}}
			}
			before := f.check(t)
			require.Error(t, f.exec(msg))
			require.Equal(t, before, f.check(t))
		})
	}
}

func TestNativeDisbursementQueriesAndContinuation(t *testing.T) {
	t.Run("indexed_history_and_export", func(t *testing.T) {
		f := newNativeDisbursement(t)
		f.open(t, disbursementtypes.Pool_POOL_MEMBERS, math.NewInt(100_000))
		a, b := nativeAddress("a"), nativeAddress("b")
		id := f.register(t, a)
		f.register(t, b)
		f.advance(11)
		require.NoError(t, f.release(id))
		require.NoError(t, f.exec(&disbursementtypes.MsgCancelGrants{Authority: f.authority.String(), GrantIds: []uint64{id}}))
		res, err := f.query.Grants(f.ctx, &disbursementtypes.QueryGrantsRequest{Beneficiary: a.String()})
		require.NoError(t, err)
		require.Len(t, res.Grants, 1)
		require.Equal(t, id, res.Grants[0].Id)
		var key []byte
		var entries []disbursementtypes.JournalEntry
		for {
			r, err := f.query.Journal(f.ctx, &disbursementtypes.QueryJournalRequest{GrantId: id, Pagination: &query.PageRequest{Limit: 1, Key: key}})
			require.NoError(t, err)
			entries = append(entries, r.Entries...)
			key = r.Pagination.NextKey
			if len(key) == 0 {
				break
			}
		}
		require.Len(t, entries, 4)
		for _, e := range entries {
			require.Equal(t, id, e.GrantId)
		}
		_, err = f.query.Grants(f.ctx, &disbursementtypes.QueryGrantsRequest{Pagination: &query.PageRequest{CountTotal: true}})
		require.Error(t, err)
		exported := f.check(t)
		fresh := newNativeDisbursement(t)
		fresh.ctx = fresh.ctx.WithBlockTime(f.ctx.BlockTime()).WithBlockHeight(f.ctx.BlockHeight())
		balance := f.app.BankKeeper.GetBalance(f.ctx, authtypes.NewModuleAddress(disbursementtypes.ModuleName), chain.NoahBaseDenom)
		apptestutil.FundModule(t, fresh.app, fresh.ctx, disbursementtypes.ModuleName, sdk.NewCoins(balance))
		require.NoError(t, fresh.app.DisbursementKeeper.InitGenesis(fresh.ctx, exported))
		require.Equal(t, exported, fresh.check(t))
		fresh.advance(10)
		require.NoError(t, fresh.release(id+1))
		fresh.check(t)
	})
}

func TestNativeDisbursementMemberTermsAndWindow(t *testing.T) {
	for _, recovery := range []string{"reinstate", "void"} {
		t.Run(recovery, func(t *testing.T) {
			f := newNativeDisbursement(t)
			f.open(t, disbursementtypes.Pool_POOL_MEMBERS, math.NewInt(100_000))
			p, err := f.app.DisbursementKeeper.Params.Get(f.ctx)
			require.NoError(t, err)
			p.MaxMembers, p.WindowSeconds = 1, 12
			require.NoError(t, f.exec(&disbursementtypes.MsgUpdateParams{Authority: f.authority.String(), Params: p}))
			a, b := nativeAddress("a"), nativeAddress("b")
			id := f.register(t, a)
			terms := f.grant(t, id)
			require.NoError(t, f.exec(&disbursementtypes.MsgCommitteeSuspend{Committee: f.committee.String(), ExpectedTerm: f.term, Addresses: []string{a.String()}}))
			p.MemberAmount = math.NewInt(20_000)
			p.MemberSchedule = []disbursementtypes.Period{{Length: 1, Parts: 1}, {Length: 100, Parts: 1}}
			require.NoError(t, f.exec(&disbursementtypes.MsgUpdateParams{Authority: f.authority.String(), Params: p}))
			f.advance(11)
			require.Error(t, f.exec(&disbursementtypes.MsgCommitteeRegister{Committee: f.committee.String(), ExpectedTerm: f.term, Addresses: []string{b.String()}}))
			f.advance(1)
			other := f.register(t, b)
			require.Equal(t, p.MemberAmount, f.grant(t, other).Amount.Amount)
			require.Equal(t, terms.Amount, f.grant(t, id).Amount)
			require.Equal(t, terms.Schedule, f.grant(t, id).Schedule)
			if recovery == "void" {
				previous := f.term
				f.appoint(t, f.committee)
				require.NoError(t, f.exec(&disbursementtypes.MsgVoidSuspensions{Authority: f.authority.String(), Term: previous}))
			} else {
				require.NoError(t, f.exec(&disbursementtypes.MsgReinstateMembers{Authority: f.authority.String(), Addresses: []string{a.String()}}))
			}
			// An invalidated suspension cannot backdate a later cancellation.
			require.NoError(t, f.exec(&disbursementtypes.MsgCancelGrants{Authority: f.authority.String(), GrantIds: []uint64{id}}))
			require.Equal(t, math.NewInt(3_000), f.grant(t, id).Remaining)
			require.Equal(t, uint64(f.ctx.BlockTime().Unix()), f.grant(t, id).CutoffTime)
			f.check(t)
		})
	}
}

func TestNativeDisbursementGrantsMandate(t *testing.T) {
	t.Run("term_window_and_appointment_bounds", func(t *testing.T) {
		f := newNativeDisbursement(t)
		f.open(t, disbursementtypes.Pool_POOL_MEMBERS, math.NewInt(100_000))
		member := nativeAddress("member")
		require.ErrorContains(t, f.exec(&disbursementtypes.MsgCommitteeRegister{Committee: f.committee.String(), ExpectedTerm: f.term + 1, Addresses: []string{member.String()}}), "term mismatch")
		height := uint64(f.ctx.BlockHeight())
		require.ErrorContains(t, f.exec(&disbursementtypes.MsgSetGrantsMandate{Authority: f.authority.String(), Committee: f.authority.String(), ActivationHeight: height, ExpiryHeight: height + 10}), "distinct")
		require.ErrorContains(t, f.exec(&disbursementtypes.MsgSetGrantsMandate{Authority: f.authority.String(), Committee: f.committee.String(), ActivationHeight: height - 5, ExpiryHeight: height}), "not above the current height")
		// A short window lapses on its own. The term does not move, so the hold stays effective.
		require.NoError(t, f.exec(&disbursementtypes.MsgSetGrantsMandate{Authority: f.authority.String(), Committee: f.committee.String(), ActivationHeight: height, ExpiryHeight: height + 2}))
		f.term++
		id := f.register(t, member)
		require.NoError(t, f.exec(&disbursementtypes.MsgCommitteeSuspend{Committee: f.committee.String(), ExpectedTerm: f.term, Addresses: []string{member.String()}}))
		f.advance(1)
		f.advance(1)
		require.ErrorContains(t, f.exec(&disbursementtypes.MsgCommitteeReinstate{Committee: f.committee.String(), ExpectedTerm: f.term, Addresses: []string{member.String()}}), "not active")
		status, err := f.query.GrantsMandate(f.ctx, &disbursementtypes.QueryGrantsMandateRequest{})
		require.NoError(t, err)
		require.False(t, status.Active)
		require.Equal(t, f.term, status.Mandate.Term)
		f.advance(11)
		require.ErrorContains(t, f.release(id), "nothing is payable")
		// Governance lifts the hold directly; disabling advances the term and refuses the old key.
		require.NoError(t, f.exec(&disbursementtypes.MsgReinstateMembers{Authority: f.authority.String(), Addresses: []string{member.String()}}))
		require.NoError(t, f.release(id))
		require.NoError(t, f.exec(&disbursementtypes.MsgSetGrantsMandate{Authority: f.authority.String()}))
		f.term++
		require.Error(t, f.exec(&disbursementtypes.MsgCommitteeRegister{Committee: f.committee.String(), ExpectedTerm: f.term, Addresses: []string{nativeAddress("other").String()}}))
		exported := f.check(t)
		require.Equal(t, f.term, exported.GrantsMandate.Term)
		require.True(t, exported.GrantsMandate.IsDisabled())
	})
}

func TestNativeDisbursementPaymentBatchRollback(t *testing.T) {
	t.Run("second_destination_refuses", func(t *testing.T) {
		f := newNativeDisbursement(t)
		a, b := nativeAddress("a"), nativeAddress("b")
		f.open(t, disbursementtypes.Pool_POOL_MEMBERS, math.NewInt(20_000))
		first, second := f.register(t, a), f.register(t, b)
		f.advance(11)
		before := f.check(t)
		balance := f.app.BankKeeper.GetBalance(f.ctx, a, chain.NoahBaseDenom)
		f.app.BankKeeper.AppendSendRestriction(func(_ context.Context, _ sdk.AccAddress, to sdk.AccAddress, _ sdk.Coins) (sdk.AccAddress, error) {
			if to.Equals(b) {
				return nil, errors.New("second destination refuses")
			}
			return to, nil
		})
		require.ErrorContains(t, f.exec(&disbursementtypes.MsgRelease{Sender: a.String(), GrantIds: []uint64{first, second}}), "second destination refuses")
		require.Equal(t, before, f.check(t))
		require.Equal(t, balance, f.app.BankKeeper.GetBalance(f.ctx, a, chain.NoahBaseDenom))
	})
}

func TestNativeDisbursementFounderBloc(t *testing.T) {
	t.Run("proportional_room_and_retained_debt", func(t *testing.T) {
		a, b := nativeAddress("a"), nativeAddress("b")
		f := newNativeDisbursement(t, a, b)
		f.open(t, disbursementtypes.Pool_POOL_CONTRIBUTORS, nativeNoah(80_000_000))
		apptestutil.FundModule(t, f.app, f.ctx, stakingtypes.BondedPoolName, sdk.NewCoins(chain.NoahCoin(nativeNoah(60_000_000))))
		schedule := []disbursementtypes.Period{{Length: 10, Parts: 1}, {Length: 10, Parts: 1}}
		first := f.create(t, disbursementtypes.GrantKind_GRANT_KIND_OWNERSHIP, a, chain.NoahCoin(nativeNoah(20_000_000)), schedule)
		second := f.create(t, disbursementtypes.GrantKind_GRANT_KIND_OWNERSHIP, b, chain.NoahCoin(nativeNoah(60_000_000)), schedule)
		f.advance(10)
		qa, err := f.query.Releasable(f.ctx, &disbursementtypes.QueryReleasableRequest{Id: first})
		require.NoError(t, err)
		qb, err := f.query.Releasable(f.ctx, &disbursementtypes.QueryReleasableRequest{Id: second})
		require.NoError(t, err)
		room := qa.Bonded.MulRaw(3).QuoRaw(10).Sub(nativeNoah(10_000_000))
		require.Equal(t, room.QuoRaw(4), qa.BlocAllowance)
		require.Equal(t, room.MulRaw(3).QuoRaw(4), qb.BlocAllowance)
		require.NoError(t, f.release(first))
		require.NoError(t, f.exec(&disbursementtypes.MsgCancelGrants{Authority: f.authority.String(), GrantIds: []uint64{second}}))
		retained := f.grant(t, second)
		require.Equal(t, nativeNoah(30_000_000), retained.Remaining)
		remaining, err := f.app.DisbursementKeeper.FounderRemaining.Get(f.ctx)
		require.NoError(t, err)
		require.Equal(t, f.grant(t, first).Remaining.Add(retained.Remaining), remaining)
		require.NoError(t, f.release(second))
		paid, err := f.app.DisbursementKeeper.FounderPaid.Get(f.ctx)
		require.NoError(t, err)
		require.Equal(t, f.grant(t, first).Paid.Add(f.grant(t, second).Paid), paid)
		require.True(t, paid.LTE(room))
		// Seats and payments stay under the 33.3% that blocks a proposal.
		require.True(t, nativeNoah(10_000_000).Add(paid).MulRaw(1000).LT(qa.Bonded.MulRaw(333)))
		exported := f.check(t)
		fresh := newNativeDisbursement(t)
		fresh.ctx = fresh.ctx.WithBlockTime(f.ctx.BlockTime()).WithBlockHeight(f.ctx.BlockHeight())
		balance := f.app.BankKeeper.GetBalance(f.ctx, authtypes.NewModuleAddress(disbursementtypes.ModuleName), chain.NoahBaseDenom)
		apptestutil.FundModule(t, fresh.app, fresh.ctx, disbursementtypes.ModuleName, sdk.NewCoins(balance))
		require.NoError(t, fresh.app.DisbursementKeeper.InitGenesis(fresh.ctx, exported))
		freshPaid, err := fresh.app.DisbursementKeeper.FounderPaid.Get(fresh.ctx)
		require.NoError(t, err)
		require.Equal(t, paid, freshPaid)
		freshRemaining, err := fresh.app.DisbursementKeeper.FounderRemaining.Get(fresh.ctx)
		require.NoError(t, err)
		require.Equal(t, f.grant(t, first).Remaining.Add(f.grant(t, second).Remaining), freshRemaining)
	})
}

func TestNativeDisbursementImportRefusals(t *testing.T) {
	for _, corruption := range []string{"reservation", "paid", "history", "time", "unbacked"} {
		t.Run(corruption, func(t *testing.T) {
			f := newNativeDisbursement(t)
			f.open(t, disbursementtypes.Pool_POOL_MEMBERS, math.NewInt(10_000))
			f.register(t, nativeAddress("a"))
			gs := f.check(t)
			switch corruption {
			case "reservation":
				gs.Totals[0].Reserved = gs.Totals[0].Reserved.SubRaw(1)
			case "paid":
				gs.Grants[0].Paid = gs.Grants[0].Paid.AddRaw(1)
			case "history":
				gs.Journal = gs.Journal[:len(gs.Journal)-1]
			case "time":
				gs.Journal[0].Time++
			}
			fresh := newNativeDisbursement(t)
			if corruption != "unbacked" {
				apptestutil.FundModule(t, fresh.app, fresh.ctx, disbursementtypes.ModuleName, sdk.NewCoins(chain.NoahCoin(math.NewInt(10_000))))
			}
			require.Error(t, fresh.app.DisbursementKeeper.InitGenesis(fresh.ctx, gs))
		})
	}
}

func requirePool(t *testing.T, p disbursementtypes.PoolBalance, unallocated, open int64) {
	t.Helper()
	require.True(t, p.Unallocated.Equal(math.NewInt(unallocated)), "unallocated %s", p.Unallocated)
	require.True(t, p.Open.Equal(math.NewInt(open)), "open %s", p.Open)
}

func TestNativeDisbursementPools(t *testing.T) {
	members, contributors := disbursementtypes.Pool_POOL_MEMBERS, disbursementtypes.Pool_POOL_CONTRIBUTORS
	t.Run("tranches", func(t *testing.T) {
		f := newNativeDisbursement(t)
		a, b := nativeAddress("a"), nativeAddress("b")
		openTranche := func(authority sdk.AccAddress, pool disbursementtypes.Pool, amount int64) error {
			return f.exec(&disbursementtypes.MsgOpenTranche{Authority: authority.String(), Pool: pool, Amount: math.NewInt(amount)})
		}
		f.seed(t, members, math.NewInt(20_000))
		require.ErrorContains(t, f.exec(&disbursementtypes.MsgCommitteeRegister{Committee: f.committee.String(), ExpectedTerm: f.term, Addresses: []string{a.String()}}), "open member tranche")
		require.ErrorContains(t, openTranche(f.authority, members, 20_001), "exceeds its pool")
		require.ErrorContains(t, openTranche(f.authority, disbursementtypes.Pool_POOL_UNSPECIFIED, 1), "unknown distribution pool")
		require.Error(t, openTranche(a, members, 1))
		require.NoError(t, openTranche(f.authority, members, 10_000))
		id := f.register(t, a)
		require.ErrorContains(t, f.exec(&disbursementtypes.MsgCommitteeRegister{Committee: f.committee.String(), ExpectedTerm: f.term, Addresses: []string{b.String()}}), "open member tranche")
		f.advance(11)
		require.NoError(t, f.exec(&disbursementtypes.MsgCancelGrants{Authority: f.authority.String(), GrantIds: []uint64{id}}))
		// The unearned 6,000 returns to the step it was charged to, ready for the next member.
		requirePool(t, f.poolBalance(t, members), 16_000, 6_000)
		require.NoError(t, openTranche(f.authority, members, 4_000))
		f.register(t, b)
		requirePool(t, f.poolBalance(t, members), 6_000, 0)
		f.check(t)
	})
	t.Run("returns", func(t *testing.T) {
		f := newNativeDisbursement(t)
		f.open(t, contributors, math.NewInt(20_000))
		f.seed(t, contributors, math.NewInt(30_000))
		f.seed(t, members, math.NewInt(10_000))
		ret := func(pool disbursementtypes.Pool, coin sdk.Coin) error {
			return f.exec(&disbursementtypes.MsgReturnUnallocated{Authority: f.authority.String(), Amount: coin, Pool: pool})
		}
		require.ErrorContains(t, ret(members, chain.NoahCoin(math.NewInt(1))), "no exit")
		require.ErrorContains(t, ret(contributors, sdk.NewInt64Coin("ausd", 1)), "holds NOAH")
		require.ErrorContains(t, ret(contributors, chain.NoahCoin(math.NewInt(50_001))), "exceeds the contributor pool")
		require.ErrorContains(t, ret(disbursementtypes.Pool_POOL_UNSPECIFIED, chain.NoahCoin(math.NewInt(1))), "committed funds")
		before, err := f.app.DistrKeeper.FeePool.Get(f.ctx)
		require.NoError(t, err)
		require.NoError(t, ret(contributors, chain.NoahCoin(math.NewInt(40_000))))
		// The 30,000 unopened goes first, then 10,000 of the open tranche.
		requirePool(t, f.poolBalance(t, contributors), 10_000, 10_000)
		requirePool(t, f.poolBalance(t, members), 10_000, 0)
		after, err := f.app.DistrKeeper.FeePool.Get(f.ctx)
		require.NoError(t, err)
		require.Equal(t, math.LegacyNewDec(40_000), after.CommunityPool.AmountOf(chain.NoahBaseDenom).Sub(before.CommunityPool.AmountOf(chain.NoahBaseDenom)))
		// A stray deposit sits outside both pools and stays returnable.
		apptestutil.FundModule(t, f.app, f.ctx, disbursementtypes.ModuleName, sdk.NewCoins(chain.NoahCoin(math.NewInt(5_000))))
		require.NoError(t, ret(disbursementtypes.Pool_POOL_UNSPECIFIED, chain.NoahCoin(math.NewInt(5_000))))
		require.Error(t, ret(disbursementtypes.Pool_POOL_UNSPECIFIED, chain.NoahCoin(math.NewInt(1))))
		f.check(t)
	})
	t.Run("conversion_orders", func(t *testing.T) {
		f := newNativeDisbursement(t)
		p, err := f.app.DisbursementKeeper.Params.Get(f.ctx)
		require.NoError(t, err)
		p.CompensationDenoms = []string{"anoah", "ausd"}
		require.NoError(t, f.exec(&disbursementtypes.MsgUpdateParams{Authority: f.authority.String(), Params: p}))
		f.open(t, contributors, math.NewInt(20_000))
		f.seed(t, contributors, math.NewInt(30_000))
		spread := math.LegacyMustNewDecFromStr("0.05")
		authorise := func(denom string, amount int64, maxSpread math.LegacyDec) error {
			return f.exec(&disbursementtypes.MsgAuthoriseConversion{Authority: f.authority.String(), Denom: denom, Amount: math.NewInt(amount), MaxSpread: maxSpread})
		}
		for _, tc := range []struct {
			name, denom string
			amount      int64
			maxSpread   math.LegacyDec
			err         string
		}{
			{"noah", "anoah", 1, spread, "sell NOAH"},
			{"unapproved", "aeur", 1, spread, "not approved"},
			{"zero_cap", "ausd", 1, math.LegacyZeroDec(), "max spread"},
			{"whole_cap", "ausd", 1, math.LegacyOneDec(), "max spread"},
			{"beyond_tranche", "ausd", 20_001, spread, "exceeds the open"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				require.ErrorContains(t, authorise(tc.denom, tc.amount, tc.maxSpread), tc.err)
			})
		}
		require.NoError(t, authorise("ausd", 15_000, spread))
		require.NoError(t, authorise("ausd", 5_000, math.LegacyMustNewDecFromStr("0.03")))
		requirePool(t, f.poolBalance(t, contributors), 30_000, 0)
		orders, err := f.query.ConversionOrders(f.ctx, &disbursementtypes.QueryConversionOrdersRequest{})
		require.NoError(t, err)
		require.Len(t, orders.Orders, 1)
		require.Equal(t, math.NewInt(20_000), orders.Orders[0].Remaining)
		require.Equal(t, math.LegacyMustNewDecFromStr("0.03"), orders.Orders[0].MaxSpread, "each authorisation restates the cap")
		balance, err := f.query.Balance(f.ctx, &disbursementtypes.QueryBalanceRequest{Denom: chain.NoahBaseDenom})
		require.NoError(t, err)
		require.Equal(t, math.NewInt(20_000), balance.Converting)
		convert := func(sender sdk.AccAddress, denom string, amount int64) error {
			return f.exec(&disbursementtypes.MsgConvert{Sender: sender.String(), Denom: denom, Amount: math.NewInt(amount)})
		}
		caller := nativeAddress("caller")
		require.ErrorContains(t, convert(f.authority, "ausd", 1), "a transaction executes them")
		require.ErrorContains(t, convert(caller, "ausd", 20_001), "exceeds the conversion order")
		require.ErrorContains(t, convert(caller, "akrw", 1), "conversion order")
		exported := f.check(t)
		fresh := newNativeDisbursement(t)
		fresh.ctx = fresh.ctx.WithBlockTime(f.ctx.BlockTime()).WithBlockHeight(f.ctx.BlockHeight())
		custody := f.app.BankKeeper.GetBalance(f.ctx, authtypes.NewModuleAddress(disbursementtypes.ModuleName), chain.NoahBaseDenom)
		apptestutil.FundModule(t, fresh.app, fresh.ctx, disbursementtypes.ModuleName, sdk.NewCoins(custody))
		require.NoError(t, fresh.app.DisbursementKeeper.InitGenesis(fresh.ctx, exported))
		require.Equal(t, exported, fresh.check(t))
		require.NoError(t, f.exec(&disbursementtypes.MsgCancelConversion{Authority: f.authority.String(), Denom: "ausd"}))
		requirePool(t, f.poolBalance(t, contributors), 50_000, 20_000)
		require.ErrorContains(t, convert(caller, "ausd", 1), "conversion order")
		require.ErrorContains(t, f.exec(&disbursementtypes.MsgCancelConversion{Authority: f.authority.String(), Denom: "ausd"}), "conversion order")
		f.check(t)
	})
}

func TestNativeDisbursementCommitteeCompensation(t *testing.T) {
	founder, worker := nativeAddress("founder"), nativeAddress("worker")
	allowance := sdk.NewCoins(chain.NoahCoin(math.NewInt(30_000)), sdk.NewInt64Coin("ausd", 20_000))
	const minFirstPeriod = 100
	setup := func(t *testing.T) *nativeDisbursementFixture {
		t.Helper()
		f := newNativeDisbursement(t, founder)
		p, err := f.app.DisbursementKeeper.Params.Get(f.ctx)
		require.NoError(t, err)
		p.CompensationDenoms = []string{"anoah", "ausd"}
		require.NoError(t, f.exec(&disbursementtypes.MsgUpdateParams{Authority: f.authority.String(), Params: p}))
		f.open(t, disbursementtypes.Pool_POOL_CONTRIBUTORS, math.NewInt(100_000))
		f.fund(t, sdk.NewInt64Coin("ausd", 50_000))
		f.appointWith(t, f.committee, allowance, minFirstPeriod)
		return f
	}
	award := func(f *nativeDisbursementFixture, beneficiary sdk.AccAddress, coin sdk.Coin, first uint64) error {
		return f.exec(&disbursementtypes.MsgCommitteeCompensate{
			Committee: f.committee.String(), ExpectedTerm: f.term, Beneficiary: beneficiary.String(), Amount: coin,
			Schedule: []disbursementtypes.Period{{Length: first, Parts: 1}, {Length: 100, Parts: 1}}, Reference: "committee pay",
		})
	}
	usage := func(t *testing.T, f *nativeDisbursementFixture) *disbursementtypes.QueryGrantsMandateResponse {
		t.Helper()
		res, err := f.query.GrantsMandate(f.ctx, &disbursementtypes.QueryGrantsMandateRequest{})
		require.NoError(t, err)
		return res
	}

	t.Run("appointment_bounds", func(t *testing.T) {
		f := setup(t)
		appoint := func(allowance sdk.Coins, period uint64) error {
			height := uint64(f.ctx.BlockHeight())
			return f.exec(&disbursementtypes.MsgSetGrantsMandate{
				Authority: f.authority.String(), Committee: f.committee.String(), ActivationHeight: height, ExpiryHeight: height + 100,
				CompensationAllowance: allowance, MinFirstPeriod: period,
			})
		}
		require.ErrorContains(t, appoint(sdk.NewCoins(sdk.NewInt64Coin("aeur", 1)), minFirstPeriod), "not approved")
		require.ErrorContains(t, appoint(allowance, 0), "positive minimum first period")
		// A disabling keeps the canonical empty payload whatever the message carries.
		require.NoError(t, f.exec(&disbursementtypes.MsgSetGrantsMandate{Authority: f.authority.String(), CompensationAllowance: allowance, MinFirstPeriod: minFirstPeriod}))
		f.term++
		m := usage(t, f).Mandate
		require.True(t, m.IsDisabled())
		require.True(t, m.CompensationAllowance.Empty())
		require.Zero(t, m.MinFirstPeriod)
	})

	t.Run("awards_within_the_term", func(t *testing.T) {
		f := setup(t)
		require.ErrorContains(t, award(f, worker, chain.NoahCoin(math.NewInt(1_000)), minFirstPeriod-1), "shorter than the mandate's minimum")
		require.ErrorContains(t, award(f, founder, chain.NoahCoin(math.NewInt(1_000)), minFirstPeriod), "seat holder")
		require.NoError(t, award(f, worker, chain.NoahCoin(math.NewInt(20_000)), minFirstPeriod))
		requirePool(t, f.poolBalance(t, disbursementtypes.Pool_POOL_CONTRIBUTORS), 80_000, 80_000)
		require.NoError(t, award(f, worker, sdk.NewInt64Coin("ausd", 20_000), minFirstPeriod))
		require.ErrorContains(t, award(f, worker, sdk.NewInt64Coin("ausd", 1), minFirstPeriod), "compensation allowance")
		// Cancelling an award frees its principal but never its allowance.
		first := uint64(1)
		for ; ; first++ {
			if f.grant(t, first).Kind == disbursementtypes.GrantKind_GRANT_KIND_COMPENSATION {
				break
			}
		}
		require.NoError(t, f.exec(&disbursementtypes.MsgCancelGrants{Authority: f.authority.String(), GrantIds: []uint64{first}}))
		require.ErrorContains(t, award(f, worker, chain.NoahCoin(math.NewInt(10_001)), minFirstPeriod), "compensation allowance")
		require.NoError(t, award(f, worker, chain.NoahCoin(math.NewInt(10_000)), minFirstPeriod))
		res := usage(t, f)
		require.Equal(t, allowance, res.CompensationUsed)
		require.EqualValues(t, 3, res.CompensationGrants)
		listed, err := f.query.Grants(f.ctx, &disbursementtypes.QueryGrantsRequest{MandateTerm: f.term})
		require.NoError(t, err)
		require.Len(t, listed.Grants, 3)
		_, err = f.query.Grants(f.ctx, &disbursementtypes.QueryGrantsRequest{MandateTerm: f.term, Beneficiary: worker.String()})
		require.Error(t, err)
		// A new term starts with nothing used.
		f.appointWith(t, f.committee, allowance, minFirstPeriod)
		require.True(t, usage(t, f).CompensationUsed.Empty())
		require.NoError(t, award(f, worker, chain.NoahCoin(math.NewInt(30_000)), minFirstPeriod))
		f.check(t)
	})

	t.Run("term_cap", func(t *testing.T) {
		f := setup(t)
		for range disbursementtypes.MaxTermGrants {
			require.NoError(t, award(f, worker, chain.NoahCoin(math.NewInt(2)), minFirstPeriod))
		}
		require.ErrorContains(t, award(f, worker, chain.NoahCoin(math.NewInt(2)), minFirstPeriod), "awards")
	})

	t.Run("recovery_by_term", func(t *testing.T) {
		f := setup(t)
		require.NoError(t, award(f, worker, chain.NoahCoin(math.NewInt(10_000)), minFirstPeriod))
		require.NoError(t, award(f, worker, sdk.NewInt64Coin("ausd", 5_000), minFirstPeriod))
		stolen := f.term
		// Governance replaces the key; an award it lands before the vote executes is still under its term.
		require.NoError(t, award(f, worker, chain.NoahCoin(math.NewInt(5_000)), minFirstPeriod))
		f.appointWith(t, nativeAddress("new-committee"), allowance, minFirstPeriod)
		cancel := func(term uint64) error {
			return f.exec(&disbursementtypes.MsgCancelTermGrants{Authority: f.authority.String(), Term: term})
		}
		require.ErrorContains(t, cancel(0), "no such committee term")
		require.ErrorContains(t, cancel(f.term+1), "no such committee term")
		require.NoError(t, cancel(stolen))
		listed, err := f.query.Grants(f.ctx, &disbursementtypes.QueryGrantsRequest{MandateTerm: stolen})
		require.NoError(t, err)
		require.Len(t, listed.Grants, 3)
		for _, g := range listed.Grants {
			require.True(t, g.Cancelled)
			require.True(t, g.Remaining.IsZero(), "nothing accrued before the first period")
		}
		// NOAH returns to the open tranche; stablecoins stay in custody.
		requirePool(t, f.poolBalance(t, disbursementtypes.Pool_POOL_CONTRIBUTORS), 100_000, 100_000)
		custody, err := f.query.Balance(f.ctx, &disbursementtypes.QueryBalanceRequest{Denom: "ausd"})
		require.NoError(t, err)
		require.Equal(t, math.NewInt(50_000), custody.Unallocated)
		require.NoError(t, cancel(stolen), "a term's cancellation is idempotent")
		f.check(t)
	})

	t.Run("continuation", func(t *testing.T) {
		f := setup(t)
		require.NoError(t, award(f, worker, chain.NoahCoin(math.NewInt(10_000)), minFirstPeriod))
		require.NoError(t, award(f, worker, sdk.NewInt64Coin("ausd", 5_000), minFirstPeriod))
		exported := f.check(t)
		fresh := newNativeDisbursement(t, founder)
		fresh.ctx = fresh.ctx.WithBlockTime(f.ctx.BlockTime()).WithBlockHeight(f.ctx.BlockHeight())
		for _, denom := range []string{chain.NoahBaseDenom, "ausd"} {
			balance := f.app.BankKeeper.GetBalance(f.ctx, authtypes.NewModuleAddress(disbursementtypes.ModuleName), denom)
			apptestutil.FundModule(t, fresh.app, fresh.ctx, disbursementtypes.ModuleName, sdk.NewCoins(balance))
		}
		require.NoError(t, fresh.app.DisbursementKeeper.InitGenesis(fresh.ctx, exported))
		require.Equal(t, exported, fresh.check(t))
		require.Equal(t, usage(t, f).CompensationUsed, usage(t, fresh).CompensationUsed, "the term index is rebuilt from the grants")

		for _, tc := range []struct {
			name   string
			mutate func(*disbursementtypes.GenesisState)
		}{
			{"allowance below usage", func(g *disbursementtypes.GenesisState) {
				g.GrantsMandate.CompensationAllowance = sdk.NewCoins(chain.NoahCoin(math.NewInt(9_999)), sdk.NewInt64Coin("ausd", 5_000))
			}},
			{"minimum above an award", func(g *disbursementtypes.GenesisState) { g.GrantsMandate.MinFirstPeriod = minFirstPeriod + 1 }},
			{"future term", func(g *disbursementtypes.GenesisState) {
				g.Grants[len(g.Grants)-1].MandateTerm = g.GrantsMandate.Term + 1
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				g, err := f.app.DisbursementKeeper.ExportGenesis(f.ctx)
				require.NoError(t, err)
				tc.mutate(g)
				require.Error(t, g.Validate())
			})
		}
	})
}
