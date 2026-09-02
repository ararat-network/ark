package types_test

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/reserve/types"
)

// validPosition returns one minimal valid open position, for the mutate
// pattern: each case changes exactly one aspect of this base.
func validPosition() types.Position {
	return types.Position{
		PositionId:     1,
		Quantity:       sdk.NewCoin(chain.XDRBaseDenom+"-x", math.NewInt(50)),
		Deployed:       chain.NoahCoin(math.NewInt(100)),
		Returned:       chain.NoahCoin(math.ZeroInt()),
		VenueReference: "custodian-alpha",
		OpenedHeight:   7,
	}
}

// testAddress builds a deterministic bech32 account address from one repeated
// byte, so entry validation cases need addresses that are valid but otherwise
// meaningless.
func testAddress(seed byte) string {
	return sdk.AccAddress(bytes.Repeat([]byte{seed}, 20)).String()
}

// validEntry returns a minimal valid ledger entry of the given kind, for the
// mutate pattern: each case changes exactly one aspect of this base.
func validEntry(kind types.EntryKind) types.AccountingEntry {
	entry := types.AccountingEntry{
		EntryId:        2,
		PositionId:     1,
		Kind:           kind,
		Quantity:       sdk.NewCoin(chain.XDRBaseDenom, math.NewInt(50)),
		MovedNoahValue: chain.NoahCoin(math.ZeroInt()),
		MovedCoin:      chain.NoahCoin(math.ZeroInt()),
		Reference:      "tx-0x01",
		RecordedBy:     testAddress(1),
		Height:         7,
		Term:           1,
	}
	switch kind {
	case types.EntryKind_ENTRY_KIND_DEPLOYMENT:
		// A NOAH outflow: the two legs agree at par.
		entry.MovedCoin = chain.NoahCoin(math.NewInt(100))
		entry.MovedNoahValue = chain.NoahCoin(math.NewInt(100))
	case types.EntryKind_ENTRY_KIND_RETURN_ATTRIBUTION:
		// An in-kind inflow: the legs differ, which is why both are stored.
		entry.MovedCoin = sdk.NewCoin(chain.XDRBaseDenom, math.NewInt(25))
		entry.MovedNoahValue = chain.NoahCoin(math.NewInt(60))
	case types.EntryKind_ENTRY_KIND_CORRECTION:
		entry.Corrects = 1
		// Either authority may correct, so the term is free; zero is the
		// governance case.
		entry.Term = 0
	case types.EntryKind_ENTRY_KIND_RETURN_REVERSAL:
		// Carries the reversed attribution's movement verbatim, so it is
		// shaped like the return above.
		entry.Corrects = 1
		entry.MovedCoin = sdk.NewCoin(chain.XDRBaseDenom, math.NewInt(25))
		entry.MovedNoahValue = chain.NoahCoin(math.NewInt(60))
		entry.Term = 0
	}
	return entry
}

// missingEvidenceErr is what a movement kind earns with a blank reference.
const missingEvidenceErr = "entry reference must not be empty"

// disabledMandateErr is the one answer every field of a disabled appointment
// must earn, whichever of them carries power it should not.
const disabledMandateErr = "unconfigured Reserve mandate must be empty"

// enabledReserveMandate returns one complete appointment, for the mutate
// pattern: a live allowance, the spending floor it may not breach, and the one
// custodian it may pay.
func enabledReserveMandate() types.ReserveMandate {
	appointment := types.NewDisabledReserveMandate(1)
	appointment.Committee = testAddress(9)
	appointment.ActivationHeight = 10
	appointment.ExpiryHeight = 20
	appointment.DeploymentAllowance = chain.NoahCoin(math.NewInt(1_000))
	appointment.MinimumNoahBalance = chain.NoahCoin(math.NewInt(100))
	appointment.Destinations = []string{testAddress(2)}

	return appointment
}

func TestDefaultReserveMandateIsDisabled(t *testing.T) {
	appointment := types.DefaultReserveMandate()

	require.True(t, appointment.IsDisabled())
	require.Equal(t, uint64(0), appointment.Term)
	require.True(t, appointment.DeploymentAllowance.Amount.IsZero())
	require.True(t, appointment.MinimumNoahBalance.Amount.IsZero())
	require.Empty(t, appointment.Destinations)
	require.NoError(t, appointment.Validate())
}

func TestValidateReserveMandate(t *testing.T) {
	tests := []struct {
		name      string
		mandate   func() types.ReserveMandate
		mutate    func(*types.ReserveMandate)
		expectErr string
	}{
		{
			name:    "disabled default",
			mandate: types.DefaultReserveMandate,
		},
		{
			// Disabling retains the term, so a transaction prepared under an
			// earlier appointment cannot become valid again.
			name:    "disabled retaining term",
			mandate: func() types.ReserveMandate { return types.NewDisabledReserveMandate(7) },
		},
		{
			name:    "configured",
			mandate: enabledReserveMandate,
		},
		{
			// A disabled mandate delegates nothing, so every field that spends
			// must read as spent-out. An allowance carried through a disablement
			// would say a committee may deploy when none is appointed.
			name:      "disabled with an allowance",
			mandate:   types.DefaultReserveMandate,
			mutate:    func(m *types.ReserveMandate) { m.DeploymentAllowance = chain.NoahCoin(math.NewInt(1)) },
			expectErr: disabledMandateErr,
		},
		{
			name:      "disabled with a spending floor",
			mandate:   types.DefaultReserveMandate,
			mutate:    func(m *types.ReserveMandate) { m.MinimumNoahBalance = chain.NoahCoin(math.NewInt(1)) },
			expectErr: disabledMandateErr,
		},
		{
			// Destinations are custodian relationships belonging to one
			// appointment, so an unappointed mandate names none.
			name:      "disabled with a destination",
			mandate:   types.DefaultReserveMandate,
			mutate:    func(m *types.ReserveMandate) { m.Destinations = []string{testAddress(2)} },
			expectErr: disabledMandateErr,
		},
		{
			name:      "disabled with a window",
			mandate:   types.DefaultReserveMandate,
			mutate:    func(m *types.ReserveMandate) { m.ExpiryHeight = 20 },
			expectErr: "Reserve mandate: disabled envelope must not have an activation or expiry height",
		},
		{
			name:      "configured zero term",
			mandate:   enabledReserveMandate,
			mutate:    func(m *types.ReserveMandate) { m.Term = 0 },
			expectErr: "Reserve mandate: configured term must be positive",
		},
		{
			name:      "non-canonical committee",
			mandate:   enabledReserveMandate,
			mutate:    func(m *types.ReserveMandate) { m.Committee = "invalid" },
			expectErr: "Reserve mandate: committee is invalid",
		},
		{
			name:      "empty window",
			mandate:   enabledReserveMandate,
			mutate:    func(m *types.ReserveMandate) { m.ActivationHeight = m.ExpiryHeight },
			expectErr: "Reserve mandate: activation height must precede expiry height",
		},
		{
			// The mirror of the disabled rule: an appointment that may spend
			// nothing is a stillborn delegation, and disabling is how that is
			// spelled.
			name:      "configured with no allowance",
			mandate:   enabledReserveMandate,
			mutate:    func(m *types.ReserveMandate) { m.DeploymentAllowance = chain.NoahCoin(math.ZeroInt()) },
			expectErr: "reserve deployment allowance must be positive",
		},
		{
			name:      "configured with no destination",
			mandate:   enabledReserveMandate,
			mutate:    func(m *types.ReserveMandate) { m.Destinations = nil },
			expectErr: "configured Reserve mandate must name at least one destination",
		},
		{
			name:      "duplicate destination",
			mandate:   enabledReserveMandate,
			mutate:    func(m *types.ReserveMandate) { m.Destinations = append(m.Destinations, m.Destinations[0]) },
			expectErr: "duplicate Reserve destination",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			appointment := testCase.mandate()
			if testCase.mutate != nil {
				testCase.mutate(&appointment)
			}

			err := appointment.Validate()
			if testCase.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, testCase.expectErr)
		})
	}
}

// TestPositionValidateBoundsTheAttestation pins the domain cap the recognition
// fold rests on. The quantity is a committee attestation about custody the
// chain cannot see, and the fold reading it every block is Treasury's
// settlement, where arithmetic that leaves range fails the block rather than
// the message that wrote it. Both magnitudes below reached that fold before
// the cap existed: the larger overflows the per-denomination sum, the smaller
// overflows the credit conversion against a small rate.
func TestPositionValidateBoundsTheAttestation(t *testing.T) {
	tests := []struct {
		name      string
		quantity  math.Int
		expectErr bool
	}{
		{
			name:     "an ordinary attestation",
			quantity: math.NewInt(50),
		},
		{
			// Boundary-valid: the cap itself is admissible, so the refusal
			// starts strictly above it.
			name:     "the cap itself",
			quantity: types.MaxAttestedQuantity,
		},
		{
			name:      "one base unit past the cap",
			quantity:  types.MaxAttestedQuantity.AddRaw(1),
			expectErr: true,
		},
		{
			name:      "a quantity near the Int ceiling",
			quantity:  math.NewIntFromBigInt(new(big.Int).Lsh(big.NewInt(1), 255)),
			expectErr: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			position := validPosition()
			position.Quantity.Amount = testCase.quantity

			err := position.Validate()
			if !testCase.expectErr {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, "position quantity must not exceed")
		})
	}
}

func TestAccountingEntryValidateTerm(t *testing.T) {
	// What remains committee-only is the mandate's own work: an outward
	// movement, an inflow attributed to one, and the attestation restated
	// between them. None can happen without an appointment, so none may carry
	// the governance term.
	t.Run("a committee entry must name its term", func(t *testing.T) {
		for _, kind := range []types.EntryKind{
			types.EntryKind_ENTRY_KIND_DEPLOYMENT,
			types.EntryKind_ENTRY_KIND_QUANTITY_UPDATE,
			types.EntryKind_ENTRY_KIND_RETURN_ATTRIBUTION,
		} {
			entry := validEntry(kind)
			entry.Term = 0
			require.ErrorContains(t, entry.Validate(), "must name the term", kind.String())
		}
	})

	// The ledger-keeping kinds are the ones either authority may write, so the
	// term is what records which one did rather than a constraint on who may.
	// Both directions must validate for every one of them.
	t.Run("the shared kinds accept either authority's term", func(t *testing.T) {
		for _, kind := range []types.EntryKind{
			types.EntryKind_ENTRY_KIND_IMPAIRMENT,
			types.EntryKind_ENTRY_KIND_CORRECTION,
			types.EntryKind_ENTRY_KIND_CLOSURE,
			types.EntryKind_ENTRY_KIND_RETURN_REVERSAL,
		} {
			committee := validEntry(kind)
			committee.Term = 3
			require.NoError(t, committee.Validate(), "committee act carries its term: %s", kind)

			governance := validEntry(kind)
			governance.Term = 0
			require.NoError(t, governance.Validate(), "governance act carries none: %s", kind)
		}
	})
}

func TestAccountingEntryValidateMovement(t *testing.T) {
	judgmentKinds := []types.EntryKind{
		types.EntryKind_ENTRY_KIND_QUANTITY_UPDATE,
		types.EntryKind_ENTRY_KIND_IMPAIRMENT,
		types.EntryKind_ENTRY_KIND_CLOSURE,
		types.EntryKind_ENTRY_KIND_CORRECTION,
	}

	t.Run("NOAH must be booked at par", func(t *testing.T) {
		entry := validEntry(types.EntryKind_ENTRY_KIND_DEPLOYMENT)
		entry.MovedNoahValue = chain.NoahCoin(math.NewInt(99))
		require.ErrorContains(t, entry.Validate(), "NOAH is valued at par")
	})

	t.Run("a non-NOAH outflow books its own valuation", func(t *testing.T) {
		entry := validEntry(types.EntryKind_ENTRY_KIND_DEPLOYMENT)
		entry.MovedCoin = sdk.NewCoin(chain.USDBaseDenom, math.NewInt(100))
		entry.MovedNoahValue = chain.NoahCoin(math.NewInt(250))
		require.NoError(t, entry.Validate())
	})

	t.Run("a deployment must be booked at a real valuation", func(t *testing.T) {
		entry := validEntry(types.EntryKind_ENTRY_KIND_DEPLOYMENT)
		entry.MovedCoin = sdk.NewCoin(chain.USDBaseDenom, math.NewInt(100))
		entry.MovedNoahValue = chain.NoahCoin(math.ZeroInt())
		require.ErrorContains(t, entry.Validate(), "positive booked value")
	})

	t.Run("a return may book zero under a dark feed", func(t *testing.T) {
		entry := validEntry(types.EntryKind_ENTRY_KIND_RETURN_ATTRIBUTION)
		entry.MovedNoahValue = chain.NoahCoin(math.ZeroInt())
		require.NoError(t, entry.Validate())
	})

	t.Run("a movement kind must move a coin", func(t *testing.T) {
		for _, kind := range []types.EntryKind{
			types.EntryKind_ENTRY_KIND_DEPLOYMENT,
			types.EntryKind_ENTRY_KIND_RETURN_ATTRIBUTION,
			types.EntryKind_ENTRY_KIND_RETURN_REVERSAL,
		} {
			entry := validEntry(kind)
			entry.MovedCoin = chain.NoahCoin(math.ZeroInt())
			entry.MovedNoahValue = chain.NoahCoin(math.ZeroInt())
			require.ErrorContains(t, entry.Validate(), "positive coin", kind.String())
		}
	})

	// The zero-booked-value case a dark feed produces must survive reversal.
	t.Run("a reversal may carry a zero booked value", func(t *testing.T) {
		entry := validEntry(types.EntryKind_ENTRY_KIND_RETURN_REVERSAL)
		entry.MovedCoin = sdk.NewCoin(chain.XDRBaseDenom, math.NewInt(100))
		entry.MovedNoahValue = chain.NoahCoin(math.ZeroInt())
		require.NoError(t, entry.Validate())
	})

	t.Run("a judgment kind may not carry a moved coin", func(t *testing.T) {
		for _, kind := range judgmentKinds {
			entry := validEntry(kind)
			entry.MovedCoin = sdk.NewCoin(chain.XDRBaseDenom, math.NewInt(5))
			require.ErrorContains(t, entry.Validate(), "may carry a movement", kind.String())
		}
	})

	t.Run("a judgment kind may not carry a booked value", func(t *testing.T) {
		for _, kind := range judgmentKinds {
			entry := validEntry(kind)
			entry.MovedNoahValue = chain.NoahCoin(math.NewInt(5))
			require.ErrorContains(t, entry.Validate(), "may carry a movement", kind.String())
		}
	})
}

func TestAccountingEntryValidateReference(t *testing.T) {
	cases := []struct {
		name    string
		kind    types.EntryKind
		wantErr string
	}{
		{
			name:    "deployment requires evidence",
			kind:    types.EntryKind_ENTRY_KIND_DEPLOYMENT,
			wantErr: missingEvidenceErr,
		},
		{
			name:    "return attribution requires evidence",
			kind:    types.EntryKind_ENTRY_KIND_RETURN_ATTRIBUTION,
			wantErr: missingEvidenceErr,
		},
		{
			name: "quantity update is a judgment and may omit evidence",
			kind: types.EntryKind_ENTRY_KIND_QUANTITY_UPDATE,
		},
		{
			name: "impairment is a judgment and may omit evidence",
			kind: types.EntryKind_ENTRY_KIND_IMPAIRMENT,
		},
		{
			name: "closure is a judgment and may omit evidence",
			kind: types.EntryKind_ENTRY_KIND_CLOSURE,
		},
		{
			name: "correction is a judgment and may omit evidence",
			kind: types.EntryKind_ENTRY_KIND_CORRECTION,
		},
		{
			name:    "return reversal requires evidence",
			kind:    types.EntryKind_ENTRY_KIND_RETURN_REVERSAL,
			wantErr: missingEvidenceErr,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			entry := validEntry(testCase.kind)
			require.NoError(t, entry.Validate(), "base entry must be valid")

			entry.Reference = ""
			err := entry.Validate()
			if testCase.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, testCase.wantErr)

			// Whitespace is not evidence.
			entry.Reference = "   "
			require.ErrorContains(t, entry.Validate(), testCase.wantErr)

			// A content address naming a bundle is the intended shape when one
			// act has several documents behind it.
			entry.Reference = "bafybeigdyrztktx5f5ihvxumhqrjq2dwjm4nzrn77oqk3zmnhpxwrbhviy"
			require.NoError(t, entry.Validate())
		})
	}
}

// TestAccountingEntryValidateRestatement covers the field the two restating
// kinds share: each names the earlier entry it acts on, and no other kind may.
func TestAccountingEntryValidateRestatement(t *testing.T) {
	restatingKinds := []types.EntryKind{
		types.EntryKind_ENTRY_KIND_CORRECTION,
		types.EntryKind_ENTRY_KIND_RETURN_REVERSAL,
	}

	t.Run("a restatement must name what it acts on", func(t *testing.T) {
		for _, kind := range restatingKinds {
			entry := validEntry(kind)
			entry.Corrects = 0
			require.ErrorContains(t, entry.Validate(), "must name the entry it restates", kind.String())
		}
	})

	// The ordering rule lets a genesis import's single forward pass resolve
	// every target, and stops an entry restating itself.
	t.Run("a restatement must act on an earlier entry", func(t *testing.T) {
		for _, kind := range restatingKinds {
			entry := validEntry(kind)
			entry.Corrects = entry.EntryId
			require.ErrorContains(t, entry.Validate(), "must restate an earlier entry", kind.String())

			entry.Corrects = entry.EntryId + 1
			require.ErrorContains(t, entry.Validate(), "must restate an earlier entry", kind.String())
		}
	})

	t.Run("no other kind may name a restated entry", func(t *testing.T) {
		for _, kind := range []types.EntryKind{
			types.EntryKind_ENTRY_KIND_DEPLOYMENT,
			types.EntryKind_ENTRY_KIND_QUANTITY_UPDATE,
			types.EntryKind_ENTRY_KIND_RETURN_ATTRIBUTION,
			types.EntryKind_ENTRY_KIND_IMPAIRMENT,
			types.EntryKind_ENTRY_KIND_CLOSURE,
		} {
			entry := validEntry(kind)
			entry.Corrects = 1
			require.ErrorContains(t, entry.Validate(), "may name a restated entry", kind.String())
		}
	})
}
