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
	DefaultMetricsEnabled = false
	DefaultPriceTTL       = 10 * time.Second
	DefaultInterval       = 1500 * time.Millisecond
	MaxInterval           = 1 * time.Minute
	MaxPriceTTL           = 1 * time.Minute
)

const (
	// DefaultConfigTemplate should be utilised in the app.toml file.
	// This template configures the application to connect to the
	// price-feed sidecar and exposes instrumentation for the client
	// and the interaction between the sidecar and the app.
	DefaultConfigTemplate = `

###############################################################################
###                               Price Feed                                ###
###############################################################################
[pricefeed]
# Enabled indicates whether the price-feed client is enabled.
enabled = "{{ .PriceFeed.Enabled }}"

# Sidecar Address is the URL of the out-of-process price-feed sidecar. This is
# used to connect to the sidecar when the application boots up. Note that the
# address can be modified at any point, but will only take effect after the
# application is restarted. This can be the address of a sidecar container
# running on the same machine or a remote machine.
sidecar_address = "{{ .PriceFeed.SidecarAddress }}"

# Client Timeout is the time that the client is willing to wait for responses from
# the sidecar before timing out. The recommended timeout is 3 seconds (3000ms).
client_timeout = "{{ .PriceFeed.ClientTimeout }}"

# MetricsEnabled determines whether price-feed metrics are enabled. Specifically
# this enables instrumentation of the client and the interaction between
# the sidecar and the app.
metrics_enabled = "{{ .PriceFeed.MetricsEnabled }}"

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
		MetricsEnabled: DefaultMetricsEnabled,
		PriceTTL:       DefaultPriceTTL,
		Interval:       DefaultInterval,
	}
}

const (
	flagEnabled        = "pricefeed.enabled"
	flagSidecarAddress = "pricefeed.sidecar_address"
	flagClientTimeout  = "pricefeed.client_timeout"
	flagMetricsEnabled = "pricefeed.metrics_enabled"
	flagPriceTTL       = "pricefeed.price_ttl"
	flagInterval       = "pricefeed.interval"
)

// Config contains the application side price-feed configurations that must
// be set in the app.toml file.
type Config struct {
	// Enabled indicates whether the price-feed client is enabled.
	Enabled bool `mapstructure:"enabled" toml:"enabled"`

	// SidecarAddress is the URL of the out-of-process price-feed sidecar. This
	// is used to connect to the sidecar.
	SidecarAddress string `mapstructure:"sidecar_address" toml:"sidecar_address"`

	// ClientTimeout is the time that the client is willing to wait for responses
	// from the sidecar before timing out.
	ClientTimeout time.Duration `mapstructure:"client_timeout" toml:"client_timeout"`

	// MetricsEnabled is a flag that determines whether price-feed metrics are enabled.
	MetricsEnabled bool `mapstructure:"metrics_enabled" toml:"metrics_enabled"`

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
		return errors.New("poorly formatted app.toml (pricefeed subsection): sidecar address must not be empty")
	}

	if c.ClientTimeout <= 0 {
		return errors.New("poorly formatted app.toml (pricefeed subsection): client timeout must be greater than 0")
	}

	if c.PriceTTL <= 0 || c.PriceTTL > MaxPriceTTL {
		return fmt.Errorf("poorly formatted app.toml (pricefeed subsection): price time to live (price_ttl) must be between 0 and %s", MaxPriceTTL)
	}

	if c.Interval <= 0 || c.Interval > MaxInterval {
		return fmt.Errorf("poorly formatted app.toml (pricefeed subsection): interval must be between 0 and %s", MaxInterval)
	}

	if c.Interval >= c.PriceTTL {
		return errors.New("poorly formatted app.toml (pricefeed subsection): interval must be strictly less than max age")
	}

	return nil
}

// ReadConfigFromAppOpts reads the config parameters from the AppOptions and returns the config.
func ReadConfigFromAppOpts(opts servertypes.AppOptions) (Config, error) {
	cfg := NewDefaultConfig()

	if v := opts.Get(flagEnabled); v != nil {
		enabled, err := cast.ToBoolE(v)
		if err != nil {
			return cfg, err
		}
		cfg.Enabled = enabled
	}

	if !cfg.Enabled {
		return cfg, cfg.Validate()
	}

	if v := opts.Get(flagSidecarAddress); v != nil {
		address, err := cast.ToStringE(v)
		if err != nil || strings.TrimSpace(address) == "" {
			return cfg, errors.New("sidecar address must be a non-empty string")
		}
		cfg.SidecarAddress = address
	}

	if v := opts.Get(flagClientTimeout); v != nil {
		clientTimeout, err := cast.ToDurationE(v)
		if err != nil {
			return cfg, errors.New("client timeout must be a positive duration")
		}
		cfg.ClientTimeout = clientTimeout
	}

	if v := opts.Get(flagMetricsEnabled); v != nil {
		metricsEnabled, err := cast.ToBoolE(v)
		if err != nil {
			return cfg, err
		}
		cfg.MetricsEnabled = metricsEnabled
	}

	if v := opts.Get(flagPriceTTL); v != nil {
		priceTTL, err := cast.ToDurationE(v)
		if err != nil {
			return cfg, errors.New("price ttl must be a positive duration")
		}
		cfg.PriceTTL = priceTTL
	}

	if v := opts.Get(flagInterval); v != nil {
		interval, err := cast.ToDurationE(v)
		if err != nil {
			return cfg, errors.New("interval must be a positive duration")
		}
		cfg.Interval = interval
	}

	if err := cfg.Validate(); err != nil {
		return cfg, err
	}

	return cfg, nil
}

// String implements fmt.Stringer.
func (c Config) String() string {
	return fmt.Sprintf(`Price-feed config:
  Enabled: %v
  Sidecar Address: %s
  Client Timeout: %s
  Metrics Enabled: %v
  Price TTL: %s
  Interval: %s`,
		c.Enabled, c.SidecarAddress, c.ClientTimeout, c.MetricsEnabled, c.PriceTTL, c.Interval)
}
