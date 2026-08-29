package keeper_test

import (
	"context"
	"maps"
	"slices"
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

	chain "github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	claimstypes "github.com/ararat-network/ark/x/claims/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	reservetypes "github.com/ararat-network/ark/x/reserve/types"
	"github.com/ararat-network/ark/x/treasury/keeper"
	"github.com/ararat-network/ark/x/treasury/testutil"
	"github.com/ararat-network/ark/x/treasury/types"
)

// testMinBaseGasPrice is the suite's gas price floor: a tenth of a base unit
// per gas unit, so fee arithmetic in tests reads in small integers rather
// than the launch default's atto-scaled figures.
var testMinBaseGasPrice = math.LegacyMustNewDecFromStr("0.1")

type KeeperTestSuite struct {
	suite.Suite

	ctx           context.Context
	cdc           codec.Codec
	keeper        *keeper.Keeper
	msgServer     types.MsgServer
	queryClient   types.QueryClient
	authority     string
	accountKeeper *testutil.MockAccountKeeper
	bankKeeper    *testutil.MockBankKeeper
	oracleKeeper  *testutil.MockOracleKeeper
	assetKeeper   *testutil.MockAssetKeeper
	claimsKeeper  *testutil.MockClaimsKeeper
	reserveKeeper *testutil.MockReserveKeeper
	// insuranceRecognised and reserveRecognised are what the two funds report.
	// Both default to zero so a test that never funds either keeps its old
	// shape; a test that does calls the matching setter. These replace the Bank
	// balance stubs those funds used before they moved out.
	insuranceRecognised math.Int
	reserveRecognised   math.Int
	// reserveHoldings is what the strategic Reserve holds of registry members,
	// which nets out of the claimable aggregate for flows. It is empty by
	// default, so a test that never parks protocol paper sees gross and net
	// agree and keeps its old shape.
	reserveHoldings       sdk.Coins
	transientStoreService store.TransientStoreService
	commitMultiStore      storetypes.CommitMultiStore

	// assets is the mock asset registry every AssetKeeper answer derives from.
	// SetupTest seeds the historical suite denominations as ACTIVE so tests
	// that never bend membership keep their old fixtures; a test that cares
	// about the registry replaces it with setAssets or bends one entry with
	// seedAsset.
	assets map[string]assettypes.Asset
	// plans backs SettlementPlan lookups. The stored plan is served whether or
	// not it has activated — mirroring the keeper's deliberately ungated read.
	plans map[string]assettypes.SettlementPlan

	// rates is what the Oracle can currently price, keyed by denom. The
	// registry fold reads it for members only, so a denom absent here is a
	// stale or never-priced feed — the omission the available-set read makes.
	rates oracletypes.RateSet

	// lastRates is what the Oracle last stored for a denom whose feed is now
	// unavailable, keyed by denom. It stands for the store record that outlives
	// the freshness window, so a member listed here has a history and one left
	// out was never priced at all.
	lastRates oracletypes.RateSet

	// ratesErr fails the registry fold outright. Rate unavailability is an
	// omission, never an error, so this stands for a genuine store or state
	// fault — the only thing a valuation is allowed to propagate.
	ratesErr error
	// rateCaptures records each rate capture's denom list, newest last, so a
	// test can assert the capture shape without its own mock expectation.
	rateCaptures [][]string
	// reference is the protocol reference the mock reports. It defaults to
	// the reference the default params' tax cap is denominated in, matching
	// the genesis invariant the keeper enforces.
	reference string
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
	storeService := runtime.NewKVStoreService(key)
	transientStoreService := runtime.NewTransientStoreService(transientKey)
	testCtx := sdktestutil.DefaultContextWithDB(
		s.T(),
		key,
		transientKey,
	)
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
	s.claimsKeeper = testutil.NewMockClaimsKeeper(ctrl)
	s.reserveKeeper = testutil.NewMockReserveKeeper(ctrl)
	s.insuranceRecognised = math.ZeroInt()
	s.reserveRecognised = math.ZeroInt()
	s.claimsKeeper.EXPECT().
		RecognisedCapital(gomock.Any()).
		DoAndReturn(func(context.Context) (math.Int, error) {
			return s.insuranceRecognised, nil
		}).
		AnyTimes()
	s.reserveKeeper.EXPECT().
		RecognisedCapital(gomock.Any()).
		DoAndReturn(func(context.Context) (math.Int, error) {
			return s.reserveRecognised, nil
		}).
		AnyTimes()
	// The liability fold asks what the Reserve holds of every member it counts.
	// The expectation is address-specific, so it never shadows the fund-balance
	// reads other tests set up, and it answers zero unless a test parks paper.
	s.reserveHoldings = sdk.NewCoins()
	s.bankKeeper.EXPECT().
		GetBalance(gomock.Any(), authtypes.NewModuleAddress(reservetypes.StrategicReserveName), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
			return sdk.NewCoin(denom, s.reserveHoldings.AmountOf(denom))
		}).
		AnyTimes()
	s.transientStoreService = transientStoreService
	s.commitMultiStore = testCtx.CMS
	for _, moduleName := range types.FundAccountNames() {
		s.accountKeeper.EXPECT().
			GetModuleAddress(moduleName).
			Return(authtypes.NewModuleAddress(moduleName)).
			AnyTimes()
	}
	// Insurance is not a Treasury fund account, but Treasury still reads its
	// balance to report it in FundStatus.
	s.accountKeeper.EXPECT().
		GetModuleAddress(claimstypes.InsuranceName).
		Return(authtypes.NewModuleAddress(claimstypes.InsuranceName)).
		AnyTimes()
	// Nor is the strategic Reserve, but the liability fold reads what it holds
	// of every counted member, and the constructor asserts its registration.
	s.accountKeeper.EXPECT().
		GetModuleAddress(reservetypes.StrategicReserveName).
		Return(authtypes.NewModuleAddress(reservetypes.StrategicReserveName)).
		AnyTimes()
	s.accountKeeper.EXPECT().
		GetModuleAddress(types.StabilityTaxCollectorName).
		Return(authtypes.NewModuleAddress(types.StabilityTaxCollectorName)).
		AnyTimes()
	s.accountKeeper.EXPECT().
		GetModuleAddress(authtypes.FeeCollectorName).
		Return(authtypes.NewModuleAddress(authtypes.FeeCollectorName)).
		AnyTimes()

	// The asset mock is permissive by design: membership questions are asked
	// on many unrelated paths, so every method derives its answer from the
	// suite fixtures instead of per-test expectations. Strictness stays where
	// it matters — the bank and oracle mocks still fail on unexpected calls.
	s.assets = map[string]assettypes.Asset{}
	s.plans = map[string]assettypes.SettlementPlan{}
	s.rates = oracletypes.NewRateSet()
	s.lastRates = oracletypes.NewRateSet()
	s.ratesErr = nil
	s.rateCaptures = nil
	s.reference = chain.SDRBaseDenom
	for _, denom := range []string{chain.KRWBaseDenom, chain.SDRBaseDenom, chain.USDBaseDenom} {
		s.seedAsset(denom, assettypes.AssetStatus_ASSET_STATUS_ACTIVE)
	}
	s.assetKeeper.EXPECT().
		OraclePricedDenoms(gomock.Any()).
		DoAndReturn(func(context.Context) ([]string, error) {
			denoms := make([]string, 0, len(s.assets))
			for denom, asset := range s.assets {
				if asset.IsOraclePriced() {
					denoms = append(denoms, denom)
				}
			}
			slices.Sort(denoms)
			return denoms, nil
		}).
		AnyTimes()
	// Membership is presence in the fixture registry regardless of status,
	// mirroring the real registry's permanent rows: a departed member is a
	// status change, never a missing row.
	s.assetKeeper.EXPECT().
		HasAsset(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, denom string) (bool, error) {
			_, member := s.assets[denom]
			return member, nil
		}).
		AnyTimes()
	// Pricing verdicts mirror the real registry fold over the suite fixtures,
	// so a test changes what a denomination is worth by seeding an asset,
	// a plan, or an Oracle rate — never by stubbing a verdict directly.
	s.assetKeeper.EXPECT().
		Pricings(gomock.Any(), gomock.Any()).
		DoAndReturn(func(
			_ context.Context,
			denoms ...string,
		) (assettypes.AssetPricings, error) {
			return s.pricingsFor(denoms)
		}).
		AnyTimes()
	// The registry-wide fold answers from the same derivation as the
	// denomination-keyed one, over every fixture asset in key order, so the two
	// entry points cannot disagree here any more than they can in the registry.
	s.assetKeeper.EXPECT().
		PricedAssets(gomock.Any()).
		DoAndReturn(func(
			_ context.Context,
		) ([]string, assettypes.AssetPricings, error) {
			// Key order, like the real registry walk. Tests that stub
			// per-denomination supply reads rely on this order.
			denoms := slices.Sorted(maps.Keys(s.assets))
			pricings, err := s.pricingsFor(denoms)
			if err != nil {
				return nil, nil, err
			}
			return denoms, pricings, nil
		}).
		AnyTimes()
	s.oracleKeeper.EXPECT().
		GetReferenceDenom(gomock.Any()).
		DoAndReturn(func(context.Context) (string, error) {
			return s.reference, nil
		}).
		AnyTimes()
	// One fake serves every rate capture — the per-block factor refresh,
	// exposure sampling, and the re-point executor alike — from s.rates and
	// s.ratesErr, recording each call's denom list so a test can assert the
	// capture shape without its own expectation.
	s.oracleKeeper.EXPECT().
		GetAvailableRateSet(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, denoms ...string) (oracletypes.RateSet, error) {
			s.rateCaptures = append(s.rateCaptures, denoms)
			if s.ratesErr != nil {
				return nil, s.ratesErr
			}
			set := oracletypes.NewRateSet()
			for _, denom := range denoms {
				if rate, priceable := s.rates[denom]; priceable {
					set[denom] = rate
				}
			}
			return set, nil
		}).
		AnyTimes()

	s.keeper = keeper.NewKeeper(
		s.cdc,
		storeService,
		transientStoreService,
		s.authority,
		s.accountKeeper,
		s.bankKeeper,
		s.oracleKeeper,
		s.assetKeeper,
		s.claimsKeeper,
		s.reserveKeeper,
	)
	// The baseline zeroes the launch default's one-unit reference cap: an
	// uncapped set rebuilds rate-free, keeping the strict oracle mock quiet
	// for the many tests that never look at caps. Every cap test states its
	// own reference explicitly.
	baselineParams := types.DefaultParams()
	baselineParams.ReferenceTaxCap = math.ZeroInt()
	// The launch floor is atto-scaled; the suite prices gas at a tenth of a
	// base unit instead so fee arithmetic in tests reads in small integers.
	baselineParams.MinBaseGasPrice = testMinBaseGasPrice
	s.Require().NoError(s.keeper.Params.Set(s.ctx, baselineParams))
	// The live price always exists on a real chain — InitGenesis writes it —
	// so the suite keeps that invariant for everything that reads it.
	s.Require().NoError(s.keeper.BaseGasPrice.Set(s.ctx, testMinBaseGasPrice))
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, types.DefaultMonetaryPolicy()))
	s.Require().NoError(s.keeper.RewardFunding.Set(
		s.ctx,
		types.DefaultRewardFundingState(),
	))
	s.Require().NoError(s.keeper.MonetaryMandate.Set(
		s.ctx,
		types.DefaultMonetaryMandate(),
	))
	queryHelper := baseapp.NewQueryServerTestHelper(testCtx.Ctx, interfaceRegistry)
	types.RegisterQueryServer(queryHelper, keeper.NewQueryServerImpl(s.keeper))
	s.queryClient = types.NewQueryClient(queryHelper)
	s.msgServer = keeper.NewMsgServerImpl(s.keeper)
}

// pricingsFor mirrors the registry's fold over the suite fixtures for the named
// denominations. Both asset-keeper pricing stubs answer through it, so the
// registry-wide entry point and the denomination-keyed one cannot drift apart
// in the fixtures the way they cannot in the registry.
func (s *KeeperTestSuite) pricingsFor(denoms []string) (assettypes.AssetPricings, error) {
	if s.ratesErr != nil {
		return nil, s.ratesErr
	}

	// Capture mirrors the registry's: every oracle-priced member, at whatever
	// the suite's Oracle can answer.
	rates := oracletypes.NewRateSet()
	for _, denom := range denoms {
		asset, listed := s.assets[denom]
		if !listed || !asset.IsOraclePriced() {
			continue
		}
		if rate, priceable := s.rates[denom]; priceable {
			rates[denom] = rate
		}
	}

	pricings := make(assettypes.AssetPricings, len(denoms)+1)
	pricings[chain.NoahBaseDenom] = assettypes.NumeraireVerdict()
	for _, denom := range denoms {
		if denom == chain.NoahBaseDenom {
			continue
		}
		asset, listed := s.assets[denom]
		if !listed {
			pricings[denom] = assettypes.PricedAsset{
				Reason: assettypes.UnpricedReason_UNPRICED_REASON_UNRECOGNISED,
			}
			continue
		}
		var plan *assettypes.SettlementPlan
		if stored, found := s.plans[denom]; found {
			plan = &stored
		}
		// The real derivation, not a copy of it: a change to the registry's
		// authority table must reach these tests rather than let the fixture
		// drift into agreeing with itself.
		verdict := assettypes.PriceVerdict(asset, rates, plan)
		// Mirrors the registry's second pass: a member whose feed is
		// unavailable carries whatever the Oracle last stored for it, and a
		// member absent from lastRates was never priced.
		if !verdict.IsPriced() && verdict.Reason == assettypes.UnpricedReason_UNPRICED_REASON_FEED_UNAVAILABLE {
			if lastRate, known := s.lastRates[denom]; known {
				verdict.LastRate = &lastRate
			}
		}
		pricings[denom] = verdict
	}

	return pricings, nil
}

// setInsuranceRecognised sets what x/claims reports for Insurance. Treasury
// derives the displayed reservation as balance minus this.
func (s *KeeperTestSuite) setInsuranceRecognised(amount int64) {
	s.insuranceRecognised = math.NewInt(amount)
}

// setReserveRecognised sets what x/reserve reports for the strategic Reserve.
// At launch that is simply its NOAH balance.
func (s *KeeperTestSuite) setReserveRecognised(amount int64) {
	s.reserveRecognised = math.NewInt(amount)
}

func (s *KeeperTestSuite) setBlockHeight(height int64) {
	s.ctx = sdk.UnwrapSDKContext(s.ctx).WithBlockHeight(height)
}

// setDerivedTaxCap gives one denomination a derived cap of exactly the given
// amount: the reference amount pins to one base unit and the factor carries
// the value, so several denominations hold distinct caps side by side.
func (s *KeeperTestSuite) setDerivedTaxCap(denom string, amount math.Int) {
	params, err := s.keeper.Params.Get(s.ctx)
	s.Require().NoError(err)
	params.ReferenceTaxCap = math.OneInt()
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.Require().NoError(s.keeper.ConversionFactors.Set(s.ctx, denom, types.ConversionFactor{
		Denom:  denom,
		Factor: math.LegacyNewDecFromInt(amount),
	}))
}

// beginBlock runs BeginBlocker. The indirection is the seam tests share: when
// BeginBlocker grows a per-block concern the suite can absorb here, its call
// sites stay untouched. The zero-supply fallback that used to live here went
// with liability priming, which BeginBlocker no longer performs.
func (s *KeeperTestSuite) beginBlock() error {
	return s.keeper.BeginBlocker(s.ctx)
}

func (s *KeeperTestSuite) endBlock() error {
	return s.keeper.EndBlocker(s.ctx)
}

// seedAsset registers or overwrites one asset in the mock registry. The
// version is fixed at 1 unless a test overwrites the entry directly.
func (s *KeeperTestSuite) seedAsset(denom string, status assettypes.AssetStatus) {
	s.assets[denom] = assettypes.Asset{Denom: denom, Status: status, Version: 1}
}

// setRates replaces what the Oracle can price. A member left out of the set is
// one whose feed is stale or was never warmed, which the fold reports as
// unpriced rather than as an error.
func (s *KeeperTestSuite) setRates(rates oracletypes.RateSet) {
	s.rates = oracletypes.NewRateSetFrom(rates)
}

// setLastKnownRates replaces what the Oracle still holds for members it can no
// longer price freshly. Leaving a member out of both this and setRates is the
// never-priced case, which no aggregate may count.
func (s *KeeperTestSuite) setLastKnownRates(rates oracletypes.RateSet) {
	s.lastRates = oracletypes.NewRateSetFrom(rates)
}

// setAssets resets the mock registry to exactly the given denominations, all
// ACTIVE. Liability scans fold over the whole registry and read Bank supply
// for every entry, so a test that stubs supply expectations must pin the
// registry to the denominations it stubbed.
func (s *KeeperTestSuite) setAssets(denoms ...string) {
	s.assets = map[string]assettypes.Asset{}
	for _, denom := range denoms {
		s.seedAsset(denom, assettypes.AssetStatus_ASSET_STATUS_ACTIVE)
	}
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

// requireNoTypedEvent asserts that no event of the given type was emitted,
// proving a branch was not taken rather than merely that its effects were
// absent.
func (s *KeeperTestSuite) requireNoTypedEvent(unexpected proto.Message) {
	unexpectedEvent, err := sdk.TypedEventToEvent(unexpected)
	s.Require().NoError(err)
	for _, event := range sdk.UnwrapSDKContext(s.ctx).EventManager().Events() {
		s.Require().NotEqual(unexpectedEvent.Type, event.Type)
	}
}
