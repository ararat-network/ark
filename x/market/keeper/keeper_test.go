package keeper_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/core/store"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	codectestutil "github.com/cosmos/cosmos-sdk/codec/testutil"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/std"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	assettypes "github.com/ararat-network/ark/x/asset/types"
	"github.com/ararat-network/ark/x/market/keeper"
	"github.com/ararat-network/ark/x/market/testutil"
	"github.com/ararat-network/ark/x/market/types"
)

type KeeperTestSuite struct {
	suite.Suite

	ctx                   context.Context
	keeper                *keeper.Keeper
	msgServer             types.MsgServer
	queryClient           types.QueryClient
	accountKeeper         *testutil.MockAccountKeeper
	bankKeeper            *testutil.MockBankKeeper
	oracleKeeper          *testutil.MockOracleKeeper
	treasuryKeeper        *testutil.MockTreasuryKeeper
	assetKeeper           *testutil.MockAssetKeeper
	transientStoreService store.TransientStoreService

	// assetStatuses overrides the lifecycle status the asset mock reports per
	// denomination. Denominations default to ACTIVE, which is the state every
	// pre-existing swap test assumes; a test that cares about the gate names
	// only the denomination it is bending.
	assetStatuses map[string]assettypes.AssetStatus
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}

func (s *KeeperTestSuite) SetupTest() {
	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)

	key := storetypes.NewKVStoreKey(types.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("transient_test")
	storeService := runtime.NewKVStoreService(key)
	transientStoreService := runtime.NewTransientStoreService(transientKey)
	testCtx := sdktestutil.DefaultContextWithDB(s.T(), key, transientKey)
	s.ctx = testCtx.Ctx

	ctrl := gomock.NewController(s.T())

	accountKeeper := testutil.NewMockAccountKeeper(ctrl)
	// Committee accounts default to absent, which an appointment records as
	// the shape it observed rather than refusing.
	accountKeeper.EXPECT().GetAccount(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	bankKeeper := testutil.NewMockBankKeeper(ctrl)
	oracleKeeper := testutil.NewMockOracleKeeper(ctrl)
	treasuryKeeper := testutil.NewMockTreasuryKeeper(ctrl)
	assetKeeper := testutil.NewMockAssetKeeper(ctrl)

	// Required by NewKeeper's panic guard
	accountKeeper.EXPECT().GetModuleAddress(types.ModuleName).Return(sdk.AccAddress{1})

	s.keeper = keeper.NewKeeper(
		cdc,
		storeService,
		transientStoreService,
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		accountKeeper,
		bankKeeper,
		oracleKeeper,
		treasuryKeeper,
		assetKeeper,
	)

	s.transientStoreService = transientStoreService
	s.accountKeeper = accountKeeper
	s.bankKeeper = bankKeeper
	s.oracleKeeper = oracleKeeper
	s.treasuryKeeper = treasuryKeeper
	s.assetKeeper = assetKeeper

	s.assetStatuses = map[string]assettypes.AssetStatus{}
	assetKeeper.EXPECT().
		GetAsset(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, denom string) (assettypes.Asset, error) {
			status, overridden := s.assetStatuses[denom]
			if !overridden {
				status = assettypes.AssetStatus_ASSET_STATUS_ACTIVE
			}
			if status == assettypes.AssetStatus_ASSET_STATUS_UNSPECIFIED {
				return assettypes.Asset{}, assettypes.ErrAssetNotFound.Wrap(denom)
			}

			return assettypes.Asset{Denom: denom, Status: status, Version: 1}, nil
		}).
		AnyTimes()

	// Set default state
	s.Require().NoError(s.keeper.Params.Set(s.ctx, types.DefaultParams()))
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, math.LegacyZeroDec()))
	s.Require().NoError(s.keeper.ConversionPolicy.Set(s.ctx, types.DefaultConversionPolicy()))
	s.Require().NoError(s.keeper.ConversionMandate.Set(s.ctx, types.DefaultConversionMandate()))

	// Wire gRPC query client
	queryHelper := baseapp.NewQueryServerTestHelper(testCtx.Ctx, interfaceRegistry)
	types.RegisterQueryServer(queryHelper, keeper.NewQueryServerImpl(s.keeper))
	s.queryClient = types.NewQueryClient(queryHelper)

	// Create message server
	s.msgServer = keeper.NewMsgServerImpl(s.keeper)
}

// conversionTotalsMatcher compares totals by value rather than representation:
// a decimal that survived a store round trip is the same figure whether or not
// its internal words match the literal a test wrote.
type conversionTotalsMatcher struct {
	expected types.ConversionTotals
}

func (m conversionTotalsMatcher) Matches(actual any) bool {
	totals, ok := actual.(types.ConversionTotals)
	if !ok {
		return false
	}

	return totals.GrossOffer.Equal(m.expected.GrossOffer) &&
		totals.EligiblePrincipal.Equal(m.expected.EligiblePrincipal) &&
		totals.RedemptionOutput.Equal(m.expected.RedemptionOutput) &&
		totals.RedeemedValue.Equal(m.expected.RedeemedValue)
}

func (m conversionTotalsMatcher) String() string {
	return fmt.Sprintf(
		"conversion totals {gross %s, principal %s, output %s, redeemed %s}",
		m.expected.GrossOffer,
		m.expected.EligiblePrincipal,
		m.expected.RedemptionOutput,
		m.expected.RedeemedValue,
	)
}

// requireSettledTotals drives the EndBlocker and asserts what Market hands
// Treasury. The accumulators are unexported block-local state, so the settle
// call is where a test observes what the block's conversions actually recorded.
//
// It clears the accumulators afterwards, standing in for the transient store
// reset a real block boundary performs. The suite's context outlives one block,
// so without it a later case would settle its predecessor's conversions too.
func (s *KeeperTestSuite) requireSettledTotals(expected types.ConversionTotals) {
	s.treasuryKeeper.EXPECT().
		SettleConversions(gomock.Any(), conversionTotalsMatcher{expected: expected}).
		Return(math.ZeroInt(), nil)
	s.Require().NoError(s.keeper.EndBlocker(s.ctx))
	s.endBlock()
}

// endBlock clears the block-local conversion accumulators.
func (s *KeeperTestSuite) endBlock() {
	transientStore := s.transientStoreService.OpenTransientStore(s.ctx)
	iterator, err := transientStore.Iterator(nil, nil)
	s.Require().NoError(err)
	var keys [][]byte
	for ; iterator.Valid(); iterator.Next() {
		keys = append(keys, append([]byte(nil), iterator.Key()...))
	}
	s.Require().NoError(iterator.Close())
	for _, key := range keys {
		s.Require().NoError(transientStore.Delete(key))
	}
}

func (s *KeeperTestSuite) TestReplenishPools() {
	tests := []struct {
		name           string
		initialDelta   math.LegacyDec
		recoveryPeriod uint64
		expectedDelta  math.LegacyDec
	}{
		{
			name:           "positive delta converges to zero",
			initialDelta:   math.LegacyNewDec(1000),
			recoveryPeriod: 10,
			// 1000 - 1000/10 = 900
			expectedDelta: math.LegacyNewDec(900),
		},
		{
			name:           "negative delta converges to zero",
			initialDelta:   math.LegacyNewDec(-1000),
			recoveryPeriod: 10,
			// 1000 - 1000/10 = 900
			expectedDelta: math.LegacyNewDec(-900),
		},
		{
			name:           "zero delta stays zero",
			initialDelta:   math.LegacyZeroDec(),
			recoveryPeriod: 10,
			expectedDelta:  math.LegacyZeroDec(),
		},
		{
			name:           "small delta with large recovery period",
			initialDelta:   math.LegacyNewDec(1),
			recoveryPeriod: 100,
			// 1 - 1/100 = 0.99
			expectedDelta: math.LegacyNewDecWithPrec(99, 2),
		},
		{
			name:           "large recovery period - slow convergence",
			initialDelta:   math.LegacyNewDec(14400),
			recoveryPeriod: 14400,
			// 14400 - 14400/14400 = 14399
			expectedDelta: math.LegacyNewDec(14399),
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			err := s.keeper.ArkPoolDelta.Set(s.ctx, tc.initialDelta)
			s.Require().NoError(err)

			capacity := types.DefaultConversionPolicy()
			capacity.PoolRecoveryPeriod = tc.recoveryPeriod
			err = s.keeper.ConversionPolicy.Set(s.ctx, capacity)
			s.Require().NoError(err)

			err = s.keeper.ReplenishPools(s.ctx)
			s.Require().NoError(err)

			delta, err := s.keeper.ArkPoolDelta.Get(s.ctx)
			s.Require().NoError(err)
			s.Require().True(tc.expectedDelta.Equal(delta), "expected %s, got %s", tc.expectedDelta, delta)
		})
	}
}

func (s *KeeperTestSuite) TestReplenishPoolsZeroDeltaSkipsCapacityRead() {
	s.Require().NoError(s.keeper.ConversionPolicy.Remove(s.ctx))
	s.Require().NoError(s.keeper.ArkPoolDelta.Set(s.ctx, math.LegacyZeroDec()))

	s.Require().NoError(s.keeper.ReplenishPools(s.ctx))
}

func (s *KeeperTestSuite) requireTypedEvent(expected proto.Message) {
	expectedEvent, err := sdk.TypedEventToEvent(expected)
	s.Require().NoError(err)
	events := sdk.UnwrapSDKContext(s.ctx).EventManager().Events()
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.Type != expectedEvent.Type {
			continue
		}
		s.Require().Equal(expectedEvent, event)
		parsed, err := sdk.ParseTypedEvent(sdk.Events{event}.ToABCIEvents()[0])
		s.Require().NoError(err)
		roundTripEvent, err := sdk.TypedEventToEvent(parsed)
		s.Require().NoError(err)
		s.Require().Equal(expectedEvent, roundTripEvent)
		return
	}
	s.FailNow("typed event not found", expectedEvent.Type)
}

// countTypedEvents reports how many events of one typed-event message are on
// the context. It is what a no-op guard has to be asserted against: the suite
// shares one event manager across a test, so the question is how many were
// added, not what the whole stream contains.
func (s *KeeperTestSuite) countTypedEvents(message proto.Message) int {
	eventType := proto.MessageName(message)
	count := 0
	for _, event := range sdk.UnwrapSDKContext(s.ctx).EventManager().Events() {
		if event.Type == eventType {
			count++
		}
	}

	return count
}

func (s *KeeperTestSuite) requireTypedEvents(actual sdk.Events, expected ...proto.Message) {
	s.Require().Len(actual, len(expected))
	for i, expectedMessage := range expected {
		expectedEvent, err := sdk.TypedEventToEvent(expectedMessage)
		s.Require().NoError(err)
		s.Require().Equal(expectedEvent, actual[i])
		parsed, err := sdk.ParseTypedEvent(sdk.Events{actual[i]}.ToABCIEvents()[0])
		s.Require().NoError(err)
		roundTripEvent, err := sdk.TypedEventToEvent(parsed)
		s.Require().NoError(err)
		s.Require().Equal(expectedEvent, roundTripEvent)
	}
}
