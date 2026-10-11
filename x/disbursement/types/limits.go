package types

import stdmath "math"

const (
	MaxBatch = 100
	// MaxPaymentsPerBlock bounds EndBlock work; entries past it stay due for the next block.
	MaxPaymentsPerBlock = 1_000
)

// fitsInt64 reports whether a chain instant or height, uint64 on the wire, fits the block header's int64.
func fitsInt64(v uint64) bool {
	return v <= stdmath.MaxInt64
}

// positiveInt64 is fitsInt64 for a value that must also be set.
func positiveInt64(v uint64) bool {
	return v != 0 && fitsInt64(v)
}
