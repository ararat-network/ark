package chainstate

import (
	"errors"
	"strings"
	"time"
)

// Config controls how the chainstate client queries the feed registry from the
// chain's oracle query service.
type Config struct {
	// Address is the oracle query service endpoint.
	Address string `json:"address"`

	// Timeout caps each feed query.
	Timeout time.Duration `json:"timeout"`

	// Interval controls the steady-state poll cadence and retry delay.
	Interval time.Duration `json:"interval"`
}

// Equal reports whether two configs would produce the same polling behaviour.
func (c Config) Equal(b Config) bool {
	return c.Address == b.Address && c.Timeout == b.Timeout && c.Interval == b.Interval
}

// Validate checks that the client has an endpoint and positive timing values.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Address) == "" {
		return errors.New("feed query address cannot be empty")
	}
	if c.Timeout <= 0 {
		return errors.New("feed query timeout must be greater than 0")
	}
	if c.Interval <= 0 {
		return errors.New("feed poll interval must be greater than 0")
	}

	return nil
}
