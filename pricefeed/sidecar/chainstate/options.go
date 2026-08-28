package chainstate

import (
	"cosmossdk.io/log/v2"
)

// Option customizes a Client during construction.
type Option func(*Client)

// WithLogger sets the logger used by the chainstate client.
func WithLogger(logger log.Logger) Option {
	return func(c *Client) {
		c.logger = logger
	}
}
