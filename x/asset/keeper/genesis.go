package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"

	"ark/x/asset/types"
)

// InitGenesis validates and imports asset registry state. Asset locks are
// reconstructed by their owning modules and are not imported here.
func (k Keeper) InitGenesis(ctx context.Context, data *types.GenesisState) error {
	if data == nil {
		return fmt.Errorf("asset genesis state is nil")
	}
	if err := data.Validate(); err != nil {
		return fmt.Errorf("invalid asset genesis state: %w", err)
	}
	// The InitGenesis context carries no consensus params, so this can only
	// compare against the fallback authority; the message path re-checks every
	// later replacement against the effective authority.
	if data.EmergencyMandate.Committee == k.authority {
		return fmt.Errorf("emergency committee must be distinct from the Asset authority")
	}

	if err := k.Params.Set(ctx, data.Params); err != nil {
		return fmt.Errorf("setting genesis asset params: %w", err)
	}

	resolvedDenoms := make(map[string]struct{}, len(data.ResolutionRecords))
	for _, record := range data.ResolutionRecords {
		resolvedDenoms[record.Denom] = struct{}{}
	}
	for _, asset := range data.Assets {
		if asset.Status == types.AssetStatus_ASSET_STATUS_RETIRED {
			supply := k.bankKeeper.GetSupply(ctx, asset.Denom)
			if _, resolved := resolvedDenoms[asset.Denom]; supply.IsPositive() && !resolved {
				return fmt.Errorf(
					"retired asset %s has residual supply %s without a resolution record",
					asset.Denom,
					supply.Amount,
				)
			}
		}
		// Feed existence lives in x/oracle, so it is a keeper-level rule rather
		// than a GenesisState one. An oracle-priced asset must name an active
		// feed, the same rule registration and recovery answer to: a launching
		// chain lists its feeds directly in the active set, and the two-block
		// activation delay is an artefact of runtime transitions alone, so import
		// reaches the rule with nothing to wait for. A suspended, written-off, or
		// retired asset may legitimately name a feed that does not exist yet or
		// has since been removed.
		if asset.IsOraclePriced() {
			if err := k.requireFeedActive(ctx, asset.Denom); err != nil {
				return fmt.Errorf("invalid genesis asset %s: %w", asset.Denom, err)
			}
		}
		if err := k.Assets.Set(ctx, asset.Denom, asset); err != nil {
			return fmt.Errorf("setting genesis asset %s: %w", asset.Denom, err)
		}
		k.bankKeeper.SetDenomMetaData(ctx, asset.Metadata)
	}
	for _, plan := range data.SettlementPlans {
		if err := k.SettlementPlans.Set(ctx, plan.Denom, plan); err != nil {
			return fmt.Errorf("setting genesis settlement plan for %s: %w", plan.Denom, err)
		}
	}
	for _, record := range data.ResolutionRecords {
		if err := k.ResolutionRecords.Set(ctx, record.Key(), record); err != nil {
			return fmt.Errorf(
				"setting genesis resolution record for %s version %d: %w",
				record.Denom,
				record.Version,
				err,
			)
		}
	}
	if err := k.EmergencyMandate.Set(ctx, data.EmergencyMandate); err != nil {
		return fmt.Errorf("setting genesis emergency mandate: %w", err)
	}
	for _, denom := range data.EmergencyActions {
		if err := k.EmergencyActions.Set(ctx, denom); err != nil {
			return fmt.Errorf(
				"setting genesis emergency action for asset %s: %w",
				denom,
				err,
			)
		}
	}

	return nil
}

// ExportGenesis exports asset-owned state in collection key order. Asset locks
// remain excluded because their owning modules reconstruct them.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting asset params: %w", err)
	}

	assets := []types.Asset{}
	if err := k.Assets.Walk(ctx, nil, func(_ string, asset types.Asset) (bool, error) {
		assets = append(assets, asset)
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating assets: %w", err)
	}

	settlementPlans := []types.SettlementPlan{}
	if err := k.SettlementPlans.Walk(
		ctx,
		nil,
		func(_ string, plan types.SettlementPlan) (bool, error) {
			settlementPlans = append(settlementPlans, plan)
			return false, nil
		},
	); err != nil {
		return nil, fmt.Errorf("iterating settlement plans: %w", err)
	}

	resolutionRecords := []types.ResolutionRecord{}
	if err := k.ResolutionRecords.Walk(
		ctx,
		nil,
		func(_ collections.Pair[string, uint64], record types.ResolutionRecord) (bool, error) {
			resolutionRecords = append(resolutionRecords, record)
			return false, nil
		},
	); err != nil {
		return nil, fmt.Errorf("iterating write-off records: %w", err)
	}

	emergencyMandate, err := k.EmergencyMandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting emergency mandate: %w", err)
	}

	emergencyActions := []string{}
	if err := k.EmergencyActions.Walk(
		ctx,
		nil,
		func(denom string) (bool, error) {
			emergencyActions = append(emergencyActions, denom)
			return false, nil
		},
	); err != nil {
		return nil, fmt.Errorf("iterating emergency actions: %w", err)
	}

	return &types.GenesisState{
		Params:            params,
		Assets:            assets,
		SettlementPlans:   settlementPlans,
		ResolutionRecords: resolutionRecords,
		EmergencyMandate:  emergencyMandate,
		EmergencyActions:  emergencyActions,
	}, nil
}
