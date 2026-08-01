package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	chain "ark/pkg/chain"
	"ark/x/treasury/types"
)

// InitGenesis validates and imports Treasury policy state. Fund custody is
// imported by Bank and is inspected here, never duplicated or minted.
func (k Keeper) InitGenesis(ctx context.Context, data *types.GenesisState) error {
	if data == nil {
		return fmt.Errorf("treasury genesis state is nil")
	}
	if err := data.Validate(); err != nil {
		return fmt.Errorf("invalid Treasury genesis state: %w", err)
	}
	for _, claim := range data.Claims {
		if claim.Status != types.ClaimStatus_CLAIM_STATUS_PENDING {
			continue
		}
		recipient, err := chain.ParseCanonicalAccountAddress("claim recipient", claim.Recipient)
		if err != nil {
			return fmt.Errorf("invalid pending claim %d: %w", claim.ClaimId, err)
		}
		if k.bankKeeper.BlockedAddr(recipient) {
			return fmt.Errorf(
				"invalid pending claim %d: claim recipient %s is blocked from receiving funds",
				claim.ClaimId,
				claim.Recipient,
			)
		}
	}
	// The InitGenesis context carries no consensus params, so these can only
	// compare against the fallback authority; the message path re-checks every
	// later replacement against the effective authority.
	if data.ClaimsMandate.Committee == k.authority {
		return fmt.Errorf("Claims committee must be distinct from Treasury authority")
	}
	if data.MonetaryMandate.Committee == k.authority {
		return fmt.Errorf("monetary-policy committee must be distinct from Treasury authority")
	}

	// The reference tax cap and Market's base pool are the same unit by
	// design, permanently: the protocol reference. A treasury genesis whose
	// cap disagrees with the configured reference is not a launchable
	// configuration. x/oracle imports before x/treasury, so the reference is
	// already in state here.
	referenceDenom, err := k.oracleKeeper.GetReferenceDenom(ctx)
	if err != nil {
		return fmt.Errorf("getting protocol reference: %w", err)
	}
	if referenceDenom == "" {
		return fmt.Errorf(
			"reference tax cap denom %s requires a configured protocol reference",
			data.Params.ReferenceTaxCap.Denom,
		)
	}
	if data.Params.ReferenceTaxCap.Denom != referenceDenom {
		return fmt.Errorf(
			"reference tax cap denom %s must be the protocol reference %s",
			data.Params.ReferenceTaxCap.Denom,
			referenceDenom,
		)
	}

	taxCaps := append([]types.TaxCap(nil), data.TaxCaps...)
	if len(taxCaps) == 0 {
		denoms, err := k.assetKeeper.PricedLiveDenoms(ctx)
		if err != nil {
			return fmt.Errorf("getting priced-live denominations: %w", err)
		}
		derivedTaxCaps, err := k.buildTaxCaps(ctx, data.Params, denoms)
		if err != nil {
			return fmt.Errorf("deriving genesis tax caps: %w", err)
		}
		taxCaps = derivedTaxCaps
	}
	// A supplied cap set is imported as-is, loose on both sides of membership.
	// A cap naming no member is the residue of a departure, and a member
	// holding no cap is the gap an activation opens until a rebuild lands —
	// live state maintains coverage eventually, not continuously, because a
	// rebuild skips while any needed rate is stale. An export taken inside
	// either window must remain importable, and the gap costs at import
	// exactly what it costs at runtime: the member is untaxed until the
	// membership trigger re-derives its cap, which the first BeginBlocker
	// attempts. Amounts are not checked against the reference either: a kept
	// cap is anchored to the reference amount it was derived under, which
	// later policy moves and truncation drift are both free to leave behind.

	insuranceBalance := math.ZeroInt()
	for _, moduleName := range types.FundAccountNames() {
		moduleAccount := k.accountKeeper.GetModuleAccount(ctx, moduleName)
		if moduleAccount == nil {
			return fmt.Errorf("%s module account has not been set", moduleName)
		}
		balances := k.bankKeeper.GetAllBalances(ctx, moduleAccount.GetAddress())
		for _, balance := range balances {
			if balance.Denom != chain.NoahBaseDenom {
				return fmt.Errorf(
					"%s fund account contains unsupported genesis denom %s",
					moduleName,
					balance.Denom,
				)
			}
		}
		if moduleName == types.InsuranceName {
			insuranceBalance = balances.AmountOf(chain.NoahBaseDenom)
		}
	}
	if data.InsuranceReserved.GT(insuranceBalance) {
		return fmt.Errorf(
			"pending Insurance reservation %s exceeds Insurance balance %s",
			data.InsuranceReserved,
			insuranceBalance,
		)
	}

	if err := k.Params.Set(ctx, data.Params); err != nil {
		return fmt.Errorf("setting params: %w", err)
	}
	if err := k.MonetaryPolicy.Set(ctx, data.MonetaryPolicy); err != nil {
		return fmt.Errorf("setting monetary policy: %w", err)
	}
	for _, cap := range taxCaps {
		if err := k.TaxCaps.Set(ctx, cap.Denom, cap.TaxCap); err != nil {
			return fmt.Errorf("setting tax cap %s: %w", cap.Denom, err)
		}
	}
	// The cadence flag is imported state rather than something this import
	// derives: an export taken while a refresh was owed keeps it owed on the
	// new chain, and a fresh genesis carries false so block 1 does not rebuild
	// what genesis just established.
	if err := k.TaxCapRefreshPending.Set(ctx, data.TaxCapRefreshPending); err != nil {
		return fmt.Errorf("setting pending tax cap refresh: %w", err)
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
	if err := k.RewardFunding.Set(ctx, data.RewardFunding); err != nil {
		return fmt.Errorf("setting reward funding state: %w", err)
	}
	if err := k.MonetaryMandate.Set(ctx, data.MonetaryMandate); err != nil {
		return fmt.Errorf("setting monetary mandate: %w", err)
	}
	for _, claim := range data.Claims {
		if err := k.Claims.Set(ctx, claim.ClaimId, claim); err != nil {
			return fmt.Errorf("setting claim %d: %w", claim.ClaimId, err)
		}
	}

	return nil
}

// ExportGenesis exports exactly the Treasury-owned policy state. Fund balances
// remain part of Bank genesis.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting params: %w", err)
	}
	monetaryPolicy, err := k.MonetaryPolicy.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting monetary policy: %w", err)
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
	rewardFunding, err := k.RewardFunding.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting reward funding state: %w", err)
	}
	monetaryMandate, err := k.MonetaryMandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting monetary mandate: %w", err)
	}
	nextClaimID, err := k.NextClaimID.Peek(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting next claim ID: %w", err)
	}
	taxCapRefreshPending, err := k.TaxCapRefreshPending.Get(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, fmt.Errorf("getting pending tax cap refresh: %w", err)
	}

	taxCaps := make([]types.TaxCap, 0)
	if err := k.TaxCaps.Walk(ctx, nil, func(denom string, amount math.Int) (bool, error) {
		taxCaps = append(taxCaps, types.TaxCap{Denom: denom, TaxCap: amount})
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating tax caps: %w", err)
	}

	claims := make([]types.Claim, 0)
	if err := k.Claims.Walk(ctx, nil, func(_ uint64, claim types.Claim) (bool, error) {
		claims = append(claims, claim)
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating claims: %w", err)
	}

	return &types.GenesisState{
		Params:               params,
		TaxCaps:              taxCaps,
		ClaimsMandate:        claimsMandate,
		ClaimsAllowanceUsed:  claimsAllowanceUsed,
		InsuranceReserved:    insuranceReserved,
		NextClaimId:          nextClaimID,
		Claims:               claims,
		RewardFunding:        rewardFunding,
		MonetaryMandate:      monetaryMandate,
		MonetaryPolicy:       monetaryPolicy,
		TaxCapRefreshPending: taxCapRefreshPending,
	}, nil
}
