package keeper_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

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

	chain "ark/pkg/chain"
	assettypes "ark/x/asset/types"
	oraclekeeper "ark/x/oracle/keeper"
	oracletestutil "ark/x/oracle/testutil"
	oracletypes "ark/x/oracle/types"
	treasurykeeper "ark/x/treasury/keeper"
	treasurytestutil "ark/x/treasury/testutil"
	treasurytypes "ark/x/treasury/types"
)

// benchAssetKeeper reports a fixed denomination set as ACTIVE assets so
// benchmarks measure the membership-driven paths — the cap refresh's
// denom comparison and the registry-fold liability scan — without mock
// bookkeeping in the hot loop. The reference matches the default params' cap
// denomination, which the refresh insists on.
type benchAssetKeeper struct {
	denoms []string
}

func (b benchAssetKeeper) Pricings(
	_ context.Context,
	overlay oracletypes.RateSet,
	denoms ...string,
) (assettypes.DenomPricings, error) {
	pricings := make(assettypes.DenomPricings, len(denoms)+1)
	pricings[chain.NoahBaseDenom] = assettypes.NumerairePricing()
	for _, denom := range denoms {
		if denom == chain.NoahBaseDenom {
			continue
		}
		if !slices.Contains(b.denoms, denom) {
			pricings[denom] = assettypes.DenomPricing{Reason: assettypes.UnpricedUnrecognised}
			continue
		}
		rate, rated := overlay[denom]
		if !rated {
			rate = math.LegacyOneDec()
		}
		pricings[denom] = assettypes.DenomPricing{
			Rate:   rate,
			Source: assettypes.PriceSourceOracle,
			Priced: true,
		}
	}
	return pricings, nil
}

func (b benchAssetKeeper) ListAssets(context.Context) ([]assettypes.Asset, error) {
	assets := make([]assettypes.Asset, 0, len(b.denoms))
	for _, denom := range b.denoms {
		assets = append(assets, assettypes.Asset{
			Denom:   denom,
			Status:  assettypes.AssetStatus_ASSET_STATUS_ACTIVE,
			Version: 1,
		})
	}
	return assets, nil
}

func (benchAssetKeeper) SettlementPlan(context.Context, string) (assettypes.SettlementPlan, bool, error) {
	return assettypes.SettlementPlan{}, false, nil
}

func (b benchAssetKeeper) PricedLiveDenoms(context.Context) ([]string, error) {
	return slices.Clone(b.denoms), nil
}

func BenchmarkTreasuryBeginBlocker(b *testing.B) {
	for _, targetCount := range []int{len(oracletypes.DefaultFeedDenoms), oracletypes.MaxFeeds} {
		for _, feeDenom := range []string{"anoah", "stable"} {
			b.Run(fmt.Sprintf("targets_%d/fees_%s", targetCount, feeDenom), func(b *testing.B) {
				keeper, ctx := benchmarkTreasuryKeeper(b, targetCount, feeDenom)
				// The first call builds the caps; the timed loop then measures
				// the steady state: a denom comparison that matches, plus
				// reward-funding accrual.
				if err := keeper.BeginBlocker(ctx); err != nil {
					b.Fatal(err)
				}

				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if err := keeper.BeginBlocker(ctx); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func benchmarkTreasuryKeeper(b *testing.B, targetCount int, feeDenom string) (*treasurykeeper.Keeper, sdk.Context) {
	b.Helper()

	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	oracletypes.RegisterInterfaces(interfaceRegistry)
	treasurytypes.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)

	treasuryKey := storetypes.NewKVStoreKey(treasurytypes.StoreKey)
	oracleKey := storetypes.NewKVStoreKey(oracletypes.StoreKey)
	transientKey := storetypes.NewTransientStoreKey("treasury_benchmark_transient")
	ctx := sdktestutil.DefaultContextWithKeys(
		map[string]*storetypes.KVStoreKey{
			treasurytypes.StoreKey: treasuryKey,
			oracletypes.StoreKey:   oracleKey,
		},
		map[string]*storetypes.TransientStoreKey{
			transientKey.Name(): transientKey,
		},
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
	if err := oracleKeeper.ReferenceDenom.Set(ctx, chain.SDRBaseDenom); err != nil {
		b.Fatal(err)
	}

	denoms := make([]string, targetCount)
	for i := range targetCount {
		denoms[i] = fmt.Sprintf("uasset%03d", i)
	}

	treasuryAccountKeeper := treasurytestutil.NewMockAccountKeeper(ctrl)
	for _, moduleName := range treasurytypes.FundAccountNames() {
		treasuryAccountKeeper.EXPECT().GetModuleAddress(moduleName).
			Return(authtypes.NewModuleAddress(moduleName))
	}
	treasuryAccountKeeper.EXPECT().GetModuleAddress(treasurytypes.StabilityTaxCollectorName).
		Return(authtypes.NewModuleAddress(treasurytypes.StabilityTaxCollectorName))
	treasuryAccountKeeper.EXPECT().GetModuleAddress(authtypes.FeeCollectorName).
		Return(authtypes.NewModuleAddress(authtypes.FeeCollectorName)).
		AnyTimes()
	treasuryBankKeeper := treasurytestutil.NewMockBankKeeper(ctrl)
	fees := sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1))
	if feeDenom == "stable" {
		stableDenom := denoms[0]
		fees = sdk.NewCoins(sdk.NewInt64Coin(stableDenom, 1))
		if err := oracleKeeper.ExchangeRate.Set(ctx, stableDenom, oracletypes.ExchangeRate{
			Denom:          stableDenom,
			Rate:           math.LegacyOneDec(),
			BlockTimestamp: sdk.UnwrapSDKContext(ctx).BlockTime(),
		}); err != nil {
			b.Fatal(err)
		}
	}
	treasuryBankKeeper.EXPECT().GetAllBalances(gomock.Any(), gomock.Any()).Return(fees).AnyTimes()

	keeper := treasurykeeper.NewKeeper(
		cdc,
		runtime.NewKVStoreService(treasuryKey),
		runtime.NewTransientStoreService(transientKey),
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		treasuryAccountKeeper,
		treasuryBankKeeper,
		oracleKeeper,
		benchAssetKeeper{denoms: denoms},
	)
	if err := keeper.Params.Set(ctx, treasurytypes.DefaultParams()); err != nil {
		b.Fatal(err)
	}
	if err := keeper.MonetaryPolicy.Set(ctx, treasurytypes.DefaultMonetaryPolicy()); err != nil {
		b.Fatal(err)
	}
	funding := treasurytypes.DefaultRewardFundingState()
	funding.BlocksRemaining = ^uint64(0)
	if err := keeper.RewardFunding.Set(ctx, funding); err != nil {
		b.Fatal(err)
	}

	return keeper, ctx
}
