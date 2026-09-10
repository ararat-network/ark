# Chain feed client

This is the sidecar-to-node query client. It polls the oracle feed registry; it does not fetch sidecar prices.
[client.go](client.go) owns configuration and the cached feed set, while [polling.go](polling.go) owns connections,
endpoint selection, refresh, and retry. [config.go](config.go) defines validation and timing limits.

`Run(ctx)` owns connections and TLS rotation until cancellation. Each poll tries the active endpoint and then alternatives;
a successful endpoint remains selected until it fails. Failed sweeps retain the last successful feed snapshot. `Feeds`
returns a copy and reports an error before the first successful snapshot; a successful empty list is distinguishable from failure.

Queries union active feeds with scheduled additions so providers can warm prices before activation. A scheduled removal remains active until promotion. Runtime updates compare feed sets and retain prices for
unchanged pairs; a net-zero set change needs no provider retargeting. The client is
wall-clock polled and does not assign consensus target versions; the node's vote-extension handler owns that boundary.

`Update` validates the full replacement. Address/TLS changes prepare replacement material before accepting the config and
cause a reconnect; timing-only changes keep current material. Do not share caller-owned address slices with live state.
[Runtime](../runtime/README.md) owns this client's run and consumes its snapshots.

Run `go test ./pricefeed/sidecar/chainstate/...` from the root. Tests cover startup, last-good state, empty snapshots,
failover, config replacement, and TLS. [Pricefeed operations](../../../docs/PRICEFEED_OPERATIONS.md) owns deployment;
[PROCESS_MONITORING.md](../../../docs/PROCESS_MONITORING.md) owns refresh/failover metric interpretation.
