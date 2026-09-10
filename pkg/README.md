# Shared packages

These packages provide small primitives used across application and sidecar boundaries. Domain policy stays with its
owning module; a shared package should not acquire keeper dependencies merely to reduce repeated call-site code.

| Package | Owns | Read next |
| --- | --- | --- |
| [chain](chain/) | Chain units, denomination grammar, metadata, addresses, and block constants. | [denom.go](chain/denom.go), [coins.go](chain/coins.go) |
| [decimal](decimal/) | Checked `LegacyDec` arithmetic. | [Package contract](decimal/README.md) |
| [encoding](encoding/) | Compact per-value decimal encoding. | [legacy_dec.go](encoding/legacy_dec.go) |
| [fsutil](fsutil/) | Atomic, synced file replacement for the config files both binaries write. | [replace.go](fsutil/replace.go) |
| [grpcconn](grpcconn/) | gRPC transport preparation, endpoint locality, connections and cleanup. | [Package contract](grpcconn/grpcconn.go) |
| [tlsconfig](tlsconfig/) | TLS file loading and reloadable identity material. | [tlsconfig.go](tlsconfig/tlsconfig.go), [material.go](tlsconfig/material.go) |
| [mandate](mandate/) | Shared appointment envelope and bounded delegation checks. | [Package guide](mandate/README.md) |
| [metrics](metrics/) | Small metric types and module method instrumentation. | [metrics.go](metrics/metrics.go) |
| [telemetry](telemetry/README.md) | Process telemetry startup and export integration. | [Developer guide](telemetry/README.md) |

Primitive encoding limits belong in `encoding`; aggregate vote-extension limits belong in [abci/codec](../abci/codec/).
TLS loading does not start an independent daemon: the connection owner starts and stops identity rotation with its own
lifecycle. [Pricefeed operations](../docs/operations/PRICEFEED_OPERATIONS.md) owns transport deployment and compatibility policy.

Run focused tests from the root, for example `go test ./pkg/tlsconfig/... ./pkg/grpcconn/...` for transport changes or
`go test ./pkg/decimal/... ./pkg/encoding/...` for arithmetic/encoding. Check affected callers too when a shared contract
changes. Test helper subpackages document their exported APIs in Go rather than duplicating this guide.
