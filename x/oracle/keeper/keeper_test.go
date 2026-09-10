package keeper_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	codectestutil "github.com/cosmos/cosmos-sdk/codec/testutil"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/std"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/oracle/keeper"
	"github.com/ararat-network/ark/x/oracle/testutil"
	"github.com/ararat-network/ark/x/oracle/types"
)

type KeeperTestSuite struct {
	suite.Suite

	ctx           context.Context
	keeper        *keeper.Keeper
	msgServer     types.MsgServer
	queryClient   types.QueryClient
	accountKeeper *testutil.MockAccountKeeper
	bankKeeper    *testutil.MockBankKeeper
	distrKeeper   *testutil.MockDistributionKeeper
	stakingKeeper *testutil.MockStakingKeeper

	marketReferenceDenom   *testutil.MockMarketReferenceDenomKeeper
	treasuryReferenceDenom *testutil.MockTreasuryReferenceDenomKeeper
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}

func TestNewKeeperRequiresDistributionModuleAccount(t *testing.T) {
	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)
	storeService := runtime.NewKVStoreService(storetypes.NewKVStoreKey(types.StoreKey))

	ctrl := gomock.NewController(t)
	accountKeeper := testutil.NewMockAccountKeeper(ctrl)
	accountKeeper.EXPECT().GetModuleAddress(types.ModuleName).Return(sdk.AccAddress{1})
	accountKeeper.EXPECT().GetModuleAddress("distribution").Return(nil)

	requirePanic := func() {
		keeper.NewKeeper(
			cdc,
			storeService,
			authtypes.NewModuleAddress(govtypes.ModuleName).String(),
			"distribution",
			accountKeeper,
			nil,
			nil,
			nil,
		)
	}

	require.PanicsWithValue(t, "distribution module account has not been set", requirePanic)
}

func (s *KeeperTestSuite) SetupTest() {
	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)

	key := storetypes.NewKVStoreKey(types.StoreKey)
	storeService := runtime.NewKVStoreService(key)
	testCtx := sdktestutil.DefaultContextWithDB(s.T(), key, storetypes.NewTransientStoreKey("transient_test"))
	s.ctx = sdk.UnwrapSDKContext(testCtx.Ctx).
		WithBlockTime(oracleTestBlockTime).
		WithBlockHeight(oracleTestGenesisHeight)

	ctrl := gomock.NewController(s.T())

	s.accountKeeper = testutil.NewMockAccountKeeper(ctrl)
	s.bankKeeper = testutil.NewMockBankKeeper(ctrl)
	s.distrKeeper = testutil.NewMockDistributionKeeper(ctrl)
	s.stakingKeeper = testutil.NewMockStakingKeeper(ctrl)
	s.marketReferenceDenom = testutil.NewMockMarketReferenceDenomKeeper(ctrl)
	s.treasuryReferenceDenom = testutil.NewMockTreasuryReferenceDenomKeeper(ctrl)

	s.accountKeeper.EXPECT().GetModuleAddress(types.ModuleName).Return(sdk.AccAddress{1})
	s.accountKeeper.EXPECT().GetModuleAddress("distribution").Return(sdk.AccAddress{2})

	s.keeper = keeper.NewKeeper(
		cdc,
		storeService,
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		"distribution",
		s.accountKeeper,
		s.bankKeeper,
		s.distrKeeper,
		s.stakingKeeper,
	)

	s.keeper.SetReferenceDenomConsumers(s.marketReferenceDenom, s.treasuryReferenceDenom)

	params := types.DefaultParams()
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.Require().NoError(s.keeper.Accounting.Set(s.ctx, types.NewAccounting(params)))

	// Mirror the launch feed set genesis seeds.
	s.Require().NoError(s.keeper.Feeds.Set(s.ctx, types.DefaultFeeds()))

	queryHelper := baseapp.NewQueryServerTestHelper(sdk.UnwrapSDKContext(s.ctx), interfaceRegistry)
	types.RegisterQueryServer(queryHelper, keeper.NewQueryServerImpl(s.keeper))
	s.queryClient = types.NewQueryClient(queryHelper)

	s.msgServer = keeper.NewMsgServerImpl(s.keeper)
}

func (s *KeeperTestSuite) SetupSubTest() {
	s.SetupTest()
}

var (
	valAddr1            = sdk.ValAddress([]byte("validator1___________"))
	valAddr2            = sdk.ValAddress([]byte("validator2___________"))
	valAddr3            = sdk.ValAddress([]byte("validator3___________"))
	oracleTestBlockTime = time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
)

// oracleTestGenesisHeight is the suite's default block height. It is non-zero
// so that height-relative assertions — accounting anchors especially — can
// distinguish "at the current height" from "unset".
const oracleTestGenesisHeight int64 = 20

// newBondedValidator builds a bonded validator fixture with operator address
// valAddr and returns it alongside its derived consensus address. Callers
// that need a different bond status or jailed flag mutate the returned
// (local, by-value) validator before arming a mock expectation with it.
func (s *KeeperTestSuite) newBondedValidator(valAddr sdk.ValAddress) (sdk.ConsAddress, stakingtypes.Validator) {
	s.T().Helper()

	pubKey := ed25519.GenPrivKey().PubKey()
	validator, err := stakingtypes.NewValidator(valAddr.String(), pubKey, stakingtypes.Description{})
	s.Require().NoError(err)
	validator.Status = stakingtypes.Bonded
	consAddr, err := validator.GetConsAddr()
	s.Require().NoError(err)

	return consAddr, validator
}

func newStoredExchangeRate(denom string, rate math.LegacyDec) types.ExchangeRate {
	return types.ExchangeRate{Denom: denom, Rate: rate, BlockTimestamp: oracleTestBlockTime}
}

func (s *KeeperTestSuite) TestGetExchangeRate() {
	tests := []struct {
		name      string
		setup     func()
		denom     string
		expected  math.LegacyDec
		expectErr bool
	}{
		{
			name:     "noah denom returns one",
			denom:    chain.NoahBaseDenom,
			expected: math.LegacyOneDec(),
		},
		{
			name: "known denom returns stored rate",
			setup: func() {
				s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.USDBaseDenom, newStoredExchangeRate(chain.USDBaseDenom, math.LegacyNewDecWithPrec(123, 2))))
			},
			denom:    chain.USDBaseDenom,
			expected: math.LegacyNewDecWithPrec(123, 2),
		},
		{
			name:      "unknown denom returns error",
			denom:     "afoo",
			expectErr: true,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			if tc.setup != nil {
				tc.setup()
			}

			rate, err := s.keeper.GetExchangeRate(s.ctx, tc.denom)
			if tc.expectErr {
				s.Require().Error(err)
				s.Require().ErrorContains(err, types.ErrUnknownDenom.Error())
				return
			}

			s.Require().NoError(err)
			s.Require().True(tc.expected.Equal(rate), "expected %s, got %s", tc.expected, rate)
		})
	}
}

func (s *KeeperTestSuite) TestGetRateSet() {
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.USDBaseDenom, types.ExchangeRate{
		Denom:          chain.USDBaseDenom,
		Rate:           math.LegacyOneDec(),
		BlockTimestamp: oracleTestBlockTime.Add(-30 * time.Second),
	}))
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.XDRBaseDenom, types.ExchangeRate{
		Denom:          chain.XDRBaseDenom,
		Rate:           math.LegacyMustNewDecFromStr("1.7"),
		BlockTimestamp: oracleTestBlockTime.Add(-30 * time.Second),
	}))

	rates, err := s.keeper.GetRateSet(
		s.ctx,
		chain.USDBaseDenom,
		chain.XDRBaseDenom,
		chain.USDBaseDenom,
	)
	s.Require().NoError(err)
	s.Require().Len(rates, 3)
	s.Require().True(rates[chain.USDBaseDenom].Equal(math.LegacyOneDec()))
	s.Require().True(rates[chain.XDRBaseDenom].Equal(math.LegacyMustNewDecFromStr("1.7")))
	s.Require().True(rates[chain.NoahBaseDenom].Equal(math.LegacyOneDec()))
}

func (s *KeeperTestSuite) TestGetRateSetReturnsNoahIdentityByDefault() {
	rates, err := s.keeper.GetRateSet(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(types.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
	}, rates)
}

func (s *KeeperTestSuite) TestGetAvailableRateSetOmitsUnknownAndStaleDenoms() {
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.MaxExchangeRateAge = time.Minute
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.USDBaseDenom, types.ExchangeRate{
		Denom:          chain.USDBaseDenom,
		Rate:           math.LegacyMustNewDecFromStr("1.3"),
		BlockTimestamp: oracleTestBlockTime.Add(-30 * time.Second),
	}))
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.XDRBaseDenom, types.ExchangeRate{
		Denom:          chain.XDRBaseDenom,
		Rate:           math.LegacyOneDec(),
		BlockTimestamp: oracleTestBlockTime.Add(-time.Minute - time.Second),
	}))

	// axdr is stale and akrw was never priced: both are omitted rather than
	// failing the set, while the fresh rate and the NOAH identity survive.
	rates, err := s.keeper.GetAvailableRateSet(
		s.ctx,
		chain.USDBaseDenom,
		chain.XDRBaseDenom,
		chain.KRWBaseDenom,
	)
	s.Require().NoError(err)
	s.Require().Equal(types.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom:  math.LegacyMustNewDecFromStr("1.3"),
	}, rates)
}

// TestGetRateSetWithinJudgesOneFeedPerRequest checks derived feeds and independent caller windows,
// with results keyed by requested symbols. Ordinary reads still enforce the chain default.
func (s *KeeperTestSuite) TestGetRateSetWithinJudgesOneFeedPerRequest() {
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.MaxExchangeRateAge = time.Minute
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.XDRBaseDenom, types.ExchangeRate{
		Denom:          chain.XDRBaseDenom,
		Rate:           math.LegacyOneDec(),
		BlockTimestamp: oracleTestBlockTime.Add(-2 * time.Hour),
	}))

	rates, err := s.keeper.GetRateSetWithin(s.ctx, []types.RateRequest{
		// The series is derived from the requested name — its prefix for an
		// external symbol, itself for a bare feed key — so a request cannot
		// route a name to any series but its own.
		{Denom: "axdr-patient", MaxAge: 26 * time.Hour},
		{Denom: "axdr-strict", MaxAge: time.Hour},
		{Denom: chain.XDRBaseDenom, MaxAge: 26 * time.Hour},
		// A feed never priced is omitted rather than zero: a judged read is
		// not licence to invent a rate where none was stored.
		{Denom: "ausd-x", MaxAge: 26 * time.Hour},
		// A non-positive window admits nothing rather than erroring: this read
		// sits behind arithmetic that settles every block.
		{Denom: "axdr-zero2", MaxAge: 0},
	})
	s.Require().NoError(err)
	s.Require().Equal(types.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		"axdr-patient":      math.LegacyOneDec(),
		chain.XDRBaseDenom:  math.LegacyOneDec(),
	}, rates)

	// Requested denominations must be unique, or two verdicts would silently
	// collapse into one.
	_, err = s.keeper.GetRateSetWithin(s.ctx, []types.RateRequest{
		{Denom: "axdr-x", MaxAge: time.Hour},
		{Denom: "axdr-x", MaxAge: 26 * time.Hour},
	})
	s.Require().ErrorContains(err, "duplicate rate request")

	available, err := s.keeper.GetAvailableRateSet(s.ctx, chain.XDRBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(types.RateSet{chain.NoahBaseDenom: math.LegacyOneDec()}, available)
	_, err = s.keeper.GetExchangeRate(s.ctx, chain.XDRBaseDenom)
	s.Require().ErrorIs(err, types.ErrStaleExchangeRate)
}

// TestGetLastKnownRateSetIgnoresStalenessButNotAbsence checks stale observations remain available
// for outstanding-supply accounting without inventing never-observed rates.
func (s *KeeperTestSuite) TestGetLastKnownRateSetIgnoresStalenessButNotAbsence() {
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.MaxExchangeRateAge = time.Minute
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.USDBaseDenom, types.ExchangeRate{
		Denom:          chain.USDBaseDenom,
		Rate:           math.LegacyMustNewDecFromStr("1.3"),
		BlockTimestamp: oracleTestBlockTime.Add(-30 * time.Second),
	}))
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.XDRBaseDenom, types.ExchangeRate{
		Denom:          chain.XDRBaseDenom,
		Rate:           math.LegacyOneDec(),
		BlockTimestamp: oracleTestBlockTime.Add(-24 * time.Hour),
	}))

	rates, err := s.keeper.GetLastKnownRateSet(
		s.ctx,
		chain.USDBaseDenom,
		chain.XDRBaseDenom,
		chain.KRWBaseDenom,
	)
	s.Require().NoError(err)
	// axdr is a day stale and comes back anyway; akrw was never priced and does
	// not. The same call through GetAvailableRateSet omits axdr, which is what
	// makes the two reads answer different questions.
	s.Require().Equal(types.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom:  math.LegacyMustNewDecFromStr("1.3"),
		chain.XDRBaseDenom:  math.LegacyOneDec(),
	}, rates)

	available, err := s.keeper.GetAvailableRateSet(s.ctx, chain.XDRBaseDenom)
	s.Require().NoError(err)
	s.Require().NotContains(available, chain.XDRBaseDenom)
}

func (s *KeeperTestSuite) TestGetRateSetRejectsElapsedTimeStaleness() {
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.MaxExchangeRateAge = time.Minute
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.USDBaseDenom, types.ExchangeRate{
		Denom:          chain.USDBaseDenom,
		Rate:           math.LegacyOneDec(),
		BlockTimestamp: oracleTestBlockTime.Add(-time.Minute - time.Second),
	}))

	_, err = s.keeper.GetRateSet(s.ctx, chain.USDBaseDenom)
	s.Require().ErrorIs(err, types.ErrStaleExchangeRate)
}

func (s *KeeperTestSuite) TestSetExchangeRateWithEventRejectsInvalidRate() {
	tests := []struct {
		name         string
		exchangeRate types.ExchangeRate
	}{
		{
			name: "rate is unset",
			exchangeRate: types.ExchangeRate{
				Denom:          chain.USDBaseDenom,
				Rate:           math.LegacyDec{},
				BlockTimestamp: oracleTestBlockTime,
			},
		},
		{
			name: "rate is not positive",
			exchangeRate: types.ExchangeRate{
				Denom:          chain.USDBaseDenom,
				Rate:           math.LegacyZeroDec(),
				BlockTimestamp: oracleTestBlockTime,
			},
		},
		{
			name: "rate is out of range",
			exchangeRate: types.ExchangeRate{
				Denom: chain.USDBaseDenom,
				Rate: math.LegacyNewDecFromBigInt(
					new(big.Int).Lsh(big.NewInt(1), 256),
				),
				BlockTimestamp: oracleTestBlockTime,
			},
		},
		{
			// Representable, but past the bound the tally holds every price
			// to: the backstop behind the folds that multiply by the store.
			name: "rate exceeds the store bound",
			exchangeRate: types.ExchangeRate{
				Denom:          chain.USDBaseDenom,
				Rate:           types.MaxExchangeRate.Add(math.LegacySmallestDec()),
				BlockTimestamp: oracleTestBlockTime,
			},
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			err := s.keeper.SetExchangeRateWithEvent(s.ctx, tc.exchangeRate)
			s.Require().ErrorIs(err, types.ErrInvalidExchangeRate)

			has, getErr := s.keeper.ExchangeRate.Has(s.ctx, tc.exchangeRate.Denom)
			s.Require().NoError(getErr)
			s.Require().False(has)
			s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
		})
	}
}

func (s *KeeperTestSuite) TestSetExchangeRateWithEventRejectsFutureTimestamp() {
	err := s.keeper.SetExchangeRateWithEvent(s.ctx, types.ExchangeRate{
		Denom:          chain.USDBaseDenom,
		Rate:           math.LegacyOneDec(),
		BlockTimestamp: oracleTestBlockTime.Add(time.Second),
	})
	s.Require().ErrorIs(err, types.ErrInvalidExchangeRate)

	has, getErr := s.keeper.ExchangeRate.Has(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(getErr)
	s.Require().False(has)
	s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
}

func (s *KeeperTestSuite) TestSetExchangeRateWithEvent() {
	rate := math.LegacyNewDecWithPrec(123, 2)

	err := s.keeper.SetExchangeRateWithEvent(s.ctx, newStoredExchangeRate(chain.USDBaseDenom, rate))
	s.Require().NoError(err)

	stored, err := s.keeper.ExchangeRate.Get(s.ctx, chain.USDBaseDenom)
	s.Require().NoError(err)
	s.Require().Equal(chain.USDBaseDenom, stored.Denom)
	s.Require().True(rate.Equal(stored.Rate))

	events := sdk.UnwrapSDKContext(s.ctx).EventManager().Events()
	s.requireTypedEvents(events, &types.EventExchangeRateUpdate{
		Denom:        chain.USDBaseDenom,
		ExchangeRate: rate,
	})
}

func (s *KeeperTestSuite) TestSetExchangeRateWithEventRejectsInvalidFeed() {
	err := s.keeper.SetExchangeRateWithEvent(s.ctx, newStoredExchangeRate("aUSD", math.LegacyOneDec()))
	s.Require().ErrorContains(err, "invalid exchange rate feed")

	has, getErr := s.keeper.ExchangeRate.Has(s.ctx, "aUSD")
	s.Require().NoError(getErr)
	s.Require().False(has)
	s.Require().Empty(sdk.UnwrapSDKContext(s.ctx).EventManager().Events())
}

// TestSetExchangeRateWithEventAcceptsUnlistedDenom covers the reason feed
// denoms are not checked against the asset registry: a commodity feed is priced
// like any other, and its denom may run ahead of being listed as an asset.
func (s *KeeperTestSuite) TestSetExchangeRateWithEventAcceptsUnlistedDenom() {
	s.Require().NoError(s.keeper.SetExchangeRateWithEvent(
		s.ctx,
		newStoredExchangeRate("agold", math.LegacyNewDec(2000)),
	))

	stored, err := s.keeper.ExchangeRate.Get(s.ctx, "agold")
	s.Require().NoError(err)
	s.Require().True(math.LegacyNewDec(2000).Equal(stored.Rate))
}

func (s *KeeperTestSuite) TestGetExchangeRates() {
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.USDBaseDenom, types.ExchangeRate{
		Denom:          chain.USDBaseDenom,
		Rate:           math.LegacyOneDec(),
		BlockTimestamp: oracleTestBlockTime.Add(-30 * time.Second),
	}))
	s.Require().NoError(s.keeper.ExchangeRate.Set(s.ctx, chain.KRWBaseDenom, types.ExchangeRate{
		Denom:          chain.KRWBaseDenom,
		Rate:           math.LegacyOneDec(),
		BlockTimestamp: oracleTestBlockTime.Add(-2 * time.Minute),
	}))

	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.MaxExchangeRateAge = time.Minute
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))

	exchangeRates, err := s.keeper.GetExchangeRates(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(
		sdk.DecCoins{sdk.NewDecCoinFromDec(chain.USDBaseDenom, math.LegacyOneDec())},
		exchangeRates,
	)
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
