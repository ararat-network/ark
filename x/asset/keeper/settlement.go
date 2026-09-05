package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/x/asset/types"
)

// GetSettlementPlan returns the stored settlement plan whether or not its
// activation height has arrived. Treasury recognises the liability from plan
// open — the commitment is irrevocable from that block — while Market's
// activation-gated ActiveSettlementPlan governs when holders may execute.
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
	if !plan.IsActive(sdk.UnwrapSDKContext(ctx).BlockHeight()) {
		return types.SettlementPlan{}, false, nil
	}

	return plan, true, nil
}

// OpenSettlement establishes a fixed one-way asset-to-NOAH redemption plan.
//
// Governance states the closing height and nothing else about timing: the plan
// activates SettlementActivationDelayBlocks past this block. The delay is the
// correction window and only governance benefits from it, so there is nothing
// to gain by letting a proposal name a later activation — every block before it
// is one where holders sit in a suspended asset they cannot redeem, against a
// plan that can still be cancelled. Deriving it also keeps a proposal from
// expiring: an absolute height chosen at drafting time falls inside the delay
// if voting runs long, and the settlement then fails at execution.
func (k Keeper) OpenSettlement(
	ctx context.Context,
	denom string,
	expectedVersion uint64,
	redemptionRate math.LegacyDec,
	earliestClosingHeight int64,
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
	// Validation caps the delay at a chain year, so the widening to int64 and
	// the addition below cannot overflow for any stored value.
	delayBlocks := int64(params.SettlementActivationDelayBlocks)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockHeight := sdkCtx.BlockHeight()
	activationHeight := blockHeight + delayBlocks

	plan := types.SettlementPlan{
		Denom:                 denom,
		RedemptionRate:        redemptionRate,
		ActivationHeight:      activationHeight,
		EarliestClosingHeight: earliestClosingHeight,
		OpenedHeight:          blockHeight,
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

// CancelSettlement removes a settlement plan that has not yet activated,
// leaving the asset suspended.
//
// This is the whole of the correction window: the activation delay exists so a
// mistaken plan can be withdrawn before it binds, and once holders can redeem
// there is nothing here to withdraw. From activation onward a plan ends only by
// RecoverAsset, FinaliseRetirement, or a WriteOffAsset past the announced
// closing height — so no message shortens the window a holder was shown.
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
	blockHeight := sdkCtx.BlockHeight()
	if plan.IsActive(blockHeight) {
		return errorsmod.Wrapf(
			types.ErrInvalidAssetTransition,
			"asset %s settlement activated at height %d and can no longer be cancelled",
			denom,
			plan.ActivationHeight,
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

// WriteOffAsset derecognizes suspended exposure without modifying balances.
//
// It is the only act that can leave a holder with nothing, so it is the only
// one the announced redemption window constrains. Every other plan-ending path
// either restores pricing or requires the supply already gone; this one ends
// the claim outright, which is why committing to a window and then writing off
// immediately must not be expressible.
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

	// The commitment is hard and this is the one message that could break it.
	// The closing height is validated to fall after activation, so a single
	// comparison covers the not-yet-activated plan too: governance that wants
	// to derecognize before holders could ever redeem cancels the plan first,
	// inside the correction window built for exactly that.
	blockHeight := sdk.UnwrapSDKContext(ctx).BlockHeight()
	if hasPlan && blockHeight < plan.EarliestClosingHeight {
		return errorsmod.Wrapf(
			types.ErrInvalidAssetTransition,
			"asset %s settlement is committed until height %d and cannot be written off at height %d",
			denom,
			plan.EarliestClosingHeight,
			blockHeight,
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
			ClosedHeight:   sdkCtx.BlockHeight(),
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

// validateResolutionRecord decides everything that could reject a derecognition
// record: its own contents, and whether a record already occupies its version.
//
// It is separate from the write because its callers need it at a different
// moment. Both derecognition paths advance the asset and close its settlement
// plan before the record is stored, and within one keeper call there is no
// cache context to unwind those writes, so a rejection discovered at the write
// site would leave the asset moved and the plan closed by a message that
// returned an error. Callers run this before their first write; nothing between
// it and writeResolutionRecord can change its answer, because the record is
// built whole beforehand and only these paths write this collection.
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
