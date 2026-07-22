package keeper

import (
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	codectestutil "github.com/cosmos/cosmos-sdk/codec/testutil"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/std"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdktestutil "github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"go.uber.org/mock/gomock"

	markettestutil "ark/x/market/testutil"
	markettypes "ark/x/market/types"
	oraclekeeper "ark/x/oracle/keeper"
	oracletestutil "ark/x/oracle/testutil"
	oracletypes "ark/x/oracle/types"
)

var benchmarkSwapQuote swapQuote

func BenchmarkStableToStableQuote(b *testing.B) {
	for _, targetCount := range []int{len(oracletypes.DefaultTobinTaxes), oracletypes.MaxVoteTargets} {
		b.Run(fmt.Sprintf("targets_%d", targetCount), func(b *testing.B) {
			keeper, ctx, offerCoin, askDenom := benchmarkMarketKeeper(b, targetCount)
			if _, err := keeper.quoteSwap(ctx, offerCoin, askDenom); err != nil {
				b.Fatal(err)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				quote, err := keeper.quoteSwap(ctx, offerCoin, askDenom)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkSwapQuote = quote
			}
		})
	}
}

func benchmarkMarketKeeper(
	b *testing.B,
	targetCount int,
) (*Keeper, sdk.Context, sdk.Coin, string) {
	b.Helper()

	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	markettypes.RegisterInterfaces(interfaceRegistry)
	oracletypes.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)

	marketKey := storetypes.NewKVStoreKey(markettypes.StoreKey)
	oracleKey := storetypes.NewKVStoreKey(oracletypes.StoreKey)
	ctx := sdktestutil.DefaultContextWithKeys(
		map[string]*storetypes.KVStoreKey{
			markettypes.StoreKey: marketKey,
			oracletypes.StoreKey: oracleKey,
		},
		nil,
		nil,
	).WithBlockHeight(2).WithBlockTime(time.Unix(1, 0))

	ctrl := gomock.NewController(b)
	oracleAccountKeeper := oracletestutil.NewMockAccountKeeper(ctrl)
	oracleAccountKeeper.EXPECT().GetModuleAddress(oracletypes.ModuleName).
		Return(authtypes.NewModuleAddress(oracletypes.ModuleName))
	oracleAccountKeeper.EXPECT().GetModuleAddress(distrtypes.ModuleName).
		Return(authtypes.NewModuleAddress(distrtypes.ModuleName))
	oracleKeeper := oraclekeeper.NewKeeper(
		cdc,
		runtime.NewKVStoreService(oracleKey),
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		distrtypes.ModuleName,
		oracleAccountKeeper,
		oracletestutil.NewMockBankKeeper(ctrl),
		oracletestutil.NewMockDistributionKeeper(ctrl),
		oracletestutil.NewMockStakingKeeper(ctrl),
	)

	tobinTaxes := make([]oracletypes.TobinTax, targetCount)
	for i := range targetCount {
		tobinTaxes[i] = oracletypes.TobinTax{
			Denom:    fmt.Sprintf("uasset%03d", i),
			TobinTax: oracletypes.DefaultTobinTax,
		}
	}
	oracleParams := oracletypes.DefaultParams()
	oracleParams.TobinTaxes = tobinTaxes
	if err := oracleKeeper.Params.Set(ctx, oracleParams); err != nil {
		b.Fatal(err)
	}
	for _, denom := range []string{tobinTaxes[0].Denom, tobinTaxes[1].Denom} {
		if err := oracleKeeper.ExchangeRate.Set(ctx, denom, oracletypes.ExchangeRate{
			Denom:          denom,
			Rate:           math.LegacyOneDec(),
			BlockTimestamp: sdk.UnwrapSDKContext(ctx).BlockTime(),
		}); err != nil {
			b.Fatal(err)
		}
	}

	marketAccountKeeper := markettestutil.NewMockAccountKeeper(ctrl)
	marketAccountKeeper.EXPECT().GetModuleAddress(markettypes.ModuleName).
		Return(authtypes.NewModuleAddress(markettypes.ModuleName))
	keeper := NewKeeper(
		cdc,
		runtime.NewKVStoreService(marketKey),
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		marketAccountKeeper,
		markettestutil.NewMockBankKeeper(ctrl),
		oracleKeeper,
		markettestutil.NewMockTreasuryKeeper(ctrl),
	)
	if err := keeper.Params.Set(ctx, markettypes.DefaultParams()); err != nil {
		b.Fatal(err)
	}
	if err := keeper.ArkPoolDelta.Set(ctx, math.LegacyZeroDec()); err != nil {
		b.Fatal(err)
	}

	return keeper, ctx, sdk.NewInt64Coin(tobinTaxes[0].Denom, 1_000_000), tobinTaxes[1].Denom
}
