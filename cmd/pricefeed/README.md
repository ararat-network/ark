# pricefeed command

[main.go](main.go) runs [cmd/root.go](cmd/root.go). This executable is the off-chain sidecar and does not run consensus.
The root supplies the runtime config path and the logger, built from the log flags before any command runs; command
implementations own process options and output.

| Command / files | Owns |
| --- | --- |
| `init` — [cmd/init.go](cmd/init.go) | Write the default runtime file; refuse replacement without `--overwrite`. |
| `start` — [cmd/start.go](cmd/start.go) | Load config, construct the service, and own process endpoint/telemetry lifecycle. |
| `config validate/reload` — [cmd/config.go](cmd/config.go) | Local validation and loopback admin reload request. |
| `prices` — [cmd/prices.go](cmd/prices.go) | Fetch and render the latest snapshot as a table or JSON. |
| `check` — [cmd/check.go](cmd/check.go) | Compare sampled prices with the chain's active feed set through `pricefeed/validation`. |
| [cmd/tls_flags.go](cmd/tls_flags.go), [cmd/telemetry.go](cmd/telemetry.go) | Transport flags and process telemetry. |

Runtime config defaults/loading live in [pricefeed/config](../../pricefeed/config/); the
[sidecar service](../../pricefeed/sidecar/README.md) owns RPC listeners and delegates price work to runtime.
[Pricefeed operations](../../docs/operations/PRICEFEED_OPERATIONS.md) owns deployment, runtime reload, transport, and compatibility.

## Build and verify

From the repository root:

```sh
make build-pricefeed
./build/pricefeed --help
go test ./cmd/pricefeed/cmd
```

Use command-level `--help` for the current flag surface. Tests cover command defaults, config handling, and process
lifecycle. Endpoint tests need loopback sockets. The [telemetry guide](../../pkg/telemetry/README.md) owns the exported-series
fixture update procedure. [The repository map](../../README.md) links the other binary and subsystem guides.
