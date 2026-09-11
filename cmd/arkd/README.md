# arkd command

[main.go](main.go) invokes the SDK server command using [cmd/root.go](cmd/root.go). Root construction creates the
client context and AutoCLI tree, then dresses transaction commands with Ark fee handling. [cmd/commands.go](cmd/commands.go)
registers genesis, query, transaction, server, and export commands and constructs the application.

| Command / files | Owns |
| --- | --- |
| `start` — [cmd/start.go](cmd/start.go) | The start command's hooks: app.toml validation, the metrics endpoint, and the price-feed client. |
| `config migrate/diff/set/validate` — [cmd/config_command.go](cmd/config_command.go) | app.toml upkeep against this binary's template, and the validation start applies over a file alone. |
| `export` — [cmd/export.go](cmd/export.go) | The continuation genesis. |
| `genesis add-validator-seat` — [cmd/add_validator_seat.go](cmd/add_validator_seat.go) | One equal validator seat granted from the community pool at assembly; the edit itself is [app/genesis](../../app/genesis/seat.go). |
| `query vote-extensions` — [cmd/vote_extensions.go](cmd/vote_extensions.go) | The oracle votes retained in a committed block. |
| `testnet start/init-files` — [cmd/testnet.go](cmd/testnet.go) | Testnet file generation and the in-process testnet. |
| [cmd/root.go](cmd/root.go), [cmd/commands.go](cmd/commands.go) | Root command, client context, AutoCLI tree, transaction-command dressing, command registration, and app construction. |
| [cmd/config.go](cmd/config.go), [cmd/otel.go](cmd/otel.go), [cmd/telemetry.go](cmd/telemetry.go) | app.toml as a typed file with its template and validation, config.toml's defaults, the otel.yaml read, and the Prometheus endpoint. |

The start command validates local configuration before constructing the app and owns running its pricefeed client.
[app/](../../app/README.md) owns keeper wiring and consensus lifecycle. Export delegates to the app's continuation export and
writes the whole consensus block, the ABCI parameters the SDK's own export drops included; zero-height export is refused. [Node operations](../../docs/operations/NODE_OPERATIONS.md) owns commands for operators and
[client fees](../../docs/clients/CLIENT_FEES.md) owns fee construction.

## CLI fee completion

[app/client/fees.go](../../app/client/fees.go) implements completion for transaction commands.
[Client fee construction](../../docs/clients/CLIENT_FEES.md) owns the external contract; these choices are CLI policy:

- `DefaultFeeHeadroom` (1.1) and `DefaultGasAdjustment` (1.15) are **policy, not consensus**. They are one prudent
  choice against a 2.5%-per-block controller. A market maker may want more; a client resubmitting on failure may
  want less.
- The denomination ranking in `pickFeeDenom` — the denominations in `tax_base`, then NOAH, then the reference,
  then the rest of the sheet — is a convenience for a CLI user who has not said which denomination to pay in. An
  integration that holds one denomination has no use for it.
- `arkd` nets `tax_base` against the payer's spendable balance when ranking. `tax_base` is what was taxed, not what
  leaves the payer: a `MsgSwap` spends its offer coin untaxed and is absent from it, and a `MsgExec` moves the
  granter's coins and is present. The chain is the arbiter either way; a wrong guess costs a refusal, not funds.

The split to hold onto: the chain publishes what is true, the client decides what is prudent. Tax and gas price
are true and come from the queries. Headroom, gas adjustment, and denomination preference are prudent and are
yours.

## Telemetry startup

Only `start` reads process telemetry configuration. Its PreRunE, in `cmd/start.go`, decodes and validates app.toml once
and reads `config/otel.yaml` before building the app. Export, rollback, pruning, and snapshot construct
the app without starting exporters. [Operator configuration](../../docs/operations/PROCESS_MONITORING.md) owns accepted combinations,
startup warnings, and example files.

OpenTelemetry meters bind to the global provider at creation; package-level proxy meters bind to the first installed
provider. The Prometheus adapter installs its provider before SDK init and again after SDK init, so retained meters
land on the application endpoint even when the SDK installs noop providers for an empty file. This is why an otel.yaml
meter provider is shadowed while the endpoint is enabled. Package-load initialisation through
`OTEL_EXPERIMENTAL_CONFIG_FILE` happens too early for this sequence and is rejected with Prometheus enabled.

The node uses a private registry to avoid collisions with CometBFT's default registry and the SDK legacy sink.
It registers baseapp instruments unless otel.yaml already requests them, plus the Wasm cache collectors. PostSetup
installs the shared sanitising legacy bridge. SDK providers shut down in app cleanup; the application endpoint's
provider shuts down with the server errgroup.

### Why configuration remains in two files

- The SDK reads `config/otel.yaml` at a fixed path, unconditionally, and overwrites all three globals with noop when
  the file is empty. There is no hook to hand it a decoded struct. Merging means reimplementing the SDK's init and
  shutdown and re-installing providers behind its back.
- app.toml is viper and mapstructure, with no schema. otel.yaml has one, a `file_format` version, and env
  substitution, all of which a TOML table would lose.
- The schema is arrays of one-key tables four levels deep, which TOML expresses badly.
- Upstream keeps otel.yaml and is deprecating `[telemetry]`; merging goes against the current and against every other
  chain's operator docs.

## Build and verify

From the repository root:

```sh
make build
./build/arkd --help
go test ./cmd/arkd/cmd
```

Use command-level `--help` for the current flag surface. Tests cover command defaults, config handling, and process
lifecycle. Endpoint tests need loopback sockets. The [telemetry guide](../../pkg/telemetry/README.md) owns the exported-series
fixture update procedure. [The repository map](../../README.md) links the other binary and subsystem guides.
