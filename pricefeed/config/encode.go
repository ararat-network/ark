package config

import (
	"bytes"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"text/template"
	"time"
	"unicode"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base"
	"github.com/ararat-network/ark/pricefeed/sidecar/runtime"
)

// Encode renders cfg as the TOML file Load reads. Durations render as
// strings, a provider carries only the transport table it uses, and maps
// render in key order so two encodings of one config are byte-identical.
func Encode(cfg runtime.Config) ([]byte, error) {
	var buf bytes.Buffer
	if err := configTemplate.Execute(&buf, cfg); err != nil {
		return nil, fmt.Errorf("encoding oracle config: %w", err)
	}

	return buf.Bytes(), nil
}

var configTemplate = template.Must(template.New("pricefeed.toml").Funcs(template.FuncMap{
	"str":         tomlString,
	"strs":        tomlStrings,
	"dur":         func(d time.Duration) string { return tomlString(d.String()) },
	"key":         tomlKey,
	"isAPI":       func(t base.TransportType) bool { return t == base.API },
	"isWebSocket": func(t base.TransportType) bool { return t == base.WebSocket },
}).Parse(configTemplateText))

// Top-level keys precede every table; a provider's transport table follows
// its own table, and the resolver's arrays of tables come last.
const configTemplateText = `# Cadence for resolving cached provider prices into a public snapshot.
update_interval = {{ dur .UpdateInterval }}

# Feeds served until the first on-chain feed registry snapshot arrives.
fallback_feeds = {{ strs .FallbackFeeds }}

# Chain node to read the feed registry from.
[client]
address = {{ str .Client.Address }}
timeout = {{ dur .Client.Timeout }}
interval = {{ dur .Client.Interval }}
{{ range $name, $p := .Providers }}
# The table name, name, and the transport's name must agree.
[providers.{{ key $name }}]
name = {{ str $p.Name }}
transport_type = {{ str $p.TransportType }}
# Oldest cached price still served from this provider.
max_price_age = {{ dur $p.MaxPriceAge }}
# How long unchanged results keep a price alive. Zero: they never do.
max_unchanged_age = {{ dur $p.MaxUnchangedAge }}
# Canonical pair to provider symbol.
markets = [
{{- range $p.Markets }}
  { pair = {{ str .Pair }}, symbol = {{ str .Symbol }} },
{{- end }}
]
{{- if isAPI $p.TransportType }}

[providers.{{ key $name }}.api]
name = {{ str $p.API.Name }}
timeout = {{ dur $p.API.Timeout }}
interval = {{ dur $p.API.Interval }}
# Zero: unlimited.
requests_per_second = {{ $p.API.RequestsPerSecond }}
# Tickers per request. Zero: one request per provider-defined group.
batch_size = {{ $p.API.BatchSize }}
# Longest an on-chain source may repeat a block height. Zero: unchecked.
max_block_height_age = {{ dur $p.API.MaxBlockHeightAge }}
{{- range $p.API.Endpoints }}

[[providers.{{ key $name }}.api.endpoints]]
url = {{ str .URL }}
authentication = { api_key = {{ str .Authentication.APIKey }}, api_key_header = {{ str .Authentication.APIKeyHeader }} }
{{- end }}
{{- end }}
{{- if isWebSocket $p.TransportType }}

[providers.{{ key $name }}.websocket]
name = {{ str $p.WebSocket.Name }}
max_buffer_size = {{ $p.WebSocket.MaxBufferSize }}
reconnection_timeout = {{ dur $p.WebSocket.ReconnectionTimeout }}
post_connection_timeout = {{ dur $p.WebSocket.PostConnectionTimeout }}
handshake_timeout = {{ dur $p.WebSocket.HandshakeTimeout }}
enable_compression = {{ $p.WebSocket.EnableCompression }}
read_timeout = {{ dur $p.WebSocket.ReadTimeout }}
write_timeout = {{ dur $p.WebSocket.WriteTimeout }}
# Heartbeat cadence. Zero: no heartbeat.
ping_interval = {{ dur $p.WebSocket.PingInterval }}
write_interval = {{ dur $p.WebSocket.WriteInterval }}
# Zero: all tickers share one connection.
max_tickers_per_connection = {{ $p.WebSocket.MaxTickersPerConnection }}
max_subscriptions_per_batch = {{ $p.WebSocket.MaxSubscriptionsPerBatch }}
{{- range $p.WebSocket.Endpoints }}

[[providers.{{ key $name }}.websocket.endpoints]]
url = {{ str .URL }}
authentication = { api_key = {{ str .Authentication.APIKey }}, api_key_header = {{ str .Authentication.APIKeyHeader }} }
{{- end }}
{{- end }}
{{ end }}
{{- if .Resolver.BootstrapPrices }}
# Last-resort prices for one route leg each, in the leg's orientation, used
# only while no provider observes the leg and never past valid_until.
{{- range .Resolver.BootstrapPrices }}
[[resolver.bootstrap_prices]]
pair = {{ str .Pair }}
price = {{ str .Price }}
valid_until = {{ str .ValidUntil }}
{{ end }}
{{- end }}
{{- if .Resolver.Routes }}
# A feed denom's paths to NOAH; a route's legs multiply, and a leg accepts a
# provider observation in either orientation. A denom with no route resolves
# through the direct UNIT/NOAH pair.
{{- range $denom, $routes := .Resolver.Routes }}
{{- range $routes }}
[[resolver.routes.{{ key $denom }}]]
name = {{ str .Name }}
pairs = {{ strs .Pairs }}
{{ end }}
{{- end }}
{{- end }}`

var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// tomlKey renders a map key as a table-header segment: bare when TOML allows
// it, quoted otherwise.
func tomlKey(v any) string {
	s := stringOf(v)
	if bareKey.MatchString(s) {
		return s
	}

	return tomlString(s)
}

// tomlStrings renders a slice of string-kinded values as a TOML array.
func tomlStrings(v any) string {
	rv := reflect.ValueOf(v)
	parts := make([]string, 0, rv.Len())
	for i := range rv.Len() {
		parts = append(parts, tomlString(rv.Index(i).Interface()))
	}

	return "[" + strings.Join(parts, ", ") + "]"
}

// tomlString renders a string-kinded value as a TOML basic string.
func tomlString(v any) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range stringOf(v) {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if unicode.IsControl(r) {
				fmt.Fprintf(&b, `\u%04X`, r)
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')

	return b.String()
}

func stringOf(v any) string {
	if rv := reflect.ValueOf(v); rv.Kind() == reflect.String {
		return rv.String()
	}

	return fmt.Sprint(v)
}
