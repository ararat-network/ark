package keeper

import (
	"context"
	"fmt"

	"ark/x/market/types"
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

	// The virtual pool prices conversion in the protocol reference unit, so a
	// market genesis whose pool disagrees with the configured reference is not
	// a launchable configuration. x/oracle imports before x/market, so the
	// reference denom is already in state here.
	referenceDenom, err := k.oracleKeeper.GetReferenceDenom(ctx)
	if err != nil {
		return fmt.Errorf("getting protocol reference: %w", err)
	}
	if referenceDenom == "" {
		return fmt.Errorf(
			"base pool denom %s requires a configured protocol reference",
			data.ConversionPolicy.BasePool.Denom,
		)
	}
	if referenceDenom != data.ConversionPolicy.BasePool.Denom {
		return fmt.Errorf(
			"base pool denom %s must be the protocol reference %s",
			data.ConversionPolicy.BasePool.Denom,
			referenceDenom,
		)
	}
	// Governance already holds the unbounded conversion path, so an appointment
	// naming the authority is a delegation to nobody that still reads as a live
	// fast path.
	if data.ConversionMandate.Committee == k.authority {
		return fmt.Errorf("conversion committee must be distinct from Market authority")
	}

	if err := k.Params.Set(ctx, data.Params); err != nil {
		return fmt.Errorf("setting params: %w", err)
	}
	if err := k.ConversionPolicy.Set(ctx, data.ConversionPolicy); err != nil {
		return fmt.Errorf("setting conversion policy: %w", err)
	}
	if err := k.ConversionMandate.Set(ctx, data.ConversionMandate); err != nil {
		return fmt.Errorf("setting conversion mandate: %w", err)
	}
	if err := k.ArkPoolDelta.Set(ctx, data.ArkPoolDelta); err != nil {
		return fmt.Errorf("setting ark pool delta: %w", err)
	}
	// Overrides are governance judgment that cannot be re-derived, so genesis
	// is the only way they survive an export and import cycle.
	for _, override := range data.TobinTaxOverrides {
		if err := k.TobinTaxOverrides.Set(ctx, override.Denom, override.TobinTax); err != nil {
			return fmt.Errorf(
				"setting genesis tobin tax override for %s: %w",
				override.Denom,
				err,
			)
		}
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
	overrides, err := k.GetTobinTaxOverrides(ctx)
	if err != nil {
		return nil, err
	}
	conversionPolicy, err := k.ConversionPolicy.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting conversion policy: %w", err)
	}
	conversionMandate, err := k.ConversionMandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting conversion mandate: %w", err)
	}

	genesis := types.NewGenesisState(params, arkPoolDelta, overrides, conversionPolicy, conversionMandate)

	return genesis, nil
}
