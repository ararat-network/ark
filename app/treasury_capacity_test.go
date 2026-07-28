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
	treasurytypes "ark/x/treasury/types"
)

type phase3AIntegrationResult struct {
	output       math.Int
	bufferPaid   math.Int
	residualMint math.Int
	endingDelta  math.LegacyDec
}

func TestPhase3ACapacityIntegrationSplitDoesNotIncreaseResidualMint(t *testing.T) {
	unsplit := runPhase3AIntegrationRedemption(t, 1, true)
	split := runPhase3AIntegrationRedemption(t, 10, true)

	require.Equal(t, math.NewInt(109_649_122_807), unsplit.output)
	require.Equal(t, math.NewInt(109_649_122_803), split.output)
	require.True(t, split.output.LT(unsplit.output))
	require.True(t, split.residualMint.LTE(unsplit.residualMint))
	require.True(t, split.endingDelta.Equal(unsplit.endingDelta))
	t.Logf(
		"output coverage: unsplit_buffer=%s unsplit_residual=%s split_buffer=%s split_residual=%s",
		unsplit.bufferPaid,
		unsplit.residualMint,
		split.bufferPaid,
		split.residualMint,
	)
}

func TestPhase3ACapacityIntegrationIncompleteValuationMintsFullOutput(t *testing.T) {
	result := runPhase3AIntegrationRedemption(t, 1, false)
	require.True(t, result.output.IsPositive())
	require.True(t, result.bufferPaid.IsZero())
	require.Equal(t, result.output, result.residualMint)
}

func runPhase3AIntegrationRedemption(
	t *testing.T,
	splitCount int,
	valuationComplete bool,
) phase3AIntegrationResult {
	t.Helper()

	arkApp := Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{
		Height: arkApp.LastBlockHeight(),
		Time:   time.Unix(1_800_000_000, 0),
	})
	trader := treasuryGovernanceVoter(t, arkApp, ctx)

	basePool := math.LegacyNewDec(1_000_000_000_000)
	initialDelta := math.LegacyMustNewDecFromStr("900000000000")
	params := markettypes.DefaultParams()
	params.BasePool = sdk.NewDecCoinFromDec(chain.SDRBaseDenom, basePool)
	require.NoError(t, arkApp.MarketKeeper.Params.Set(ctx, params))
	require.NoError(t, arkApp.MarketKeeper.ArkPoolDelta.Set(ctx, initialDelta))

	require.NoError(t, arkApp.OracleKeeper.ExchangeRate.Set(ctx, chain.SDRBaseDenom, oracletypes.ExchangeRate{
		Denom:          chain.SDRBaseDenom,
		Rate:           math.LegacyOneDec(),
		BlockTimestamp: ctx.BlockTime(),
		BlockHeight:    uint64(ctx.BlockHeight()),
	}))

	stableSupply := math.NewInt(1_000_000_000_000)
	require.NoError(t, arkApp.BankKeeper.MintCoins(
		ctx,
		markettypes.ModuleName,
		sdk.NewCoins(sdk.NewCoin(chain.SDRBaseDenom, stableSupply)),
	))
	require.NoError(t, arkApp.BankKeeper.SendCoinsFromModuleToAccount(
		ctx,
		markettypes.ModuleName,
		trader,
		sdk.NewCoins(sdk.NewCoin(chain.SDRBaseDenom, stableSupply)),
	))

	if !valuationComplete {
		missingRateSupply := sdk.NewInt64Coin(chain.KRWBaseDenom, 1)
		require.NoError(t, arkApp.BankKeeper.MintCoins(
			ctx,
			markettypes.ModuleName,
			sdk.NewCoins(missingRateSupply),
		))
		require.NoError(t, arkApp.BankKeeper.SendCoinsFromModuleToAccount(
			ctx,
			markettypes.ModuleName,
			trader,
			sdk.NewCoins(missingRateSupply),
		))
	}

	// The mints above stand in for supply that existed before this block. They
	// bypass Market, so they never reach RecordSupplyChange; re-prime the block
	// snapshot the way the preblocker would have at a real block start. In the
	// incomplete case this is what records the missing KRW rate as an
	// unavailable valuation for the whole block.
	require.NoError(t, arkApp.TreasuryKeeper.PrimeLiabilitySnapshot(ctx))

	bufferSeed := math.NewInt(250_000_000_000)
	require.NoError(t, arkApp.BankKeeper.MintCoins(
		ctx,
		markettypes.ModuleName,
		sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, bufferSeed)),
	))
	require.NoError(t, arkApp.BankKeeper.SendCoinsFromModuleToModule(
		ctx,
		markettypes.ModuleName,
		treasurytypes.RedemptionBufferName,
		sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, bufferSeed)),
	))

	bufferAddress := authtypes.NewModuleAddress(treasurytypes.RedemptionBufferName)
	bufferBefore := arkApp.BankKeeper.GetBalance(ctx, bufferAddress, chain.NoahBaseDenom).Amount
	noahSupplyBefore := arkApp.BankKeeper.GetSupply(ctx, chain.NoahBaseDenom).Amount
	stableSupplyBefore := arkApp.BankKeeper.GetSupply(ctx, chain.SDRBaseDenom).Amount

	totalOffer := math.NewInt(500_000_000_000)
	chunk := totalOffer.QuoRaw(int64(splitCount))
	msgServer := marketkeeper.NewMsgServerImpl(arkApp.MarketKeeper)
	totalOutput := math.ZeroInt()
	for i := 0; i < splitCount; i++ {
		response, err := msgServer.Swap(ctx, &markettypes.MsgSwap{
			Trader:         trader.String(),
			OfferCoin:      sdk.NewCoin(chain.SDRBaseDenom, chunk),
			AskDenom:       chain.NoahBaseDenom,
			MinimumReceive: sdk.NewInt64Coin(chain.NoahBaseDenom, 1),
		})
		require.NoError(t, err)
		totalOutput = totalOutput.Add(response.SwapCoin.Amount)
	}

	bufferAfter := arkApp.BankKeeper.GetBalance(ctx, bufferAddress, chain.NoahBaseDenom).Amount
	noahSupplyAfter := arkApp.BankKeeper.GetSupply(ctx, chain.NoahBaseDenom).Amount
	stableSupplyAfter := arkApp.BankKeeper.GetSupply(ctx, chain.SDRBaseDenom).Amount
	endingDelta, err := arkApp.MarketKeeper.ArkPoolDelta.Get(ctx)
	require.NoError(t, err)

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
