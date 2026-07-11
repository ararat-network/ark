package encoding

import (
	"fmt"

	"cosmossdk.io/math"

	arkencoding "ark/pkg/encoding"
)

// MaxEncodedRateBytes bounds one encoded exchange rate in a vote extension.
const MaxEncodedRateBytes = 128

func EncodeRate(rate math.LegacyDec) ([]byte, error) {
	return arkencoding.EncodeLegacyDec(rate)
}

func DecodeRate(bz []byte) (math.LegacyDec, error) {
	if len(bz) > MaxEncodedRateBytes {
		return math.LegacyDec{}, fmt.Errorf(
			"oracle rate bytes length %d exceeds maximum %d",
			len(bz),
			MaxEncodedRateBytes,
		)
	}

	return arkencoding.DecodeLegacyDec(bz)
}
