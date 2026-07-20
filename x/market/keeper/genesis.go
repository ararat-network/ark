package keeper

import (
	"context"
	"errors"
	"fmt"

	"ark/x/market/types"
	oracletypes "ark/x/oracle/types"
)

// InitGenesis initialises the market module genesis
func (k Keeper) InitGenesis(ctx context.Context, data *types.GenesisState) error {
	if data == nil {
		return fmt.Errorf("market genesis state is nil")
	}
	if err := data.Validate(); err != nil {
		return fmt.Errorf("invalid market genesis state: %w", err)
	}

	// Check that the module account exists before writing module state.
	moduleAcc := k.accountKeeper.GetModuleAccount(ctx, types.ModuleName)
	if moduleAcc == nil {
		return fmt.Errorf("%s module account has not been set", types.ModuleName)
	}

	if _, err := k.oracleKeeper.GetTobinTax(ctx, data.Params.BasePool.Denom); err != nil {
		if errors.Is(err, oracletypes.ErrUnknownDenom) {
			return fmt.Errorf(
				"base pool denom %s is not configured in oracle: %w",
				data.Params.BasePool.Denom,
				err,
			)
		}
		return fmt.Errorf("checking base pool denom %s in oracle: %w", data.Params.BasePool.Denom, err)
	}

	if err := k.Params.Set(ctx, data.Params); err != nil {
		return fmt.Errorf("setting params: %w", err)
	}
	if err := k.ArkPoolDelta.Set(ctx, data.ArkPoolDelta); err != nil {
		return fmt.Errorf("setting ark pool delta: %w", err)
	}

	return nil
}

// ExportGenesis returns a GenesisState for a given context and keeper.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting params: %w", err)
	}
	arkPoolDelta, err := k.ArkPoolDelta.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting ark pool delta: %w", err)
	}

	return types.NewGenesisState(arkPoolDelta, params), nil
}
