package keeper_test

import (
	"context"
	"encoding/binary"
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
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"ark/x/oracle/keeper"
	"ark/x/oracle/types"
)

const benchmarkValidatorCount = 100

type accountingBenchmarkCase struct {
	name             string
	rewardWeight     math.Int
	eligible         bool
	participated     bool
	seedRewardWeight bool
	seedAttendance   bool
}

type accountingBenchmarkVoter struct {
	consAddr sdk.ConsAddress
	valAddr  sdk.ValAddress
}

type accountingBenchmarkAccountKeeper struct{}

func (accountingBenchmarkAccountKeeper) GetModuleAddress(name string) sdk.AccAddress {
	switch name {
	case types.ModuleName:
		return sdk.AccAddress{1}
	case "distribution":
		return sdk.AccAddress{2}
	default:
		return nil
	}
}

func (accountingBenchmarkAccountKeeper) GetModuleAccount(context.Context, string) sdk.ModuleAccountI {
	return nil
}

type accountingBenchmarkStakingKeeper struct {
	validators map[string]stakingtypes.Validator
}

func (k accountingBenchmarkStakingKeeper) ValidatorByConsAddr(
	_ context.Context,
	consAddr sdk.ConsAddress,
) (stakingtypes.ValidatorI, error) {
	validator, found := k.validators[string(consAddr)]
	if !found {
		return nil, stakingtypes.ErrNoValidatorFound
	}
	return validator, nil
}

func (accountingBenchmarkStakingKeeper) Validator(
	context.Context,
	sdk.ValAddress,
) (stakingtypes.ValidatorI, error) {
	panic("unexpected Validator call in accounting benchmark")
}

func (accountingBenchmarkStakingKeeper) Jail(context.Context, sdk.ConsAddress) error {
	panic("unexpected Jail call in accounting benchmark")
}

func BenchmarkRecordVoteAccounting(b *testing.B) {
	cases := []accountingBenchmarkCase{
		{
			name:             "reward_only",
			rewardWeight:     math.NewInt(8),
			seedRewardWeight: true,
		},
		{
			name:             "reward_and_attendance",
			rewardWeight:     math.NewInt(7),
			eligible:         true,
			participated:     true,
			seedRewardWeight: true,
			seedAttendance:   true,
		},
		{
			name:         "first_write",
			rewardWeight: math.NewInt(8),
			eligible:     true,
			participated: true,
		},
		{
			name:         "empty_update",
			rewardWeight: math.ZeroInt(),
		},
	}

	for _, benchmarkCase := range cases {
		b.Run(fmt.Sprintf("validators_%d/%s", benchmarkValidatorCount, benchmarkCase.name), func(b *testing.B) {
			benchmarkRecordVoteAccounting(b, benchmarkCase)
		})
	}
}

func benchmarkRecordVoteAccounting(b *testing.B, benchmarkCase accountingBenchmarkCase) {
	b.Helper()

	interfaceRegistry := codectestutil.CodecOptions{}.NewInterfaceRegistry()
	std.RegisterInterfaces(interfaceRegistry)
	types.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)

	key := storetypes.NewKVStoreKey(types.StoreKey)
	storeService := runtime.NewKVStoreService(key)
	testCtx := sdktestutil.DefaultContextWithDB(
		b,
		key,
		storetypes.NewTransientStoreKey("oracle_accounting_benchmark_transient"),
	)
	baseCtx := sdk.UnwrapSDKContext(testCtx.Ctx).
		WithBlockHeight(10_000).
		WithBlockTime(time.Unix(10_000, 0).UTC())

	voters := make([]accountingBenchmarkVoter, benchmarkValidatorCount)
	validators := make(map[string]stakingtypes.Validator, benchmarkValidatorCount)
	for i := range voters {
		consAddr := benchmarkAddress[sdk.ConsAddress](i + 1)
		valAddr := benchmarkAddress[sdk.ValAddress](i + 1)
		voters[i] = accountingBenchmarkVoter{consAddr: consAddr, valAddr: valAddr}
		validators[string(consAddr)] = stakingtypes.Validator{OperatorAddress: valAddr.String()}
	}

	oracleKeeper := keeper.NewKeeper(
		cdc,
		storeService,
		"authority",
		"distribution",
		accountingBenchmarkAccountKeeper{},
		nil,
		nil,
		accountingBenchmarkStakingKeeper{validators: validators},
	)

	for _, voter := range voters {
		if benchmarkCase.seedRewardWeight {
			if err := oracleKeeper.RewardWeight.Set(baseCtx, voter.valAddr, math.NewInt(1_000_000)); err != nil {
				b.Fatal(err)
			}
		}
		if benchmarkCase.seedAttendance {
			if err := oracleKeeper.Attendance.Set(baseCtx, voter.valAddr, types.Attendance{
				EligibleBlocks: 1_000,
				AttendedBlocks: 1_000,
			}); err != nil {
				b.Fatal(err)
			}
		}
	}

	b.ReportAllocs()
	b.ReportMetric(benchmarkValidatorCount, "validators/op")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		ctx, _ := baseCtx.CacheContext()
		b.StartTimer()

		for _, voter := range voters {
			if err := oracleKeeper.RecordVoteAccounting(
				ctx,
				voter.consAddr,
				benchmarkCase.rewardWeight,
				benchmarkCase.eligible,
				benchmarkCase.participated,
			); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func benchmarkAddress[T ~[]byte](index int) T {
	address := make([]byte, 20)
	binary.BigEndian.PutUint64(address[12:], uint64(index))
	return T(address)
}
