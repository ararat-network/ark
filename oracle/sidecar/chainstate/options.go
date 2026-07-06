package chainstate

import (
	"google.golang.org/grpc"

	"cosmossdk.io/log/v2"
)

type Option func(*Client)

func WithDialOptions(opts ...grpc.DialOption) Option {
	return func(c *Client) {
		c.dialOptions = append(c.dialOptions, opts...)
	}
}

func WithLogger(logger log.Logger) Option {
	return func(c *Client) {
		c.logger = logger
	}
}
