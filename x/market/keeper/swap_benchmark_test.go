package keeper

import (
	"context"
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

	assettypes "ark/x/asset/types"
	markettestutil "ark/x/market/testutil"
	markettypes "ark/x/market/types"
	oraclekeeper "ark/x/oracle/keeper"
	oracletestutil "ark/x/oracle/testutil"
	oracletypes "ark/x/oracle/types"
)

var benchmarkSwapQuote swapQuote

// BenchmarkStableToStableQuote varies how many denominations Oracle prices and
// expects the result to be flat. A quote costs a point lookup per leg on both
// sides — Market's spread and Oracle's rate — plus one fixed-size params read,
// so nothing in the path walks the registry. The axis is kept precisely to
// catch a regression that reintroduces registry-scaled work: it was real until
// the Tobin list was deleted from Oracle's params, when every quote paid to
// decode a record that grew with the priced set.
func BenchmarkStableToStableQuote(b *testing.B) {
	for _, pricedCount := range []int{len(oracletypes.DefaultFeedDenoms), oracletypes.MaxFeeds} {
		b.Run(fmt.Sprintf("priced_denoms_%d", pricedCount), func(b *testing.B) {
			keeper, ctx, offerCoin, askDenom := benchmarkMarketKeeper(b, pricedCount)
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
	pricedCount int,
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

	if err := oracleKeeper.Params.Set(ctx, oracletypes.DefaultParams()); err != nil {
		b.Fatal(err)
	}

	// The registry is the stored rate set, so price every denomination in it.
	// The two the quote actually converts between are the first two.
	pricedDenoms := make([]string, pricedCount)
	for i := range pricedCount {
		pricedDenoms[i] = fmt.Sprintf("uasset%03d", i)
		if err := oracleKeeper.ExchangeRate.Set(ctx, pricedDenoms[i], oracletypes.ExchangeRate{
			Denom:          pricedDenoms[i],
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
		benchmarkAssetKeeper{},
	)
	if err := keeper.Params.Set(ctx, markettypes.DefaultParams()); err != nil {
		b.Fatal(err)
	}
	if err := keeper.ArkPoolDelta.Set(ctx, math.LegacyZeroDec()); err != nil {
		b.Fatal(err)
	}

	return keeper, ctx, sdk.NewInt64Coin(pricedDenoms[0], 1_000_000), pricedDenoms[1]
}

// benchmarkAssetKeeper reports every denomination ACTIVE so the benchmark
// measures the conversion path rather than the lifecycle gate.
type benchmarkAssetKeeper struct{}

func (benchmarkAssetKeeper) GetAsset(_ context.Context, denom string) (assettypes.Asset, error) {
	return assettypes.Asset{
		Denom:   denom,
		Status:  assettypes.AssetStatus_ASSET_STATUS_ACTIVE,
		Version: 1,
	}, nil
}

func (benchmarkAssetKeeper) ActiveSettlementPlan(
	context.Context,
	string,
) (assettypes.SettlementPlan, bool, error) {
	return assettypes.SettlementPlan{}, false, nil
}

func (benchmarkAssetKeeper) PricedLiveDenoms(context.Context) ([]string, error) {
	return nil, nil
}

func (benchmarkAssetKeeper) GetReference(
	context.Context,
) (assettypes.ReferenceState, error) {
	return assettypes.ReferenceState{}, nil
}
