package mexc

// Field numbers follow MEXC's published schema, mexcdevelop/websocket-proto
// (Apache-2.0); THIRD_PARTY_NOTICES.md names the revision. Only the fields
// this adapter reads are named; the rest are skipped, which is what keeps
// the reader working when MEXC adds fields.

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"google.golang.org/protobuf/encoding/protowire"
)

const (
	// wrapperChannelField is PushDataV3ApiWrapper.channel.
	wrapperChannelField protowire.Number = 1
	// wrapperSymbolField is PushDataV3ApiWrapper.symbol.
	wrapperSymbolField protowire.Number = 3
	// wrapperMiniTickerField is PushDataV3ApiWrapper.publicMiniTicker.
	wrapperMiniTickerField protowire.Number = 309

	// miniTickerSymbolField is PublicMiniTickerV3Api.symbol.
	miniTickerSymbolField protowire.Number = 1
	// miniTickerPriceField is PublicMiniTickerV3Api.price.
	miniTickerPriceField protowire.Number = 2
)

// Push is what this adapter reads from a PushDataV3ApiWrapper frame.
type Push struct {
	// Channel is the stream the frame belongs to.
	Channel string
	// Symbol is the wrapper's symbol, when the frame carries one.
	Symbol string
	// MiniTicker is the frame's mini ticker, nil when the frame carries
	// another body.
	MiniTicker *MiniTicker
}

// MiniTicker carries the fields this adapter reads from a
// PublicMiniTickerV3Api body.
type MiniTicker struct {
	Symbol string
	Price  string
}

// DecodePush decodes a push frame.
func DecodePush(frame []byte) (Push, error) {
	var push Push
	err := walkFields(frame, func(num protowire.Number, typ protowire.Type, raw []byte) error {
		switch num {
		case wrapperChannelField:
			value, err := stringField(num, typ, raw)
			if err != nil {
				return err
			}
			push.Channel = value
		case wrapperSymbolField:
			value, err := stringField(num, typ, raw)
			if err != nil {
				return err
			}
			push.Symbol = value
		case wrapperMiniTickerField:
			body, err := bytesField(num, typ, raw)
			if err != nil {
				return err
			}
			ticker, err := decodeMiniTicker(body)
			if err != nil {
				return err
			}
			push.MiniTicker = &ticker
		}
		return nil
	})
	if err != nil {
		return Push{}, fmt.Errorf("decoding push frame: %w", err)
	}

	return push, nil
}

// decodeMiniTicker decodes a PublicMiniTickerV3Api body.
func decodeMiniTicker(body []byte) (MiniTicker, error) {
	var ticker MiniTicker
	err := walkFields(body, func(num protowire.Number, typ protowire.Type, raw []byte) error {
		switch num {
		case miniTickerSymbolField:
			value, err := stringField(num, typ, raw)
			if err != nil {
				return err
			}
			ticker.Symbol = value
		case miniTickerPriceField:
			value, err := stringField(num, typ, raw)
			if err != nil {
				return err
			}
			ticker.Price = value
		}
		return nil
	})
	if err != nil {
		return MiniTicker{}, fmt.Errorf("decoding mini ticker: %w", err)
	}

	return ticker, nil
}

// walkFields visits every field of a message with its tag and the raw
// bytes of its value. A truncated or malformed field ends the walk.
func walkFields(b []byte, visit func(num protowire.Number, typ protowire.Type, raw []byte) error) error {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return protowire.ParseError(n)
		}
		b = b[n:]

		n = protowire.ConsumeFieldValue(num, typ, b)
		if n < 0 {
			return fmt.Errorf("field %d: %w", num, protowire.ParseError(n))
		}
		if err := visit(num, typ, b[:n]); err != nil {
			return err
		}
		b = b[n:]
	}

	return nil
}

// bytesField returns the content of a length-delimited field.
func bytesField(num protowire.Number, typ protowire.Type, raw []byte) ([]byte, error) {
	if typ != protowire.BytesType {
		return nil, fmt.Errorf("field %d has wire type %d, expected %d", num, typ, protowire.BytesType)
	}

	value, n := protowire.ConsumeBytes(raw)
	if n < 0 {
		return nil, fmt.Errorf("field %d: %w", num, protowire.ParseError(n))
	}

	return value, nil
}

// stringField returns a length-delimited field as a string, which protobuf
// requires to be valid UTF-8.
func stringField(num protowire.Number, typ protowire.Type, raw []byte) (string, error) {
	value, err := bytesField(num, typ, raw)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(value) {
		return "", fmt.Errorf("field %d: %w", num, errors.New("string is not valid UTF-8"))
	}

	return string(value), nil
}
