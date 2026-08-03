package keeper_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	corestore "cosmossdk.io/core/store"
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
	oraclekeeper "ark/x/oracle/keeper"
	oracletestutil "ark/x/oracle/testutil"
	oracletypes "ark/x/oracle/types"
	treasurykeeper "ark/x/treasury/keeper"
	treasurytestutil "ark/x/treasury/testutil"
	treasurytypes "ark/x/treasury/types"
)

// benchLiabilityValuationKey mirrors the unexported key in liability.go.
var benchLiabilityValuationKey = []byte{0x01}

type liabilityBenchFixture struct {
	keeper           *treasurykeeper.Keeper
	ctx              sdk.Context
	transientService corestore.TransientStoreService
	denoms           []string
}

func (f *liabilityBenchFixture) quoteRates() oracletypes.RateSet {
	return oracletypes.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		f.denoms[0]:         math.LegacyOneDec(),
	}
}

func (f *liabilityBenchFixture) draw() error {
	_, err := f.keeper.DrawRedemptionBuffer(
		f.ctx,
		sdk.NewInt64Coin(f.denoms[0], 1000),
		math.NewInt(500),
		f.quoteRates(),
	)
	return err
}

func (f *liabilityBenchFixture) resetSnapshot(tb testing.TB) {
	tb.Helper()
	store := f.transientService.OpenTransientStore(f.ctx)
	if err := store.Delete(benchLiabilityValuationKey); err != nil {
		tb.Fatal(err)
	}
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
		DoAndReturn(func(name string) sdk.AccAddress {
			return authtypes.NewModuleAddress(name)
		}).AnyTimes()
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
		DoAndReturn(func(name string) sdk.AccAddress {
			return authtypes.NewModuleAddress(name)
		}).AnyTimes()
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

// BenchmarkLiabilityValuation measures the three paths that define the
// flat-metering design: the once-per-block preblock scan, the per-swap
// snapshot read, and per-swap snapshot maintenance.
func BenchmarkLiabilityValuation(b *testing.B) {
	for _, denomCount := range []int{len(oracletypes.DefaultFeedDenoms), 26} {
		// The real scan, performed once per block by the preblocker. Includes
		// two transient deletes per iteration to reset the block.
		b.Run(fmt.Sprintf("denoms_%d/preblock_prime", denomCount), func(b *testing.B) {
			fix := newLiabilityBenchFixture(b, denomCount)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				fix.resetSnapshot(b)
				if err := fix.keeper.PrimeLiabilitySnapshot(fix.ctx); err != nil {
					b.Fatal(err)
				}
			}
		})
		// The per-swap steady state: valuation via the primed snapshot.
		b.Run(fmt.Sprintf("denoms_%d/snapshot_hit", denomCount), func(b *testing.B) {
			fix := newLiabilityBenchFixture(b, denomCount)
			if err := fix.keeper.PrimeLiabilitySnapshot(fix.ctx); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if err := fix.draw(); err != nil {
					b.Fatal(err)
				}
			}
		})
		// The per-swap snapshot maintenance after burns and mints.
		b.Run(fmt.Sprintf("denoms_%d/record_supply_change", denomCount), func(b *testing.B) {
			fix := newLiabilityBenchFixture(b, denomCount)
			if err := fix.keeper.PrimeLiabilitySnapshot(fix.ctx); err != nil {
				b.Fatal(err)
			}
			rates := fix.quoteRates()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if err := fix.keeper.RecordSupplyChange(
					fix.ctx,
					sdk.NewInt64Coin(fix.denoms[0], 1),
					sdk.NewInt64Coin(chain.NoahBaseDenom, 1),
					rates,
				); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
