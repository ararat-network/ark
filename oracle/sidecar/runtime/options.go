package runtime

import (
	"cosmossdk.io/log/v2"

	"noah/oracle/sidecar/providers/base"
)

// Option customizes oracle construction.
type Option func(impl *Runtime)

// WithLogger sets the logger for the oracle.
func WithLogger(logger log.Logger) Option {
	return func(m *Runtime) {
		m.logger = logger
	}
}

// WithLastUpdated sets the last update block height for the oracle.
func WithLastUpdated(lastUpdated uint64) Option {
	return func(m *Runtime) {
		m.lastUpdated = lastUpdated
	}
}

// WithProviders allows pre-instantiated price providers to be used in the Runtime's price fetching loop.
// This option is mainly used for testing, but can be useful for programmatically setting customised providers.
func WithProviders(ps ...*base.Provider) Option {
	return func(m *Runtime) {
		for _, p := range ps {
			m.providers[p.Name()] = p
		}
	}
}

// WithVoteTargetsClient sets the client used to fetch on-chain oracle vote targets.
func WithVoteTargetsClient(client VoteTargetsClient) Option {
	return func(m *Runtime) {
		m.voteTargetsClient = client
	}
}

// WithResolver sets the price resolver used by the oracle.
func WithResolver(resolver PriceResolver) Option {
	return func(m *Runtime) {
		m.resolver = resolver
	}
}
