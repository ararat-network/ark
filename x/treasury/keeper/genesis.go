package keeper

import (
	"context"
	"fmt"
	"slices"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	oracletypes "ark/x/oracle/types"
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
		recipient, err := types.ParseCanonicalAccountAddress("claim recipient", claim.Recipient)
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
	if data.ClaimsMandate.Committee != "" {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		if data.ClaimsMandate.Committee == k.authority || sdk.ValidateAuthority(sdkCtx, k.authority, data.ClaimsMandate.Committee) == nil {
			return fmt.Errorf("Claims committee must be distinct from Treasury authority")
		}
	}
	if data.MonetaryMandate.Committee != "" {
		sdkCtx := sdk.UnwrapSDKContext(ctx)
		committee := data.MonetaryMandate.Committee
		if committee == k.authority || sdk.ValidateAuthority(sdkCtx, k.authority, committee) == nil {
			return fmt.Errorf("monetary-policy committee must be distinct from Treasury authority")
		}
	}

	taxCaps := append([]types.TaxCap(nil), data.TaxCaps...)
	if len(taxCaps) == 0 {
		derivedTaxCaps, err := k.BuildTaxCaps(ctx, data.Params)
		if err != nil {
			return fmt.Errorf("deriving genesis tax caps: %w", err)
		}
		taxCaps = derivedTaxCaps
	} else {
		tobinTaxes, err := k.oracleKeeper.GetTobinTaxes(ctx)
		if err != nil {
			return fmt.Errorf("getting Tobin taxes: %w", err)
		}
		if !slices.ContainsFunc(tobinTaxes, func(tax oracletypes.TobinTax) bool {
			return tax.Denom == data.Params.ReferenceTaxCap.Denom
		}) {
			return fmt.Errorf(
				"reference tax cap denom %s is not configured in oracle",
				data.Params.ReferenceTaxCap.Denom,
			)
		}
		if len(taxCaps) != len(tobinTaxes) {
			return fmt.Errorf(
				"genesis tax cap set has %d denoms; expected %d configured native stables",
				len(taxCaps),
				len(tobinTaxes),
			)
		}
		seen := make(map[string]math.Int, len(taxCaps))
		for _, cap := range taxCaps {
			seen[cap.Denom] = cap.TaxCap
		}
		for _, tax := range tobinTaxes {
			if _, ok := seen[tax.Denom]; !ok {
				return fmt.Errorf("genesis tax cap is missing configured native stable %s", tax.Denom)
			}
		}
		if amount, ok := seen[data.Params.ReferenceTaxCap.Denom]; !ok || !amount.Equal(data.Params.ReferenceTaxCap.Amount) {
			return fmt.Errorf(
				"genesis tax cap for reference denom %s must equal %s",
				data.Params.ReferenceTaxCap.Denom,
				&data.Params.ReferenceTaxCap.Amount,
			)
		}
	}

	insuranceBalance := math.ZeroInt()
	for _, moduleName := range types.FundAccountNames() {
		moduleAccount := k.accountKeeper.GetModuleAccount(ctx, moduleName)
		if moduleAccount == nil {
			return fmt.Errorf("%s module account has not been set", moduleName)
		}
		balances := k.bankKeeper.GetAllBalances(ctx, moduleAccount.GetAddress())
		for _, balance := range balances {
			if balance.Denom != chain.MicroNoahDenom {
				return fmt.Errorf(
					"%s fund account contains unsupported genesis denom %s",
					moduleName,
					balance.Denom,
				)
			}
		}
		if moduleName == types.InsuranceName {
			insuranceBalance = balances.AmountOf(chain.MicroNoahDenom)
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
	if err := k.ReplaceTaxCaps(ctx, taxCaps); err != nil {
		return fmt.Errorf("setting tax caps: %w", err)
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
		Params:              params,
		MonetaryPolicy:      monetaryPolicy,
		TaxCaps:             taxCaps,
		ClaimsMandate:       claimsMandate,
		ClaimsAllowanceUsed: claimsAllowanceUsed,
		InsuranceReserved:   insuranceReserved,
		NextClaimId:         nextClaimID,
		Claims:              claims,
		RewardFunding:       rewardFunding,
		MonetaryMandate:     monetaryMandate,
	}, nil
}
