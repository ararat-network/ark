package sidecar

import "github.com/ararat-network/ark/pricefeed/sidecar/runtime"

const (
	defaultServerAddress = "127.0.0.1:8080"
)

// Config separates reloadable runtime behaviour from process-scoped sidecar
// behaviour that is fixed for the lifetime of the oracle process.
type Config struct {
	// Runtime contains reloadable price-fetching and aggregation settings.
	Runtime runtime.Config
	// Process contains transport and config-file settings fixed at construction.
	Process ProcessConfig
}

// ProcessConfig contains non-reloadable process settings.
type ProcessConfig struct {
	// ServerAddress is the public gRPC and HTTP gateway listen address.
	ServerAddress string
	// AdminAddress enables the process-local administration service when non-empty.
	// It must use a loopback IP address.
	AdminAddress string
	// RuntimeConfigPath is the file reloaded by the administration service. It is
	// required when AdminAddress is configured.
	RuntimeConfigPath string
}
