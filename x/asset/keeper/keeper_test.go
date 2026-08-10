package keeper_test

import (
	"context"
	"testing"

	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	codectestutil "github.com/cosmos/cosmos-sdk/codec/testutil"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/std"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"ark/x/asset/keeper"
	"ark/x/asset/testutil"
	"ark/x/asset/types"
	oracletypes "ark/x/oracle/types"
)

type KeeperTestSuite struct {
	suite.Suite

	ctx           context.Context
	keeper        *keeper.Keeper
	accountKeeper *testutil.MockAccountKeeper
	bankKeeper    *testutil.MockBankKeeper
	oracleKeeper  *testutil.MockOracleKeeper

	// feedPhases overrides the phase the oracle mock reports per feed. Feeds
	// default to Active, which is the launch state for every seeded asset.
	feedPhases map[string]oracletypes.FeedPhase
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
	storeService := runtime.NewKVStoreService(key)
	testCtx := sdktestutil.DefaultContextWithDB(
		s.T(),
		key,
		storetypes.NewTransientStoreKey("transient_test"),
	)
	s.ctx = testCtx.Ctx

	ctrl := gomock.NewController(s.T())
	s.accountKeeper = testutil.NewMockAccountKeeper(ctrl)
	s.bankKeeper = testutil.NewMockBankKeeper(ctrl)
	s.oracleKeeper = testutil.NewMockOracleKeeper(ctrl)
	// Committee accounts default to absent, which an appointment records as the
	// shape it observed rather than refusing.
	s.accountKeeper.EXPECT().GetAccount(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	s.keeper = keeper.NewKeeper(
		cdc,
		storeService,
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		s.accountKeeper,
		s.bankKeeper,
		s.oracleKeeper,
	)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, types.DefaultParams()))

	s.feedPhases = map[string]oracletypes.FeedPhase{}
	s.oracleKeeper.EXPECT().
		FeedPhase(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, denom string) (oracletypes.FeedPhase, error) {
			if phase, overridden := s.feedPhases[denom]; overridden {
				return phase, nil
			}

			return oracletypes.FeedPhaseActive, nil
		}).
		AnyTimes()
}

func (s *KeeperTestSuite) TestCollectionsPersistState() {
	asset := types.DefaultGenesisState().Assets[0]
	s.Require().NoError(s.keeper.Assets.Set(s.ctx, asset.Denom, asset))

	storedAsset, err := s.keeper.Assets.Get(s.ctx, asset.Denom)
	s.Require().NoError(err)
	s.Require().Equal(asset, storedAsset)

	settlementPlan := types.SettlementPlan{
		Denom:                 asset.Denom,
		RedemptionRate:        math.LegacyOneDec(),
		ActivationHeight:      10,
		EarliestClosingHeight: 110,
		OpenedHeight:          1,
	}
	s.Require().NoError(s.keeper.SettlementPlans.Set(
		s.ctx,
		asset.Denom,
		settlementPlan,
	))

	storedSettlementPlan, err := s.keeper.SettlementPlans.Get(s.ctx, asset.Denom)
	s.Require().NoError(err)
	s.Require().Equal(settlementPlan, storedSettlementPlan)

	resolutionRecord := types.ResolutionRecord{
		Denom:             asset.Denom,
		Version:           3,
		ResolutionHeight:  20,
		OutstandingSupply: sdk.NewInt64Coin(asset.Denom, 100),
		SettlementPlan:    &settlementPlan,
	}
	writeOffKey := resolutionRecord.Key()
	s.Require().NoError(s.keeper.ResolutionRecords.Set(
		s.ctx,
		writeOffKey,
		resolutionRecord,
	))

	storedResolutionRecord, err := s.keeper.ResolutionRecords.Get(s.ctx, writeOffKey)
	s.Require().NoError(err)
	s.Require().Equal(resolutionRecord, storedResolutionRecord)
}

// expectFreshDenom expects the registration preconditions for an unused
// denomination: no Bank supply and no pre-existing Bank metadata.
func (s *KeeperTestSuite) expectFreshDenom(denom string) {
	s.bankKeeper.EXPECT().
		GetSupply(s.ctx, denom).
		Return(zeroAssetCoin(denom))
	s.bankKeeper.EXPECT().
		GetDenomMetaData(s.ctx, denom).
		Return(banktypes.Metadata{}, false)
}

func (s *KeeperTestSuite) requireTypedEvents(
	actual sdk.Events,
	expected ...proto.Message,
) {
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
