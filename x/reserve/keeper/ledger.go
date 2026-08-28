package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/reserve/types"
)

// ledgerAppend carries one already-authorized accounting act into the shared
// ledger core. Authority is decided by the message type that reached here.
type ledgerAppend struct {
	PositionID     uint64
	Kind           types.EntryKind
	Corrects       uint64
	Quantity       sdk.Coin
	MovedNoahValue sdk.Coin
	MovedCoin      sdk.Coin
	Reference      string
	RecordedBy     string
	// Term is the appointment that acted, or zero for a governance act.
	Term uint64
}

// appendEntry writes one ledger entry and returns its identifier. Every
// mutation of the ledger goes through here, so its entries are complete by
// construction rather than by each handler remembering to record.
func (k *Keeper) appendEntry(ctx context.Context, record ledgerAppend) (uint64, error) {
	entryID, err := k.NextEntryID.Peek(ctx)
	if err != nil {
		return 0, fmt.Errorf("getting next entry ID: %w", err)
	}

	// The judgment kinds set neither movement field, because nothing moved.
	// Validate rejects an empty denomination, so an unset coin becomes an
	// explicit zero NOAH one, which also satisfies the par identity.
	if record.MovedNoahValue.Denom == "" || record.MovedNoahValue.Amount.IsNil() {
		record.MovedNoahValue = chain.NoahCoin(math.ZeroInt())
	}
	if record.MovedCoin.Denom == "" || record.MovedCoin.Amount.IsNil() {
		record.MovedCoin = chain.NoahCoin(math.ZeroInt())
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	entry := types.AccountingEntry{
		EntryId:        entryID,
		PositionId:     record.PositionID,
		Kind:           record.Kind,
		Corrects:       record.Corrects,
		Quantity:       record.Quantity,
		MovedNoahValue: record.MovedNoahValue,
		MovedCoin:      record.MovedCoin,
		Reference:      record.Reference,
		RecordedBy:     record.RecordedBy,
		Height:         uint64(sdkCtx.BlockHeight()),
		Term:           record.Term,
	}
	if err := entry.Validate(); err != nil {
		return 0, err
	}
	if err := k.NextEntryID.Set(ctx, entryID+1); err != nil {
		return 0, fmt.Errorf("setting next entry ID: %w", err)
	}
	if err := k.Ledger.Set(ctx, entry.EntryId, entry); err != nil {
		return 0, fmt.Errorf("recording ledger entry %d: %w", entry.EntryId, err)
	}
	if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventLedgerEntryRecorded{
		EntryId:    entry.EntryId,
		PositionId: entry.PositionId,
		Kind:       entry.Kind.String(),
	}); err != nil {
		return 0, fmt.Errorf("emitting Reserve ledger event: %w", err)
	}

	return entry.EntryId, nil
}
