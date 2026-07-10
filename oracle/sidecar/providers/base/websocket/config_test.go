package websocket_test

import (
	. "ark/oracle/sidecar/providers/base/websocket"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ark/oracle/sidecar/providers/types"
)

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
