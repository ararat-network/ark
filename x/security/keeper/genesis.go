package keeper

import (
	"context"
	"fmt"

	"ark/x/security/types"
)

// InitGenesis initialises the security module genesis.
func (k Keeper) InitGenesis(ctx context.Context, data *types.GenesisState) error {
	if data == nil {
		return fmt.Errorf("security genesis state is nil")
	}
	if err := data.Validate(); err != nil {
		return fmt.Errorf("invalid security genesis state: %w", err)
	}
	// An appointment naming the authority is a delegation to nobody that still
	// reads as a live fast path.
	if !data.SecurityMandate.IsDisabled() && data.SecurityMandate.Committee == k.authority {
		return fmt.Errorf("security committee must be distinct from the chain authority")
	}

	if err := k.Mandate.Set(ctx, data.SecurityMandate); err != nil {
		return fmt.Errorf("setting security mandate: %w", err)
	}
	if err := k.CommitteePlan.Set(ctx, data.CommitteePlan); err != nil {
		return fmt.Errorf("setting committee plan: %w", err)
	}

	return nil
}

// ExportGenesis returns a GenesisState for a given context and keeper.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	securityMandate, err := k.Mandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting security mandate: %w", err)
	}
	committeePlan, err := k.CommitteePlan.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting committee plan: %w", err)
	}

	return types.NewGenesisState(securityMandate, committeePlan), nil
}
