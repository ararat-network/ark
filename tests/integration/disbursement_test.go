package integration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	query "github.com/cosmos/cosmos-sdk/types/query"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/ararat-network/ark/app"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/pagination"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	disbursementkeeper "github.com/ararat-network/ark/x/disbursement/keeper"
	disbursementtypes "github.com/ararat-network/ark/x/disbursement/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

type nativeDisbursementFixture struct {
	app                             *app.ArkApp
	ctx                             sdk.Context
	committee, registrar, authority sdk.AccAddress
	// term is the committee's live term, registrarTerm the registrar's.
	term, registrarTerm uint64
	query               disbursementtypes.QueryServer
	// emitted collects the events each committed message returned through the router.
	emitted []cmtabci.Event
}

func nativeAddress(name string) sdk.AccAddress {
	return authtypes.NewModuleAddress("native-disbursement-" + name)
}
func nativeNoah(n int64) math.Int { return chain.NativeBaseAmount(n) }

// tenFounders pads the given operators to the ten seats of a launch set.
func tenFounders(operators ...sdk.AccAddress) []sdk.AccAddress {
	founders := append([]sdk.AccAddress{}, operators...)
	for i := len(founders); i < 10; i++ {
		founders = append(founders, nativeAddress(fmt.Sprintf("founder-%d", i)))
	}
	return founders
}

func newNativeDisbursement(t *testing.T, founders ...sdk.AccAddress) *nativeDisbursementFixture {
	t.Helper()
	a := apptestutil.Setup(t, false)
	f := &nativeDisbursementFixture{app: a, ctx: a.NewContextLegacy(false, cmtproto.Header{Height: 10, Time: time.Unix(1_800_000_000, 0)}), committee: nativeAddress("committee"), registrar: nativeAddress("registrar"), authority: authtypes.NewModuleAddress(govtypes.ModuleName), query: disbursementkeeper.NewQueryServerImpl(a.DisbursementKeeper)}
	g := disbursementtypes.DefaultGenesisState()
	// The first founder holds the founder's seat, half the others' sum, as launch assembly seats them.
	for i, founder := range founders {
		seat := chain.SeatGrant
		if i == 0 {
			seat = chain.FounderSeat(len(founders))
		}
		g.Beneficiaries = append(g.Beneficiaries, disbursementtypes.Beneficiary{Address: founder.String(), Controller: founder.String(), OwnershipCommitted: math.ZeroInt(), Seat: seat})
	}
	disbursementtypes.SortBeneficiaries(g.Beneficiaries)
	require.NoError(t, a.DisbursementKeeper.InitGenesis(f.ctx, g))
	// Compensation is valued in the reference unit when awarded: price NOAH and ausd at one each and
	// keep the rates fresh across the long advances below.
	op, err := a.OracleKeeper.Params.Get(f.ctx)
	require.NoError(t, err)
	op.MaxExchangeRateAge = 100 * 365 * 86_400
	require.NoError(t, a.OracleKeeper.Params.Set(f.ctx, op))
	f.setRate(t, chain.XDRBaseDenom, math.LegacyOneDec())
	f.setRate(t, "ausd", math.LegacyOneDec())
	p := g.Params
	p.MemberAmount = math.NewInt(10_000)
	// Short shapes past the two-week floor every first period keeps: a member's tenth and an ownership
	// quarter at the floor, then the rest ten seconds apart; compensation in periods of the floor.
	p.MemberSchedule = disbursementtypes.Schedule{FirstLength: hold, FirstWeight: 1, PeriodLength: 10, PeriodWeight: 3, Periods: 3}
	p.OwnershipSchedule = disbursementtypes.Schedule{FirstLength: hold, FirstWeight: 1, PeriodLength: 10, PeriodWeight: 3, Periods: 1}
	p.CompensationPeriod = hold
	require.NoError(t, f.exec(&disbursementtypes.MsgUpdateParams{Authority: f.authority.String(), Params: p}))
	f.appoint(t, f.committee)
	f.appointRegistrar(t, f.registrar)
	return f
}

// setRate stores a fresh oracle rate, NOAH per unit, as the feed would.
func (f *nativeDisbursementFixture) setRate(t *testing.T, denom string, rate math.LegacyDec) {
	t.Helper()
	require.NoError(t, f.app.OracleKeeper.ExchangeRate.Set(f.ctx, denom, oracletypes.ExchangeRate{
		Denom: denom, Rate: rate, BlockTime: uint64(f.ctx.BlockTime().Unix()), BlockHeight: uint64(f.ctx.BlockHeight()),
	}))
}

// appoint replaces the committee for a long window; every call advances the term.
func (f *nativeDisbursementFixture) appoint(t *testing.T, committee sdk.AccAddress) {
	t.Helper()
	f.appointWith(t, committee, math.ZeroInt())
}

// appointWith appoints the committee with its compensation allowance. A committee appointment
// clears an appointed registrar, advancing its term.
func (f *nativeDisbursementFixture) appointWith(t *testing.T, committee sdk.AccAddress, compensation math.Int) {
	t.Helper()
	now := f.now()
	registrar, err := f.app.DisbursementKeeper.RegistrarMandate.Get(f.ctx)
	require.NoError(t, err)
	require.NoError(t, f.exec(&disbursementtypes.MsgSetGrantsMandate{
		Authority: f.authority.String(), Committee: committee.String(), ActivationTime: now, ExpiryTime: now + disbursementtypes.MaxPeriodLength,
		CompensationAllowance: compensation,
	}))
	f.term++
	if !registrar.IsDisabled() {
		f.registrarTerm++
	}
}

// appointRegistrar names the registrar under the committee's term for the committee's own window;
// every call advances the registrar term.
func (f *nativeDisbursementFixture) appointRegistrar(t *testing.T, registrar sdk.AccAddress) {
	t.Helper()
	committee, err := f.app.DisbursementKeeper.GrantsMandate.Get(f.ctx)
	require.NoError(t, err)
	require.NoError(t, f.exec(&disbursementtypes.MsgCommitteeRegistrar{
		Committee: f.committee.String(), ExpectedTerm: f.term, Registrar: registrar.String(),
		ActivationTime: committee.ActivationTime, ExpiryTime: committee.ExpiryTime,
	}))
	f.registrarTerm++
}

// registrarStatus reads the registrar appointment and whether it is active.
func (f *nativeDisbursementFixture) registrarStatus(t *testing.T) *disbursementtypes.QueryRegistrarMandateResponse {
	t.Helper()
	res, err := f.query.RegistrarMandate(f.ctx, &disbursementtypes.QueryRegistrarMandateRequest{})
	require.NoError(t, err)
	return res
}

func (f *nativeDisbursementFixture) exec(msgs ...sdk.Msg) error {
	ctx, write := f.ctx.CacheContext()
	for _, msg := range msgs {
		h := f.app.MsgServiceRouter().Handler(msg)
		if h == nil {
			return errors.New("unregistered grant message")
		}
		res, err := h(ctx, msg)
		if err != nil {
			return err
		}
		f.emitted = append(f.emitted, res.Events...)
	}
	write()
	return nil
}

// events returns every typed event of want's type that the fixture's committed messages emitted.
func (f *nativeDisbursementFixture) events(t *testing.T, want proto.Message) []proto.Message {
	t.Helper()
	var out []proto.Message
	for _, e := range f.emitted {
		if e.Type != proto.MessageName(want) {
			continue
		}
		got, err := sdk.ParseTypedEvent(e)
		require.NoError(t, err)
		out = append(out, got)
	}
	return out
}

// fund deposits into contributor custody, as a community pool spend does.
func (f *nativeDisbursementFixture) fund(t *testing.T, coin sdk.Coin) {
	t.Helper()
	funder := nativeAddress("funder")
	coins := sdk.NewCoins(coin)
	apptestutil.FundAccount(t, f.app, f.ctx, funder, coins)
	require.NoError(t, f.app.DistrKeeper.FundCommunityPool(f.ctx, coins, funder))
	require.NoError(t, f.exec(&distrtypes.MsgCommunityPoolSpend{Authority: f.authority.String(), Recipient: authtypes.NewModuleAddress(disbursementtypes.ContributorsName).String(), Amount: coins}))
}

// account reads one account's custody in a denomination from the Balance query.
func (f *nativeDisbursementFixture) account(t *testing.T, name, denom string) disbursementtypes.AccountBalance {
	t.Helper()
	balance, err := f.query.Balance(f.ctx, &disbursementtypes.QueryBalanceRequest{Denom: denom})
	require.NoError(t, err)
	for _, a := range balance.Accounts {
		if a.Account == name {
			return a
		}
	}
	require.FailNowf(t, "missing account", "%s holds no %s", name, denom)
	return disbursementtypes.AccountBalance{}
}

// seed stands in for genesis funding: only genesis grows the member pool.
func (f *nativeDisbursementFixture) seed(t *testing.T, account string, amount math.Int) {
	t.Helper()
	apptestutil.FundModule(t, f.app, f.ctx, account, sdk.NewCoins(chain.NoahCoin(amount)))
}

// open seeds the member account and opens the same amount as governance would for a step.
func (f *nativeDisbursementFixture) open(t *testing.T, amount math.Int) {
	t.Helper()
	f.seed(t, disbursementtypes.MembersName, amount)
	require.NoError(t, f.exec(&disbursementtypes.MsgOpenTranche{Authority: f.authority.String(), Amount: amount}))
}

// mirror funds a fresh app's accounts with this one's custody, as Bank's export would.
func (f *nativeDisbursementFixture) mirror(t *testing.T, fresh *nativeDisbursementFixture, denoms ...string) {
	t.Helper()
	for _, account := range disbursementtypes.PoolAccountNames() {
		for _, denom := range denoms {
			if balance := f.app.BankKeeper.GetBalance(f.ctx, authtypes.NewModuleAddress(account), denom); balance.IsPositive() {
				apptestutil.FundModule(t, fresh.app, fresh.ctx, account, sdk.NewCoins(balance))
			}
		}
	}
}

// hold is every first period in the fixture: the two-week floor nothing pays before (D126).
const hold = disbursementtypes.MinFirstPeriod

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
	require.NoError(t, f.exec(&disbursementtypes.MsgRegisterMembers{Registrar: f.registrar.String(), ExpectedTerm: f.registrarTerm, Addresses: []string{address.String()}, Reference: "admission batch"}))
	return id
}

// create awards a governance grant whose clock starts now: ownership on the fixture's shape, compensation
// over a count of its periods, one for a lump.
func (f *nativeDisbursementFixture) create(t *testing.T, kind disbursementtypes.GrantKind, address sdk.AccAddress, coin sdk.Coin, periods uint32) uint64 {
	t.Helper()
	return f.createAt(t, kind, address, coin, f.now(), periods)
}

// createAt is create with the grantor's start time.
func (f *nativeDisbursementFixture) createAt(t *testing.T, kind disbursementtypes.GrantKind, address sdk.AccAddress, coin sdk.Coin, start uint64, periods uint32) uint64 {
	t.Helper()
	id, err := f.app.DisbursementKeeper.NextGrantID.Peek(f.ctx)
	require.NoError(t, err)
	var msg sdk.Msg = &disbursementtypes.MsgCompensate{Authority: f.authority.String(), Beneficiary: address.String(), Amount: coin, StartTime: start, Periods: periods, Reference: "Public contributor agreement"}
	if kind == disbursementtypes.GrantKind_GRANT_KIND_OWNERSHIP {
		require.Equal(t, chain.NoahBaseDenom, coin.Denom)
		require.Zero(t, periods)
		msg = &disbursementtypes.MsgGrantOwnership{Authority: f.authority.String(), Beneficiary: address.String(), Amount: coin.Amount, StartTime: start, Reference: "Public contributor agreement"}
	}
	require.NoError(t, f.exec(msg))
	return id
}

// endBlock pays what is due, as the chain does at the end of a block, and keeps its events.
func (f *nativeDisbursementFixture) endBlock(t *testing.T) {
	t.Helper()
	ctx := f.ctx.WithEventManager(sdk.NewEventManager())
	require.NoError(t, f.app.DisbursementKeeper.EndBlocker(ctx))
	f.emitted = append(f.emitted, ctx.EventManager().ABCIEvents()...)
}

// now is the fixture's block time as the module records it.
func (f *nativeDisbursementFixture) now() uint64 { return uint64(f.ctx.BlockTime().Unix()) }

// deferred returns the deferral events the fixture has seen.
func (f *nativeDisbursementFixture) deferred(t *testing.T) []*disbursementtypes.EventPaymentDeferred {
	t.Helper()
	var out []*disbursementtypes.EventPaymentDeferred
	for _, e := range f.events(t, &disbursementtypes.EventPaymentDeferred{}) {
		out = append(out, e.(*disbursementtypes.EventPaymentDeferred))
	}
	return out
}

func (f *nativeDisbursementFixture) check(t *testing.T) *disbursementtypes.GenesisState {
	t.Helper()
	g, err := f.app.DisbursementKeeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NoError(t, g.Validate())
	balance, err := f.query.Balance(f.ctx, &disbursementtypes.QueryBalanceRequest{})
	require.NoError(t, err)
	listed := map[string]math.Int{}
	for _, a := range balance.Accounts {
		require.True(t, a.Balance.Equal(a.Reserved.Add(a.Uncommitted)), a.Account)
		if a.Account == disbursementtypes.MembersName {
			require.True(t, balance.MemberTranche.LTE(a.Uncommitted.Amount))
		}
		listed[a.Account+"/"+a.Balance.Denom] = a.Reserved.Amount
	}
	for _, book := range g.Books {
		for _, c := range book.Reserved {
			reserved, ok := listed[book.Account+"/"+c.Denom]
			require.True(t, ok && reserved.Equal(c.Amount), "%s lists its %s reservation", book.Account, c.Denom)
		}
	}
	return g
}

func TestNativeDisbursementMemberCancellation(t *testing.T) {
	for _, suspended := range []bool{false, true} {
		t.Run(fmt.Sprintf("suspended_%t", suspended), func(t *testing.T) {
			f := newNativeDisbursement(t)
			member := nativeAddress("member")
			payee := nativeAddress("new-payee")
			f.open(t, math.NewInt(10_000))
			id := f.register(t, member)
			require.True(t, f.app.BankKeeper.GetBalance(f.ctx, member, chain.NoahBaseDenom).IsZero(), "the tenth waits for the hold")
			f.advance(int64(hold) + 10)
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
			require.True(t, g.Cancelled())
			require.Equal(t, math.NewInt(4_000), g.Remaining, "the tenth and the first month retained")
			require.Equal(t, math.NewInt(6_000), g.Amount.Amount.Sub(g.Paid).Sub(g.Remaining), "unearned principal freed")
			require.Equal(t, f.now(), g.DueTime, "the retained debt is due at once")
			// The block defers the refused payment and waits a retry; the debt stays retained.
			f.endBlock(t)
			deferred := f.deferred(t)
			require.Len(t, deferred, 1)
			g = f.grant(t, id)
			require.Equal(t, math.NewInt(4_000), g.Remaining)
			require.Equal(t, f.now()+disbursementtypes.PaymentRetryInterval, g.DueTime)
			require.True(t, f.app.BankKeeper.GetBalance(f.ctx, member, chain.NoahBaseDenom).IsZero())
			f.check(t)
			require.Error(t, f.exec(&disbursementtypes.MsgSetPayee{Sender: f.committee.String(), GrantId: id, Payee: payee.String()}))
			require.NoError(t, f.exec(&disbursementtypes.MsgSetPayee{Sender: member.String(), GrantId: id, Payee: payee.String()}))
			// A new payee is looked at in its own block rather than at the retry.
			require.Equal(t, f.now(), f.grant(t, id).DueTime)
			f.endBlock(t)
			require.Equal(t, math.NewInt(4_000), f.app.BankKeeper.GetBalance(f.ctx, payee, chain.NoahBaseDenom).Amount)
			require.Zero(t, f.grant(t, id).DueTime, "nothing left to look at")
			require.Error(t, f.exec(&disbursementtypes.MsgRegisterMembers{Registrar: f.registrar.String(), ExpectedTerm: f.registrarTerm, Addresses: []string{member.String()}, Reference: "admission batch"}))
			require.Error(t, f.exec(&disbursementtypes.MsgCommitteeReinstate{Committee: f.committee.String(), ExpectedTerm: f.term, Addresses: []string{member.String()}}))
			f.check(t)
		})
	}
}

func TestNativeDisbursementSuspensionRecovery(t *testing.T) {
	for _, mode := range []string{"committee", "governance", "replacement"} {
		t.Run(mode, func(t *testing.T) {
			f := newNativeDisbursement(t)
			member := nativeAddress("member")
			f.open(t, math.NewInt(10_000))
			id := f.register(t, member)
			require.NoError(t, f.exec(&disbursementtypes.MsgCommitteeSuspend{Committee: f.committee.String(), ExpectedTerm: f.term, Addresses: []string{member.String()}}))
			require.Zero(t, f.grant(t, id).DueTime, "a suspended member leaves the queue")
			f.advance(int64(hold) + 10)
			f.endBlock(t)
			require.True(t, f.grant(t, id).Paid.IsZero(), "suspended inside its hold, it is paid nothing")
			switch mode {
			case "replacement":
				// A suspension stands across a committee replacement until someone reinstates it.
				f.appoint(t, f.committee)
				f.endBlock(t)
				require.True(t, f.grant(t, id).Paid.IsZero())
				require.NoError(t, f.exec(&disbursementtypes.MsgReinstateMembers{Authority: f.authority.String(), Addresses: []string{member.String()}}))
			case "governance":
				require.NoError(t, f.exec(&disbursementtypes.MsgReinstateMembers{Authority: f.authority.String(), Addresses: []string{member.String()}}))
			default:
				require.NoError(t, f.exec(&disbursementtypes.MsgCommitteeReinstate{Committee: f.committee.String(), ExpectedTerm: f.term, Addresses: []string{member.String()}}))
			}
			// Reinstatement queues the arrears for this block.
			require.Equal(t, f.now(), f.grant(t, id).DueTime)
			f.endBlock(t)
			require.Equal(t, math.NewInt(4_000), f.grant(t, id).Paid)
			f.check(t)
		})
	}
}

func TestNativeDisbursementOwnershipAndFounders(t *testing.T) {
	for _, founder := range []bool{false, true} {
		t.Run(fmt.Sprintf("founder_%t", founder), func(t *testing.T) {
			beneficiary := nativeAddress("owner")
			// The founder sits among the launch ten with the founder's seat, 22.5M at
			// ten, recorded on the beneficiary; the cap reads committed ownership alone,
			// so nothing differs from anyone else's record (D115, D121).
			var founders []sdk.AccAddress
			seat := math.ZeroInt()
			if founder {
				founders = tenFounders(beneficiary)
				seat = chain.FounderSeat(len(founders))
				require.Equal(t, nativeNoah(22_500_000), seat)
			}
			f := newNativeDisbursement(t, founders...)
			f.seed(t, disbursementtypes.ContributorsName, nativeNoah(100_000_000))
			cap := nativeNoah(disbursementtypes.DefaultOwnershipCap)
			ownership := func(amount math.Int) *disbursementtypes.MsgGrantOwnership {
				return &disbursementtypes.MsgGrantOwnership{Authority: f.authority.String(), Beneficiary: beneficiary.String(), Amount: amount, StartTime: f.now(), Reference: "Public contributor agreement"}
			}
			// The cap bounds what the record's grants commit, so an award past it is refused outright.
			require.ErrorContains(t, f.exec(ownership(nativeNoah(80_000_000))), "remaining ownership")
			id := f.create(t, disbursementtypes.GrantKind_GRANT_KIND_OWNERSHIP, beneficiary, chain.NoahCoin(cap), 0)
			require.Equal(t, f.now()+hold, f.grant(t, id).DueTime, "queued for its first boundary")
			record, err := f.query.Beneficiary(f.ctx, &disbursementtypes.QueryBeneficiaryRequest{Address: beneficiary.String()})
			require.NoError(t, err)
			require.Equal(t, cap, record.Beneficiary.OwnershipCommitted)
			require.True(t, record.OwnershipRemaining.IsZero(), "an award at the cap leaves no room")
			require.ErrorContains(t, f.exec(ownership(nativeNoah(1))), "remaining ownership")
			f.endBlock(t)
			require.True(t, f.grant(t, id).Paid.IsZero(), "nothing accrued")
			f.advance(int64(hold))
			// The first period pays its quarter whole: nothing at payment reads the cap.
			f.endBlock(t)
			paid := f.grant(t, id).Paid
			require.Equal(t, cap.QuoRaw(4), paid)
			require.Equal(t, f.now()+10, f.grant(t, id).DueTime)
			controller, payee := nativeAddress("controller"), nativeAddress("payee")
			require.NoError(t, f.exec(&disbursementtypes.MsgSetController{Sender: beneficiary.String(), Beneficiary: beneficiary.String(), Controller: controller.String()}))
			require.NoError(t, f.exec(&disbursementtypes.MsgSetPayee{Sender: controller.String(), GrantId: id, Payee: payee.String()}))
			require.ErrorContains(t, f.exec(&disbursementtypes.MsgSetController{Sender: controller.String(), Beneficiary: beneficiary.String(), Controller: controller.String()}), "unchanged")
			require.ErrorContains(t, f.exec(&disbursementtypes.MsgSetPayee{Sender: controller.String(), GrantId: id, Payee: payee.String()}), "unchanged")
			entries, err := f.query.BeneficiaryJournal(f.ctx, &disbursementtypes.QueryBeneficiaryJournalRequest{Address: beneficiary.String()})
			require.NoError(t, err)
			require.Len(t, entries.Entries, 1, "one controller change, listed by beneficiary")
			require.Equal(t, controller.String(), entries.Entries[0].Controller)
			// A cancel frees what the grant had not earned; the paid quarter stays committed.
			require.NoError(t, f.exec(&disbursementtypes.MsgCancelGrants{Authority: f.authority.String(), GrantIds: []uint64{id}}))
			person, err := f.query.Beneficiary(f.ctx, &disbursementtypes.QueryBeneficiaryRequest{Address: beneficiary.String()})
			require.NoError(t, err)
			require.Equal(t, paid, person.Beneficiary.OwnershipCommitted)
			require.Equal(t, cap.Sub(paid), person.OwnershipRemaining)
			require.Equal(t, controller.String(), person.Beneficiary.Controller)
			require.Equal(t, seat, person.Beneficiary.Seat)
			// The freed room takes a new award without a founder classification input, and it pays in full.
			other := f.create(t, disbursementtypes.GrantKind_GRANT_KIND_OWNERSHIP, beneficiary, chain.NoahCoin(cap.Sub(paid)), 0)
			require.ErrorContains(t, f.exec(ownership(nativeNoah(1))), "remaining ownership")
			f.advance(int64(hold) + 10)
			f.endBlock(t)
			require.Equal(t, cap.Sub(paid), f.grant(t, other).Paid)
			require.Zero(t, f.grant(t, other).DueTime, "paid in full, out of the queue")
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
		f.fund(t, sdk.NewInt64Coin("ausd", 12_000))
		id := f.create(t, disbursementtypes.GrantKind_GRANT_KIND_COMPENSATION, beneficiary, sdk.NewInt64Coin("ausd", 12_000), 3)
		f.endBlock(t)
		require.True(t, f.grant(t, id).Paid.IsZero(), "nothing accrued")
		collector := authtypes.NewModuleAddress(treasurytypes.TransferTaxCollectorName)
		before := f.app.BankKeeper.GetBalance(f.ctx, collector, "ausd")
		asset.Status = assettypes.AssetStatus_ASSET_STATUS_SUSPENDED
		require.NoError(t, f.app.AssetKeeper.Assets.Set(f.ctx, "ausd", asset))
		require.ErrorContains(t, f.exec(&disbursementtypes.MsgCompensate{Authority: f.authority.String(), Beneficiary: beneficiary.String(), Amount: sdk.NewInt64Coin("ausd", 1), StartTime: f.now(), Periods: 1, Reference: "new work"}), "active asset")
		f.advance(int64(hold))
		f.endBlock(t)
		require.Equal(t, math.NewInt(4_000), f.app.BankKeeper.GetBalance(f.ctx, beneficiary, "ausd").Amount)
		require.Equal(t, before, f.app.BankKeeper.GetBalance(f.ctx, collector, "ausd"))
		person, err := f.query.Beneficiary(f.ctx, &disbursementtypes.QueryBeneficiaryRequest{Address: beneficiary.String()})
		require.NoError(t, err)
		require.True(t, person.Beneficiary.OwnershipCommitted.IsZero())
		f.advance(int64(hold))
		require.NoError(t, f.exec(&disbursementtypes.MsgCancelGrants{Authority: f.authority.String(), GrantIds: []uint64{id}}))
		g := f.grant(t, id)
		require.Equal(t, math.NewInt(4_000), g.Remaining)
		require.Equal(t, math.NewInt(4_000), g.Amount.Amount.Sub(g.Paid).Sub(g.Remaining), "unearned principal freed")
		require.Equal(t, math.NewInt(4_000), f.account(t, disbursementtypes.ContributorsName, "ausd").Uncommitted.Amount, "unearned stablecoins stay for the next award")
		pool, err := f.app.DistrKeeper.FeePool.Get(f.ctx)
		require.NoError(t, err)
		require.True(t, pool.CommunityPool.AmountOf("ausd").IsZero())
		require.Equal(t, f.now(), g.DueTime, "the retained period is due at once")
		f.endBlock(t)
		require.Equal(t, math.NewInt(8_000), f.grant(t, id).Paid, "two periods paid before the cancel, the retained third after")
		f.check(t)
	})
}

func TestNativeDisbursementAtomicRefusals(t *testing.T) {
	for _, tc := range []string{"wrong_registrar", "duplicate_members", "batch_overflow", "unfunded", "reserved_return", "blocked_batch", "pool_member", "pool_award", "pool_payee", "empty_reference", "long_reference"} {
		t.Run(tc, func(t *testing.T) {
			f := newNativeDisbursement(t)
			member := nativeAddress("member")
			f.open(t, math.NewInt(30_000))
			id := f.register(t, member)
			var msg sdk.Msg
			switch tc {
			case "wrong_registrar":
				msg = &disbursementtypes.MsgRegisterMembers{Registrar: member.String(), ExpectedTerm: f.registrarTerm, Addresses: []string{nativeAddress("other").String()}, Reference: "admission batch"}
			case "duplicate_members":
				msg = &disbursementtypes.MsgRegisterMembers{Registrar: f.registrar.String(), ExpectedTerm: f.registrarTerm, Addresses: []string{member.String()}, Reference: "admission batch"}
			case "batch_overflow":
				msg = &disbursementtypes.MsgCancelGrants{Authority: f.authority.String(), GrantIds: make([]uint64, disbursementtypes.MaxBatch+1)}
			case "unfunded":
				msg = &disbursementtypes.MsgCompensate{Authority: f.authority.String(), Beneficiary: member.String(), Amount: chain.NoahCoin(math.NewInt(40_000)), StartTime: f.now(), Periods: 1, Reference: "work"}
			case "reserved_return":
				msg = &disbursementtypes.MsgReturnUnallocated{Authority: f.authority.String(), Amount: chain.NoahCoin(math.NewInt(20_001))}
			case "blocked_batch":
				msg = &disbursementtypes.MsgRegisterMembers{Registrar: f.registrar.String(), ExpectedTerm: f.registrarTerm, Addresses: []string{nativeAddress("other").String(), authtypes.NewModuleAddress(authtypes.FeeCollectorName).String()}, Reference: "admission batch"}
			case "pool_member":
				msg = &disbursementtypes.MsgRegisterMembers{Registrar: f.registrar.String(), ExpectedTerm: f.registrarTerm, Addresses: []string{authtypes.NewModuleAddress(disbursementtypes.MembersName).String()}, Reference: "admission batch"}
			case "pool_award":
				f.seed(t, disbursementtypes.ContributorsName, math.NewInt(1_000))
				msg = &disbursementtypes.MsgCompensate{Authority: f.authority.String(), Beneficiary: authtypes.NewModuleAddress(disbursementtypes.ContributorsName).String(), Amount: chain.NoahCoin(math.NewInt(100)), StartTime: f.now(), Periods: 1, Reference: "work"}
			case "pool_payee":
				msg = &disbursementtypes.MsgSetPayee{Sender: member.String(), GrantId: id, Payee: authtypes.NewModuleAddress(disbursementtypes.ContributorsName).String()}
			case "empty_reference":
				msg = &disbursementtypes.MsgRegisterMembers{Registrar: f.registrar.String(), ExpectedTerm: f.registrarTerm, Addresses: []string{nativeAddress("other").String()}}
			case "long_reference":
				msg = &disbursementtypes.MsgRegisterMembers{Registrar: f.registrar.String(), ExpectedTerm: f.registrarTerm, Addresses: []string{nativeAddress("other").String()}, Reference: strings.Repeat("x", chain.MaxReferenceBytes+1)}
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
		f.open(t, math.NewInt(100_000))
		a, b := nativeAddress("a"), nativeAddress("b")
		id := f.register(t, a)
		f.register(t, b)
		f.advance(int64(hold) + 10)
		f.endBlock(t)
		require.NoError(t, f.exec(&disbursementtypes.MsgCancelGrants{Authority: f.authority.String(), GrantIds: []uint64{id}}))
		res, err := f.query.Grants(f.ctx, &disbursementtypes.QueryGrantsRequest{Beneficiary: a.String()})
		require.NoError(t, err)
		require.Len(t, res.Grants, 1)
		require.Equal(t, id, res.Grants[0].Id)
		var key []byte
		var entries []disbursementtypes.GrantEntry
		for {
			r, err := f.query.GrantJournal(f.ctx, &disbursementtypes.QueryGrantJournalRequest{GrantId: id, Pagination: &query.PageRequest{Limit: 1, Key: key}})
			require.NoError(t, err)
			entries = append(entries, r.Entries...)
			key = r.Pagination.NextKey
			if len(key) == 0 {
				break
			}
		}
		// Created, the tenth and first month paid together after the hold, cancelled.
		require.Len(t, entries, 3)
		for _, e := range entries {
			require.Equal(t, id, e.GrantId)
		}
		_, err = f.query.Grants(f.ctx, &disbursementtypes.QueryGrantsRequest{Pagination: &query.PageRequest{CountTotal: true}})
		require.Error(t, err)
		exported := f.check(t)
		fresh := newNativeDisbursement(t)
		fresh.ctx = fresh.ctx.WithBlockTime(f.ctx.BlockTime()).WithBlockHeight(f.ctx.BlockHeight())
		f.mirror(t, fresh, chain.NoahBaseDenom)
		require.NoError(t, fresh.app.DisbursementKeeper.InitGenesis(fresh.ctx, exported))
		require.Equal(t, exported, fresh.check(t))
		// The due queue is rebuilt from the grants' own times.
		queued, err := fresh.app.DisbursementKeeper.Due.Has(fresh.ctx, collections.Join(fresh.grant(t, id+1).DueTime, id+1))
		require.NoError(t, err)
		require.True(t, queued)
		fresh.advance(20)
		fresh.endBlock(t)
		require.Equal(t, math.NewInt(10_000), fresh.grant(t, id+1).Paid)
		require.Zero(t, fresh.grant(t, id+1).DueTime)
		fresh.check(t)
	})
	t.Run("balance_denominations", func(t *testing.T) {
		f := newNativeDisbursement(t)
		members, contributors := disbursementtypes.MembersName, disbursementtypes.ContributorsName
		f.open(t, math.NewInt(10_000))
		f.fund(t, sdk.NewInt64Coin("ausd", 12_000))
		f.create(t, disbursementtypes.GrantKind_GRANT_KIND_COMPENSATION, nativeAddress("worker"), sdk.NewInt64Coin("ausd", 12_000), 1)
		f.fund(t, sdk.NewInt64Coin("aeur", 3_000)) // registered, unreserved; every other registered asset is unheld
		donor, donation := nativeAddress("donor"), sdk.NewCoins(sdk.NewInt64Coin("adonated", 5))
		apptestutil.FundAccount(t, f.app, f.ctx, donor, donation)
		require.ErrorContains(t, f.app.BankKeeper.SendCoins(f.ctx, donor, authtypes.NewModuleAddress(contributors), donation), "registered asset", "Bank refuses custody nothing can grant")
		stable := sdk.NewCoins(sdk.NewInt64Coin("ausd", 7))
		apptestutil.FundAccount(t, f.app, f.ctx, donor, stable)
		require.ErrorContains(t, f.app.BankKeeper.SendCoins(f.ctx, donor, authtypes.NewModuleAddress(members), stable), "member pool", "the member account takes NOAH only")
		require.NoError(t, f.app.BankKeeper.SendCoins(f.ctx, donor, authtypes.NewModuleAddress(contributors), stable), "a registered asset is admitted")
		type entry struct{ account, denom string }
		list := func(denom string) []entry {
			res, err := f.query.Balance(f.ctx, &disbursementtypes.QueryBalanceRequest{Denom: denom})
			require.NoError(t, err)
			var out []entry
			for _, a := range res.Accounts {
				out = append(out, entry{a.Account, a.Balance.Denom})
			}
			return out
		}
		for _, tc := range []struct {
			denom string
			want  []entry
		}{
			{"", []entry{{contributors, chain.NoahBaseDenom}, {contributors, "aeur"}, {contributors, "ausd"}, {members, chain.NoahBaseDenom}}},
			{chain.NoahBaseDenom, []entry{{contributors, chain.NoahBaseDenom}, {members, chain.NoahBaseDenom}}},
			{"ausd", []entry{{contributors, "ausd"}}},
			{"ajpy", []entry{{contributors, "ajpy"}}},
		} {
			require.Equal(t, tc.want, list(tc.denom), "denom %q", tc.denom)
		}
		_, err := f.query.Balance(f.ctx, &disbursementtypes.QueryBalanceRequest{Denom: "!"})
		require.Error(t, err)
		f.check(t)
	})
}

// TestNativeDisbursementStartTimes: an ownership clock never starts before its award, a compensation
// clock reaches back at most a quarter and pays its elapsed periods at the first look, and both reach ahead.
func TestNativeDisbursementStartTimes(t *testing.T) {
	f := newNativeDisbursement(t)
	worker := nativeAddress("worker")
	f.seed(t, disbursementtypes.ContributorsName, nativeNoah(1_000))
	p, err := f.app.DisbursementKeeper.Params.Get(f.ctx)
	require.NoError(t, err)
	ownership := func(start uint64) error {
		return f.exec(&disbursementtypes.MsgGrantOwnership{Authority: f.authority.String(), Beneficiary: worker.String(), Amount: nativeNoah(1), StartTime: start, Reference: "work"})
	}
	compensation := func(start uint64, periods uint32) error {
		return f.exec(&disbursementtypes.MsgCompensate{Authority: f.authority.String(), Beneficiary: worker.String(), Amount: chain.NoahCoin(math.NewInt(50)), StartTime: start, Periods: periods, Reference: "pay"})
	}
	require.ErrorContains(t, ownership(f.now()-1), "no earlier than its award")
	require.ErrorContains(t, ownership(f.now()+disbursementtypes.MaxStartOffset+1), "within a century")
	require.ErrorContains(t, compensation(f.now()-disbursementtypes.MaxCompensationBackdate-1, 1), "at most a quarter")
	require.NoError(t, compensation(f.now()-disbursementtypes.MaxCompensationBackdate, 1))
	require.ErrorContains(t, compensation(f.now(), 0), "at least one period")
	require.NoError(t, ownership(f.now()+1_000))
	// Five periods that have all elapsed: nothing pays before one period after the award, then whole.
	period := p.CompensationPeriod
	id := f.createAt(t, disbursementtypes.GrantKind_GRANT_KIND_COMPENSATION, worker, chain.NoahCoin(math.NewInt(50)), f.now()-6*period, 5)
	require.Equal(t, f.now()+period, f.grant(t, id).DueTime, "a clock already run out is due one period after the award")
	f.endBlock(t)
	require.True(t, f.grant(t, id).Paid.IsZero(), "nothing pays before the gate")
	f.advance(int64(period))
	f.endBlock(t)
	require.Equal(t, math.NewInt(50), f.grant(t, id).Paid)
	f.check(t)
}

func TestNativeDisbursementModuleAccounts(t *testing.T) {
	f := newNativeDisbursement(t)
	// A deposit to an address with no account would create a base account there.
	f.fund(t, sdk.NewInt64Coin("ausd", 1))
	for _, name := range disbursementtypes.PoolAccountNames() {
		t.Run(name, func(t *testing.T) {
			_, ok := f.app.AccountKeeper.GetAccount(f.ctx, authtypes.NewModuleAddress(name)).(sdk.ModuleAccountI)
			require.True(t, ok, "genesis creates the module account")
		})
	}
}

func TestNativeDisbursementMemberTerms(t *testing.T) {
	for _, recovery := range []string{"reinstate", "replacement"} {
		t.Run(recovery, func(t *testing.T) {
			f := newNativeDisbursement(t)
			f.open(t, math.NewInt(100_000))
			p, err := f.app.DisbursementKeeper.Params.Get(f.ctx)
			require.NoError(t, err)
			a, b := nativeAddress("a"), nativeAddress("b")
			id := f.register(t, a)
			terms := f.grant(t, id)
			require.NoError(t, f.exec(&disbursementtypes.MsgCommitteeSuspend{Committee: f.committee.String(), ExpectedTerm: f.term, Addresses: []string{a.String()}}))
			p.MemberAmount = math.NewInt(20_000)
			p.MemberSchedule = disbursementtypes.Schedule{FirstLength: hold, FirstWeight: 1, PeriodLength: 100, PeriodWeight: 1, Periods: 1}
			require.NoError(t, f.exec(&disbursementtypes.MsgUpdateParams{Authority: f.authority.String(), Params: p}))
			f.advance(int64(hold) + 12)
			other := f.register(t, b)
			require.Equal(t, p.MemberAmount, f.grant(t, other).Amount.Amount)
			require.Equal(t, terms.Amount, f.grant(t, id).Amount)
			require.Equal(t, terms.Schedule, f.grant(t, id).Schedule)
			if recovery == "replacement" {
				// A committee replacement leaves the suspension standing; governance lifts it.
				f.appoint(t, f.committee)
				f.endBlock(t)
				require.True(t, f.grant(t, id).Paid.IsZero(), "suspended inside its hold, it is paid nothing")
			}
			require.NoError(t, f.exec(&disbursementtypes.MsgReinstateMembers{Authority: f.authority.String(), Addresses: []string{a.String()}}))
			// A lapsed or lifted suspension cannot backdate a later cancellation.
			require.NoError(t, f.exec(&disbursementtypes.MsgCancelGrants{Authority: f.authority.String(), GrantIds: []uint64{id}}))
			require.Equal(t, math.NewInt(4_000), f.grant(t, id).Remaining)
			require.Equal(t, uint64(f.ctx.BlockTime().Unix()), f.grant(t, id).CutoffTime)
			f.check(t)
		})
	}
}

func TestNativeDisbursementMembersQuery(t *testing.T) {
	f := newNativeDisbursement(t)
	f.open(t, math.NewInt(100_000))
	f.seed(t, disbursementtypes.ContributorsName, math.NewInt(100_000))
	addresses := []sdk.AccAddress{nativeAddress("c"), nativeAddress("a"), nativeAddress("b")}
	ids := map[string]uint64{}
	for _, a := range addresses {
		ids[a.String()] = f.register(t, a)
	}
	// A contributor grant is no member and stays off the list.
	f.create(t, disbursementtypes.GrantKind_GRANT_KIND_COMPENSATION, nativeAddress("worker"), chain.NoahCoin(math.NewInt(1_000)), 1)
	slices.SortFunc(addresses, func(x, y sdk.AccAddress) int { return bytes.Compare(x, y) })
	first, err := f.query.Members(f.ctx, &disbursementtypes.QueryMembersRequest{Pagination: &query.PageRequest{Limit: 2}})
	require.NoError(t, err)
	require.Len(t, first.Grants, 2)
	require.NotEmpty(t, first.Pagination.NextKey)
	second, err := f.query.Members(f.ctx, &disbursementtypes.QueryMembersRequest{Pagination: &query.PageRequest{Key: first.Pagination.NextKey, Limit: 2}})
	require.NoError(t, err)
	require.Len(t, second.Grants, 1)
	require.Empty(t, second.Pagination.NextKey)
	for i, g := range append(first.Grants, second.Grants...) {
		require.Equal(t, addresses[i].String(), g.Beneficiary, "address order")
		require.Equal(t, ids[g.Beneficiary], g.Id)
		require.Equal(t, disbursementtypes.GrantKind_GRANT_KIND_MEMBER, g.Kind)
		require.Equal(t, f.registrarTerm, g.MandateTerm, "a registration carries its registrar term")
	}
	_, err = f.query.Members(f.ctx, &disbursementtypes.QueryMembersRequest{Pagination: &query.PageRequest{Limit: pagination.MaxLimit + 1}})
	require.Error(t, err)
}

func TestNativeDisbursementGrantsMandate(t *testing.T) {
	t.Run("term_window_and_appointment_bounds", func(t *testing.T) {
		f := newNativeDisbursement(t)
		f.open(t, math.NewInt(100_000))
		member := nativeAddress("member")
		require.ErrorContains(t, f.exec(&disbursementtypes.MsgRegisterMembers{Registrar: f.registrar.String(), ExpectedTerm: f.registrarTerm + 1, Addresses: []string{member.String()}, Reference: "admission batch"}), "term mismatch")
		now := f.now()
		require.ErrorContains(t, f.exec(&disbursementtypes.MsgSetGrantsMandate{Authority: f.authority.String(), Committee: f.authority.String(), ActivationTime: now, ExpiryTime: now + 10}), "distinct")
		require.ErrorContains(t, f.exec(&disbursementtypes.MsgSetGrantsMandate{Authority: f.authority.String(), Committee: f.committee.String(), ActivationTime: now - 5, ExpiryTime: now}), "not after the current block time")
		// A short window lapses on its own. The term does not move, so the hold stays effective. The
		// appointment clears the registrar, which the committee names again inside its new window.
		require.NoError(t, f.exec(&disbursementtypes.MsgSetGrantsMandate{Authority: f.authority.String(), Committee: f.committee.String(), ActivationTime: now, ExpiryTime: now + 2}))
		f.term++
		f.registrarTerm++
		f.appointRegistrar(t, f.registrar)
		id := f.register(t, member)
		require.NoError(t, f.exec(&disbursementtypes.MsgCommitteeSuspend{Committee: f.committee.String(), ExpectedTerm: f.term, Addresses: []string{member.String()}}))
		f.advance(1)
		f.advance(1)
		require.ErrorContains(t, f.exec(&disbursementtypes.MsgCommitteeReinstate{Committee: f.committee.String(), ExpectedTerm: f.term, Addresses: []string{member.String()}}), "not active")
		status, err := f.query.GrantsMandate(f.ctx, &disbursementtypes.QueryGrantsMandateRequest{})
		require.NoError(t, err)
		require.False(t, status.Active)
		require.Equal(t, f.term, status.Mandate.Term)
		f.advance(int64(hold) + 10)
		f.endBlock(t)
		require.True(t, f.grant(t, id).Paid.IsZero(), "a suspended member is not looked at")
		// Governance lifts the hold directly; disabling advances the term and refuses the old key, and
		// clears the registrar with it.
		require.NoError(t, f.exec(&disbursementtypes.MsgReinstateMembers{Authority: f.authority.String(), Addresses: []string{member.String()}}))
		f.endBlock(t)
		require.Equal(t, math.NewInt(4_000), f.grant(t, id).Paid)
		require.NoError(t, f.exec(&disbursementtypes.MsgSetGrantsMandate{Authority: f.authority.String()}))
		f.term++
		f.registrarTerm++
		require.ErrorContains(t, f.exec(&disbursementtypes.MsgCommitteeSuspend{Committee: f.committee.String(), ExpectedTerm: f.term, Addresses: []string{member.String()}}), "exact appointed")
		require.ErrorContains(t, f.exec(&disbursementtypes.MsgRegisterMembers{Registrar: f.registrar.String(), ExpectedTerm: f.registrarTerm, Addresses: []string{nativeAddress("other").String()}, Reference: "admission batch"}), "exact appointed")
		exported := f.check(t)
		require.Equal(t, f.term, exported.GrantsMandate.Term)
		require.True(t, exported.GrantsMandate.IsDisabled())
		require.True(t, exported.RegistrarMandate.IsDisabled())
		require.Equal(t, f.registrarTerm, exported.RegistrarMandate.Term)
	})
}

func TestNativeDisbursementRegistrar(t *testing.T) {
	a, b, c, d, e := nativeAddress("a"), nativeAddress("b"), nativeAddress("c"), nativeAddress("d"), nativeAddress("e")
	later, after := nativeAddress("later"), nativeAddress("after")
	registerAs := func(f *nativeDisbursementFixture, registrar sdk.AccAddress, addresses ...sdk.AccAddress) error {
		batch := make([]string, 0, len(addresses))
		for _, address := range addresses {
			batch = append(batch, address.String())
		}
		return f.exec(&disbursementtypes.MsgRegisterMembers{Registrar: registrar.String(), ExpectedTerm: f.registrarTerm, Addresses: batch, Reference: "admission batch"})
	}
	list := func(f *nativeDisbursementFixture, req *disbursementtypes.QueryGrantsRequest) []disbursementtypes.Grant {
		listed, err := f.query.Grants(f.ctx, req)
		require.NoError(t, err)
		return listed.Grants
	}
	t.Run("appointment_and_rotation", func(t *testing.T) {
		f := newNativeDisbursement(t)
		f.open(t, math.NewInt(50_000))
		committee, err := f.app.DisbursementKeeper.GrantsMandate.Get(f.ctx)
		require.NoError(t, err)
		appoint := func(signer, registrar sdk.AccAddress, activation, expiry uint64) error {
			return f.exec(&disbursementtypes.MsgCommitteeRegistrar{
				Committee: signer.String(), ExpectedTerm: f.term, Registrar: registrar.String(),
				ActivationTime: activation, ExpiryTime: expiry,
			})
		}
		// Only the committee appoints, never the authority, and only inside its own window; it may name
		// itself, and the replaced registrar's prepared batch dies with its term.
		require.ErrorContains(t, appoint(a, a, committee.ActivationTime, committee.ExpiryTime), "exact appointed")
		require.ErrorContains(t, appoint(f.committee, f.authority, committee.ActivationTime, committee.ExpiryTime), "distinct")
		require.ErrorContains(t, appoint(f.committee, f.committee, committee.ActivationTime-1, committee.ExpiryTime), "inside the committee's")
		require.ErrorContains(t, appoint(f.committee, f.committee, committee.ActivationTime, committee.ExpiryTime+1), "inside the committee's")
		require.NoError(t, appoint(f.committee, f.committee, committee.ActivationTime, committee.ExpiryTime))
		f.registrarTerm++
		require.ErrorContains(t, f.exec(&disbursementtypes.MsgRegisterMembers{Registrar: f.committee.String(), ExpectedTerm: f.registrarTerm - 1, Addresses: []string{a.String()}, Reference: "admission batch"}), "term mismatch")
		require.ErrorContains(t, registerAs(f, f.registrar, a), "exact appointed")
		require.NoError(t, registerAs(f, f.committee, a, b))
		status := f.registrarStatus(t)
		require.True(t, status.Active)
		require.Equal(t, f.committee.String(), status.Mandate.Committee)
		// The open tranche is the registrar's only bound; what a cancel reopens, the next batch may take.
		require.ErrorContains(t, registerAs(f, f.committee, c, d, e, later), "open member tranche")
		require.NoError(t, f.exec(&disbursementtypes.MsgCancelGrants{Authority: f.authority.String(), GrantIds: []uint64{1}}))
		// Cancelled inside its hold, the whole grant reopens.
		requirePool(t, f, disbursementtypes.MembersName, 40_000, 40_000)
		require.NoError(t, registerAs(f, f.committee, c, d, e))
		// The committee rotates the registrar without a vote; the old key is refused at once.
		f.appointRegistrar(t, f.registrar)
		f.open(t, math.NewInt(20_000))
		require.ErrorContains(t, registerAs(f, f.committee, later), "exact appointed")
		require.NoError(t, registerAs(f, f.registrar, later))
		set := f.events(t, &disbursementtypes.EventRegistrarMandateSet{})
		require.Len(t, set, 3, "the fixture's appointment and the two above")
		last := set[2].(*disbursementtypes.EventRegistrarMandateSet)
		require.Equal(t, f.registrarTerm, last.Term)
		require.Equal(t, f.registrar.String(), last.Registrar)
		// Each registrar term lists its own registrations; the filters exclude one another.
		require.Len(t, list(f, &disbursementtypes.QueryGrantsRequest{RegistrarTerm: f.registrarTerm - 1}), 5)
		require.Len(t, list(f, &disbursementtypes.QueryGrantsRequest{RegistrarTerm: f.registrarTerm}), 1)
		require.Empty(t, list(f, &disbursementtypes.QueryGrantsRequest{CommitteeTerm: f.term}), "registrations are not the committee's awards")
		_, err = f.query.Grants(f.ctx, &disbursementtypes.QueryGrantsRequest{RegistrarTerm: f.registrarTerm, CommitteeTerm: f.term})
		require.Error(t, err)
		_, err = f.query.Grants(f.ctx, &disbursementtypes.QueryGrantsRequest{RegistrarTerm: f.registrarTerm, Beneficiary: later.String()})
		require.Error(t, err)
		for _, g := range list(f, &disbursementtypes.QueryGrantsRequest{RegistrarTerm: f.registrarTerm}) {
			require.Equal(t, f.registrar.String(), g.CreatedBy)
		}
		// A committee replacement clears the registrar at an advanced term and publishes the clearing;
		// the new committee names one again before registration resumes.
		cleared := f.registrarTerm + 1
		f.appoint(t, f.committee)
		require.Equal(t, cleared, f.registrarTerm)
		set = f.events(t, &disbursementtypes.EventRegistrarMandateSet{})
		require.Len(t, set, 4)
		require.Empty(t, set[3].(*disbursementtypes.EventRegistrarMandateSet).Registrar)
		require.True(t, f.registrarStatus(t).Mandate.IsDisabled())
		require.ErrorContains(t, registerAs(f, f.registrar, after), "exact appointed")
		f.appointRegistrar(t, f.registrar)
		require.NoError(t, registerAs(f, f.registrar, after))
		// A lapsed registrar window stops it inside a live committee; a disabling by the committee
		// advances its term and refuses the key.
		now := f.now()
		require.NoError(t, appoint(f.committee, f.registrar, now, now+1))
		f.registrarTerm++
		f.advance(1)
		require.ErrorContains(t, registerAs(f, f.registrar, nativeAddress("late")), "not active")
		require.False(t, f.registrarStatus(t).Active)
		require.NoError(t, f.exec(&disbursementtypes.MsgCommitteeRegistrar{Committee: f.committee.String(), ExpectedTerm: f.term}))
		f.registrarTerm++
		require.ErrorContains(t, registerAs(f, f.registrar, nativeAddress("late")), "exact appointed")
		exported := f.check(t)
		require.True(t, exported.RegistrarMandate.IsDisabled())
		require.Equal(t, f.registrarTerm, exported.RegistrarMandate.Term)
		// A committee appointment over a disabled registrar leaves it where it is.
		f.appoint(t, f.committee)
		require.Equal(t, f.registrarTerm, f.registrarStatus(t).Mandate.Term)
	})

	t.Run("continuation", func(t *testing.T) {
		f := newNativeDisbursement(t)
		f.open(t, math.NewInt(30_000))
		f.register(t, a)
		f.register(t, b)
		exported := f.check(t)
		fresh := newNativeDisbursement(t)
		fresh.ctx = fresh.ctx.WithBlockTime(f.ctx.BlockTime()).WithBlockHeight(f.ctx.BlockHeight())
		f.mirror(t, fresh, chain.NoahBaseDenom)
		require.NoError(t, fresh.app.DisbursementKeeper.InitGenesis(fresh.ctx, exported))
		require.Equal(t, exported, fresh.check(t))
		require.Len(t, list(fresh, &disbursementtypes.QueryGrantsRequest{RegistrarTerm: f.registrarTerm}), 2, "the registrar term index is rebuilt")
		for _, tc := range []struct {
			name   string
			mutate func(*disbursementtypes.GenesisState)
		}{
			{"member beyond the live registrar term", func(g *disbursementtypes.GenesisState) { g.Grants[0].MandateTerm = g.RegistrarMandate.Term + 1 }},
			{"member without a term", func(g *disbursementtypes.GenesisState) { g.Grants[0].MandateTerm = 0 }},
			{"registrar under a disabled committee", func(g *disbursementtypes.GenesisState) {
				g.GrantsMandate = disbursementtypes.NewDisabledGrantsMandate(g.GrantsMandate.Term)
			}},
			{"registrar outside the committee window", func(g *disbursementtypes.GenesisState) {
				g.RegistrarMandate.ExpiryTime = g.GrantsMandate.ExpiryTime + 1
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

func TestNativeDisbursementPaymentDeferral(t *testing.T) {
	t.Run("refused_payee_waits_a_retry", func(t *testing.T) {
		f := newNativeDisbursement(t)
		a, b := nativeAddress("a"), nativeAddress("b")
		f.open(t, math.NewInt(20_000))
		first, second := f.register(t, a), f.register(t, b)
		f.advance(int64(hold) + 10)
		f.app.BankKeeper.AppendSendRestriction(func(_ context.Context, _ sdk.AccAddress, to sdk.AccAddress, _ sdk.Coins) (sdk.AccAddress, error) {
			if to.Equals(b) {
				return nil, errors.New("second destination refuses")
			}
			return to, nil
		})
		f.endBlock(t)
		// The refusal holds only its own grant: the other is paid, and the block goes on.
		require.Equal(t, math.NewInt(4_000), f.grant(t, first).Paid)
		require.True(t, f.grant(t, second).Paid.IsZero())
		require.Equal(t, f.now()+10, f.grant(t, first).DueTime)
		require.Equal(t, f.now()+10, f.grant(t, second).DueTime, "the sooner of the next boundary and a retry")
		deferred := f.deferred(t)
		require.Len(t, deferred, 1)
		require.Equal(t, second, deferred[0].GrantId)
		require.Equal(t, b.String(), deferred[0].Payee)
		require.Equal(t, math.NewInt(4_000), deferred[0].Amount)
		require.Equal(t, f.now()+10, deferred[0].DueTime)
		// A deferral moves nothing and writes no journal entry.
		journal, err := f.query.GrantJournal(f.ctx, &disbursementtypes.QueryGrantJournalRequest{GrantId: second})
		require.NoError(t, err)
		require.Len(t, journal.Entries, 1)
		f.check(t)
		// Past its schedule, a refused grant waits a retry interval between looks.
		f.advance(20)
		f.endBlock(t)
		require.Equal(t, math.NewInt(10_000), f.grant(t, first).Paid)
		require.Equal(t, f.now()+disbursementtypes.PaymentRetryInterval, f.grant(t, second).DueTime)
		require.Len(t, f.deferred(t), 2)
		// Changing the payee brings the look forward, and the new destination is paid in its block.
		payee := nativeAddress("payee")
		require.NoError(t, f.exec(&disbursementtypes.MsgSetPayee{Sender: b.String(), GrantId: second, Payee: payee.String()}))
		require.Equal(t, f.now(), f.grant(t, second).DueTime)
		f.endBlock(t)
		require.Equal(t, math.NewInt(10_000), f.app.BankKeeper.GetBalance(f.ctx, payee, chain.NoahBaseDenom).Amount)
		require.Zero(t, f.grant(t, second).DueTime)
		f.check(t)
	})
	t.Run("payee_change_keeps_a_first_look", func(t *testing.T) {
		f := newNativeDisbursement(t)
		worker, payee := nativeAddress("worker"), nativeAddress("payee")
		f.seed(t, disbursementtypes.ContributorsName, nativeNoah(1_000))
		award := f.create(t, disbursementtypes.GrantKind_GRANT_KIND_COMPENSATION, worker, chain.NoahCoin(math.NewInt(50)), 1)
		ownership := f.createAt(t, disbursementtypes.GrantKind_GRANT_KIND_OWNERSHIP, worker, chain.NoahCoin(nativeNoah(1)), f.now()+1_000, 0)
		before := map[uint64]uint64{award: f.grant(t, award).DueTime, ownership: f.grant(t, ownership).DueTime}
		// Nothing is due before one period after its award or before its clock starts, so a payee
		// change leaves those looks where they were, and an export taken before the next block imports.
		for id, due := range before {
			require.NoError(t, f.exec(&disbursementtypes.MsgSetPayee{Sender: worker.String(), GrantId: id, Payee: payee.String()}))
			require.Equal(t, due, f.grant(t, id).DueTime)
		}
		f.check(t)
	})
	t.Run("at_most_a_thousand_a_block", func(t *testing.T) {
		f := newNativeDisbursement(t)
		const members = disbursementtypes.MaxPaymentsPerBlock + disbursementtypes.MaxBatch
		f.open(t, math.NewInt(10_000*members))
		for batch := 0; batch < members/disbursementtypes.MaxBatch; batch++ {
			addresses := make([]string, 0, disbursementtypes.MaxBatch)
			for i := range disbursementtypes.MaxBatch {
				addresses = append(addresses, nativeAddress(fmt.Sprintf("m-%d-%d", batch, i)).String())
			}
			require.NoError(t, f.exec(&disbursementtypes.MsgRegisterMembers{Registrar: f.registrar.String(), ExpectedTerm: f.registrarTerm, Addresses: addresses, Reference: "admission batch"}))
		}
		paid := func() (n int) {
			require.NoError(t, f.app.DisbursementKeeper.Grants.Walk(f.ctx, nil, func(_ uint64, g disbursementtypes.Grant) (bool, error) {
				if g.Paid.Equal(math.NewInt(4_000)) {
					n++
				}
				return false, nil
			}))
			return n
		}
		f.advance(int64(hold) + 10)
		f.endBlock(t)
		require.Equal(t, disbursementtypes.MaxPaymentsPerBlock, paid(), "the block pays its bound and leaves the rest due")
		f.endBlock(t)
		require.Equal(t, members, paid(), "the next block pays the rest")
		f.check(t)
	})
}

// TestNativeDisbursementStrayDueKey plants a queue key at a time its grant does not carry, the shape a
// writer bug would leave: the block drops it without touching the grant, which keeps its own key.
func TestNativeDisbursementStrayDueKey(t *testing.T) {
	f := newNativeDisbursement(t)
	worker := nativeAddress("worker")
	f.seed(t, disbursementtypes.ContributorsName, nativeNoah(1_000))
	id := f.create(t, disbursementtypes.GrantKind_GRANT_KIND_COMPENSATION, worker, chain.NoahCoin(math.NewInt(50)), 1)
	before := f.grant(t, id)
	require.Greater(t, before.DueTime, f.now(), "nothing is due before one period after the award")
	stray := collections.Join(f.now(), id)
	require.NoError(t, f.app.DisbursementKeeper.Due.Set(f.ctx, stray))
	f.endBlock(t)
	has, err := f.app.DisbursementKeeper.Due.Has(f.ctx, stray)
	require.NoError(t, err)
	require.False(t, has, "the stray key is dropped")
	has, err = f.app.DisbursementKeeper.Due.Has(f.ctx, collections.Join(before.DueTime, id))
	require.NoError(t, err)
	require.True(t, has, "the grant keeps its own key")
	require.Equal(t, before, f.grant(t, id), "the grant is untouched")
	require.Empty(t, f.deferred(t))
	f.check(t)
}

func TestNativeDisbursementImportRefusals(t *testing.T) {
	for _, tc := range []struct{ corruption, want string }{
		{"reservation", "reservations"},
		{"paid", "principal"},
		{"payee", "journal leads to"},
		{"order", "sorted"},
		{"committee", "distinct from the disbursement authority"},
		{"registrar", "distinct from the disbursement authority"},
		{"height", "in the future"},
		{"denom", "registered asset"},
		{"due", "due time"},
		{"history", "does not follow"},
		{"time", "in the future"},
		{"unbacked", "not backed"},
		{"tranche", "not backed"},
	} {
		t.Run(tc.corruption, func(t *testing.T) {
			f := newNativeDisbursement(t)
			f.open(t, math.NewInt(10_000))
			f.register(t, nativeAddress("a"))
			gs := f.check(t)
			switch tc.corruption {
			case "reservation":
				require.NoError(t, gs.Books[0].Release(chain.NoahCoin(math.OneInt())))
			case "paid":
				gs.Grants[0].Paid = gs.Grants[0].Paid.AddRaw(1)
			case "payee":
				member := nativeAddress("a")
				require.NoError(t, f.exec(&disbursementtypes.MsgSetPayee{Sender: member.String(), GrantId: gs.Grants[0].Id, Payee: nativeAddress("elsewhere").String()}))
				gs = f.check(t)
				gs.Grants[0].Payee = member.String()
			case "order":
				// The tenth pays after the hold, giving the grant a second entry to misorder.
				f.advance(int64(hold))
				f.endBlock(t)
				gs = f.check(t)
				gs.GrantJournal[0], gs.GrantJournal[1] = gs.GrantJournal[1], gs.GrantJournal[0]
			case "committee":
				gs.GrantsMandate.Committee = f.authority.String()
			case "registrar":
				gs.RegistrarMandate.Committee = f.authority.String()
			case "height":
				gs.Grants[0].CreatedHeight++
				for i := range gs.GrantJournal {
					gs.GrantJournal[i].Height++
				}
			case "denom":
				// A paid-out award in a denomination no asset carries passes validation; import refuses it.
				// It follows the member's registration and is paid a period later, before the import.
				period := hold
				b, created := nativeAddress("b"), gs.Grants[0].CreatedTime+1
				award := disbursementtypes.Grant{
					Id: 2, Kind: disbursementtypes.GrantKind_GRANT_KIND_COMPENSATION, Beneficiary: b.String(), Payee: b.String(), CreatedBy: f.authority.String(),
					Amount: sdk.NewInt64Coin("azzz", 10), Schedule: disbursementtypes.Schedule{FirstLength: period, FirstWeight: 1, PeriodLength: period, PeriodWeight: 1}, StartTime: created, CreatedTime: created,
					CreatedHeight: gs.Grants[0].CreatedHeight, Paid: math.NewInt(10), Remaining: math.ZeroInt(), Reference: "work", AwardValue: math.NewInt(10), Charge: math.ZeroInt(),
				}
				gs.Grants, gs.NextGrantId = append(gs.Grants, award), 3
				gs.Beneficiaries = append(gs.Beneficiaries, disbursementtypes.Beneficiary{Address: b.String(), Controller: b.String(), OwnershipCommitted: math.ZeroInt(), Seat: math.ZeroInt()})
				last := gs.GrantJournal[len(gs.GrantJournal)-1]
				gs.GrantJournal = append(gs.GrantJournal,
					disbursementtypes.GrantEntry{Id: last.Id + 1, GrantId: 2, Action: disbursementtypes.GrantAction_GRANT_ACTION_CREATED, Actor: award.CreatedBy, Amount: award.Amount, Offer: math.ZeroInt(), Height: award.CreatedHeight, Time: created},
					disbursementtypes.GrantEntry{Id: last.Id + 2, GrantId: 2, Action: disbursementtypes.GrantAction_GRANT_ACTION_PAID, Actor: award.CreatedBy, Amount: award.Amount, Subject: b.String(), Offer: math.ZeroInt(), Height: award.CreatedHeight + 1, Time: created + period},
				)
				gs.NextGrantEntryId += 2
				require.NoError(t, gs.Validate())
			case "due":
				gs.Grants[0].DueTime = 0
			case "history":
				gs.GrantJournal = gs.GrantJournal[:len(gs.GrantJournal)-1]
			case "time":
				// A registration stamped after the genesis block, its clock with it.
				gs.Grants[0].CreatedTime++
				gs.Grants[0].StartTime++
				gs.Grants[0].DueTime++
				gs.GrantJournal[0].Time++
			case "tranche":
				gs.MemberTranche = math.NewInt(10_001) // past the whole member funding below
			}
			fresh := newNativeDisbursement(t)
			if tc.corruption == "denom" {
				fresh.advance(int64(hold) + 1)
			}
			if tc.corruption != "unbacked" {
				apptestutil.FundModule(t, fresh.app, fresh.ctx, disbursementtypes.MembersName, sdk.NewCoins(chain.NoahCoin(math.NewInt(10_000))))
			}
			require.ErrorContains(t, fresh.app.DisbursementKeeper.InitGenesis(fresh.ctx, gs), tc.want)
		})
	}
}

// TestNativeDisbursementImportBlockedPayee pins that a payee Bank blocks imports, since the blocked
// list can change between an export and its relaunch, and that the block defers its payment.
func TestNativeDisbursementImportBlockedPayee(t *testing.T) {
	f := newNativeDisbursement(t)
	f.open(t, math.NewInt(10_000))
	member := nativeAddress("a")
	id := f.register(t, member)
	gs := f.check(t)
	blocked := authtypes.NewModuleAddress(authtypes.FeeCollectorName).String()
	last := gs.GrantJournal[len(gs.GrantJournal)-1]
	gs.GrantJournal = append(gs.GrantJournal, disbursementtypes.GrantEntry{
		Id: last.Id + 1, GrantId: id, Action: disbursementtypes.GrantAction_GRANT_ACTION_PAYEE_CHANGED, Actor: member.String(),
		Amount: chain.NoahCoin(math.ZeroInt()), Subject: blocked, Offer: math.ZeroInt(), Height: last.Height, Time: last.Time,
	})
	gs.NextGrantEntryId++
	gs.Grants[0].Payee = blocked
	fresh := newNativeDisbursement(t)
	apptestutil.FundModule(t, fresh.app, fresh.ctx, disbursementtypes.MembersName, sdk.NewCoins(chain.NoahCoin(math.NewInt(10_000))))
	require.NoError(t, fresh.app.DisbursementKeeper.InitGenesis(fresh.ctx, gs))
	fresh.advance(int64(hold) + 10)
	fresh.endBlock(t)
	deferred := fresh.deferred(t)
	require.Len(t, deferred, 1)
	require.Equal(t, id, deferred[0].GrantId)
	require.Equal(t, blocked, deferred[0].Payee)
	require.True(t, fresh.grant(t, id).Paid.IsZero(), "the refused payment moved nothing")
	fresh.check(t)
}

func requirePool(t *testing.T, f *nativeDisbursementFixture, account string, uncommitted, open int64) {
	t.Helper()
	a := f.account(t, account, chain.NoahBaseDenom)
	require.True(t, a.Uncommitted.Amount.Equal(math.NewInt(uncommitted)), "uncommitted %s", a.Uncommitted)
	if account != disbursementtypes.MembersName {
		require.Zero(t, open, "only the member pool has a tranche")
		return
	}
	tranche, err := f.app.DisbursementKeeper.MemberTranche.Get(f.ctx)
	require.NoError(t, err)
	require.True(t, tranche.Equal(math.NewInt(open)), "open %s", tranche)
}

func TestNativeDisbursementPools(t *testing.T) {
	members, contributors := disbursementtypes.MembersName, disbursementtypes.ContributorsName
	t.Run("tranches", func(t *testing.T) {
		f := newNativeDisbursement(t)
		a, b := nativeAddress("a"), nativeAddress("b")
		openTranche := func(authority sdk.AccAddress, amount int64) error {
			return f.exec(&disbursementtypes.MsgOpenTranche{Authority: authority.String(), Amount: math.NewInt(amount)})
		}
		f.seed(t, members, math.NewInt(20_000))
		require.ErrorContains(t, f.exec(&disbursementtypes.MsgRegisterMembers{Registrar: f.registrar.String(), ExpectedTerm: f.registrarTerm, Addresses: []string{a.String()}, Reference: "admission batch"}), "open member tranche")
		require.ErrorContains(t, openTranche(f.authority, 20_001), "exceeds the member pool")
		require.Error(t, openTranche(a, 1))
		require.NoError(t, openTranche(f.authority, 10_000))
		opened := f.events(t, &disbursementtypes.EventTrancheOpened{})
		require.Len(t, opened, 1, "the opening is published once")
		tranche := opened[0].(*disbursementtypes.EventTrancheOpened)
		require.True(t, tranche.Amount.Equal(math.NewInt(10_000)) && tranche.MemberTranche.Equal(math.NewInt(10_000)), "the event carries the amount and the member tranche")
		id := f.register(t, a)
		var actions []string
		for _, e := range f.events(t, &disbursementtypes.EventGrantEntryRecorded{}) {
			if entry := e.(*disbursementtypes.EventGrantEntryRecorded); entry.GrantId == id {
				actions = append(actions, entry.Action)
			}
		}
		require.Equal(t, []string{"GRANT_ACTION_CREATED"}, actions, "registration publishes the creation by name and pays nothing inside the hold")
		require.ErrorContains(t, f.exec(&disbursementtypes.MsgRegisterMembers{Registrar: f.registrar.String(), ExpectedTerm: f.registrarTerm, Addresses: []string{b.String()}, Reference: "admission batch"}), "open member tranche")
		f.advance(int64(hold) + 10)
		require.NoError(t, f.exec(&disbursementtypes.MsgCancelGrants{Authority: f.authority.String(), GrantIds: []uint64{id}}))
		// The unearned 6,000 returns to the step it was charged to, ready for the next member.
		requirePool(t, f, members, 16_000, 6_000)
		require.NoError(t, openTranche(f.authority, 4_000))
		f.register(t, b)
		requirePool(t, f, members, 6_000, 0)
		f.check(t)
	})
	t.Run("returns", func(t *testing.T) {
		f := newNativeDisbursement(t)
		f.seed(t, contributors, math.NewInt(50_000))
		f.seed(t, members, math.NewInt(10_000))
		ret := func(coin sdk.Coin) error {
			return f.exec(&disbursementtypes.MsgReturnUnallocated{Authority: f.authority.String(), Amount: coin})
		}
		require.ErrorContains(t, ret(sdk.NewInt64Coin("ausd", 1)), "uncommitted contributor custody")
		require.ErrorContains(t, ret(chain.NoahCoin(math.NewInt(50_001))), "uncommitted contributor custody")
		before, err := f.app.DistrKeeper.FeePool.Get(f.ctx)
		require.NoError(t, err)
		require.NoError(t, ret(chain.NoahCoin(math.NewInt(40_000))))
		returned := f.events(t, &disbursementtypes.EventUnallocatedReturned{})
		require.Len(t, returned, 1, "the return is published once")
		require.True(t, returned[0].(*disbursementtypes.EventUnallocatedReturned).Amount.Equal(math.NewInt(40_000)))
		// The pool shrinks by the return; the member account is untouched.
		requirePool(t, f, contributors, 10_000, 0)
		requirePool(t, f, members, 10_000, 0)
		after, err := f.app.DistrKeeper.FeePool.Get(f.ctx)
		require.NoError(t, err)
		require.Equal(t, math.LegacyNewDec(40_000), after.CommunityPool.AmountOf(chain.NoahBaseDenom).Sub(before.CommunityPool.AmountOf(chain.NoahBaseDenom)))
		// A NOAH deposit joins the contributor pool; a stablecoin deposit waits for an award. Both return.
		f.fund(t, chain.NoahCoin(math.NewInt(5_000)))
		f.fund(t, sdk.NewInt64Coin("ausd", 700))
		requirePool(t, f, contributors, 15_000, 0)
		require.NoError(t, ret(chain.NoahCoin(math.NewInt(5_000))))
		require.NoError(t, ret(sdk.NewInt64Coin("ausd", 700)))
		require.ErrorContains(t, ret(sdk.NewInt64Coin("ausd", 1)), "uncommitted contributor custody")
		// The member account takes NOAH from anyone, unopened, and has no exit: a return never reaches it.
		donor := nativeAddress("donor")
		apptestutil.FundAccount(t, f.app, f.ctx, donor, sdk.NewCoins(chain.NoahCoin(math.NewInt(2_000)), sdk.NewInt64Coin("ausd", 1)))
		send := func(coin sdk.Coin) error {
			return f.exec(&banktypes.MsgSend{FromAddress: donor.String(), ToAddress: authtypes.NewModuleAddress(members).String(), Amount: sdk.NewCoins(coin)})
		}
		require.ErrorContains(t, send(sdk.NewInt64Coin("ausd", 1)), "member pool")
		require.NoError(t, send(chain.NoahCoin(math.NewInt(2_000))))
		require.NoError(t, ret(chain.NoahCoin(math.NewInt(10_000))))
		requirePool(t, f, contributors, 0, 0)
		requirePool(t, f, members, 12_000, 0)
		require.ErrorContains(t, ret(chain.NoahCoin(math.NewInt(1))), "uncommitted contributor custody")
		f.check(t)
	})
	t.Run("funding", func(t *testing.T) {
		f := newNativeDisbursement(t)
		worker := nativeAddress("worker")
		f.seed(t, contributors, math.NewInt(20_000))
		f.fund(t, sdk.NewInt64Coin("ausd", 5_000))
		award := func(coin sdk.Coin, maxOffer int64) (uint64, error) {
			id, err := f.app.DisbursementKeeper.NextGrantID.Peek(f.ctx)
			require.NoError(t, err)
			return id, f.exec(&disbursementtypes.MsgCompensate{Authority: f.authority.String(), Beneficiary: worker.String(), Amount: coin, StartTime: f.now(), Periods: 1, Reference: "work", MaxOffer: math.NewInt(maxOffer)})
		}
		_, err := award(chain.NoahCoin(math.NewInt(1_000)), 1)
		require.ErrorContains(t, err, "converts nothing")
		_, err = award(sdk.NewInt64Coin("ausd", 6_000), 0)
		require.ErrorContains(t, err, "set max_offer")
		// Custody covers the award, so the cap is unused and nothing converts.
		id, err := award(sdk.NewInt64Coin("ausd", 4_000), 1)
		require.NoError(t, err)
		require.Equal(t, math.NewInt(1_000), f.account(t, contributors, "ausd").Uncommitted.Amount)
		journal, err := f.query.GrantJournal(f.ctx, &disbursementtypes.QueryGrantJournalRequest{GrantId: id})
		require.NoError(t, err)
		for _, e := range journal.Entries {
			require.NotEqual(t, disbursementtypes.GrantAction_GRANT_ACTION_CONVERTED, e.Action)
		}
		// Custody covers part: the 1,000 left is used and only the shortfall is bought, at least
		// 2,000 and never the whole 3,000; what Market delivers past the shortfall stays in custody.
		id, err = award(sdk.NewInt64Coin("ausd", 3_000), 10_000)
		require.NoError(t, err)
		journal, err = f.query.GrantJournal(f.ctx, &disbursementtypes.QueryGrantJournalRequest{GrantId: id})
		require.NoError(t, err)
		require.Len(t, journal.Entries, 2)
		bought := journal.Entries[1]
		require.Equal(t, disbursementtypes.GrantAction_GRANT_ACTION_CONVERTED, bought.Action)
		require.True(t, bought.Amount.Amount.GTE(math.NewInt(2_000)) && bought.Amount.Amount.LT(math.NewInt(3_000)), "bought %s", bought.Amount)
		require.True(t, bought.Offer.IsPositive())
		require.Equal(t, bought.Amount.Amount.SubRaw(2_000), f.account(t, contributors, "ausd").Uncommitted.Amount)
		// Every check runs before the buy: an award refused for its reference never reaches Market, whose
		// maximum offer this one would also break.
		require.ErrorContains(t, f.exec(&disbursementtypes.MsgCompensate{
			Authority: f.authority.String(), Beneficiary: worker.String(), Amount: sdk.NewInt64Coin("ausd", 2_000),
			StartTime: f.now(), Periods: 1, MaxOffer: math.OneInt(),
		}), "grant reference must not be empty")
		// Without an ausd rate the award can be neither valued nor converted; it fails whole.
		require.NoError(t, f.app.OracleKeeper.ExchangeRate.Remove(f.ctx, "ausd"))
		_, err = award(sdk.NewInt64Coin("ausd", 2_000), 10_000)
		require.Error(t, err)
		f.check(t)
	})
}

func TestNativeDisbursementCommitteeAwards(t *testing.T) {
	founder, worker := nativeAddress("founder"), nativeAddress("worker")
	// Compensation is in the reference unit, which NOAH and ausd each price at one.
	compensationAllowance := math.NewInt(50_000)
	const compPeriod = hold
	setup := func(t *testing.T) *nativeDisbursementFixture {
		t.Helper()
		f := newNativeDisbursement(t, founder)
		f.seed(t, disbursementtypes.ContributorsName, math.NewInt(100_000))
		f.fund(t, sdk.NewInt64Coin("ausd", 50_000))
		f.appointWith(t, f.committee, compensationAllowance)
		f.appointRegistrar(t, f.registrar)
		return f
	}
	// award is a committee award, compensation by construction: ownership is governance's alone (D113).
	// Two fixture periods paying half each, the clock starting now.
	award := func(f *nativeDisbursementFixture, beneficiary sdk.AccAddress, coin sdk.Coin) error {
		return f.exec(&disbursementtypes.MsgCommitteeCompensate{
			Committee: f.committee.String(), ExpectedTerm: f.term, Beneficiary: beneficiary.String(), Amount: coin,
			StartTime: f.now(), Periods: 2, Reference: "committee pay",
		})
	}
	// capOver sets a 10,000 cap over a window of four compensation periods.
	capOver := func(t *testing.T, f *nativeDisbursementFixture) (period, window uint64) {
		t.Helper()
		p, err := f.app.DisbursementKeeper.Params.Get(f.ctx)
		require.NoError(t, err)
		period, window = p.CompensationPeriod, 4*p.CompensationPeriod
		p.CompensationCap, p.CompensationWindow = math.NewInt(10_000), window
		require.NoError(t, f.exec(&disbursementtypes.MsgUpdateParams{Authority: f.authority.String(), Params: p}))
		return period, window
	}
	// compensate is a governance award to the worker on the grantor's clock.
	compensate := func(f *nativeDisbursementFixture, amount int64, start uint64, periods uint32) error {
		return f.exec(&disbursementtypes.MsgCompensate{
			Authority: f.authority.String(), Beneficiary: worker.String(),
			Amount: chain.NoahCoin(math.NewInt(amount)), StartTime: start, Periods: periods, Reference: "pay",
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
		appoint := func(compensation math.Int) error {
			now := f.now()
			return f.exec(&disbursementtypes.MsgSetGrantsMandate{
				Authority: f.authority.String(), Committee: f.committee.String(), ActivationTime: now, ExpiryTime: now + 100,
				CompensationAllowance: compensation,
			})
		}
		require.ErrorContains(t, appoint(math.NewInt(-1)), "allowance")
		// A disabling keeps the canonical empty payload whatever the message carries.
		require.NoError(t, f.exec(&disbursementtypes.MsgSetGrantsMandate{Authority: f.authority.String(), CompensationAllowance: compensationAllowance}))
		f.term++
		m := usage(t, f).Mandate
		require.True(t, m.IsDisabled())
		require.True(t, m.CompensationAllowance.IsZero())
	})

	t.Run("awards_within_the_term", func(t *testing.T) {
		f := setup(t)
		require.NoError(t, award(f, worker, chain.NoahCoin(math.NewInt(20_000))))
		requirePool(t, f, disbursementtypes.ContributorsName, 80_000, 0)
		require.NoError(t, award(f, worker, sdk.NewInt64Coin("ausd", 20_000)))
		require.Equal(t, math.NewInt(10_000), usage(t, f).CompensationRemaining)
		require.ErrorContains(t, award(f, worker, sdk.NewInt64Coin("ausd", 10_001)), "term's compensation allowance")
		// Cancelling an award frees its principal but never its allowance.
		first := uint64(1)
		for ; ; first++ {
			if f.grant(t, first).Kind == disbursementtypes.GrantKind_GRANT_KIND_COMPENSATION {
				break
			}
		}
		require.NoError(t, f.exec(&disbursementtypes.MsgCancelGrants{Authority: f.authority.String(), GrantIds: []uint64{first}}))
		require.ErrorContains(t, award(f, worker, chain.NoahCoin(math.NewInt(10_001))), "term's compensation allowance")
		require.NoError(t, award(f, worker, chain.NoahCoin(math.NewInt(10_000))))
		res := usage(t, f)
		require.Equal(t, compensationAllowance, res.CompensationUsed)
		require.True(t, res.CompensationRemaining.IsZero())
		listed, err := f.query.Grants(f.ctx, &disbursementtypes.QueryGrantsRequest{CommitteeTerm: f.term})
		require.NoError(t, err)
		require.Len(t, listed.Grants, 3)
		_, err = f.query.Grants(f.ctx, &disbursementtypes.QueryGrantsRequest{CommitteeTerm: f.term, Beneficiary: worker.String()})
		require.Error(t, err)
		// A new term starts with nothing used.
		f.appointWith(t, f.committee, compensationAllowance)
		res = usage(t, f)
		require.True(t, res.CompensationUsed.IsZero())
		require.Equal(t, compensationAllowance, res.CompensationRemaining)
		require.NoError(t, award(f, worker, chain.NoahCoin(math.NewInt(30_000))))
		f.check(t)
	})

	t.Run("seat_holders_and_ownership", func(t *testing.T) {
		f := newNativeDisbursement(t, founder)
		// Scaled to the test app's small balances, far under the ownership cap.
		scale := func(n int64) math.Int { return math.NewInt(n).MulRaw(1_000_000_000_000) }
		f.seed(t, disbursementtypes.ContributorsName, scale(10_000_000))
		f.appointWith(t, f.committee, scale(1_000))
		// Ownership is governance's alone, carrying no term; the committee's award to a seat holder is
		// compensation like anyone's, and a NOAH award draws on the compensation figure.
		workerGrant := f.create(t, disbursementtypes.GrantKind_GRANT_KIND_OWNERSHIP, worker, chain.NoahCoin(scale(2_000_000)), 0)
		require.NoError(t, award(f, founder, chain.NoahCoin(scale(1_000))))
		founderGrant := f.create(t, disbursementtypes.GrantKind_GRANT_KIND_OWNERSHIP, founder, chain.NoahCoin(scale(1_000_000)), 0)
		require.ErrorContains(t, award(f, worker, chain.NoahCoin(math.NewInt(1))), "term's compensation allowance")
		res := usage(t, f)
		require.Equal(t, scale(1_000), res.CompensationUsed)
		requirePool(t, f, disbursementtypes.ContributorsName, 6_999_000*1_000_000_000_000, 0)
		require.Equal(t, disbursementtypes.GrantKind_GRANT_KIND_OWNERSHIP, f.grant(t, workerGrant).Kind)
		require.Zero(t, f.grant(t, workerGrant).MandateTerm, "a governance grant carries no term")
		require.Equal(t, disbursementtypes.GrantKind_GRANT_KIND_COMPENSATION, f.grant(t, 2).Kind)
		require.Equal(t, f.term, f.grant(t, 2).MandateTerm)
		// Past both ownership periods, inside compensation's second.
		f.advance(int64(compPeriod) + 10)
		record, err := f.query.Beneficiary(f.ctx, &disbursementtypes.QueryBeneficiaryRequest{Address: worker.String()})
		require.NoError(t, err)
		// The award commits its whole amount at once; both ownership periods have elapsed, so the block
		// pays the grants whole.
		require.Equal(t, nativeNoah(disbursementtypes.DefaultOwnershipCap).Sub(scale(2_000_000)), record.OwnershipRemaining)
		f.endBlock(t)
		require.Equal(t, scale(2_000_000), f.grant(t, workerGrant).Paid)
		person, err := f.query.Beneficiary(f.ctx, &disbursementtypes.QueryBeneficiaryRequest{Address: worker.String()})
		require.NoError(t, err)
		require.Equal(t, scale(2_000_000), person.Beneficiary.OwnershipCommitted, "an ownership grant counts on the record")
		// Compensation pays outside the cap, half at its first boundary, and a seat holder's ownership pays like
		// anyone's: the seat is a record nothing reads.
		require.Equal(t, scale(500), f.grant(t, 2).Paid)
		require.Equal(t, scale(1_000_000), f.grant(t, founderGrant).Paid)
		seat, err := f.query.Beneficiary(f.ctx, &disbursementtypes.QueryBeneficiaryRequest{Address: founder.String()})
		require.NoError(t, err)
		require.Equal(t, chain.SeatGrant, seat.Beneficiary.Seat)
		require.Equal(t, scale(1_000_000), seat.Beneficiary.OwnershipCommitted)
		f.check(t)
	})

	t.Run("compensation_cap", func(t *testing.T) {
		f := setup(t)
		// A 25,000 cap over a window of two periods, the fixture award's own length, so each award
		// charges its whole value; NOAH is one reference unit, ausd here half of one.
		p, err := f.app.DisbursementKeeper.Params.Get(f.ctx)
		require.NoError(t, err)
		window := 2 * p.CompensationPeriod
		p.CompensationCap, p.CompensationWindow = math.NewInt(25_000), window
		require.NoError(t, f.exec(&disbursementtypes.MsgUpdateParams{Authority: f.authority.String(), Params: p}))
		f.setRate(t, "ausd", math.LegacyNewDecWithPrec(5, 1))
		require.NoError(t, award(f, worker, chain.NoahCoin(math.NewInt(10_000))))
		require.Equal(t, math.NewInt(10_000), f.grant(t, 1).AwardValue)
		f.advance(int64(window / 2))
		require.NoError(t, award(f, worker, sdk.NewInt64Coin("ausd", 20_000)))
		require.Equal(t, math.NewInt(10_000), f.grant(t, 2).AwardValue, "valued at the rate when awarded")
		require.ErrorContains(t, award(f, worker, chain.NoahCoin(math.NewInt(5_001))), "compensation cap")
		// A cancel never restores room.
		require.NoError(t, f.exec(&disbursementtypes.MsgCancelGrants{Authority: f.authority.String(), GrantIds: []uint64{1}}))
		require.ErrorContains(t, award(f, worker, chain.NoahCoin(math.NewInt(5_001))), "compensation cap")
		require.NoError(t, award(f, worker, chain.NoahCoin(math.NewInt(5_000))))
		// Governance is bound the same way; ownership is outside it.
		require.ErrorContains(t, f.exec(&disbursementtypes.MsgCompensate{
			Authority: f.authority.String(), Beneficiary: worker.String(),
			Amount: chain.NoahCoin(math.NewInt(1)), StartTime: f.now(), Periods: 1, Reference: "pay",
		}), "compensation cap")
		ownership := f.create(t, disbursementtypes.GrantKind_GRANT_KIND_OWNERSHIP, worker, chain.NoahCoin(math.NewInt(1_000)), 0)
		require.True(t, f.grant(t, ownership).AwardValue.IsZero())
		// The window rolls: the first award stops counting a window after it was made.
		f.advance(int64(window / 2))
		require.NoError(t, award(f, worker, chain.NoahCoin(math.NewInt(10_000))))
		require.ErrorContains(t, award(f, worker, chain.NoahCoin(math.NewInt(2))), "compensation cap")
		// A stream five windows long charges a fifth of its value a window: 10,000 of 50,000, which
		// the 25,000 counted now refuses and a half-window later admits; it then counts at that rate
		// for as long as it runs (D119).
		stream := func() error {
			return f.exec(&disbursementtypes.MsgCompensate{
				Authority: f.authority.String(), Beneficiary: worker.String(),
				Amount: chain.NoahCoin(math.NewInt(50_000)), StartTime: f.now(), Periods: 10, Reference: "a long stream",
			})
		}
		require.ErrorContains(t, stream(), "compensation cap")
		f.advance(int64(window / 2))
		require.NoError(t, stream())
		require.ErrorContains(t, award(f, worker, chain.NoahCoin(math.NewInt(10_000))), "compensation cap")
		f.advance(int64(window))
		govAward := func(amount int64) error {
			return f.exec(&disbursementtypes.MsgCompensate{
				Authority: f.authority.String(), Beneficiary: worker.String(),
				Amount: chain.NoahCoin(math.NewInt(amount)), StartTime: f.now(), Periods: 2, Reference: "pay",
			})
		}
		require.ErrorContains(t, govAward(15_001), "compensation cap")
		require.NoError(t, govAward(15_000))
		// An award the oracle cannot value in the reference unit is refused.
		require.NoError(t, f.app.OracleKeeper.ExchangeRate.Remove(f.ctx, chain.XDRBaseDenom))
		require.ErrorContains(t, award(f, worker, chain.NoahCoin(math.NewInt(2))), "valuing the award")
		f.check(t)
	})

	t.Run("compensation_cap_backdate", func(t *testing.T) {
		f := setup(t)
		period, window := capOver(t, f)
		// Eight periods, two windows long, starting a window back: the first look pays the backdated
		// window at once and the rest follows inside the same window, so the award counts whole in it
		// rather than at half its value a window (D122).
		back := func(amount int64) error { return compensate(f, amount, f.now()-window, 8) }
		require.ErrorContains(t, back(10_008), "compensation cap")
		require.NoError(t, back(10_000))
		require.ErrorContains(t, compensate(f, 1, f.now(), 1), "compensation cap", "the backdated award fills its window")
		before := f.app.BankKeeper.GetBalance(f.ctx, worker, chain.NoahBaseDenom).Amount
		for range 4 {
			f.advance(int64(period))
			f.endBlock(t)
		}
		paid := f.app.BankKeeper.GetBalance(f.ctx, worker, chain.NoahBaseDenom).Amount.Sub(before)
		require.Equal(t, math.NewInt(10_000), paid, "the whole award is paid inside its first window")
		// The next window admits the same again and no more: the cap holds window after window.
		require.ErrorContains(t, back(10_008), "compensation cap")
		require.NoError(t, back(10_000))
		f.check(t)
	})

	t.Run("compensation_cap_cancelled_stream", func(t *testing.T) {
		f := setup(t)
		_, window := capOver(t, f)
		// Three windows of 30,000 charge 10,000 a window, the whole cap.
		require.NoError(t, compensate(f, 30_000, f.now(), 12))
		require.NoError(t, f.exec(&disbursementtypes.MsgCancelGrants{Authority: f.authority.String(), GrantIds: []uint64{1}}))
		require.ErrorContains(t, compensate(f, 1, f.now(), 1), "compensation cap", "a cancel restores no room in the first window")
		// Past its first window a cancelled stream stops counting, though its schedule would still run.
		f.advance(int64(window))
		require.NoError(t, compensate(f, 10_000, f.now(), 1))
		f.check(t)
	})

	t.Run("compensation_cap_forward_clock", func(t *testing.T) {
		f := setup(t)
		period, window := capOver(t, f)
		// Two windows of 20,000 starting a window ahead charge 10,000 a window from the award until the
		// clock runs out, eight periods after it starts: twelve periods after the award, not eight.
		require.NoError(t, compensate(f, 20_000, f.now()+window, 8))
		require.ErrorContains(t, compensate(f, 1, f.now(), 1), "compensation cap")
		f.advance(int64(9 * period))
		require.ErrorContains(t, compensate(f, 1, f.now(), 1), "compensation cap", "still paying, so still counted")
		f.advance(int64(3 * period))
		require.NoError(t, compensate(f, 10_000, f.now(), 1))
		f.check(t)
	})

	t.Run("recovery_by_id", func(t *testing.T) {
		f := setup(t)
		require.NoError(t, award(f, worker, chain.NoahCoin(math.NewInt(10_000))))
		require.NoError(t, award(f, worker, sdk.NewInt64Coin("ausd", 5_000)))
		stolen, stolenRegistrar := f.term, f.registrarTerm
		// Governance replaces both keys; an award or registration landed before the vote executes is
		// still under its term.
		require.NoError(t, award(f, worker, chain.NoahCoin(math.NewInt(5_000))))
		f.open(t, math.NewInt(10_000))
		fake := f.register(t, nativeAddress("fake"))
		// The new committee's appointment clears the old registrar with the old committee.
		f.appointWith(t, nativeAddress("new-committee"), compensationAllowance)
		list := func(req *disbursementtypes.QueryGrantsRequest) []disbursementtypes.Grant {
			listed, err := f.query.Grants(f.ctx, req)
			require.NoError(t, err)
			return listed.Grants
		}
		term := func() []disbursementtypes.Grant {
			return append(list(&disbursementtypes.QueryGrantsRequest{CommitteeTerm: stolen}), list(&disbursementtypes.QueryGrantsRequest{RegistrarTerm: stolenRegistrar})...)
		}
		// The term queries list the old keys' awards and registrations, and governance cancels them by ID.
		var ids []uint64
		for _, g := range term() {
			ids = append(ids, g.Id)
		}
		require.Len(t, ids, 4)
		require.Contains(t, ids, fake)
		cancel := func() error {
			return f.exec(&disbursementtypes.MsgCancelGrants{Authority: f.authority.String(), GrantIds: ids})
		}
		require.NoError(t, cancel())
		for _, g := range term() {
			require.True(t, g.Cancelled())
			require.True(t, g.Remaining.IsZero(), "nothing accrued: no award's first period or the fake's hold had ended")
		}
		// NOAH returns to the pool and the fake's whole grant to the tranche, since a cancel inside its
		// hold finds nothing paid; stablecoins stay in custody.
		requirePool(t, f, disbursementtypes.ContributorsName, 100_000, 0)
		requirePool(t, f, disbursementtypes.MembersName, 10_000, 10_000)
		require.Equal(t, math.NewInt(50_000), f.account(t, disbursementtypes.ContributorsName, "ausd").Uncommitted.Amount)
		require.NoError(t, cancel(), "cancelling again changes nothing")
		f.check(t)
	})

	t.Run("continuation", func(t *testing.T) {
		f := setup(t)
		require.NoError(t, award(f, worker, chain.NoahCoin(math.NewInt(10_000))))
		require.NoError(t, award(f, worker, sdk.NewInt64Coin("ausd", 5_000)))
		exported := f.check(t)
		fresh := newNativeDisbursement(t, founder)
		fresh.ctx = fresh.ctx.WithBlockTime(f.ctx.BlockTime()).WithBlockHeight(f.ctx.BlockHeight())
		f.mirror(t, fresh, chain.NoahBaseDenom, "ausd")
		require.NoError(t, fresh.app.DisbursementKeeper.InitGenesis(fresh.ctx, exported))
		require.Equal(t, exported, fresh.check(t))
		require.Equal(t, usage(t, f).CompensationUsed, usage(t, fresh).CompensationUsed, "usage round-trips")

		for _, tc := range []struct {
			name   string
			mutate func(*disbursementtypes.GenesisState)
		}{
			{"allowance below usage", func(g *disbursementtypes.GenesisState) {
				g.GrantsMandate.CompensationAllowance = math.NewInt(14_999) // 10,000 NOAH and 5,000 ausd, one reference unit each
			}},
			{"usage differs from the grants", func(g *disbursementtypes.GenesisState) { g.CompensationUsed = g.CompensationUsed.AddRaw(1) }},
			{"ownership under a committee term", func(g *disbursementtypes.GenesisState) {
				g.Grants[0].Kind, g.Grants[0].MandateTerm = disbursementtypes.GrantKind_GRANT_KIND_OWNERSHIP, g.GrantsMandate.Term
			}},
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

// TestNativeDisbursementReferenceRepoint re-points the protocol reference on the real app and checks
// every wired consumer moved in the one transaction: Market's pool, Treasury's params, and
// Disbursement's compensation figures, each rounded up at a three-quarter cross, with the export
// reconciling in the new unit. The three events are the guard against a consumer left off the
// app's wiring line, which would rebase nothing and fail nothing.
func TestNativeDisbursementReferenceRepoint(t *testing.T) {
	f := newNativeDisbursement(t)
	f.fund(t, chain.NoahCoin(nativeNoah(1_000)))
	f.appointWith(t, f.committee, math.NewInt(1_000))
	beneficiary, colleague := nativeAddress("beneficiary"), nativeAddress("colleague")
	governance := f.create(t, disbursementtypes.GrantKind_GRANT_KIND_COMPENSATION, beneficiary, chain.NoahCoin(math.NewInt(301)), 1)
	require.NoError(t, f.exec(&disbursementtypes.MsgCommitteeCompensate{
		Committee: f.committee.String(), ExpectedTerm: f.term, Beneficiary: colleague.String(), Amount: chain.NoahCoin(math.NewInt(500)),
		StartTime: f.now(), Periods: 1, Reference: "committee pay",
	}))
	committee := governance + 1

	f.setRate(t, chain.XDRBaseDenom, math.LegacyNewDecWithPrec(15, 1))
	f.setRate(t, chain.USDBaseDenom, math.LegacyNewDec(2))
	require.NoError(t, f.exec(&oracletypes.MsgSetReferenceDenom{Authority: f.authority.String(), ReferenceDenom: chain.USDBaseDenom}))

	reference, err := f.app.OracleKeeper.GetReferenceDenom(f.ctx)
	require.NoError(t, err)
	require.Equal(t, chain.USDBaseDenom, reference)
	pool, err := f.app.MarketKeeper.ConversionPolicy.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, chain.USDBaseDenom, pool.BasePool.Denom)
	treasury, err := f.app.TreasuryKeeper.Params.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, chain.USDBaseDenom, treasury.ReferenceDenom)

	p, err := f.app.DisbursementKeeper.Params.Get(f.ctx)
	require.NoError(t, err)
	require.True(t, p.CompensationCap.Equal(disbursementtypes.DefaultParams().CompensationCap.MulRaw(3).QuoRaw(4)))
	require.True(t, f.grant(t, governance).AwardValue.Equal(math.NewInt(226)))
	require.True(t, f.grant(t, committee).AwardValue.Equal(math.NewInt(375)))
	require.True(t, f.grant(t, committee).Charge.Equal(math.NewInt(375)))
	require.True(t, f.grant(t, governance).Charge.IsZero())
	used, err := f.app.DisbursementKeeper.CompensationUsed.Get(f.ctx)
	require.NoError(t, err)
	require.True(t, used.Equal(math.NewInt(375)))
	mandate, err := f.app.DisbursementKeeper.GrantsMandate.Get(f.ctx)
	require.NoError(t, err)
	require.True(t, mandate.CompensationAllowance.Equal(math.NewInt(750)))

	moved := map[string]bool{}
	for _, e := range f.emitted {
		moved[e.Type] = true
	}
	for _, consumer := range []string{
		"ark.market.v1.EventPoolUpdated",
		"ark.treasury.v1.EventReferenceTaxCapRebased",
		"ark.disbursement.v1.EventCompensationRebased",
	} {
		require.True(t, moved[consumer], consumer)
	}
	f.check(t)
}
