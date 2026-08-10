package simulation

// DONTCOVER

import (
	"encoding/json"
	"fmt"
	"math/rand"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	chain "ark/pkg/chain"
	"ark/x/market/types"
)

// Simulation parameter constants
const (
	basePoolKey           = "base_pool"
	poolRecoveryPeriodKey = "pool_recovery_period"
	minStabilitySpreadKey = "min_spread"
)

// GenBasePool randomised BasePool
func GenBasePool(r *rand.Rand) sdk.DecCoin {
	wholeUnits := int64(50_000_000 + r.Intn(10_000))
	amount := math.LegacyNewDecFromInt(chain.NativeBaseAmount(wholeUnits))
	return sdk.NewDecCoinFromDec(chain.SDRBaseDenom, amount)
}

// GenPoolRecoveryPeriod randomised PoolRecoveryPeriod, inside the domain the
// policy accepts. The span was open-ended while the parameter was, which had
// the simulator routinely proposing recovery periods so long the per-block
// quotient truncates to nothing and the pool never recovers at all.
func GenPoolRecoveryPeriod(r *rand.Rand) uint64 {
	return uint64(100 + r.Intn(int(types.MaxPoolRecoveryPeriod)-100))
}

// GenMinSpread randomised MinSpread
func GenMinSpread(r *rand.Rand) math.LegacyDec {
	return math.LegacyNewDecWithPrec(1, 2).Add(math.LegacyNewDecWithPrec(int64(r.Intn(100)), 3))
}

// RandomisedGenState generates a random GenesisState for the market module
func RandomisedGenState(simState *module.SimulationState) {
	var basePool sdk.DecCoin
	simState.AppParams.GetOrGenerate(
		basePoolKey,
		&basePool,
		simState.Rand,
		func(r *rand.Rand) { basePool = GenBasePool(r) },
	)

	var poolRecoveryPeriod uint64
	simState.AppParams.GetOrGenerate(
		poolRecoveryPeriodKey,
		&poolRecoveryPeriod,
		simState.Rand,
		func(r *rand.Rand) { poolRecoveryPeriod = GenPoolRecoveryPeriod(r) },
	)

	var minStabilitySpread math.LegacyDec
	simState.AppParams.GetOrGenerate(
		minStabilitySpreadKey,
		&minStabilitySpread,
		simState.Rand,
		func(r *rand.Rand) { minStabilitySpread = GenMinSpread(r) },
	)

	marketGenesis := types.NewGenesisState(
		types.Params{
			DefaultTobinTax: types.DefaultTobinTax,
		},
		math.LegacyZeroDec(),
		nil,
		types.ConversionPolicy{
			BasePool:           basePool,
			PoolRecoveryPeriod: poolRecoveryPeriod,
			MinStabilitySpread: minStabilitySpread,
		},
		// Simulation never appoints a committee: the conversion fast path is a
		// governance act, not a randomised one.
		types.DefaultConversionMandate(),
	)

	bz, err := json.MarshalIndent(marketGenesis, "", " ")
	if err != nil {
		panic(err)
	}

	fmt.Printf("Selected randomly generated market parameters:\n%s\n", bz)
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(marketGenesis)
}
