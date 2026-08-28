package keeper_test

import (
	"context"
	"testing"

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
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"github.com/ararat-network/ark/pkg/chain"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	"github.com/ararat-network/ark/x/reserve/keeper"
	"github.com/ararat-network/ark/x/reserve/testutil"
	"github.com/ararat-network/ark/x/reserve/types"
)

type KeeperTestSuite struct {
	suite.Suite

	ctx            context.Context
	cdc            codec.Codec
	keeper         *keeper.Keeper
	msgServer      types.MsgServer
	queryServer    types.QueryServer
	authority      string
	accountKeeper  *testutil.MockAccountKeeper
	bankKeeper     *testutil.MockBankKeeper
	oracleKeeper   *testutil.MockOracleKeeper
	assetKeeper    *testutil.MockAssetKeeper
	treasuryReader *stubTreasuryCapital
	// reserveBalance is the NOAH Bank reports for the Reserve account, and
	// reserveAssets the non-NOAH custody beside it. They are suite fixtures
	// because every Reserve path reads them and what a test varies is the
	// capacity they imply, not the fact of the read.
	reserveBalance math.Int
	reserveAssets  sdk.Coins
	// registeredAssets is the asset registry as the Reserve sees it. It is
	// empty by default so the membership refusal is something a test opts into
	// rather than something every policy change works around.
	registeredAssets map[string]bool
	// feedPhases overrides the phase the oracle mock reports per feed. Feeds
	// default to Active, because listing an external symbol requires its series
	// to be an active feed and that check is not what most of these tests are
	// about.
	feedPhases map[string]oracletypes.FeedPhase
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}

func (s *KeeperTestSuite) SetupTest() {
	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	s.cdc = codec.NewProtoCodec(interfaceRegistry)

	key := storetypes.NewKVStoreKey(types.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("transient_test")
	testCtx := sdktestutil.DefaultContextWithDB(s.T(), key, transientKey)
	s.ctx = testCtx.Ctx
	s.authority = authtypes.NewModuleAddress(govtypes.ModuleName).String()

	ctrl := gomock.NewController(s.T())
	s.accountKeeper = testutil.NewMockAccountKeeper(ctrl)
	// Committee accounts default to absent, which an appointment records as
	// the shape it observed rather than refusing.
	s.accountKeeper.EXPECT().GetAccount(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	s.bankKeeper = testutil.NewMockBankKeeper(ctrl)
	s.oracleKeeper = testutil.NewMockOracleKeeper(ctrl)
	s.assetKeeper = testutil.NewMockAssetKeeper(ctrl)
	s.reserveBalance = math.ZeroInt()
	s.reserveAssets = sdk.NewCoins()

	s.registeredAssets = map[string]bool{}
	s.assetKeeper.EXPECT().
		HasAsset(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, denom string) (bool, error) {
			return s.registeredAssets[denom], nil
		}).
		AnyTimes()

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

	reserveAddr := authtypes.NewModuleAddress(types.StrategicReserveName)
	s.accountKeeper.EXPECT().
		GetModuleAddress(types.StrategicReserveName).
		Return(reserveAddr).
		AnyTimes()
	s.accountKeeper.EXPECT().
		GetModuleAccount(gomock.Any(), types.StrategicReserveName).
		Return(authtypes.NewEmptyModuleAccount(types.StrategicReserveName)).
		AnyTimes()
	s.bankKeeper.EXPECT().
		GetBalance(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
			if !addr.Equals(reserveAddr) {
				return sdk.NewCoin(denom, math.ZeroInt())
			}
			if denom == chain.NoahBaseDenom {
				return sdk.NewCoin(denom, s.reserveBalance)
			}
			return sdk.NewCoin(denom, s.reserveAssets.AmountOf(denom))
		}).
		AnyTimes()
	s.bankKeeper.EXPECT().
		GetAllBalances(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, addr sdk.AccAddress) sdk.Coins {
			if !addr.Equals(reserveAddr) {
				return sdk.NewCoins()
			}
			return s.reserveAssets.Add(chain.NoahCoins(s.reserveBalance)...)
		}).
		AnyTimes()

	s.keeper = keeper.NewKeeper(
		s.cdc,
		runtime.NewKVStoreService(key),
		s.authority,
		s.accountKeeper,
		s.bankKeeper,
		s.oracleKeeper,
		s.assetKeeper,
	)
	// The requirement reader is a stub rather than a mock: what tests vary is
	// the number Treasury would answer with — or its refusal under incomplete
	// valuation — not the shape of the call.
	s.treasuryReader = &stubTreasuryCapital{
		required:           math.ZeroInt(),
		bufferShortfall:    math.ZeroInt(),
		insuranceShortfall: math.ZeroInt(),
	}
	s.keeper.SetTreasuryCapitalReader(s.treasuryReader)
	s.Require().NoError(s.keeper.Mandate.Set(s.ctx, types.DefaultReserveMandate()))
	s.Require().NoError(s.keeper.AllowanceUsed.Set(s.ctx, math.ZeroInt()))
	// Identifier sequences hold the next ID to issue and are one-based, so a
	// suite that bypasses InitGenesis has to seed them: an unset sequence reads
	// as zero, which is the "none" sentinel and no valid identifier.
	s.Require().NoError(s.keeper.NextPositionID.Set(s.ctx, 1))
	s.Require().NoError(s.keeper.NextEntryID.Set(s.ctx, 1))
	s.msgServer = keeper.NewMsgServerImpl(s.keeper)
	s.queryServer = keeper.NewQueryServerImpl(s.keeper)
}

// stubTreasuryCapital stands in for Treasury's side of the capital contract:
// the Reserve's own requirement, and how far each fund a committee transfer
// may top up falls below its target.
type stubTreasuryCapital struct {
	required           math.Int
	bufferShortfall    math.Int
	insuranceShortfall math.Int
	// err fails every read: all three figures fold the same registry, so a
	// store or state fault reaches all three together.
	err error
	// requiredErr fails only the burn bound, which is how Treasury behaves
	// under an incomplete valuation. The requirement is withheld because
	// understating it would overstate the surplus a burn may destroy, while
	// both transfer bounds keep answering on the claimable aggregate.
	requiredErr error
}

func (s *stubTreasuryCapital) RequiredReserveCapital(_ context.Context) (math.Int, error) {
	if s.requiredErr != nil {
		return math.Int{}, s.requiredErr
	}
	return s.answer(s.required)
}

func (s *stubTreasuryCapital) RedemptionBufferShortfall(_ context.Context) (math.Int, error) {
	return s.answer(s.bufferShortfall)
}

func (s *stubTreasuryCapital) InsuranceShortfall(_ context.Context) (math.Int, error) {
	return s.answer(s.insuranceShortfall)
}

func (s *stubTreasuryCapital) answer(figure math.Int) (math.Int, error) {
	if s.err != nil {
		return math.Int{}, s.err
	}
	return figure, nil
}

// fundReserve sets the NOAH Bank reports for the Reserve account.
func (s *KeeperTestSuite) fundReserve(amount int64) {
	s.reserveBalance = math.NewInt(amount)
}

// registerAsset marks denom as an Ark-issued registry member.
func (s *KeeperTestSuite) registerAsset(denom string) {
	s.registeredAssets[denom] = true
}

// attest records external custody the way the chain can actually reach it: as
// an open position. No external symbol can enter the Reserve's bank balance,
// so seeding the account with one would exercise the fold through a door that
// does not exist. The cost basis is a nominal one anoah: Validate only
// requires it positive, and no fold reads it.
func (s *KeeperTestSuite) attest(denom string, amount int64) {
	s.attestQuantity(denom, math.NewInt(amount))
}

// attestQuantity is attest for a holding beyond int64, which is the range the
// recognition fold's arithmetic bounds are stated in.
func (s *KeeperTestSuite) attestQuantity(denom string, amount math.Int) uint64 {
	positionID, err := s.keeper.NextPositionID.Peek(s.ctx)
	s.Require().NoError(err)
	position := types.Position{
		PositionId:     positionID,
		Quantity:       sdk.NewCoin(denom, amount),
		Deployed:       noahCoin(1),
		Returned:       noahCoin(0),
		VenueReference: "custodian-alpha",
		OpenedHeight:   1,
	}
	s.Require().NoError(position.Validate())
	s.Require().NoError(s.keeper.NextPositionID.Set(s.ctx, positionID+1))
	s.Require().NoError(s.keeper.OpenPositions.Set(s.ctx, positionID, position))
	return positionID
}

// fundReserveAsset sets a non-NOAH balance Bank reports for the Reserve
// account, replacing any prior amount of the same denomination. Only NOAH and
// registry members belong here; external custody goes through attest.
func (s *KeeperTestSuite) fundReserveAsset(denom string, amount int64) {
	kept := sdk.NewCoins()
	for _, coin := range s.reserveAssets {
		if coin.Denom != denom {
			kept = kept.Add(coin)
		}
	}
	s.reserveAssets = kept.Add(sdk.NewInt64Coin(denom, amount))
}
