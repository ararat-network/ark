package types

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	DefaultTaxRate      = math.LegacyNewDecWithPrec(1, 3) // 0.1%
	DefaultRewardWeight = math.LegacyNewDecWithPrec(5, 2) // 5%
)

// NewGenesisState creates a new GenesisState object
func NewGenesisState(
	params Params,
	taxRate,
	rewardWeight math.LegacyDec,
	taxCaps []TaxCap,
	taxProceeds,
	epochInitialIssuance sdk.Coins,
	epochStates []EpochState,
) *GenesisState {
	return &GenesisState{
		Params:               params,
		TaxRate:              taxRate,
		RewardWeight:         rewardWeight,
		TaxCaps:              taxCaps,
		EpochTaxProceeds:     taxProceeds,
		EpochInitialIssuance: epochInitialIssuance,
		EpochStates:          epochStates,
	}
}

// DefaultGenesisState gets raw genesis raw message for testing
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		Params:               DefaultParams(),
		TaxRate:              DefaultTaxRate,
		RewardWeight:         DefaultRewardWeight,
		TaxCaps:              []TaxCap{},
		EpochTaxProceeds:     sdk.Coins{},
		EpochInitialIssuance: sdk.Coins{},
		EpochStates:          []EpochState{},
	}
}

// Validate validates the provided oracle genesis state to ensure the
// expected invariants holds. (i.e. params in correct bounds, no duplicate validators)
func (gs GenesisState) Validate() error {
	if gs.TaxRate.IsNil() {
		return errors.New("tax_rate must be set")
	}

	if gs.RewardWeight.IsNil() {
		return errors.New("reward_weight must be set")
	}

	if err := gs.Params.Validate(); err != nil {
		return err
	}

	if gs.TaxRate.LT(gs.Params.TaxPolicy.RateMin) || gs.TaxRate.GT(gs.Params.TaxPolicy.RateMax) {
		return fmt.Errorf("tax_rate must be less than RateMax(%s) and greater than RateMin(%s)", gs.Params.TaxPolicy.RateMax, gs.Params.TaxPolicy.RateMin)
	}

	if gs.RewardWeight.LT(gs.Params.RewardPolicy.RateMin) || gs.RewardWeight.GT(gs.Params.RewardPolicy.RateMax) {
		return fmt.Errorf("reward_weight must be less than WeightMax(%s) and greater than RateMin(%s)", gs.Params.RewardPolicy.RateMax, gs.Params.RewardPolicy.RateMin)
	}

	if gs.RewardWeight.Add(gs.Params.BurnWeight).GT(math.LegacyOneDec()) {
		return fmt.Errorf("sum of reward_weight and BurnWeight(%s) cannot be greater than one", gs.Params.BurnWeight)
	}

	seenTaxCaps := make(map[string]struct{}, len(gs.TaxCaps))
	for _, taxCap := range gs.TaxCaps {
		if err := sdk.ValidateDenom(taxCap.Denom); err != nil {
			return fmt.Errorf("tax cap denom is invalid: %s", taxCap.Denom)
		}
		if _, ok := seenTaxCaps[taxCap.Denom]; ok {
			return fmt.Errorf("duplicate tax cap for denom %s", taxCap.Denom)
		}
		seenTaxCaps[taxCap.Denom] = struct{}{}

		if taxCap.TaxCap.IsNil() {
			return fmt.Errorf("tax cap for %s must be set", taxCap.Denom)
		}
		if taxCap.TaxCap.IsNegative() {
			return fmt.Errorf("tax cap for %s must be zero or positive: %s", taxCap.Denom, taxCap.TaxCap)
		}
	}

	if !validGenesisCoins(gs.EpochTaxProceeds) {
		return errors.New("epoch_tax_proceeds must be valid")
	}

	if !validGenesisCoins(gs.EpochInitialIssuance) {
		return errors.New("epoch_initial_issuance must be valid")
	}

	seenEpochs := make(map[uint64]struct{}, len(gs.EpochStates))
	for _, epochState := range gs.EpochStates {
		if _, ok := seenEpochs[epochState.Epoch]; ok {
			return fmt.Errorf("duplicate epoch state for epoch %d", epochState.Epoch)
		}
		seenEpochs[epochState.Epoch] = struct{}{}

		if epochState.TaxReward.IsNil() {
			return fmt.Errorf("epoch state %d tax_reward must be set", epochState.Epoch)
		}
		if epochState.TaxReward.IsNegative() {
			return fmt.Errorf("epoch state %d tax_reward must be zero or positive: %s", epochState.Epoch, epochState.TaxReward)
		}
		if epochState.SeigniorageReward.IsNil() {
			return fmt.Errorf("epoch state %d seigniorage_reward must be set", epochState.Epoch)
		}
		if epochState.SeigniorageReward.IsNegative() {
			return fmt.Errorf("epoch state %d seigniorage_reward must be zero or positive: %s", epochState.Epoch, epochState.SeigniorageReward)
		}
		if epochState.TotalStakedArk.IsNil() {
			return fmt.Errorf("epoch state %d total_staked_ark must be set", epochState.Epoch)
		}
		if epochState.TotalStakedArk.IsNegative() {
			return fmt.Errorf("epoch state %d total_staked_ark must be zero or positive: %s", epochState.Epoch, epochState.TotalStakedArk)
		}
	}

	return nil
}

func validGenesisCoins(coins sdk.Coins) bool {
	seenDenoms := make(map[string]struct{}, len(coins))
	for _, coin := range coins {
		if err := sdk.ValidateDenom(coin.Denom); err != nil {
			return false
		}
		if coin.Amount.IsNil() || !coin.Amount.IsPositive() {
			return false
		}
		if _, ok := seenDenoms[coin.Denom]; ok {
			return false
		}
		seenDenoms[coin.Denom] = struct{}{}
	}

	return sort.IsSorted(coins)
}

// GetGenesisStateFromAppState returns x/treasury GenesisState given raw application
// genesis state.
func GetGenesisStateFromAppState(cdc codec.JSONCodec, appState map[string]json.RawMessage) *GenesisState {
	var genesisState GenesisState

	if appState[ModuleName] != nil {
		cdc.MustUnmarshalJSON(appState[ModuleName], &genesisState)
	}

	return &genesisState
}
