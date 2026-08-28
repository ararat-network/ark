package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/reserve/types"
)

// openPosition records a new position funded by a proven outflow. It writes no
// entry for the deployment: the caller appends the DEPLOYMENT entry once the
// outflow has actually been paid, so a failed transfer leaves no record of a
// movement that never happened.
func (k *Keeper) openPosition(ctx context.Context, acquired sdk.Coin, deployed sdk.Coin, venueReference string) (types.Position, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	positionID, err := k.NextPositionID.Peek(ctx)
	if err != nil {
		return types.Position{}, fmt.Errorf("getting next position ID: %w", err)
	}

	position := types.Position{
		PositionId:     positionID,
		Quantity:       acquired,
		Deployed:       deployed,
		Returned:       chain.NoahCoin(math.ZeroInt()),
		VenueReference: venueReference,
		OpenedHeight:   uint64(sdkCtx.BlockHeight()),
	}
	if err := position.Validate(); err != nil {
		return types.Position{}, err
	}
	if err := k.NextPositionID.Set(ctx, positionID+1); err != nil {
		return types.Position{}, fmt.Errorf("setting next position ID: %w", err)
	}
	if err := k.OpenPositions.Set(ctx, position.PositionId, position); err != nil {
		return types.Position{}, fmt.Errorf("setting position %d: %w", position.PositionId, err)
	}
	if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventPositionOpened{
		PositionId: position.PositionId,
		Deployed:   position.Deployed,
	}); err != nil {
		return types.Position{}, fmt.Errorf("emitting Reserve position event: %w", err)
	}

	return position, nil
}

// requireDenomHoldable decides whether a position may name this denomination:
// an external symbol is admissible by shape, but a bare denomination is
// admissible only while it is actually registered. Membership is asked at the
// write alone, so a stored position naming an asset that later retires stays
// valid.
func (k *Keeper) requireDenomHoldable(ctx context.Context, denom string) error {
	if _, isExternal := chain.ExternalFeed(denom); isExternal {
		return nil
	}
	member, err := k.assetKeeper.HasAsset(ctx, denom)
	if err != nil {
		return fmt.Errorf("checking the asset registry for %s: %w", denom, err)
	}
	if !member {
		return fmt.Errorf(
			"%s is neither an external symbol naming external custody nor a registered "+
				"Ark-issued asset, and no position may be attested in it",
			denom,
		)
	}

	return nil
}

// executeDeployment pays the destination and books what the committee attests
// it bought, returning the position the deployment landed on and the ledger
// entry that recorded it. The caller has authorised the committee, checked the
// request's shape, and confirmed the mandate names the destination.
func (k *Keeper) executeDeployment(
	ctx context.Context,
	reserveMandate types.ReserveMandate,
	msg *types.MsgCommitteeDeploy,
	destination sdk.AccAddress,
) (positionID uint64, entryID uint64, err error) {
	// Both branches answer the same question — can the account part with this
	// coin — against the constraint that applies to it.
	if msg.Amount.Denom == chain.NoahBaseDenom {
		balance := k.balance(ctx)
		remaining, err := balance.SafeSub(msg.Amount.Amount)
		if err != nil || remaining.LT(reserveMandate.MinimumNoahBalance.Amount) {
			return 0, 0, fmt.Errorf(
				"reserve balance %s cannot fund %s while retaining the mandate floor %s",
				balance,
				msg.Amount,
				reserveMandate.MinimumNoahBalance,
			)
		}
	} else {
		held := k.bankKeeper.GetBalance(ctx, k.reserveAddress, msg.Amount.Denom)
		if held.Amount.LT(msg.Amount.Amount) {
			return 0, 0, fmt.Errorf(
				"deployment %s exceeds the Reserve's %s holding",
				msg.Amount,
				held,
			)
		}
	}

	booked, err := k.valueMovement(ctx, msg.Amount)
	if err != nil {
		return 0, 0, fmt.Errorf("valuing deployment: %w", err)
	}
	// A fresh feed may still price a small outflow to zero. Booking a
	// deployment at zero would write a permanently false cost basis, and an
	// outflow that has not happened yet can simply decline to happen.
	if !booked.IsPositive() {
		return 0, 0, fmt.Errorf("deployment of %s values to zero anoah", msg.Amount)
	}
	// Only NOAH consumes the allowance: an in-kind outflow moves capital
	// already charged when it first went out, so metering it would charge a
	// round trip twice and leave a spent committee unable to sell anything
	// back.
	if msg.Amount.Denom == chain.NoahBaseDenom {
		if err := k.addAllowanceUsed(ctx, reserveMandate, booked); err != nil {
			return 0, 0, err
		}
	}

	position, err := k.resolveDeploymentPosition(ctx, msg, booked)
	if err != nil {
		return 0, 0, err
	}

	if err := k.bankKeeper.SendCoinsFromModuleToAccount(
		ctx,
		types.StrategicReserveName,
		destination,
		sdk.NewCoins(msg.Amount),
	); err != nil {
		return 0, 0, fmt.Errorf("paying deployment destination: %w", err)
	}
	entryID, err = k.appendEntry(ctx, ledgerAppend{
		PositionID:     position.PositionId,
		Kind:           types.EntryKind_ENTRY_KIND_DEPLOYMENT,
		Quantity:       position.Quantity,
		MovedCoin:      msg.Amount,
		MovedNoahValue: chain.NoahCoin(booked),
		Reference:      msg.Reference,
		RecordedBy:     msg.Committee,
		Term:           reserveMandate.Term,
	})
	if err != nil {
		return 0, 0, err
	}
	return position.PositionId, entryID, nil
}

// resolveDeploymentPosition opens a new position or funds the named open one.
//
// The cost basis it records is the outflow's booked anoah value rather than the
// coin itself, so an in-kind deployment lands on the same NOAH-denominated
// basis a NOAH one does and Realised stays comparable across both.
func (k *Keeper) resolveDeploymentPosition(ctx context.Context, msg *types.MsgCommitteeDeploy, booked math.Int) (types.Position, error) {
	basis := chain.NoahCoin(booked)
	if msg.PositionId == 0 {
		if err := k.requireDenomHoldable(ctx, msg.Acquired.Denom); err != nil {
			return types.Position{}, err
		}
		return k.openPosition(ctx, msg.Acquired, basis, msg.VenueReference)
	}

	// Funding inherits the position's venue; a supplied one would be silently
	// dropped. Restating the venue is a correction's work.
	if msg.VenueReference != "" {
		return types.Position{}, fmt.Errorf(
			"funding position %d cannot restate its venue reference; use a correction",
			msg.PositionId,
		)
	}
	position, err := k.OpenPositions.Get(ctx, msg.PositionId)
	if err != nil {
		return types.Position{}, fmt.Errorf("getting open position %d: %w", msg.PositionId, err)
	}
	if msg.Acquired.Denom != position.Quantity.Denom {
		return types.Position{}, fmt.Errorf(
			"position %d holds %s and cannot be funded with %s",
			position.PositionId,
			position.Quantity.Denom,
			msg.Acquired.Denom,
		)
	}
	// Both sums are checked rather than taken through Coin.Add, which panics.
	// The attestation arrives from the message bounded by nothing and is judged
	// against MaxAttestedQuantity only by the Validate below, so the addition
	// has to survive reaching it. The basis accumulates over the position's
	// whole life exactly as Returned does: each outflow is bank-bounded, the sum
	// of them is not.
	quantity, err := position.Quantity.Amount.SafeAdd(msg.Acquired.Amount)
	if err != nil {
		return types.Position{}, fmt.Errorf(
			"funding the attested quantity of position %d: %w",
			position.PositionId,
			err,
		)
	}
	deployed, err := position.Deployed.Amount.SafeAdd(basis.Amount)
	if err != nil {
		return types.Position{}, fmt.Errorf(
			"funding the cost basis of position %d: %w",
			position.PositionId,
			err,
		)
	}
	position.Quantity = sdk.NewCoin(position.Quantity.Denom, quantity)
	position.Deployed = chain.NoahCoin(deployed)
	if err := position.Validate(); err != nil {
		return types.Position{}, err
	}
	if err := k.OpenPositions.Set(ctx, position.PositionId, position); err != nil {
		return types.Position{}, fmt.Errorf("funding position %d: %w", position.PositionId, err)
	}
	return position, nil
}

// attributeReturn books an inflow already sitting in the Reserve account
// against the position it settles, returning the ledger entry that recorded
// it. The caller has authorised the committee and checked the request's shape;
// the anoah valuation is computed here rather than taken from the message.
func (k *Keeper) attributeReturn(
	ctx context.Context,
	reserveMandate types.ReserveMandate,
	msg *types.MsgCommitteeAttributeReturn,
) (uint64, error) {
	position, err := k.OpenPositions.Get(ctx, msg.PositionId)
	if err != nil {
		return 0, fmt.Errorf("getting open position %d: %w", msg.PositionId, err)
	}
	if msg.RemainingQuantity.Denom != position.Quantity.Denom {
		return 0, fmt.Errorf(
			"position %d holds %s and cannot be restated in %s",
			position.PositionId,
			position.Quantity.Denom,
			msg.RemainingQuantity.Denom,
		)
	}
	// The inflow must already sit in the account: attribution matches a
	// custody fact, it does not assert one. It cannot tell two attributions of
	// the same inflow apart, but it refuses attributing coins the account does
	// not hold.
	held := k.bankKeeper.GetBalance(ctx, k.reserveAddress, msg.ReturnedCoin.Denom)
	if held.Amount.LT(msg.ReturnedCoin.Amount) {
		return 0, fmt.Errorf(
			"attributed return %s exceeds the Reserve's %s holding",
			msg.ReturnedCoin,
			held,
		)
	}

	recovered, err := k.valueMovement(ctx, msg.ReturnedCoin)
	if err != nil {
		return 0, fmt.Errorf("valuing attributed return: %w", err)
	}

	position.Quantity = msg.RemainingQuantity
	// Returned accumulates over the position's whole life: each inflow is
	// bank-bounded, but the sum of them is not, so the addition is checked.
	returned, err := position.Returned.Amount.SafeAdd(recovered)
	if err != nil {
		return 0, fmt.Errorf("accumulating the return attributed to position %d: %w", position.PositionId, err)
	}
	position.Returned = chain.NoahCoin(returned)
	if err := position.Validate(); err != nil {
		return 0, err
	}
	if err := k.OpenPositions.Set(ctx, position.PositionId, position); err != nil {
		return 0, fmt.Errorf("updating position %d: %w", position.PositionId, err)
	}
	return k.appendEntry(ctx, ledgerAppend{
		PositionID:     position.PositionId,
		Kind:           types.EntryKind_ENTRY_KIND_RETURN_ATTRIBUTION,
		Quantity:       position.Quantity,
		MovedCoin:      msg.ReturnedCoin,
		MovedNoahValue: chain.NoahCoin(recovered),
		Reference:      msg.Reference,
		RecordedBy:     msg.Committee,
		Term:           reserveMandate.Term,
	})
}

// restateQuantity replaces a position's attested holding and records the
// restatement as a ledger entry, returning that entry. The denomination is
// held fixed: restating the instrument is a correction's work.
func (k *Keeper) restateQuantity(
	ctx context.Context,
	reserveMandate types.ReserveMandate,
	msg *types.MsgCommitteeRecordUpdate,
) (uint64, error) {
	position, err := k.OpenPositions.Get(ctx, msg.PositionId)
	if err != nil {
		return 0, fmt.Errorf("getting open position %d: %w", msg.PositionId, err)
	}
	if msg.Quantity.Denom != position.Quantity.Denom {
		return 0, fmt.Errorf(
			"position %d holds %s and cannot be restated in %s",
			position.PositionId,
			position.Quantity.Denom,
			msg.Quantity.Denom,
		)
	}
	position.Quantity = msg.Quantity
	if err := position.Validate(); err != nil {
		return 0, err
	}
	if err := k.OpenPositions.Set(ctx, position.PositionId, position); err != nil {
		return 0, fmt.Errorf("updating position %d: %w", position.PositionId, err)
	}
	return k.appendEntry(ctx, ledgerAppend{
		PositionID: position.PositionId,
		Kind:       types.EntryKind_ENTRY_KIND_QUANTITY_UPDATE,
		Quantity:   position.Quantity,
		Reference:  msg.Reference,
		RecordedBy: msg.Committee,
		Term:       reserveMandate.Term,
	})
}

// setImpaired flips a position's impairment flag and records the change as a
// ledger entry, returning that entry.
func (k *Keeper) setImpaired(
	ctx context.Context,
	positionID uint64,
	impaired bool,
	reference string,
	recordedBy string,
	term uint64,
) (uint64, error) {
	position, err := k.OpenPositions.Get(ctx, positionID)
	if err != nil {
		return 0, fmt.Errorf("getting open position %d: %w", positionID, err)
	}
	if position.Impaired == impaired {
		return 0, fmt.Errorf("position %d is already in the requested impairment state", positionID)
	}
	position.Impaired = impaired
	if err := k.OpenPositions.Set(ctx, position.PositionId, position); err != nil {
		return 0, fmt.Errorf("updating position %d: %w", position.PositionId, err)
	}
	return k.appendEntry(ctx, ledgerAppend{
		PositionID: position.PositionId,
		Kind:       types.EntryKind_ENTRY_KIND_IMPAIRMENT,
		Quantity:   position.Quantity,
		Reference:  reference,
		RecordedBy: recordedBy,
		Term:       term,
	})
}

// positionCorrection carries one already-authorized restatement into the
// shared correction core. Authority is decided by the message type that
// reached here.
type positionCorrection struct {
	PositionID     uint64
	Corrects       uint64
	Quantity       sdk.Coin
	VenueReference string
	Reference      string
	RecordedBy     string
	// Term is the appointment that acted, or zero for a governance act.
	Term uint64
}

// correctPosition restates a position and records what it corrected as a
// ledger entry. The position is loaded from either store — a closed position
// is history that should still read true — and the restated record goes back
// where it came from, so a correction cannot reopen or retire a position. The
// corrected entry must exist and belong to this position, keeping the mistake
// readable beside its correction.
func (k *Keeper) correctPosition(ctx context.Context, correction positionCorrection) (uint64, error) {
	position, err := k.OpenPositions.Get(ctx, correction.PositionID)
	closed := errors.Is(err, collections.ErrNotFound)
	if closed {
		position, err = k.ClosedPositions.Get(ctx, correction.PositionID)
	}
	if err != nil {
		return 0, fmt.Errorf("getting position %d: %w", correction.PositionID, err)
	}
	corrected, err := k.Ledger.Get(ctx, correction.Corrects)
	if err != nil {
		return 0, fmt.Errorf("getting corrected entry %d: %w", correction.Corrects, err)
	}
	if corrected.PositionId != correction.PositionID {
		return 0, fmt.Errorf(
			"entry %d belongs to position %d, not %d",
			corrected.EntryId,
			corrected.PositionId,
			correction.PositionID,
		)
	}

	// A correction may restate the instrument, so it answers the same
	// admission rule an opening does.
	if correction.Quantity.Denom != position.Quantity.Denom {
		if err := k.requireDenomHoldable(ctx, correction.Quantity.Denom); err != nil {
			return 0, err
		}
	}
	position.Quantity = correction.Quantity
	if correction.VenueReference != "" {
		position.VenueReference = correction.VenueReference
	}
	if err := position.Validate(); err != nil {
		return 0, err
	}
	positions := k.OpenPositions
	if closed {
		positions = k.ClosedPositions
	}
	if err := positions.Set(ctx, position.PositionId, position); err != nil {
		return 0, fmt.Errorf("correcting position %d: %w", position.PositionId, err)
	}
	return k.appendEntry(ctx, ledgerAppend{
		PositionID: position.PositionId,
		Kind:       types.EntryKind_ENTRY_KIND_CORRECTION,
		Corrects:   correction.Corrects,
		Quantity:   position.Quantity,
		Reference:  correction.Reference,
		RecordedBy: correction.RecordedBy,
		Term:       correction.Term,
	})
}

// returnReversal carries one already-authorized retraction of a recorded
// return into the shared reversal core. Authority is decided by the message
// type that reached here.
type returnReversal struct {
	PositionID uint64
	// Reverses names the RETURN_ATTRIBUTION entry being undone.
	Reverses   uint64
	Reference  string
	RecordedBy string
	// Term is the appointment that acted, or zero for a governance act.
	Term uint64
}

// reverseReturn undoes one return attribution and records the reversal as a
// ledger entry, returning that entry. Attribution is the one recorded movement
// whose linkage the chain cannot verify — Bank witnesses that coins arrived,
// never which position they settle — so it is the only one needing a remedy.
//
// The reversed value is copied rather than re-derived: a re-priced reversal
// would let a committee reverse and re-attribute one inflow across a rate move
// and book the difference. The position is loaded from either store, because a
// closed position's recovery should read as true afterwards as an open one's.
func (k *Keeper) reverseReturn(ctx context.Context, reversal returnReversal) (uint64, error) {
	position, err := k.OpenPositions.Get(ctx, reversal.PositionID)
	closed := errors.Is(err, collections.ErrNotFound)
	if closed {
		position, err = k.ClosedPositions.Get(ctx, reversal.PositionID)
	}
	if err != nil {
		return 0, fmt.Errorf("getting position %d: %w", reversal.PositionID, err)
	}
	reversed, err := k.Ledger.Get(ctx, reversal.Reverses)
	if err != nil {
		return 0, fmt.Errorf("getting reversed entry %d: %w", reversal.Reverses, err)
	}
	if reversed.PositionId != reversal.PositionID {
		return 0, fmt.Errorf(
			"entry %d belongs to position %d, not %d",
			reversed.EntryId,
			reversed.PositionId,
			reversal.PositionID,
		)
	}
	if reversed.Kind != types.EntryKind_ENTRY_KIND_RETURN_ATTRIBUTION {
		return 0, fmt.Errorf(
			"entry %d records %s, and only a return attribution can be reversed",
			reversed.EntryId,
			reversed.Kind,
		)
	}
	alreadyReversed, err := k.ReversedReturns.Has(ctx, reversal.Reverses)
	if err != nil {
		return 0, fmt.Errorf("checking whether entry %d is reversed: %w", reversal.Reverses, err)
	}
	if alreadyReversed {
		return 0, fmt.Errorf("entry %d has already been reversed", reversal.Reverses)
	}

	returned, err := position.Returned.Amount.SafeSub(reversed.MovedNoahValue.Amount)
	if err == nil && returned.IsNegative() {
		err = errors.New("reversal exceeds the recovery attributed to the position")
	}
	if err != nil {
		// Unreachable: every attribution adds its booked value to Returned
		// exactly once and reverses at most once, so the recovery cannot pass
		// zero. Kept because Returned funds the realised figure a closure
		// crystallises.
		return 0, fmt.Errorf(
			"reversing the return attributed to position %d: %w",
			position.PositionId,
			err,
		)
	}
	position.Returned = chain.NoahCoin(returned)
	if err := position.Validate(); err != nil {
		return 0, err
	}
	positions := k.OpenPositions
	if closed {
		positions = k.ClosedPositions
	}
	if err := positions.Set(ctx, position.PositionId, position); err != nil {
		return 0, fmt.Errorf("reversing a return of position %d: %w", position.PositionId, err)
	}
	if err := k.ReversedReturns.Set(ctx, reversal.Reverses); err != nil {
		return 0, fmt.Errorf("marking entry %d reversed: %w", reversal.Reverses, err)
	}
	return k.appendEntry(ctx, ledgerAppend{
		PositionID:     position.PositionId,
		Kind:           types.EntryKind_ENTRY_KIND_RETURN_REVERSAL,
		Corrects:       reversal.Reverses,
		Quantity:       position.Quantity,
		MovedCoin:      reversed.MovedCoin,
		MovedNoahValue: reversed.MovedNoahValue,
		Reference:      reversal.Reference,
		RecordedBy:     reversal.RecordedBy,
		Term:           reversal.Term,
	})
}

// closePosition crystallises a position's realised profit or loss and records
// the closure as a ledger entry, returning that entry. The position must still
// be open, so a second closure is refused rather than re-stamping the height
// and emitting a duplicate event.
func (k *Keeper) closePosition(
	ctx context.Context,
	positionID uint64,
	reference string,
	recordedBy string,
	term uint64,
) (uint64, error) {
	position, err := k.OpenPositions.Get(ctx, positionID)
	if err != nil {
		return 0, fmt.Errorf("getting open position %d: %w", positionID, err)
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	position.ClosedHeight = uint64(sdkCtx.BlockHeight())
	if err := position.Validate(); err != nil {
		return 0, err
	}
	// Closing is a move between stores rather than a flag write, which takes
	// the record out of every open-set walk permanently.
	if err := k.OpenPositions.Remove(ctx, position.PositionId); err != nil {
		return 0, fmt.Errorf("closing position %d: %w", position.PositionId, err)
	}
	if err := k.ClosedPositions.Set(ctx, position.PositionId, position); err != nil {
		return 0, fmt.Errorf("closing position %d: %w", position.PositionId, err)
	}
	entryID, err := k.appendEntry(ctx, ledgerAppend{
		PositionID: position.PositionId,
		Kind:       types.EntryKind_ENTRY_KIND_CLOSURE,
		Quantity:   position.Quantity,
		Reference:  reference,
		RecordedBy: recordedBy,
		Term:       term,
	})
	if err != nil {
		return 0, err
	}
	if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventPositionClosed{
		PositionId: position.PositionId,
		Deployed:   position.Deployed,
		Returned:   position.Returned,
		Realised:   position.Realised(),
	}); err != nil {
		return 0, fmt.Errorf("emitting Reserve closure event: %w", err)
	}
	return entryID, nil
}
