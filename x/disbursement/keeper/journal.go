package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/disbursement/types"
)

// stamp numbers a journal entry from seq and dates it to the block.
func stamp(ctx context.Context, seq collections.Sequence) (id, height, at uint64, err error) {
	h := sdk.UnwrapSDKContext(ctx).BlockHeight()
	if h <= 0 {
		return 0, 0, 0, errors.New("disbursement operations require a positive block height")
	}
	if id, err = nextID(ctx, seq); err != nil {
		return 0, 0, 0, err
	}
	return id, uint64(h), chain.BlockSeconds(ctx), nil
}

// recordGrant is the grant journal's only writer outside import; callers commit it with the state and
// bank movement. The caller fills the action, grant, actor, amount, subject, and offer; recordGrant
// stamps and validates it.
func (k *Keeper) recordGrant(ctx context.Context, e types.GrantEntry) error {
	if e.Offer.IsNil() {
		e.Offer = math.ZeroInt()
	}
	var err error
	if e.Id, e.Height, e.Time, err = stamp(ctx, k.NextGrantEntryID); err != nil {
		return err
	}
	if err := e.Validate(); err != nil {
		return fmt.Errorf("grant entry: %w", err)
	}
	if err := k.GrantJournal.Set(ctx, collections.Join(e.GrantId, e.Id), e); err != nil {
		return err
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventGrantEntryRecorded{
		Id:          e.Id,
		GrantId:     e.GrantId,
		Action:      e.Action.String(),
		Actor:       e.Actor,
		AmountDenom: e.Amount.Denom,
		Amount:      e.Amount.Amount,
		Subject:     e.Subject,
		Offer:       e.Offer,
	}); err != nil {
		return fmt.Errorf("disbursement grant journal event: %w", err)
	}
	return nil
}

// recordBeneficiary is the beneficiary journal's only writer outside import; SetController commits it
// with the record.
func (k *Keeper) recordBeneficiary(ctx context.Context, e types.BeneficiaryEntry) error {
	var err error
	if e.Id, e.Height, e.Time, err = stamp(ctx, k.NextBeneficiaryEntryID); err != nil {
		return err
	}
	if err := e.Validate(); err != nil {
		return fmt.Errorf("beneficiary entry: %w", err)
	}
	address, err := chain.ParseCanonicalAccountAddress("beneficiary", e.Beneficiary)
	if err != nil {
		return err
	}
	if err := k.BeneficiaryJournal.Set(ctx, collections.Join(address, e.Id), e); err != nil {
		return err
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventBeneficiaryEntryRecorded{
		Id:          e.Id,
		Beneficiary: e.Beneficiary,
		Actor:       e.Actor,
		Controller:  e.Controller,
	}); err != nil {
		return fmt.Errorf("disbursement beneficiary journal event: %w", err)
	}
	return nil
}
