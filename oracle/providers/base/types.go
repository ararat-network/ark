package base

// TransportType identifies the transport used by a provider fetcher.
type TransportType string

const (
	// WebSocket identifies a websocket provider fetcher.
	WebSocket TransportType = "websocket"
	// API identifies an HTTP API provider fetcher.
	API TransportType = "api"
)

type ProviderType string

const (
	Fiat   ProviderType = "fiat"
	Crypto ProviderType = "crypto"
)
