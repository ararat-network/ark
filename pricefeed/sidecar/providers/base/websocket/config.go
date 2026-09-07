package websocket

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
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

	// DefaultMaxTickersPerConnection disables connection sharding by default, so
	// one connection handles all tickers.
	DefaultMaxTickersPerConnection = 0

	// DefaultMaxSubscriptionsPerBatch is the default provider-handler limit for
	// subscriptions in one subscription message.
	DefaultMaxSubscriptionsPerBatch = 1
)

// Config defines websocket fetcher settings for one provider.
//
// Config values are treated as immutable after being passed to NewFetcher.
// Build a replacement config instead of mutating Endpoints in place.
type Config struct {
	// Name is the fetcher name used in logs and metrics.
	Name string `mapstructure:"name"`

	// MaxBufferSize is the provider response channel capacity.
	MaxBufferSize int `mapstructure:"max_buffer_size"`

	// ReconnectionTimeout is the required positive delay before reconnecting after
	// a connection session fails.
	ReconnectionTimeout time.Duration `mapstructure:"reconnection_timeout"`

	// PostConnectionTimeout is the delay after dial before sending subscription
	// messages.
	PostConnectionTimeout time.Duration `mapstructure:"post_connection_timeout"`

	// Endpoints are provider endpoints that may be selected for each connection
	// attempt.
	Endpoints []types.Endpoint `mapstructure:"endpoints"`

	// HandshakeTimeout is the maximum duration allowed for websocket dial and
	// handshake.
	HandshakeTimeout time.Duration `mapstructure:"handshake_timeout"`

	// EnableCompression asks the websocket client to negotiate per-message
	// compression. The server may still decline compression.
	EnableCompression bool `mapstructure:"enable_compression"`

	// ReadTimeout is the timeout applied to each websocket read.
	ReadTimeout time.Duration `mapstructure:"read_timeout"`

	// WriteTimeout is the required positive timeout applied to each websocket write.
	WriteTimeout time.Duration `mapstructure:"write_timeout"`

	// PingInterval is the heartbeat interval. If zero, heartbeat messages are not
	// sent.
	PingInterval time.Duration `mapstructure:"ping_interval"`

	// WriteInterval is the delay between consecutive subscription writes.
	WriteInterval time.Duration `mapstructure:"write_interval"`

	// MaxTickersPerConnection is the maximum number of tickers assigned to
	// one websocket connection. If zero, all tickers share one connection.
	MaxTickersPerConnection int `mapstructure:"max_tickers_per_connection"`

	// MaxSubscriptionsPerBatch is a provider-handler setting for the maximum
	// number of subscriptions included in one subscription message.
	MaxSubscriptionsPerBatch int `mapstructure:"max_subscriptions_per_batch"`
}

// Validate performs validation of the websocket config.
func (c *Config) Validate() error {
	if c.MaxBufferSize <= 0 {
		return errors.New("websocket max buffer size must be greater than 0")
	}

	if c.ReconnectionTimeout <= 0 {
		return errors.New("websocket reconnection timeout must be greater than 0")
	}

	if c.PostConnectionTimeout < 0 {
		return errors.New("websocket post connection timeout cannot be negative")
	}

	if len(c.Endpoints) == 0 {
		return errors.New("websocket endpoints cannot be empty")
	}

	for i, e := range c.Endpoints {
		if err := e.Validate(); err != nil {
			return fmt.Errorf("endpoint %d: %w", i, err)
		}
	}

	if len(c.Name) == 0 {
		return errors.New("websocket name cannot be empty")
	}

	if c.HandshakeTimeout <= 0 {
		return errors.New("websocket handshake timeout must be greater than 0")
	}

	if c.ReadTimeout <= 0 {
		return errors.New("websocket read timeout must be greater than 0")
	}

	if c.WriteTimeout <= 0 {
		return errors.New("websocket write timeout must be greater than 0")
	}

	if c.PingInterval < 0 {
		return errors.New("websocket ping interval cannot be negative")
	}

	if c.WriteInterval < 0 {
		return errors.New("websocket write interval cannot be negative")
	}

	if c.MaxTickersPerConnection < 0 {
		return errors.New("websocket max tickers per connection cannot be negative")
	}

	if c.MaxSubscriptionsPerBatch <= 0 {
		return errors.New("websocket max subscriptions per batch must be greater than 0")
	}

	return nil
}

// Equal reports whether two websocket configs are equivalent.
func (c Config) Equal(other Config) bool {
	return c.Name == other.Name &&
		c.MaxBufferSize == other.MaxBufferSize &&
		c.ReconnectionTimeout == other.ReconnectionTimeout &&
		c.PostConnectionTimeout == other.PostConnectionTimeout &&
		slices.Equal(c.Endpoints, other.Endpoints) &&
		c.HandshakeTimeout == other.HandshakeTimeout &&
		c.EnableCompression == other.EnableCompression &&
		c.ReadTimeout == other.ReadTimeout &&
		c.WriteTimeout == other.WriteTimeout &&
		c.PingInterval == other.PingInterval &&
		c.WriteInterval == other.WriteInterval &&
		c.MaxTickersPerConnection == other.MaxTickersPerConnection &&
		c.MaxSubscriptionsPerBatch == other.MaxSubscriptionsPerBatch
}
