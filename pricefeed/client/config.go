package client

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cast"

	servertypes "github.com/cosmos/cosmos-sdk/server/types"
)

const (
	DefaultEnabled        = false
	DefaultSidecarAddress = "localhost:8080"
	DefaultClientTimeout  = 3 * time.Second
	DefaultPriceTTL       = 10 * time.Second
	DefaultInterval       = 1500 * time.Millisecond
	MaxInterval           = 1 * time.Minute
	MaxPriceTTL           = 1 * time.Minute
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

# Sidecar Address is the URL of the out-of-process price-feed sidecar. This is
# used to connect to the sidecar when the application boots up. Note that the
# address can be modified at any point, but will only take effect after the
# application is restarted. This can be the address of a sidecar container
# running on the same machine or a remote machine.
sidecar_address = "{{ .PriceFeed.SidecarAddress }}"

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

`
)

// NewDefaultConfig returns a default application side price-feed configuration.
func NewDefaultConfig() Config {
	return Config{
		Enabled:        DefaultEnabled,
		SidecarAddress: DefaultSidecarAddress,
		ClientTimeout:  DefaultClientTimeout,
		PriceTTL:       DefaultPriceTTL,
		Interval:       DefaultInterval,
	}
}

// Viper keys for the [pricefeed] section of app.toml. Not CLI flags —
// nothing registers a pflag for these.
const (
	keyEnabled        = "pricefeed.enabled"
	keySidecarAddress = "pricefeed.sidecar_address"
	keyClientTimeout  = "pricefeed.client_timeout"
	keyPriceTTL       = "pricefeed.price_ttl"
	keyInterval       = "pricefeed.interval"
)

// Config contains the application side price-feed configurations that must
// be set in the app.toml file.
type Config struct {
	// Enabled indicates whether the price-feed client is enabled.
	Enabled bool `mapstructure:"enabled" toml:"enabled"`

	// SidecarAddress is the URL of the out-of-process price-feed sidecar.
	SidecarAddress string `mapstructure:"sidecar_address" toml:"sidecar_address"`

	// ClientTimeout is the time that the client is willing to wait for responses
	// from the sidecar before timing out.
	ClientTimeout time.Duration `mapstructure:"client_timeout" toml:"client_timeout"`

	// PriceTTL is the maximum accepted age of the sidecar snapshot timestamp.
	PriceTTL time.Duration `mapstructure:"price_ttl" toml:"price_ttl"`

	// Interval is the time between each price update request.
	Interval time.Duration `mapstructure:"interval" toml:"interval"`
}

// Validate checks whether the client runtime fields are safe to use. Enabled
// controls whether app wiring constructs the client; it does not relax the
// runtime invariants.
func (c Config) Validate() error {
	if strings.TrimSpace(c.SidecarAddress) == "" {
		return errors.New("sidecar address must not be empty")
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

	return nil
}

// ReadConfigFromAppOpts reads the [pricefeed] keys from the app options,
// defaults for absent ones. It decodes and nothing more: the start command
// validates app.toml as a whole before the app exists, and NewClient checks
// the fields again for the commands that build the app without starting it.
func ReadConfigFromAppOpts(opts servertypes.AppOptions) (Config, error) {
	cfg := NewDefaultConfig()
	if err := errors.Join(
		read(opts, keyEnabled, cast.ToBoolE, &cfg.Enabled),
		read(opts, keySidecarAddress, cast.ToStringE, &cfg.SidecarAddress),
		read(opts, keyClientTimeout, cast.ToDurationE, &cfg.ClientTimeout),
		read(opts, keyPriceTTL, cast.ToDurationE, &cfg.PriceTTL),
		read(opts, keyInterval, cast.ToDurationE, &cfg.Interval),
	); err != nil {
		return Config{}, err
	}
	return cfg, nil
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
