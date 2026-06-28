package client

// Option enables consumers to configure the behavior of a GRPCClient on initialization.
type Option func(*Client)

// WithBlockingDial configures the GRPCClient to block on dialing the remote oracle server.
//
// NOTICE: This option is not recommended to be used in practice. See the [GRPC docs](https://github.com/grpc/grpc-go/blob/master/Documentation/anti-patterns.md)
func WithBlockingDial() Option {
	return func(c *Client) {
		c.blockingDial = true
	}
}
