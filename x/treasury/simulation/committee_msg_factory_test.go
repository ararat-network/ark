package simulation_test

import (
	"math/rand"
	"testing"

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

	"github.com/ararat-network/ark/x/treasury/keeper"
	"github.com/ararat-network/ark/x/treasury/simulation"
	"github.com/ararat-network/ark/x/treasury/testutil"
	"github.com/ararat-network/ark/x/treasury/types"
)

// The economic committee signs its policy moves rather than proposing them, so
// a run reaches this surface only when genesis appointed an address the run
// holds and the window is open at this height. Both failures read as skips.

type committeeFixture struct {
	ctx       sdk.Context
	keeper    *keeper.Keeper
	msgServer types.MsgServer
	testData  *simsx.ChainDataSource
	reporter  *simsx.BasicSimulationReporter
	accounts  []simtypes.Account
	rand      *rand.Rand
}

// newCommitteeFixture builds a real Treasury keeper over mocks at height 10,
// so a message the factory emits can be delivered through the handler it
// targets.
func newCommitteeFixture(t *testing.T, seed int64) committeeFixture {
	t.Helper()

	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)
	key := storetypes.NewKVStoreKey(types.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("transient_test")
	testCtx := sdktestutil.DefaultContextWithDB(t, key, transientKey)
	ctx := testCtx.Ctx.WithBlockHeight(10)

	ctrl := gomock.NewController(t)
	accountKeeper := testutil.NewMockAccountKeeper(ctrl)
	// The keeper panics on a fund account the app never registered, so every
	// name it resolves at construction has to answer.
	accountKeeper.EXPECT().
		GetModuleAddress(gomock.Any()).
		DoAndReturn(authtypes.NewModuleAddress).
		AnyTimes()
	accountKeeper.EXPECT().GetAccount(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	k := keeper.NewKeeper(
		cdc,
		runtime.NewKVStoreService(key),
		runtime.NewTransientStoreService(transientKey),
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		accountKeeper,
		testutil.NewMockBankKeeper(ctrl),
		testutil.NewMockOracleKeeper(ctrl),
		testutil.NewMockAssetKeeper(ctrl),
		testutil.NewMockClaimsKeeper(ctrl),
		testutil.NewMockReserveKeeper(ctrl),
	)
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams()))
	require.NoError(t, k.EconomicPolicy.Set(ctx, types.DefaultEconomicPolicy()))
	require.NoError(t, k.EconomicMandate.Set(ctx, types.DefaultEconomicMandate()))

	r := rand.New(rand.NewSource(seed))
	accounts := simtypes.RandomAccounts(r, 2)
	testData := simsx.NewChainDataSource(
		ctx,
		r,
		accountKeeper,
		nil,
		addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix()),
		accounts...,
	)

	return committeeFixture{
		ctx:       ctx,
		keeper:    k,
		msgServer: keeper.NewMsgServerImpl(k),
		testData:  testData,
		reporter:  simsx.NewBasicSimulationReporter(),
		accounts:  accounts,
		rand:      r,
	}
}

// appointCommittee installs the appointment the sim's own genesis generator
// draws over the run's accounts.
func (f committeeFixture) appointCommittee(t *testing.T) types.EconomicMandate {
	t.Helper()

	addresses := make([]string, 0, len(f.accounts))
	for _, account := range f.accounts {
		addresses = append(addresses, account.Address.String())
	}
	mandate := simulation.GenEconomicMandate(f.rand, addresses)
	require.NoError(t, f.keeper.EconomicMandate.Set(f.ctx, mandate))

	return mandate
}

// TestMsgCommitteeUpdatePolicyFactoryStaysInsideTheCorridor is the
// reachability proof. The corridor is what the committee may move within, and
// a draw outside it is refused by the handler rather than skipped, so a run
// would report delivered messages that changed nothing.
func TestMsgCommitteeUpdatePolicyFactoryStaysInsideTheCorridor(t *testing.T) {
	for seed := range int64(factorySeeds) {
		f := newCommitteeFixture(t, seed+1)
		mandate := f.appointCommittee(t)

		signers, msg := simulation.MsgCommitteeUpdatePolicyFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), "seed %d: %s", seed, f.reporter.Comment())
		require.Len(t, signers, 1)
		require.Equal(t, mandate.Committee, signers[0].Address.String(), "the appointed committee must sign")
		require.Equal(t, mandate.Term, msg.ExpectedTerm)
		require.NoError(t, msg.Policy.Validate())
		require.NoError(t, mandate.ValidatePolicy(msg.Policy), "seed %d: the draw left the corridor", seed)

		_, err := f.msgServer.CommitteeUpdatePolicy(f.ctx, msg)
		require.NoError(t, err)
	}
}

func TestMsgCommitteeUpdatePolicyFactorySkipsWithoutAnActiveMandate(t *testing.T) {
	f := newCommitteeFixture(t, 1)

	_, msg := simulation.MsgCommitteeUpdatePolicyFactory(f.keeper)(f.ctx, f.testData, f.reporter)

	require.True(t, f.reporter.IsSkipped())
	require.Contains(t, f.reporter.Comment(), "not active")
	require.Nil(t, msg)
}

// TestMsgCommitteeUpdatePolicyFactorySkipsAnUnheldAppointment covers the case
// an import produces: a mandate restored from an export naming an address this
// run never held. The factory must decline rather than emit a message no
// account in the run can sign.
func TestMsgCommitteeUpdatePolicyFactorySkipsAnUnheldAppointment(t *testing.T) {
	f := newCommitteeFixture(t, 1)
	mandate := f.appointCommittee(t)
	mandate.Committee = authtypes.NewModuleAddress("nobody-this-run-holds").String()
	require.NoError(t, f.keeper.EconomicMandate.Set(f.ctx, mandate))

	_, msg := simulation.MsgCommitteeUpdatePolicyFactory(f.keeper)(f.ctx, f.testData, f.reporter)

	require.True(t, f.reporter.IsSkipped())
	require.Nil(t, msg)
}

// TestPolicyDrawHonoursAPinnedField pins the corridor helpers directly. A
// mandate may pin a field by setting its bounds equal, and a draw that treated
// an empty corridor as an error — or widened it — would emit a policy the
// handler refuses.
func TestPolicyDrawHonoursAPinnedField(t *testing.T) {
	f := newCommitteeFixture(t, 1)
	pinned := types.DefaultEconomicPolicy()
	mandate := types.NewDisabledEconomicMandate(1)
	mandate.MinimumPolicy = pinned
	mandate.MaximumPolicy = pinned

	for range factorySeeds {
		drawn := simulation.PolicyWithin(f.testData.Rand(), mandate)

		require.Equal(t, pinned, drawn, "a pinned corridor has one admissible value")
	}
}

// A widened corridor draws inside its own bounds on every field it governs.
func TestPolicyDrawStaysWithinAWidenedCorridor(t *testing.T) {
	f := newCommitteeFixture(t, 1)
	mandate := simulation.GenEconomicMandate(f.rand, []string{f.accounts[0].Address.String()})

	for range factorySeeds {
		drawn := simulation.PolicyWithin(f.testData.Rand(), mandate)

		require.NoError(t, drawn.Validate())
		require.NoError(t, mandate.ValidatePolicy(drawn))
		require.True(t, drawn.ValidatorBlockRewardTarget.GTE(mandate.MinimumPolicy.ValidatorBlockRewardTarget))
		require.True(t, drawn.ValidatorBlockRewardTarget.LTE(mandate.MaximumPolicy.ValidatorBlockRewardTarget))
		require.True(t, drawn.LiabilityRatioWeight.GTE(mandate.MinimumPolicy.LiabilityRatioWeight))
		require.True(t, drawn.LiabilityRatioWeight.LTE(mandate.MaximumPolicy.LiabilityRatioWeight))
	}
}

// TestBetweenHelpersReturnTheOnlyValueOfAnInvertedRange holds the guard both
// draw helpers share: a maximum at or below the minimum yields the minimum
// rather than failing, which is what a pinned or malformed corridor produces.
func TestBetweenHelpersReturnTheOnlyValueOfAnInvertedRange(t *testing.T) {
	f := newCommitteeFixture(t, 1)
	r := f.testData.Rand()

	require.Equal(t, math.NewInt(5), simulation.BetweenInt(r, math.NewInt(5), math.NewInt(5)))
	require.Equal(t, math.NewInt(5), simulation.BetweenInt(r, math.NewInt(5), math.NewInt(1)))
	require.Equal(t, math.LegacyOneDec(), simulation.BetweenDec(r, math.LegacyOneDec(), math.LegacyOneDec()))
	require.Equal(t, math.LegacyOneDec(), simulation.BetweenDec(r, math.LegacyOneDec(), math.LegacyZeroDec()))
}
