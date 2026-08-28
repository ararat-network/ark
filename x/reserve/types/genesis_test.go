package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/reserve/types"
)

// positionFixture returns one position and the deployment entry that funded
// it, keyed to the supplied identifiers. Genesis reconciles every position
// against the ledger, so a position can only be tested inside a pair that
// already balances — the mutate pattern then changes one thing about it.
func positionFixture(positionID, entryID uint64) (types.Position, types.AccountingEntry) {
	position := types.Position{
		PositionId:     positionID,
		Quantity:       sdk.NewCoin(chain.SDRBaseDenom, math.NewInt(50)),
		Deployed:       chain.NoahCoin(math.NewInt(100)),
		Returned:       chain.NoahCoin(math.ZeroInt()),
		VenueReference: "custodian-alpha",
		OpenedHeight:   7,
	}
	entry := types.AccountingEntry{
		EntryId:        entryID,
		PositionId:     positionID,
		Kind:           types.EntryKind_ENTRY_KIND_DEPLOYMENT,
		Quantity:       position.Quantity,
		MovedNoahValue: chain.NoahCoin(math.NewInt(100)),
		MovedCoin:      chain.NoahCoin(math.NewInt(100)),
		Reference:      "tx-0x01",
		RecordedBy:     testAddress(1),
		Height:         7,
		Term:           1,
	}
	return position, entry
}

// closed marks a position closed the way the keeper does, by stamping the
// height rather than a flag.
func closed(position types.Position, height uint64) types.Position {
	position.ClosedHeight = height
	return position
}

// genesisWithPositions returns a genesis carrying the supplied positions and
// the ledger that funded them, with the identifier bounds set to fit.
func genesisWithPositions(open, closedPositions []types.Position) *types.GenesisState {
	genesisState := types.DefaultGenesisState()
	ledger := make([]types.AccountingEntry, 0, len(open)+len(closedPositions))
	nextPositionID := uint64(1)
	entryID := uint64(1)
	for _, list := range [][]types.Position{open, closedPositions} {
		for _, position := range list {
			_, entry := positionFixture(position.PositionId, entryID)
			ledger = append(ledger, entry)
			entryID++
			if position.PositionId >= nextPositionID {
				nextPositionID = position.PositionId + 1
			}
		}
	}
	genesisState.OpenPositions = open
	genesisState.ClosedPositions = closedPositions
	genesisState.Ledger = ledger
	genesisState.NextPositionId = nextPositionID
	genesisState.NextEntryId = entryID
	return genesisState
}

func TestDefaultGenesisStateIsValid(t *testing.T) {
	require.NoError(t, types.DefaultGenesisState().Validate())
}

// TestGenesisPositionListMembership pins that the list a position arrives in
// is the store it lands in, so an import whose record contradicts its list
// would produce a position that reads one way to the recognition fold and the
// other way to anyone holding it.
func TestGenesisPositionListMembership(t *testing.T) {
	open, _ := positionFixture(1, 1)

	testCases := []struct {
		name   string
		mutate func(*types.GenesisState)
		expErr string
	}{
		{
			name:   "an open position with no closing height is valid",
			mutate: func(*types.GenesisState) {},
		},
		{
			name: "a closed position with a closing height is valid",
			mutate: func(gs *types.GenesisState) {
				gs.OpenPositions = nil
				gs.ClosedPositions = []types.Position{closed(open, 9)}
			},
		},
		{
			name: "an open position carrying a closing height is refused",
			mutate: func(gs *types.GenesisState) {
				gs.OpenPositions = []types.Position{closed(open, 9)}
			},
			expErr: "is in the open list but records a closed height of 9",
		},
		{
			name: "a closed position with no closing height is refused",
			mutate: func(gs *types.GenesisState) {
				gs.OpenPositions = nil
				gs.ClosedPositions = []types.Position{open}
			},
			expErr: "is in the closed list but records a closed height of 0",
		},
		{
			name: "a closing height before the opening height is refused",
			mutate: func(gs *types.GenesisState) {
				gs.OpenPositions = nil
				gs.ClosedPositions = []types.Position{closed(open, 6)}
			},
			expErr: "closed position cannot close before it opened",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			genesisState := genesisWithPositions([]types.Position{open}, nil)
			testCase.mutate(genesisState)

			err := genesisState.Validate()
			if testCase.expErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, testCase.expErr)
		})
	}
}

// TestGenesisPositionIdentifiers pins that identifiers stay unique and ordered
// across the split. One sequence issues both lists, so a collision between them
// is an import that would silently drop a record on the way into state.
func TestGenesisPositionIdentifiers(t *testing.T) {
	first, _ := positionFixture(1, 1)
	second, _ := positionFixture(2, 2)

	testCases := []struct {
		name   string
		open   []types.Position
		closed []types.Position
		expErr string
	}{
		{
			name:   "one identifier per list is valid",
			open:   []types.Position{first},
			closed: []types.Position{closed(second, 9)},
		},
		{
			name:   "the same identifier in both lists is refused",
			open:   []types.Position{first},
			closed: []types.Position{closed(first, 9)},
			expErr: "position ID 1 appears in both position lists",
		},
		{
			name:   "an unsorted open list is refused",
			open:   []types.Position{second, first},
			expErr: "genesis open positions must be sorted by unique position ID",
		},
		{
			name:   "an unsorted closed list is refused",
			closed: []types.Position{closed(second, 9), closed(first, 9)},
			expErr: "genesis closed positions must be sorted by unique position ID",
		},
		{
			name:   "a duplicate within one list is refused",
			open:   []types.Position{first, first},
			expErr: "genesis open positions must be sorted by unique position ID",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			genesisState := genesisWithPositions(testCase.open, testCase.closed)

			err := genesisState.Validate()
			if testCase.expErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, testCase.expErr)
		})
	}
}

// TestGenesisPositionsBoundedBySequence pins that both lists answer to the one
// sequence that issues them, so neither can import a position the keeper would
// later hand out again.
func TestGenesisPositionsBoundedBySequence(t *testing.T) {
	open, _ := positionFixture(1, 1)

	t.Run("an open position at the next identifier is refused", func(t *testing.T) {
		genesisState := genesisWithPositions([]types.Position{open}, nil)
		genesisState.NextPositionId = open.PositionId

		require.ErrorContains(t, genesisState.Validate(), "must be below next position ID")
	})

	t.Run("a closed position at the next identifier is refused", func(t *testing.T) {
		genesisState := genesisWithPositions(nil, []types.Position{closed(open, 9)})
		genesisState.NextPositionId = open.PositionId

		require.ErrorContains(t, genesisState.Validate(), "must be below next position ID")
	})
}

// TestGenesisReconcilesBothListsAgainstTheLedger pins that closing exempts a
// position from nothing: the ledger is the record for history exactly as it is
// for the open set.
func TestGenesisReconcilesBothListsAgainstTheLedger(t *testing.T) {
	open, _ := positionFixture(1, 1)

	t.Run("a tampered open position is refused", func(t *testing.T) {
		genesisState := genesisWithPositions([]types.Position{open}, nil)
		genesisState.OpenPositions[0].Deployed = chain.NoahCoin(math.NewInt(999))

		require.ErrorContains(t, genesisState.Validate(), "does not equal its ledger sum")
	})

	t.Run("a tampered closed position is refused", func(t *testing.T) {
		genesisState := genesisWithPositions(nil, []types.Position{closed(open, 9)})
		genesisState.ClosedPositions[0].Deployed = chain.NoahCoin(math.NewInt(999))

		require.ErrorContains(t, genesisState.Validate(), "does not equal its ledger sum")
	})

	t.Run("an entry naming a closed position reconciles", func(t *testing.T) {
		genesisState := genesisWithPositions(nil, []types.Position{closed(open, 9)})

		require.NoError(t, genesisState.Validate())
	})

	t.Run("an entry naming no imported position is refused", func(t *testing.T) {
		genesisState := genesisWithPositions([]types.Position{open}, nil)
		genesisState.Ledger[0].PositionId = 42

		require.ErrorContains(t, genesisState.Validate(), "names unknown position 42")
	})
}

// attributedRecovery is the anoah one attribution books.
const attributedRecovery = 60

// attributedGenesis returns a genesis whose open position has one return
// attributed to it. Reversal cases mutate it, so it must already balance.
func attributedGenesis(t *testing.T) *types.GenesisState {
	t.Helper()
	position, _ := positionFixture(1, 1)
	genesisState := genesisWithPositions([]types.Position{position}, nil)

	attribution := types.AccountingEntry{
		EntryId:        genesisState.NextEntryId,
		PositionId:     position.PositionId,
		Kind:           types.EntryKind_ENTRY_KIND_RETURN_ATTRIBUTION,
		Quantity:       position.Quantity,
		MovedNoahValue: chain.NoahCoin(math.NewInt(attributedRecovery)),
		MovedCoin:      sdk.NewCoin(chain.SDRBaseDenom, math.NewInt(25)),
		Reference:      "tx-0x02",
		RecordedBy:     testAddress(1),
		Height:         8,
		Term:           1,
	}
	genesisState.Ledger = append(genesisState.Ledger, attribution)
	genesisState.NextEntryId = attribution.EntryId + 1
	// The position carries the recovery its ledger implies.
	genesisState.OpenPositions[0].Returned = chain.NoahCoin(math.NewInt(attributedRecovery))
	require.NoError(t, genesisState.Validate(), "attributed fixture must balance")
	return genesisState
}

// withReversal appends the reversal of the last attribution and zeroes the
// recovery, which is the state a reversed chain exports.
func withReversal(genesisState *types.GenesisState) *types.GenesisState {
	attribution := genesisState.Ledger[len(genesisState.Ledger)-1]
	reversal := types.AccountingEntry{
		EntryId:        genesisState.NextEntryId,
		PositionId:     attribution.PositionId,
		Kind:           types.EntryKind_ENTRY_KIND_RETURN_REVERSAL,
		Corrects:       attribution.EntryId,
		Quantity:       attribution.Quantity,
		MovedNoahValue: attribution.MovedNoahValue,
		MovedCoin:      attribution.MovedCoin,
		Reference:      "custodian-statement-0x03",
		RecordedBy:     testAddress(1),
		Height:         9,
		Term:           0,
	}
	genesisState.Ledger = append(genesisState.Ledger, reversal)
	genesisState.NextEntryId = reversal.EntryId + 1
	genesisState.OpenPositions[0].Returned = chain.NoahCoin(math.ZeroInt())
	return genesisState
}

// TestGenesisFoldsReversalsOutOfTheRecovery pins that the fold treats a
// reversal as the subtraction it is, so a reversed chain can round-trip.
func TestGenesisFoldsReversalsOutOfTheRecovery(t *testing.T) {
	t.Run("a reversed attribution reconciles to zero recovery", func(t *testing.T) {
		require.NoError(t, withReversal(attributedGenesis(t)).Validate())
	})

	// A position still claiming the reversed recovery must not import.
	t.Run("a position keeping the reversed recovery is refused", func(t *testing.T) {
		genesisState := withReversal(attributedGenesis(t))
		genesisState.OpenPositions[0].Returned = chain.NoahCoin(math.NewInt(attributedRecovery))

		require.ErrorContains(t, genesisState.Validate(), "does not equal its ledger sum")
	})

	// Validate enforces "earlier" on shape alone; existence is separate, which
	// a gap in the identifier space is what exercises.
	t.Run("a reversal of an unknown entry is refused", func(t *testing.T) {
		genesisState := withReversal(attributedGenesis(t))
		reversal := &genesisState.Ledger[len(genesisState.Ledger)-1]
		reversal.EntryId = 10
		reversal.Corrects = 5
		genesisState.NextEntryId = 11

		require.ErrorContains(t, genesisState.Validate(), "restates unknown entry 5")
	})

	// Only an attribution adds to the recovery, so only one can be taken out.
	t.Run("a reversal of a non-attribution is refused", func(t *testing.T) {
		genesisState := withReversal(attributedGenesis(t))
		// Entry 1 is the deployment that opened the position.
		genesisState.Ledger[len(genesisState.Ledger)-1].Corrects = 1

		require.ErrorContains(t, genesisState.Validate(), "is not a return attribution")
	})

	t.Run("a second reversal of one attribution is refused", func(t *testing.T) {
		genesisState := withReversal(attributedGenesis(t))
		duplicate := genesisState.Ledger[len(genesisState.Ledger)-1]
		duplicate.EntryId = genesisState.NextEntryId
		genesisState.Ledger = append(genesisState.Ledger, duplicate)
		genesisState.NextEntryId = duplicate.EntryId + 1

		require.ErrorContains(t, genesisState.Validate(), "is already reversed")
	})

	// The verbatim rule stops an import booking a reversal at a value its
	// attribution never carried, as the live path does.
	t.Run("a reversal restating the booked value is refused", func(t *testing.T) {
		genesisState := withReversal(attributedGenesis(t))
		genesisState.Ledger[len(genesisState.Ledger)-1].MovedNoahValue = chain.NoahCoin(math.NewInt(59))

		require.ErrorContains(t, genesisState.Validate(), "verbatim")
	})

	t.Run("a reversal restating the moved coin is refused", func(t *testing.T) {
		genesisState := withReversal(attributedGenesis(t))
		genesisState.Ledger[len(genesisState.Ledger)-1].MovedCoin = sdk.NewCoin(chain.SDRBaseDenom, math.NewInt(24))

		require.ErrorContains(t, genesisState.Validate(), "verbatim")
	})
}

// TestGenesisRefusesRestatementsCrossingPositions pins that both restating
// kinds name an entry of their own position, as both live paths require: a
// crossing reversal would walk down a recovery it never contributed to.
func TestGenesisRefusesRestatementsCrossingPositions(t *testing.T) {
	t.Run("a reversal naming another position's entry is refused", func(t *testing.T) {
		genesisState := withReversal(attributedGenesis(t))
		second, secondEntry := positionFixture(2, genesisState.NextEntryId)
		genesisState.OpenPositions = append(genesisState.OpenPositions, second)
		genesisState.NextPositionId = 3
		// The reversal names position 1's entry while claiming position 2.
		genesisState.Ledger[len(genesisState.Ledger)-1].PositionId = 2
		genesisState.Ledger = append(genesisState.Ledger, secondEntry)
		genesisState.NextEntryId = secondEntry.EntryId + 1

		require.ErrorContains(t, genesisState.Validate(), "belongs to position 1, not 2")
	})

	t.Run("a correction naming another position's entry is refused", func(t *testing.T) {
		first, _ := positionFixture(1, 1)
		second, _ := positionFixture(2, 2)
		genesisState := genesisWithPositions([]types.Position{first, second}, nil)
		correction := types.AccountingEntry{
			EntryId:        genesisState.NextEntryId,
			PositionId:     2,
			Kind:           types.EntryKind_ENTRY_KIND_CORRECTION,
			Corrects:       1,
			Quantity:       second.Quantity,
			MovedNoahValue: chain.NoahCoin(math.ZeroInt()),
			MovedCoin:      chain.NoahCoin(math.ZeroInt()),
			RecordedBy:     testAddress(1),
			Height:         9,
		}
		genesisState.Ledger = append(genesisState.Ledger, correction)
		genesisState.NextEntryId = correction.EntryId + 1

		require.ErrorContains(t, genesisState.Validate(), "belongs to position 1, not 2")
	})
}
