package keeper

import (
	"context"
	"fmt"

	"ark/pkg/chain"
	"ark/x/reserve/types"
)

// InitGenesis validates and imports Reserve state. Non-NOAH balances are
// permitted but must be registry members: Bank genesis writes balances
// directly, so this is the only door the send restriction's rule does not
// guard. Membership is permanent, so the rule cannot refuse a chain its own
// export.
func (k Keeper) InitGenesis(ctx context.Context, data *types.GenesisState) error {
	if data == nil {
		return fmt.Errorf("reserve genesis state is nil")
	}
	if err := data.Validate(); err != nil {
		return fmt.Errorf("invalid Reserve genesis state: %w", err)
	}
	if data.Mandate.Committee == k.authority {
		return fmt.Errorf("reserve committee must be distinct from the Reserve authority")
	}

	moduleAccount := k.accountKeeper.GetModuleAccount(ctx, types.StrategicReserveName)
	if moduleAccount == nil {
		return fmt.Errorf("%s module account has not been set", types.StrategicReserveName)
	}
	for _, balance := range k.bankKeeper.GetAllBalances(ctx, moduleAccount.GetAddress()) {
		if balance.Denom == chain.NoahBaseDenom {
			continue
		}
		// A registered asset's denomination passed ValidatePricedDenom at
		// registration, so nothing admitted here can be malformed.
		member, err := k.assetKeeper.HasAsset(ctx, balance.Denom)
		if err != nil {
			return fmt.Errorf("checking the asset registry for %s: %w", balance.Denom, err)
		}
		if !member {
			return fmt.Errorf(
				"%s account contains unsupported genesis denom %s: deposits must be %s "+
					"or an Ark-issued asset",
				types.StrategicReserveName,
				balance.Denom,
				chain.NoahBaseDenom,
			)
		}
	}
	// Oracle initialises before Reserve, so the feed registry is readable here
	// and an import cannot seed a claim whose series no validator prices — the
	// same refusal a live proposal meets.
	if err := k.validateExternalFeeds(ctx, data.RecognitionPolicy); err != nil {
		return err
	}

	if err := k.Params.Set(ctx, data.Params); err != nil {
		return fmt.Errorf("setting Reserve params: %w", err)
	}
	if err := k.Mandate.Set(ctx, data.Mandate); err != nil {
		return fmt.Errorf("setting Reserve mandate: %w", err)
	}
	if err := k.AllowanceUsed.Set(ctx, data.AllowanceUsed.Amount); err != nil {
		return fmt.Errorf("setting Reserve allowance usage: %w", err)
	}
	if err := k.NextPositionID.Set(ctx, data.NextPositionId); err != nil {
		return fmt.Errorf("setting next position ID: %w", err)
	}
	if err := k.NextEntryID.Set(ctx, data.NextEntryId); err != nil {
		return fmt.Errorf("setting next entry ID: %w", err)
	}
	for _, position := range data.OpenPositions {
		if err := k.OpenPositions.Set(ctx, position.PositionId, position); err != nil {
			return fmt.Errorf("setting open position %d: %w", position.PositionId, err)
		}
	}
	for _, position := range data.ClosedPositions {
		if err := k.ClosedPositions.Set(ctx, position.PositionId, position); err != nil {
			return fmt.Errorf("setting closed position %d: %w", position.PositionId, err)
		}
	}
	for _, entry := range data.Ledger {
		if err := k.Ledger.Set(ctx, entry.EntryId, entry); err != nil {
			return fmt.Errorf("setting ledger entry %d: %w", entry.EntryId, err)
		}
	}
	for _, entry := range data.RecognitionPolicy {
		if err := k.RecognitionPolicy.Set(ctx, entry.Denom, entry); err != nil {
			return fmt.Errorf("setting eligibility entry for %s: %w", entry.Denom, err)
		}
	}

	return nil
}

// ExportGenesis exports the Reserve-owned state. The custody balance remains
// part of Bank genesis.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting Reserve params: %w", err)
	}
	reserveMandate, err := k.Mandate.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting Reserve mandate: %w", err)
	}
	allowanceUsed, err := k.AllowanceUsed.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting Reserve allowance usage: %w", err)
	}
	nextPositionID, err := k.NextPositionID.Peek(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting next position ID: %w", err)
	}
	nextEntryID, err := k.NextEntryID.Peek(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting next entry ID: %w", err)
	}

	openPositions := make([]types.Position, 0)
	if err := k.OpenPositions.Walk(ctx, nil, func(_ uint64, position types.Position) (bool, error) {
		openPositions = append(openPositions, position)
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating open positions: %w", err)
	}
	closedPositions := make([]types.Position, 0)
	if err := k.ClosedPositions.Walk(ctx, nil, func(_ uint64, position types.Position) (bool, error) {
		closedPositions = append(closedPositions, position)
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating closed positions: %w", err)
	}
	ledger := make([]types.AccountingEntry, 0)
	if err := k.Ledger.Walk(ctx, nil, func(_ uint64, entry types.AccountingEntry) (bool, error) {
		ledger = append(ledger, entry)
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating ledger: %w", err)
	}
	recognitionPolicy := make([]types.EligibilityEntry, 0)
	if err := k.RecognitionPolicy.Walk(ctx, nil, func(_ string, entry types.EligibilityEntry) (bool, error) {
		recognitionPolicy = append(recognitionPolicy, entry)
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating Reserve recognition policy: %w", err)
	}

	return types.NewGenesisState(
		params,
		reserveMandate,
		recognitionPolicy,
		allowanceUsed,
		openPositions,
		closedPositions,
		ledger,
		nextPositionID,
		nextEntryID,
	), nil
}
