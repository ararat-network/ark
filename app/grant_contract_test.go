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
	oneNoah          = math.NewInt(1_000_000_000_000_000_000)
	memberGrant      = oneNoah.MulRaw(10_000)
	memberAllowance  = oneNoah.MulRaw(2)
	standardSchedule = func() []map[string]uint64 {
		s := []map[string]uint64{{"length": 31_536_000, "parts": 12}}
		for range 36 {
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

	// The cap's unit is one base unit so the test app's small bonded stake
	// still releases something; the plan's is a million NOAH.
	init := map[string]any{
		"denom":           chain.NoahBaseDenom,
		"registrar":       f.registrar.String(),
		"issuance_limit":  map[string]any{"max_members": 1000, "window_seconds": 604_800},
		"member_grant":    memberGrant.String(),
		"member_schedule": standardSchedule,
		"fee_allowance":   memberAllowance.String(),
		"founding_stake":  oneNoah.MulRaw(50_000_000).String(),
		"seat_stake":      oneNoah.MulRaw(5_000_000).String(),
		"cap": map[string]any{
			"numerator": 1, "denominator": 5,
			"ceiling": oneNoah.MulRaw(60_000_000).String(), "unit": "1",
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
	ContributorsPaid math.Int `json:"contributors_paid"`
	MembersIssued    uint64   `json:"members_issued"`
	MembersRejected  uint64   `json:"members_rejected"`
}

type grantRecord struct {
	Released       math.Int `json:"released"`
	Remaining      math.Int `json:"remaining"`
	ReleaseAddress *string  `json:"release_address"`
	Cancelled      bool     `json:"cancelled"`
}

type memberStatus struct {
	Status *struct {
		Issued   *struct{ Height uint64 } `json:"issued"`
		Rejected *struct {
			Height uint64 `json:"height"`
			Error  string `json:"error"`
		} `json:"rejected"`
	} `json:"status"`
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

func (f grantFixture) member(t *testing.T, addr sdk.AccAddress) memberStatus {
	t.Helper()
	var m memberStatus
	f.query(t, map[string]any{"member": map[string]any{"address": addr.String()}}, &m)
	return m
}

// TestGrantContractIssuesMembersAndSkipsDustedAddresses: the registrar's
// batch creates a periodic vesting account and a fee allowance for each
// fresh address, and an address that already holds an account is rejected
// and recorded without failing the rest. Nothing but the grants leaves the
// contract: the vesting message is untaxed for NOAH.
func TestGrantContractIssuesMembersAndSkipsDustedAddresses(t *testing.T) {
	f := newGrantFixture(t)
	f.spendFromPool(t, memberGrant.MulRaw(3))
	require.Equal(t, memberGrant.MulRaw(3), f.noah(f.contract))

	alice := sdk.AccAddress([]byte("alice_______________"))
	bob := sdk.AccAddress([]byte("bob_________________"))
	dusted := sdk.AccAddress([]byte("dusted______________"))
	// A send to the address while the proposal was open.
	apptestutil.FundAccount(t, f.app, f.ctx, dusted, sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1)))
	_, isBase := f.app.AccountKeeper.GetAccount(f.ctx, dusted).(*authtypes.BaseAccount)
	require.True(t, isBase)

	require.NoError(t, f.execute(f.registrar, map[string]any{"register_members": map[string]any{
		"addresses": []string{alice.String(), dusted.String(), bob.String()},
	}}))

	for _, member := range []sdk.AccAddress{alice, bob} {
		acc, ok := f.app.AccountKeeper.GetAccount(f.ctx, member).(*vestingtypes.PeriodicVestingAccount)
		require.True(t, ok, "%s is %T", member, f.app.AccountKeeper.GetAccount(f.ctx, member))
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, memberGrant)), acc.OriginalVesting)
		require.Equal(t, f.ctx.BlockTime().Unix(), acc.StartTime)
		require.Len(t, acc.VestingPeriods, 37)
		require.Equal(t, memberGrant.QuoRaw(4), acc.VestingPeriods[0].Amount.AmountOf(chain.NoahBaseDenom))
		require.Equal(t, memberGrant, f.noah(member))
		require.Equal(t, memberGrant, acc.LockedCoins(f.ctx.BlockTime()).AmountOf(chain.NoahBaseDenom), "nothing vested yet")

		allowance, err := f.app.FeeGrantKeeper.GetAllowance(f.ctx, f.contract, member)
		require.NoError(t, err)
		basic, ok := allowance.(*feegrant.BasicAllowance)
		require.True(t, ok)
		require.Equal(t, memberAllowance, basic.SpendLimit.AmountOf(chain.NoahBaseDenom))
		require.NotNil(t, f.member(t, member).Status.Issued)
	}

	_, stillBase := f.app.AccountKeeper.GetAccount(f.ctx, dusted).(*authtypes.BaseAccount)
	require.True(t, stillBase, "the dusted address was left alone")
	require.Equal(t, math.OneInt(), f.noah(dusted))
	rejected := f.member(t, dusted).Status.Rejected
	require.NotNil(t, rejected)
	// Wasmd hands a contract only the codespace and code of a failed
	// submessage, so replies are deterministic: this is ErrInvalidRequest,
	// the vesting handler's "account already exists".
	require.Contains(t, rejected.Error, "codespace: sdk, code: 18")
	_, err := f.app.FeeGrantKeeper.GetAllowance(f.ctx, f.contract, dusted)
	require.Error(t, err)

	totals := f.totals(t)
	require.Equal(t, uint64(2), totals.MembersIssued)
	require.Equal(t, uint64(1), totals.MembersRejected)
	require.Equal(t, memberGrant, totals.Unallocated, "the rejected grant stays in the tranche")
	require.Equal(t, memberGrant, f.noah(f.contract), "exactly two grants left, no tax")

	// The unvested grant delegates like any other stake.
	f.delegate(t, alice, memberGrant.QuoRaw(2))
	acc := f.app.AccountKeeper.GetAccount(f.ctx, alice).(*vestingtypes.PeriodicVestingAccount)
	require.Equal(t, memberGrant.QuoRaw(2), acc.DelegatedVesting.AmountOf(chain.NoahBaseDenom))
}

// TestGrantContractEscrowsAndReleasesByBondedStake: a grant larger than a
// fifth of what others have bonded pays that fifth now, escrows the rest,
// and releases the rest once bonded stake grows; governance can cancel what
// is unreleased back to the community pool.
func TestGrantContractEscrowsAndReleasesByBondedStake(t *testing.T) {
	f := newGrantFixture(t)
	bonded := f.bonded(t)
	amount := bonded // more than a fifth of bonded stake, so it escrows
	f.spendFromPool(t, amount)
	poolBefore := f.communityPool(t)

	grantee := sdk.AccAddress([]byte("contributor_________"))
	f.sudo(t, map[string]any{"add_grant": map[string]any{
		"grantee": grantee.String(), "amount": amount.String(), "schedule": standardSchedule,
		"seat_holder": false, "paid_elsewhere": "0",
	}})

	first := bonded.QuoRaw(5)
	acc, ok := f.app.AccountKeeper.GetAccount(f.ctx, grantee).(*vestingtypes.PeriodicVestingAccount)
	require.True(t, ok)
	require.Equal(t, first, acc.OriginalVesting.AmountOf(chain.NoahBaseDenom))
	g := f.grant(t, 1)
	require.Equal(t, first, g.Released)
	require.Equal(t, amount.Sub(first), g.Remaining)
	require.Nil(t, g.ReleaseAddress)
	require.Equal(t, amount.Sub(first), f.totals(t).Escrowed)

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
		Bonded math.Int `json:"bonded"`
		Own    math.Int `json:"own"`
	}
	f.query(t, map[string]any{"releasable": map[string]any{"id": 1}}, &r)
	require.Equal(t, bonded.MulRaw(6), r.Bonded)
	require.Equal(t, first, r.Own)
	require.Equal(t, amount.Sub(first), r.Amount)
	anyone := sdk.AccAddress([]byte("anyone______________"))
	require.NoError(t, f.execute(anyone, map[string]any{"release": map[string]any{"id": 1}}))
	acc, ok = f.app.AccountKeeper.GetAccount(f.ctx, second).(*vestingtypes.PeriodicVestingAccount)
	require.True(t, ok)
	require.Equal(t, amount.Sub(first), acc.OriginalVesting.AmountOf(chain.NoahBaseDenom))
	g = f.grant(t, 1)
	require.True(t, g.Remaining.IsZero())
	require.Equal(t, amount, g.Released)
	require.True(t, f.totals(t).Escrowed.IsZero())
	require.True(t, f.noah(f.contract).IsZero())

	// A second grant, three times the original stake against a cap now a
	// fifth of six times it, escrows most of itself; cancelling returns that.
	large := bonded.MulRaw(3)
	f.spendFromPool(t, large)
	other := sdk.AccAddress([]byte("contributor_b_______"))
	f.sudo(t, map[string]any{"add_grant": map[string]any{
		"grantee": other.String(), "amount": large.String(), "schedule": standardSchedule,
		"seat_holder": false, "paid_elsewhere": "0",
	}})
	require.Equal(t, bonded.MulRaw(6).QuoRaw(5), f.grant(t, 2).Released)
	escrowed := f.grant(t, 2).Remaining
	require.True(t, escrowed.IsPositive())
	poolBeforeCancel := f.communityPool(t)
	f.sudo(t, map[string]any{"cancel_grant": map[string]any{"id": 2}})
	require.True(t, f.grant(t, 2).Cancelled)
	require.Equal(t, poolBeforeCancel.Add(math.LegacyNewDecFromInt(escrowed)), f.communityPool(t))
	require.Equal(t, poolBefore.Add(math.LegacyNewDecFromInt(escrowed)), f.communityPool(t), "only the escrow came back; released tranches are the grantee's")
	require.True(t, f.noah(f.contract).IsZero())
	require.ErrorContains(t, f.execute(anyone, map[string]any{"release": map[string]any{"id": 2}}), "cancelled")
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
	require.NotNil(t, f.member(t, member).Status.Issued)
	require.Equal(t, fmt.Sprint(memberGrant), f.noah(member).String())
}
