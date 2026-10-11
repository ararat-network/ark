package websocket_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	. "github.com/ararat-network/ark/pricefeed/sidecar/providers/base/websocket"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "valid"},
		{
			name: "zero optional durations and limits",
			mutate: func(c *Config) {
				c.PostConnectionTimeout, c.PingInterval, c.WriteInterval, c.MaxTickersPerConnection = 0, 0, 0, 0
			},
		},
		{
			name:    "zero max buffer size",
			mutate:  func(c *Config) { c.MaxBufferSize = 0 },
			wantErr: "websocket max buffer size must be greater than 0",
		},
		{
			name:    "zero reconnection timeout",
			mutate:  func(c *Config) { c.ReconnectionTimeout = 0 },
			wantErr: "websocket reconnection timeout must be greater than 0",
		},
		{
			name:    "negative post connection timeout",
			mutate:  func(c *Config) { c.PostConnectionTimeout = -1 },
			wantErr: "websocket post connection timeout cannot be negative",
		},
		{
			name:    "no endpoints",
			mutate:  func(c *Config) { c.Endpoints = nil },
			wantErr: "websocket endpoints cannot be empty",
		},
		{
			name: "https endpoint",
			mutate: func(c *Config) {
				c.Endpoints = append(c.Endpoints, types.Endpoint{URL: "https://example.invalid/stream"})
			},
			wantErr: "endpoint 1: provider endpoint requires wss",
		},
		{
			name:    "empty name",
			mutate:  func(c *Config) { c.Name = "" },
			wantErr: "websocket name cannot be empty",
		},
		{
			name:    "zero handshake timeout",
			mutate:  func(c *Config) { c.HandshakeTimeout = 0 },
			wantErr: "websocket handshake timeout must be greater than 0",
		},
		{
			name:    "zero read timeout",
			mutate:  func(c *Config) { c.ReadTimeout = 0 },
			wantErr: "websocket read timeout must be greater than 0",
		},
		{
			name:    "zero write timeout",
			mutate:  func(c *Config) { c.WriteTimeout = 0 },
			wantErr: "websocket write timeout must be greater than 0",
		},
		{
			name:    "negative ping interval",
			mutate:  func(c *Config) { c.PingInterval = -1 },
			wantErr: "websocket ping interval cannot be negative",
		},
		{
			name:    "negative write interval",
			mutate:  func(c *Config) { c.WriteInterval = -1 },
			wantErr: "websocket write interval cannot be negative",
		},
		{
			name:    "negative max tickers per connection",
			mutate:  func(c *Config) { c.MaxTickersPerConnection = -1 },
			wantErr: "websocket max tickers per connection cannot be negative",
		},
		{
			name:    "zero max subscriptions per batch",
			mutate:  func(c *Config) { c.MaxSubscriptionsPerBatch = 0 },
			wantErr: "websocket max subscriptions per batch must be greater than 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig("test")
			if tt.mutate != nil {
				tt.mutate(&cfg)
			}

			err := cfg.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tt.wantErr)
		})
	}
}

func TestConfigEqual(t *testing.T) {
	testCases := []struct {
		name   string
		mutate func(*Config)
		want   bool
	}{
		{
			name: "equal",
			want: true,
		},
		{
			name: "name differs",
			mutate: func(cfg *Config) {
				cfg.Name = "other"
			},
		},
		{
			name: "max buffer size differs",
			mutate: func(cfg *Config) {
				cfg.MaxBufferSize++
			},
		},
		{
			name: "reconnection timeout differs",
			mutate: func(cfg *Config) {
				cfg.ReconnectionTimeout++
			},
		},
		{
			name: "post connection timeout differs",
			mutate: func(cfg *Config) {
				cfg.PostConnectionTimeout++
			},
		},
		{
			name: "endpoints differ",
			mutate: func(cfg *Config) {
				cfg.Endpoints = append(cfg.Endpoints, types.Endpoint{URL: "wss://backup.example.invalid/stream"})
			},
		},
		{
			name: "handshake timeout differs",
			mutate: func(cfg *Config) {
				cfg.HandshakeTimeout++
			},
		},
		{
			name: "enable compression differs",
			mutate: func(cfg *Config) {
				cfg.EnableCompression = !cfg.EnableCompression
			},
		},
		{
			name: "read timeout differs",
			mutate: func(cfg *Config) {
				cfg.ReadTimeout++
			},
		},
		{
			name: "write timeout differs",
			mutate: func(cfg *Config) {
				cfg.WriteTimeout++
			},
		},
		{
			name: "ping interval differs",
			mutate: func(cfg *Config) {
				cfg.PingInterval++
			},
		},
		{
			name: "write interval differs",
			mutate: func(cfg *Config) {
				cfg.WriteInterval++
			},
		},
		{
			name: "max tickers per connection differs",
			mutate: func(cfg *Config) {
				cfg.MaxTickersPerConnection++
			},
		},
		{
			name: "max subscriptions per batch differs",
			mutate: func(cfg *Config) {
				cfg.MaxSubscriptionsPerBatch++
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			a := testConfig("unknown")
			b := testConfig("unknown")
			if tc.mutate != nil {
				tc.mutate(&b)
			}

			require.Equal(t, tc.want, a.Equal(b))
		})
	}
}

func testConfig(name string) Config {
	return Config{
		Name:                     name,
		MaxBufferSize:            1,
		ReconnectionTimeout:      time.Second,
		PostConnectionTimeout:    time.Second,
		Endpoints:                []types.Endpoint{{URL: "wss://example.invalid/stream"}},
		HandshakeTimeout:         time.Second,
		EnableCompression:        false,
		ReadTimeout:              time.Second,
		WriteTimeout:             time.Second,
		PingInterval:             time.Second,
		WriteInterval:            time.Second,
		MaxTickersPerConnection:  1,
		MaxSubscriptionsPerBatch: 1,
	}
}
