package keeper

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/asset/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// RegisterAsset admits an immutable denomination as ACTIVE, derives Bank metadata, and creates no
// supply. Its feed must already be active, so feed addition and registration require separate
// governance cycles. See x/asset/README.md for lifecycle policy.
func (k Keeper) RegisterAsset(ctx context.Context, denom string) error {
	// The denomination is validated before it is used to derive anything: the
	// derivation strips the base-unit prefix, which only a valid denomination
	// is guaranteed to carry.
	if err := chain.ValidatePricedDenom(denom); err != nil {
		return errorsmod.Wrapf(
			types.ErrInvalidAssetTransition,
			"invalid asset registration: %v",
			err,
		)
	}
	asset := types.Asset{
		Denom:    denom,
		Metadata: chain.NativeAssetMetadata(denom),
		Status:   types.AssetStatus_ASSET_STATUS_ACTIVE,
		Version:  1,
	}
	if err := asset.Validate(); err != nil {
		return errorsmod.Wrapf(
			types.ErrInvalidAssetTransition,
			"invalid asset registration: %v",
			err,
		)
	}

	exists, err := k.Assets.Has(ctx, asset.Denom)
	if err != nil {
		return fmt.Errorf("checking registered asset %s: %w", asset.Denom, err)
	}
	if exists {
		return errorsmod.Wrap(types.ErrAssetAlreadyExists, asset.Denom)
	}
	// A registration starts an asset at zero by construction, not by
	// assumption, and x/asset is the sole owner of the denomination's Bank
	// metadata from registration onward.
	if supply := k.bankKeeper.GetSupply(ctx, asset.Denom); !supply.IsZero() {
		return errorsmod.Wrapf(
			types.ErrAssetSupplyNotZero,
			"asset %s has supply %s",
			asset.Denom,
			supply.Amount,
		)
	}
	if _, found := k.bankKeeper.GetDenomMetaData(ctx, asset.Denom); found {
		return errorsmod.Wrapf(
			types.ErrAssetAlreadyExists,
			"denom %s already has Bank metadata",
			asset.Denom,
		)
	}
	// The feed is checked last of the preconditions so a denomination that is
	// already spoken for is reported as the conflict it is. A re-registration
	// whose feed has since been removed is an identity collision first; naming
	// the feed there would send governance to fix the wrong thing.
	if err := k.requireFeedActive(ctx, asset.Denom); err != nil {
		return err
	}

	if err := k.Assets.Set(ctx, asset.Denom, asset); err != nil {
		return fmt.Errorf("registering asset %s: %w", asset.Denom, err)
	}
	k.bankKeeper.SetDenomMetaData(ctx, asset.Metadata)
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(
		&types.EventAssetRegistered{Asset: asset},
	); err != nil {
		return fmt.Errorf("emitting registration for asset %s: %w", asset.Denom, err)
	}

	return nil
}

// HaltIssuance stops new issuance while preserving pricing, liability
// accounting, transfers, and redemption.
func (k Keeper) HaltIssuance(ctx context.Context, denom string, expectedVersion uint64) error {
	asset, err := k.getAssetAtVersion(ctx, denom, expectedVersion)
	if err != nil {
		return err
	}

	if asset.Status != types.AssetStatus_ASSET_STATUS_ACTIVE {
		return errorsmod.Wrapf(
			types.ErrInvalidAssetTransition,
			"%s asset %s cannot halt issuance",
			asset.Status,
			asset.Denom,
		)
	}

	updated := asset
	updated.Status = types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED

	return k.advanceAsset(ctx, asset, updated)
}

// ResumeIssuance returns an issuance-halted asset to active status. The status
// precondition is the whole guard: nothing about the feed changed while
// issuance was halted, so nothing has to be re-established to resume it.
func (k Keeper) ResumeIssuance(ctx context.Context, denom string, expectedVersion uint64) error {
	asset, err := k.getAssetAtVersion(ctx, denom, expectedVersion)
	if err != nil {
		return err
	}
	if asset.Status != types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED {
		return errorsmod.Wrapf(
			types.ErrInvalidAssetTransition,
			"%s asset %s cannot resume issuance",
			asset.Status,
			asset.Denom,
		)
	}

	updated := asset
	updated.Status = types.AssetStatus_ASSET_STATUS_ACTIVE

	return k.advanceAsset(ctx, asset, updated)
}

// SuspendAsset disables ordinary economic operations without altering holder
// balances or dormant policy dependencies.
func (k Keeper) SuspendAsset(ctx context.Context, denom string, expectedVersion uint64) error {
	asset, err := k.getAssetAtVersion(ctx, denom, expectedVersion)
	if err != nil {
		return err
	}

	return k.suspendAsset(ctx, asset)
}

// RecoverAsset immediately restores SUSPENDED or WRITTEN_OFF assets to ISSUANCE_HALTED under an
// active feed. It closes any settlement and restores ordinary pricing and redemption without
// enabling new issuance.
func (k Keeper) RecoverAsset(ctx context.Context, denom string, expectedVersion uint64) error {
	asset, err := k.getAssetAtVersion(ctx, denom, expectedVersion)
	if err != nil {
		return err
	}
	if asset.Status != types.AssetStatus_ASSET_STATUS_SUSPENDED &&
		asset.Status != types.AssetStatus_ASSET_STATUS_WRITTEN_OFF {
		return errorsmod.Wrapf(
			types.ErrInvalidAssetTransition,
			"%s asset %s cannot recover",
			asset.Status,
			asset.Denom,
		)
	}
	// The feed may have been legitimately removed while the asset was
	// suspended, so recovery is a referent-creating path and must re-check it.
	if err := k.requireFeedActive(ctx, denom); err != nil {
		return err
	}

	updated := asset
	updated.Status = types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED
	if err := k.advanceAsset(ctx, asset, updated); err != nil {
		return err
	}

	// Recovery replaces fixed settlement pricing with live Oracle pricing and closes the plan
	// regardless of its window or relative rate. Holders regain ordinary redemption.
	return k.closeSettlementPlan(ctx, denom, asset.Version+1)
}

// FinaliseRetirement permanently retires an asset and closes any plan. ISSUANCE_HALTED permits an
// approved residual; SUSPENDED requires zero supply; WRITTEN_OFF has already disclosed its
// residual. Registry and metadata tombstones prevent denomination reuse.
func (k Keeper) FinaliseRetirement(ctx context.Context, denom string, expectedVersion uint64, maxResidualSupply math.Int) error {
	asset, err := k.getAssetAtVersion(ctx, denom, expectedVersion)
	if err != nil {
		return err
	}
	if asset.Status != types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED &&
		asset.Status != types.AssetStatus_ASSET_STATUS_SUSPENDED &&
		asset.Status != types.AssetStatus_ASSET_STATUS_WRITTEN_OFF {
		return errorsmod.Wrapf(
			types.ErrInvalidAssetTransition,
			"%s asset %s cannot finalise retirement",
			asset.Status,
			asset.Denom,
		)
	}
	if maxResidualSupply.IsNil() || maxResidualSupply.IsNegative() {
		return errorsmod.Wrapf(
			types.ErrInvalidAssetTransition,
			"asset %s maximum residual supply must be set and non-negative",
			denom,
		)
	}
	// Only halted assets can approve a new residual. Suspended retirement requires zero supply, so
	// an attached plan has no remaining claimants. Retirement leaves reference and feed state
	// untouched.
	supply := k.bankKeeper.GetSupply(ctx, denom)
	switch asset.Status {
	case types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED:
		// Redemption has been continuously available in this status, so
		// governance may judge the remainder unredeemable and derecognize it
		// explicitly within the bound it approved.
		if supply.Amount.GT(maxResidualSupply) {
			return errorsmod.Wrapf(
				types.ErrAssetSupplyNotZero,
				"asset %s supply %s exceeds approved residual bound %s",
				denom,
				supply.Amount,
				maxResidualSupply,
			)
		}
	case types.AssetStatus_ASSET_STATUS_SUSPENDED:
		// Holders here may have had no exit, so a remainder is not governance's
		// to derecognize on this message; WriteOffAsset names it honestly.
		if !supply.IsZero() {
			return errorsmod.Wrapf(
				types.ErrAssetSupplyNotZero,
				"suspended asset %s has supply %s; write it off instead",
				denom,
				supply.Amount,
			)
		}
		if !maxResidualSupply.IsZero() {
			return errorsmod.Wrapf(
				types.ErrInvalidAssetTransition,
				"suspended asset %s cannot approve a residual; write it off instead",
				denom,
			)
		}
	case types.AssetStatus_ASSET_STATUS_WRITTEN_OFF:
		// WRITE_OFF already disclosed this residual; retirement approves no further derecognition
		// and therefore requires a zero bound.
		if !maxResidualSupply.IsZero() {
			return errorsmod.Wrapf(
				types.ErrInvalidAssetTransition,
				"written-off asset %s already disclosed its residual; approve zero",
				denom,
			)
		}
	}

	// Only one path discloses a residual, so only that path builds a record —
	// but it is built and validated here, before the asset moves, for the
	// reason validateResolutionRecord gives.
	var residual *types.ResolutionRecord
	if asset.Status == types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED && supply.IsPositive() {
		residual = &types.ResolutionRecord{
			Denom:             denom,
			Kind:              types.ResolutionKind_RESOLUTION_KIND_RETIREMENT_RESIDUAL,
			Version:           asset.Version + 1,
			ResolutionHeight:  sdk.UnwrapSDKContext(ctx).BlockHeight(),
			OutstandingSupply: supply,
		}
		if err := k.validateResolutionRecord(ctx, *residual); err != nil {
			return err
		}
	}

	// Retirement checks and records the residual bound immediately. Oracle owns feed membership
	// independently of asset status.
	updated := asset
	updated.Status = types.AssetStatus_ASSET_STATUS_RETIRED
	if err := k.advanceAsset(ctx, asset, updated); err != nil {
		return err
	}
	if err := k.closeSettlementPlan(ctx, denom, asset.Version+1); err != nil {
		return err
	}
	if residual == nil {
		return nil
	}

	return k.writeResolutionRecord(ctx, *residual)
}

// suspendAsset closes the only unbounded transmission channel from an asset
// failure into NOAH: ordinary conversion at the reference rate. The emergency
// committee reaches the same semantics through the mandate.
func (k Keeper) suspendAsset(ctx context.Context, asset types.Asset) error {
	if asset.Status != types.AssetStatus_ASSET_STATUS_ACTIVE &&
		asset.Status != types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED {
		return errorsmod.Wrapf(
			types.ErrInvalidAssetTransition,
			"%s asset %s cannot suspend",
			asset.Status,
			asset.Denom,
		)
	}
	// Suspension changes asset status while its feed keeps running. Market and Treasury enforce the
	// status gate independently of reference-feed pricing.
	updated := asset
	updated.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED

	return k.advanceAsset(ctx, asset, updated)
}

// requireFeedActive enforces the common feed-admission rule for registration, recovery, and
// genesis. An active feed may have no fresh rate; consumers enforce freshness at use time. See
// x/asset/README.md, "Feed dependency".
func (k Keeper) requireFeedActive(ctx context.Context, denom string) error {
	phase, err := k.oracleKeeper.FeedPhase(ctx, denom)
	if err != nil {
		return fmt.Errorf("getting feed phase for asset %s: %w", denom, err)
	}
	if phase != oracletypes.FeedPhaseActive {
		return errorsmod.Wrapf(
			types.ErrAssetNotPriceable,
			"asset %s feed must be in phase Active",
			denom,
		)
	}

	return nil
}
