package telemetry

import (
	"errors"
	"fmt"
	"net"

	"github.com/spf13/cast"

	servertypes "github.com/cosmos/cosmos-sdk/server/types"
)

const (
	DefaultPrometheusEnabled = false
	DefaultPrometheusAddress = "localhost:9464"
)

// PrometheusConfigTemplate is the app.toml section for the application's
// scrape endpoint. Distinct from CometBFT's [instrumentation] endpoint in
// config.toml, which serves consensus metrics, and from the legacy
// [telemetry] section, whose go-metrics bridge lands on this endpoint when
// metrics-sink is "otel".
const PrometheusConfigTemplate = `

###############################################################################
###                               Prometheus                                ###
###############################################################################
[prometheus]
# Enabled serves the application's OpenTelemetry metrics for Prometheus to
# scrape at /metrics on the address below.
enabled = {{ .Prometheus.Enabled }}

# Address is the listen address of the scrape endpoint.
address = "{{ .Prometheus.Address }}"
`

const (
	flagPrometheusEnabled = "prometheus.enabled"
	flagPrometheusAddress = "prometheus.address"
)

// PrometheusConfig is the [prometheus] section of app.toml.
type PrometheusConfig struct {
	// Enabled serves the scrape endpoint.
	Enabled bool `mapstructure:"enabled" toml:"enabled"`

	// Address is the listen address of the scrape endpoint.
	Address string `mapstructure:"address" toml:"address"`
}

// DefaultPrometheusConfig returns the section's defaults: disabled, loopback.
func DefaultPrometheusConfig() PrometheusConfig {
	return PrometheusConfig{
		Enabled: DefaultPrometheusEnabled,
		Address: DefaultPrometheusAddress,
	}
}

// Validate checks the address is a host:port pair a listener can bind.
func (c PrometheusConfig) Validate() error {
	if _, _, err := net.SplitHostPort(c.Address); err != nil {
		return fmt.Errorf("poorly formatted app.toml (prometheus subsection): address must be host:port: %w", err)
	}
	return nil
}

// ReadPrometheusConfig reads the section from the app options, falling back
// to defaults for absent keys.
func ReadPrometheusConfig(opts servertypes.AppOptions) (PrometheusConfig, error) {
	cfg := DefaultPrometheusConfig()

	if v := opts.Get(flagPrometheusEnabled); v != nil {
		enabled, err := cast.ToBoolE(v)
		if err != nil {
			return cfg, fmt.Errorf("prometheus enabled must be a boolean: %w", err)
		}
		cfg.Enabled = enabled
	}

	if v := opts.Get(flagPrometheusAddress); v != nil {
		address, err := cast.ToStringE(v)
		if err != nil {
			return cfg, errors.New("prometheus address must be a string")
		}
		cfg.Address = address
	}

	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}
