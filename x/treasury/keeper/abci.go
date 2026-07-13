package keeper

import (
	"context"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	arkmetrics "ark/pkg/metrics"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/types"
)

// EndBlocker is called at the end of every block
func (k Keeper) EndBlocker(ctx context.Context) (err error) {
	defer arkmetrics.RecordModuleMethodLatency(ctx, types.ModuleName, arkmetrics.EndBlock)()

	// Check epoch last block
	if !chain.IsPeriodLastBlock(ctx, chain.BlocksPerWeek) {
		return nil
	}

	// Record issuance after all epoch-end work completes, so the next epoch
	// starts with an accurate snapshot. Uses errors.Join to surface deferred
	// errors without masking any earlier error from the main body.
	defer func() {
		if deferErr := k.RecordEpochInitialIssuance(ctx); deferErr != nil {
			err = errors.Join(err, fmt.Errorf("recording epoch initial issuance: %w", deferErr))
		}
	}()

	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	pastProbation := sdkCtx.BlockHeight() >= int64(chain.BlocksPerWeek*params.WindowProbation)

	epochTaxProceeds, err := k.EpochTaxProceeds.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting epoch tax proceeds: %w", err)
	}
	rateDenoms := make([]string, 0, len(epochTaxProceeds.TaxProceeds)+2)
	rateDenoms = append(rateDenoms, chain.MicroNoahDenom, chain.MicroSDRDenom)
	for _, coin := range epochTaxProceeds.TaxProceeds {
		rateDenoms = append(rateDenoms, coin.Denom)
	}

	var tobinTaxes oracletypes.TobinTaxes
	if pastProbation {
		tobinTaxes, err = k.oracleKeeper.GetTobinTaxes(ctx)
		if err != nil {
			return fmt.Errorf("getting tobin taxes: %w", err)
		}
		rateDenoms = append(rateDenoms, params.TaxPolicy.Cap.Denom)
		for _, tobinTax := range tobinTaxes {
			rateDenoms = append(rateDenoms, tobinTax.Denom)
		}
	}

	rates, err := k.oracleKeeper.GetRateSnapshot(ctx, rateDenoms...)
	if err != nil {
		if errors.Is(err, oracletypes.ErrStaleExchangeRate) || errors.Is(err, oracletypes.ErrUnknownDenom) {
			k.Logger(ctx).Warn(
				"skipping treasury epoch policy update because oracle rates are unavailable",
				"epoch", k.GetEpoch(ctx),
				"error", err,
			)
			return nil
		}
		return fmt.Errorf("getting treasury rate snapshot: %w", err)
	}

	var taxCaps sdk.Coins
	if pastProbation {
		taxCaps, err = k.ComputeTaxCaps(ctx, tobinTaxes, rates)
		if err != nil {
			return fmt.Errorf("computing tax caps: %w", err)
		}
	}

	// Compute & Update internal indicators for the current epoch.
	if err := k.UpdateIndicators(ctx, rates); err != nil {
		return fmt.Errorf("updating indicators: %w", err)
	}

	if !pastProbation {
		return nil
	}

	// Settle seigniorage to oracle & distribution(community-pool) module-account
	if err := k.SettleSeigniorage(ctx); err != nil {
		return fmt.Errorf("settling seigniorage: %w", err)
	}

	// Update tax-rate and reward-weight of next epoch
	taxRate, err := k.UpdateTaxPolicy(ctx)
	if err != nil {
		return fmt.Errorf("updating tax policy: %w", err)
	}
	rewardWeight, err := k.UpdateRewardPolicy(ctx)
	if err != nil {
		return fmt.Errorf("updating reward policy: %w", err)
	}
	if err := k.SetTaxCaps(ctx, taxCaps); err != nil {
		return fmt.Errorf("setting tax caps: %w", err)
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypePolicyUpdate,
			sdk.NewAttribute(types.AttributeKeyTaxRate, taxRate.String()),
			sdk.NewAttribute(types.AttributeKeyRewardWeight, rewardWeight.String()),
			sdk.NewAttribute(types.AttributeKeyTaxCap, taxCaps.String()),
		),
	)
	return nil
}
