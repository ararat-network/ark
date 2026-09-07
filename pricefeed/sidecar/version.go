package sidecar

import "github.com/ararat-network/ark/pkg/telemetry"

// Version returns the build version reported by the price RPC and the CLI:
// the same identity the sidecar's metrics endpoint carries as
// service.version.
func Version() string {
	return telemetry.BuildVersion()
}
