package types

import (
	"errors"

	"cosmossdk.io/math"
)

const (
	MaxBatch          = 100
	MaxReferenceBytes = 2_048
	MaxPageSize       = 100
)

// ValidateAmount caps one commitment and the outstanding reservation at 128 bits.
func ValidateAmount(amount math.Int, positive bool) error {
	if amount.IsNil() || amount.IsNegative() || amount.BigInt().BitLen() > 128 {
		return errors.New("amount must be nonnegative and at most 128 bits")
	}
	if positive && amount.IsZero() {
		return errors.New("amount must be positive")
	}
	return nil
}
