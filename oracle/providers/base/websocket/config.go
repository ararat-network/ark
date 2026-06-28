package websocket

import (
	"fmt"
	"time"

	"noah/oracle/providers/types"
)

const (
	// DefaultMaxBufferSize is the default response channel capacity for websocket
	// provider responses.
	DefaultMaxBufferSize = 1024

	// DefaultReconnectionTimeout is the default delay between websocket reconnect
	// attempts.
	DefaultReconnectionTimeout = 10 * time.Second

	// DefaultPostConnectionTimeout is the default delay after dial before sending
	// subscription messages.
	DefaultPostConnectionTimeout = 1 * time.Second

	// DefaultHandshakeTimeout is the default duration allowed for websocket dial
	// and handshake.
	DefaultHandshakeTimeout = 10 * time.Second

	// DefaultEnableCompression disables websocket per-message compression.
	DefaultEnableCompression = false

	// DefaultReadTimeout is the default timeout applied to each websocket read.
	DefaultReadTimeout = 10 * time.Second

	// DefaultWriteTimeout is the default timeout applied to each websocket write.
	DefaultWriteTimeout = 5 * time.Second

	// DefaultPingInterval disables heartbeat messages by default.
	DefaultPingInterval = 0 * time.Second

	// DefaultWriteInterval is the default delay between consecutive websocket
	// writes in a subscription batch.
	DefaultWriteInterval = 100 * time.Millisecond

	// DefaultMaxReadErrorCount is the default number of consecutive read errors
	// tolerated before the connection is treated as failed.
	DefaultMaxReadErrorCount = 100

	// DefaultMaxTickersPerConnection disables connection sharding by default, so
	// one connection handles all tickers.
	DefaultMaxTickersPerConnection = 0

	// DefaultMaxSubscriptionsPerBatch is the default provider-handler limit for
	// subscriptions in one subscription message.
	DefaultMaxSubscriptionsPerBatch = 1
)

// Config defines websocket fetcher settings for one provider.
type Config struct {
	// Name is the fetcher name used in logs and metrics.
	Name string `json:"name"`

	// MaxBufferSize is the provider response channel capacity.
	MaxBufferSize int `json:"maxBufferSize"`

	// ReconnectionTimeout is the delay before reconnecting after a connection
	// session fails. If zero, reconnects are immediate.
	ReconnectionTimeout time.Duration `json:"reconnectionTimeout"`

	// PostConnectionTimeout is the delay after dial before sending subscription
	// messages.
	PostConnectionTimeout time.Duration `json:"postConnectionTimeout"`

	// Endpoints are provider endpoints that may be selected for each connection
	// attempt.
	Endpoints []types.Endpoint `json:"endpoints"`

	// HandshakeTimeout is the maximum duration allowed for websocket dial and
	// handshake. If zero, only the parent context deadline applies.
	HandshakeTimeout time.Duration `json:"handshakeTimeout"`

	// EnableCompression asks the websocket client to negotiate per-message
	// compression. The server may still decline compression.
	EnableCompression bool `json:"enableCompression"`

	// ReadTimeout is the timeout applied to each websocket read. If zero, only
	// the parent context deadline applies.
	ReadTimeout time.Duration `json:"readTimeout"`

	// WriteTimeout is the timeout applied to each websocket write. If zero, only
	// the parent context deadline applies.
	WriteTimeout time.Duration `json:"writeTimeout"`

	// PingInterval is the heartbeat interval. If zero, heartbeat messages are not
	// sent.
	PingInterval time.Duration `json:"pingInterval"`

	// WriteInterval is the delay between consecutive subscription writes.
	WriteInterval time.Duration `json:"writeInterval"`

	// MaxReadErrorCount is the number of consecutive read errors tolerated before
	// the connection is treated as failed.
	MaxReadErrorCount int `json:"maxReadErrorCount"`

	// MaxTickersPerConnection is the maximum number of tickers assigned to
	// one websocket connection. If zero, all tickers share one connection.
	MaxTickersPerConnection int `json:"maxTickersPerConnection"`

	// MaxSubscriptionsPerBatch is a provider-handler setting for the maximum
	// number of subscriptions included in one subscription message.
	MaxSubscriptionsPerBatch int `json:"maxSubscriptionsPerBatch"`
}

// Validate performs validation of the websocket config.
func (c *Config) Validate() error {
	if c.MaxBufferSize < 1 {
		return fmt.Errorf("websocket max buffer size must be greater than 0")
	}

	if c.ReconnectionTimeout < 0 {
		return fmt.Errorf("websocket reconnection timeout cannot be negative")
	}

	if c.PostConnectionTimeout < 0 {
		return fmt.Errorf("websocket post connection timeout cannot be negative")
	}

	if len(c.Endpoints) == 0 {
		return fmt.Errorf("websocket endpoints cannot be empty")
	}

	for i, e := range c.Endpoints {
		if err := e.Validate(); err != nil {
			return fmt.Errorf("endpoint %d: %w", i, err)
		}
	}

	if len(c.Name) == 0 {
		return fmt.Errorf("websocket name cannot be empty")
	}

	if c.HandshakeTimeout < 0 {
		return fmt.Errorf("websocket handshake timeout cannot be negative")
	}

	if c.ReadTimeout < 0 {
		return fmt.Errorf("websocket read timeout cannot be negative")
	}

	if c.WriteTimeout < 0 {
		return fmt.Errorf("websocket write timeout cannot be negative")
	}

	if c.PingInterval < 0 {
		return fmt.Errorf("websocket ping interval cannot be negative")
	}

	if c.WriteInterval < 0 {
		return fmt.Errorf("websocket write interval cannot be negative")
	}

	if c.MaxReadErrorCount < 0 {
		return fmt.Errorf("websocket max read error count cannot be negative")
	}

	if c.MaxTickersPerConnection < 0 {
		return fmt.Errorf("websocket max tickers per connection cannot be negative")
	}

	// TODO: can we allow 0 value? depends on data handler implementation
	if c.MaxSubscriptionsPerBatch <= 0 {
		return fmt.Errorf("websocket max subscriptions per batch must be greater than 0")
	}

	return nil
}
