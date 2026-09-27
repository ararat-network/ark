package types

import (
	"errors"
	"fmt"
	stdmath "math"

	"cosmossdk.io/math"
)

const (
	MaxBatch               = 100
	MaxSchedulePeriods     = 1_200
	MaxPeriodLength        = uint64(100 * 365 * 86_400)
	MaxIssuanceMembers     = uint64(10_000)
	MaxCompensationDenoms  = 64
	MaxReferenceBytes      = 2_048
	MaxJournalDetailsBytes = 131_072
	MaxPageSize            = 100
	// MaxTermGrants keeps a term's committee awards within one cancel batch.
	MaxTermGrants = MaxBatch
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

// Split floors each weighted period; the final period owns the remainder.
func Split(amount math.Int, periods []Period) ([]math.Int, error) {
	if err := ValidateAmount(amount, true); err != nil {
		return nil, err
	}
	if len(periods) == 0 || len(periods) > MaxSchedulePeriods {
		return nil, fmt.Errorf("schedule must have 1..%d periods", MaxSchedulePeriods)
	}
	var parts, duration uint64
	for _, p := range periods {
		if p.Length == 0 || p.Length > MaxPeriodLength || p.Parts == 0 {
			return nil, errors.New("period requires a bounded positive length and positive parts")
		}
		if parts > stdmath.MaxUint64-p.Parts {
			return nil, errors.New("schedule parts overflow")
		}
		parts += p.Parts
		duration += p.Length // At most 1,200 century-long periods, below uint64.
	}
	if duration > stdmath.MaxInt64 {
		return nil, errors.New("schedule duration overflows time")
	}
	remaining := amount
	result := make([]math.Int, len(periods))
	for i, p := range periods {
		share := remaining
		if i < len(periods)-1 {
			product, err := amount.SafeMul(math.NewIntFromUint64(p.Parts))
			if err != nil {
				return nil, fmt.Errorf("splitting schedule: %w", err)
			}
			share = product.Quo(math.NewIntFromUint64(parts))
		}
		if share.IsZero() {
			return nil, errors.New("every period must receive a positive amount")
		}
		remaining = remaining.Sub(share)
		result[i] = share
	}
	return result, nil
}

// Accrued returns total earned principal and the next accrual time. Members earn
// their first period at registration; cancellation freezes the original clock.
func Accrued(g Grant, now uint64) (math.Int, uint64, error) {
	shares, err := Split(g.Amount.Amount, g.Schedule)
	if err != nil {
		return math.Int{}, 0, err
	}
	if g.Cancelled {
		now = g.CutoffTime
	}
	paid := math.ZeroInt()
	end, next := g.StartTime, uint64(0)
	for i, p := range g.Schedule {
		if end > stdmath.MaxInt64-p.Length {
			return math.Int{}, 0, errors.New("schedule end overflows time")
		}
		end += p.Length
		if now >= end || (i == 0 && g.Kind == GrantKind_GRANT_KIND_MEMBER) {
			paid = paid.Add(shares[i]) // Partial sums cannot exceed the validated amount.
		} else if next == 0 {
			next = end
		}
	}
	if g.Cancelled {
		next = 0
	}
	return paid, next, nil
}

// MulDiv floors a payment ratio after a checked multiplication.
func MulDiv(amount, numerator, denominator math.Int) (math.Int, error) {
	if amount.IsNegative() || numerator.IsNegative() || !denominator.IsPositive() {
		return math.Int{}, errors.New("ratio requires nonnegative operands and a positive denominator")
	}
	product, err := amount.SafeMul(numerator)
	if err != nil {
		return math.Int{}, fmt.Errorf("payment ratio product: %w", err)
	}
	return product.Quo(denominator), nil
}

// PositiveDifference floors a remaining allowance at zero.
func PositiveDifference(a, b math.Int) math.Int {
	if a.LTE(b) {
		return math.ZeroInt()
	}
	return a.Sub(b)
}
