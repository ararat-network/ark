package simulation_test

import (
	"context"
	"errors"
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
	mandatepkg "github.com/ararat-network/ark/pkg/mandate"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	"github.com/ararat-network/ark/x/market/keeper"
	"github.com/ararat-network/ark/x/market/simulation"
	"github.com/ararat-network/ark/x/market/testutil"
	"github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// factorySeeds is how many draws each randomised factory is put through. One
// seed proves only that one draw validates; the guarantee worth holding is
// that no draw produces a message the handler would refuse.
const factorySeeds = 200

// TestMsgUpdateParamsFactoryProducesValidMessages holds the invariant a
// simulation rests on: every generated message passes the same validation its
// handler applies. A factory that drifts out of the domain turns a run into a
// stream of refused messages that still reports success.
func TestMsgUpdateParamsFactoryProducesValidMessages(t *testing.T) {
	f := newMarketFixture(t, 1)

	_, msg := simulation.MsgUpdateParamsFactory()(f.ctx, f.testData, f.reporter)

	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	require.NotNil(t, msg)
	require.Equal(t, govAddress().String(), msg.Authority)
	require.NoError(t, msg.Params.Validate())
}

// TestMsgUpdatePolicyFactoryProducesValidMessages exercises the three
// generators the policy carries. Their domains are what keeps a proposed
// policy inside the bounds ConversionPolicy.Validate enforces.
func TestMsgUpdatePolicyFactoryProducesValidMessages(t *testing.T) {
	for seed := int64(0); seed < factorySeeds; seed++ {
		f := newMarketFixture(t, seed)

		_, msg := simulation.MsgUpdatePolicyFactory()(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), "seed %d: %s", seed, f.reporter.Comment())
		require.NotNil(t, msg)
		require.Equal(t, govAddress().String(), msg.Authority)
		require.NoError(t, msg.Policy.Validate(), "seed %d", seed)
	}
}

// TestGovernanceFactoriesSkipWithoutGovAccount covers the branch neither
// factory owns: an unresolvable module account skips the operation and leaves
// the authority empty rather than emitting an unsigned message.
func TestGovernanceFactoriesSkipWithoutGovAccount(t *testing.T) {
	t.Run("update params", func(t *testing.T) {
		f := newMarketFixture(t, 1, withoutGovAccount())
		_, msg := simulation.MsgUpdateParamsFactory()(f.ctx, f.testData, f.reporter)
		require.True(t, f.reporter.IsSkipped())
		require.Empty(t, msg.Authority)
	})
	t.Run("update policy", func(t *testing.T) {
		f := newMarketFixture(t, 1, withoutGovAccount())
		_, msg := simulation.MsgUpdatePolicyFactory()(f.ctx, f.testData, f.reporter)
		require.True(t, f.reporter.IsSkipped())
		require.Empty(t, msg.Authority)
	})
}

// TestSwapFactoriesEmitQuotablePairs is the regression guard the swap half
// needs most. Every failure path in these factories is a silent skip, so a
// factory that stopped producing messages would leave a simulation reporting
// success over an empty run. It also pins the pair shape: randomDenomPairX
// routes through NOAH in one direction or the other, never NOAH to NOAH, which
// quoteSwap refuses as recursive.
func TestSwapFactoriesEmitQuotablePairs(t *testing.T) {
	t.Run("swap", func(t *testing.T) {
		for seed := int64(0); seed < factorySeeds; seed++ {
			f := newMarketFixture(t, seed)

			_, msg := simulation.MsgSwapFactory(f.keeper)(f.ctx, f.testData, f.reporter)

			require.False(t, f.reporter.IsSkipped(), "seed %d: %s", seed, f.reporter.Comment())
			require.NotNil(t, msg, "seed %d", seed)
			requireNoahOnOneSide(t, seed, msg.OfferCoin.Denom, msg.AskDenom)
			require.True(t, msg.OfferCoin.IsPositive(), "seed %d", seed)
		}
	})
	t.Run("swap send", func(t *testing.T) {
		emitted := 0
		for seed := int64(0); seed < factorySeeds; seed++ {
			f := newMarketFixture(t, seed)

			_, msg := simulation.MsgSwapSendFactory(f.keeper)(f.ctx, f.testData, f.reporter)

			// The receiver draw is allowed to come up empty: AnyAccount retries
			// once, so drawing the sender twice out of a small account set
			// leaves nobody to send to. That is the only skip this fixture
			// admits — any other means the factory stopped working.
			if f.reporter.IsSkipped() {
				require.Contains(t, f.reporter.Comment(), "failed to find a matching account", "seed %d", seed)
				require.Nil(t, msg, "seed %d", seed)
				continue
			}

			emitted++
			require.NotNil(t, msg, "seed %d", seed)
			requireNoahOnOneSide(t, seed, msg.OfferCoin.Denom, msg.AskDenom)
			require.NotEqual(t, msg.FromAddress, msg.ToAddress, "seed %d", seed)
		}
		// Seeds are fixed, so this count is deterministic. It is the guard
		// against a factory that skips its way through an entire run.
		require.Greater(t, emitted, factorySeeds/2)
	})
}

// TestSwapFactoriesSkipWhenNothingIsPriced covers the registry reads. Both are
// legitimate simulation states rather than faults, so both skip.
func TestSwapFactoriesSkipWhenNothingIsPriced(t *testing.T) {
	tests := []struct {
		name    string
		option  marketOption
		comment string
	}{
		{
			name:    "registry prices nothing",
			option:  withPricedDenoms(),
			comment: "no available exchange rates",
		},
		{
			name:    "registry read fails",
			option:  withRegistryError(),
			comment: "registry unavailable",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newMarketFixture(t, 1, tc.option)

			_, msg := simulation.MsgSwapFactory(f.keeper)(f.ctx, f.testData, f.reporter)

			require.True(t, f.reporter.IsSkipped())
			require.Contains(t, f.reporter.Comment(), tc.comment)
			require.Nil(t, msg)
		})
	}
}

// TestMsgSwapFactorySkipsAnUnquotablePair is the staleness path quotable
// exists for: a simulation casts no vote extensions, so its genesis rates age
// out and every later swap would be refused. The factory must skip rather than
// emit a message that only ever fails.
func TestMsgSwapFactorySkipsAnUnquotablePair(t *testing.T) {
	f := newMarketFixture(t, 1, withStaleRates())

	_, msg := simulation.MsgSwapFactory(f.keeper)(f.ctx, f.testData, f.reporter)

	require.True(t, f.reporter.IsSkipped())
	require.Nil(t, msg)
}

// TestMsgSwapSendFactorySkipsWhenTheOfferCannotBeSent covers the send-enabled
// gate MsgSwapSend carries and plain MsgSwap does not.
func TestMsgSwapSendFactorySkipsWhenTheOfferCannotBeSent(t *testing.T) {
	f := newMarketFixture(t, 1, withSendDisabled())

	_, msg := simulation.MsgSwapSendFactory(f.keeper)(f.ctx, f.testData, f.reporter)

	require.True(t, f.reporter.IsSkipped())
	require.Contains(t, f.reporter.Comment(), "offer denom send not enabled")
	require.Nil(t, msg)
}

func requireNoahOnOneSide(t *testing.T, seed int64, offerDenom, askDenom string) {
	t.Helper()
	require.NotEqual(t, offerDenom, askDenom, "seed %d", seed)
	require.True(t,
		(offerDenom == chain.NoahBaseDenom) != (askDenom == chain.NoahBaseDenom),
		"seed %d: expected NOAH on exactly one side, got %s -> %s", seed, offerDenom, askDenom,
	)
}

// simDenom is the one oracle-priced member the fixture registers. Pairs route
// through NOAH, so one member is enough to exercise both directions.
const simDenom = chain.USDBaseDenom

type marketFixture struct {
	ctx       sdk.Context
	keeper    *keeper.Keeper
	msgServer types.MsgServer
	testData  *simsx.ChainDataSource
	reporter  simsx.SimulationReporter
	accounts  []simtypes.Account
	rand      *rand.Rand
}

// marketConfig is what the options bend before the fixture is built.
type marketConfig struct {
	govRegistered bool
	pricedDenoms  []string
	registryErr   error
	ratesErr      error
	sendEnabled   bool
}

type marketOption func(*marketConfig)

// withoutGovAccount stands for a module account the app never registered.
func withoutGovAccount() marketOption {
	return func(c *marketConfig) { c.govRegistered = false }
}

// withPricedDenoms replaces the priced set; called with no arguments it empties
// it, which is the state a chain is in before the first oracle window closes.
func withPricedDenoms(denoms ...string) marketOption {
	return func(c *marketConfig) { c.pricedDenoms = denoms }
}

func withRegistryError() marketOption {
	return func(c *marketConfig) { c.registryErr = errors.New("registry unavailable") }
}

// withStaleRates ages every rate out of its window, which is where a long
// simulation run ends up.
func withStaleRates() marketOption {
	return func(c *marketConfig) { c.ratesErr = oracletypes.ErrStaleExchangeRate }
}

func withSendDisabled() marketOption {
	return func(c *marketConfig) { c.sendEnabled = false }
}

// balanceStub is the simsx balance source. It funds every account identically,
// which is all the factories ask of it.
type balanceStub struct {
	coins       sdk.Coins
	sendEnabled bool
}

func (b balanceStub) SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins { return b.coins }
func (b balanceStub) IsSendEnabledDenom(context.Context, string) bool          { return b.sendEnabled }

func govAddress() sdk.AccAddress { return authtypes.NewModuleAddress(govtypes.ModuleName) }

func newMarketFixture(t *testing.T, seed int64, opts ...marketOption) marketFixture {
	t.Helper()

	cfg := marketConfig{govRegistered: true, pricedDenoms: []string{simDenom}, sendEnabled: true}
	for _, opt := range opts {
		opt(&cfg)
	}

	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)

	key := storetypes.NewKVStoreKey(types.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("transient_test")
	testCtx := sdktestutil.DefaultContextWithDB(t, key, transientKey)
	// Past the first block, so an appointment activating at height one is
	// open the way a run's would be.
	ctx := testCtx.Ctx.WithBlockHeight(10)

	ctrl := gomock.NewController(t)
	accountKeeper := testutil.NewMockAccountKeeper(ctrl)
	accountKeeper.EXPECT().GetModuleAddress(types.ModuleName).Return(sdk.AccAddress{1})
	gov := govAddress()
	if !cfg.govRegistered {
		gov = nil
	}
	accountKeeper.EXPECT().GetModuleAddress(govtypes.ModuleName).Return(gov).AnyTimes()

	assetKeeper := testutil.NewMockAssetKeeper(ctrl)
	assetKeeper.EXPECT().
		OraclePricedDenoms(gomock.Any()).
		Return(cfg.pricedDenoms, cfg.registryErr).
		AnyTimes()
	assetKeeper.EXPECT().
		GetAsset(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, denom string) (assettypes.Asset, error) {
			return assettypes.Asset{
				Denom:  denom,
				Status: assettypes.AssetStatus_ASSET_STATUS_ACTIVE,
			}, nil
		}).
		AnyTimes()

	oracleKeeper := testutil.NewMockOracleKeeper(ctrl)
	oracleKeeper.EXPECT().
		GetRateSet(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, denoms ...string) (oracletypes.RateSet, error) {
			if cfg.ratesErr != nil {
				return nil, cfg.ratesErr
			}
			return simRates(), nil
		}).
		AnyTimes()

	wasmKeeper := testutil.NewMockWasmKeeper(ctrl)
	wasmKeeper.EXPECT().HasContractInfo(gomock.Any(), gomock.Any()).Return(false).AnyTimes()
	marketKeeper := keeper.NewKeeper(
		cdc,
		runtime.NewKVStoreService(key),
		runtime.NewTransientStoreService(transientKey),
		gov.String(),
		accountKeeper,
		wasmKeeper,
		testutil.NewMockBankKeeper(ctrl),
		oracleKeeper,
		testutil.NewMockTreasuryKeeper(ctrl),
		assetKeeper,
	)

	require.NoError(t, marketKeeper.Params.Set(ctx, types.DefaultParams()))
	require.NoError(t, marketKeeper.ConversionPolicy.Set(ctx, types.DefaultConversionPolicy()))
	require.NoError(t, marketKeeper.ArkPoolDelta.Set(ctx, math.LegacyZeroDec()))

	r := rand.New(rand.NewSource(seed))
	// Three accounts so MsgSwapSend can always find a receiver distinct from
	// the sender it drew.
	accounts := simtypes.RandomAccounts(r, 3)
	testData := simsx.NewChainDataSource(
		ctx,
		r,
		accountKeeper,
		balanceStub{coins: simBalances(), sendEnabled: cfg.sendEnabled},
		addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix()),
		accounts...,
	)

	return marketFixture{
		ctx:       ctx,
		keeper:    marketKeeper,
		msgServer: keeper.NewMsgServerImpl(marketKeeper),
		testData:  testData,
		reporter:  simsx.NewBasicSimulationReporter(),
		accounts:  accounts,
		rand:      r,
	}
}

// simRates prices every denomination the fixture can name. Rates are NOAH per
// unit, so NOAH is one by definition.
func simRates() oracletypes.RateSet {
	return oracletypes.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		simDenom:            math.LegacyNewDec(2),
		chain.XDRBaseDenom:  math.LegacyNewDec(3),
	}
}

// simBalances funds both sides of the pair, since the factory may draw either
// direction.
func simBalances() sdk.Coins {
	return sdk.NewCoins(
		sdk.NewInt64Coin(chain.NoahBaseDenom, 1_000_000_000),
		sdk.NewInt64Coin(simDenom, 1_000_000_000),
	)
}

// TestTobinOverrideFactories covers the pair: setting names a registered
// denomination, and removing names one already carrying an override.
func TestTobinOverrideFactories(t *testing.T) {
	t.Run("set names a registered denomination", func(t *testing.T) {
		f := newMarketFixture(t, 1)

		_, msg := simulation.MsgSetTobinTaxOverrideFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.Equal(t, simDenom, msg.Denom)
		require.NoError(t, types.ValidateTobinTax(msg.TobinTax))
		require.NoError(t, f.keeper.SetTobinTaxOverride(f.ctx, msg.Denom, msg.TobinTax))
	})

	t.Run("set skips an empty registry", func(t *testing.T) {
		f := newMarketFixture(t, 1, withPricedDenoms())

		_, msg := simulation.MsgSetTobinTaxOverrideFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.True(t, f.reporter.IsSkipped())
		require.Contains(t, f.reporter.Comment(), "no registered denomination")
		require.Nil(t, msg)
	})

	t.Run("remove skips without a standing override", func(t *testing.T) {
		f := newMarketFixture(t, 1)

		_, msg := simulation.MsgRemoveTobinTaxOverrideFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.True(t, f.reporter.IsSkipped())
		require.Contains(t, f.reporter.Comment(), "no standing tobin tax override")
		require.Nil(t, msg)
	})

	t.Run("remove names a standing override", func(t *testing.T) {
		f := newMarketFixture(t, 1)
		require.NoError(t, f.keeper.SetTobinTaxOverride(f.ctx, simDenom, math.LegacyNewDecWithPrec(1, 2)))

		_, msg := simulation.MsgRemoveTobinTaxOverrideFactory(f.keeper)(f.ctx, f.testData, f.reporter)

		require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
		require.Equal(t, simDenom, msg.Denom)
		require.NoError(t, f.keeper.RemoveTobinTaxOverride(f.ctx, msg.Denom))
	})
}

// TestMsgSetConversionMandateFactory pins the appointment the factory emits.
func TestMsgSetConversionMandateFactory(t *testing.T) {
	f := newMarketFixture(t, 1)

	_, msg := simulation.MsgSetConversionMandateFactory()(f.ctx, f.testData, f.reporter)

	require.False(t, f.reporter.IsSkipped(), f.reporter.Comment())
	require.NotEqual(t, msg.Authority, msg.Committee)
	require.Less(t, msg.ActivationHeight, msg.ExpiryHeight)
	mandate := types.ConversionMandate{
		Envelope:      mandatepkg.Envelope{Term: 1, Committee: msg.Committee, ActivationHeight: msg.ActivationHeight, ExpiryHeight: msg.ExpiryHeight},
		MinimumPolicy: msg.MinimumPolicy,
		MaximumPolicy: msg.MaximumPolicy,
		MaxTobinTax:   msg.MaxTobinTax,
	}
	require.NoError(t, mandate.Validate())
}
