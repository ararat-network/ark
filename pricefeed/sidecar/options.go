package sidecar

import (
	"github.com/ararat-network/ark/pricefeed/sidecar/providers"
)

// Option customises service construction.
type Option func(*serviceOptions)

// serviceOptions collects construction inputs that NewService applies before
// building the runtime.
type serviceOptions struct {
	registry *providers.Registry
}

// WithRegistry builds and rebuilds runtime providers through reg instead of
// providers.DefaultRegistry(). Finish registration before calling NewService.
func WithRegistry(reg *providers.Registry) Option {
	return func(o *serviceOptions) {
		o.registry = reg
	}
}
