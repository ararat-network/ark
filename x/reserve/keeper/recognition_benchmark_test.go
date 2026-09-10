package keeper_test

import (
	"context"
	"fmt"
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

	"github.com/ararat-network/ark/pkg/chain"
	oraclekeeper "github.com/ararat-network/ark/x/oracle/keeper"
	oracletestutil "github.com/ararat-network/ark/x/oracle/testutil"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	reservekeeper "github.com/ararat-network/ark/x/reserve/keeper"
	"github.com/ararat-network/ark/x/reserve/testutil"
	"github.com/ararat-network/ark/x/reserve/types"
)

type recognitionBenchFixture struct {
	keeper *reservekeeper.Keeper
	ctx    sdk.Context
}

// newRecognitionBenchFixture builds a Reserve keeper whose recognition inputs
// pay realistic store costs: the policy and position walks run against a real
// KV store, rate reads run through the real Oracle keeper, and the bank mock
// serves balances from a mounted store so each read is metered like Bank's
// own lookup. Positions are spread round-robin across the eligible
// denominations, every denomination also holds an on-chain balance, and the
// NOAH base is large enough that credits count rather than clipping to zero.
func newRecognitionBenchFixture(tb testing.TB, denomCount, positionCount int) *recognitionBenchFixture {
	tb.Helper()

	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	oracletypes.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)

	reserveKey := storetypes.NewKVStoreKey(types.StoreKey)
	oracleKey := storetypes.NewKVStoreKey(oracletypes.StoreKey)
	benchBankKey := storetypes.NewKVStoreKey("bench_reserve_bank")
	ctx := sdktestutil.DefaultContextWithKeys(
		map[string]*storetypes.KVStoreKey{
			types.StoreKey:       reserveKey,
			oracletypes.StoreKey: oracleKey,
			"bench_reserve_bank": benchBankKey,
		},
		map[string]*storetypes.TransientStoreKey{},
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
	if err := oracleKeeper.Params.Set(ctx, oracletypes.DefaultParams()); err != nil {
		tb.Fatal(err)
	}

	denoms := make([]string, denomCount)
	for i := range denomCount {
		denoms[i] = fmt.Sprintf("abench%03d", i)
	}
	for _, denom := range denoms {
		if err := oracleKeeper.ExchangeRate.Set(ctx, denom, oracletypes.ExchangeRate{
			Denom:          denom,
			Rate:           math.LegacyOneDec(),
			BlockTimestamp: ctx.BlockTime(),
			BlockHeight:    uint64(ctx.BlockHeight()),
		}); err != nil {
			tb.Fatal(err)
		}
	}

	// Balances live in a mounted store so GetAllBalances and the NOAH read pay
	// per-key metered work equivalent to Bank's own prefix walk.
	reserveAddress := authtypes.NewModuleAddress(types.StrategicReserveName)
	balanceStore := ctx.KVStore(benchBankKey)
	balanceStore.Set([]byte("balance/"+chain.NoahBaseDenom), []byte("1000000000000"))
	for _, denom := range denoms {
		balanceStore.Set([]byte("balance/"+denom), []byte("1000000"))
	}

	accountKeeper := testutil.NewMockAccountKeeper(ctrl)
	// Committee accounts default to absent, which an appointment records as
	// the shape it observed rather than refusing.
	accountKeeper.EXPECT().GetAccount(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	accountKeeper.EXPECT().
		GetModuleAddress(types.StrategicReserveName).
		Return(reserveAddress).
		AnyTimes()
	bankKeeper := testutil.NewMockBankKeeper(ctrl)
	bankKeeper.EXPECT().
		GetBalance(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(c context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
			bz := sdk.UnwrapSDKContext(c).KVStore(benchBankKey).Get([]byte("balance/" + denom))
			if bz == nil {
				return sdk.NewCoin(denom, math.ZeroInt())
			}
			amount, ok := math.NewIntFromString(string(bz))
			if !ok {
				tb.Fatalf("bad seeded balance for %s", denom)
			}
			return sdk.NewCoin(denom, amount)
		}).AnyTimes()
	bankKeeper.EXPECT().
		GetAllBalances(gomock.Any(), gomock.Any()).
		DoAndReturn(func(c context.Context, _ sdk.AccAddress) sdk.Coins {
			store := sdk.UnwrapSDKContext(c).KVStore(benchBankKey)
			iterator := store.Iterator([]byte("balance/"), []byte("balance0"))
			defer iterator.Close()
			balances := sdk.NewCoins()
			for ; iterator.Valid(); iterator.Next() {
				denom := string(iterator.Key()[len("balance/"):])
				amount, ok := math.NewIntFromString(string(iterator.Value()))
				if !ok {
					tb.Fatalf("bad seeded balance for %s", denom)
				}
				balances = balances.Add(sdk.NewCoin(denom, amount))
			}
			return balances
		}).AnyTimes()

	wasmKeeper := testutil.NewMockWasmKeeper(ctrl)
	wasmKeeper.EXPECT().HasContractInfo(gomock.Any(), gomock.Any()).Return(false).AnyTimes()
	keeper := reservekeeper.NewKeeper(
		cdc,
		runtime.NewKVStoreService(reserveKey),
		authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		accountKeeper,
		wasmKeeper,
		bankKeeper,
		oracleKeeper,
		testutil.NewMockAssetKeeper(ctrl),
	)

	// Every denomination is credited: haircut 0.8, ratios summing to 0.9 so
	// the policy is one governance could set and the solve does real work.
	ratio := math.LegacyMustNewDecFromStr("0.9").QuoInt64(int64(denomCount))
	for _, denom := range denoms {
		if err := keeper.RecognitionPolicy.Set(ctx, denom, types.EligibilityEntry{
			Denom:               denom,
			HaircutFactor:       math.LegacyMustNewDecFromStr("0.8"),
			RecognitionCapRatio: ratio,
		}); err != nil {
			tb.Fatal(err)
		}
	}
	for i := range positionCount {
		if err := keeper.OpenPositions.Set(ctx, uint64(i), types.Position{
			PositionId:     uint64(i),
			Quantity:       sdk.NewInt64Coin(denoms[i%denomCount], 1_000_000),
			Deployed:       chain.NoahCoin(math.NewInt(1_000_000)),
			Returned:       chain.NoahCoin(math.ZeroInt()),
			VenueReference: "bench",
			OpenedHeight:   1,
		}); err != nil {
			tb.Fatal(err)
		}
	}

	fix := &recognitionBenchFixture{keeper: keeper, ctx: ctx}

	// Guard against benchmarking a degenerate path: if rates were stale or the
	// policy inert, every credit would be zero and the loop would measure the
	// walks without the arithmetic they exist to feed.
	recognised, err := keeper.RecognisedCapital(ctx)
	if err != nil {
		tb.Fatal(err)
	}
	if !recognised.GT(math.NewInt(1_000_000_000_000)) {
		tb.Fatalf("fixture credits nothing above the NOAH base: recognised %s", recognised)
	}

	return fix
}

// BenchmarkRecognisedCapital measures the fold conversion settlement pays once
// per block that expanded: the policy walk, the open position walk, the balance
// walk, one available-rate read per eligible denomination, and the recognition
// solve. The gas/op metric is the same call's metered store cost. It is block
// overhead now rather than a per-swap charge, so what it bounds is how much a
// block pays however many conversions it settled.
//
// The axes are the two inputs operators actually grow: eligible denominations
// (governance-bounded, ratio sum below one) and open positions
// (committee-bounded, accumulating until closed).
func BenchmarkRecognisedCapital(b *testing.B) {
	for _, bc := range []struct {
		denoms    int
		positions int
	}{
		{denoms: 8, positions: 16},
		{denoms: 8, positions: 256},
		{denoms: 8, positions: 2048},
		{denoms: 32, positions: 16},
		{denoms: 32, positions: 256},
		{denoms: 32, positions: 2048},
	} {
		b.Run(fmt.Sprintf("denoms_%d/positions_%d", bc.denoms, bc.positions), func(b *testing.B) {
			fix := newRecognitionBenchFixture(b, bc.denoms, bc.positions)

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := fix.keeper.RecognisedCapital(fix.ctx); err != nil {
					b.Fatal(err)
				}
			}

			// Reported after the loop because ResetTimer (and Loop's first
			// call) deletes user metrics. The figure is one metered call's
			// store gas: deterministic, so measuring once is exact.
			metered := fix.ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
			if _, err := fix.keeper.RecognisedCapital(metered); err != nil {
				b.Fatal(err)
			}
			b.ReportMetric(float64(metered.GasMeter().GasConsumed()), "gas/op")
		})
	}
}
