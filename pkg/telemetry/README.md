# Telemetry integration

This package owns process-level telemetry setup. Subsystems define their own instruments; startup code decides which
registries and exporters serve them. [PROCESS_MONITORING.md](../../docs/operations/PROCESS_MONITORING.md) owns operator configuration, scrape
surfaces, and series interpretation.

## Code map

- [config.go](config.go) validates process telemetry configuration.
- [prometheus.go](prometheus.go) builds the application Prometheus export surface.
- [gometrics.go](gometrics.go) bridges SDK go-metrics instruments.
- [http_server.go](http_server.go) owns the common HTTP server lifecycle.
- [build.go](build.go) identifies build metadata for process resources and sidecar version reporting.
- [telemetrytest/golden.go](telemetrytest/golden.go) compares exported series names, types, and label keys against fixtures.

The node's startup adapter is [cmd/arkd/cmd/telemetry.go](../../cmd/arkd/cmd/telemetry.go); the sidecar's is
[cmd/pricefeed/cmd/telemetry.go](../../cmd/pricefeed/cmd/telemetry.go). Keep their lifecycle wiring with the process that
owns shutdown. Protocol-state observability through queries/events is described separately in [PROTOCOL_MONITORING.md](../../docs/operations/PROTOCOL_MONITORING.md).

## Legacy bridge

The SDK's go-metrics bridge can panic on query-path instrument names. `gometrics.go` instead routes completed ABCI
queries into one bounded route-labelled histogram and sanitises other keys with kind separation and a digest of the
original NUL-separated components. The instrument cache is bounded; existing names keep recording when new names
are dropped at capacity. Query routes do not consume that cache. [Process monitoring](../../docs/operations/PROCESS_MONITORING.md#4-the-legacy-telemetry-bridge)
owns the exact exported names, limits, labels, and dashboard migration rules.

Keep process-specific registry selection and SDK initialisation in the [node adapter](../../cmd/arkd/README.md#telemetry-startup)
or the sidecar adapter; subsystem instrument ownership stays with each subsystem.

## Exported series contract

The fixtures are [node exported_series.txt](../../cmd/arkd/cmd/testdata/exported_series.txt) and
[sidecar exported_series.txt](../../cmd/pricefeed/cmd/testdata/exported_series.txt). Names, types, and label keys are an
integration contract for dashboards and alerts; sampled values are not part of the golden comparison.

From the root, verify with:

```sh
go test ./pkg/telemetry/...
go test ./cmd/arkd/cmd -run TestPrometheusEndpointOwnsMetersAcrossSDKInit
go test ./cmd/pricefeed/cmd -run TestRunServiceServesItsProcessEndpointsUntilCancelled
```

For an intentional contract change, append `-args -update-golden` to the relevant command, review the fixture diff,
update affected dashboard/alert queries and documentation, then rerun without the update flag. These endpoint tests bind
local sockets. Do not regenerate the fixtures simply to make an unexplained rename pass.
