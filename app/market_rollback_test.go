package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"ark/pkg/chain"
	marketkeeper "ark/x/market/keeper"
	markettypes "ark/x/market/types"
	oracletypes "ark/x/oracle/types"
	treasurykeeper "ark/x/treasury/keeper"
	treasurytypes "ark/x/treasury/types"
)

func TestMarketSettlementLateFailureRollsBackByDirection(t *testing.T) {
	tests := []struct {
		name      string
		offerCoin sdk.Coin
		askDenom  string
	}{
		{
			name:      "noah to stable expansion",
			offerCoin: sdk.NewInt64Coin(chain.NoahBaseDenom, 1_000_000),
			askDenom:  chain.SDRBaseDenom,
		},
		{
			name:      "stable to noah redemption",
			offerCoin: sdk.NewInt64Coin(chain.SDRBaseDenom, 1_000_000),
			askDenom:  chain.NoahBaseDenom,
		},
		{
			name:      "stable to stable conversion",
			offerCoin: sdk.NewInt64Coin(chain.SDRBaseDenom, 1_000_000),
			askDenom:  chain.USDBaseDenom,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			arkApp := Setup(t, false)
			ctx := arkApp.NewContextLegacy(false, cmtproto.Header{
				Height: arkApp.LastBlockHeight(),
				Time:   time.Unix(1_800_000_000, 0),
			})
			trader := treasuryGovernanceVoter(t, arkApp, ctx)

			for _, denom := range []string{chain.SDRBaseDenom, chain.USDBaseDenom} {
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
	sdrSupply       math.Int
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
	arkApp *ArkApp,
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
		sdrSupply:       arkApp.BankKeeper.GetSupply(ctx, chain.SDRBaseDenom).Amount,
		usdSupply:       arkApp.BankKeeper.GetSupply(ctx, chain.USDBaseDenom).Amount,
		arkPoolDelta:    delta,
		marketParams:    params,
		liability:       fundStatus.NominalLiabilityNoahEquivalent,
		bufferTarget:    fundStatus.RedemptionBufferTarget,
		reserveTarget:   fundStatus.StrategicReserveTarget,
		insuranceTarget: fundStatus.InsuranceTarget,
	}
}
