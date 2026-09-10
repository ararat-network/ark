# Sidecar service

[Service](service.go) owns the off-chain process-facing service. It implements the generated RPC server and delegates
provider/resolver work to [runtime](runtime/README.md). Runtime configuration and process configuration are separate:
[config.go](config.go) defines reloadable runtime settings and listener/config-path settings fixed at construction.

## Code and lifecycle

| Entry point | Responsibility |
| --- | --- |
| [service.go](service.go), [options.go](options.go) | Service assembly and dependency options. |
| [server.go](server.go) | Public gRPC/HTTP gateway listener and shutdown. |
| [rpc.go](rpc.go) | Public price/health/config RPC handling. |
| [admin_server.go](admin_server.go) | Separate loopback administration service and runtime reload. |
| [rpc_metrics.go](rpc_metrics.go), [metrics/](metrics/) | RPC and service instrumentation. |
| [types/](types/) | Pair and price domain values used off chain. |

The public listener serves the transport contract; the admin listener is local process administration. The
[CLI](../../cmd/pricefeed/README.md) owns process telemetry and cancellation. Listener failures and runtime termination
must propagate through the service's run/shutdown path so no worker is left detached from its owner.

## Price work

[Runtime](runtime/README.md) owns provider runs and snapshot publication. [Chain state](chainstate/README.md) polls the
node's active/scheduled feeds. [Providers](providers/README.md) construct exchange adapters and shared fetchers;
[resolver](resolver/README.md) combines their observations into feed prices. Keep these responsibilities out of RPC handlers.

Run `go test ./pricefeed/sidecar/...` from the root for the subtree, or `go test ./pricefeed/sidecar` for service tests.
Transport tests exercise public/admin listeners, cancellation, TLS and malformed requests. Deployment and reload commands
are in [PRICEFEED_OPERATIONS.md](../../docs/PRICEFEED_OPERATIONS.md); update the [threat model](../../docs/THREAT_MODEL.md)
when adding a listener or inbound parser.
