package integration

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	apptestutil "github.com/ararat-network/ark/app/testutil"
	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	marketkeeper "github.com/ararat-network/ark/x/market/keeper"
	markettypes "github.com/ararat-network/ark/x/market/types"
	oraclekeeper "github.com/ararat-network/ark/x/oracle/keeper"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	treasurykeeper "github.com/ararat-network/ark/x/treasury/keeper"
	treasurytestutil "github.com/ararat-network/ark/x/treasury/testutil"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// fixtureRate is the rate the fixture validator reports for every feed: a
// hundredth of a NOAH per unit. Uniform rates make cross-denomination
// conversion 1:1, which keeps the re-point arithmetic assertable by eye.
var fixtureRate = math.LegacyNewDecWithPrec(1, 2)

// TestLiabilityPartitionTracksLifecycle drives one asset through the statuses
// the partition classifies and reads FundStatus at each step. The report is
// the substance: suspension without a plan must make the total unavailable
// rather than silently dropping the exposure, and a plan must move the same
// supply into settlement-priced recognition at the committed rate.
func TestLiabilityPartitionTracksLifecycle(t *testing.T) {
	f := newActivationFixture(t)
	marketMsgServer := marketkeeper.NewMsgServerImpl(f.app.MarketKeeper)
	queryServer := treasurykeeper.NewQueryServerImpl(f.app.TreasuryKeeper)

	fundStatus := func(ctx sdk.Context) *treasurytypes.QueryFundStatusResponse {
		response, err := queryServer.FundStatus(ctx, &treasurytypes.QueryFundStatusRequest{})
		require.NoError(t, err)

		return response
	}

	// A positive buffer ratio makes the targets carry information: they must
	// track recognised liability while it is valued and claim nothing while it
	// is not.
	var acquired sdk.Coin
	f.nextBlock(func(ctx sdk.Context) {
		policy, err := f.app.TreasuryKeeper.EconomicPolicy.Get(ctx)
		require.NoError(t, err)
		policy.RedemptionBufferTargetRatio = math.LegacyNewDecWithPrec(1, 1)
		require.NoError(t, f.app.TreasuryKeeper.EconomicPolicy.Set(ctx, policy))

		response, err := marketMsgServer.Swap(ctx, &markettypes.MsgSwap{
			Trader:         f.trader.String(),
			OfferCoin:      sdk.NewCoin(chain.NoahBaseDenom, chain.NativeBaseAmount(1_000)),
			AskDenom:       chain.KRWBaseDenom,
			MinimumReceive: sdk.NewCoin(chain.KRWBaseDenom, math.OneInt()),
		})
		require.NoError(t, err)
		acquired = response.SwapCoin
		require.True(t, acquired.IsPositive())
	})

	f.nextBlock(func(ctx sdk.Context) {
		// The only non-NOAH supply is the acquired KRW, so priced liability is
		// exactly that supply valued at the fixture rate.
		status := fundStatus(ctx)
		require.True(t, valuationComplete(status))
		expectedPriced := math.LegacyNewDecFromInt(acquired.Amount).Mul(fixtureRate)
		require.True(t, expectedPriced.Equal(status.PricedLiability.Amount))
		require.True(t, status.SettlementLiability.Amount.IsZero())
		require.Empty(t, status.UntrustedSuspendedSupply)
		require.Empty(t, status.WrittenOffExposure)
		require.True(t, status.RedemptionBufferTarget.Amount.IsPositive())

		asset, err := f.app.AssetKeeper.Assets.Get(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		require.NoError(t, f.app.AssetKeeper.SuspendAsset(ctx, chain.KRWBaseDenom, asset.Version))

		// Suspension without a plan is recognised-but-unvaluable exposure: the
		// report lists it by name, the total goes unavailable, and every
		// target returns to zero rather than being computed from a fiction.
		status = fundStatus(ctx)
		require.False(t, valuationComplete(status))
		require.True(t, status.PricedLiability.Amount.IsZero())
		require.True(t, status.NominalLiability.Amount.IsZero())
		require.Equal(t, []sdk.Coin{sdk.NewCoin(chain.KRWBaseDenom, acquired.Amount)}, status.UntrustedSuspendedSupply)
		require.True(t, status.RedemptionBufferTarget.Amount.IsZero())
	})

	// Quoted in NOAH per one unit of the settled asset, the same orientation as
	// an oracle rate.
	redemptionRate := math.LegacyNewDecWithPrec(5, 3)
	f.nextBlock(func(ctx sdk.Context) {
		// An open plan re-prices the same supply at the committed rate from
		// the block it opens — the commitment is irrevocable from open, and
		// the activation delay gates execution, not obligation.
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

		status := fundStatus(ctx)
		require.True(t, valuationComplete(status))
		expectedSettlement := math.LegacyNewDecFromInt(acquired.Amount).Mul(redemptionRate)
		require.True(t, expectedSettlement.Equal(status.SettlementLiability.Amount))
		require.True(t, expectedSettlement.Equal(status.NominalLiability.Amount))
		require.Empty(t, status.UntrustedSuspendedSupply)
		require.True(t, status.RedemptionBufferTarget.Amount.IsPositive())
	})

	f.nextBlock(func(ctx sdk.Context) {
		// A full settlement burns the outstanding supply, so the recognised
		// settlement liability returns to zero and the report stays available.
		_, err := marketMsgServer.Settle(ctx, &markettypes.MsgSettle{
			Trader:    f.trader.String(),
			OfferCoin: acquired,
		})
		require.NoError(t, err)

		status := fundStatus(ctx)
		require.True(t, valuationComplete(status))
		require.True(t, status.SettlementLiability.Amount.IsZero())
		require.Empty(t, status.UntrustedSuspendedSupply)
		require.True(t,
			f.app.BankKeeper.GetBalance(ctx, f.trader, chain.KRWBaseDenom).IsZero(),
		)
	})
}

// TestReferenceRepointRebasesPoolAndCap is the Phase 5 completion criterion:
// with both executors wired, one MsgSetReferenceDenom re-denominates Market's base
// pool and Treasury's reference tax cap in the same transaction. The fixture
// prices every feed identically, so the conversion is 1:1 and only the units
// move — which is exactly the claim: a re-denomination changes unit, never
// stance.
func TestReferenceRepointRebasesPoolAndCap(t *testing.T) {
	f := newActivationFixture(t)
	oracleMsgServer := oraclekeeper.NewMsgServerImpl(f.app.OracleKeeper)
	marketQueryServer := marketkeeper.NewQueryServerImpl(f.app.MarketKeeper)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	capAmount := math.NewInt(1_000_000)
	var poolBefore sdk.DecCoin
	var deltaBefore math.LegacyDec
	f.nextBlock(func(ctx sdk.Context) {
		// A positive cap is what makes the rebase observable; the launch
		// default is zero, whose conversion would prove nothing.
		params, err := f.app.TreasuryKeeper.Params.Get(ctx)
		require.NoError(t, err)
		params.ReferenceTaxCap = capAmount
		require.NoError(t, f.app.TreasuryKeeper.Params.Set(ctx, params))

		marketCapacity, err := f.app.MarketKeeper.ConversionPolicy.Get(ctx)
		require.NoError(t, err)
		poolBefore = marketCapacity.BasePool
		require.Equal(t, chain.XDRBaseDenom, poolBefore.Denom)
		deltaBefore, err = f.app.MarketKeeper.ArkPoolDelta.Get(ctx)
		require.NoError(t, err)
	})

	f.nextBlock(func(ctx sdk.Context) {
		_, err := oracleMsgServer.SetReferenceDenom(ctx, &oracletypes.MsgSetReferenceDenom{
			Authority:      authority,
			ReferenceDenom: chain.USDBaseDenom,
		})
		require.NoError(t, err)

		referenceDenom, err := f.app.OracleKeeper.GetReferenceDenom(ctx)
		require.NoError(t, err)
		require.Equal(t, chain.USDBaseDenom, referenceDenom)

		// Market's pool moved unit at the 1:1 fixture rate: same depth, same
		// delta, new denomination.
		marketCapacity, err := f.app.MarketKeeper.ConversionPolicy.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, chain.USDBaseDenom, marketCapacity.BasePool.Denom)
		require.True(t, poolBefore.Amount.Equal(marketCapacity.BasePool.Amount))
		delta, err := f.app.MarketKeeper.ArkPoolDelta.Get(ctx)
		require.NoError(t, err)
		require.True(t, deltaBefore.Equal(delta))

		// Treasury's cap moved with it, in the same transaction.
		treasuryParams, err := f.app.TreasuryKeeper.Params.Get(ctx)
		require.NoError(t, err)
		require.Equal(t, chain.USDBaseDenom, treasuryParams.ReferenceDenom)
		require.Equal(t, capAmount, treasuryParams.ReferenceTaxCap)

		var sawPoolUpdate, sawCapRebase bool
		for _, event := range ctx.EventManager().Events() {
			switch event.Type {
			case "ark.market.v1.EventPoolUpdated":
				sawPoolUpdate = true
			case "ark.treasury.v1.EventReferenceTaxCapRebased":
				sawCapRebase = true
			}
		}
		require.True(t, sawPoolUpdate, "market pool rebase event")
		require.True(t, sawCapRebase, "treasury cap rebase event")
	})

	// The chain keeps pricing conversion in the new unit on the next block.
	f.nextBlock(func(ctx sdk.Context) {
		_, err := marketQueryServer.Swap(ctx, &markettypes.QuerySwapRequest{
			OfferCoin: sdk.NewCoin(chain.USDBaseDenom, chain.NativeBaseAmount(1)).String(),
			AskDenom:  chain.KRWBaseDenom,
		})
		require.NoError(t, err)
	})
}

// TestIssuanceHaltPreservesTreasuryPolicy pins the halt's asymmetry on the
// Treasury side: ISSUANCE_HALTED supply keeps being taxed, keeps being priced
// liability, and keeps its redemption exit — the halt stops supply growth,
// never the accounting or the way out.
func TestIssuanceHaltPreservesTreasuryPolicy(t *testing.T) {
	f := newActivationFixture(t)
	marketMsgServer := marketkeeper.NewMsgServerImpl(f.app.MarketKeeper)
	queryServer := treasurykeeper.NewQueryServerImpl(f.app.TreasuryKeeper)

	taxRate := math.LegacyNewDecWithPrec(1, 3)
	var acquired sdk.Coin
	f.nextBlock(func(ctx sdk.Context) {
		// Activate the tax and lift the launch reference cap, which clamps tax
		// to one base unit, so the assertions read the rate, not the clamp —
		// through the params, which the per-block factor refresh leaves alone,
		// where a fixture factor would be re-derived away next block.
		params, err := f.app.TreasuryKeeper.Params.Get(ctx)
		require.NoError(t, err)
		params.TransferTaxRate = taxRate
		params.ReferenceTaxCap = math.ZeroInt()
		require.NoError(t, f.app.TreasuryKeeper.Params.Set(ctx, params))

		response, err := marketMsgServer.Swap(ctx, &markettypes.MsgSwap{
			Trader:         f.trader.String(),
			OfferCoin:      sdk.NewCoin(chain.NoahBaseDenom, chain.NativeBaseAmount(1_000)),
			AskDenom:       chain.KRWBaseDenom,
			MinimumReceive: sdk.NewCoin(chain.KRWBaseDenom, math.OneInt()),
		})
		require.NoError(t, err)
		acquired = response.SwapCoin

		asset, err := f.app.AssetKeeper.Assets.Get(ctx, chain.KRWBaseDenom)
		require.NoError(t, err)
		require.NoError(t, f.app.AssetKeeper.HaltIssuance(ctx, chain.KRWBaseDenom, asset.Version))
	})

	f.nextBlock(func(ctx sdk.Context) {
		// Still taxed: the halted denomination remains protocol-convertible
		// money on the way out, so it keeps paying the premium.
		sendAmount := math.NewInt(10_000)
		tax, _, err := f.app.TreasuryKeeper.ComputeTax(ctx, []sdk.Msg{&banktypes.MsgSend{
			FromAddress: f.trader.String(),
			ToAddress:   f.trader.String(),
			Amount:      sdk.NewCoins(sdk.NewCoin(chain.KRWBaseDenom, sendAmount)),
		}})
		require.NoError(t, err)
		expectedTax := taxRate.MulInt(sendAmount).TruncateInt()
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(chain.KRWBaseDenom, expectedTax)), tax)

		// Still priced liability, with the total available.
		status, err := queryServer.FundStatus(ctx, &treasurytypes.QueryFundStatusRequest{})
		require.NoError(t, err)
		require.True(t, valuationComplete(status))
		expectedPriced := math.LegacyNewDecFromInt(acquired.Amount).Mul(fixtureRate)
		require.True(t, expectedPriced.Equal(status.PricedLiability.Amount))

		// Still redeemable: conversion out draws the ordinary redemption path.
		_, err = marketMsgServer.Swap(ctx, &markettypes.MsgSwap{
			Trader:         f.trader.String(),
			OfferCoin:      acquired,
			AskDenom:       chain.NoahBaseDenom,
			MinimumReceive: sdk.NewCoin(chain.NoahBaseDenom, math.OneInt()),
		})
		require.NoError(t, err)
	})
}

// TestTaxCapsFollowMembershipEpoch pins the epoch machinery end to end: a
// lifecycle transition that changes the oracle-priced set bumps the version in
// preblock, and the next BeginBlocker re-derives the caps for the new
// membership without any per-block store walk — leaving the departed
// denomination's cap where it stands, because its supply is still moving.
func TestTaxCapsFollowMembershipEpoch(t *testing.T) {
	f := newActivationFixture(t)

	ctx := f.readCtx()
	found, err := f.app.TreasuryKeeper.ConversionFactors.Has(ctx, chain.KRWBaseDenom)
	require.NoError(t, err)
	require.True(t, found, "launch member carries a cap")

	f.nextBlock(func(blockCtx sdk.Context) {
		asset, err := f.app.AssetKeeper.Assets.Get(blockCtx, chain.KRWBaseDenom)
		require.NoError(t, err)
		require.NoError(t, f.app.AssetKeeper.SuspendAsset(blockCtx, chain.KRWBaseDenom, asset.Version))
	})
	// The suspension landed before the previous block ran, so that block's
	// BeginBlocker already saw the bumped epoch; one more block simply proves
	// the state is settled rather than in flight.
	f.nextBlock(nil)

	ctx = f.readCtx()
	found, err = f.app.TreasuryKeeper.ConversionFactors.Has(ctx, chain.KRWBaseDenom)
	require.NoError(t, err)
	require.True(t, found, "suspended denomination keeps its cap")

	// The remaining members keep their caps too: the rebuild is a
	// re-derivation, not a clear.
	found, err = f.app.TreasuryKeeper.ConversionFactors.Has(ctx, chain.USDBaseDenom)
	require.NoError(t, err)
	require.True(t, found)

	// And the taxable set follows the caps, not the lifecycle: suspended
	// supply still moves between holders, so the transfer still pays the
	// transfer tax that ordinary money pays.
	params, err := f.app.TreasuryKeeper.Params.Get(ctx)
	require.NoError(t, err)
	params.TransferTaxRate = math.LegacyNewDecWithPrec(1, 3)
	f.nextBlock(func(blockCtx sdk.Context) {
		require.NoError(t, f.app.TreasuryKeeper.Params.Set(blockCtx, params))
		// The launch reference cap clamps tax to one base unit; lift the
		// ceiling so the assertion reads the rate, not the clamp.
		treasurytestutil.SetDerivedTaxCap(t, f.app.TreasuryKeeper, blockCtx, chain.KRWBaseDenom, math.NewInt(1_000_000))

		tax, _, err := f.app.TreasuryKeeper.ComputeTax(blockCtx, []sdk.Msg{&banktypes.MsgSend{
			FromAddress: f.trader.String(),
			ToAddress:   f.trader.String(),
			Amount:      sdk.NewCoins(sdk.NewCoin(chain.KRWBaseDenom, math.NewInt(10_000))),
		}})
		require.NoError(t, err)
		require.Equal(t, sdk.NewCoins(sdk.NewCoin(chain.KRWBaseDenom, math.NewInt(10))), tax)
	})
}

type phase3AIntegrationResult struct {
	output       math.Int
	bufferPaid   math.Int
	residualMint math.Int
	endingDelta  math.LegacyDec
}

// TestPhase3ACapacityIntegrationIncompleteValuationStillFundsFromBuffer pins
// the claimable denominator end to end: the unpriced dust member is excluded
// from the aggregate and disclosed rather than switching the Buffer off, and
// since it cannot itself redeem, the healthy redemption's funding is identical
// to the fully priced run in everything but the audit flag.
func TestPhase3ACapacityIntegrationIncompleteValuationStillFundsFromBuffer(t *testing.T) {
	complete := runPhase3AIntegrationRedemption(t, true)
	incomplete := runPhase3AIntegrationRedemption(t, false)

	require.True(t, incomplete.bufferPaid.IsPositive())
	require.True(t, incomplete.residualMint.LT(incomplete.output))
	require.Equal(t, complete.output, incomplete.output)
	require.Equal(t, complete.bufferPaid, incomplete.bufferPaid)
	require.Equal(t, complete.residualMint, incomplete.residualMint)
	require.True(t, complete.endingDelta.Equal(incomplete.endingDelta))
}

func runPhase3AIntegrationRedemption(t *testing.T, valuationComplete bool) phase3AIntegrationResult {
	t.Helper()

	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{
		Height: arkApp.LastBlockHeight(),
		Time:   time.Unix(1_800_000_000, 0),
	})
	trader := treasuryGovernanceVoter(t, arkApp, ctx)

	basePool := math.LegacyNewDec(1_000_000_000_000)
	initialDelta := math.LegacyMustNewDecFromStr("900000000000")
	capacity := markettypes.DefaultConversionPolicy()
	capacity.BasePool = sdk.NewDecCoinFromDec(chain.XDRBaseDenom, basePool)
	require.NoError(t, arkApp.MarketKeeper.ConversionPolicy.Set(ctx, capacity))
	require.NoError(t, arkApp.MarketKeeper.ArkPoolDelta.Set(ctx, initialDelta))

	// The reference unit denominates the base pool and is read by every quote,
	// but no asset is listed against it, so the traded stable is a listed one
	// carrying the same rate.
	for _, denom := range []string{chain.XDRBaseDenom, chain.USDBaseDenom} {
		require.NoError(t, arkApp.OracleKeeper.ExchangeRate.Set(ctx, denom, oracletypes.ExchangeRate{
			Denom:          denom,
			Rate:           math.LegacyOneDec(),
			BlockTimestamp: ctx.BlockTime(),
			BlockHeight:    uint64(ctx.BlockHeight()),
		}))
	}

	stableSupply := math.NewInt(1_000_000_000_000)
	apptestutil.FundAccount(t, arkApp, ctx, trader, sdk.NewCoins(sdk.NewCoin(chain.USDBaseDenom, stableSupply)))

	if !valuationComplete {
		missingRateSupply := sdk.NewInt64Coin(chain.KRWBaseDenom, 1)
		apptestutil.FundAccount(t, arkApp, ctx, trader, sdk.NewCoins(missingRateSupply))
	}

	// The mints above stand in for supply that existed before this block. They
	// bypass Market, and nothing has to be primed for them to count: settlement
	// values the registry at the end of the block, so it sees them the same way
	// it sees supply the block's own conversions created. In the incomplete case
	// that fold is also what excludes the rateless KRW dust from the claimable
	// aggregate and marks the valuation incomplete.
	bufferSeed := math.NewInt(250_000_000_000)
	apptestutil.FundModule(
		t,
		arkApp,
		ctx,
		treasurytypes.RedemptionBufferName,
		sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, bufferSeed)),
	)

	bufferAddress := authtypes.NewModuleAddress(treasurytypes.RedemptionBufferName)
	bufferBefore := arkApp.BankKeeper.GetBalance(ctx, bufferAddress, chain.NoahBaseDenom).Amount
	noahSupplyBefore := arkApp.BankKeeper.GetSupply(ctx, chain.NoahBaseDenom).Amount
	stableSupplyBefore := arkApp.BankKeeper.GetSupply(ctx, chain.USDBaseDenom).Amount

	totalOffer := math.NewInt(500_000_000_000)
	msgServer := marketkeeper.NewMsgServerImpl(arkApp.MarketKeeper)
	response, err := msgServer.Swap(ctx, &markettypes.MsgSwap{
		Trader:         trader.String(),
		OfferCoin:      sdk.NewCoin(chain.USDBaseDenom, totalOffer),
		AskDenom:       chain.NoahBaseDenom,
		MinimumReceive: sdk.NewInt64Coin(chain.NoahBaseDenom, 1),
	})
	require.NoError(t, err)
	totalOutput := response.SwapCoin.Amount

	// The delta the conversions themselves produced, read before the EndBlocker
	// replenishes the pools toward base.
	endingDelta, err := arkApp.MarketKeeper.ArkPoolDelta.Get(ctx)
	require.NoError(t, err)

	// The redemption above minted its whole output and drew nothing; the
	// Buffer funds it here, at the block's single coverage ratio.
	require.NoError(t, arkApp.MarketKeeper.EndBlocker(ctx))

	bufferAfter := arkApp.BankKeeper.GetBalance(ctx, bufferAddress, chain.NoahBaseDenom).Amount
	noahSupplyAfter := arkApp.BankKeeper.GetSupply(ctx, chain.NoahBaseDenom).Amount
	stableSupplyAfter := arkApp.BankKeeper.GetSupply(ctx, chain.USDBaseDenom).Amount

	bufferPaid := bufferBefore.Sub(bufferAfter)
	residualMint := noahSupplyAfter.Sub(noahSupplyBefore)
	require.Equal(t, totalOutput, bufferPaid.Add(residualMint))
	require.Equal(t, totalOffer, stableSupplyBefore.Sub(stableSupplyAfter))
	require.True(t, endingDelta.Equal(initialDelta.Add(math.LegacyNewDecFromInt(totalOffer))))

	return phase3AIntegrationResult{
		output:       totalOutput,
		bufferPaid:   bufferPaid,
		residualMint: residualMint,
		endingDelta:  endingDelta,
	}
}
