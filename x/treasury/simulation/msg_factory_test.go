package simulation_test

import (
	"context"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/testutil/simsx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/treasury/simulation"
	"github.com/ararat-network/ark/x/treasury/testutil"
	"github.com/ararat-network/ark/x/treasury/types"
)

// factorySeeds is how many draws each factory is put through. The payload is
// randomised, so one seed proves only that one draw validates; the guarantee
// worth holding is that no draw produces a message the handler would refuse.
const factorySeeds = 200

// TestMsgUpdateParamsFactoryProducesValidMessages holds the invariant a
// simulation rests on: every generated message must pass the same validation
// its handler applies. A factory that drifts out of the domain turns a sim run
// into a stream of refused messages that still reports success.
func TestMsgUpdateParamsFactoryProducesValidMessages(t *testing.T) {
	gov := authtypes.NewModuleAddress(govtypes.ModuleName)

	for seed := int64(0); seed < factorySeeds; seed++ {
		reporter, testData := newFactoryFixture(t, seed, gov)

		_, msg := simulation.MsgUpdateParamsFactory()(context.Background(), testData, reporter)

		require.False(t, reporter.IsSkipped(), "seed %d: %s", seed, reporter.Comment())
		require.NotNil(t, msg)
		require.Equal(t, gov.String(), msg.Authority)
		require.NoError(t, msg.Params.Validate(), "seed %d", seed)
	}
}

// TestMsgUpdateParamsFactorySkipsWithoutGovAccount covers the one branch the
// factory does not own: an unresolvable module account skips the operation and
// leaves the authority empty rather than emitting an unsigned message.
func TestMsgUpdateParamsFactorySkipsWithoutGovAccount(t *testing.T) {
	reporter, testData := newFactoryFixture(t, 1, nil)

	_, msg := simulation.MsgUpdateParamsFactory()(context.Background(), testData, reporter)

	require.True(t, reporter.IsSkipped())
	require.Contains(t, reporter.Comment(), "unknown module account")
	require.NotNil(t, msg)
	require.Empty(t, msg.Authority)
}

// newFactoryFixture builds the chain data source the factories read. A nil gov
// address stands for a module account the app never registered.
func newFactoryFixture(
	t *testing.T,
	seed int64,
	gov sdk.AccAddress,
) (simsx.SimulationReporter, *simsx.ChainDataSource) {
	t.Helper()

	accountKeeper := testutil.NewMockAccountKeeper(gomock.NewController(t))
	accountKeeper.EXPECT().GetModuleAddress(govtypes.ModuleName).Return(gov).AnyTimes()

	r := rand.New(rand.NewSource(seed))
	testData := simsx.NewChainDataSource(
		context.Background(),
		r,
		accountKeeper,
		nil,
		addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix()),
		simtypes.RandomAccounts(r, 1)...,
	)

	return simsx.NewBasicSimulationReporter(), testData
}

// TestMsgSetEconomicMandateFactory pins the appointment the factory emits.
func TestMsgSetEconomicMandateFactory(t *testing.T) {
	reporter, testData := newFactoryFixture(t, 1, authtypes.NewModuleAddress(govtypes.ModuleName))

	_, msg := simulation.MsgSetEconomicMandateFactory()(context.Background(), testData, reporter)

	require.False(t, reporter.IsSkipped(), reporter.Comment())
	require.NotNil(t, msg)
	require.Equal(t, authtypes.NewModuleAddress(govtypes.ModuleName).String(), msg.Authority)
	require.NotEqual(t, msg.Authority, msg.Committee)
	require.Less(t, msg.ActivationHeight, msg.ExpiryHeight)
	// A configured mandate must carry bounds that validate as a corridor.
	mandate := types.NewDisabledEconomicMandate(1)
	mandate.Committee = msg.Committee
	mandate.ActivationHeight = msg.ActivationHeight
	mandate.ExpiryHeight = msg.ExpiryHeight
	mandate.MinimumPolicy = msg.MinimumPolicy
	mandate.MaximumPolicy = msg.MaximumPolicy
	require.NoError(t, mandate.Validate())
}

// TestMsgUpdatePolicyFactory pins the policy the factory emits, which the
// handler accepts on its own validation alone.
func TestMsgUpdatePolicyFactory(t *testing.T) {
	reporter, testData := newFactoryFixture(t, 1, authtypes.NewModuleAddress(govtypes.ModuleName))

	_, msg := simulation.MsgUpdatePolicyFactory()(context.Background(), testData, reporter)

	require.False(t, reporter.IsSkipped(), reporter.Comment())
	require.NotNil(t, msg)
	require.Equal(t, authtypes.NewModuleAddress(govtypes.ModuleName).String(), msg.Authority)
	require.NoError(t, msg.Policy.Validate())
}

// TestMsgReturnSubsidyFactory pins the skip on an empty pool and the message
// the factory emits otherwise: gov as authority, a positive NOAH amount within
// the balance it read, and a zero minimum.
func TestMsgReturnSubsidyFactory(t *testing.T) {
	fixture := newCommitteeFixture(t, 1)
	balance := math.ZeroInt()
	fixture.bankKeeper.EXPECT().
		GetBalance(gomock.Any(), authtypes.NewModuleAddress(types.SubsidyPoolName), chain.NoahBaseDenom).
		DoAndReturn(func(context.Context, sdk.AccAddress, string) sdk.Coin { return chain.NoahCoin(balance) }).
		AnyTimes()
	factory := simulation.MsgReturnSubsidyFactory(fixture.keeper)

	_, msg := factory(fixture.ctx, fixture.testData, fixture.reporter)
	require.True(t, fixture.reporter.IsSkipped(), "an empty pool skips")
	require.Nil(t, msg)

	balance = math.NewInt(1_000)
	reporter := simsx.NewBasicSimulationReporter()
	_, msg = factory(fixture.ctx, fixture.testData, reporter)
	require.False(t, reporter.IsSkipped(), reporter.Comment())
	require.NotNil(t, msg)
	require.Equal(t, authtypes.NewModuleAddress(govtypes.ModuleName).String(), msg.Authority)
	require.Equal(t, chain.NoahBaseDenom, msg.Amount.Denom)
	require.True(t, msg.Amount.Amount.IsPositive() && msg.Amount.Amount.LTE(balance), msg.Amount)
	require.True(t, msg.MinimumSubsidyBalance.IsZero())
}
