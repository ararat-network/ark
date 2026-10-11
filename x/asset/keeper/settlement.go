package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/asset/types"
)

// GetSettlementPlan returns the stored settlement plan whether or not its
// activation time has arrived. Treasury recognises the liability at the plan
// rate from plan open, and a cancelled pending plan drops it again, while
// Market's activation-gated ActiveSettlementPlan governs when holders may execute.
func (k Keeper) GetSettlementPlan(ctx context.Context, denom string) (types.SettlementPlan, bool, error) {
	plan, err := k.SettlementPlans.Get(ctx, denom)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.SettlementPlan{}, false, nil
		}
		return types.SettlementPlan{}, false, fmt.Errorf(
			"getting settlement plan for asset %s: %w",
			denom,
			err,
		)
	}

	return plan, true, nil
}

// ActiveSettlementPlan returns the asset's settlement plan when one exists and
// has activated, which is the only state a redemption may execute against. An
// announced-but-not-yet-activated plan reports false: the activation delay is
// the window governance gives holders to see the terms before they bind.
func (k Keeper) ActiveSettlementPlan(ctx context.Context, denom string) (types.SettlementPlan, bool, error) {
	plan, found, err := k.GetSettlementPlan(ctx, denom)
	if err != nil || !found {
		return types.SettlementPlan{}, false, err
	}
	if !plan.IsActive(chain.BlockSeconds(ctx)) {
		return types.SettlementPlan{}, false, nil
	}

	return plan, true, nil
}

// OpenSettlement establishes fixed asset-to-NOAH redemption. Activation is derived from the
// execution time and governed delay; governance supplies a closing time after activation. See
// x/asset/README.md for the correction-window contract.
func (k Keeper) OpenSettlement(
	ctx context.Context,
	denom string,
	expectedVersion uint64,
	redemptionRate math.LegacyDec,
	earliestClosingTime uint64,
) error {
	asset, err := k.getAssetAtVersion(ctx, denom, expectedVersion)
	if err != nil {
		return err
	}
	if asset.Status != types.AssetStatus_ASSET_STATUS_SUSPENDED &&
		asset.Status != types.AssetStatus_ASSET_STATUS_WRITTEN_OFF {
		return errorsmod.Wrapf(
			types.ErrInvalidAssetTransition,
			"%s asset %s cannot open settlement",
			asset.Status,
			asset.Denom,
		)
	}

	supply := k.bankKeeper.GetSupply(ctx, denom)
	if !supply.IsPositive() {
		return errorsmod.Wrapf(
			types.ErrInvalidAssetTransition,
			"asset %s must have positive supply to open settlement",
			denom,
		)
	}
	if _, found, err := k.GetSettlementPlan(ctx, denom); err != nil {
		return err
	} else if found {
		return errorsmod.Wrapf(
			types.ErrInvalidAssetTransition,
			"asset %s already has a settlement plan",
			denom,
		)
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting asset params: %w", err)
	}
	// Validation caps the delay at a year and block time fits the header's int64, so the addition
	// cannot overflow for any stored value.
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := chain.BlockSeconds(ctx)

	plan := types.SettlementPlan{
		Denom:               denom,
		RedemptionRate:      redemptionRate,
		ActivationTime:      now + params.SettlementActivationDelay,
		EarliestClosingTime: earliestClosingTime,
		OpenedHeight:        chain.BlockHeight(ctx),
		OpenedTime:          now,
	}
	if err := plan.Validate(); err != nil {
		return errorsmod.Wrapf(
			types.ErrInvalidAssetTransition,
			"opening settlement for asset %s: %v",
			denom,
			err,
		)
	}

	// Opening a settlement over a written-off asset restores it to SUSPENDED:
	// exposure governance is settling is exposure it has stopped derecognizing.
	// From SUSPENDED the status already holds and only the plan moves.
	updated := asset
	updated.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED
	if err := k.advanceAsset(ctx, asset, updated); err != nil {
		return err
	}
	if err := k.SettlementPlans.Set(ctx, denom, plan); err != nil {
		return fmt.Errorf("setting settlement plan for asset %s: %w", denom, err)
	}

	if err := sdkCtx.EventManager().EmitTypedEvent(
		&types.EventSettlementOpened{
			SettlementPlan: plan,
		},
	); err != nil {
		return fmt.Errorf(
			"emitting settlement plan for asset %s: %w",
			plan.Denom,
			err,
		)
	}

	return nil
}

// CancelSettlement removes only a not-yet-active plan and leaves the asset suspended. After
// activation, the plan ends only through recovery, retirement, or write-off after its closing
// height.
func (k Keeper) CancelSettlement(ctx context.Context, denom string, expectedVersion uint64) error {
	asset, err := k.getAssetAtVersion(ctx, denom, expectedVersion)
	if err != nil {
		return err
	}
	if asset.Status != types.AssetStatus_ASSET_STATUS_SUSPENDED {
		return errorsmod.Wrapf(
			types.ErrInvalidAssetTransition,
			"%s asset %s cannot cancel settlement",
			asset.Status,
			asset.Denom,
		)
	}

	plan, found, err := k.GetSettlementPlan(ctx, denom)
	if err != nil {
		return err
	}
	if !found {
		return errorsmod.Wrap(types.ErrSettlementPlanNotFound, denom)
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if plan.IsActive(chain.BlockSeconds(ctx)) {
		return errorsmod.Wrapf(
			types.ErrInvalidAssetTransition,
			"asset %s settlement activated at time %d and can no longer be cancelled",
			denom,
			plan.ActivationTime,
		)
	}

	if err := k.SettlementPlans.Remove(ctx, denom); err != nil {
		return fmt.Errorf("removing settlement plan for asset %s: %w", denom, err)
	}
	// Cancelling leaves the asset itself untouched — it stays suspended — but
	// the version still advances, because the plan it names is governance state
	// that proposals pipeline against.
	if err := k.advanceAsset(ctx, asset, asset); err != nil {
		return err
	}
	if err := sdkCtx.EventManager().EmitTypedEvent(
		&types.EventSettlementCancelled{
			Denom:   denom,
			Version: asset.Version + 1,
		},
	); err != nil {
		return fmt.Errorf(
			"emitting settlement cancellation for asset %s: %w",
			denom,
			err,
		)
	}

	return nil
}

// WriteOffAsset derecognises suspended exposure without changing balances. Any settlement's
// earliest closing time must have passed before its redemption entitlement can end.
func (k Keeper) WriteOffAsset(ctx context.Context, denom string, expectedVersion uint64) error {
	asset, err := k.getAssetAtVersion(ctx, denom, expectedVersion)
	if err != nil {
		return err
	}
	if asset.Status != types.AssetStatus_ASSET_STATUS_SUSPENDED {
		return errorsmod.Wrapf(
			types.ErrInvalidAssetTransition,
			"%s asset %s cannot write off",
			asset.Status,
			asset.Denom,
		)
	}

	supply := k.bankKeeper.GetSupply(ctx, denom)
	if !supply.IsPositive() {
		return errorsmod.Wrapf(
			types.ErrInvalidAssetTransition,
			"asset %s must have positive supply to write off",
			denom,
		)
	}
	plan, hasPlan, err := k.GetSettlementPlan(ctx, denom)
	if err != nil {
		return err
	}

	// Closing follows activation, so this comparison also protects pending plans. Derecognition
	// before activation requires cancelling the plan first.
	blockHeight := chain.BlockHeight(ctx)
	if now := chain.BlockSeconds(ctx); hasPlan && now < plan.EarliestClosingTime {
		return errorsmod.Wrapf(
			types.ErrInvalidAssetTransition,
			"asset %s settlement is committed until time %d and cannot be written off at time %d",
			denom,
			plan.EarliestClosingTime,
			now,
		)
	}
	record := types.ResolutionRecord{
		Denom:             denom,
		Kind:              types.ResolutionKind_RESOLUTION_KIND_WRITE_OFF,
		Version:           asset.Version + 1,
		ResolutionHeight:  blockHeight,
		OutstandingSupply: supply,
	}
	if hasPlan {
		planCopy := plan
		record.SettlementPlan = &planCopy
	}
	if err := k.validateResolutionRecord(ctx, record); err != nil {
		return err
	}

	updated := asset
	updated.Status = types.AssetStatus_ASSET_STATUS_WRITTEN_OFF
	if err := k.advanceAsset(ctx, asset, updated); err != nil {
		return err
	}
	if err := k.closeSettlementPlan(ctx, denom, record.Version); err != nil {
		return err
	}

	return k.writeResolutionRecord(ctx, record)
}

// closeSettlementPlan removes any plan attached to the asset and records the
// closure. It is shared by every path that ends an activated plan — recovery,
// retirement, and write-off — so the plan record never outlives the state that
// gave it meaning and every closure is observable on the same event.
func (k Keeper) closeSettlementPlan(ctx context.Context, denom string, version uint64) error {
	plan, found, err := k.GetSettlementPlan(ctx, denom)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if err := k.SettlementPlans.Remove(ctx, denom); err != nil {
		return fmt.Errorf("closing settlement plan for asset %s: %w", denom, err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := sdkCtx.EventManager().EmitTypedEvent(
		&types.EventSettlementClosed{
			SettlementPlan: plan,
			Version:        version,
			ClosedHeight:   chain.BlockHeight(ctx),
		},
	); err != nil {
		return fmt.Errorf(
			"emitting settlement closure for asset %s: %w",
			plan.Denom,
			err,
		)
	}

	return nil
}

// validateResolutionRecord checks contents and version uniqueness before any lifecycle or plan
// writes. Keeper calls have no internal rollback; callers build the complete record first and
// preserve it until writeResolutionRecord.
func (k Keeper) validateResolutionRecord(ctx context.Context, record types.ResolutionRecord) error {
	if err := record.Validate(); err != nil {
		return errorsmod.Wrapf(
			types.ErrInvalidAssetTransition,
			"recording %s resolution for asset %s: %v",
			record.Kind,
			record.Denom,
			err,
		)
	}

	exists, err := k.ResolutionRecords.Has(ctx, record.Key())
	if err != nil {
		return fmt.Errorf(
			"checking resolution record for asset %s version %d: %w",
			record.Denom,
			record.Version,
			err,
		)
	}
	if exists {
		return errorsmod.Wrapf(
			types.ErrInvalidAssetTransition,
			"asset %s already has a resolution record at version %d",
			record.Denom,
			record.Version,
		)
	}

	return nil
}

// writeResolutionRecord stores one derecognition record and announces it. It
// rejects nothing: validateResolutionRecord is the whole gate and must already
// have run, which is what lets this be the last write of a transition rather
// than a step that can still fail it.
func (k Keeper) writeResolutionRecord(ctx context.Context, record types.ResolutionRecord) error {
	if err := k.ResolutionRecords.Set(ctx, record.Key(), record); err != nil {
		return fmt.Errorf(
			"appending %s resolution for asset %s version %d: %w",
			record.Kind,
			record.Denom,
			record.Version,
			err,
		)
	}

	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(
		&types.EventAssetResolved{ResolutionRecord: record},
	); err != nil {
		return fmt.Errorf("emitting write-off for asset %s: %w", record.Denom, err)
	}
	return nil
}
