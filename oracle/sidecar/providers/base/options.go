package base

import (
	"cosmossdk.io/log/v2"
)

// Option configures a provider during construction.
type Option func(*Provider)

// WithLogger sets the provider logger.
func WithLogger(logger log.Logger) Option {
	return func(p *Provider) {
		p.logger = logger
	}
}
