package simulation_test

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	codectestutil "github.com/cosmos/cosmos-sdk/codec/testutil"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/std"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/disbursement/keeper"
	"github.com/ararat-network/ark/x/disbursement/simulation"
	"github.com/ararat-network/ark/x/disbursement/testutil"
	"github.com/ararat-network/ark/x/disbursement/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// seededCustody is what each pool holds at the fixture's start.
const seededCustody = int64(1_000_000_000_000)

type disbursementFixture struct {
	ctx       sdk.Context
	keeper    *keeper.Keeper
	msgServer types.MsgServer
	testData  *simsx.ChainDataSource
	reporter  *simsx.BasicSimulationReporter
	accounts  []simtypes.Account
	rand      *rand.Rand
	authority string
}

// newDisbursementFixture builds the keeper over an in-memory Bank that moves NOAH between module and
// run accounts, and an oracle that values NOAH at a fixed rate.
func newDisbursementFixture(t *testing.T) disbursementFixture {
	t.Helper()

	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := sdktestutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("transient_test"))
	ctx := testCtx.Ctx.WithBlockHeight(10).WithBlockTime(time.Unix(1_800_000_000, 0))

	ctrl := gomock.NewController(t)
	modules := map[string]sdk.AccAddress{govtypes.ModuleName: authtypes.NewModuleAddress(govtypes.ModuleName)}
	for _, name := range types.PoolAccountNames() {
		modules[name] = authtypes.NewModuleAddress(name)
	}
	balances := map[string]math.Int{}
	for _, name := range types.PoolAccountNames() {
		balances[modules[name].String()] = math.NewInt(seededCustody)
	}
	balance := func(address sdk.AccAddress) math.Int {
		if held, ok := balances[address.String()]; ok {
			return held
		}
		return math.ZeroInt()
	}

	accountKeeper := testutil.NewMockAccountKeeper(ctrl)
	accountKeeper.EXPECT().GetModuleAddress(gomock.Any()).DoAndReturn(func(name string) sdk.AccAddress { return modules[name] }).AnyTimes()
	accountKeeper.EXPECT().GetAccount(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	bankKeeper := testutil.NewMockBankKeeper(ctrl)
	bankKeeper.EXPECT().BlockedAddr(gomock.Any()).Return(false).AnyTimes()
	bankKeeper.EXPECT().
		GetBalance(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, address sdk.AccAddress, denom string) sdk.Coin {
			if denom != chain.NoahBaseDenom {
				return sdk.NewCoin(denom, math.ZeroInt())
			}
			return chain.NoahCoin(balance(address))
		}).
		AnyTimes()
	bankKeeper.EXPECT().
		SendCoinsFromModuleToAccount(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, name string, to sdk.AccAddress, coins sdk.Coins) error {
			amount := coins.AmountOf(chain.NoahBaseDenom)
			balances[modules[name].String()] = balance(modules[name]).Sub(amount)
			balances[to.String()] = balance(to).Add(amount)
			return nil
		}).
		AnyTimes()
	distributionKeeper := testutil.NewMockDistributionKeeper(ctrl)
	distributionKeeper.EXPECT().
		FundCommunityPool(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, coins sdk.Coins, from sdk.AccAddress) error {
			balances[from.String()] = balance(from).Sub(coins.AmountOf(chain.NoahBaseDenom))
			return nil
		}).
		AnyTimes()
	wasmKeeper := testutil.NewMockWasmKeeper(ctrl)
	wasmKeeper.EXPECT().HasContractInfo(gomock.Any(), gomock.Any()).Return(false).AnyTimes()
	oracleKeeper := testutil.NewMockOracleKeeper(ctrl)
	oracleKeeper.EXPECT().GetReferenceDenom(gomock.Any()).Return("axdr", nil).AnyTimes()
	oracleKeeper.EXPECT().
		GetRateSet(gomock.Any(), gomock.Any()).
		Return(oracletypes.NewRateSetFrom(map[string]math.LegacyDec{"axdr": math.LegacyNewDec(2)}), nil).
		AnyTimes()

	authority := modules[govtypes.ModuleName].String()
	k := keeper.NewKeeper(
		cdc,
		runtime.NewKVStoreService(key),
		authority,
		accountKeeper,
		wasmKeeper,
		bankKeeper,
		distributionKeeper,
		testutil.NewMockMarketKeeper(ctrl),
		testutil.NewMockAssetKeeper(ctrl),
		oracleKeeper,
	)
	r := rand.New(rand.NewSource(1))
	require.NoError(t, k.Params.Set(ctx, simulation.RandomisedParams(r)))
	require.NoError(t, k.GrantsMandate.Set(ctx, types.DefaultGrantsMandate()))
	require.NoError(t, k.RegistrarMandate.Set(ctx, types.DefaultRegistrarMandate()))
	require.NoError(t, k.CompensationUsed.Set(ctx, math.ZeroInt()))
	require.NoError(t, k.MemberTranche.Set(ctx, math.ZeroInt()))
	require.NoError(t, k.NextGrantID.Set(ctx, 1))
	require.NoError(t, k.NextGrantEntryID.Set(ctx, 1))
	require.NoError(t, k.NextBeneficiaryEntryID.Set(ctx, 1))

	accounts := simtypes.RandomAccounts(r, 4)
	testData := simsx.NewChainDataSource(
		ctx,
		r,
		accountKeeper,
		nil,
		addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix()),
		accounts...,
	)

	return disbursementFixture{
		ctx:       ctx,
		keeper:    k,
		msgServer: keeper.NewMsgServerImpl(k),
		testData:  testData,
		reporter:  simsx.NewBasicSimulationReporter(),
		accounts:  accounts,
		rand:      r,
		authority: authority,
	}
}

// appoint installs the committee and registrar the genesis generator draws over the run's accounts.
func (f disbursementFixture) appoint(t *testing.T) (types.GrantsMandate, types.RegistrarMandate) {
	t.Helper()

	addresses := make([]string, 0, len(f.accounts))
	for _, account := range f.accounts {
		addresses = append(addresses, account.Address.String())
	}
	committee := simulation.GenGrantsMandate(f.rand, addresses)
	registrar := simulation.GenRegistrarMandate(f.rand, addresses, committee)
	require.NoError(t, f.keeper.GrantsMandate.Set(f.ctx, committee))
	require.NoError(t, f.keeper.RegistrarMandate.Set(f.ctx, registrar))
	return committee, registrar
}

// openTranche opens the whole member pool, as a passed tranche proposal would.
func (f disbursementFixture) openTranche(t *testing.T) {
	t.Helper()

	_, err := f.msgServer.OpenTranche(f.ctx, &types.MsgOpenTranche{Authority: f.authority, Amount: math.NewInt(seededCustody)})
	require.NoError(t, err)
}

// register delivers one registration batch from the factory.
func (f disbursementFixture) register(t *testing.T) {
	t.Helper()

	_, msg := simulation.MsgRegisterMembersFactory(f.keeper)(f.ctx, f.testData, f.reporter)
	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	_, err := f.msgServer.RegisterMembers(f.ctx, msg)
	require.NoError(t, err)
}

// TestGovernanceFactoriesDeliver runs every governance factory from a reachable state and executes
// its message as the passed proposal would.
func TestGovernanceFactoriesDeliver(t *testing.T) {
	f := newDisbursementFixture(t)
	f.appoint(t)
	f.openTranche(t)
	f.register(t)

	_, params := simulation.MsgUpdateParamsFactory()(f.ctx, f.testData, f.reporter)
	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	_, err := f.msgServer.UpdateParams(f.ctx, params)
	require.NoError(t, err)

	_, ownership := simulation.MsgGrantOwnershipFactory(f.keeper)(f.ctx, f.testData, f.reporter)
	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	require.Equal(t, f.authority, ownership.Authority)
	_, err = f.msgServer.GrantOwnership(f.ctx, ownership)
	require.NoError(t, err)

	_, compensation := simulation.MsgCompensateFactory(f.keeper)(f.ctx, f.testData, f.reporter)
	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	require.Equal(t, f.authority, compensation.Authority)
	_, err = f.msgServer.Compensate(f.ctx, compensation)
	require.NoError(t, err)

	_, returned := simulation.MsgReturnUnallocatedFactory(f.keeper)(f.ctx, f.testData, f.reporter)
	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	_, err = f.msgServer.ReturnUnallocated(f.ctx, returned)
	require.NoError(t, err)

	// Suspend a member through the committee so the governance reinstatement has a subject.
	_, suspend := simulation.MsgCommitteeSuspendFactory(f.keeper)(f.ctx, f.testData, f.reporter)
	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	_, err = f.msgServer.CommitteeSuspend(f.ctx, suspend)
	require.NoError(t, err)
	_, reinstate := simulation.MsgReinstateMembersFactory(f.keeper)(f.ctx, f.testData, f.reporter)
	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	require.Subset(t, suspend.Addresses, reinstate.Addresses)
	_, err = f.msgServer.ReinstateMembers(f.ctx, reinstate)
	require.NoError(t, err)

	_, cancel := simulation.MsgCancelGrantsFactory(f.keeper)(f.ctx, f.testData, f.reporter)
	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	_, err = f.msgServer.CancelGrants(f.ctx, cancel)
	require.NoError(t, err)
	for _, id := range cancel.GrantIds {
		g, err := f.keeper.Grants.Get(f.ctx, id)
		require.NoError(t, err)
		require.True(t, g.Cancelled())
	}

	// The new appointment clears the registrar.
	_, appoint := simulation.MsgSetGrantsMandateFactory()(f.ctx, f.testData, f.reporter)
	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	_, err = f.msgServer.SetGrantsMandate(f.ctx, appoint)
	require.NoError(t, err)
	registrar, err := f.keeper.RegistrarMandate.Get(f.ctx)
	require.NoError(t, err)
	require.True(t, registrar.IsDisabled())
}

// An ownership clock may not start before its award, which executes only after the vote.
func TestMsgGrantOwnershipFactoryStartsAhead(t *testing.T) {
	f := newDisbursementFixture(t)
	now := chain.BlockSeconds(f.ctx)

	for range 20 {
		_, msg := simulation.MsgGrantOwnershipFactory(f.keeper)(f.ctx, f.testData, f.reporter)
		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.Greater(t, msg.StartTime, now)
	}
}

func TestGovernanceFactoriesSkipWithoutSubject(t *testing.T) {
	tests := map[string]struct {
		emit    func(f disbursementFixture) any
		comment string
	}{
		"cancel grants": {
			emit: func(f disbursementFixture) any {
				_, msg := simulation.MsgCancelGrantsFactory(f.keeper)(f.ctx, f.testData, f.reporter)
				return msg
			},
			comment: "no live grant to cancel",
		},
		"reinstate members": {
			emit: func(f disbursementFixture) any {
				_, msg := simulation.MsgReinstateMembersFactory(f.keeper)(f.ctx, f.testData, f.reporter)
				return msg
			},
			comment: "no suspended member to reinstate",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			f := newDisbursementFixture(t)

			msg := tc.emit(f)

			require.True(t, f.reporter.IsSkipped())
			require.Contains(t, f.reporter.Comment(), tc.comment)
			require.Nil(t, msg)
		})
	}
}

func TestMsgOpenTrancheFactoryOpensWithinThePool(t *testing.T) {
	f := newDisbursementFixture(t)

	_, msg := simulation.MsgOpenTrancheFactory(f.keeper)(f.ctx, f.testData, f.reporter)
	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	require.True(t, msg.Amount.LTE(math.NewInt(seededCustody)))
	_, err := f.msgServer.OpenTranche(f.ctx, msg)
	require.NoError(t, err)

	open, err := f.keeper.MemberTranche.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, msg.Amount, open)
}

// Once the whole member pool is open there is nothing left to open.
func TestMsgOpenTrancheFactorySkipsWhenFullyOpen(t *testing.T) {
	f := newDisbursementFixture(t)
	f.openTranche(t)

	_, msg := simulation.MsgOpenTrancheFactory(f.keeper)(f.ctx, f.testData, f.reporter)

	require.True(t, f.reporter.IsSkipped())
	require.Contains(t, f.reporter.Comment(), "already open in full")
	require.Nil(t, msg)
}
