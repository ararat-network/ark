package integration

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	marketkeeper "github.com/ararat-network/ark/x/market/keeper"
	markettypes "github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// TestSwapGatedByAssetLifecycle drives the eligibility rules through real
// wiring. The asymmetry between the two legs is the substance: halting issuance
// is meant to preserve every exit while forbidding new supply, so the same
// status that keeps holders converting out must stop conversion producing more.
func TestSwapGatedByAssetLifecycle(t *testing.T) {
	f := newActivationFixture(t)
	queryServer := marketkeeper.NewQueryServerImpl(f.app.MarketKeeper)

	quote := func(ctx sdk.Context, offerDenom, askDenom string) error {
		_, err := queryServer.Swap(ctx, &markettypes.QuerySwapRequest{
			OfferCoin: sdk.NewCoin(offerDenom, chain.NativeBaseAmount(1)).String(),
			AskDenom:  askDenom,
		})

		return err
	}

	f.nextBlock(func(ctx sdk.Context) {
		// Both legs ACTIVE is the baseline every other case is measured against.
		require.NoError(t, quote(ctx, chain.USDBaseDenom, chain.KRWBaseDenom))
		require.NoError(t, quote(ctx, chain.USDBaseDenom, chain.NoahBaseDenom))
		require.NoError(t, quote(ctx, chain.NoahBaseDenom, chain.KRWBaseDenom))

		krw, err := f.app.AssetKeeper.Assets.Get(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		require.NoError(t, f.app.AssetKeeper.HaltIssuance(ctx, chain.KRWBaseDenom, krw.Version))

		// Halted: still convertible out, no longer producible.
		require.NoError(t, quote(ctx, chain.KRWBaseDenom, chain.USDBaseDenom))
		require.NoError(t, quote(ctx, chain.KRWBaseDenom, chain.NoahBaseDenom))
		require.ErrorContains(t,
			quote(ctx, chain.USDBaseDenom, chain.KRWBaseDenom),
			"cannot be produced by conversion",
		)
		require.ErrorContains(t,
			quote(ctx, chain.NoahBaseDenom, chain.KRWBaseDenom),
			"cannot be produced by conversion",
		)

		// Suspension closes both directions: that is what makes it containment
		// rather than a softer halt.
		halted, err := f.app.AssetKeeper.Assets.Get(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		require.NoError(t, f.app.AssetKeeper.SuspendAsset(ctx, chain.KRWBaseDenom, halted.Version))

		require.ErrorContains(t,
			quote(ctx, chain.KRWBaseDenom, chain.USDBaseDenom),
			"cannot be offered for conversion",
		)
		require.ErrorContains(t,
			quote(ctx, chain.USDBaseDenom, chain.KRWBaseDenom),
			"cannot be produced by conversion",
		)
	})
}

// TestTobinOverrideAppliesToSpread checks that Market's stored Tobin override determines the spread
// in an executed quote.
func TestTobinOverrideAppliesToSpread(t *testing.T) {
	f := newActivationFixture(t)
	msgServer := marketkeeper.NewMsgServerImpl(f.app.MarketKeeper)
	queryServer := marketkeeper.NewQueryServerImpl(f.app.MarketKeeper)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	quoteFee := func(ctx sdk.Context) math.LegacyDec {
		response, err := queryServer.Swap(ctx, &markettypes.QuerySwapRequest{
			OfferCoin: sdk.NewCoin(chain.USDBaseDenom, chain.NativeBaseAmount(1)).String(),
			AskDenom:  chain.KRWBaseDenom,
		})
		require.NoError(t, err)

		return response.SwapFee.Amount
	}

	f.nextBlock(func(ctx sdk.Context) {
		// The genesis override sits on amnt, so this pair starts at the default.
		effective, err := f.app.MarketKeeper.GetTobinTax(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		require.True(t, markettypes.DefaultTobinTax.Equal(effective))
		defaultFee := quoteFee(ctx)

		// A raise on one leg governs the pair: the spread must cover whichever
		// side carries more oracle-staleness risk.
		_, err = msgServer.SetTobinTaxOverride(ctx, &markettypes.MsgSetTobinTaxOverride{
			Authority: authority,
			Denom:     chain.KRWBaseDenom,
			TobinTax:  math.LegacyNewDecWithPrec(10, 2),
		})
		require.NoError(t, err)
		require.True(t, quoteFee(ctx).GT(defaultFee))

		// Removing it returns the pair to the default rather than to zero.
		_, err = msgServer.RemoveTobinTaxOverride(ctx, &markettypes.MsgRemoveTobinTaxOverride{
			Authority: authority,
			Denom:     chain.KRWBaseDenom,
		})
		require.NoError(t, err)
		require.True(t, quoteFee(ctx).Equal(defaultFee))
	})
}

// TestSettlementRedeemsAtPlanRate checks holder redemption at the committed plan rate while
// ordinary swaps remain forbidden. The fixture seeds an active plan; Asset tests own the activation
// delay.
func TestSettlementRedeemsAtPlanRate(t *testing.T) {
	f := newActivationFixture(t)
	marketMsgServer := marketkeeper.NewMsgServerImpl(f.app.MarketKeeper)
	queryServer := marketkeeper.NewQueryServerImpl(f.app.MarketKeeper)

	// Acquire a real balance the only way the chain allows: by converting.
	offer := sdk.NewCoin(chain.NoahBaseDenom, chain.NativeBaseAmount(1_000))
	var acquired sdk.Coin
	f.nextBlock(func(ctx sdk.Context) {
		response, err := marketMsgServer.Swap(ctx, &markettypes.MsgSwap{
			Trader:         f.trader.String(),
			OfferCoin:      offer,
			AskDenom:       chain.KRWBaseDenom,
			MinimumReceive: sdk.NewCoin(chain.KRWBaseDenom, math.OneInt()),
		})
		require.NoError(t, err)
		acquired = response.SwapCoin
		require.True(t, acquired.IsPositive())
	})

	// Quoted in NOAH per one unit of the settled asset, the same orientation as
	// an oracle rate.
	redemptionRate := math.LegacyNewDecWithPrec(5, 3)
	f.nextBlock(func(ctx sdk.Context) {
		asset, err := f.app.AssetKeeper.Assets.Get(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		require.NoError(t, f.app.AssetKeeper.SuspendAsset(ctx, chain.KRWBaseDenom, asset.Version))

		require.NoError(t, f.app.AssetKeeper.SettlementPlans.Set(
			ctx,
			chain.KRWBaseDenom,
			assettypes.SettlementPlan{
				Denom:            chain.KRWBaseDenom,
				RedemptionRate:   redemptionRate,
				OpenedHeight:     ctx.BlockHeight(),
				ActivationHeight: ctx.BlockHeight() + 1,
			},
		))
	})

	f.nextBlock(func(ctx sdk.Context) {
		// Ordinary conversion stays refused for the same denomination, so the
		// plan rate is reachable only through the settlement path.
		_, err := queryServer.Swap(ctx, &markettypes.QuerySwapRequest{
			OfferCoin: acquired.String(),
			AskDenom:  chain.NoahBaseDenom,
		})
		require.ErrorContains(t, err, "cannot be offered for conversion")

		before := f.app.BankKeeper.GetBalance(ctx, f.trader, chain.NoahBaseDenom)
		response, err := marketMsgServer.Settle(ctx, &markettypes.MsgSettle{
			Trader:    f.trader.String(),
			OfferCoin: acquired,
		})
		require.NoError(t, err)

		// The holder receives exactly the plan's arithmetic, whatever share of
		// it the shared buffer happened to cover.
		expected := math.LegacyNewDecFromInt(acquired.Amount).Mul(redemptionRate).TruncateInt()
		require.True(t, expected.Equal(response.RedeemedCoin.Amount))
		require.Equal(t, chain.NoahBaseDenom, response.RedeemedCoin.Denom)

		after := f.app.BankKeeper.GetBalance(ctx, f.trader, chain.NoahBaseDenom)
		require.True(t, after.Amount.Sub(before.Amount).Equal(expected))
		require.True(t,
			f.app.BankKeeper.GetBalance(ctx, f.trader, chain.KRWBaseDenom).IsZero(),
		)
	})
}

// TestBasePoolDenomPinnedToReference pins the ownership boundary: capacity may
// resize the pool's depth but never re-anchor its unit, because the unit is the
// protocol reference and x/asset owns that choice.
func TestBasePoolDenomPinnedToReference(t *testing.T) {
	f := newActivationFixture(t)
	msgServer := marketkeeper.NewMsgServerImpl(f.app.MarketKeeper)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	f.nextBlock(func(ctx sdk.Context) {
		capacity, err := f.app.MarketKeeper.ConversionPolicy.Get(ctx)
		require.NoError(t, err)

		referenceDenom, err := f.app.OracleKeeper.GetReferenceDenom(ctx)
		require.NoError(t, err)
		require.Equal(t, referenceDenom, capacity.BasePool.Denom)

		moved := capacity
		moved.BasePool = sdk.NewDecCoinFromDec(
			chain.USDBaseDenom,
			capacity.BasePool.Amount,
		)
		_, err = msgServer.UpdatePolicy(ctx, &markettypes.MsgUpdatePolicy{
			Authority: authority,
			Policy:    moved,
		})
		require.ErrorContains(t, err, "MsgSetReferenceDenom")

		// Depth alone still moves, and carries the pool gap with it.
		deeper := capacity
		deeper.BasePool = sdk.NewDecCoinFromDec(
			capacity.BasePool.Denom,
			capacity.BasePool.Amount.MulInt64(2),
		)
		_, err = msgServer.UpdatePolicy(ctx, &markettypes.MsgUpdatePolicy{
			Authority: authority,
			Policy:    deeper,
		})
		require.NoError(t, err)

		stored, err := f.app.MarketKeeper.ConversionPolicy.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, referenceDenom, stored.BasePool.Denom)
		require.True(t, stored.BasePool.Amount.Equal(capacity.BasePool.Amount.MulInt64(2)))
	})
}

// TestCapacityCommitteeResizesWithinCorridor checks real governance appointment, accepted bounded
// updates, rejected over-corridor policy, and governance's unrestricted override.
func TestCapacityCommitteeResizesWithinCorridor(t *testing.T) {
	f := newActivationFixture(t)
	msgServer := marketkeeper.NewMsgServerImpl(f.app.MarketKeeper)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	committee := authtypes.NewModuleAddress("capacity-committee").String()

	var launch markettypes.ConversionPolicy
	var term uint64

	f.nextBlock(func(ctx sdk.Context) {
		var err error
		launch, err = f.app.MarketKeeper.ConversionPolicy.Get(ctx)
		require.NoError(t, err)

		// Depth and the recovery period are delegated in both directions; the
		// spread floor is delegated raise-only, by naming the live floor as the
		// minimum. Both shapes come out of the one corridor.
		minimum := markettypes.ConversionPolicy{
			BasePool: sdk.NewDecCoinFromDec(
				launch.BasePool.Denom,
				launch.BasePool.Amount.QuoInt64(2),
			),
			PoolRecoveryPeriod: launch.PoolRecoveryPeriod / 4,
			MinStabilitySpread: launch.MinStabilitySpread,
		}
		maximum := markettypes.ConversionPolicy{
			BasePool: sdk.NewDecCoinFromDec(
				launch.BasePool.Denom,
				launch.BasePool.Amount.MulInt64(2),
			),
			PoolRecoveryPeriod: launch.PoolRecoveryPeriod,
			MinStabilitySpread: launch.MinStabilitySpread.MulInt64(4),
		}

		_, err = msgServer.SetConversionMandate(ctx, &markettypes.MsgSetConversionMandate{
			Authority:        authority,
			Committee:        committee,
			ActivationHeight: uint64(ctx.BlockHeight()),
			ExpiryHeight:     uint64(ctx.BlockHeight()) + 1_000,
			MinimumPolicy:    minimum,
			MaximumPolicy:    maximum,
			MaxTobinTax:      math.LegacyZeroDec(),
		})
		require.NoError(t, err)

		appointment, err := f.app.MarketKeeper.ConversionMandate.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, committee, appointment.Committee)
		term = appointment.Term
	})

	f.nextBlock(func(ctx sdk.Context) {
		// Terra's May 2022 response in one message: double the depth, quarter
		// the recovery period, and double the spread floor so the conversions
		// too small to feel the constant product get dearer in the same move.
		response := markettypes.ConversionPolicy{
			BasePool: sdk.NewDecCoinFromDec(
				launch.BasePool.Denom,
				launch.BasePool.Amount.MulInt64(2),
			),
			PoolRecoveryPeriod: launch.PoolRecoveryPeriod / 4,
			MinStabilitySpread: launch.MinStabilitySpread.MulInt64(2),
		}
		_, err := msgServer.CommitteeUpdatePolicy(ctx, &markettypes.MsgCommitteeUpdatePolicy{
			Committee:    committee,
			ExpectedTerm: term,
			Policy:       response,
		})
		require.NoError(t, err)

		stored, err := f.app.MarketKeeper.ConversionPolicy.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, response, stored)

		// Dropping the floor below where it stands is refused even though every
		// other field is inside the corridor: that is the raise-only delegation
		// holding.
		_, err = msgServer.CommitteeUpdatePolicy(ctx, &markettypes.MsgCommitteeUpdatePolicy{
			Committee:    committee,
			ExpectedTerm: term,
			Policy: markettypes.ConversionPolicy{
				BasePool:           response.BasePool,
				PoolRecoveryPeriod: response.PoolRecoveryPeriod,
				MinStabilitySpread: launch.MinStabilitySpread.QuoInt64(2),
			},
		})
		require.ErrorContains(t, err, "min stability spread")

		// Past the corridor the committee is refused, and governance is not.
		beyond := markettypes.ConversionPolicy{
			BasePool: sdk.NewDecCoinFromDec(
				launch.BasePool.Denom,
				launch.BasePool.Amount.MulInt64(10),
			),
			PoolRecoveryPeriod: launch.PoolRecoveryPeriod,
			MinStabilitySpread: launch.MinStabilitySpread,
		}
		_, err = msgServer.CommitteeUpdatePolicy(ctx, &markettypes.MsgCommitteeUpdatePolicy{
			Committee:    committee,
			ExpectedTerm: term,
			Policy:       beyond,
		})
		require.ErrorContains(t, err, "outside mandate range")

		_, err = msgServer.UpdatePolicy(ctx, &markettypes.MsgUpdatePolicy{
			Authority: authority,
			Policy:    beyond,
		})
		require.NoError(t, err)

		stored, err = f.app.MarketKeeper.ConversionPolicy.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, beyond, stored)
	})
}

// TestConversionCommitteeRaisesTobinWithinCap drives the Tobin ratchet through
// real wiring: the committee widens one market's oracle-staleness buffer up to
// the appointed cap, never past it and never downward, while governance keeps
// both restore paths.
func TestConversionCommitteeRaisesTobinWithinCap(t *testing.T) {
	f := newActivationFixture(t)
	msgServer := marketkeeper.NewMsgServerImpl(f.app.MarketKeeper)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	committee := authtypes.NewModuleAddress("conversion-committee").String()
	tobinCap := math.LegacyNewDecWithPrec(5, 2)

	var term uint64

	f.nextBlock(func(ctx sdk.Context) {
		launch, err := f.app.MarketKeeper.ConversionPolicy.Get(ctx)
		require.NoError(t, err)

		_, err = msgServer.SetConversionMandate(ctx, &markettypes.MsgSetConversionMandate{
			Authority:        authority,
			Committee:        committee,
			ActivationHeight: uint64(ctx.BlockHeight()),
			ExpiryHeight:     uint64(ctx.BlockHeight()) + 1_000,
			MinimumPolicy:    launch,
			MaximumPolicy:    launch,
			MaxTobinTax:      tobinCap,
		})
		require.NoError(t, err)

		appointment, err := f.app.MarketKeeper.ConversionMandate.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, tobinCap, appointment.MaxTobinTax)
		term = appointment.Term
	})

	f.nextBlock(func(ctx sdk.Context) {
		// Terra's MNT move in one message: an illiquid market's buffer goes
		// from the fiat default to 2%.
		raise := math.LegacyNewDecWithPrec(2, 2)
		_, err := msgServer.CommitteeSetTobinTax(ctx, &markettypes.MsgCommitteeSetTobinTax{
			Committee:    committee,
			ExpectedTerm: term,
			Denom:        chain.KRWBaseDenom,
			TobinTax:     raise,
		})
		require.NoError(t, err)

		effective, err := f.app.MarketKeeper.GetTobinTax(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		require.Equal(t, raise, effective)

		// Past the cap is refused.
		_, err = msgServer.CommitteeSetTobinTax(ctx, &markettypes.MsgCommitteeSetTobinTax{
			Committee:    committee,
			ExpectedTerm: term,
			Denom:        chain.KRWBaseDenom,
			TobinTax:     math.LegacyNewDecWithPrec(6, 2),
		})
		require.ErrorContains(t, err, "exceeds the mandate cap")

		// Once the incident passes the committee retires its own rate, all the
		// way back to the default, without waiting for a proposal.
		_, err = msgServer.CommitteeSetTobinTax(ctx, &markettypes.MsgCommitteeSetTobinTax{
			Committee:    committee,
			ExpectedTerm: term,
			Denom:        chain.KRWBaseDenom,
			TobinTax:     markettypes.DefaultTobinTax,
		})
		require.NoError(t, err)
		effective, err = f.app.MarketKeeper.GetTobinTax(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		require.Equal(t, markettypes.DefaultTobinTax, effective)

		// Below the default is still governance's alone.
		_, err = msgServer.CommitteeSetTobinTax(ctx, &markettypes.MsgCommitteeSetTobinTax{
			Committee:    committee,
			ExpectedTerm: term,
			Denom:        chain.KRWBaseDenom,
			TobinTax:     math.LegacyNewDecWithPrec(1, 3),
		})
		require.ErrorContains(t, err, "below the default rate")

		// Governance is the only path back to tracking the default rather than
		// sitting pinned at its value.
		_, err = msgServer.RemoveTobinTaxOverride(ctx, &markettypes.MsgRemoveTobinTaxOverride{
			Authority: authority,
			Denom:     chain.KRWBaseDenom,
		})
		require.NoError(t, err)

		params, err := f.app.MarketKeeper.Params.Get(ctx)
		require.NoError(t, err)
		effective, err = f.app.MarketKeeper.GetTobinTax(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		require.Equal(t, params.DefaultTobinTax, effective)
	})
}

// TestMarketPoolDenomRebasePreservesQuotesAndUSDRSupport checks Oracle-directed reference rebasing
// preserves quotes, proportional pool delta, and exported units.
func TestMarketPoolDenomRebasePreservesQuotesAndUSDRSupport(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewNextBlockContext(cmtproto.Header{
		Height: arkApp.LastBlockHeight() + 1,
		Time:   time.Unix(1_800_000_000, 0),
	})

	// The reference unit prices the pool but is never traded: no asset is
	// listed against it, so the swap legs below use listed stables. One XDR
	// is one NOAH, one USD half a NOAH, one EUR a quarter.
	for denom, rate := range map[string]math.LegacyDec{
		chain.XDRBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom: math.LegacyNewDecWithPrec(5, 1),
		chain.EURBaseDenom: math.LegacyNewDecWithPrec(25, 2),
	} {
		require.NoError(t, arkApp.OracleKeeper.ExchangeRate.Set(ctx, denom, oracletypes.ExchangeRate{
			Denom:          denom,
			Rate:           rate,
			BlockTimestamp: ctx.BlockTime(),
			BlockHeight:    uint64(ctx.BlockHeight()),
		}))
	}

	current := markettypes.DefaultConversionPolicy()
	current.BasePool = sdk.NewDecCoin(chain.XDRBaseDenom, math.NewInt(1_000_000_000_000))
	require.NoError(t, arkApp.MarketKeeper.ConversionPolicy.Set(ctx, current))
	oldDelta := math.LegacyNewDec(250_000_000_000)
	require.NoError(t, arkApp.MarketKeeper.ArkPoolDelta.Set(ctx, oldDelta))

	stableOffer := sdk.NewInt64Coin(chain.USDBaseDenom, 1_000_000)
	queryServer := marketkeeper.NewQueryServerImpl(arkApp.MarketKeeper)
	beforeQuote, err := queryServer.Swap(ctx, &markettypes.QuerySwapRequest{
		OfferCoin: stableOffer.String(),
		AskDenom:  chain.EURBaseDenom,
	})
	require.NoError(t, err)

	msgServer := marketkeeper.NewMsgServerImpl(arkApp.MarketKeeper)
	// Market cannot re-anchor its own unit; the reference move is what carries
	// the pool across, and the depth it lands on is derived from rates rather
	// than proposed. The rates arrive from x/asset, which reads the pair once
	// for both executors.
	require.NoError(t, arkApp.MarketKeeper.RebaseBasePool(
		ctx,
		chain.XDRBaseDenom,
		chain.USDBaseDenom,
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.XDRBaseDenom:  math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyNewDecWithPrec(5, 1),
		},
	))

	stored, err := arkApp.MarketKeeper.ConversionPolicy.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, chain.USDBaseDenom, stored.BasePool.Denom)
	require.True(t, math.LegacyNewDec(2_000_000_000_000).Equal(stored.BasePool.Amount))
	newDelta, err := arkApp.MarketKeeper.ArkPoolDelta.Get(ctx)
	require.NoError(t, err)
	require.True(t, math.LegacyNewDec(500_000_000_000).Equal(newDelta))

	poolResponse, err := queryServer.Pool(ctx, &markettypes.QueryPoolRequest{})
	require.NoError(t, err)
	require.Equal(t, stored.BasePool, poolResponse.BasePool)
	require.True(t, newDelta.Equal(poolResponse.ArkPoolDelta))

	afterQuote, err := queryServer.Swap(ctx, &markettypes.QuerySwapRequest{
		OfferCoin: stableOffer.String(),
		AskDenom:  chain.EURBaseDenom,
	})
	require.NoError(t, err)
	require.Equal(t, beforeQuote, afterQuote)

	trader := treasuryGovernanceVoter(t, arkApp, ctx)
	noahOffer := sdk.NewInt64Coin(chain.NoahBaseDenom, 1_000_000)
	apptestutil.FundAccount(t, arkApp, ctx, trader, sdk.NewCoins(noahOffer))

	response, err := msgServer.Swap(ctx, &markettypes.MsgSwap{
		Trader:         trader.String(),
		OfferCoin:      noahOffer,
		AskDenom:       chain.EURBaseDenom,
		MinimumReceive: sdk.NewInt64Coin(chain.EURBaseDenom, 1),
	})
	require.NoError(t, err)
	require.Equal(t, chain.EURBaseDenom, response.SwapCoin.Denom)
	require.True(t, response.SwapCoin.IsPositive())
	require.Equal(
		t,
		response.SwapCoin.Amount,
		arkApp.BankKeeper.GetBalance(ctx, trader, chain.EURBaseDenom).Amount,
	)
	finalDelta, err := arkApp.MarketKeeper.ArkPoolDelta.Get(ctx)
	require.NoError(t, err)

	arkApp.SimWriteState()
	_, err = arkApp.Commit()
	require.NoError(t, err)
	exported, err := arkApp.ExportAppStateAndValidators(false, nil, nil)
	require.NoError(t, err)
	var appState map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(exported.AppState, &appState))
	var exportedMarket markettypes.GenesisState
	arkApp.AppCodec().MustUnmarshalJSON(appState[markettypes.ModuleName], &exportedMarket)
	require.Equal(t, stored.BasePool, exportedMarket.ConversionPolicy.BasePool)
	require.True(t, finalDelta.Equal(exportedMarket.ArkPoolDelta))
}
