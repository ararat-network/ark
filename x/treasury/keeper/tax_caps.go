package keeper

import (
	"context"
	"fmt"
	"slices"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/types"
)

func (k Keeper) refreshTaxCaps(ctx context.Context, tobinTaxes []oracletypes.TobinTax, configuredDenoms map[string]struct{}) error {
	mismatch, err := k.taxCapDenomsMismatch(ctx, configuredDenoms)
	if err != nil {
		return fmt.Errorf("checking tax cap denominations: %w", err)
	}
	if !mismatch && !chain.IsPeriodLastBlock(ctx, chain.BlocksPerWeek) {
		return nil
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}
	caps, err := k.buildTaxCapsFromSnapshot(ctx, params, tobinTaxes)
	if err != nil {
		if !isValuationUnavailable(err) {
			return fmt.Errorf("building tax caps: %w", err)
		}

		reason := eventSkipReason(err)
		k.Logger(ctx).Warn("skipping Treasury tax-cap refresh", "reason", reason, "error", err)
		if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventTaxCapsUpdateSkipped{
			Reason: reason,
		}); err != nil {
			return fmt.Errorf("emitting Treasury tax-cap refresh skip event: %w", err)
		}
		return nil
	}

	if err := k.replaceTaxCaps(ctx, caps); err != nil {
		return fmt.Errorf("replacing tax caps: %w", err)
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventTaxCapsUpdated{
		TaxCaps: caps,
	}); err != nil {
		return fmt.Errorf("emitting Treasury tax-cap update event: %w", err)
	}
	return nil
}

func (k Keeper) buildTaxCaps(
	ctx context.Context,
	params types.Params,
) ([]types.TaxCap, error) {
	tobinTaxes, err := k.oracleKeeper.GetTobinTaxes(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting Tobin taxes: %w", err)
	}
	return k.buildTaxCapsFromSnapshot(ctx, params, tobinTaxes)
}

func (k Keeper) buildTaxCapsFromSnapshot(
	ctx context.Context,
	params types.Params,
	tobinTaxes []oracletypes.TobinTax,
) ([]types.TaxCap, error) {
	referenceTaxCap := params.ReferenceTaxCap
	if !slices.ContainsFunc(tobinTaxes, func(tax oracletypes.TobinTax) bool {
		return tax.Denom == referenceTaxCap.Denom
	}) {
		return nil, fmt.Errorf("reference tax cap denom %s is not configured in oracle", referenceTaxCap.Denom)
	}

	caps := make([]types.TaxCap, 0, len(tobinTaxes))
	if referenceTaxCap.IsZero() {
		for _, tax := range tobinTaxes {
			caps = append(caps, types.TaxCap{Denom: tax.Denom, TaxCap: math.ZeroInt()})
		}
		return caps, nil
	}
	if len(tobinTaxes) == 1 {
		return []types.TaxCap{{Denom: referenceTaxCap.Denom, TaxCap: referenceTaxCap.Amount}}, nil
	}

	denoms := make([]string, len(tobinTaxes))
	for i, tax := range tobinTaxes {
		denoms[i] = tax.Denom
	}
	rates, err := k.oracleKeeper.GetRateSet(ctx, denoms...)
	if err != nil {
		return nil, fmt.Errorf("capturing tax-cap rates: %w", err)
	}

	reference := sdk.NewDecCoinFromCoin(referenceTaxCap)
	for _, tax := range tobinTaxes {
		if tax.Denom == reference.Denom {
			caps = append(caps, types.TaxCap{Denom: tax.Denom, TaxCap: referenceTaxCap.Amount})
			continue
		}
		converted, err := rates.Convert(reference, tax.Denom)
		if err != nil {
			return nil, fmt.Errorf("converting tax cap from %s to %s: %w", reference.Denom, tax.Denom, err)
		}
		coin, _ := converted.TruncateDecimal()
		if !coin.Amount.IsPositive() {
			return nil, errorsmod.Wrapf(
				oracletypes.ErrConversionOutOfRange,
				"converting positive tax cap from %s to %s truncated to zero",
				reference.Denom,
				tax.Denom,
			)
		}
		caps = append(caps, types.TaxCap{Denom: tax.Denom, TaxCap: coin.Amount})
	}
	return caps, nil
}

func (k Keeper) replaceTaxCaps(ctx context.Context, caps []types.TaxCap) error {
	if err := k.TaxCaps.Clear(ctx, nil); err != nil {
		return fmt.Errorf("clearing tax caps: %w", err)
	}
	for _, cap := range caps {
		if err := k.TaxCaps.Set(ctx, cap.Denom, cap.TaxCap); err != nil {
			return fmt.Errorf("setting tax cap %s: %w", cap.Denom, err)
		}
	}
	return nil
}

func (k Keeper) taxCapDenomsMismatch(ctx context.Context, expected map[string]struct{}) (bool, error) {
	mismatch := false
	matched := 0
	if err := k.TaxCaps.Walk(ctx, nil, func(denom string, _ math.Int) (bool, error) {
		if _, ok := expected[denom]; !ok {
			mismatch = true
			return true, nil
		}
		matched++
		return false, nil
	}); err != nil {
		return false, err
	}
	return mismatch || matched != len(expected), nil
}
