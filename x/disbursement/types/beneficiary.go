package types

import (
	"bytes"
	"fmt"
	"slices"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
)

// Validate checks a record's addresses and bounds; import checks its ownership against its grants and
// its controller against its journal.
func (b Beneficiary) Validate() error {
	if _, err := chain.ParseCanonicalAccountAddress("beneficiary", b.Address); err != nil {
		return err
	}
	if _, err := chain.ParseCanonicalAccountAddress("beneficiary controller", b.Controller); err != nil {
		return err
	}
	if err := ValidateAmount(b.OwnershipPaid, false); err != nil {
		return fmt.Errorf("ownership paid: %w", err)
	}
	if err := ValidateAmount(b.Seat, false); err != nil {
		return fmt.Errorf("seat: %w", err)
	}
	return nil
}

// SortBeneficiaries puts records in address order, the order the store walks and genesis requires.
func SortBeneficiaries(list []Beneficiary) {
	key := func(p Beneficiary) sdk.AccAddress {
		address, _ := chain.ParseCanonicalAccountAddress("beneficiary", p.Address)
		return address
	}
	slices.SortFunc(list, func(a, b Beneficiary) int {
		return bytes.Compare(key(a), key(b))
	})
}
