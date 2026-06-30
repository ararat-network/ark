package oracle

import (
	"cosmossdk.io/log/v2"

	"noah/oracle/providers/base"
)

// Option customizes oracle construction.
type Option func(impl *Oracle)

// WithLogger sets the logger for the oracle.
func WithLogger(logger log.Logger) Option {
	return func(m *Oracle) {
		m.logger = logger
	}
}

// WithLastUpdated sets the last update block height for the oracle.
func WithLastUpdated(lastUpdated uint64) Option {
	return func(m *Oracle) {
		m.lastUpdated = lastUpdated
	}
}

// WithProviders allows pre-instantiated price providers to be used in the Oracle's price fetching loop.
// This option is mainly used for testing, but can be useful for programmatically setting customised providers.
func WithProviders(ps ...*base.Provider) Option {
	return func(m *Oracle) {
		for _, p := range ps {
			m.providers[p.Name()] = p
		}
	}
}
