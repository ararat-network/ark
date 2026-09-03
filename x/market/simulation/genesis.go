package simulation

// DONTCOVER

import (
	"encoding/json"
	"fmt"
	"math/rand"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	chain "github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/market/types"
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
	return sdk.NewDecCoinFromDec(chain.XDRBaseDenom, amount)
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

// GenConversionMandate appoints the conversion committee from the run's own
// accounts and opens the window at the first block for longer than any run
// lasts, so the committee surface is signable for the whole run. The corridor
// spans the drawn policy rather than pinning it, and the Tobin cap is positive,
// which leaves the committee somewhere to move.
func GenConversionMandate(r *rand.Rand, accounts []string, policy types.ConversionPolicy) types.ConversionMandate {
	if len(accounts) == 0 {
		return types.DefaultConversionMandate()
	}

	// Only the base pool widens. The recovery period is drawn near its domain
	// cap, so doubling it would leave the corridor invalid rather than wide.
	minimum := policy
	maximum := policy
	maximum.BasePool = sdk.NewDecCoinFromDec(
		policy.BasePool.Denom,
		policy.BasePool.Amount.MulInt64(2),
	)

	appointment := types.NewDisabledConversionMandate(1)
	appointment.Committee = accounts[r.Intn(len(accounts))]
	appointment.ActivationHeight = 1
	appointment.ExpiryHeight = chain.BlocksPerYear
	appointment.MinimumPolicy = minimum
	appointment.MaximumPolicy = maximum
	appointment.MaxTobinTax = math.LegacyNewDecWithPrec(50, 2)

	return appointment
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

	accounts := make([]string, 0, len(simState.Accounts))
	for _, account := range simState.Accounts {
		accounts = append(accounts, account.Address.String())
	}

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
		GenConversionMandate(simState.Rand, accounts, types.ConversionPolicy{
			BasePool:           basePool,
			PoolRecoveryPeriod: poolRecoveryPeriod,
			MinStabilitySpread: minStabilitySpread,
		}),
	)

	bz, err := json.MarshalIndent(marketGenesis, "", " ")
	if err != nil {
		panic(err)
	}

	fmt.Printf("Selected randomly generated market parameters:\n%s\n", bz)
	simState.GenState[types.ModuleName] = simState.Cdc.MustMarshalJSON(marketGenesis)
}
