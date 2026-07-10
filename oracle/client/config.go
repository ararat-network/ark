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
	DefaultOracleEnabled  = false
	DefaultOracleAddress  = "localhost:8080"
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
	// oracle sidecar and exposes instrumentation for the oracle client
	// and the interaction between the oracle and the app.
	DefaultConfigTemplate = `

###############################################################################
###                                  Oracle                                 ###
###############################################################################
[oracle]
# Enabled indicates whether the oracle is enabled.
enabled = "{{ .Oracle.Enabled }}"

# Oracle Address is the URL of the out of process oracle sidecar. This is used to
# connect to the oracle sidecar when the application boots up. Note that the address
# can be modified at any point, but will only take effect after the application is
# restarted. This can be the address of an oracle container running on the same
# machine or a remote machine.
oracle_address = "{{ .Oracle.OracleAddress }}"

# Client Timeout is the time that the client is willing to wait for responses from
# the oracle before timing out. The recommended timeout is 3 seconds (3000ms).
client_timeout = "{{ .Oracle.ClientTimeout }}"

# MetricsEnabled determines whether oracle metrics are enabled. Specifically
# this enables instrumentation of the oracle client and the interaction between
# the oracle and the app.
metrics_enabled = "{{ .Oracle.MetricsEnabled }}"

# PriceTTL is the maximum age of the sidecar snapshot timestamp before it is considered stale.
# The recommended max age is 10 seconds (10s). If this is greater than 1 minute (1m), the app
# will not start.
price_ttl = "{{ .Oracle.PriceTTL }}"

# Interval is the time between each price update request. The recommended interval
# is the block time of the chain. Otherwise, 1.5 seconds (1500ms) is a good default. If this
# is greater than 1 minute (1m), the app will not start.
interval = "{{ .Oracle.Interval }}"
`
)

// NewDefaultConfig returns a default application side oracle configuration.
func NewDefaultConfig() Config {
	return Config{
		Enabled:        DefaultOracleEnabled,
		OracleAddress:  DefaultOracleAddress,
		ClientTimeout:  DefaultClientTimeout,
		MetricsEnabled: DefaultMetricsEnabled,
		PriceTTL:       DefaultPriceTTL,
		Interval:       DefaultInterval,
	}
}

const (
	flagEnabled        = "oracle.enabled"
	flagOracleAddress  = "oracle.oracle_address"
	flagClientTimeout  = "oracle.client_timeout"
	flagMetricsEnabled = "oracle.metrics_enabled"
	flagPriceTTL       = "oracle.price_ttl"
	flagInterval       = "oracle.interval"
)

// Config contains the application side oracle configurations that must
// be set in the app.toml file.
type Config struct {
	// Enabled indicates whether the oracle is enabled.
	Enabled bool `mapstructure:"enabled" toml:"enabled"`

	// OracleAddress is the URL of the out of process oracle sidecar. This is
	// used to connect to the oracle sidecar.
	OracleAddress string `mapstructure:"oracle_address" toml:"oracle_address"`

	// ClientTimeout is the time that the client is willing to wait for responses
	// from the oracle before timing out.
	ClientTimeout time.Duration `mapstructure:"client_timeout" toml:"client_timeout"`

	// MetricsEnabled is a flag that determines whether oracle metrics are enabled.
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
	if strings.TrimSpace(c.OracleAddress) == "" {
		return errors.New("poorly formatted app.toml (oracle subsection): oracle address must not be empty")
	}

	if c.ClientTimeout <= 0 {
		return errors.New("poorly formatted app.toml (oracle subsection): oracle client timeout must be greater than 0")
	}

	if c.PriceTTL <= 0 || c.PriceTTL > MaxPriceTTL {
		return fmt.Errorf("poorly formatted app.toml (oracle subsection): oracle price time to live (price_ttl) must be between 0 and %s", MaxPriceTTL)
	}

	if c.Interval <= 0 || c.Interval > MaxInterval {
		return fmt.Errorf("poorly formatted app.toml (oracle subsection): oracle interval must be between 0 and %s", MaxInterval)
	}

	if c.Interval >= c.PriceTTL {
		return errors.New("poorly formatted app.toml (oracle subsection): oracle interval must be strictly less than max age")
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

	if v := opts.Get(flagOracleAddress); v != nil {
		address, err := cast.ToStringE(v)
		if err != nil || strings.TrimSpace(address) == "" {
			return cfg, errors.New("oracle address must be a non-empty string")
		}
		cfg.OracleAddress = address
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
	return fmt.Sprintf(`Oracle Config:
  Enabled: %v
  Oracle Address: %s
  Client Timeout: %s
  Metrics Enabled: %v
  Price TTL: %s
  Interval: %s`,
		c.Enabled, c.OracleAddress, c.ClientTimeout, c.MetricsEnabled, c.PriceTTL, c.Interval)
}
