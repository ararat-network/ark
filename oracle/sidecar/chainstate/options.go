package chainstate

import (
	"google.golang.org/grpc"

	"cosmossdk.io/log/v2"
)

// Option customizes a Client during construction.
type Option func(*Client)

// WithDialOptions appends gRPC dial options to the client's default insecure
// transport credentials.
func WithDialOptions(opts ...grpc.DialOption) Option {
	return func(c *Client) {
		c.dialOptions = append(c.dialOptions, opts...)
	}
}

// WithLogger sets the logger used by the chainstate client.
func WithLogger(logger log.Logger) Option {
	return func(c *Client) {
		c.logger = logger
	}
}
