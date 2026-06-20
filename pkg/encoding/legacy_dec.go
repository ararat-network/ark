package encoding

import (
	"fmt"

	"cosmossdk.io/math"
)

func EncodeLegacyDec(value math.LegacyDec) ([]byte, error) {
	if value.IsNil() {
		return nil, fmt.Errorf("nil LegacyDec")
	}

	return value.Marshal()
}

func DecodeLegacyDec(bz []byte) (math.LegacyDec, error) {
	if len(bz) == 0 {
		return math.LegacyDec{}, fmt.Errorf("empty LegacyDec bytes")
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
