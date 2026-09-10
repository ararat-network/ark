// Package decimal provides checked LegacyDec arithmetic with SDK-identical results and errors in
// place of panics. This equivalence is consensus-critical; see README.md for arithmetic boundaries
// and differential verification.
package decimal

import (
	"errors"
	"math/big"

	"cosmossdk.io/math"
)

var (
	ErrNil            = errors.New("decimal is nil")
	ErrOutOfRange     = errors.New("decimal is out of range")
	ErrDivisionByZero = errors.New("decimal division by zero")
)

var (
	precision        = new(big.Int).Exp(big.NewInt(10), big.NewInt(math.LegacyPrecision), nil)
	squaredPrecision = new(big.Int).Mul(new(big.Int).Set(precision), precision)
)

const (
	// Valid LegacyDec raw values extend beyond 2^315. Adding or subtracting
	// operands no wider than 314 bits therefore cannot exceed the range.
	maxDirectAddSubOperandBitLen = 314

	// LegacyDec multiplication divides the raw product by 10^18, which is
	// greater than 2^59. A combined operand width of 374 bits therefore rounds
	// to at most 2^315.
	maxDirectMulOperandBitLenSum = 374

	// LegacyDec division multiplies the raw numerator by 10^18, which is less
	// than 2^60. A bit-length difference of 254 therefore rounds to at most
	// 2^315.
	maxDirectQuotientBitDelta = 254
)

// Add returns a+b when both operands and the result are representable.
func Add(a, b math.LegacyDec) (math.LegacyDec, error) {
	if err := validate(a); err != nil {
		return math.LegacyDec{}, err
	}
	if err := validate(b); err != nil {
		return math.LegacyDec{}, err
	}
	aRaw := a.BigIntMut()
	bRaw := b.BigIntMut()
	if aRaw.BitLen() <= maxDirectAddSubOperandBitLen &&
		bRaw.BitLen() <= maxDirectAddSubOperandBitLen {
		return a.Add(b), nil
	}

	return fromRaw(new(big.Int).Add(aRaw, bRaw))
}

// Sub returns a-b when both operands and the result are representable.
func Sub(a, b math.LegacyDec) (math.LegacyDec, error) {
	if err := validate(a); err != nil {
		return math.LegacyDec{}, err
	}
	if err := validate(b); err != nil {
		return math.LegacyDec{}, err
	}
	aRaw := a.BigIntMut()
	bRaw := b.BigIntMut()
	if aRaw.BitLen() <= maxDirectAddSubOperandBitLen &&
		bRaw.BitLen() <= maxDirectAddSubOperandBitLen {
		return a.Sub(b), nil
	}

	return fromRaw(new(big.Int).Sub(aRaw, bRaw))
}

// Mul returns a*b with LegacyDec's existing bankers-rounding semantics when
// both operands and the result are representable.
func Mul(a, b math.LegacyDec) (math.LegacyDec, error) {
	if err := validate(a); err != nil {
		return math.LegacyDec{}, err
	}
	if err := validate(b); err != nil {
		return math.LegacyDec{}, err
	}
	aRaw := a.BigIntMut()
	bRaw := b.BigIntMut()
	if aRaw.BitLen()+bRaw.BitLen() <= maxDirectMulOperandBitLenSum {
		return a.Mul(b), nil
	}

	product := new(big.Int).Mul(aRaw, bRaw)
	return fromRaw(roundByPrecision(product))
}

// Quo returns a/b with LegacyDec's existing bankers-rounding semantics when
// both operands and the result are representable.
func Quo(a, b math.LegacyDec) (math.LegacyDec, error) {
	if err := validate(a); err != nil {
		return math.LegacyDec{}, err
	}
	if err := validate(b); err != nil {
		return math.LegacyDec{}, err
	}
	if b.IsZero() {
		return math.LegacyDec{}, ErrDivisionByZero
	}

	aRaw := a.BigIntMut()
	bRaw := b.BigIntMut()
	bitDelta := aRaw.BitLen() - bRaw.BitLen()
	if bitDelta <= maxDirectQuotientBitDelta {
		return a.Quo(b), nil
	}

	// LegacyDec.Quo keeps an extra precision factor during integer division,
	// then rounds the result back to 18 decimal places. Mirror that order so
	// checked arithmetic does not change consensus-visible rounding.
	quotient := new(big.Int).Mul(aRaw, squaredPrecision)
	quotient.Quo(quotient, bRaw)

	return fromRaw(roundByPrecision(quotient))
}

func validate(value math.LegacyDec) error {
	if value.IsNil() {
		return ErrNil
	}
	if !value.IsInValidRange() {
		return ErrOutOfRange
	}
	return nil
}

func fromRaw(value *big.Int) (math.LegacyDec, error) {
	result := math.LegacyNewDecFromBigIntWithPrec(value, math.LegacyPrecision)
	if !result.IsInValidRange() {
		return math.LegacyDec{}, ErrOutOfRange
	}
	return result, nil
}

// roundByPrecision removes one LegacyDec precision factor using the same
// round-half-to-even rule as cosmossdk.io/math.
func roundByPrecision(value *big.Int) *big.Int {
	sign := value.Sign()
	absValue := new(big.Int).Abs(value)
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(absValue, precision, remainder)

	twiceRemainder := new(big.Int).Lsh(remainder, 1)
	if cmp := twiceRemainder.Cmp(precision); cmp > 0 || cmp == 0 && quotient.Bit(0) == 1 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if sign < 0 {
		quotient.Neg(quotient)
	}

	return quotient
}
