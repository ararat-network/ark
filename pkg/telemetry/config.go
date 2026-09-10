package telemetry

import (
	"fmt"

	"github.com/ararat-network/ark/pkg/grpcconn"
)

const (
	DefaultPrometheusEnabled = false
	DefaultPrometheusAddress = "localhost:9464"
)

// PrometheusConfigTemplate configures the application scrape endpoint in app.toml. It is separate
// from CometBFT instrumentation; the SDK telemetry bridge exports here when metrics-sink is otel.
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

// PrometheusConfig is the [prometheus] section of app.toml; the tags are its
// keys.
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
	if _, _, err := grpcconn.ListenAddress(c.Address); err != nil {
		return fmt.Errorf("address must be host:port: %w", err)
	}
	return nil
}
