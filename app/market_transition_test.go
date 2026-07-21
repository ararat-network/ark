package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"ark/pkg/chain"
	marketkeeper "ark/x/market/keeper"
	markettypes "ark/x/market/types"
	oracletypes "ark/x/oracle/types"
)

func TestMarketPoolDenomTransitionPreservesQuotesAndUSDRSupport(t *testing.T) {
	arkApp := Setup(t, false)
	ctx := arkApp.NewNextBlockContext(cmtproto.Header{
		Height: arkApp.LastBlockHeight() + 1,
		Time:   time.Unix(1_800_000_000, 0),
	})

	for denom, rate := range map[string]math.LegacyDec{
		chain.MicroSDRDenom: math.LegacyOneDec(),
		chain.MicroUSDDenom: math.LegacyNewDec(2),
	} {
		require.NoError(t, arkApp.OracleKeeper.ExchangeRate.Set(ctx, denom, oracletypes.ExchangeRate{
			Denom:          denom,
			Rate:           rate,
			BlockTimestamp: ctx.BlockTime(),
			BlockHeight:    uint64(ctx.BlockHeight()),
		}))
	}

	current := markettypes.DefaultParams()
	current.BasePool = sdk.NewDecCoin(chain.MicroSDRDenom, math.NewInt(1_000_000_000_000))
	require.NoError(t, arkApp.MarketKeeper.Params.Set(ctx, current))
	oldDelta := math.LegacyNewDec(250_000_000_000)
	require.NoError(t, arkApp.MarketKeeper.ArkPoolDelta.Set(ctx, oldDelta))

	stableOffer := sdk.NewInt64Coin(chain.MicroUSDDenom, 1_000_000)
	queryServer := marketkeeper.NewQueryServerImpl(arkApp.MarketKeeper)
	beforeQuote, err := queryServer.Swap(ctx, &markettypes.QuerySwapRequest{
		OfferCoin: stableOffer.String(),
		AskDenom:  chain.MicroSDRDenom,
	})
	require.NoError(t, err)

	submitted := current
	submitted.BasePool = sdk.NewDecCoinFromDec(chain.MicroUSDDenom, math.LegacySmallestDec())
	msgServer := marketkeeper.NewMsgServerImpl(arkApp.MarketKeeper)
	_, err = msgServer.UpdateParams(ctx, &markettypes.MsgUpdateParams{
		Authority: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		Params:    submitted,
	})
	require.NoError(t, err)

	stored, err := arkApp.MarketKeeper.Params.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, chain.MicroUSDDenom, stored.BasePool.Denom)
	require.True(t, math.LegacyNewDec(2_000_000_000_000).Equal(stored.BasePool.Amount))
	newDelta, err := arkApp.MarketKeeper.ArkPoolDelta.Get(ctx)
	require.NoError(t, err)
	require.True(t, math.LegacyNewDec(500_000_000_000).Equal(newDelta))

	deltaResponse, err := queryServer.ArkPoolDelta(ctx, &markettypes.QueryArkPoolDeltaRequest{})
	require.NoError(t, err)
	require.Equal(t, chain.MicroUSDDenom, deltaResponse.PoolDenom)
	require.True(t, newDelta.Equal(deltaResponse.ArkPoolDelta))

	afterQuote, err := queryServer.Swap(ctx, &markettypes.QuerySwapRequest{
		OfferCoin: stableOffer.String(),
		AskDenom:  chain.MicroSDRDenom,
	})
	require.NoError(t, err)
	require.Equal(t, beforeQuote, afterQuote)

	trader := treasuryGovernanceVoter(t, arkApp, ctx)
	noahOffer := sdk.NewInt64Coin(chain.MicroNoahDenom, 1_000_000)
	require.NoError(t, arkApp.BankKeeper.MintCoins(
		ctx,
		markettypes.ModuleName,
		sdk.NewCoins(noahOffer),
	))
	require.NoError(t, arkApp.BankKeeper.SendCoinsFromModuleToAccount(
		ctx,
		markettypes.ModuleName,
		trader,
		sdk.NewCoins(noahOffer),
	))

	response, err := msgServer.Swap(ctx, &markettypes.MsgSwap{
		Trader:         trader.String(),
		OfferCoin:      noahOffer,
		AskDenom:       chain.MicroSDRDenom,
		MinimumReceive: sdk.NewInt64Coin(chain.MicroSDRDenom, 1),
	})
	require.NoError(t, err)
	require.Equal(t, chain.MicroSDRDenom, response.SwapCoin.Denom)
	require.True(t, response.SwapCoin.IsPositive())
	require.Equal(
		t,
		response.SwapCoin.Amount,
		arkApp.BankKeeper.GetBalance(ctx, trader, chain.MicroSDRDenom).Amount,
	)
	finalDelta, err := arkApp.MarketKeeper.ArkPoolDelta.Get(ctx)
	require.NoError(t, err)

	arkApp.SimWriteState()
	_, err = arkApp.Commit()
	require.NoError(t, err)
	exported, err := arkApp.ExportAppStateAndValidators(false, nil, nil)
	require.NoError(t, err)
	var appState GenesisState
	require.NoError(t, json.Unmarshal(exported.AppState, &appState))
	var exportedMarket markettypes.GenesisState
	arkApp.appCodec.MustUnmarshalJSON(appState[markettypes.ModuleName], &exportedMarket)
	require.Equal(t, stored.BasePool, exportedMarket.Params.BasePool)
	require.True(t, finalDelta.Equal(exportedMarket.ArkPoolDelta))
}
