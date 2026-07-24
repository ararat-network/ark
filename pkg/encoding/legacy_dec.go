package encoding

import (
	"fmt"

	"cosmossdk.io/math"
)

// MaxEncodedLegacyDecBytes bounds the canonical base-10 integer encoding used
// by math.LegacyDec.Marshal. The current LegacyDec range fits within 97 bytes;
// 128 bytes retains conservative headroom while rejecting unbounded inputs
// before big.Int parsing.
const MaxEncodedLegacyDecBytes = 128

func EncodeLegacyDec(value math.LegacyDec) ([]byte, error) {
	if value.IsNil() {
		return nil, fmt.Errorf("nil LegacyDec")
	}
	if !value.IsInValidRange() {
		return nil, fmt.Errorf("LegacyDec is out of range")
	}

	bz, err := value.Marshal()
	if err != nil {
		return nil, err
	}
	if len(bz) > MaxEncodedLegacyDecBytes {
		return nil, fmt.Errorf(
			"encoded LegacyDec length %d exceeds maximum %d",
			len(bz),
			MaxEncodedLegacyDecBytes,
		)
	}

	return bz, nil
}

func DecodeLegacyDec(bz []byte) (math.LegacyDec, error) {
	if len(bz) == 0 {
		return math.LegacyDec{}, fmt.Errorf("empty LegacyDec bytes")
	}
	if len(bz) > MaxEncodedLegacyDecBytes {
		return math.LegacyDec{}, fmt.Errorf(
			"encoded LegacyDec length %d exceeds maximum %d",
			len(bz),
			MaxEncodedLegacyDecBytes,
		)
	}

	var value math.LegacyDec
	if err := value.Unmarshal(bz); err != nil {
		return math.LegacyDec{}, err
	}
	if value.IsNil() {
		return math.LegacyDec{}, fmt.Errorf("nil LegacyDec")
	}

	return value, nil
}
