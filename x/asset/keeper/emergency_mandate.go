package keeper

import (
	"context"
	"fmt"

	sdkerrors "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/pkg/chain"
	"ark/pkg/mandate"
	"ark/x/asset/types"
)

// SetEmergencyMandate replaces or disables the committee appointment. Every
// replacement advances the term and clears recorded per-term actions; because
// replacement always advances the term, clearing on replacement is exactly
// term-scoping. Mandate mutations never touch asset versions.
//
// Clearing is also the only way to re-arm a committee that has spent an asset's
// action, and it deliberately runs at governance speed. A committee that halted
// an asset it should have suspended cannot correct itself, and re-appointment is
// no faster than governance suspending the asset directly. That is the price of
// the shared budget, paid knowingly: it is what a chain must give up to be sure
// a halt is a judgement rather than a probe.
func (k Keeper) SetEmergencyMandate(ctx context.Context, committee string, activationHeight uint64, expiryHeight uint64) error {
	current, err := k.EmergencyMandate.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting emergency mandate: %w", err)
	}
	envelope, err := mandate.Next(current.Envelope, committee, activationHeight, expiryHeight)
	if err != nil {
		return err
	}

	// The mandate carries no fields of its own, so the derived envelope is the
	// whole appointment.
	updated := types.EmergencyMandate{Envelope: envelope}
	if err := updated.Validate(); err != nil {
		return err
	}

	if err := k.EmergencyMandate.Set(ctx, updated); err != nil {
		return fmt.Errorf("setting emergency mandate: %w", err)
	}
	if err := k.EmergencyActions.Clear(ctx, nil); err != nil {
		return fmt.Errorf("clearing emergency action usage: %w", err)
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(
		&types.EventEmergencyMandateSet{
			Term:             updated.Term,
			Committee:        updated.Committee,
			ActivationHeight: updated.ActivationHeight,
			ExpiryHeight:     updated.ExpiryHeight,
		},
	); err != nil {
		return fmt.Errorf("emitting emergency mandate: %w", err)
	}

	return nil
}

// EmergencySuspendAsset applies SuspendAsset semantics under a live mandate. It
// is the answer to an impaired asset: ordinary conversion at the reference rate
// is the unbounded transmission channel from that failure into NOAH, and the
// exit leg is the half that carries it. It is a pure status move: the reference
// is a feed read, so an asset sharing the reference denomination suspends while
// the unit keeps its price.
func (k Keeper) EmergencySuspendAsset(ctx context.Context, committee string, denom string, expectedTerm uint64) error {
	asset, err := k.authoriseEmergencyAction(ctx, committee, denom, expectedTerm)
	if err != nil {
		return err
	}
	if err := k.suspendAsset(ctx, asset); err != nil {
		return err
	}
	// Committee transitions are the only lifecycle moves that land inside a block
	// still being read. Governance transitions execute in x/gov's EndBlocker,
	// after every transaction, so consumers folding the registry once per block
	// are safe by ordering; the committee acts in an ordinary transaction, so a
	// consumer that already folded this block now holds a fold describing a
	// registry that no longer exists. Treasury's liability snapshot is one: it
	// tracks mint and burn but not status, so without this a later expansion in
	// this block would size fund targets on a valuation that still counts the
	// asset just suspended, and still calls itself complete.
	//
	// Of the two committee powers only this one needs it, because only this one
	// moves supply across that partition. See EmergencyHaltIssuance.
	if err := k.invalidateRegistryCaches(ctx); err != nil {
		return err
	}

	// The action is spent after the transition, not before it, so the term's
	// recorded usage only ever names moves that actually happened.
	if err := k.EmergencyActions.Set(ctx, denom); err != nil {
		return fmt.Errorf("recording emergency action for asset %s: %w", denom, err)
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(
		&types.EventEmergencySuspended{
			Denom:   denom,
			Term:    expectedTerm,
			Version: asset.Version + 1,
		},
	); err != nil {
		return fmt.Errorf("emitting emergency suspension for asset %s: %w", denom, err)
	}

	return nil
}

// EmergencyHaltIssuance applies HaltIssuance semantics under a live mandate. It
// answers the other direction of the same failure. A conversion is a two-sided
// trade at one oracle rate, so value leaks on whichever side that rate is wrong:
// an asset over-valued against NOAH leaks through the exit, which is
// suspension's case, and NOAH over-valued against the asset leaks through the
// entry, which is this one.
//
// The entry direction has no other fast remedy. Staleness fails closed on absent
// data, not on wrong data, so a fresh but manipulated rate passes every check
// the pipeline makes; the Oracle has no committee at all, and Market's own
// throttles bound the arbitrage per block against a pool that recovers every
// block. Left to governance, the leak runs for the whole cycle.
//
// It never runs before a suspension. Both powers spend the same per-term action,
// so reaching for the halt is committing to a diagnosis — a sound asset behind a
// mispriced entry leg — and being wrong about it costs the term's fast
// suspension for that asset. That is the intended shape rather than a
// side-effect: a committee that could halt freely would halt on suspicion, and
// an ISSUANCE_HALTED that had come to mean "the committee smelled smoke" would
// be a run signal through a door the halt deliberately holds open.
func (k Keeper) EmergencyHaltIssuance(ctx context.Context, committee string, denom string, expectedTerm uint64) error {
	asset, err := k.authoriseEmergencyAction(ctx, committee, denom, expectedTerm)
	if err != nil {
		return err
	}
	if err := k.haltIssuance(ctx, asset); err != nil {
		return err
	}
	// Deliberately no registry-cache invalidation, and the reason is the halt's
	// whole character. Treasury's block-scoped fold partitions supply by
	// lifecycle status, and ISSUANCE_HALTED sits inside oracle-priced membership
	// beside ACTIVE — so this transition moves no supply across that partition. A
	// consumer that already folded this block still holds a fold that is true:
	// nothing left the recognised aggregate, nothing became incomplete, and no
	// waterfall can read a figure that has moved. A halt that ever did change the
	// partition would need the invalidation its sibling does.
	if err := k.EmergencyActions.Set(ctx, denom); err != nil {
		return fmt.Errorf("recording emergency action for asset %s: %w", denom, err)
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(
		&types.EventEmergencyHalted{
			Denom:   denom,
			Term:    expectedTerm,
			Version: asset.Version + 1,
		},
	); err != nil {
		return fmt.Errorf("emitting emergency halt for asset %s: %w", denom, err)
	}

	return nil
}

// authoriseEmergencyAction checks the committee signer, the mandate window, the
// exact term, and the one-action-per-asset-per-term bound, then returns the
// asset the caller is about to move. Status preconditions stay with the
// lifecycle operation itself, so this reads the asset and moves nothing.
//
// One budget covers both powers, and that is the whole of the no-ladder rule: a
// committee cannot halt an asset to buy itself a look and suspend it once it has
// one. The two are a choice made on a single diagnosis — the entry leg is
// mispriced and the asset is sound, or the asset itself is impaired — and the
// chain cannot tell a probe from a genuine escalation, so it refuses both and
// leaves the second move to governance.
//
// The bound's original reason is unchanged by the sharing. Re-acting on an asset
// governance has already recovered within the same term is griefing, and
// governance cannot win a race against a committee that never runs out of turns.
func (k Keeper) authoriseEmergencyAction(ctx context.Context, committee string, denom string, expectedTerm uint64) (types.Asset, error) {
	if _, err := chain.ParseCanonicalAccountAddress("committee", committee); err != nil {
		return types.Asset{}, err
	}
	emergencyMandate, err := k.EmergencyMandate.Get(ctx)
	if err != nil {
		return types.Asset{}, fmt.Errorf("getting emergency mandate: %w", err)
	}
	if err := emergencyMandate.Authorise(
		committee,
		expectedTerm,
		uint64(sdk.UnwrapSDKContext(ctx).BlockHeight()),
	); err != nil {
		return types.Asset{}, sdkerrors.Wrap(types.ErrEmergencyMandateInactive, err.Error())
	}

	used, err := k.EmergencyActions.Has(ctx, denom)
	if err != nil {
		return types.Asset{}, fmt.Errorf("checking emergency action for asset %s: %w", denom, err)
	}
	if used {
		return types.Asset{}, sdkerrors.Wrapf(
			types.ErrEmergencyActionConsumed,
			"asset %s already acted on in term %d",
			denom,
			emergencyMandate.Term,
		)
	}

	return k.GetAsset(ctx, denom)
}
