package keeper_test

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

	chain "ark/pkg/chain"
	oraclekeeper "ark/x/oracle/keeper"
	oracletestutil "ark/x/oracle/testutil"
	oracletypes "ark/x/oracle/types"
	treasurykeeper "ark/x/treasury/keeper"
	treasurytestutil "ark/x/treasury/testutil"
	treasurytypes "ark/x/treasury/types"
)

func BenchmarkTreasuryBeginBlocker(b *testing.B) {
	for _, targetCount := range []int{len(oracletypes.DefaultTobinTaxes), oracletypes.MaxVoteTargets} {
		for _, feeDenom := range []string{"anoah", "stable"} {
			b.Run(fmt.Sprintf("targets_%d/fees_%s", targetCount, feeDenom), func(b *testing.B) {
				keeper, ctx := benchmarkTreasuryKeeper(b, targetCount, feeDenom)
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
		stableDenom := tobinTaxes[0].Denom
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
	for _, tobinTax := range tobinTaxes {
		if err := keeper.TaxCaps.Set(ctx, tobinTax.Denom, math.ZeroInt()); err != nil {
			b.Fatal(err)
		}
	}

	return keeper, ctx
}
