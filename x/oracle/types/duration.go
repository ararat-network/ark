package types

import (
	"encoding/json"
	"fmt"
	"time"

	"cosmossdk.io/collections"
	collcodec "cosmossdk.io/collections/codec"
)

// DurationValue codes a time.Duration collection value.
//
// Collections ships no duration codec, and neither substitute is honest. A
// bare int64 loses the unit everywhere the value is read back as text, and
// storing the generated ExchangeRateAgeOverride message would repeat the
// denomination already held in the key, leaving two copies free to disagree.
var DurationValue collcodec.ValueCodec[time.Duration] = durationValue{}

type durationValue struct{}

// Encode delegates to the int64 codec: a duration already is a nanosecond
// count, so the stored bytes stay one.
func (durationValue) Encode(value time.Duration) ([]byte, error) {
	return collections.Int64Value.Encode(int64(value))
}

func (durationValue) Decode(b []byte) (time.Duration, error) {
	nanoseconds, err := collections.Int64Value.Decode(b)
	if err != nil {
		return 0, err
	}

	return time.Duration(nanoseconds), nil
}

// EncodeJSON writes the duration as a string so schema export and store
// introspection read "24h0m0s" rather than a nanosecond count.
func (durationValue) EncodeJSON(value time.Duration) ([]byte, error) {
	return json.Marshal(value.String())
}

func (durationValue) DecodeJSON(b []byte) (time.Duration, error) {
	var encoded string
	if err := json.Unmarshal(b, &encoded); err != nil {
		return 0, err
	}
	duration, err := time.ParseDuration(encoded)
	if err != nil {
		return 0, fmt.Errorf("parsing duration %q: %w", encoded, err)
	}

	return duration, nil
}

func (durationValue) Stringify(value time.Duration) string {
	return value.String()
}

func (durationValue) ValueType() string {
	return "time.Duration"
}
