package keeper_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"cosmossdk.io/core/store"
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

	"github.com/ararat-network/ark/pkg/chain"
	markettypes "github.com/ararat-network/ark/x/market/types"
	oraclekeeper "github.com/ararat-network/ark/x/oracle/keeper"
	oracletestutil "github.com/ararat-network/ark/x/oracle/testutil"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	treasurykeeper "github.com/ararat-network/ark/x/treasury/keeper"
	treasurytestutil "github.com/ararat-network/ark/x/treasury/testutil"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

type liabilityBenchFixture struct {
	keeper           *treasurykeeper.Keeper
	ctx              sdk.Context
	transientService store.TransientStoreService
	denoms           []string
}

func newLiabilityBenchFixture(tb testing.TB, denomCount int) *liabilityBenchFixture {
	tb.Helper()

	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	oracletypes.RegisterInterfaces(interfaceRegistry)
	treasurytypes.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)

	treasuryKey := storetypes.NewKVStoreKey(treasurytypes.StoreKey)
	oracleKey := storetypes.NewKVStoreKey(oracletypes.StoreKey)
	benchBankKey := storetypes.NewKVStoreKey("bench_bank_supply")
	transientKey := storetypes.NewTransientStoreKey("liability_benchmark_transient")
	ctx := sdktestutil.DefaultContextWithKeys(
		map[string]*storetypes.KVStoreKey{
			treasurytypes.StoreKey: treasuryKey,
			oracletypes.StoreKey:   oracleKey,
			"bench_bank_supply":    benchBankKey,
		},
		map[string]*storetypes.TransientStoreKey{
			transientKey.Name(): transientKey,
		},
		nil,
	).WithBlockHeight(2).WithBlockTime(time.Unix(1, 0))

	ctrl := gomock.NewController(tb)
	oracleAccountKeeper := oracletestutil.NewMockAccountKeeper(ctrl)
	oracleAccountKeeper.EXPECT().GetModuleAddress(gomock.Any()).
		DoAndReturn(authtypes.NewModuleAddress).AnyTimes()
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

	denoms := make([]string, denomCount)
	for i := range denomCount {
		denoms[i] = fmt.Sprintf("uasset%03d", i)
	}
	if err := oracleKeeper.Params.Set(ctx, oracletypes.DefaultParams()); err != nil {
		tb.Fatal(err)
	}
	for _, denom := range denoms {
		if err := oracleKeeper.ExchangeRate.Set(ctx, denom, oracletypes.ExchangeRate{
			Denom:          denom,
			Rate:           math.LegacyOneDec(),
			BlockTimestamp: ctx.BlockTime(),
		}); err != nil {
			tb.Fatal(err)
		}
	}

	// Seed per-denom supply into a real mounted store so GetSupply pays a
	// metered read equivalent to Bank's own lookup.
	supplyStore := ctx.KVStore(benchBankKey)
	for _, denom := range denoms {
		supplyStore.Set([]byte("supply/"+denom), []byte("1000000000000000"))
	}

	treasuryAccountKeeper := treasurytestutil.NewMockAccountKeeper(ctrl)
	treasuryAccountKeeper.EXPECT().GetModuleAddress(gomock.Any()).
		DoAndReturn(authtypes.NewModuleAddress).AnyTimes()
	treasuryBankKeeper := treasurytestutil.NewMockBankKeeper(ctrl)
	treasuryBankKeeper.EXPECT().GetSupply(gomock.Any(), gomock.Any()).
		DoAndReturn(func(c context.Context, denom string) sdk.Coin {
			bz := sdk.UnwrapSDKContext(c).KVStore(benchBankKey).Get([]byte("supply/" + denom))
			amount, ok := math.NewIntFromString(string(bz))
			if !ok {
				tb.Fatalf("bad seeded supply for %s", denom)
			}
			return sdk.NewCoin(denom, amount)
		}).AnyTimes()
	treasuryBankKeeper.EXPECT().GetBalance(gomock.Any(), gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0)).AnyTimes()
	// The partition now asks what the strategic Reserve holds of every counted
	// member (D66); an empty Reserve keeps the measured path the gross fold
	// while still paying the per-member balance read the netting added.
	treasuryBankKeeper.EXPECT().GetBalance(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
			return sdk.NewCoin(denom, math.ZeroInt())
		}).AnyTimes()

	transientService := runtime.NewTransientStoreService(transientKey)
	// The liability scan is now a fold over the asset registry, so the
	// benchmark hands the keeper a registry listing every seeded denom as
	// ACTIVE: the measured path walks it, reads each supply, and captures one
	// rate set for the whole membership.
	keeper := treasurykeeper.NewKeeper(
		cdc,
		runtime.NewKVStoreService(treasuryKey),
		transientService,
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		treasuryAccountKeeper,
		treasuryBankKeeper,
		oracleKeeper,
		benchAssetKeeper{denoms: denoms},
		stubFund{},
		stubFund{},
	)

	return &liabilityBenchFixture{
		keeper:           keeper,
		ctx:              ctx,
		transientService: transientService,
		denoms:           denoms,
	}
}

// BenchmarkLiabilityValuation measures the whole per-block cost of the
// aggregate: one registry fold and the settlement that consumes it. There is no
// per-conversion path left to measure — a conversion records two integers — so
// what this bounds is block overhead, paid once however busy the block was.
func BenchmarkLiabilityValuation(b *testing.B) {
	for _, denomCount := range []int{len(oracletypes.DefaultFeedDenoms), 26} {
		// Settlement of a redemption-only block: the fold, the coverage
		// arithmetic, and the draw.
		b.Run(fmt.Sprintf("denoms_%d/settle_redemption", denomCount), func(b *testing.B) {
			fix := newLiabilityBenchFixture(b, denomCount)
			totals := markettypes.ConversionTotals{
				GrossOffer:        math.ZeroInt(),
				EligiblePrincipal: math.ZeroInt(),
				RedemptionOutput:  math.NewInt(500),
				RedeemedValue:     math.LegacyNewDec(1000),
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := fix.keeper.SettleConversions(fix.ctx, totals); err != nil {
					b.Fatal(err)
				}
			}
		})
		// Settlement of an expansion-only block: the same fold, then the
		// waterfall, which additionally asks both committee funds what they hold.
		b.Run(fmt.Sprintf("denoms_%d/settle_expansion", denomCount), func(b *testing.B) {
			fix := newLiabilityBenchFixture(b, denomCount)
			totals := markettypes.ConversionTotals{
				GrossOffer:        math.NewInt(500),
				EligiblePrincipal: math.NewInt(500),
				RedemptionOutput:  math.ZeroInt(),
				RedeemedValue:     math.LegacyZeroDec(),
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := fix.keeper.SettleConversions(fix.ctx, totals); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
