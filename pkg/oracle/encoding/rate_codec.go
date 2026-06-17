package encoding

import (
	"fmt"

	"cosmossdk.io/math"
)

const MaxRateBytes = 128

func EncodeRate(rate math.LegacyDec) ([]byte, error) {
	if rate.IsNil() {
		return nil, fmt.Errorf("nil oracle rate")
	}
	return rate.Marshal()
}

func DecodeRate(bz []byte) (math.LegacyDec, error) {
	if len(bz) == 0 {
		return math.LegacyDec{}, fmt.Errorf("empty oracle rate bytes")
	}
	if len(bz) > MaxRateBytes {
		return math.LegacyDec{}, fmt.Errorf("oracle rate bytes length %d exceeds maximum %d", len(bz), MaxRateBytes)
	}

	var rate math.LegacyDec
	if err := rate.Unmarshal(bz); err != nil {
		return math.LegacyDec{}, err
	}
	if rate.IsNil() {
		return math.LegacyDec{}, fmt.Errorf("nil oracle rate")
	}

	return rate, nil
}
