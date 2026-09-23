package app_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/app/testdata"
	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
)

// grantFixture is an app with the grant contract instantiated the way the
// distribution plan's first proposal would: no admin, a registrar set, the
// member grant and cap in place, and the community pool funded so a
// tranche can be spent to it.
type grantFixture struct {
	app       *app.ArkApp
	ctx       sdk.Context
	pk        *wasmkeeper.PermissionedKeeper
	contract  sdk.AccAddress
	registrar sdk.AccAddress
	creator   sdk.AccAddress
}

var (
	oneNoah         = math.NewInt(1_000_000_000_000_000_000)
	memberGrant     = oneNoah.MulRaw(10_000)
	memberAllowance = oneNoah.MulRaw(2)
	// memberSchedule is the plan's: a tenth after a second, then twelve
	// months of three fortieths.
	memberSchedule = func() []map[string]uint64 {
		s := []map[string]uint64{{"length": 1, "parts": 4}}
		for range 12 {
			s = append(s, map[string]uint64{"length": 2_628_000, "parts": 3})
		}
		return s
	}()
	standardSchedule = func() []map[string]uint64 {
		s := []map[string]uint64{{"length": 31_536_000, "parts": 12}}
		for range 36 {
			s = append(s, map[string]uint64{"length": 2_628_000, "parts": 1})
		}
		return s
	}()
	// monthlySchedule is the plan's pay stream: twenty-four months, no cliff.
	monthlySchedule = func() []map[string]uint64 {
		s := make([]map[string]uint64, 0, 24)
		for range 24 {
			s = append(s, map[string]uint64{"length": 2_628_000, "parts": 1})
		}
		return s
	}()
)

func newGrantFixture(t *testing.T) grantFixture {
	t.Helper()

	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{Height: 1, Time: time.Now()})
	f := grantFixture{
		app:       arkApp,
		ctx:       ctx,
		pk:        wasmkeeper.NewDefaultPermissionKeeper(&arkApp.WasmKeeper),
		registrar: authtypes.NewModuleAddress("grant-registrar"),
		creator:   authtypes.NewModuleAddress("grant-creator"),
	}
	codeID, _, err := f.pk.Create(ctx, f.creator, testdata.GrantContractWasm(), nil)
	require.NoError(t, err)

	// The cap's unit is a fifth of the test app's small bonded stake so the
	// cap still releases whole units; the plan's is a million NOAH.
	unit := f.bonded(t).QuoRaw(5)
	init := map[string]any{
		"denom":           chain.NoahBaseDenom,
		"registrar":       f.registrar.String(),
		"issuance_limit":  map[string]any{"max_members": 1000, "window_seconds": 604_800},
		"member_grant":    memberGrant.String(),
		"member_schedule": memberSchedule,
		"fee_allowance":   memberAllowance.String(),
		"founding_stake":  oneNoah.MulRaw(50_000_000).String(),
		"seat_stake":      oneNoah.MulRaw(5_000_000).String(),
		"cap": map[string]any{
			"numerator": 1, "denominator": 5,
			"ceiling": oneNoah.MulRaw(60_000_000).String(), "unit": unit.String(),
		},
	}
	body, err := json.Marshal(init)
	require.NoError(t, err)
	f.contract, _, err = f.pk.Instantiate(ctx, codeID, f.creator, nil, body, "grant", nil)
	require.NoError(t, err)
	return f
}

// spendFromPool is the proposal's community pool spend to the contract.
func (f grantFixture) spendFromPool(t *testing.T, amount math.Int) {
	t.Helper()
	coins := sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, amount))
	funder := authtypes.NewModuleAddress("pool-funder")
	apptestutil.FundAccount(t, f.app, f.ctx, funder, coins)
	require.NoError(t, f.app.DistrKeeper.FundCommunityPool(f.ctx, coins, funder))
	require.NoError(t, f.app.DistrKeeper.DistributeFromFeePool(f.ctx, coins, f.contract))
}

func (f grantFixture) execute(sender sdk.AccAddress, msg any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	cacheCtx, write := f.ctx.CacheContext()
	if _, err := f.pk.Execute(cacheCtx, f.contract, sender, body, nil); err != nil {
		return err
	}
	write()
	return nil
}

func (f grantFixture) sudo(t *testing.T, msg any) {
	t.Helper()
	body, err := json.Marshal(msg)
	require.NoError(t, err)
	_, err = f.pk.Sudo(f.ctx, f.contract, body)
	require.NoError(t, err)
}

func (f grantFixture) query(t *testing.T, msg any, into any) {
	t.Helper()
	body, err := json.Marshal(msg)
	require.NoError(t, err)
	raw, err := f.app.WasmKeeper.QuerySmart(f.ctx, f.contract, body)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, into))
}

// allowance is the NOAH the contract has granted the account for gas.
func (f grantFixture) allowance(t *testing.T, grantee sdk.AccAddress) math.Int {
	t.Helper()
	allowance, err := f.app.FeeGrantKeeper.GetAllowance(f.ctx, f.contract, grantee)
	require.NoError(t, err)
	basic, ok := allowance.(*feegrant.BasicAllowance)
	require.True(t, ok)
	return basic.SpendLimit.AmountOf(chain.NoahBaseDenom)
}

func (f grantFixture) noah(addr sdk.AccAddress) math.Int {
	return f.app.BankKeeper.GetBalance(f.ctx, addr, chain.NoahBaseDenom).Amount
}

func (f grantFixture) communityPool(t *testing.T) math.LegacyDec {
	t.Helper()
	pool, err := f.app.DistrKeeper.FeePool.Get(f.ctx)
	require.NoError(t, err)
	return pool.CommunityPool.AmountOf(chain.NoahBaseDenom)
}

func (f grantFixture) bonded(t *testing.T) math.Int {
	t.Helper()
	// The same query the contract reads.
	pool, err := stakingkeeper.Querier{Keeper: f.app.StakingKeeper}.Pool(f.ctx, &stakingtypes.QueryPoolRequest{})
	require.NoError(t, err)
	return pool.Pool.BondedTokens
}

func (f grantFixture) delegate(t *testing.T, from sdk.AccAddress, amount math.Int) {
	t.Helper()
	validators, err := f.app.StakingKeeper.GetAllValidators(f.ctx)
	require.NoError(t, err)
	require.NotEmpty(t, validators)
	denom, err := f.app.StakingKeeper.BondDenom(f.ctx)
	require.NoError(t, err)
	_, err = stakingkeeper.NewMsgServerImpl(f.app.StakingKeeper).Delegate(f.ctx, &stakingtypes.MsgDelegate{
		DelegatorAddress: from.String(),
		ValidatorAddress: validators[0].GetOperator(),
		Amount:           sdk.NewCoin(denom, amount),
	})
	require.NoError(t, err)
}

type grantTotals struct {
	Balance          math.Int `json:"balance"`
	Unallocated      math.Int `json:"unallocated"`
	Escrowed         math.Int `json:"escrowed"`
	FeesReserved     math.Int `json:"fees_reserved"`
	FeesPromised     math.Int `json:"fees_promised"`
	ContributorsPaid math.Int `json:"contributors_paid"`
	MembersPaid      math.Int `json:"members_paid"`
	MembersIssued    uint64   `json:"members_issued"`
	MembersCancelled uint64   `json:"members_cancelled"`
}

type grantRecord struct {
	Released  math.Int `json:"released"`
	Remaining math.Int `json:"remaining"`
	Cancelled bool     `json:"cancelled"`
	Kind      struct {
		Ownership *struct {
			FeeReserve     math.Int `json:"fee_reserve"`
			ReleaseAddress *string  `json:"release_address"`
		} `json:"ownership"`
		Stream *struct {
			Start uint64 `json:"start"`
			Paid  uint32 `json:"paid"`
			Payee string `json:"payee"`
		} `json:"stream"`
	} `json:"kind"`
}

type memberRecord struct {
	Member *struct {
		Start     uint64   `json:"start"`
		Amount    math.Int `json:"amount"`
		Released  math.Int `json:"released"`
		Remaining math.Int `json:"remaining"`
		Paid      uint32   `json:"paid"`
		Suspended *struct {
			At     uint64 `json:"at"`
			Height uint64 `json:"height"`
			By     string `json:"by"`
		} `json:"suspended"`
		Cancelled bool `json:"cancelled"`
	} `json:"member"`
	Releasable math.Int `json:"releasable"`
	Elapsed    uint32   `json:"elapsed"`
	NextAt     *uint64  `json:"next_at"`
}

func (f grantFixture) totals(t *testing.T) grantTotals {
	t.Helper()
	var totals grantTotals
	f.query(t, map[string]any{"totals": map[string]any{}}, &totals)
	return totals
}

func (f grantFixture) grant(t *testing.T, id uint64) grantRecord {
	t.Helper()
	var g grantRecord
	f.query(t, map[string]any{"grant": map[string]any{"id": id}}, &g)
	return g
}

func (f grantFixture) member(t *testing.T, addr sdk.AccAddress) memberRecord {
	t.Helper()
	var m memberRecord
	f.query(t, map[string]any{"member": map[string]any{"address": addr.String()}}, &m)
	return m
}

// members is a batched member call's payload.
func members(call string, addrs ...sdk.AccAddress) map[string]any {
	addresses := make([]string, 0, len(addrs))
	for _, a := range addrs {
		addresses = append(addresses, a.String())
	}
	return map[string]any{call: map[string]any{"addresses": addresses}}
}

// TestGrantContractStreamsMemberGrants: the registrar's batch sends each
// address the first period at once, into a fresh account or one that already
// holds coins, spendable on arrival and untaxed, and holds the rest, which
// anyone releases as periods elapse. The registrar can stop a member's pay
// and governance can end it, settling what had elapsed by the suspension and
// keeping the rest in the tranche rather than returning it to the pool.
func TestGrantContractStreamsMemberGrants(t *testing.T) {
	f := newGrantFixture(t)
	tranche := memberGrant.MulRaw(3)
	f.spendFromPool(t, tranche)
	require.Equal(t, tranche, f.noah(f.contract))
	tenth := memberGrant.QuoRaw(10)
	period := memberGrant.MulRaw(3).QuoRaw(40)
	month := 2_628_000 * time.Second
	anyone := sdk.AccAddress([]byte("anyone______________"))
	release := func(addrs ...sdk.AccAddress) error {
		return f.execute(anyone, members("release_members", addrs...))
	}

	alice := sdk.AccAddress([]byte("alice_______________"))
	bob := sdk.AccAddress([]byte("bob_________________"))
	dusted := sdk.AccAddress([]byte("dusted______________"))
	// A send to the address while the proposal was open.
	apptestutil.FundAccount(t, f.app, f.ctx, dusted, sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1)))
	require.NoError(t, f.execute(f.registrar, members("register_members", alice, dusted, bob)))

	for _, member := range []sdk.AccAddress{alice, bob} {
		_, isBase := f.app.AccountKeeper.GetAccount(f.ctx, member).(*authtypes.BaseAccount)
		require.True(t, isBase, "%s is %T", member, f.app.AccountKeeper.GetAccount(f.ctx, member))
		require.Equal(t, tenth, f.noah(member))
		require.Equal(t, tenth, f.app.BankKeeper.SpendableCoins(f.ctx, member).AmountOf(chain.NoahBaseDenom), "the first period pays gas at once")
		_, err := f.app.FeeGrantKeeper.GetAllowance(f.ctx, f.contract, member)
		require.Error(t, err, "members get no allowance")
		m := f.member(t, member)
		require.NotNil(t, m.Member)
		require.Equal(t, uint32(1), m.Member.Paid)
		require.Equal(t, memberGrant.Sub(tenth), m.Member.Remaining)
		require.True(t, m.Releasable.IsZero())
	}
	require.Equal(t, tenth.AddRaw(1), f.noah(dusted), "an address holding coins is paid like any other")
	totals := f.totals(t)
	require.Equal(t, uint64(3), totals.MembersIssued)
	require.Equal(t, tenth.MulRaw(3), totals.MembersPaid)
	require.Equal(t, tranche.Sub(tenth.MulRaw(3)), totals.Escrowed)
	require.True(t, totals.Unallocated.IsZero())
	require.True(t, totals.FeesPromised.IsZero())
	require.Equal(t, tranche.Sub(tenth.MulRaw(3)), f.noah(f.contract), "only the sends left, no tax")
	require.ErrorContains(t, release(alice, bob), "nothing due")

	// The first period is stake like any other.
	f.delegate(t, alice, tenth.QuoRaw(2))

	// The month pays, spendable; a member with nothing due is skipped.
	f.ctx = f.ctx.WithBlockTime(f.ctx.BlockTime().Add(time.Second + month))
	require.Equal(t, period, f.member(t, alice).Releasable)
	require.NoError(t, release(alice, bob))
	require.Equal(t, tenth.QuoRaw(2).Add(period), f.noah(alice))
	require.Equal(t, tenth.Add(period), f.noah(bob))
	require.Equal(t, tenth.Add(period), f.app.BankKeeper.SpendableCoins(f.ctx, bob).AmountOf(chain.NoahBaseDenom))
	require.NoError(t, release(alice, dusted))
	require.Equal(t, tenth.AddRaw(1).Add(period), f.noah(dusted))
	require.Equal(t, uint32(2), f.member(t, alice).Member.Paid)

	// A month on the registrar suspends alice before she is paid: bob is
	// paid, alice is not.
	f.ctx = f.ctx.WithBlockTime(f.ctx.BlockTime().Add(month))
	require.ErrorContains(t, f.execute(anyone, members("suspend_members", alice)), "not the registrar")
	require.NoError(t, f.execute(f.registrar, members("suspend_members", alice)))
	m := f.member(t, alice)
	require.NotNil(t, m.Member.Suspended)
	require.Equal(t, f.registrar.String(), m.Member.Suspended.By)
	require.True(t, m.Releasable.IsZero())
	require.NoError(t, release(alice, bob))
	require.Equal(t, tenth.QuoRaw(2).Add(period), f.noah(alice), "suspended")
	require.Equal(t, tenth.Add(period.MulRaw(2)), f.noah(bob))

	// A month later governance cancels alice: the month that had elapsed by
	// the suspension settles, the month since does not, and the rest stays
	// in the contract as unallocated.
	f.ctx = f.ctx.WithBlockTime(f.ctx.BlockTime().Add(month))
	poolBefore := f.communityPool(t)
	f.sudo(t, members("cancel_members", alice))
	require.Equal(t, tenth.QuoRaw(2).Add(period.MulRaw(2)), f.noah(alice), "one month settled")
	require.Equal(t, poolBefore, f.communityPool(t), "nothing went back to the pool")
	m = f.member(t, alice)
	require.True(t, m.Member.Cancelled)
	require.True(t, m.Member.Remaining.IsZero())
	require.Equal(t, uint32(3), m.Member.Paid)
	returned := memberGrant.Sub(tenth).Sub(period.MulRaw(2))
	totals = f.totals(t)
	require.Equal(t, returned, totals.Unallocated, "the rest is the tranche's again")
	require.Equal(t, uint64(1), totals.MembersCancelled)
	require.Equal(t, totals.Escrowed.Add(returned), f.noah(f.contract))
	require.ErrorContains(t, release(alice), "nothing due")
	require.ErrorContains(t, f.execute(f.registrar, members("register_members", alice)), "already registered")
}

// TestGrantContractEscrowsAndReleasesByBondedStake: a grant larger than a
// fifth of what others have bonded pays that fifth now, escrows the rest,
// and releases the rest once bonded stake grows; governance can cancel what
// is unreleased back to the community pool.
func TestGrantContractEscrowsAndReleasesByBondedStake(t *testing.T) {
	f := newGrantFixture(t)
	bonded := f.bonded(t)
	amount := bonded // more than a fifth of bonded stake, so it escrows
	// Five units, so five tranches at most: the proposal brings five allowances.
	reserve := memberAllowance.MulRaw(5)
	f.spendFromPool(t, amount.Add(reserve))
	poolBefore := f.communityPool(t)

	grantee := sdk.AccAddress([]byte("contributor_________"))
	f.sudo(t, map[string]any{"add_grant": map[string]any{
		"grantee": grantee.String(), "amount": amount.String(), "schedule": standardSchedule,
		"seat_holder": false,
	}})

	first := bonded.QuoRaw(5)
	acc, ok := f.app.AccountKeeper.GetAccount(f.ctx, grantee).(*vestingtypes.PeriodicVestingAccount)
	require.True(t, ok)
	require.Equal(t, first, acc.OriginalVesting.AmountOf(chain.NoahBaseDenom))
	require.Equal(t, memberAllowance, f.allowance(t, grantee), "the tranche can pay its own gas")
	g := f.grant(t, 1)
	require.Equal(t, first, g.Released)
	require.Equal(t, amount.Sub(first), g.Remaining)
	require.Nil(t, g.Kind.Ownership.ReleaseAddress)
	require.Equal(t, amount.Sub(first), f.totals(t).Escrowed)
	require.Equal(t, memberAllowance, f.totals(t).FeesPromised)
	require.Equal(t, reserve.Sub(memberAllowance), g.Kind.Ownership.FeeReserve, "one allowance promised from the reserve")
	require.Equal(t, reserve.Sub(memberAllowance), f.totals(t).FeesReserved)

	// The grantee names the next address from the account they now hold.
	second := sdk.AccAddress([]byte("contributor_2_______"))
	require.NoError(t, f.execute(grantee, map[string]any{"set_release_address": map[string]any{"id": 1, "address": second.String()}}))
	err := f.execute(grantee, map[string]any{"release": map[string]any{"id": 1}})
	require.ErrorContains(t, err, "nothing releasable")

	// A whale bonds five times the stake: others then hold six times it less
	// the grantee's fifth, and a fifth of that, less what the grantee has,
	// covers the rest.
	whale := sdk.AccAddress([]byte("whale_______________"))
	apptestutil.FundAccount(t, f.app, f.ctx, whale, sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, bonded.MulRaw(5))))
	f.delegate(t, whale, bonded.MulRaw(5))
	var r struct {
		Amount math.Int `json:"amount"`
		Rule   struct {
			Cap *struct {
				Bonded math.Int `json:"bonded"`
				Own    math.Int `json:"own"`
			} `json:"cap"`
		} `json:"rule"`
	}
	f.query(t, map[string]any{"releasable": map[string]any{"id": 1}}, &r)
	require.Equal(t, bonded.MulRaw(6), r.Rule.Cap.Bonded)
	require.Equal(t, first, r.Rule.Cap.Own)
	require.Equal(t, amount.Sub(first), r.Amount)
	anyone := sdk.AccAddress([]byte("anyone______________"))
	require.NoError(t, f.execute(anyone, map[string]any{"release": map[string]any{"id": 1}}))
	acc, ok = f.app.AccountKeeper.GetAccount(f.ctx, second).(*vestingtypes.PeriodicVestingAccount)
	require.True(t, ok)
	require.Equal(t, amount.Sub(first), acc.OriginalVesting.AmountOf(chain.NoahBaseDenom))
	require.Equal(t, memberAllowance, f.allowance(t, second))
	g = f.grant(t, 1)
	require.True(t, g.Remaining.IsZero())
	require.Equal(t, amount, g.Released)
	require.True(t, f.totals(t).Escrowed.IsZero())
	require.True(t, f.totals(t).FeesReserved.IsZero(), "the last tranche returned the unused reserve")
	require.Equal(t, memberAllowance.MulRaw(2), f.noah(f.contract), "only promised gas remains")

	// A second grant, three times the original stake against a cap now a
	// fifth of six times it, escrows most of itself; cancelling returns that.
	large := bonded.MulRaw(3)
	reserve = memberAllowance.MulRaw(15) // fifteen units
	f.spendFromPool(t, large.Add(reserve))
	other := sdk.AccAddress([]byte("contributor_b_______"))
	f.sudo(t, map[string]any{"add_grant": map[string]any{
		"grantee": other.String(), "amount": large.String(), "schedule": standardSchedule,
		"seat_holder": false,
	}})
	require.Equal(t, bonded.MulRaw(6).QuoRaw(5), f.grant(t, 2).Released)
	escrowed := f.grant(t, 2).Remaining
	require.True(t, escrowed.IsPositive())
	poolBeforeCancel := f.communityPool(t)
	f.sudo(t, map[string]any{"cancel_grant": map[string]any{"id": 2}})
	require.True(t, f.grant(t, 2).Cancelled)
	unusedGas := reserve.Sub(memberAllowance)
	require.Equal(t, poolBeforeCancel.Add(math.LegacyNewDecFromInt(escrowed.Add(unusedGas))), f.communityPool(t), "the escrow and its unused gas came back")
	// The first grant's last tranche returned its three unused allowances too.
	returned := escrowed.Add(unusedGas).Add(memberAllowance.MulRaw(3))
	require.Equal(t, poolBefore.Add(math.LegacyNewDecFromInt(returned)), f.communityPool(t), "the escrow and unused gas came back; released tranches are the grantee's")
	require.Equal(t, memberAllowance.MulRaw(3), f.noah(f.contract), "only the three tranches' promised gas remains")
	require.ErrorContains(t, f.execute(anyone, map[string]any{"release": map[string]any{"id": 2}}), "cancelled")
}

func TestGrantContractOwnershipCeilingExcludesTheSeat(t *testing.T) {
	for _, tc := range []struct {
		name       string
		seatHolder bool
		own        int64
	}{
		{name: "contributor", own: 60_000_000},
		{name: "seat holder", seatHolder: true, own: 65_000_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newGrantFixture(t)
			unit := f.bonded(t).QuoRaw(5)
			public := authtypes.NewModuleAddress("ceiling-public-stake")
			stake := oneNoah.MulRaw(400_000_000)
			apptestutil.FundAccount(t, f.app, f.ctx, public, sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, stake)))
			f.delegate(t, public, stake)
			grantee := authtypes.NewModuleAddress("ceiling-contributor")
			amount := oneNoah.MulRaw(30_000_000)
			reserve := amount.Add(unit).SubRaw(1).Quo(unit).Mul(memberAllowance)

			for i, address := range []sdk.AccAddress{grantee, authtypes.NewModuleAddress("ceiling-second-tranche")} {
				f.spendFromPool(t, amount.Add(reserve))
				f.sudo(t, map[string]any{"add_grant": map[string]any{
					"grantee": grantee.String(), "amount": amount.String(), "schedule": standardSchedule,
					"seat_holder": tc.seatHolder, "release_address": address.String(),
				}})
				g := f.grant(t, uint64(i+1))
				require.Equal(t, amount, g.Released)
				require.True(t, g.Remaining.IsZero())
				acc, ok := f.app.AccountKeeper.GetAccount(f.ctx, address).(*vestingtypes.PeriodicVestingAccount)
				require.True(t, ok)
				require.Equal(t, amount, acc.OriginalVesting.AmountOf(chain.NoahBaseDenom))
			}
			require.Equal(t, oneNoah.MulRaw(60_000_000), f.totals(t).ContributorsPaid)

			// Further grants share the ceiling already reached by the first two.
			amount = oneNoah.MulRaw(1_000_000)
			reserve = amount.Add(unit).SubRaw(1).Quo(unit).Mul(memberAllowance)
			f.spendFromPool(t, amount.Add(reserve))
			next := authtypes.NewModuleAddress("ceiling-extra-tranche")
			f.sudo(t, map[string]any{"add_grant": map[string]any{
				"grantee": grantee.String(), "amount": amount.String(), "schedule": standardSchedule,
				"seat_holder": tc.seatHolder, "release_address": next.String(),
			}})
			g := f.grant(t, 3)
			require.True(t, g.Released.IsZero())
			require.Equal(t, amount, g.Remaining)
			var r struct {
				Amount math.Int `json:"amount"`
				Rule   struct {
					Cap struct {
						Own math.Int `json:"own"`
						Cap math.Int `json:"cap"`
					} `json:"cap"`
				} `json:"rule"`
			}
			f.query(t, map[string]any{"releasable": map[string]any{"id": 3}}, &r)
			require.True(t, r.Amount.IsZero())
			require.Equal(t, oneNoah.MulRaw(tc.own), r.Rule.Cap.Own)
			require.Equal(t, oneNoah.MulRaw(tc.own), r.Rule.Cap.Cap)
			require.ErrorContains(t, f.execute(grantee, map[string]any{"release": map[string]any{"id": 3}}), "nothing releasable")
		})
	}
}

// TestGrantContractRegistrarIsTheOnlyIssuer: anyone else is refused, and
// governance can swap the registrar through sudo.
func TestGrantContractRegistrarIsTheOnlyIssuer(t *testing.T) {
	f := newGrantFixture(t)
	f.spendFromPool(t, memberGrant)
	member := sdk.AccAddress([]byte("member______________"))
	stranger := sdk.AccAddress([]byte("stranger____________"))
	batch := map[string]any{"register_members": map[string]any{"addresses": []string{member.String()}}}

	require.ErrorContains(t, f.execute(stranger, batch), "not the registrar")
	f.sudo(t, map[string]any{"set_registrar": map[string]any{"registrar": stranger.String()}})
	require.ErrorContains(t, f.execute(f.registrar, batch), "not the registrar")
	require.NoError(t, f.execute(stranger, batch))
	require.NotNil(t, f.member(t, member).Member)
	require.Equal(t, fmt.Sprint(memberGrant.QuoRaw(10)), f.noah(member).String())
}

// TestGrantContractStreamsPayByTheClock: a stream stays in the contract and
// pays each elapsed period to the payee by bank send, spendable on arrival,
// into an ordinary account or beside a vesting one; governance cancels what
// has not elapsed back to the pool and holds what has for the payee, so a
// payee that cannot receive funds cannot fail the cancel.
func TestGrantContractStreamsPayByTheClock(t *testing.T) {
	f := newGrantFixture(t)
	amount := oneNoah.MulRaw(262_000)
	period := amount.QuoRaw(24)
	month := 2_628_000 * time.Second
	anyone := sdk.AccAddress([]byte("anyone______________"))
	release := func(id uint64) error {
		return f.execute(anyone, map[string]any{"release": map[string]any{"id": id}})
	}

	f.spendFromPool(t, amount)
	hire := sdk.AccAddress([]byte("hire________________"))
	f.sudo(t, map[string]any{"add_stream": map[string]any{
		"grantee": hire.String(), "amount": amount.String(), "schedule": monthlySchedule,
	}})
	require.Nil(t, f.app.AccountKeeper.GetAccount(f.ctx, hire), "nothing pays at once")
	g := f.grant(t, 1)
	require.Equal(t, amount, g.Remaining)
	require.NotNil(t, g.Kind.Stream)
	require.Equal(t, uint64(f.ctx.BlockTime().Unix()), g.Kind.Stream.Start)
	require.Equal(t, amount, f.totals(t).Escrowed)
	require.True(t, f.totals(t).FeesReserved.IsZero(), "a stream reserves no gas")
	require.ErrorContains(t, release(1), "nothing releasable")

	// The month pays into an ordinary account, spendable at once.
	f.ctx = f.ctx.WithBlockTime(f.ctx.BlockTime().Add(month))
	require.NoError(t, release(1))
	_, isBase := f.app.AccountKeeper.GetAccount(f.ctx, hire).(*authtypes.BaseAccount)
	require.True(t, isBase, "paid by send, not a vesting account")
	require.Equal(t, period, f.noah(hire))
	require.Equal(t, period, f.app.BankKeeper.SpendableCoins(f.ctx, hire).AmountOf(chain.NoahBaseDenom))
	require.Equal(t, uint32(1), f.grant(t, 1).Kind.Stream.Paid)
	require.Equal(t, period, f.totals(t).ContributorsPaid)
	require.ErrorContains(t, release(1), "nothing releasable")

	// A hire's ownership grant and stream share an address: the grant makes
	// it a vesting account, and the stream's pay arrives spendable in it.
	stake := f.bonded(t).QuoRaw(10) // under the cap, so it pays whole
	f.spendFromPool(t, stake.Add(memberAllowance).Add(amount))
	both := sdk.AccAddress([]byte("hire_with_stake_____"))
	f.sudo(t, map[string]any{"add_grant": map[string]any{
		"grantee": both.String(), "amount": stake.String(), "schedule": standardSchedule,
		"seat_holder": false,
	}})
	f.sudo(t, map[string]any{"add_stream": map[string]any{
		"grantee": both.String(), "amount": amount.String(), "schedule": monthlySchedule,
	}})
	_, isVesting := f.app.AccountKeeper.GetAccount(f.ctx, both).(*vestingtypes.PeriodicVestingAccount)
	require.True(t, isVesting)
	require.True(t, f.app.BankKeeper.SpendableCoins(f.ctx, both).AmountOf(chain.NoahBaseDenom).IsZero(), "the grant is locked")
	f.ctx = f.ctx.WithBlockTime(f.ctx.BlockTime().Add(month))
	require.NoError(t, release(3))
	require.Equal(t, stake.Add(period), f.noah(both))
	require.Equal(t, period, f.app.BankKeeper.SpendableCoins(f.ctx, both).AmountOf(chain.NoahBaseDenom), "the pay is spendable beside the locked grant")

	// The hire points the pay at a module account, which cannot receive
	// funds. Cancelled a month and a half on, the cancel still lands: the
	// elapsed months are held for the payee and the rest returns to the pool.
	blocked := authtypes.NewModuleAddress("distribution")
	require.True(t, f.app.BankKeeper.BlockedAddr(blocked))
	require.NoError(t, f.execute(hire, map[string]any{"set_release_address": map[string]any{"id": 1, "address": blocked.String()}}))
	f.ctx = f.ctx.WithBlockTime(f.ctx.BlockTime().Add(month + month/2))
	poolBefore := f.communityPool(t)
	f.sudo(t, map[string]any{"cancel_grant": map[string]any{"id": 1}})
	require.Equal(t, period, f.noah(hire), "the cancel sends nothing")
	returned := amount.Sub(period.MulRaw(3))
	require.Equal(t, poolBefore.Add(math.LegacyNewDecFromInt(returned)), f.communityPool(t), "the rest came back")
	g = f.grant(t, 1)
	require.True(t, g.Cancelled)
	require.Equal(t, period.MulRaw(2), g.Remaining, "the elapsed months are held")
	require.Equal(t, uint32(3), g.Kind.Stream.Paid)
	require.ErrorContains(t, release(1), "not allowed to receive funds")

	// The controller repoints the held pay and anyone releases it.
	require.NoError(t, f.execute(hire, map[string]any{"set_release_address": map[string]any{"id": 1, "address": hire.String()}}))
	require.NoError(t, release(1))
	require.Equal(t, period.MulRaw(3), f.noah(hire))
	require.True(t, f.grant(t, 1).Remaining.IsZero())
	require.ErrorContains(t, release(1), "cancelled")
}
