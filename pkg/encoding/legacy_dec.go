package encoding

import (
	"fmt"
	"math/big"

	"cosmossdk.io/math"
)

// MaxEncodedCompactLegacyDecBytes bounds the minimal big-endian encoding of a
// positive LegacyDec raw value. Valid raw values stay under 2^315, which is
// 40 bytes.
const MaxEncodedCompactLegacyDecBytes = 40

// EncodeCompactLegacyDec encodes a strictly positive LegacyDec as the minimal
// big-endian bytes of its raw value*10^18: never empty, never a leading zero
// byte, so every value has exactly one encoding.
func EncodeCompactLegacyDec(value math.LegacyDec) ([]byte, error) {
	if value.IsNil() {
		return nil, fmt.Errorf("nil LegacyDec")
	}
	if !value.IsInValidRange() {
		return nil, fmt.Errorf("LegacyDec is out of range")
	}
	if !value.IsPositive() {
		return nil, fmt.Errorf("LegacyDec is not positive")
	}

	return value.BigInt().Bytes(), nil
}

// DecodeCompactLegacyDec decodes the minimal big-endian encoding produced by
// EncodeCompactLegacyDec.
func DecodeCompactLegacyDec(bz []byte) (math.LegacyDec, error) {
	if len(bz) == 0 {
		return math.LegacyDec{}, fmt.Errorf("empty LegacyDec bytes")
	}
	if len(bz) > MaxEncodedCompactLegacyDecBytes {
		return math.LegacyDec{}, fmt.Errorf(
			"encoded LegacyDec length %d exceeds maximum %d",
			len(bz),
			MaxEncodedCompactLegacyDecBytes,
		)
	}
	if bz[0] == 0 {
		return math.LegacyDec{}, fmt.Errorf("encoded LegacyDec has a leading zero byte")
	}

	value := math.LegacyNewDecFromBigIntWithPrec(new(big.Int).SetBytes(bz), math.LegacyPrecision)
	if !value.IsInValidRange() {
		return math.LegacyDec{}, fmt.Errorf("LegacyDec is out of range")
	}

	return value, nil
}
