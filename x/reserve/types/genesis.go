package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"

	"ark/pkg/chain"
)

// NewGenesisState creates a Reserve genesis state.
func NewGenesisState(
	params Params,
	reserveMandate ReserveMandate,
	recognitionPolicy []EligibilityEntry,
	allowanceUsed math.Int,
	openPositions []Position,
	closedPositions []Position,
	ledger []AccountingEntry,
	nextPositionID uint64,
	nextEntryID uint64,
) *GenesisState {
	return &GenesisState{
		Params:            params,
		Mandate:           reserveMandate,
		RecognitionPolicy: append([]EligibilityEntry(nil), recognitionPolicy...),
		AllowanceUsed:     chain.NoahCoin(allowanceUsed),
		OpenPositions:     append([]Position(nil), openPositions...),
		ClosedPositions:   append([]Position(nil), closedPositions...),
		Ledger:            append([]AccountingEntry(nil), ledger...),
		NextPositionId:    nextPositionID,
		NextEntryId:       nextEntryID,
	}
}

// DefaultGenesisState returns the safe, unconfigured Reserve genesis state:
// no committee, no positions, an empty ledger, no eligible assets.
func DefaultGenesisState() *GenesisState {
	return NewGenesisState(
		DefaultParams(),
		DefaultReserveMandate(),
		[]EligibilityEntry{},
		math.ZeroInt(),
		[]Position{},
		[]Position{},
		[]AccountingEntry{},
		1,
		1,
	)
}

// Validate checks the context-free Reserve genesis invariants. Every stored
// aggregate is re-derived from the ledger and required to match. The Reserve's
// custody balance is Bank state and is validated by the keeper instead.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	if err := gs.Mandate.Validate(); err != nil {
		return err
	}
	if err := chain.ValidateNoahCoin("allowance used", gs.AllowanceUsed); err != nil {
		return err
	}
	if gs.AllowanceUsed.Amount.GT(gs.Mandate.DeploymentAllowance.Amount) {
		return fmt.Errorf(
			"reserve allowance usage %s exceeds mandate allowance %s",
			gs.AllowanceUsed,
			gs.Mandate.DeploymentAllowance,
		)
	}
	if gs.NextPositionId == 0 {
		return errors.New("next position ID must be positive")
	}
	if gs.NextEntryId == 0 {
		return errors.New("next entry ID must be positive")
	}
	if err := ValidateRecognitionPolicy(gs.RecognitionPolicy); err != nil {
		return err
	}
	for i := 1; i < len(gs.RecognitionPolicy); i++ {
		if gs.RecognitionPolicy[i].Denom <= gs.RecognitionPolicy[i-1].Denom {
			return errors.New("genesis recognition policy must be sorted by asset denomination")
		}
	}

	positions := make(map[uint64]Position, len(gs.OpenPositions)+len(gs.ClosedPositions))
	if err := collectPositions(gs.OpenPositions, false, gs.NextPositionId, positions); err != nil {
		return err
	}
	if err := collectPositions(gs.ClosedPositions, true, gs.NextPositionId, positions); err != nil {
		return err
	}

	// Fold the ledger into the figures it should have produced. Only the
	// entries carrying a proven coin movement contribute; corrections restate
	// rather than accumulate.
	deployedByPosition := make(map[uint64]math.Int, len(positions))
	returnedByPosition := make(map[uint64]math.Int, len(positions))
	entryIDs := make(map[uint64]struct{}, len(gs.Ledger))
	for i, entry := range gs.Ledger {
		if err := entry.Validate(); err != nil {
			return fmt.Errorf("invalid ledger entry %d: %w", entry.EntryId, err)
		}
		if entry.EntryId >= gs.NextEntryId {
			return fmt.Errorf(
				"entry ID %d must be below next entry ID %d",
				entry.EntryId,
				gs.NextEntryId,
			)
		}
		if i > 0 && entry.EntryId <= gs.Ledger[i-1].EntryId {
			return errors.New("genesis ledger must be sorted by unique entry ID")
		}
		if _, known := positions[entry.PositionId]; !known {
			return fmt.Errorf(
				"ledger entry %d names unknown position %d",
				entry.EntryId,
				entry.PositionId,
			)
		}
		if entry.Corrects != 0 {
			// Entry.Validate already required the corrected ID to be lower, so
			// a correction can only reference an entry this walk has passed.
			if _, known := entryIDs[entry.Corrects]; !known {
				return fmt.Errorf(
					"ledger entry %d corrects unknown entry %d",
					entry.EntryId,
					entry.Corrects,
				)
			}
		}
		entryIDs[entry.EntryId] = struct{}{}

		var err error
		switch entry.Kind {
		case EntryKind_ENTRY_KIND_DEPLOYMENT:
			deployedByPosition[entry.PositionId], err = sumInto(deployedByPosition, entry.PositionId, entry.MovedNoahValue.Amount)
			if err != nil {
				return fmt.Errorf("summing deployments for position %d: %w", entry.PositionId, err)
			}
		case EntryKind_ENTRY_KIND_RETURN_ATTRIBUTION:
			returnedByPosition[entry.PositionId], err = sumInto(returnedByPosition, entry.PositionId, entry.MovedNoahValue.Amount)
			if err != nil {
				return fmt.Errorf("summing returns for position %d: %w", entry.PositionId, err)
			}
		}
	}

	// Both lists reconcile against the ledger on identical terms.
	if err := reconcilePositions(gs.OpenPositions, deployedByPosition, returnedByPosition); err != nil {
		return err
	}
	if err := reconcilePositions(gs.ClosedPositions, deployedByPosition, returnedByPosition); err != nil {
		return err
	}

	return nil
}

// collectPositions validates one status-partitioned position list and folds
// it into the shared identifier map, so an identifier reused across the two
// lists fails rather than silently overwriting. Each record's closing height
// must agree with the list carrying it.
func collectPositions(list []Position, closed bool, nextPositionID uint64, positions map[uint64]Position) error {
	label := "open"
	if closed {
		label = "closed"
	}
	for i, position := range list {
		if err := position.Validate(); err != nil {
			return fmt.Errorf("invalid %s position %d: %w", label, position.PositionId, err)
		}
		if position.IsClosed() != closed {
			return fmt.Errorf(
				"position %d is in the %s list but records a closed height of %d",
				position.PositionId,
				label,
				position.ClosedHeight,
			)
		}
		if position.PositionId >= nextPositionID {
			return fmt.Errorf(
				"position ID %d must be below next position ID %d",
				position.PositionId,
				nextPositionID,
			)
		}
		if i > 0 && position.PositionId <= list[i-1].PositionId {
			return fmt.Errorf("genesis %s positions must be sorted by unique position ID", label)
		}
		// Within-list duplicates are already caught by the sort check above, so
		// a collision here can only be the same identifier in both lists.
		if _, duplicate := positions[position.PositionId]; duplicate {
			return fmt.Errorf("position ID %d appears in both position lists", position.PositionId)
		}
		positions[position.PositionId] = position
	}
	return nil
}

// reconcilePositions requires one status-partitioned position list to agree
// with the ledger sums folded from the entries naming it. The list is walked
// in order, so which mismatch an import reports first is deterministic.
func reconcilePositions(list []Position, deployedByPosition, returnedByPosition map[uint64]math.Int) error {
	for _, position := range list {
		deployed := zeroIfAbsent(deployedByPosition, position.PositionId)
		if !deployed.Equal(position.Deployed.Amount) {
			return fmt.Errorf(
				"position %d deployed %s does not equal its ledger sum %s",
				position.PositionId,
				position.Deployed.Amount,
				deployed,
			)
		}
		returned := zeroIfAbsent(returnedByPosition, position.PositionId)
		if !returned.Equal(position.Returned.Amount) {
			return fmt.Errorf(
				"position %d returned %s does not equal its ledger sum %s",
				position.PositionId,
				position.Returned.Amount,
				returned,
			)
		}
	}
	return nil
}

// sumInto adds amount to the running total for id.
func sumInto(totals map[uint64]math.Int, id uint64, amount math.Int) (math.Int, error) {
	return zeroIfAbsent(totals, id).SafeAdd(amount)
}

// zeroIfAbsent reads a running total, treating an absent key as zero.
func zeroIfAbsent(totals map[uint64]math.Int, id uint64) math.Int {
	if total, ok := totals[id]; ok {
		return total
	}
	return math.ZeroInt()
}
