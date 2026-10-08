package types

import (
	"errors"
	stdmath "math"

	"cosmossdk.io/math"
)

const (
	MaxBatch    = 100
	MaxPageSize = 100
	// MaxPaymentsPerBlock bounds EndBlock work; entries past it stay due for the next block.
	MaxPaymentsPerBlock = 1_000
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

// fitsInt64 reports whether a chain instant or height, uint64 on the wire, fits the block header's int64.
func fitsInt64(v uint64) bool {
	return v <= stdmath.MaxInt64
}

// positiveInt64 is fitsInt64 for a value that must also be set.
func positiveInt64(v uint64) bool {
	return v != 0 && fitsInt64(v)
}
