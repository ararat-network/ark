package runtime

import (
	"errors"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base"
	providertypes "github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

// Option customises oracle construction.
type Option func(impl *Runtime)

// WithLogger sets the logger for the oracle.
func WithLogger(logger log.Logger) Option {
	return func(r *Runtime) {
		r.logger = logger
	}
}

// WithProviderFactory replaces provider construction while preserving
// configuration-owned provider membership.
func WithProviderFactory(factory func(
	providers.Config,
	providertypes.Markets,
) (*base.Provider, error),
) Option {
	return func(r *Runtime) {
		r.providerFactory = factory
	}
}

// WithProviderRegistry builds providers through reg instead of the default
// registry, preserving configuration-owned provider membership.
func WithProviderRegistry(reg *providers.Registry) Option {
	return func(r *Runtime) {
		r.providerFactory = func(cfg providers.Config, markets providertypes.Markets) (*base.Provider, error) {
			if reg == nil {
				return nil, errors.New("provider registry is nil")
			}
			return reg.NewProvider(cfg, markets, r.logger)
		}
	}
}

// WithChainStateClient sets the client used to fetch the on-chain oracle feed set.
func WithChainStateClient(client ChainStateClient) Option {
	return func(r *Runtime) {
		r.client = client
	}
}
