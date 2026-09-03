package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/treasury/types"
)

func TestDefaultGenesisState(t *testing.T) {
	genesis := types.DefaultGenesisState()
	require.Equal(t, types.DefaultParams(), genesis.Params)
	require.True(t, types.DefaultEconomicPolicy().Equal(genesis.EconomicPolicy))
	require.Equal(t, []types.ConversionFactor{
		{Denom: chain.NoahBaseDenom, Factor: types.DefaultNoahConversionFactor},
	}, genesis.ConversionFactors)
	require.Equal(t, types.DefaultRewardFundingState(), genesis.RewardFunding)
	require.Equal(t, types.DefaultEconomicMandate(), genesis.EconomicMandate)
	require.NoError(t, genesis.Validate())
}

// TestDefaultNoahConversionFactorIsTheBootstrapPriceInXDR pins the seed to
// its derivation, so moving either the stated dollar price or the XDR cross
// is a deliberate edit here too.
func TestDefaultNoahConversionFactorIsTheBootstrapPriceInXDR(t *testing.T) {
	require.Equal(t, "1", chain.BootstrapNoahPerUSD)
	require.Equal(t, math.LegacyMustNewDecFromStr("1.371"), types.DefaultNoahConversionFactor)
}

func TestNewGenesisStateCopiesSlices(t *testing.T) {
	factors := []types.ConversionFactor{{Denom: chain.USDBaseDenom, Factor: math.LegacyOneDec()}}
	genesis := types.NewGenesisState(
		types.DefaultParams(),
		factors,
		types.DefaultRewardFundingState(),
		types.DefaultEconomicMandate(),
		types.DefaultEconomicPolicy(),
		types.DefaultExposureState(),
		false,
		types.DefaultMinBaseGasPrice,
	)
	factors[0].Denom = "mutated"
	require.Equal(t, chain.USDBaseDenom, genesis.ConversionFactors[0].Denom)
}

// TestGenesisBaseGasPriceValidation pins the controller's genesis invariant:
// the live price is a state no run of the controller could not have produced,
// so it must sit inside [MinBaseGasPrice, MaxBaseGasPrice].
func TestGenesisBaseGasPriceValidation(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*types.GenesisState)
		expectErr string
	}{
		{name: "default is valid", mutate: func(*types.GenesisState) {}},
		{
			name:      "unset price",
			mutate:    func(gs *types.GenesisState) { gs.BaseGasPrice = math.LegacyDec{} },
			expectErr: "base gas price must be set",
		},
		{
			name:      "price below the floor",
			mutate:    func(gs *types.GenesisState) { gs.BaseGasPrice = math.LegacyMustNewDecFromStr("0.05") },
			expectErr: "base gas price must be between the floor",
		},
		{
			name:      "price above the domain cap",
			mutate:    func(gs *types.GenesisState) { gs.BaseGasPrice = types.MaxBaseGasPrice.Add(math.LegacyOneDec()) },
			expectErr: "base gas price must be between the floor",
		},
		{
			// An export mid-congestion carries an elevated price.
			name: "price above the floor",
			mutate: func(gs *types.GenesisState) {
				gs.BaseGasPrice = types.DefaultMinBaseGasPrice.MulInt64(7)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			genesis := types.DefaultGenesisState()
			tc.mutate(genesis)

			err := genesis.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.expectErr)
		})
	}
}

func TestGenesisConversionFactorValidation(t *testing.T) {
	factor := func(denom string, factor string) types.ConversionFactor {
		return types.ConversionFactor{Denom: denom, Factor: math.LegacyMustNewDecFromStr(factor)}
	}

	tests := []struct {
		name      string
		mutate    func(*types.GenesisState)
		expectErr string
	}{
		{
			name: "positive factor is valid",
			mutate: func(genesis *types.GenesisState) {
				genesis.ConversionFactors = []types.ConversionFactor{
					factor(chain.NoahBaseDenom, "1"),
					factor(chain.XDRBaseDenom, "1"),
				}
			},
		},
		// A factor that disagrees with live rates is the expected shape of a
		// kept factor, not a corrupt one: it was derived under whatever the
		// rates were at the time, and only the refresh re-derives it.
		{
			name: "sub-unit factor is valid",
			mutate: func(genesis *types.GenesisState) {
				genesis.ConversionFactors = []types.ConversionFactor{
					factor(chain.NoahBaseDenom, "1"),
					factor(chain.XDRBaseDenom, "0.000001"),
				}
			},
		},
		{
			name: "zero factor is refused",
			mutate: func(genesis *types.GenesisState) {
				genesis.ConversionFactors = []types.ConversionFactor{
					factor(chain.NoahBaseDenom, "1"),
					factor(chain.XDRBaseDenom, "0"),
				}
			},
			expectErr: "must be positive",
		},
		{
			name: "sorted factors are valid",
			mutate: func(genesis *types.GenesisState) {
				genesis.ConversionFactors = []types.ConversionFactor{
					factor(chain.KRWBaseDenom, "1"),
					factor(chain.NoahBaseDenom, "1"),
					factor(chain.USDBaseDenom, "1"),
				}
			},
		},
		{
			// The numeraire's cross rides in the table exempt from the
			// priced-denom rule, which rejects NOAH by name.
			name: "noah cross beside members is valid",
			mutate: func(genesis *types.GenesisState) {
				genesis.ConversionFactors = []types.ConversionFactor{
					factor(chain.KRWBaseDenom, "1"),
					factor(chain.NoahBaseDenom, "0.25"),
					factor(chain.USDBaseDenom, "1"),
				}
			},
		},
		{
			name: "non-positive noah cross is refused",
			mutate: func(genesis *types.GenesisState) {
				genesis.ConversionFactors = []types.ConversionFactor{factor(chain.NoahBaseDenom, "0")}
			},
			expectErr: "must be positive",
		},
		// The cross is the one mandatory entry: it is what lets a launch pay
		// gas before any rate exists.
		{
			name: "missing noah cross is refused",
			mutate: func(genesis *types.GenesisState) {
				genesis.ConversionFactors = []types.ConversionFactor{factor(chain.XDRBaseDenom, "1")}
			},
			expectErr: "must include the NOAH cross",
		},
		{
			name: "empty table is refused",
			mutate: func(genesis *types.GenesisState) {
				genesis.ConversionFactors = nil
			},
			expectErr: "must include the NOAH cross",
		},
		{
			name: "unsorted factors",
			mutate: func(genesis *types.GenesisState) {
				genesis.ConversionFactors = []types.ConversionFactor{
					factor(chain.USDBaseDenom, "1"),
					factor(chain.KRWBaseDenom, "1"),
				}
			},
			expectErr: "genesis conversion factors must be sorted by unique denom",
		},
		{
			name: "duplicate factor denom",
			mutate: func(genesis *types.GenesisState) {
				genesis.ConversionFactors = []types.ConversionFactor{
					factor(chain.USDBaseDenom, "1"),
					factor(chain.USDBaseDenom, "1"),
				}
			},
			expectErr: "genesis conversion factors must be sorted by unique denom",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			genesis := types.DefaultGenesisState()
			tc.mutate(genesis)

			err := genesis.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.expectErr)
		})
	}
}

// TestGenesisRewardFundingValidation covers both halves of what an imported
// window must satisfy: the shape rules, and the whole-window ceiling. An import
// arrives mid-accrual rather than being reached one block at a time, so it
// cannot inherit the bound the per-block accrual gets from MaxBlockRewardTarget,
// and that ceiling has to be imposed here directly.
func TestGenesisRewardFundingValidation(t *testing.T) {
	overAccrued := types.MaxBlockRewardTarget.
		Mul(math.NewIntFromUint64(types.MaxRewardFundingWindow)).
		Add(math.OneInt())

	tests := []struct {
		name      string
		mutate    func(*types.RewardFundingState)
		expectErr string
	}{
		{
			name:   "valid active window",
			mutate: func(funding *types.RewardFundingState) { funding.BlocksRemaining = 2 },
		},
		{
			name: "a populated window mid-accrual",
			mutate: func(funding *types.RewardFundingState) {
				funding.BlocksRemaining = 1
				funding.ValidatorTarget = math.NewInt(1_000)
			},
		},
		{
			name:      "unset validator target",
			mutate:    func(funding *types.RewardFundingState) { funding.ValidatorTarget = math.Int{} },
			expectErr: "validator target must be set",
		},
		{
			name: "negative validator fee value",
			mutate: func(funding *types.RewardFundingState) {
				funding.BlocksRemaining = 1
				funding.ValidatorFeeValue = math.NewInt(-1)
			},
			expectErr: "validator fee value must be zero or positive",
		},
		{
			name:      "completed window",
			mutate:    func(funding *types.RewardFundingState) { funding.ValidatorTarget = math.OneInt() },
			expectErr: "empty reward funding window must use the default state",
		},
		{
			name:      "noncanonical empty window",
			mutate:    func(funding *types.RewardFundingState) { funding.ValidatorFeeValue = math.OneInt() },
			expectErr: "empty reward funding window must use the default state",
		},
		{
			name: "validator target beyond a whole window",
			mutate: func(funding *types.RewardFundingState) {
				funding.BlocksRemaining = 1
				funding.ValidatorTarget = overAccrued
			},
			expectErr: "validator target must not exceed",
		},
		{
			name: "oracle target beyond a whole window",
			mutate: func(funding *types.RewardFundingState) {
				funding.BlocksRemaining = 1
				funding.OracleTarget = overAccrued
			},
			expectErr: "oracle target must not exceed",
		},
		{
			name: "fee value beyond a whole window",
			mutate: func(funding *types.RewardFundingState) {
				funding.BlocksRemaining = 1
				funding.ValidatorFeeValue = overAccrued
			},
			expectErr: "validator fee value must not exceed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			genesis := types.DefaultGenesisState()
			tc.mutate(&genesis.RewardFunding)

			err := genesis.Validate()
			if tc.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.expectErr)
		})
	}
}
