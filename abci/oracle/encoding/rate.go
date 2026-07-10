package encoding

import (
	"fmt"

	"cosmossdk.io/math"

	arkencoding "ark/pkg/encoding"
)

const MaxRateBytes = 128

func EncodeRate(rate math.LegacyDec) ([]byte, error) {
	return arkencoding.EncodeLegacyDec(rate)
}

func DecodeRate(bz []byte) (math.LegacyDec, error) {
	if len(bz) > MaxRateBytes {
		return math.LegacyDec{}, fmt.Errorf(
			"oracle rate bytes length %d exceeds maximum %d",
			len(bz),
			MaxRateBytes,
		)
	}

	return arkencoding.DecodeLegacyDec(bz)
}
