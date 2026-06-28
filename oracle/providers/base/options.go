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

// WithDenoms sets the initial chain denoms to resolve into provider tickers during construction.
func WithDenoms(denoms []string) Option {
	return func(p *Provider) {
		p.denoms = append([]string(nil), denoms...)
	}
}
