package client

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cast"

	servertypes "github.com/cosmos/cosmos-sdk/server/types"

	"github.com/ararat-network/ark/pkg/grpcconn"
	"github.com/ararat-network/ark/pkg/tlsconfig"
)

const (
	DefaultEnabled        = false
	DefaultSidecarAddress = "localhost:8080"
	DefaultClientTimeout  = 3 * time.Second
	DefaultPriceTTL       = 10 * time.Second
	DefaultInterval       = 1500 * time.Millisecond
	MaxInterval           = 1 * time.Minute
	MaxPriceTTL           = 1 * time.Minute
	// MaxSidecarAddresses bounds the failover sweep: every address is tried
	// under ClientTimeout, so the sweep costs up to len × ClientTimeout.
	MaxSidecarAddresses = 4
)

const (
	// DefaultConfigTemplate is the [pricefeed] section of app.toml: how the
	// node reaches the price-feed sidecar and how fresh its prices must be.
	DefaultConfigTemplate = `

###############################################################################
###                               Price Feed                                ###
###############################################################################
[pricefeed]
# Enabled indicates whether the price-feed client is enabled.
enabled = {{ .PriceFeed.Enabled }}

# Sidecar Addresses are the out-of-process price-feed sidecars in preference
# order, on the same machine or remote. The client polls the first one that
# answers and stays on it until it fails, then tries the rest in order under
# client_timeout each, so a full sweep can take len × client_timeout. At most
# 4. Read when the application boots; a change takes effect after a restart.
sidecar_addresses = [{{ range $i, $a := .PriceFeed.SidecarAddresses }}{{ if $i }}, {{ end }}{{ printf "%q" $a }}{{ end }}]

# Client Timeout is the time that the client is willing to wait for responses from
# the sidecar before timing out. The recommended timeout is 3 seconds (3000ms).
client_timeout = "{{ .PriceFeed.ClientTimeout }}"

# PriceTTL is the maximum age of the sidecar snapshot timestamp before it is considered stale.
# The recommended max age is 10 seconds (10s). If this is greater than 1 minute (1m), the app
# will not start.
price_ttl = "{{ .PriceFeed.PriceTTL }}"

# Interval is the time between each price update request. The recommended interval
# is the block time of the chain. Otherwise, 1.5 seconds (1500ms) is a good default. If this
# is greater than 1 minute (1m), the app will not start.
interval = "{{ .PriceFeed.Interval }}"

# What every sidecar connection dials with. Local mode permits plaintext only on this host. Remote links require
# mode = "tls" or an explicit mode = "plaintext". Read when the application boots.
[pricefeed.tls]
mode = "{{ if .PriceFeed.TLS.Mode }}{{ .PriceFeed.TLS.Mode }}{{ else }}local{{ end }}"
# In TLS mode, an empty CA file uses system roots; a bundle replaces them.
ca_file = "{{ .PriceFeed.TLS.CAFile }}"

# Cert File and Key File are the client certificate and key presented to a sidecar
# that requires one.
cert_file = "{{ .PriceFeed.TLS.CertFile }}"
key_file = "{{ .PriceFeed.TLS.KeyFile }}"

# Server Name is the name the sidecar's certificate is verified against when it
# carries neither the dialled host nor its IP.
server_name = "{{ .PriceFeed.TLS.ServerName }}"
`
)

// NewDefaultConfig returns a default application side price-feed configuration.
func NewDefaultConfig() Config {
	return Config{
		Enabled:          DefaultEnabled,
		TLS:              tlsconfig.Client{Mode: tlsconfig.Local},
		SidecarAddresses: []string{DefaultSidecarAddress},
		ClientTimeout:    DefaultClientTimeout,
		PriceTTL:         DefaultPriceTTL,
		Interval:         DefaultInterval,
	}
}

// Viper keys for the [pricefeed] section of app.toml. Not CLI flags —
// nothing registers a pflag for these.
const (
	keyEnabled          = "pricefeed.enabled"
	keySidecarAddresses = "pricefeed.sidecar_addresses"
	keyClientTimeout    = "pricefeed.client_timeout"
	keyPriceTTL         = "pricefeed.price_ttl"
	keyInterval         = "pricefeed.interval"
	keyTLSMode          = "pricefeed.tls.mode"
	keyTLSCAFile        = "pricefeed.tls.ca_file"
	keyTLSCertFile      = "pricefeed.tls.cert_file"
	keyTLSKeyFile       = "pricefeed.tls.key_file"
	keyTLSServerName    = "pricefeed.tls.server_name"
)

// keySidecarAddress is the key sidecar_addresses replaced. Refused rather
// than ignored: an unknown key decodes silently to the default address.
const keySidecarAddress = "pricefeed.sidecar_address"

// Config contains the application side price-feed configurations that must
// be set in the app.toml file.
type Config struct {
	// Enabled indicates whether the price-feed client is enabled.
	Enabled bool `mapstructure:"enabled" toml:"enabled"`

	// SidecarAddresses are the out-of-process price-feed sidecars in
	// preference order; the order is the failover order.
	SidecarAddresses []string `mapstructure:"sidecar_addresses" toml:"sidecar_addresses"`

	// ClientTimeout is the time that the client is willing to wait for responses
	// from the sidecar before timing out.
	ClientTimeout time.Duration `mapstructure:"client_timeout" toml:"client_timeout"`

	// PriceTTL is the maximum accepted age of the sidecar snapshot timestamp.
	PriceTTL time.Duration `mapstructure:"price_ttl" toml:"price_ttl"`

	// Interval is the time between each price update request.
	Interval time.Duration `mapstructure:"interval" toml:"interval"`

	// TLS is what every sidecar connection dials with.
	TLS tlsconfig.Client `mapstructure:"tls" toml:"tls"`
}

// Validate checks whether the client runtime fields are safe to use. Enabled
// controls whether app wiring constructs the client; it does not relax the
// runtime invariants.
func (c Config) Validate() error {
	if len(c.SidecarAddresses) == 0 {
		return errors.New("sidecar_addresses must list at least one address")
	}

	if len(c.SidecarAddresses) > MaxSidecarAddresses {
		return fmt.Errorf(
			"sidecar_addresses lists %d addresses; at most %d",
			len(c.SidecarAddresses),
			MaxSidecarAddresses,
		)
	}

	seen := make(map[string]struct{}, len(c.SidecarAddresses))
	for i, address := range c.SidecarAddresses {
		if strings.TrimSpace(address) == "" {
			return fmt.Errorf("sidecar_addresses[%d] must not be empty", i)
		}
		if _, dup := seen[address]; dup {
			return fmt.Errorf("sidecar_addresses[%d] repeats %q", i, address)
		}
		seen[address] = struct{}{}
	}

	if c.ClientTimeout <= 0 {
		return errors.New("client timeout must be greater than 0")
	}

	if c.PriceTTL <= 0 || c.PriceTTL > MaxPriceTTL {
		return fmt.Errorf("price_ttl must be between 0 and %s", MaxPriceTTL)
	}

	if c.Interval <= 0 || c.Interval > MaxInterval {
		return fmt.Errorf("interval must be between 0 and %s", MaxInterval)
	}

	if c.Interval >= c.PriceTTL {
		return errors.New("interval must be strictly less than price_ttl")
	}

	if err := c.TLS.Validate(); err != nil {
		return err
	}
	return grpcconn.ValidateTargets(c.TLS.Mode, c.SidecarAddresses...)
}

// ReadConfigFromAppOpts reads the [pricefeed] keys from the app options,
// defaults for absent ones. It decodes and nothing more: the start command
// validates app.toml as a whole before the app exists, and NewClient checks
// the fields again for the commands that build the app without starting it.
func ReadConfigFromAppOpts(opts servertypes.AppOptions) (Config, error) {
	if opts.Get(keySidecarAddress) != nil {
		return Config{}, fmt.Errorf("%s was renamed to %s and takes a list", keySidecarAddress, keySidecarAddresses)
	}
	cfg := NewDefaultConfig()
	if err := errors.Join(
		read(opts, keyEnabled, cast.ToBoolE, &cfg.Enabled),
		read(opts, keySidecarAddresses, toStringSlice, &cfg.SidecarAddresses),
		read(opts, keyClientTimeout, cast.ToDurationE, &cfg.ClientTimeout),
		read(opts, keyPriceTTL, cast.ToDurationE, &cfg.PriceTTL),
		read(opts, keyInterval, cast.ToDurationE, &cfg.Interval),
		read(opts, keyTLSMode, cast.ToStringE, &cfg.TLS.Mode),
		read(opts, keyTLSCAFile, cast.ToStringE, &cfg.TLS.CAFile),
		read(opts, keyTLSCertFile, cast.ToStringE, &cfg.TLS.CertFile),
		read(opts, keyTLSKeyFile, cast.ToStringE, &cfg.TLS.KeyFile),
		read(opts, keyTLSServerName, cast.ToStringE, &cfg.TLS.ServerName),
	); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// toStringSlice decodes a TOML array as a list and splits a plain string,
// which is what an environment override gives, on commas: the rule viper's
// own decoder applies, so the app sees the list the start command validated.
func toStringSlice(v any) ([]string, error) {
	if s, ok := v.(string); ok {
		if s == "" {
			return []string{}, nil
		}
		return strings.Split(s, ","), nil
	}
	return cast.ToStringSliceE(v)
}

// read sets *dst from the key when it is present.
func read[T any](opts servertypes.AppOptions, key string, conv func(any) (T, error), dst *T) error {
	v := opts.Get(key)
	if v == nil {
		return nil
	}
	parsed, err := conv(v)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	*dst = parsed
	return nil
}
