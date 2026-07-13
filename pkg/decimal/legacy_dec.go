// Package decimal provides checked arithmetic for Cosmos SDK LegacyDec values.
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

// Add returns a+b when both operands and the result are representable.
func Add(a, b math.LegacyDec) (math.LegacyDec, error) {
	aRaw, err := raw(a)
	if err != nil {
		return math.LegacyDec{}, err
	}
	bRaw, err := raw(b)
	if err != nil {
		return math.LegacyDec{}, err
	}

	return fromRaw(new(big.Int).Add(aRaw, bRaw))
}

// Sub returns a-b when both operands and the result are representable.
func Sub(a, b math.LegacyDec) (math.LegacyDec, error) {
	aRaw, err := raw(a)
	if err != nil {
		return math.LegacyDec{}, err
	}
	bRaw, err := raw(b)
	if err != nil {
		return math.LegacyDec{}, err
	}

	return fromRaw(new(big.Int).Sub(aRaw, bRaw))
}

// Mul returns a*b with LegacyDec's existing bankers-rounding semantics when
// both operands and the result are representable.
func Mul(a, b math.LegacyDec) (math.LegacyDec, error) {
	aRaw, err := raw(a)
	if err != nil {
		return math.LegacyDec{}, err
	}
	bRaw, err := raw(b)
	if err != nil {
		return math.LegacyDec{}, err
	}

	product := new(big.Int).Mul(aRaw, bRaw)
	return fromRaw(roundByPrecision(product))
}

// Quo returns a/b with LegacyDec's existing bankers-rounding semantics when
// both operands and the result are representable.
func Quo(a, b math.LegacyDec) (math.LegacyDec, error) {
	aRaw, err := raw(a)
	if err != nil {
		return math.LegacyDec{}, err
	}
	bRaw, err := raw(b)
	if err != nil {
		return math.LegacyDec{}, err
	}
	if bRaw.Sign() == 0 {
		return math.LegacyDec{}, ErrDivisionByZero
	}

	// LegacyDec.Quo keeps an extra precision factor during integer division,
	// then rounds the result back to 18 decimal places. Mirror that order so
	// checked arithmetic does not change consensus-visible rounding.
	quotient := new(big.Int).Mul(aRaw, squaredPrecision)
	quotient.Quo(quotient, bRaw)

	return fromRaw(roundByPrecision(quotient))
}

func raw(value math.LegacyDec) (*big.Int, error) {
	if value.IsNil() {
		return nil, ErrNil
	}
	if !value.IsInValidRange() {
		return nil, ErrOutOfRange
	}
	return value.BigInt(), nil
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
