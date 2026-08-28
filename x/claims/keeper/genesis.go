package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/claims/types"
)

// InitGenesis validates and imports Claims state. Insurance custody is
// imported by Bank and is inspected here, never duplicated or minted.
func (k Keeper) InitGenesis(ctx context.Context, data *types.GenesisState) error {
	if data == nil {
		return fmt.Errorf("claims genesis state is nil")
	}
	if err := data.Validate(); err != nil {
		return fmt.Errorf("invalid Claims genesis state: %w", err)
	}
	// Whether a pending claim's recipient can still be paid is deliberately not
	// asked here. A recipient can be blocked while its claim is pending, which
	// is a state the chain already knows how to end — settlement re-tests it and
	// fails the claim, releasing its reservation. Rejecting the import instead
	// would make the same genesis file load under one binary and halt under
	// another, since the blocked set comes from app wiring rather than from the
	// file, and would leave an operator no remedy but to edit genesis by hand.
	moduleAccount := k.accountKeeper.GetModuleAccount(ctx, types.InsuranceName)
	if moduleAccount == nil {
		return fmt.Errorf("%s module account has not been set", types.InsuranceName)
	}
	balances := k.bankKeeper.GetAllBalances(ctx, moduleAccount.GetAddress())
	for _, balance := range balances {
		if balance.Denom != chain.NoahBaseDenom {
			return fmt.Errorf(
				"%s account contains unsupported genesis denom %s",
				types.InsuranceName,
				balance.Denom,
			)
		}
	}
	if data.InsuranceReserved.GT(balances.AmountOf(chain.NoahBaseDenom)) {
		return fmt.Errorf(
			"pending Insurance reservation %s exceeds Insurance balance %s",
			data.InsuranceReserved,
			balances.AmountOf(chain.NoahBaseDenom),
		)
	}

	if err := k.Params.Set(ctx, data.Params); err != nil {
		return fmt.Errorf("setting Claims params: %w", err)
	}
	if err := k.ClaimsMandate.Set(ctx, data.ClaimsMandate); err != nil {
		return fmt.Errorf("setting Claims mandate: %w", err)
	}
	if err := k.ClaimsAllowanceUsed.Set(ctx, data.ClaimsAllowanceUsed); err != nil {
		return fmt.Errorf("setting Claims allowance usage: %w", err)
	}
	if err := k.InsuranceReserved.Set(ctx, data.InsuranceReserved); err != nil {
		return fmt.Errorf("setting Insurance reservation: %w", err)
	}
	if err := k.NextClaimID.Set(ctx, data.NextClaimId); err != nil {
		return fmt.Errorf("setting next claim ID: %w", err)
	}
	for _, claim := range data.Claims {
		if err := k.Claims.Set(ctx, claim.ClaimId, claim); err != nil {
			return fmt.Errorf("setting claim %d: %w", claim.ClaimId, err)
		}
		// The settlement index is derived, not exported: a pending claim is due
		// at its own closing height, and a finalised one is due never. An
		// import whose claims are already payable settles in the first EndBlock.
		if claim.Status != types.ClaimStatus_CLAIM_STATUS_PENDING {
			continue
		}
		if err := k.DueClaims.Set(ctx, collections.Join(claim.ClosingHeight, claim.ClaimId)); err != nil {
			return fmt.Errorf("indexing claim %d for settlement: %w", claim.ClaimId, err)
		}
	}

	return nil
}

// ExportGenesis exports exactly the Claims-owned state. The Insurance balance
// remains part of Bank genesis.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting Claims params: %w", err)
	}
	claimsMandate, err := k.ClaimsMandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting Claims mandate: %w", err)
	}
	claimsAllowanceUsed, err := k.ClaimsAllowanceUsed.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting Claims allowance usage: %w", err)
	}
	insuranceReserved, err := k.InsuranceReserved.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting Insurance reservation: %w", err)
	}
	nextClaimID, err := k.NextClaimID.Peek(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting next claim ID: %w", err)
	}

	claims := make([]types.Claim, 0)
	if err := k.Claims.Walk(ctx, nil, func(_ uint64, claim types.Claim) (bool, error) {
		claims = append(claims, claim)
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating claims: %w", err)
	}

	return &types.GenesisState{
		Params:              params,
		ClaimsMandate:       claimsMandate,
		ClaimsAllowanceUsed: claimsAllowanceUsed,
		InsuranceReserved:   insuranceReserved,
		NextClaimId:         nextClaimID,
		Claims:              claims,
	}, nil
}
