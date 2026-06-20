package encoding

import (
	"fmt"

	"cosmossdk.io/math"

	noahencoding "noah/pkg/encoding"
)

const MaxRateBytes = 128

func EncodeRate(rate math.LegacyDec) ([]byte, error) {
	return noahencoding.EncodeLegacyDec(rate)
}

func DecodeRate(bz []byte) (math.LegacyDec, error) {
	if len(bz) > MaxRateBytes {
		return math.LegacyDec{}, fmt.Errorf(
			"oracle rate bytes length %d exceeds maximum %d",
			len(bz),
			MaxRateBytes,
		)
	}

	return noahencoding.DecodeLegacyDec(bz)
}
