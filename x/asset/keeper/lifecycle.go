package keeper

import (
	"context"
	"fmt"

	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/asset/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// RegisterAsset registers an immutable asset identity and admits it to normal
// policy use in the same act, without creating supply. The denomination is the
// entire input: Bank metadata is derived from it, so a registration has no
// field that could be misspelled and therefore none that a later message would
// need to correct.
//
// There is no state between registration and admission because none has
// anything to hold. The step that used to sit here waited for the feed, and an
// asset admitted the moment its feed is running is already what every consumer
// expects: admission never demanded a rate either, only a feed, so ACTIVE
// without a rate is a state the chain reaches whenever a feed is young or has
// gone stale, and consumers handle it identically in both cases.
//
// The feed must already be active, which is the same rule recovery and genesis
// import answer to. Registration cannot therefore share a proposal with
// MsgAddFeed: governance proposes the feed, watches it print rates, and
// proposes the registration in a second cycle. That is the cost of the rule and
// it falls where it is cheapest — registration is the one admission path with
// no holders waiting on it, and the one where the price data is least proven,
// since the asset goes convertible on the first rate its feed ever produces.
func (k Keeper) RegisterAsset(ctx context.Context, denom string) error {
	// The denomination is validated before it is used to derive anything: the
	// derivation strips the base-unit prefix, which only a valid denomination
	// is guaranteed to carry.
	if err := chain.ValidatePricedDenom(denom); err != nil {
		return sdkerrors.Wrapf(
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
		return sdkerrors.Wrapf(
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
		return sdkerrors.Wrap(types.ErrAssetAlreadyExists, asset.Denom)
	}
	// A registration starts an asset at zero by construction, not by
	// assumption, and x/asset is the sole owner of the denomination's Bank
	// metadata from registration onward.
	if supply := k.bankKeeper.GetSupply(ctx, asset.Denom); !supply.IsZero() {
		return sdkerrors.Wrapf(
			types.ErrAssetSupplyNotZero,
			"asset %s has supply %s",
			asset.Denom,
			supply.Amount,
		)
	}
	if _, found := k.bankKeeper.GetDenomMetaData(ctx, asset.Denom); found {
		return sdkerrors.Wrapf(
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
		return sdkerrors.Wrapf(
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
		return sdkerrors.Wrapf(
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

// RecoverAsset restores a suspended or written-off asset to issuance-halted
// status, undoing SuspendAsset.
//
// It takes effect in the block it executes rather than arming on a later rate.
// Governance watches the feed and satisfies itself the price is stable before
// proposing, so the decision and its effect describe the same evidence; an
// armed recovery would instead fire on whatever rate happened to arrive
// afterwards, moving the asset into Treasury's liability accounting and
// Market's convertible set on a price nobody voted on.
//
// It lands in ISSUANCE_HALTED rather than ACTIVE because restoring pricing and
// permitting new exposure are different bets on different evidence: the feed
// answers the first, whatever broke the asset answers the second. A halted
// asset also has a bounded holder set, so a premature recovery is re-suspendable
// with the exposure unchanged. And it is what keeps ISSUANCE_HALTED free of
// intent — recovered assets and winding-down assets share the status, so a
// wind-down halt cannot be read as distress.
func (k Keeper) RecoverAsset(ctx context.Context, denom string, expectedVersion uint64) error {
	asset, err := k.getAssetAtVersion(ctx, denom, expectedVersion)
	if err != nil {
		return err
	}
	if asset.Status != types.AssetStatus_ASSET_STATUS_SUSPENDED &&
		asset.Status != types.AssetStatus_ASSET_STATUS_WRITTEN_OFF {
		return sdkerrors.Wrapf(
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

	// Recovery closes any settlement in the same act, and does so without
	// consulting the announced window. The plan's fixed rate cannot coexist
	// with the live Oracle rate now governing conversion, and holders are not
	// losing an exit: they are getting the ordinary one back, which is the
	// outcome the settlement was a substitute for.
	//
	// Deliberately absent: any comparison between the plan's rate and the live
	// one. The two are not the same kind of quantity. A settlement rate is a
	// number governance froze at some past block — above the live rate it
	// overpays and dilutes NOAH holders to do it, below the live rate it
	// shortchanges the settling holder — so it is the degraded instrument
	// whichever way it currently points. Gating recovery on the frozen number
	// winning would keep holders in the substitute to protect a figure that was
	// never the asset's worth, and would deny them the optionality recovery
	// restores: hold, transfer, or convert either direction, instead of one
	// one-way exit.
	return k.closeSettlementPlan(ctx, denom, asset.Version+1)
}

// FinaliseRetirement retires a cleared asset in the block it executes. Feed
// membership is not asset state, so retirement schedules nothing and consults
// neither the feed registry nor the protocol reference. From ISSUANCE_HALTED
// governance may derecognize a bounded residual it judges unredeemable;
// positive residual from SUSPENDED must go through WriteOffAsset instead,
// because holders there may have had no exit.
//
// It has no bearing on a registration governance regrets. Registration admits
// the asset outright, so an unwanted one is halted and retired like any other
// — and the Bank metadata a registration writes is permanent whichever path
// ends it, so nothing here could have returned the denomination anyway.
//
// RETIRED is terminal: no transition leads out of it. An asset that might
// return does not need one, because the lifecycle already carries two
// reversible pairs — HaltIssuance/ResumeIssuance for a pause, and
// SuspendAsset/RecoverAsset for distress, which reaches back even from
// WRITTEN_OFF. Staying halted costs nothing and gives up nothing: pricing,
// redemption, and liability accounting all continue, and only new issuance
// stops. Retirement is what governance reaches for when none of that is wanted
// any longer, so an undo would only blur the one act that means finished.
//
// A comeback gated on zero supply, which is what this replaces, could never
// have served more than the clean wind-down in any case. Nothing burns a
// retired asset's residual — Market refuses to price it and no module burns
// holder balances — so a retirement that derecognized anything was already
// permanent, and the tombstones an undo could still reopen were exactly the
// ones holding no exposure worth reopening.
//
// The denomination is spent for good, so a successor asset takes a new one.
// That costs a retirement that cleared to zero nothing but the string, and for
// a retirement that derecognized a residual it is the honest outcome: the old
// denomination names an asset whose holders were written down, and supply of it
// is still out there, indistinguishable from anything reissued under the name.
func (k Keeper) FinaliseRetirement(ctx context.Context, denom string, expectedVersion uint64, maxResidualSupply math.Int) error {
	asset, err := k.getAssetAtVersion(ctx, denom, expectedVersion)
	if err != nil {
		return err
	}
	if asset.Status != types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED &&
		asset.Status != types.AssetStatus_ASSET_STATUS_SUSPENDED &&
		asset.Status != types.AssetStatus_ASSET_STATUS_WRITTEN_OFF {
		return sdkerrors.Wrapf(
			types.ErrInvalidAssetTransition,
			"%s asset %s cannot finalise retirement",
			asset.Status,
			asset.Denom,
		)
	}
	if maxResidualSupply.IsNil() || maxResidualSupply.IsNegative() {
		return sdkerrors.Wrapf(
			types.ErrInvalidAssetTransition,
			"asset %s maximum residual supply must be set and non-negative",
			denom,
		)
	}
	// Retirement never consults the reference state: the reference names a
	// feed, and the feed layer's referent guard is what pins it. A tombstone
	// is a status, not a claim on price data.
	//
	// A settlement plan does not block retirement, and needs no window check to
	// be safe. Plans exist only on SUSPENDED assets, and SUSPENDED retirement
	// already demands zero supply, so an attached plan is necessarily one
	// nobody can still redeem against. Retiring a spent settlement is the
	// honest end of the wind-down, not a way around it.
	//
	// Only ISSUANCE_HALTED may approve a positive residual, and the two statuses
	// that may not are refused for different reasons, so each states its own
	// rather than sharing one negated condition. Each refusal also sits after
	// that status's supply check, so when supply is what actually blocks the
	// retirement, governance sees the message naming the remedy instead of one
	// about the bound it passed.
	supply := k.bankKeeper.GetSupply(ctx, denom)
	switch asset.Status {
	case types.AssetStatus_ASSET_STATUS_ISSUANCE_HALTED:
		// Redemption has been continuously available in this status, so
		// governance may judge the remainder unredeemable and derecognize it
		// explicitly within the bound it approved.
		if supply.Amount.GT(maxResidualSupply) {
			return sdkerrors.Wrapf(
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
			return sdkerrors.Wrapf(
				types.ErrAssetSupplyNotZero,
				"suspended asset %s has supply %s; write it off instead",
				denom,
				supply.Amount,
			)
		}
		if !maxResidualSupply.IsZero() {
			return sdkerrors.Wrapf(
				types.ErrInvalidAssetTransition,
				"suspended asset %s cannot approve a residual; write it off instead",
				denom,
			)
		}
	case types.AssetStatus_ASSET_STATUS_WRITTEN_OFF:
		// The remaining supply is deliberately unchecked: reaching WRITTEN_OFF
		// required it to be positive and nothing since has burnt it, so this
		// asset retires with a residual by construction. The WRITE_OFF record
		// already disclosed that amount, which is why the bound must be zero —
		// this message approves nothing and must not read as though it did.
		if !maxResidualSupply.IsZero() {
			return sdkerrors.Wrapf(
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

	// Retirement is immediate on every path. Feed membership is no longer
	// asset state, so nothing has to be scheduled and no status lingers waiting
	// for an epoch: the residual bound is checked and recorded right here.
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
		return sdkerrors.Wrapf(
			types.ErrInvalidAssetTransition,
			"%s asset %s cannot suspend",
			asset.Status,
			asset.Denom,
		)
	}
	// Suspension is a pure status move, including for an asset sharing the
	// reference denomination: the reference is a feed read, so the unit keeps
	// its price while the claim on it fails. Peg failure and price-data failure
	// are different axes with different responses.
	//
	// The feed keeps running: suspension's containment is the status gate each
	// consumer already applies — Market refusing the asset either side of a
	// conversion, Treasury dropping it from oracle-priced membership — not the
	// absence of a rate.
	updated := asset
	updated.Status = types.AssetStatus_ASSET_STATUS_SUSPENDED

	return k.advanceAsset(ctx, asset, updated)
}

// requireFeedActive demands a feed already in the active set. It is the single
// admission rule: every path that makes the Oracle an asset's price authority —
// registration, recovery, and genesis import — passes through it, so an
// imported chain expresses no asset state a running one could not have reached.
//
// Active is not a promise that a rate exists. The preblocker applies each
// height's votes before AdvanceFeeds promotes that height's batch, so the votes
// landing in an activation block were signed against the epoch before it and
// cannot carry the new feed: a feed is Active with no rate for one block by
// construction, and longer if sidecar coverage is thin.
//
// What the rule buys is the governance cycle. A feed cannot be added and used
// in the same proposal, so by the time anyone votes on an admission the feed
// has been live long enough to watch. That is also why freshness goes unchecked
// here: staleness is a use-time concern Market and Treasury already enforce by
// rejecting or omitting a rate past its maximum age, and the judgement that a
// price source works belongs to the voters this rule gives something to judge.
func (k Keeper) requireFeedActive(ctx context.Context, denom string) error {
	phase, err := k.oracleKeeper.FeedPhase(ctx, denom)
	if err != nil {
		return fmt.Errorf("getting feed phase for asset %s: %w", denom, err)
	}
	if phase != oracletypes.FeedPhaseActive {
		return sdkerrors.Wrapf(
			types.ErrAssetNotPriceable,
			"asset %s feed must be in phase Active",
			denom,
		)
	}

	return nil
}
