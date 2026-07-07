package client

// Option enables consumers to configure a Client during construction.
type Option func(*Client)

// WithBlockingDial makes Start block until the remote oracle connection is ready.
//
// NOTICE: This option is not recommended to be used in practice. See the
// [gRPC docs](https://github.com/grpc/grpc-go/blob/master/Documentation/anti-patterns.md).
func WithBlockingDial() Option {
	return func(c *Client) {
		c.blockingDial = true
	}
}
