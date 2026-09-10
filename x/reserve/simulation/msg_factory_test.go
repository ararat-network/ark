package simulation_test

import (
	"context"
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

	"github.com/ararat-network/ark/pkg/chain"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	"github.com/ararat-network/ark/x/reserve/keeper"
	"github.com/ararat-network/ark/x/reserve/simulation"
	"github.com/ararat-network/ark/x/reserve/testutil"
	"github.com/ararat-network/ark/x/reserve/types"
)

// govModuleAccounts is the only account source these factories need: the
// authority they sign as.
type govModuleAccounts struct{}

func (govModuleAccounts) GetModuleAddress(moduleName string) sdk.AccAddress {
	return authtypes.NewModuleAddress(moduleName)
}

type reserveFixture struct {
	ctx       sdk.Context
	keeper    *keeper.Keeper
	msgServer types.MsgServer
	testData  *simsx.ChainDataSource
	reporter  *simsx.BasicSimulationReporter
	accounts  []simtypes.Account
	rand      *rand.Rand
	ctrl      *gomock.Controller
}

// reserveOption seeds fixture state a factory reads before it will emit
// anything. Each is applied once the keeper and the run's accounts exist,
// since an appointment has to name an account the run holds.
type reserveOption func(*testing.T, *reserveFixture)

// newReserveFixture builds a real Reserve keeper over mocks, holding the given
// NOAH balance, so a message the factory emits can be delivered through the
// handler it targets.
func newReserveFixture(t *testing.T, balance int64, opts ...reserveOption) reserveFixture {
	t.Helper()

	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)
	key := storetypes.NewKVStoreKey(types.StoreKey)
	testCtx := sdktestutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("transient_test"))
	ctx := testCtx.Ctx.WithBlockHeight(10)

	ctrl := gomock.NewController(t)
	reserveAddr := authtypes.NewModuleAddress(types.StrategicReserveName)
	accountKeeper := testutil.NewMockAccountKeeper(ctrl)
	accountKeeper.EXPECT().GetAccount(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	accountKeeper.EXPECT().GetModuleAddress(types.StrategicReserveName).Return(reserveAddr).AnyTimes()
	bankKeeper := testutil.NewMockBankKeeper(ctrl)
	bankKeeper.EXPECT().
		GetBalance(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
			if !addr.Equals(reserveAddr) || denom != chain.NoahBaseDenom {
				return sdk.NewCoin(denom, math.ZeroInt())
			}
			return sdk.NewCoin(denom, math.NewInt(balance))
		}).
		AnyTimes()
	bankKeeper.EXPECT().SendCoinsFromModuleToModule(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	bankKeeper.EXPECT().SendCoinsFromModuleToAccount(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	bankKeeper.EXPECT().BurnCoins(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	oracleKeeper := testutil.NewMockOracleKeeper(ctrl)
	oracleKeeper.EXPECT().FeedPhase(gomock.Any(), gomock.Any()).Return(oracletypes.FeedPhaseActive, nil).AnyTimes()

	wasmKeeper := testutil.NewMockWasmKeeper(ctrl)
	wasmKeeper.EXPECT().HasContractInfo(gomock.Any(), gomock.Any()).Return(false).AnyTimes()
	k := keeper.NewKeeper(
		cdc,
		runtime.NewKVStoreService(key),
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		accountKeeper,
		wasmKeeper,
		bankKeeper,
		oracleKeeper,
		testutil.NewMockAssetKeeper(ctrl),
	)
	require.NoError(t, k.Mandate.Set(ctx, types.DefaultReserveMandate()))
	require.NoError(t, k.AllowanceUsed.Set(ctx, math.ZeroInt()))
	// The identifier sequences start where genesis starts them. A position
	// opened against an unset sequence would carry ID zero, which the record's
	// own Validate refuses.
	defaults := types.DefaultGenesisState()
	require.NoError(t, k.NextPositionID.Set(ctx, defaults.NextPositionId))
	require.NoError(t, k.NextEntryID.Set(ctx, defaults.NextEntryId))

	r := rand.New(rand.NewSource(1))
	accounts := simtypes.RandomAccounts(r, 3)
	testData := simsx.NewChainDataSource(
		ctx,
		r,
		govModuleAccounts{},
		nil,
		addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix()),
		accounts...,
	)

	fixture := reserveFixture{
		ctx:       ctx,
		keeper:    k,
		msgServer: keeper.NewMsgServerImpl(k),
		testData:  testData,
		reporter:  simsx.NewBasicSimulationReporter(),
		accounts:  accounts,
		rand:      r,
		ctrl:      ctrl,
	}
	for _, opt := range opts {
		opt(t, &fixture)
	}

	return fixture
}

// addresses renders the run's accounts the way a genesis generator hands them
// to the mandate generators.
func (f reserveFixture) addresses() []string {
	out := make([]string, 0, len(f.accounts))
	for _, account := range f.accounts {
		out = append(out, account.Address.String())
	}

	return out
}

// withActiveCommittee installs the appointment the sim's own genesis generator
// draws, which is what makes the committee surface signable. Going through
// GenReserveMandate rather than a hand-built mandate keeps the tests honest
// about the state a run actually starts from.
func withActiveCommittee() reserveOption {
	return func(t *testing.T, f *reserveFixture) {
		t.Helper()

		mandate := simulation.GenReserveMandate(f.rand, f.addresses())
		require.NoError(t, f.keeper.Mandate.Set(f.ctx, mandate))
	}
}

// withTreasuryRequirement wires the capital requirement the burn surplus is
// measured against. Treasury injects Reserve, so the reader arrives after
// construction and is absent until something sets it.
func withTreasuryRequirement(required math.Int) reserveOption {
	return func(t *testing.T, f *reserveFixture) {
		t.Helper()

		reader := testutil.NewMockTreasuryCapitalReader(f.ctrl)
		reader.EXPECT().RequiredReserveCapital(gomock.Any()).Return(required, nil).AnyTimes()
		f.keeper.SetTreasuryCapitalReader(reader)
	}
}

func TestMsgSetReserveMandateFactory(t *testing.T) {
	f := newReserveFixture(t, 0)

	_, msg := simulation.MsgSetReserveMandateFactory()(f.ctx, f.testData, f.reporter)

	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	require.NotNil(t, msg)
	require.Equal(t, authtypes.NewModuleAddress(govtypes.ModuleName).String(), msg.Authority)
	require.NotEqual(t, msg.Authority, msg.Committee)
	require.Less(t, msg.ActivationHeight, msg.ExpiryHeight)
	require.True(t, msg.DeploymentAllowance.Amount.IsPositive())
	require.NotEmpty(t, msg.Destinations)
	// The factory's whole job is to emit what the handler accepts.
	_, err := f.msgServer.SetReserveMandate(f.ctx, msg)
	require.NoError(t, err)
}

func TestMsgSetRecognitionPolicyFactory(t *testing.T) {
	t.Run("skips without a standing policy", func(t *testing.T) {
		f := newReserveFixture(t, 0)

		_, msg := simulation.MsgSetRecognitionPolicyFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.True(t, f.reporter.IsSkipped())
		require.Contains(t, f.reporter.Comment(), "no standing recognition policy")
		require.Nil(t, msg)
	})

	t.Run("restates the standing policy", func(t *testing.T) {
		f := newReserveFixture(t, 0)
		standing := []string{"axau-lbma", "axag-lbma"}
		for _, denom := range standing {
			require.NoError(t, f.keeper.RecognitionPolicy.Set(f.ctx, denom, types.EligibilityEntry{
				Denom:               denom,
				HaircutFactor:       math.LegacyNewDecWithPrec(5, 1),
				RecognitionCapRatio: math.LegacyNewDecWithPrec(1, 1),
				MaxRateAge:          types.MaxRecognitionRateAge,
			}))
		}

		_, msg := simulation.MsgSetRecognitionPolicyFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.NotNil(t, msg)
		require.Len(t, msg.Entries, len(standing))
		require.NoError(t, types.ValidateRecognitionPolicy(msg.Entries))
		_, err := f.msgServer.SetRecognitionPolicy(f.ctx, msg)
		require.NoError(t, err)
	})
}

func TestFundAndBurnFactories(t *testing.T) {
	t.Run("skip when the reserve is empty", func(t *testing.T) {
		f := newReserveFixture(t, 0)

		_, buffer := simulation.MsgFundBufferFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.True(t, f.reporter.IsSkipped())
		require.Contains(t, f.reporter.Comment(), "retaining a floor")
		require.Nil(t, buffer)
	})

	t.Run("fund the buffer", func(t *testing.T) {
		f := newReserveFixture(t, 1_000_000)

		_, msg := simulation.MsgFundBufferFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.True(t, msg.Amount.Amount.Add(msg.MinimumReserveBalance.Amount).LTE(math.NewInt(1_000_000)))
		_, err := f.msgServer.FundBuffer(f.ctx, msg)
		require.NoError(t, err)
	})

	t.Run("fund insurance", func(t *testing.T) {
		f := newReserveFixture(t, 1_000_000)

		_, msg := simulation.MsgFundInsuranceFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		_, err := f.msgServer.FundInsurance(f.ctx, msg)
		require.NoError(t, err)
	})

	t.Run("burn reserve assets", func(t *testing.T) {
		f := newReserveFixture(t, 1_000_000)

		_, msg := simulation.MsgBurnReserveAssetsFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.False(t, msg.Amounts.Empty())
		_, err := f.msgServer.BurnReserveAssets(f.ctx, msg)
		require.NoError(t, err)
	})
}

// deployPosition drives a committee deployment through the handler, which is
// the only way a position comes into being. The governance factories below all
// act on one, so before the first deployment lands every one of them skips
// every draw — the state they were written for, and the state that hides a
// factory which can never apply.
func deployPosition(t *testing.T, f reserveFixture) *types.MsgCommitteeDeployResponse {
	t.Helper()

	_, msg := simulation.MsgCommitteeDeployFactory(f.keeper)(f.ctx, f.testData, f.reporter)
	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	deployed, err := f.msgServer.CommitteeDeploy(f.ctx, msg)
	require.NoError(t, err)

	return deployed
}

// TestGovernanceLifecycleFactoriesActOnAnOpenPosition is the reachability
// proof for the authority-signed half of the position surface. Each factory
// duplicates a committee power, so what is at stake is not the transition but
// the signer: an authority the handler refuses turns the whole governance
// route into refused proposals a run still reports as successful.
func TestGovernanceLifecycleFactoriesActOnAnOpenPosition(t *testing.T) {
	f := newReserveFixture(t, 1_000_000, withActiveCommittee())
	deployed := deployPosition(t, f)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	_, returned := simulation.MsgCommitteeAttributeReturnFactory(f.keeper)(f.ctx, f.testData, f.reporter)
	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	_, err := f.msgServer.CommitteeAttributeReturn(f.ctx, returned)
	require.NoError(t, err)

	t.Run("mark impaired", func(t *testing.T) {
		_, msg := simulation.MsgMarkImpairedFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.Equal(t, authority, msg.Authority)
		require.Equal(t, deployed.PositionId, msg.PositionId)
		_, err := f.msgServer.MarkImpaired(f.ctx, msg)
		require.NoError(t, err)
	})

	t.Run("clear impairment", func(t *testing.T) {
		_, msg := simulation.MsgClearImpairmentFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.Equal(t, authority, msg.Authority)
		_, err := f.msgServer.ClearImpairment(f.ctx, msg)
		require.NoError(t, err)
	})

	t.Run("correct position", func(t *testing.T) {
		_, msg := simulation.MsgCorrectPositionFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.Equal(t, authority, msg.Authority)
		require.Equal(t, deployed.EntryId, msg.Corrects)
		_, err := f.msgServer.CorrectPosition(f.ctx, msg)
		require.NoError(t, err)
	})

	t.Run("reverse return", func(t *testing.T) {
		_, msg := simulation.MsgReverseReturnFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.Equal(t, authority, msg.Authority)
		require.NotZero(t, msg.Reverses)
		_, err := f.msgServer.ReverseReturn(f.ctx, msg)
		require.NoError(t, err)
	})

	t.Run("close position", func(t *testing.T) {
		_, msg := simulation.MsgClosePositionFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.Equal(t, authority, msg.Authority)
		require.Equal(t, deployed.PositionId, msg.PositionId)
		_, err := f.msgServer.ClosePosition(f.ctx, msg)
		require.NoError(t, err)
	})
}

// TestGovernanceLifecycleFactoriesSkipWithoutAPosition holds the other side:
// these are reached from the first block of a run, long before a deployment
// has opened anything, and must decline rather than emit a message naming a
// position that does not exist.
func TestGovernanceLifecycleFactoriesSkipWithoutAPosition(t *testing.T) {
	tests := map[string]struct {
		emit   func(f reserveFixture) any
		reason string
	}{
		"mark impaired": {
			emit: func(f reserveFixture) any {
				_, msg := simulation.MsgMarkImpairedFactory(f.keeper)(f.ctx, f.testData, f.reporter)

				return msg
			},
			reason: "no sound position to impair",
		},
		"clear impairment": {
			emit: func(f reserveFixture) any {
				_, msg := simulation.MsgClearImpairmentFactory(f.keeper)(f.ctx, f.testData, f.reporter)

				return msg
			},
			reason: "no impaired position to clear",
		},
		"close position": {
			emit: func(f reserveFixture) any {
				_, msg := simulation.MsgClosePositionFactory(f.keeper)(f.ctx, f.testData, f.reporter)

				return msg
			},
			reason: "no open position to close",
		},
		"correct position": {
			emit: func(f reserveFixture) any {
				_, msg := simulation.MsgCorrectPositionFactory(f.keeper)(f.ctx, f.testData, f.reporter)

				return msg
			},
			reason: "no deployment entry on an open position to correct",
		},
		"reverse return": {
			emit: func(f reserveFixture) any {
				_, msg := simulation.MsgReverseReturnFactory(f.keeper)(f.ctx, f.testData, f.reporter)

				return msg
			},
			reason: "no unreversed return attribution on an open position",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			f := newReserveFixture(t, 1_000_000)

			msg := tc.emit(f)

			require.True(t, f.reporter.IsSkipped())
			require.Contains(t, f.reporter.Comment(), tc.reason)
			require.Nil(t, msg)
		})
	}
}
