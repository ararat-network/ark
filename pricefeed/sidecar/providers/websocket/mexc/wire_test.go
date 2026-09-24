package mexc_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"

	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/mexc"
)

// liveFrame is a PushDataV3ApiWrapper captured from wbs-api.mexc.com on
// 2026-09-24: channel, symbol, sendTime, and a publicMiniTicker body whose
// price is 1.0001.
const liveFrame = "\n/spot@public.miniTicker.v3.api.pb@USDCUSDT@UTC+8\x1a\bUSDCUSDT0\xd0\xfb\xb5\x8c\x8d4\xaa\x13u\n\bUSDCUSDT\x12\x061.0001\x1a\x060.0001\"\x060.0001*\a1.000242\a0.99989:\x0f117749638.58042B\f117741814.97J\x060.0001R\x060.0001Z\a1.00024b\a0.99989"

func TestDecodePushLiveFrame(t *testing.T) {
	push, err := DecodePush([]byte(liveFrame))

	require.NoError(t, err)
	require.Equal(t, "spot@public.miniTicker.v3.api.pb@USDCUSDT@UTC+8", push.Channel)
	require.Equal(t, "USDCUSDT", push.Symbol)
	require.NotNil(t, push.MiniTicker)
	require.Equal(t, MiniTicker{Symbol: "USDCUSDT", Price: "1.0001"}, *push.MiniTicker)
}

func TestDecodePush(t *testing.T) {
	tests := []struct {
		name        string
		frame       []byte
		want        Push
		errContains string
	}{
		{
			name:  "reads the fields it names and skips the rest",
			frame: pushFrame("spot@public.miniTicker.v3.api.pb@USDCUSDT@UTC+8", "USDCUSDT", miniTickerBody("USDCUSDT", "0.9998")),
			want: Push{
				Channel:    "spot@public.miniTicker.v3.api.pb@USDCUSDT@UTC+8",
				Symbol:     "USDCUSDT",
				MiniTicker: &MiniTicker{Symbol: "USDCUSDT", Price: "0.9998"},
			},
		},
		{
			// Another body, such as a deals batch, leaves MiniTicker nil
			// rather than being mistaken for a price.
			name: "frame with another body has no mini ticker",
			frame: appendBytes(
				appendString(nil, 1, "spot@public.aggre.deals.v3.api.pb@100ms@USDCUSDT"),
				314, appendString(nil, 2, "spot@public.aggre.deals.v3.api.pb"),
			),
			want: Push{Channel: "spot@public.aggre.deals.v3.api.pb@100ms@USDCUSDT"},
		},
		{
			name:  "empty frame",
			frame: nil,
			want:  Push{},
		},
		{
			name:        "truncated field",
			frame:       []byte{0x0a, 0x10, 'a', 'b'},
			errContains: "decoding push frame: field 1",
		},
		{
			name:        "channel with the wrong wire type",
			frame:       protowire.AppendVarint(protowire.AppendTag(nil, 1, protowire.VarintType), 7),
			errContains: "field 1 has wire type 0, expected 2",
		},
		{
			name:        "channel that is not UTF-8",
			frame:       appendBytes(nil, 1, []byte{0xff, 0xfe}),
			errContains: "string is not valid UTF-8",
		},
		{
			name:        "malformed mini ticker body",
			frame:       appendBytes(nil, 309, []byte{0x0a, 0x10, 'a'}),
			errContains: "decoding mini ticker: field 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			push, err := DecodePush(tt.frame)
			if tt.errContains != "" {
				require.ErrorContains(t, err, tt.errContains)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, push)
		})
	}
}

// pushFrame encodes a PushDataV3ApiWrapper with a sendTime the decoder
// skips.
func pushFrame(channel, symbol string, miniTicker []byte) []byte {
	b := appendString(nil, 1, channel)
	b = appendString(b, 3, symbol)
	b = protowire.AppendVarint(protowire.AppendTag(b, 6, protowire.VarintType), 1758685000000)
	return appendBytes(b, 309, miniTicker)
}

// miniTickerBody encodes a PublicMiniTickerV3Api with a rate the decoder
// skips.
func miniTickerBody(symbol, price string) []byte {
	b := appendString(nil, 1, symbol)
	b = appendString(b, 2, price)
	return appendString(b, 3, "0.0001")
}

func appendString(b []byte, num protowire.Number, value string) []byte {
	return protowire.AppendString(protowire.AppendTag(b, num, protowire.BytesType), value)
}

func appendBytes(b []byte, num protowire.Number, value []byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(b, num, protowire.BytesType), value)
}
