# Node-side price client

[Client](cached_client.go) polls sidecars in its owning process lifecycle and serves cached snapshots to ABCI callers.
`Prices` does no network I/O. [config.go](config.go) reads `[pricefeed]` from app options and validates transport, timing,
and endpoint bounds; node configuration is fixed when the application is constructed.

## Lifecycle and snapshots

`NewClient` validates configuration. `Run(ctx)` creates the connections, starts identity rotation where configured,
polls until cancellation, and cleans up before returning. The owner must cancel and wait; concurrent runs are unsupported.
A disabled client waits for cancellation without dialling.

Polling tries the active endpoint first, then alternatives in configured order, with a timeout per attempt. A successful
endpoint remains active until failure. Responses are validated before replacing the cached snapshot; the snapshot's own
timestamp is checked at acceptance and again when served, so a cached response cannot remain usable indefinitely after
polling fails. Responses are copied across the cache/caller boundary.

The sidecar build version is observed for operators and never used as a compatibility gate. The authoritative
[compatibility and failover guidance](../../docs/operations/PRICEFEED_OPERATIONS.md) explains what operators configure and what a
rolling sidecar replacement can preserve.

## Integration and tests

[app/oracle.go](../../app/oracle.go) constructs the client; the node start command owns its run lifecycle.
[ABCI vote extensions](../../abci/README.md#vote-extension-handlers) use it with their own request timeout.
[metrics/](metrics/) owns client instruments; [PROCESS_MONITORING.md](../../docs/operations/PROCESS_MONITORING.md) explains their interpretation.

From the root, run `go test ./pricefeed/client/...`. Cache, timestamp, failover, TLS and snapshot-ownership tests live
beside `cached_client.go`; also run affected `./app` or `./cmd/arkd/cmd` tests when startup integration changes.
