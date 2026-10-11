package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/claims/types"
)

// InitGenesis validates and imports Claims state. Insurance custody is
// imported by Bank and is inspected here, never duplicated or minted.
func (k *Keeper) InitGenesis(ctx context.Context, data *types.GenesisState) error {
	if data == nil {
		return fmt.Errorf("claims genesis state is nil")
	}
	if err := data.Validate(); err != nil {
		return fmt.Errorf("invalid Claims genesis state: %w", err)
	}
	// An appointment naming the authority is a delegation to nobody that still
	// reads as a live fast path; the message server refuses it and so must the import.
	if !data.ClaimsMandate.IsDisabled() && data.ClaimsMandate.Committee == k.authority {
		return fmt.Errorf("Claims committee must be distinct from the Claims authority")
	}
	// Recipient payability is checked at settlement, not import. A pending claim with a blocked
	// recipient remains importable and settles as failed with its reservation released.
	moduleAccount := k.accountKeeper.GetModuleAccount(ctx, types.InsuranceName)
	if moduleAccount == nil {
		return fmt.Errorf("%s module account has not been set", types.InsuranceName)
	}
	balances := k.bankKeeper.GetAllBalances(ctx, moduleAccount.GetAddress())
	// Custody holds what the send restriction admits: NOAH and registered assets.
	for _, balance := range balances {
		if balance.Denom == chain.NoahBaseDenom {
			continue
		}
		registered, err := k.assetKeeper.HasAsset(ctx, balance.Denom)
		if err != nil {
			return fmt.Errorf("checking the asset registry for %s: %w", balance.Denom, err)
		}
		if !registered {
			return fmt.Errorf(
				"%s account contains unregistered genesis denom %s",
				types.InsuranceName,
				balance.Denom,
			)
		}
	}
	for _, reserved := range data.InsuranceReserved {
		if held := balances.AmountOf(reserved.Denom); reserved.Amount.GT(held) {
			return fmt.Errorf(
				"pending Insurance reservation %s exceeds Insurance balance %s",
				reserved,
				sdk.NewCoin(reserved.Denom, held),
			)
		}
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
	for _, reserved := range data.InsuranceReserved {
		if err := k.InsuranceReserved.Set(ctx, reserved.Denom, reserved.Amount); err != nil {
			return fmt.Errorf("setting %s Insurance reservation: %w", reserved.Denom, err)
		}
	}
	if err := k.NextClaimID.Set(ctx, data.NextClaimId); err != nil {
		return fmt.Errorf("setting next claim ID: %w", err)
	}
	for _, claim := range data.Claims {
		if err := k.Claims.Set(ctx, claim.ClaimId, claim); err != nil {
			return fmt.Errorf("setting claim %d: %w", claim.ClaimId, err)
		}
		// The settlement index is derived, not exported: a pending claim is due
		// at its own closing time, and a finalised one is due never. An
		// import whose claims are already payable settles in the first EndBlock.
		if claim.Status != types.ClaimStatus_CLAIM_STATUS_PENDING {
			continue
		}
		if err := k.DueClaims.Set(ctx, collections.Join(claim.ClosingTime, claim.ClaimId)); err != nil {
			return fmt.Errorf("indexing claim %d for settlement: %w", claim.ClaimId, err)
		}
	}

	return nil
}

// ExportGenesis exports exactly the Claims-owned state. The Insurance balance
// remains part of Bank genesis.
func (k *Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
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
	insuranceReserved, err := k.reservations(ctx)
	if err != nil {
		return nil, err
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
