package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/metrics"
	"github.com/ararat-network/ark/x/disbursement/types"
)

// EndBlocker pays every grant whose due time has arrived, oldest first, at most MaxPaymentsPerBlock
// in a block; the rest stay due for the next. A send Bank refuses at the payee is deferred one retry
// interval and published, never a halt: the payee is the one input a write cannot vet for every
// later restriction. Every other failure is an accounting invariant and fails the block.
func (k *Keeper) EndBlocker(ctx context.Context) error {
	defer metrics.RecordModuleMethodLatency(ctx, types.ModuleName, metrics.EndBlock)()

	now := chain.BlockSeconds(ctx)
	iterator, err := k.Due.Iterate(ctx, collections.NewPrefixUntilPairRange[uint64, uint64](now))
	if err != nil {
		return fmt.Errorf("iterating due grants: %w", err)
	}
	var due []collections.Pair[uint64, uint64]
	for ; iterator.Valid() && len(due) < types.MaxPaymentsPerBlock; iterator.Next() {
		key, err := iterator.Key()
		if err != nil {
			iterator.Close()
			return fmt.Errorf("reading due grant key: %w", err)
		}
		due = append(due, key)
	}
	iterator.Close()

	for _, key := range due {
		g, err := k.Grants.Get(ctx, key.K2())
		if err != nil {
			return fmt.Errorf("getting due grant %d: %w", key.K2(), err)
		}
		// Unreachable: setDueTime is the only writer outside import, and every caller stores the grant
		// it moved. Kept because nothing else clears a stray key, which would take a payment slot
		// every block.
		if g.DueTime != key.K1() {
			k.Logger(ctx).Error("dropping a due entry the grant does not carry", "grant_id", g.Id, "due_time", key.K1(), "grant_due_time", g.DueTime)
			if err := k.Due.Remove(ctx, key); err != nil {
				return err
			}
			continue
		}
		q, err := k.payable(ctx, g)
		if err != nil {
			return fmt.Errorf("paying grant %d: %w", g.Id, err)
		}
		if err := k.settle(ctx, k.accountKeeper.GetModuleAddress(types.Account(g.Kind)).String(), g, q); err != nil {
			if !errors.Is(err, types.ErrPaymentRefused) {
				return fmt.Errorf("paying grant %d: %w", g.Id, err)
			}
			// Bank refused at the payee and nothing was written: the grant waits one retry interval,
			// or its next boundary if sooner, and the deferral is published. The refusal stays in the log.
			if err := k.setDueTime(ctx, &g, types.RetryAt(now, q.next)); err != nil {
				return fmt.Errorf("deferring grant %d: %w", g.Id, err)
			}
			// Unreachable: only due_time moved, from at or before now to after it, on a grant that
			// owes and is not suspended, so every floor Validate checks still holds.
			if err := g.Validate(); err != nil {
				return fmt.Errorf("deferring grant %d: %w", g.Id, err)
			}
			if err := k.Grants.Set(ctx, g.Id, g); err != nil {
				return fmt.Errorf("deferring grant %d: %w", g.Id, err)
			}
			k.Logger(ctx).Error("payment deferred", "grant_id", g.Id, "payee", g.Payee, "amount", q.amount, "due_time", g.DueTime, "err", err)
			if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventPaymentDeferred{
				GrantId:     g.Id,
				Payee:       g.Payee,
				AmountDenom: q.amount.Denom,
				Amount:      q.amount.Amount,
				DueTime:     g.DueTime,
			}); err != nil {
				return fmt.Errorf("deferring grant %d: %w", g.Id, err)
			}
		}
	}
	return nil
}
