package chainstate

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ararat-network/ark/pkg/grpcconn"
	"github.com/ararat-network/ark/pkg/tlsconfig"
)

// MaxAddresses bounds the failover sweep: every address is tried under
// Timeout, so a sweep costs up to len × Timeout.
const MaxAddresses = 4

// Config controls how the chainstate client queries the feed registry from the
// chain's oracle query service.
type Config struct {
	// Addresses are the oracle query service endpoints in preference order;
	// the order is the failover order.
	Addresses []string `mapstructure:"addresses"`

	// Timeout caps each feed query.
	Timeout time.Duration `mapstructure:"timeout"`

	// Interval controls the steady-state poll cadence and retry delay.
	Interval time.Duration `mapstructure:"interval"`

	// TLS is what every query connection dials with. The node's own gRPC port
	// is plaintext, so a CA here names a TLS terminator in front of it.
	TLS tlsconfig.Client `mapstructure:"tls"`
}

// Equal reports whether two configs would produce the same polling behaviour.
func (c Config) Equal(b Config) bool {
	return slices.Equal(c.Addresses, b.Addresses) &&
		c.Timeout == b.Timeout &&
		c.Interval == b.Interval &&
		c.TLS == b.TLS
}

// Validate checks that the client has at least one endpoint and positive
// timing values.
func (c Config) Validate() error {
	if len(c.Addresses) == 0 {
		return errors.New("feed query addresses must list at least one address")
	}
	if len(c.Addresses) > MaxAddresses {
		return fmt.Errorf(
			"feed query addresses lists %d addresses; at most %d",
			len(c.Addresses),
			MaxAddresses,
		)
	}
	seen := make(map[string]struct{}, len(c.Addresses))
	for i, address := range c.Addresses {
		if strings.TrimSpace(address) == "" {
			return fmt.Errorf("feed query addresses[%d] must not be empty", i)
		}
		if _, dup := seen[address]; dup {
			return fmt.Errorf("feed query addresses[%d] repeats %q", i, address)
		}
		seen[address] = struct{}{}
	}
	if c.Timeout <= 0 {
		return errors.New("feed query timeout must be greater than 0")
	}
	if c.Interval <= 0 {
		return errors.New("feed poll interval must be greater than 0")
	}
	if err := c.TLS.Validate(); err != nil {
		return fmt.Errorf("feed query tls: %w", err)
	}

	return grpcconn.ValidateTargets(c.TLS.Mode, c.Addresses...)
}
