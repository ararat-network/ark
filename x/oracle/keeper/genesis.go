package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/x/oracle/types"
)

// InitGenesis imports oracle genesis state.
func (k Keeper) InitGenesis(ctx context.Context, data *types.GenesisState) error {
	if data == nil {
		return fmt.Errorf("oracle genesis state is nil")
	}
	if err := data.Validate(); err != nil {
		return fmt.Errorf("invalid oracle genesis state: %w", err)
	}

	genesisTime := sdk.UnwrapSDKContext(ctx).BlockTime()
	for _, er := range data.ExchangeRates {
		if er.BlockTimestamp.After(genesisTime) {
			return fmt.Errorf(
				"genesis exchange rate for denom %s has timestamp %s after genesis block time %s",
				er.Denom,
				er.BlockTimestamp,
				genesisTime,
			)
		}
	}

	// Check that the module account exists before writing module state.
	moduleAcc := k.accountKeeper.GetModuleAccount(ctx, types.ModuleName)
	if moduleAcc == nil {
		return fmt.Errorf("%s module account has not been set", types.ModuleName)
	}

	if err := k.Accounting.Set(ctx, data.Accounting); err != nil {
		return fmt.Errorf("setting accounting state: %w", err)
	}

	for _, er := range data.ExchangeRates {
		if err := k.ExchangeRate.Set(ctx, er.Denom, er); err != nil {
			return fmt.Errorf("setting genesis exchange rate for denom %s: %w", er.Denom, err)
		}
	}

	for _, sw := range data.RewardWeights {
		operator, err := sdk.ValAddressFromBech32(sw.ValidatorAddress)
		if err != nil {
			return fmt.Errorf("parsing reward weight validator address %q: %w", sw.ValidatorAddress, err)
		}

		if err := k.RewardWeight.Set(ctx, operator, sw.RewardWeight); err != nil {
			return fmt.Errorf("setting genesis reward weight for validator %s: %w", operator, err)
		}
	}

	for _, mc := range data.MissCounts {
		operator, err := sdk.ValAddressFromBech32(mc.ValidatorAddress)
		if err != nil {
			return fmt.Errorf("parsing miss count validator address %q: %w", mc.ValidatorAddress, err)
		}

		if err := k.MissCount.Set(ctx, operator, mc.MissCount); err != nil {
			return fmt.Errorf("setting genesis miss counter for validator %s: %w", operator, err)
		}
	}

	if err := k.VoteTargets.Set(ctx, data.VoteTargets); err != nil {
		return fmt.Errorf("setting vote targets: %w", err)
	}

	if err := k.Params.Set(ctx, data.Params); err != nil {
		return fmt.Errorf("setting params: %w", err)
	}

	for _, tt := range data.Params.TobinTaxes {
		k.registerTobinTaxMetadata(ctx, tt.Denom)
	}

	return nil
}

// ExportGenesis exports oracle store state.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting params: %w", err)
	}
	accounting, err := k.Accounting.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting accounting state: %w", err)
	}

	exchangeRates := []types.ExchangeRate{}
	if err := k.ExchangeRate.Walk(ctx, nil, func(denom string, exchangeRate types.ExchangeRate) (bool, error) {
		exchangeRates = append(exchangeRates, types.ExchangeRate{
			Denom:          denom,
			Rate:           exchangeRate.Rate,
			BlockTimestamp: exchangeRate.BlockTimestamp,
			BlockHeight:    exchangeRate.BlockHeight,
		})
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating exchange rates: %w", err)
	}

	rewardWeights := []types.RewardWeight{}
	if err := k.RewardWeight.Walk(ctx, nil, func(operator sdk.ValAddress, rewardWeight math.Int) (bool, error) {
		rewardWeights = append(rewardWeights, types.RewardWeight{
			ValidatorAddress: operator.String(),
			RewardWeight:     rewardWeight,
		})
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating reward weights: %w", err)
	}

	missCounts := []types.MissCount{}
	if err := k.MissCount.Walk(ctx, nil, func(operator sdk.ValAddress, missCount uint64) (bool, error) {
		missCounts = append(missCounts, types.MissCount{
			ValidatorAddress: operator.String(),
			MissCount:        missCount,
		})
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating miss counts: %w", err)
	}

	voteTargets, err := k.VoteTargets.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting vote targets: %w", err)
	}

	return types.NewGenesisState(
		params,
		accounting,
		exchangeRates,
		rewardWeights,
		missCounts,
		voteTargets,
	), nil
}
