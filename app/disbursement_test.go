package app_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

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
	registrar, authority sdk.AccAddress
	query                disbursementtypes.QueryServer
}

func nativeAddress(name string) sdk.AccAddress {
	return authtypes.NewModuleAddress("native-disbursement-" + name)
}
func nativeNoah(n int64) math.Int { return chain.NativeBaseAmount(n) }

func newNativeDisbursement(t *testing.T, founders ...sdk.AccAddress) *nativeDisbursementFixture {
	t.Helper()
	a := apptestutil.Setup(t, false)
	f := &nativeDisbursementFixture{app: a, ctx: a.NewContextLegacy(false, cmtproto.Header{Height: 10, Time: time.Unix(1_800_000_000, 0)}), registrar: nativeAddress("registrar"), authority: authtypes.NewModuleAddress(govtypes.ModuleName), query: disbursementkeeper.NewQueryServerImpl(a.DisbursementKeeper)}
	g := disbursementtypes.DefaultGenesisState()
	for _, founder := range founders {
		g.Founders = append(g.Founders, disbursementtypes.Founder{Operator: sdk.ValAddress(founder).String(), Beneficiary: founder.String(), SeatAmount: nativeNoah(chain.SeatGrantNoah)})
		g.Beneficiaries = append(g.Beneficiaries, disbursementtypes.Beneficiary{Address: founder.String(), Controller: founder.String(), OwnershipPaid: math.ZeroInt()})
	}
	require.NoError(t, a.DisbursementKeeper.InitGenesis(f.ctx, g))
	p := g.Params
	p.Registrar = f.registrar.String()
	p.MemberAmount = math.NewInt(10_000)
	p.MemberSchedule = []disbursementtypes.Period{{Length: 1, Parts: 1}, {Length: 10, Parts: 3}, {Length: 10, Parts: 6}}
	require.NoError(t, f.exec(&disbursementtypes.MsgUpdateParams{Authority: f.authority.String(), Params: p}))
	return f
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

func (f *nativeDisbursementFixture) fund(t *testing.T, coin sdk.Coin) {
	t.Helper()
	funder := nativeAddress("funder")
	coins := sdk.NewCoins(coin)
	apptestutil.FundAccount(t, f.app, f.ctx, funder, coins)
	require.NoError(t, f.app.DistrKeeper.FundCommunityPool(f.ctx, coins, funder))
	require.NoError(t, f.exec(&distrtypes.MsgCommunityPoolSpend{Authority: f.authority.String(), Recipient: authtypes.NewModuleAddress(disbursementtypes.ModuleName).String(), Amount: coins}))
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
	require.NoError(t, f.exec(&disbursementtypes.MsgRegisterMembers{Registrar: f.registrar.String(), Addresses: []string{address.String()}}))
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
		require.Equal(t, balance.Balance.Amount, balance.Unallocated.Add(totals.Reserved))
	}
	return g
}

func TestNativeDisbursementMemberCancellation(t *testing.T) {
	for _, suspended := range []bool{false, true} {
		t.Run(fmt.Sprintf("suspended_%t", suspended), func(t *testing.T) {
			f := newNativeDisbursement(t)
			member := nativeAddress("member")
			payee := nativeAddress("new-payee")
			f.fund(t, chain.NoahCoin(math.NewInt(10_000)))
			id := f.register(t, member)
			require.Equal(t, math.NewInt(1_000), f.app.BankKeeper.GetBalance(f.ctx, member, chain.NoahBaseDenom).Amount)
			f.advance(11)
			if suspended {
				require.NoError(t, f.exec(&disbursementtypes.MsgSuspendMembers{Registrar: f.registrar.String(), Addresses: []string{member.String()}}))
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
			require.Error(t, f.exec(&disbursementtypes.MsgSetPayee{Sender: f.registrar.String(), GrantId: id, Payee: payee.String()}))
			require.NoError(t, f.exec(&disbursementtypes.MsgSetPayee{Sender: member.String(), GrantId: id, Payee: payee.String()}))
			f.advance(100)
			require.NoError(t, f.release(id))
			require.Equal(t, math.NewInt(3_000), f.app.BankKeeper.GetBalance(f.ctx, payee, chain.NoahBaseDenom).Amount)
			require.Error(t, f.exec(&disbursementtypes.MsgRegisterMembers{Registrar: f.registrar.String(), Addresses: []string{member.String()}}))
			require.Error(t, f.exec(&disbursementtypes.MsgReinstateMembers{Sender: f.registrar.String(), Addresses: []string{member.String()}}))
			f.check(t)
		})
	}
}

func TestNativeDisbursementSuspensionRecovery(t *testing.T) {
	for _, mode := range []string{"registrar", "governance", "void"} {
		t.Run(mode, func(t *testing.T) {
			f := newNativeDisbursement(t)
			member := nativeAddress("member")
			f.fund(t, chain.NoahCoin(math.NewInt(10_000)))
			id := f.register(t, member)
			require.NoError(t, f.exec(&disbursementtypes.MsgSuspendMembers{Registrar: f.registrar.String(), Addresses: []string{member.String()}}))
			f.advance(11)
			require.ErrorContains(t, f.release(id), "nothing is payable")
			if mode == "void" {
				require.NoError(t, f.exec(&disbursementtypes.MsgVoidSuspensions{Authority: f.authority.String(), Registrar: f.registrar.String()}))
				// Even another suspension in the same block belongs to the new epoch.
				require.NoError(t, f.exec(&disbursementtypes.MsgSuspendMembers{Registrar: f.registrar.String(), Addresses: []string{member.String()}}))
				require.Error(t, f.release(id))
				require.NoError(t, f.exec(&disbursementtypes.MsgVoidSuspensions{Authority: f.authority.String(), Registrar: f.registrar.String()}))
			} else {
				sender := f.registrar
				if mode == "governance" {
					sender = f.authority
				}
				require.NoError(t, f.exec(&disbursementtypes.MsgReinstateMembers{Sender: sender.String(), Addresses: []string{member.String()}}))
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
			f.fund(t, chain.NoahCoin(nativeNoah(100_000_000)))
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
				require.NotNil(t, person.Founder)
			} else {
				require.Nil(t, person.Founder)
			}
			require.NoError(t, f.exec(&disbursementtypes.MsgCancelGrants{Authority: f.authority.String(), GrantIds: []uint64{other}}))
			apptestutil.FundModule(t, f.app, f.ctx, stakingtypes.BondedPoolName, sdk.NewCoins(chain.NoahCoin(nativeNoah(400_000_000))))
			f.advance(10)
			require.NoError(t, f.release(id))
			require.Equal(t, nativeNoah(60_000_000), f.grant(t, id).Paid)
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
		f.fund(t, chain.NoahCoin(math.NewInt(30_000)))
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
		pool, err := f.app.DistrKeeper.FeePool.Get(f.ctx)
		require.NoError(t, err)
		require.Equal(t, math.LegacyNewDec(4_000), pool.CommunityPool.AmountOf("ausd"))
		require.NoError(t, f.release(id))
		totals, err := f.query.Balance(f.ctx, &disbursementtypes.QueryBalanceRequest{Denom: "ausd"})
		require.NoError(t, err)
		require.Equal(t, math.NewInt(8_000), totals.Totals.CompensationPaid)
		require.True(t, totals.Totals.OwnershipPaid.IsZero())
		f.check(t)
	})
}

func TestNativeDisbursementAtomicRefusals(t *testing.T) {
	for _, tc := range []string{"wrong_registrar", "duplicate_members", "batch_overflow", "issuance_window", "unfunded", "wrong_asset", "reserved_return", "blocked_batch"} {
		t.Run(tc, func(t *testing.T) {
			f := newNativeDisbursement(t)
			member := nativeAddress("member")
			f.fund(t, chain.NoahCoin(math.NewInt(30_000)))
			f.register(t, member)
			p, err := f.app.DisbursementKeeper.Params.Get(f.ctx)
			require.NoError(t, err)
			var msg sdk.Msg
			switch tc {
			case "wrong_registrar":
				msg = &disbursementtypes.MsgRegisterMembers{Registrar: member.String(), Addresses: []string{nativeAddress("other").String()}}
			case "duplicate_members":
				msg = &disbursementtypes.MsgRegisterMembers{Registrar: f.registrar.String(), Addresses: []string{member.String()}}
			case "batch_overflow":
				msg = &disbursementtypes.MsgRelease{Sender: member.String(), GrantIds: make([]uint64, disbursementtypes.MaxBatch+1)}
			case "issuance_window":
				p.MaxMembers = 1
				require.NoError(t, f.exec(&disbursementtypes.MsgUpdateParams{Authority: f.authority.String(), Params: p}))
				msg = &disbursementtypes.MsgRegisterMembers{Registrar: f.registrar.String(), Addresses: []string{nativeAddress("other").String()}}
			case "unfunded":
				msg = &disbursementtypes.MsgCreateGrant{Authority: f.authority.String(), Kind: disbursementtypes.GrantKind_GRANT_KIND_COMPENSATION, Beneficiary: member.String(), Amount: chain.NoahCoin(math.NewInt(40_000)), Schedule: []disbursementtypes.Period{{Length: 1, Parts: 1}}, Reference: "work"}
			case "wrong_asset":
				msg = &disbursementtypes.MsgCreateGrant{Authority: f.authority.String(), Kind: disbursementtypes.GrantKind_GRANT_KIND_OWNERSHIP, Beneficiary: member.String(), Amount: sdk.NewInt64Coin("ausd", 1), Schedule: []disbursementtypes.Period{{Length: 1, Parts: 1}}, Reference: "work"}
			case "reserved_return":
				msg = &disbursementtypes.MsgReturnUnallocated{Authority: f.authority.String(), Amount: chain.NoahCoin(math.NewInt(20_001))}
			case "blocked_batch":
				msg = &disbursementtypes.MsgRegisterMembers{Registrar: f.registrar.String(), Addresses: []string{nativeAddress("other").String(), authtypes.NewModuleAddress(authtypes.FeeCollectorName).String()}}
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
		f.fund(t, chain.NoahCoin(math.NewInt(100_000)))
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
			f.fund(t, chain.NoahCoin(math.NewInt(100_000)))
			p, err := f.app.DisbursementKeeper.Params.Get(f.ctx)
			require.NoError(t, err)
			p.MaxMembers, p.WindowSeconds = 1, 12
			require.NoError(t, f.exec(&disbursementtypes.MsgUpdateParams{Authority: f.authority.String(), Params: p}))
			a, b := nativeAddress("a"), nativeAddress("b")
			id := f.register(t, a)
			terms := f.grant(t, id)
			require.NoError(t, f.exec(&disbursementtypes.MsgSuspendMembers{Registrar: f.registrar.String(), Addresses: []string{a.String()}}))
			p.MemberAmount = math.NewInt(20_000)
			p.MemberSchedule = []disbursementtypes.Period{{Length: 1, Parts: 1}, {Length: 100, Parts: 1}}
			require.NoError(t, f.exec(&disbursementtypes.MsgUpdateParams{Authority: f.authority.String(), Params: p}))
			f.advance(11)
			require.Error(t, f.exec(&disbursementtypes.MsgRegisterMembers{Registrar: f.registrar.String(), Addresses: []string{b.String()}}))
			f.advance(1)
			other := f.register(t, b)
			require.Equal(t, p.MemberAmount, f.grant(t, other).Amount.Amount)
			require.Equal(t, terms.Amount, f.grant(t, id).Amount)
			require.Equal(t, terms.Schedule, f.grant(t, id).Schedule)
			if recovery == "void" {
				require.NoError(t, f.exec(&disbursementtypes.MsgVoidSuspensions{Authority: f.authority.String(), Registrar: f.registrar.String()}))
			} else {
				require.NoError(t, f.exec(&disbursementtypes.MsgReinstateMembers{Sender: f.authority.String(), Addresses: []string{a.String()}}))
			}
			// An invalidated suspension cannot backdate a later cancellation.
			require.NoError(t, f.exec(&disbursementtypes.MsgCancelGrants{Authority: f.authority.String(), GrantIds: []uint64{id}}))
			require.Equal(t, math.NewInt(3_000), f.grant(t, id).Remaining)
			require.Equal(t, uint64(f.ctx.BlockTime().Unix()), f.grant(t, id).CutoffTime)
			f.check(t)
		})
	}
}

func TestNativeDisbursementPaymentBatchRollback(t *testing.T) {
	t.Run("second_destination_refuses", func(t *testing.T) {
		f := newNativeDisbursement(t)
		a, b := nativeAddress("a"), nativeAddress("b")
		f.fund(t, chain.NoahCoin(math.NewInt(20_000)))
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
		f.fund(t, chain.NoahCoin(nativeNoah(80_000_000)))
		apptestutil.FundModule(t, f.app, f.ctx, stakingtypes.BondedPoolName, sdk.NewCoins(chain.NoahCoin(nativeNoah(60_000_000))))
		schedule := []disbursementtypes.Period{{Length: 10, Parts: 1}, {Length: 10, Parts: 1}}
		first := f.create(t, disbursementtypes.GrantKind_GRANT_KIND_OWNERSHIP, a, chain.NoahCoin(nativeNoah(20_000_000)), schedule)
		second := f.create(t, disbursementtypes.GrantKind_GRANT_KIND_OWNERSHIP, b, chain.NoahCoin(nativeNoah(60_000_000)), schedule)
		f.advance(10)
		qa, err := f.query.Releasable(f.ctx, &disbursementtypes.QueryReleasableRequest{Id: first})
		require.NoError(t, err)
		qb, err := f.query.Releasable(f.ctx, &disbursementtypes.QueryReleasableRequest{Id: second})
		require.NoError(t, err)
		room := qa.Bonded.QuoRaw(3).Sub(nativeNoah(10_000_000))
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
			f.fund(t, chain.NoahCoin(math.NewInt(10_000)))
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
