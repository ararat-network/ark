package config

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base"
	providertypes "github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/websocket/binance"
	"github.com/ararat-network/ark/pricefeed/sidecar/resolver"
	"github.com/ararat-network/ark/pricefeed/sidecar/runtime"
	sidecartypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

func TestEncodeRoundTripsThroughLoad(t *testing.T) {
	websocketProvider := providers.Config{
		Name:            binance.Name,
		TransportType:   base.WebSocket,
		Markets:         binance.DefaultMarkets,
		MaxPriceAge:     time.Minute,
		MaxUnchangedAge: 2 * time.Minute,
		WebSocket:       binance.DefaultWebSocketConfig,
	}
	websocketProvider.WebSocket.Endpoints = []providertypes.Endpoint{{
		URL: binance.WSS,
		Authentication: providertypes.Authentication{
			APIKey:       `Bearer "quoted"\backslash`,
			APIKeyHeader: "Authorization",
		},
	}}
	withWebSocket := Default()
	withWebSocket.Providers = map[string]providers.Config{binance.Name: websocketProvider}
	withWebSocket.Resolver.Routes = map[string][]resolver.Route{
		"aabc": {
			{Name: "first", Pairs: []sidecartypes.Pair{"ABC/USD", "USD/NOAH"}},
			{Name: "second", Pairs: []sidecartypes.Pair{"ABC/NOAH"}},
		},
	}

	tests := []struct {
		name string
		cfg  runtime.Config
	}{
		{name: "default", cfg: Default()},
		{name: "websocket provider, two routes, escaped strings", cfg: withWebSocket},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bz, err := Encode(tc.cfg)
			require.NoError(t, err)
			again, err := Encode(tc.cfg)
			require.NoError(t, err)
			require.Equal(t, bz, again, "encoding must be deterministic")

			path := filepath.Join(t.TempDir(), "pricefeed.toml")
			require.NoError(t, os.WriteFile(path, bz, 0o600))
			loaded, err := Load(path)
			require.NoError(t, err)

			require.Equal(t, tc.cfg, loaded)
		})
	}
}

func TestEncodeWritesOnlyTheSelectedTransport(t *testing.T) {
	bz, err := Encode(Default())
	require.NoError(t, err)
	text := string(bz)

	require.Contains(t, text, "[providers.frankfurter_api.api]")
	require.NotContains(t, text, ".websocket]")
	require.Contains(t, text, `update_interval = "1.5s"`)
	require.NotContains(t, text, "1500000000")
}

// TestEncodeTemplateNamesEveryKey catches a config field added without a
// line in the template: the round trip above only sees fields Default sets.
func TestEncodeTemplateNamesEveryKey(t *testing.T) {
	seen := map[reflect.Type]bool{}
	var walk func(t *testing.T, typ reflect.Type)
	walk = func(t *testing.T, typ reflect.Type) {
		t.Helper()
		for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Map {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct || seen[typ] {
			return
		}
		seen[typ] = true
		for i := range typ.NumField() {
			field := typ.Field(i)
			tag := field.Tag.Get("mapstructure")
			require.NotEmpty(t, tag, "%s.%s has no mapstructure tag", typ, field.Name)
			// A key is written as `key =`, opens a table `[..key]`, or
			// nests one `key.`.
			pattern := regexp.MustCompile(`(^|[\s.\[])` + regexp.QuoteMeta(tag) + `(\s*=|[\].])`)
			require.True(t, pattern.MatchString(configTemplateText), "%s.%s (%q) is not in the template", typ, field.Name, tag)
			walk(t, field.Type)
		}
	}

	walk(t, reflect.TypeFor[runtime.Config]())
}

func TestTOMLString(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{name: "plain", in: "Bearer YOUR_API_KEY", want: `"Bearer YOUR_API_KEY"`},
		{name: "named string type", in: sidecartypes.Pair("USD/NOAH"), want: `"USD/NOAH"`},
		{name: "quote and backslash", in: `C:\certs\"ca".pem`, want: `"C:\\certs\\\"ca\".pem"`},
		{name: "named escapes", in: "a\tb\nc\rd\be\ff", want: `"a\tb\nc\rd\be\ff"`},
		{name: "other control", in: "a\x01b\x7f", want: `"a\u0001b\u007F"`},
		{name: "unicode passes through", in: "€/₩", want: `"€/₩"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tomlString(tc.in))
		})
	}
}

func TestTOMLKey(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "bare", in: "frankfurter_api", want: "frankfurter_api"},
		{name: "bare with dash", in: "binance-ws", want: "binance-ws"},
		{name: "slash needs quoting", in: "ibc/ABC", want: `"ibc/ABC"`},
		{name: "dot needs quoting", in: "a.b", want: `"a.b"`},
		{name: "empty needs quoting", in: "", want: `""`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tomlKey(tc.in))
		})
	}
}

func TestTOMLStrings(t *testing.T) {
	require.Equal(t, `[]`, tomlStrings([]string(nil)))
	require.Equal(t, `["a", "b\"c"]`, tomlStrings([]string{"a", `b"c`}))
	require.Equal(t, `["KRW/USD", "USD/NOAH"]`, tomlStrings([]sidecartypes.Pair{"KRW/USD", "USD/NOAH"}))
	require.False(t, strings.Contains(tomlStrings([]string{"x"}), "\n"))
}
