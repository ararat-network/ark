package integration

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/pkg/chain"
	claimstypes "github.com/ararat-network/ark/x/claims/types"
	marketkeeper "github.com/ararat-network/ark/x/market/keeper"
	markettypes "github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	reservetypes "github.com/ararat-network/ark/x/reserve/types"
	treasurykeeper "github.com/ararat-network/ark/x/treasury/keeper"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// settlementLedger is what a block's conversions left for settlement to place,
// gathered from the quotes the conversions actually produced rather than
// predicted from the pool.
type settlementLedger struct {
	eligible      math.Int
	spread        math.Int
	redeemedValue math.LegacyDec
	output        math.Int
}

// TestBlockSettlementMatchesSequentialPlacement is the parity check the cutover
// rests on. It drives a normal block — several expansions and several
// redemptions, interleaved — and then restates settlement independently from
// the quotes those conversions produced: value each output, waterfall the total
// against end-of-block liability, price coverage on the reconstructed pre-burn
// basis, and check every balance the block should have moved.
//
// The restatement is deliberately a second implementation rather than a call
// back into the keeper. What it proves is that placing the block's principal
// once, against one valuation, lands the same funds in the same accounts as
// walking the conversions in order would have — which is the claim that makes
// deferring settlement safe rather than merely cheaper.
func TestBlockSettlementMatchesSequentialPlacement(t *testing.T) {
	arkApp := app.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{
		Height: arkApp.LastBlockHeight(),
		Time:   time.Unix(1_800_000_000, 0),
	})
	trader := treasuryGovernanceVoter(t, arkApp, ctx)

	// One NOAH per unit of the traded stable keeps the arithmetic below exact,
	// so a parity failure is a settlement bug and never a rounding artefact.
	for _, denom := range []string{chain.XDRBaseDenom, chain.USDBaseDenom} {
		require.NoError(t, arkApp.OracleKeeper.ExchangeRate.Set(ctx, denom, oracletypes.ExchangeRate{
			Denom:          denom,
			Rate:           math.LegacyOneDec(),
			BlockTimestamp: ctx.BlockTime(),
			BlockHeight:    uint64(ctx.BlockHeight()),
		}))
	}

	capacity := markettypes.DefaultConversionPolicy()
	capacity.BasePool = sdk.NewDecCoinFromDec(chain.XDRBaseDenom, math.LegacyNewDec(1_000_000_000_000))
	require.NoError(t, arkApp.MarketKeeper.ConversionPolicy.Set(ctx, capacity))
	require.NoError(t, arkApp.MarketKeeper.ArkPoolDelta.Set(ctx, math.LegacyZeroDec()))

	// Ratios chosen so the waterfall binds in stages: the Buffer gap absorbs
	// part of the principal, the Reserve takes some of the rest, and Insurance
	// is small enough that a remainder overflows into the burn.
	policy, err := arkApp.TreasuryKeeper.MonetaryPolicy.Get(ctx)
	require.NoError(t, err)
	policy.RedemptionBufferTargetRatio = math.LegacyMustNewDecFromStr("0.02")
	policy.StrategicReserveTargetRatio = math.LegacyMustNewDecFromStr("0.01")
	policy.InsuranceTargetRatio = math.LegacyMustNewDecFromStr("0.005")
	require.NoError(t, arkApp.TreasuryKeeper.MonetaryPolicy.Set(ctx, policy))

	stableSupply := math.NewInt(500_000_000_000)
	require.NoError(t, arkApp.BankKeeper.MintCoins(
		ctx,
		markettypes.ModuleName,
		sdk.NewCoins(sdk.NewCoin(chain.USDBaseDenom, stableSupply)),
	))
	require.NoError(t, arkApp.BankKeeper.SendCoinsFromModuleToAccount(
		ctx,
		markettypes.ModuleName,
		trader,
		sdk.NewCoins(sdk.NewCoin(chain.USDBaseDenom, stableSupply)),
	))

	bufferSeed := math.NewInt(1_000_000_000)
	require.NoError(t, arkApp.BankKeeper.MintCoins(
		ctx,
		markettypes.ModuleName,
		sdk.NewCoins(chain.NoahCoin(bufferSeed)),
	))
	require.NoError(t, arkApp.BankKeeper.SendCoinsFromModuleToModule(
		ctx,
		markettypes.ModuleName,
		treasurytypes.RedemptionBufferName,
		sdk.NewCoins(chain.NoahCoin(bufferSeed)),
	))

	bufferAddress := authtypes.NewModuleAddress(treasurytypes.RedemptionBufferName)
	reserveAddress := authtypes.NewModuleAddress(reservetypes.StrategicReserveName)
	insuranceAddress := authtypes.NewModuleAddress(claimstypes.InsuranceName)
	marketAddress := authtypes.NewModuleAddress(markettypes.ModuleName)

	balanceOf := func(addr sdk.AccAddress) math.Int {
		return arkApp.BankKeeper.GetBalance(ctx, addr, chain.NoahBaseDenom).Amount
	}

	bufferBefore := balanceOf(bufferAddress)
	reserveBefore := balanceOf(reserveAddress)
	insuranceBefore := balanceOf(insuranceAddress)
	marketBefore := balanceOf(marketAddress)
	noahSupplyBefore := arkApp.BankKeeper.GetSupply(ctx, chain.NoahBaseDenom).Amount

	// Interleaved, so the block cannot be settled correctly by accident from a
	// path that only ever saw one direction.
	conversions := []struct {
		expansion bool
		amount    int64
	}{
		{expansion: true, amount: 30_000_000_000},
		{expansion: false, amount: 20_000_000_000},
		{expansion: true, amount: 12_000_000_000},
		{expansion: false, amount: 8_000_000_000},
		{expansion: true, amount: 5_000_000_000},
	}

	msgServer := marketkeeper.NewMsgServerImpl(arkApp.MarketKeeper)
	ledger := settlementLedger{
		eligible:      math.ZeroInt(),
		spread:        math.ZeroInt(),
		redeemedValue: math.LegacyZeroDec(),
		output:        math.ZeroInt(),
	}
	for _, conversion := range conversions {
		offerDenom, askDenom := chain.USDBaseDenom, chain.NoahBaseDenom
		if conversion.expansion {
			offerDenom, askDenom = chain.NoahBaseDenom, chain.USDBaseDenom
		}
		offer := sdk.NewCoin(offerDenom, math.NewInt(conversion.amount))
		noahBefore := balanceOf(marketAddress)

		response, err := msgServer.Swap(ctx, &markettypes.MsgSwap{
			Trader:         trader.String(),
			OfferCoin:      offer,
			AskDenom:       askDenom,
			MinimumReceive: sdk.NewCoin(askDenom, math.OneInt()),
		})
		require.NoError(t, err)
		require.True(t, response.SwapCoin.IsPositive())

		if conversion.expansion {
			// At one NOAH per unit the output values to itself, and the offer
			// less that value is the spread, which settlement places with the
			// principal rather than the conversion burning it (D6).
			eligible := response.SwapCoin.Amount
			spread := offer.Amount.Sub(eligible)
			require.True(t, spread.IsPositive(), "the fixture's spread must be observable")
			ledger.eligible = ledger.eligible.Add(eligible)
			ledger.spread = ledger.spread.Add(spread)

			// The whole offer stayed in Market custody; nothing left.
			require.Equal(
				t,
				noahBefore.Add(offer.Amount),
				balanceOf(marketAddress),
				"an expansion must retain its whole offer for settlement",
			)
			continue
		}

		ledger.output = ledger.output.Add(response.SwapCoin.Amount)
		ledger.redeemedValue = ledger.redeemedValue.Add(math.LegacyNewDecFromInt(offer.Amount))
		// The whole quoted output was minted to the trader; nothing was drawn.
		require.Equal(t, bufferBefore, balanceOf(bufferAddress),
			"a redemption must not touch the Buffer inside the conversion")
	}

	require.True(t, ledger.eligible.IsPositive())
	require.True(t, ledger.output.IsPositive())
	// Market holds exactly the block's gross offers above where it started.
	gross := ledger.eligible.Add(ledger.spread)
	require.Equal(t, marketBefore.Add(gross), balanceOf(marketAddress))

	// Restate settlement independently, from end-of-block state.
	liability := math.LegacyNewDecFromInt(
		arkApp.BankKeeper.GetSupply(ctx, chain.USDBaseDenom).Amount,
	)
	target := func(ratio math.LegacyDec) math.Int {
		return ratio.MulRoundUp(liability).Ceil().TruncateInt()
	}
	gap := func(target, balance math.Int) math.Int {
		if target.LTE(balance) {
			return math.ZeroInt()
		}
		return target.Sub(balance)
	}
	insuranceReserved, err := arkApp.ClaimsKeeper.InsuranceReserved.Get(ctx)
	require.NoError(t, err)

	remaining := gross
	bufferCredit := math.MinInt(remaining, gap(target(policy.RedemptionBufferTargetRatio), bufferBefore))
	remaining = remaining.Sub(bufferCredit)
	reserveCredit := math.MinInt(remaining, gap(target(policy.StrategicReserveTargetRatio), reserveBefore))
	remaining = remaining.Sub(reserveCredit)
	insuranceCredit := math.MinInt(
		remaining,
		gap(target(policy.InsuranceTargetRatio), insuranceBefore.Sub(insuranceReserved)),
	)
	remaining = remaining.Sub(insuranceCredit)
	overflowBurn := remaining

	// Coverage prices on what claims existed when the redemptions quoted: the
	// supply still outstanding plus the value this block retired.
	basis := liability.Add(ledger.redeemedValue)
	// Multiplied before divided, mirroring the specified semantics: an exact
	// integer product, one rounding on the quotient, capped at the output.
	bufferAtDraw := bufferBefore.Add(bufferCredit)
	draw := math.MinInt(
		ledger.output,
		math.LegacyNewDecFromInt(ledger.output.Mul(bufferAtDraw)).Quo(basis).TruncateInt(),
	)

	require.NoError(t, arkApp.MarketKeeper.EndBlocker(ctx))

	// Every fund landed where the restatement says it should.
	require.Equal(t, bufferBefore.Add(bufferCredit).Sub(draw), balanceOf(bufferAddress))
	require.Equal(t, reserveBefore.Add(reserveCredit), balanceOf(reserveAddress))
	require.Equal(t, insuranceBefore.Add(insuranceCredit), balanceOf(insuranceAddress))
	// Market ends where it began: the principal was placed or burned, and the
	// coverage it received was burned against output already minted.
	require.Equal(t, marketBefore, balanceOf(marketAddress))

	// Supply moved by exactly the mints and burns the block performed: the
	// redemption output minted inside the conversions, less what settlement
	// burned. Expansions burn nothing on the spot.
	require.Equal(
		t,
		ledger.output.Sub(overflowBurn).Sub(draw),
		arkApp.BankKeeper.GetSupply(ctx, chain.NoahBaseDenom).Amount.Sub(noahSupplyBefore),
	)

	// The invariants the placement must satisfy however the numbers fall.
	require.True(t, bufferCredit.Add(reserveCredit).Add(insuranceCredit).LTE(gross),
		"credits cannot exceed the gross offer there was to place")
	require.True(t, draw.LTE(bufferBefore.Add(bufferCredit)),
		"the draw cannot exceed the Buffer it is paid from")
	require.True(t, draw.LTE(ledger.output),
		"coverage is a share of the output and never more than all of it")
}

// TestBlockSettlementParksEveryConversionOnMidBlockDegradation pins the scope
// the deferred design gives the degraded mode. A feed going dark part-way
// through a block used to affect only the conversions that came after it: the
// ones before had already sized targets and burned their remainder against a
// valuation that still called itself complete. Settling once means the block
// has one verdict, so every conversion in it — including those quoted while the
// feed was still live — parks in the Reserve, the fund whose allocation an
// operator can still revise.
//
// The coverage draw is deliberately not switched off with it. The aggregate
// already excludes supply that cannot redeem, so a failure elsewhere raises the
// share healthy exits receive rather than closing the exit during the contagion
// the Buffer exists for.
func TestBlockSettlementParksEveryConversionOnMidBlockDegradation(t *testing.T) {
	arkApp := app.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{
		Height: arkApp.LastBlockHeight(),
		Time:   time.Unix(1_800_000_000, 0),
	})
	trader := treasuryGovernanceVoter(t, arkApp, ctx)

	for _, denom := range []string{chain.XDRBaseDenom, chain.USDBaseDenom, chain.KRWBaseDenom} {
		require.NoError(t, arkApp.OracleKeeper.ExchangeRate.Set(ctx, denom, oracletypes.ExchangeRate{
			Denom:          denom,
			Rate:           math.LegacyOneDec(),
			BlockTimestamp: ctx.BlockTime(),
			BlockHeight:    uint64(ctx.BlockHeight()),
		}))
	}

	capacity := markettypes.DefaultConversionPolicy()
	capacity.BasePool = sdk.NewDecCoinFromDec(chain.XDRBaseDenom, math.LegacyNewDec(1_000_000_000_000))
	require.NoError(t, arkApp.MarketKeeper.ConversionPolicy.Set(ctx, capacity))
	require.NoError(t, arkApp.MarketKeeper.ArkPoolDelta.Set(ctx, math.LegacyZeroDec()))

	for _, seed := range []struct {
		denom  string
		amount int64
	}{
		{chain.USDBaseDenom, 200_000_000_000},
		// akrw is the member that will lose its feed. It never trades here; it
		// is only outstanding liability the fold has to account for.
		{chain.KRWBaseDenom, 50_000_000_000},
	} {
		coins := sdk.NewCoins(sdk.NewInt64Coin(seed.denom, seed.amount))
		require.NoError(t, arkApp.BankKeeper.MintCoins(ctx, markettypes.ModuleName, coins))
		require.NoError(t, arkApp.BankKeeper.SendCoinsFromModuleToAccount(
			ctx, markettypes.ModuleName, trader, coins,
		))
	}

	bufferSeed := math.NewInt(500_000_000_000)
	require.NoError(t, arkApp.BankKeeper.MintCoins(
		ctx, markettypes.ModuleName, sdk.NewCoins(chain.NoahCoin(bufferSeed)),
	))
	require.NoError(t, arkApp.BankKeeper.SendCoinsFromModuleToModule(
		ctx, markettypes.ModuleName, treasurytypes.RedemptionBufferName,
		sdk.NewCoins(chain.NoahCoin(bufferSeed)),
	))

	bufferAddress := authtypes.NewModuleAddress(treasurytypes.RedemptionBufferName)
	reserveAddress := authtypes.NewModuleAddress(reservetypes.StrategicReserveName)
	balanceOf := func(addr sdk.AccAddress) math.Int {
		return arkApp.BankKeeper.GetBalance(ctx, addr, chain.NoahBaseDenom).Amount
	}
	bufferBefore := balanceOf(bufferAddress)
	reserveBefore := balanceOf(reserveAddress)

	msgServer := marketkeeper.NewMsgServerImpl(arkApp.MarketKeeper)
	swap := func(offer sdk.Coin, askDenom string) math.Int {
		response, err := msgServer.Swap(ctx, &markettypes.MsgSwap{
			Trader:         trader.String(),
			OfferCoin:      offer,
			AskDenom:       askDenom,
			MinimumReceive: sdk.NewCoin(askDenom, math.OneInt()),
		})
		require.NoError(t, err)
		return response.SwapCoin.Amount
	}

	// Quoted while every member is priced.
	healthyOffer := chain.NoahCoin(math.NewInt(30_000_000_000))
	swap(healthyOffer, chain.USDBaseDenom)
	redeemedOutput := swap(sdk.NewInt64Coin(chain.USDBaseDenom, 20_000_000_000), chain.NoahBaseDenom)

	// The feed goes dark part-way through the block. akrw was never quoted here
	// and is not quoted after, so nothing about the conversions changes — only
	// what the block can say about its own liability.
	require.NoError(t, arkApp.OracleKeeper.ExchangeRate.Remove(ctx, chain.KRWBaseDenom))

	degradedOffer := chain.NoahCoin(math.NewInt(10_000_000_000))
	swap(degradedOffer, chain.USDBaseDenom)

	eventsBefore := len(ctx.EventManager().Events())
	require.NoError(t, arkApp.MarketKeeper.EndBlocker(ctx))

	// Both expansions parked, not just the one quoted after the feed dropped.
	// Nothing reached the Buffer, which is the irreversible commitment the
	// degraded branch exists to withhold.
	parked := balanceOf(reserveAddress).Sub(reserveBefore)
	require.Equal(t, healthyOffer.Amount.Add(degradedOffer.Amount), parked,
		"an incomplete valuation parks the whole block's gross offer")

	// The draw ran anyway, and the Buffer only ever paid coverage: it received
	// no credit, so its whole movement is the redemption it funded.
	drawn := bufferBefore.Sub(balanceOf(bufferAddress))
	require.True(t, drawn.IsPositive(), "coverage must survive a failure elsewhere")
	require.True(t, drawn.LTE(redeemedOutput), "the draw is a share of the output")

	var allocations, disclosures int
	for _, event := range ctx.EventManager().Events()[eventsBefore:] {
		switch event.Type {
		case "ark.treasury.v1.EventExpansionAllocated":
			allocations++
		case "ark.treasury.v1.EventLiabilityIncomplete":
			disclosures++
			for _, attribute := range event.Attributes {
				if attribute.Key == "stale_member_supply" {
					require.Contains(t, attribute.Value, chain.KRWBaseDenom)
				}
			}
		}
	}
	require.Equal(t, 1, allocations, "the block allocates once, however many conversions it held")
	require.Equal(t, 1, disclosures, "one fold, one disclosure")
}

func TestMarketSettlementLateFailureRollsBackByDirection(t *testing.T) {
	tests := []struct {
		name      string
		offerCoin sdk.Coin
		askDenom  string
	}{
		{
			name:      "noah to stable expansion",
			offerCoin: sdk.NewInt64Coin(chain.NoahBaseDenom, 1_000_000),
			askDenom:  chain.EURBaseDenom,
		},
		{
			name:      "stable to noah redemption",
			offerCoin: sdk.NewInt64Coin(chain.EURBaseDenom, 1_000_000),
			askDenom:  chain.NoahBaseDenom,
		},
		{
			name:      "stable to stable conversion",
			offerCoin: sdk.NewInt64Coin(chain.EURBaseDenom, 1_000_000),
			askDenom:  chain.USDBaseDenom,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			arkApp := app.Setup(t, false)
			ctx := arkApp.NewContextLegacy(false, cmtproto.Header{
				Height: arkApp.LastBlockHeight(),
				Time:   time.Unix(1_800_000_000, 0),
			})
			trader := treasuryGovernanceVoter(t, arkApp, ctx)

			// The reference unit is never traded here, but it denominates
			// Market's base pool, so every quote still reads its rate.
			for _, denom := range []string{
				chain.XDRBaseDenom,
				chain.EURBaseDenom,
				chain.USDBaseDenom,
			} {
				require.NoError(t, arkApp.OracleKeeper.ExchangeRate.Set(ctx, denom, oracletypes.ExchangeRate{
					Denom:          denom,
					Rate:           math.LegacyOneDec(),
					BlockTimestamp: ctx.BlockTime(),
					BlockHeight:    uint64(ctx.BlockHeight()),
				}))
			}

			require.NoError(t, arkApp.BankKeeper.MintCoins(
				ctx,
				markettypes.ModuleName,
				sdk.NewCoins(test.offerCoin),
			))
			require.NoError(t, arkApp.BankKeeper.SendCoinsFromModuleToAccount(
				ctx,
				markettypes.ModuleName,
				trader,
				sdk.NewCoins(test.offerCoin),
			))
			// The mint above stands in for supply that existed before this
			// block. It bypasses Market and needs no priming: the block's
			// valuation is folded at settlement, from final state.
			treasuryQuery := treasurykeeper.NewQueryServerImpl(arkApp.TreasuryKeeper)
			before := captureMarketSettlementState(t, arkApp, ctx, trader, treasuryQuery)
			beforeEventCount := len(ctx.EventManager().Events())

			blockedRecipient := authtypes.NewModuleAddress(authtypes.FeeCollectorName)
			require.True(t, arkApp.BankKeeper.BlockedAddr(blockedRecipient))
			cacheCtx, _ := ctx.CacheContext()
			_, err := marketkeeper.NewMsgServerImpl(arkApp.MarketKeeper).SwapSend(cacheCtx, &markettypes.MsgSwapSend{
				FromAddress:    trader.String(),
				ToAddress:      blockedRecipient.String(),
				OfferCoin:      test.offerCoin,
				AskDenom:       test.askDenom,
				MinimumReceive: sdk.NewInt64Coin(test.askDenom, 1),
			})
			require.ErrorContains(t, err, "is not allowed to receive funds")

			cached := captureMarketSettlementState(t, arkApp, cacheCtx, trader, treasuryQuery)
			require.NotEqual(t, before, cached, "failure must occur after settlement has changed cached state")
			require.NotEmpty(t, cacheCtx.EventManager().Events())

			after := captureMarketSettlementState(t, arkApp, ctx, trader, treasuryQuery)
			require.Equal(t, before, after)
			require.Len(t, ctx.EventManager().Events(), beforeEventCount)
		})
	}
}

type marketSettlementState struct {
	traderBalance   sdk.Coins
	marketBalance   sdk.Coins
	fundBalances    []sdk.Coins
	noahSupply      math.Int
	eurSupply       math.Int
	usdSupply       math.Int
	arkPoolDelta    math.LegacyDec
	marketParams    markettypes.Params
	liability       sdk.DecCoin
	bufferTarget    sdk.Coin
	reserveTarget   sdk.Coin
	insuranceTarget sdk.Coin
}

func captureMarketSettlementState(
	t *testing.T,
	arkApp *app.ArkApp,
	ctx sdk.Context,
	trader sdk.AccAddress,
	treasuryQuery treasurytypes.QueryServer,
) marketSettlementState {
	t.Helper()

	params, err := arkApp.MarketKeeper.Params.Get(ctx)
	require.NoError(t, err)
	delta, err := arkApp.MarketKeeper.ArkPoolDelta.Get(ctx)
	require.NoError(t, err)
	fundStatus, err := treasuryQuery.FundStatus(ctx, &treasurytypes.QueryFundStatusRequest{})
	require.NoError(t, err)

	fundBalances := make([]sdk.Coins, 0, len(treasurytypes.FundAccountNames()))
	for _, moduleName := range treasurytypes.FundAccountNames() {
		fundBalances = append(fundBalances, arkApp.BankKeeper.GetAllBalances(
			ctx,
			authtypes.NewModuleAddress(moduleName),
		))
	}

	return marketSettlementState{
		traderBalance:   arkApp.BankKeeper.GetAllBalances(ctx, trader),
		marketBalance:   arkApp.BankKeeper.GetAllBalances(ctx, authtypes.NewModuleAddress(markettypes.ModuleName)),
		fundBalances:    fundBalances,
		noahSupply:      arkApp.BankKeeper.GetSupply(ctx, chain.NoahBaseDenom).Amount,
		eurSupply:       arkApp.BankKeeper.GetSupply(ctx, chain.EURBaseDenom).Amount,
		usdSupply:       arkApp.BankKeeper.GetSupply(ctx, chain.USDBaseDenom).Amount,
		arkPoolDelta:    delta,
		marketParams:    params,
		liability:       fundStatus.NominalLiability,
		bufferTarget:    fundStatus.RedemptionBufferTarget,
		reserveTarget:   fundStatus.StrategicReserveTarget,
		insuranceTarget: fundStatus.InsuranceTarget,
	}
}
