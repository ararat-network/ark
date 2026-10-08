package chain

import (
	"fmt"
	"strings"
)

// MaxReferenceBytes bounds a free-form reference: a pointer to evidence the chain stores and never
// interprets, such as a transaction hash, a custodian account, or an off-chain case or agreement.
const MaxReferenceBytes = 2_048

// ValidateReference bounds a reference at MaxReferenceBytes and, when required, refuses a blank one.
func ValidateReference(field, value string, required bool) error {
	if required && strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must not be empty", field)
	}
	if len(value) > MaxReferenceBytes {
		return fmt.Errorf("%s must not exceed %d bytes", field, MaxReferenceBytes)
	}
	return nil
}
