# Price feed

The price feed bridges off-chain observations into the node's oracle vote-extension path. Two independent query links
run in opposite directions: the node polls sidecar prices; the sidecar polls the node's feed registry.

| Directory | Responsibility |
| --- | --- |
| [client](client/README.md) | App-owned cached price client, endpoint selection, and snapshot freshness. |
| [api](api/) | Generated gogo transport types and gateway for `ark.pricefeed.v1`. |
| [sidecar](sidecar/README.md) | Off-chain service, public/admin transports, and runtime ownership. |
| [config](config/) | Default runtime configuration and TOML load/encode. |
| [validation](validation/) | Liveness sampling used by the `pricefeed check` command. |

The [pricefeed binary](../cmd/pricefeed/README.md) owns CLI startup and process options. [app/oracle.go](../app/oracle.go)
connects the cached node client to [ABCI](../abci/README.md); [x/oracle](../x/oracle/README.md) owns on-chain feed/rate state.
Sidecar domain values live under `sidecar/types`; transport-generated types stay under `api`.

[Pricefeed operations](../docs/PRICEFEED_OPERATIONS.md) owns configuration, deployment, independent releases, and the
single node–sidecar compatibility contract. [Oracle design](../x/oracle/README.md) owns consensus rate semantics.

For local changes run focused `go test ./pricefeed/<package>/...` from the root. `go test ./pricefeed/...` covers the
whole subsystem; add command tests when flags or config loading change. Follow [proto generation](../proto/README.md)
for changes to the transport schema. Do not put provider network access on the ABCI request path.
