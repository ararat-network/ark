# Process monitoring

For node and sidecar operators: configure scrape/export surfaces, interpret process metrics, and diagnose alerts.
Protocol-state signals live in [protocol monitoring](PROTOCOL_MONITORING.md).

This note says which file owns which telemetry, what `arkd start` checks about them, and what an empty otel.yaml
means. [Node startup](../../cmd/arkd/README.md#telemetry-startup) owns integration details;
[shared telemetry](../../pkg/telemetry/README.md) owns exporter and bridge implementation.

## Contents

- [1. Three surfaces, one rule](#1-three-surfaces-one-rule)
- [2. [prometheus]](#2-prometheus)
- [3. otel.yaml](#3-otelyaml)
- [4. The legacy [telemetry] bridge](#4-the-legacy-telemetry-bridge)
- [5. The price-feed sidecar](#5-the-price-feed-sidecar)
- [6. Configuration ownership](#6-configuration-ownership)
- [7. Local example](#7-local-example)
- [8. What to watch](#8-what-to-watch)
- [Application mempool occupancy](#application-mempool-occupancy)
- [Metric reduction migration](#metric-reduction-migration)

## 1. Three surfaces, one rule

| Surface           | File               | Owns                                                                        |
|-------------------|--------------------|-----------------------------------------------------------------------------|
| `[prometheus]`    | `config/app.toml`  | the node's scrape endpoint: retained process metrics, pulled        |
| otel.yaml         | `config/otel.yaml` | export pipelines: traces, log export, push metrics, extra SDK instruments |
| `[instrumentation]` | `config/config.toml` | CometBFT's consensus and p2p metrics, own registry, port `:26660` |

**Metrics are scraped through `[prometheus]`; anything that pushes goes through otel.yaml.** The legacy `[telemetry]`
section of app.toml is the SDK's deprecated go-metrics bridge and is covered in §4; with `metrics-sink = "otel"` its
series land on `[prometheus]` as well.

Only `arkd start` reads either file for telemetry. `export`, `rollback`, `pruning`, and `snapshot` build the app but
never the endpoint or the OpenTelemetry SDK.

## 2. `[prometheus]`

```toml
[prometheus]
# Enabled serves the application's OpenTelemetry metrics for Prometheus to
# scrape at /metrics on the address below.
enabled = false

# Address is the listen address of the scrape endpoint.
address = "localhost:9464"
```

What the endpoint carries when enabled:

- Ark's own meters: `abci/metrics`, `abci/oracle/metrics`, `pricefeed/client/metrics`, `pkg/metrics`.
- Go runtime and process collectors.
- The SDK's `baseapp` instrument: block and transaction counts, and the FinalizeBlock, PreBlock, BeginBlock, and
  EndBlock timings. arkd starts it on the endpoint itself, so it needs no otel.yaml. If otel.yaml names it under
  `extensions.instruments`, the SDK starts it instead, on the same endpoint, and arkd does not start it twice.
- The legacy go-metrics series, when `[telemetry]` selects the `otel` sink (§4).
- The contract runtime's cache counters, `wasmvm_cache_*`, which start registers on the endpoint's registry the
  way Gaia registers them on the default one. The upstream per-contract pinned-cache metrics are also exposed;
  optional retention exclusions belong in Prometheus scrape configuration.

**Exported series are a contract.** Names and label keys are consumed by external dashboards and alerts.
Intentional renames require query migration. The [telemetry development guide](../../pkg/telemetry/README.md#exported-series-contract)
owns fixture verification and regeneration; operators should review the series diff when adopting a release.

The endpoint's `target_info` series carries `service_name="arkd"`, `service_version="<build>"`,
`service_instance_id="<chain-id>/<moniker>"`, and `ark_chain_id="<chain-id>"`; other series carry their meter's own labels.
A listener that cannot bind stops the node, as the SDK's
API and gRPC listeners do.

An app.toml older than the section reads on its defaults, endpoint off, and `start` logs the absence at Info. The SDK
writes app.toml only when it is missing and never rewrites it, so the section has to be added by hand to an existing
file.

## 3. otel.yaml

otel.yaml is the OpenTelemetry declarative configuration file, the cross-vendor spec the SDK reads through the
`otelconf` library. `arkd start` reads it right after validating app.toml and before the app is built.

**Empty is the supported default.** `arkd init`, `arkd testnet`, and the upgrade rehearsal all write an empty file.
Empty or missing means the tracer, meter, and logger globals are noop, and `[prometheus]` alone decides whether the
node has metrics.

A non-empty file switches on, per block:

- `tracer_provider`: the SDK's spans around ABCI calls and module block hooks. Ark emits no spans of its own. This is
  the only way to get traces.
- `logger_provider`: the SDK fans the node logger out to OpenTelemetry as well as the console. Only here.
- `meter_provider`: push readers for every meter in the process, **only while `[prometheus]` is off**. See the rules
  below.
- `extensions.instruments`: the SDK's optional `host`, `runtime`, `diskio`, and `baseapp` instruments.
- `extensions.propagators`. The file-exporter keys the SDK declares beside it are not wired in this release.

Values accept `${VAR}` and `${VAR:-default}` substitution. `file_format` is required. A traces-only file, the usual
collector case, is:

```yaml
file_format: "1.0-rc.3"
tracer_provider:
  processors:
    - batch:
        exporter:
          otlp_grpc:
            endpoint: ${OTEL_EXPORTER_OTLP_ENDPOINT:-http://localhost:4317}
```

Rules `arkd start` applies:

- **A `pull` reader is refused.** The otelconf release this build carries constructs none, so the SDK would fail the
  node one step later with a generic error. arkd fails first and names `[prometheus]` as the replacement.
- **A `meter_provider` with `[prometheus]` enabled is shadowed, and start warns.** Its readers export no metrics.
  Turn `[prometheus]` off to push metrics instead.
- **A malformed file fails start**, through the SDK's own parse. arkd reads only the file's shape and never judges it.
- **`OTEL_EXPERIMENTAL_CONFIG_FILE` with `[prometheus]` enabled is refused.** The SDK initialises OpenTelemetry
  from that file at package load, before arkd installs the endpoint's provider, so Ark's package-level meters bind
  to that file's provider, or to noop when it is empty, and the endpoint would serve without them. With
  `[prometheus]` off the variable is fine, but arkd reads only `config/otel.yaml`, so the checks above do not see
  that file.

## 4. The legacy `[telemetry]` bridge

`[telemetry]` in app.toml is the SDK's deprecated go-metrics configuration, which upstream is removing. What Ark does
with it:

- `enabled = true` with `metrics-sink = "otel"` routes the SDK's remaining go-metrics calls to an OpenTelemetry meter.
  The series land on `[prometheus]`; Ark's adapter normalises query routes to the bounded series described below.
- The same sink with `[prometheus]` off leaves those series with nowhere to go but otel.yaml's `meter_provider`.
  `start` warns.
- `prometheus-retention-time` is not carried over. The legacy Prometheus fan-out behind it is what `[prometheus]`
  replaces.

**Legacy metric migration.** Query paths no longer become instrument names. Completed ABCI queries export
`ark_sdk_query_duration_milliseconds{route}`; its `_count` series counts requests. The route is a registered
gRPC method, `app`, `store`, `p2p`, or `unknown`; even malformed paths without a leading slash use this bounded
classification. The histogram records from the SDK's deferred query timer, so each completed query counts once.

Other legacy metrics now export under `sdk_legacy_<kind>_<readable-key>_<digest>`, where kind is `counter`, `gauge`,
or `histogram`. The readable key joins components with underscores and replaces punctuation with underscores;
it is limited to 120 characters. The digest is the SHA-256 hex of the original key components joined with NUL.
This distinguishes names that Prometheus sanitises identically and separates generated histogram suffixes from
other instrument types. Counter and histogram suffixes follow Prometheus conventions. Update legacy dashboards
when adopting this change; existing `ark_*` names retain their contract. The bridge admits at most 1,024 other
legacy instruments; existing entries continue recording at the limit and additional names are dropped. Query
paths do not consume that allowance.

`arkd testnet` writes `enabled = true` and `metrics-sink = "otel"` beside an enabled `[prometheus]`, which is the
recommended pairing.

## 5. The price-feed sidecar

The sidecar is a separate process with its own flags: `--metrics` and `--metrics-address` (default
`127.0.0.1:9091`) serve its scrape endpoint, and `--pprof` its profiler. Nothing in app.toml or otel.yaml reaches it.
The node-side client that polls it is part of the node, and its meters appear on `[prometheus]`.

## 6. Configuration ownership

[Node telemetry startup](../../cmd/arkd/README.md#telemetry-startup) explains why the application scrape endpoint and the
SDK's declarative export configuration have separate owners. Use the configuration rules above when operating them.

## 7. Local example

[Localnet topology and ports](../../contrib/localnet/README.md#topology-and-ports) lists each validator's scrape endpoints
and generated settings. Use the application endpoint for node metrics and the instrumentation endpoint for consensus.

## 8. What to watch

Conditions only. The PromQL, the thresholds, and who gets paged live with the dashboards outside this repository
and cite these IDs. The A-series in `docs/operations/PROTOCOL_MONITORING.md` watches protocol state through queries and events;
this section watches the node and its sidecar as processes, through the exported-series contract linked in §2.
Names below are the exported form.

Two rules carry over. Alert state is off-chain: "for N blocks" needs history only the scraper has. And a gauge is
sampled once per scrape interval, which is many blocks, so the per-block gauges here (`ark_oracle_vote_targets`,
`ark_oracle_participating_power_share`) say what the last block looked like, and the counters beside them are what
`increase()` reads.

Configured sidecar endpoints, chainstate endpoints, and provider tickers export last-success timestamps of zero
until their first success. Retained entries keep their timestamps through reconnects; removed chainstate endpoints
and provider markets retire their gauges. A standby endpoint that has never been polled may legitimately remain
zero: N2 and S7 evaluate the newest success across the configured set, not every standby individually. Apply a
startup grace covering initial connection/polling and at least two scrapes. The aggregation tick counter starts
at zero, so S6 also detects a loop that never completes its first tick. Zero-added counters do not guarantee that
Prometheus scraped the initial zero; first-event alerts still need the corresponding state/last-success signal.

Aggregate-price and sample-count gauges describe the last completed resolution snapshot.
Missing prices disappear rather than carrying their last good value forward; sample counts remain zero for active
pairs with no live samples, including bootstrap fallback. A configuration change affecting prices invalidates the
old observations until the next resolution; timing-only and failed updates preserve them. A stalled aggregation
loop can still leave its last snapshot visible, so interpret these gauges alongside S6 and the node's snapshot age.

`ark_abci_method_duration_milliseconds` measures Ark's wrapper work: PrepareProposal/ProcessProposal exclude the
wrapped handler, and PreBlock excludes the module manager's preblockers. Use CometBFT's proxy timings for complete
ABCI calls and SDK baseapp timings for complete block stages. `status="Rejected"` identifies a ProcessProposal
REJECT response without a Go error. `status="Panic"` records panic unwinding; only ExtendVote has Ark recovery,
and the other handlers propagate their original panic unchanged.

### 8.1 Node

**N1 — This validator is abstaining · page**

- Condition: `ark_oracle_vote_targets{status!="priced"}` above zero across several scrapes, or
  `increase(ark_oracle_vote_dropped_targets_total[10m]) > 0`.
- Why: the report this node signs is missing active targets. An omitted target is one the sidecar did not price; a
  dropped one is a rate the sidecar produced that the node could not decode, which is a build mismatch or a codec
  fault. The ExtendVote status counter reads Success throughout, because an omission is a legal report. The node
  participates only if it prices `ParticipationThreshold` of the target set, and attendance below
  `MinAttendancePerWindow` of its eligible blocks across an `AttendanceWindow` is what jails it.
- First look: S3 on the sidecar this node polls, then `ark_pricefeed_skipped_samples_total` for the provider that
  stopped contributing. For dropped targets, compare the sidecar's `target_info.service_version` with the node's
  build; the node's version-change logs identify the endpoint and version it observed.

**N2 — Price feed unreachable · page**

- Condition: `increase(ark_abci_requests_total{method="extend_vote",status!="Success"}[5m]) > 0` sustained, or
  `time() - ark_pricefeed_sidecar_last_success_seconds` past `[pricefeed] price_ttl` for every configured address.
- Why: every ExtendVote answers with an empty extension, a full abstention, so N1's consequence with none of its
  warning. The `status` label names the stage: `PriceFeedClientError` is the sidecar, `InvalidPricesError` and
  `CodecError` are the report itself, `Panic` is a recovered handler panic that liveness otherwise hides.
- First look: S6 on the sidecar, then the addresses and TLS files under `[pricefeed]`.

**N3 — Snapshot ageing behind a live sidecar · warn**

- Condition: `time() - ark_pricefeed_snapshot_timestamp_seconds` approaching `price_ttl` while
  `ark_pricefeed_sidecar_last_success_seconds` stays fresh.
- Why: the sidecar answers, but with an old snapshot: its aggregation loop has stalled, or it has nothing fresh to
  aggregate. The node rejects the snapshot at the TTL and N2 fires; this is the lead time.
- First look: S6, then S1 for every provider at once.

**N4 — Fleet participation below the functioning floor · page**

- Condition: `ark_oracle_participating_power_share` below `FunctioningBlockThreshold`, or
  `increase(ark_oracle_blocks_total{functioning="false"}[15m]) > 0`.
- Why: this is the network, not the node. Every node exports the same value from the same commit. Below the floor no
  block is graded, so nobody accrues attendance. Price quorum is evaluated per denomination separately; a
  functioning block does not guarantee every feed was priced. A correlated
  outage of a few large validators looks fine in `ark_oracle_vote_reports_total`, which counts heads.
- First look: `ark_oracle_vote_reports_total{status="empty"}` rising is validators abstaining wholesale;
  `status="invalid"` is a release mismatch. Then `EventExchangeRateUpdate` going quiet, and A4.

**N5 — Peers' reports failing verification · warn**

- Condition: `increase(ark_abci_requests_total{method="verify_vote_extension",status!="Success"}[15m])` or
  `increase(ark_abci_requests_total{method="process_proposal",status="ExtendedCommitValidationError"}[15m])`
  beyond a trickle.
- Why: other validators are submitting reports this node rejects, or a proposer is injecting a commit it should not.
  One node rejecting is local; every node rejecting is the network; whether the same alert fires elsewhere is the
  difference.
- First look: the `status` label, then `ark_oracle_vote_reports_total{status="invalid"}` at the next block.

**N6 — Reserved mempool allocation remains occupied · warn**

- Condition: `ark_mempool_transactions{lane=~"governance|committee"} > 0` across sustained scrapes and this node's
  proposer opportunities. Compare `ark_mempool_bytes` as well when bytes may be the binding limit.
- Why: these gauges count reserved admission allocations. Governance transactions can also occupy normal capacity,
  so they do not count all privileged transactions. Persistent occupancy may be backlog or transactions that are
  not being included; occupancy alone does not establish that the same transaction is stuck.
- First look: CometBFT proposal/consensus timing, then `ark_abci_requests_total` by method/status.
  `ark_mempool_admissions_total{status="full"}` counts reservation-partition refusals. It cannot identify a full
  reserved lane on its own: a privileged transaction overflows into normal storage before it is refused.

**N7 — Block hooks approaching block time · warn**

- Condition: a high quantile of `ark_abci_method_duration_milliseconds{method="pre_blocker"}` or of
  `ark_module_method_duration_milliseconds` trending toward the block interval; `ark_abci_requests_total{status="Panic"}`
  above zero at all.
- Why: PreBlock carries the oracle aggregation and every module's block hooks follow it; time spent there is time
  taken from consensus.
- First look: the `module` label, then the SDK's baseapp timings beside them.

### 8.2 Sidecar

**S1 — Provider alive but contributing nothing · warn**

- Condition: `increase(ark_pricefeed_skipped_samples_total[10m]) > 0` for a provider and pair, sustained, while
  `time() - ark_pricefeed_provider_last_success_seconds` for that provider stays small.
- Why: the provider keeps answering, so its response counter and last-success gauge look healthy, but every sample is
  past `max_price_age` or `max_unchanged_age` and the resolver leaves it out. A frozen websocket feed looks exactly
  like this. The `reason` label says which bound.
- First look: the skipped-sample `reason`, the provider's freshness configuration, and its response/error metrics.
  For websocket providers, inspect connection events as well; use sidecar logs for individual sample details.

**S2 — Provider down · warn**

- Condition: `time() - ark_pricefeed_provider_last_success_seconds` past that provider's `max_price_age`, or
  `increase(ark_pricefeed_provider_responses_total{error_code!="ok"}[10m])` dominating its successes.
- Why: one provider out is what the median is for; two of three is not. Page only when
  `ark_pricefeed_pair_sample_count` falls with it.
- First look: `error_code`, then `ark_pricefeed_provider_api_request_duration_milliseconds_count` by `status_code`
  for an API provider or `ark_pricefeed_provider_websocket_connection_events_total` by `event` for a websocket one.

**S3 — Feed unpriced · page**

- Condition: `increase(ark_pricefeed_missing_prices_total[5m]) > 0` for a denom that is active on-chain.
  The sidecar also warms scheduled additions; missing those is a readiness warning until activation.
- Why: an active feed left the snapshot, so every validator polling this sidecar omits it and N1 follows on each.
  This is the upstream cause; alerting here catches it before it becomes attendance.
- First look: `ark_pricefeed_pair_sample_count` for the configured route legs, then S1 and S2. Compare the
  configured paths with `ark_pricefeed_aggregate_price` to identify which outputs are absent.

**S4 — Bootstrap price in use · warn**

- Condition: `increase(ark_pricefeed_bootstrap_price_uses_total[5m]) > 0`.
- Why: a route leg resolved from a configured last-resort price rather than a live sample. That is the fallback
  working, and it is a number an operator typed feeding a signed report.
- First look: which pair, then S1 and S2 for the providers that should have covered it.

**S5 — Thin median · warn**

- Condition: `ark_pricefeed_pair_sample_count` at one for a pair configured with several providers.
- Why: a median of one is that provider's price. Nothing is wrong yet; the redundancy is gone.
- First look: S2.

**S6 — Aggregation loop stalled · page**

- Condition: `rate(ark_pricefeed_ticks_total[2m]) == 0` on a process that is up, or
  `ark_pricefeed_rpc_requests_total{code!="OK"}` rising for the Prices method.
- Why: the snapshot the node fetches stops moving, and N3 then N2 follow on every node polling it. The `code` label
  says whether the sidecar is answering with an error or not being asked at all.
- First look: the sidecar log, then `update_interval` and the provider set.

**S7 — Feed registry stale · warn**

- Condition: `time() - ark_pricefeed_chainstate_last_success_seconds` past several `[client] interval`s for every
  address.
- Why: the sidecar prices the feed set it last read. A feed activated on-chain since then is not in its snapshot, and
  every node polling it omits that feed from the block it activates. The refresh counter's `status="error"` series
  says why.
- First look: the addresses under `[client]`, and whether those nodes serve gRPC.

### 8.3 What not to alert on

- A single skipped sample, reconnect, or parse error. Rates, not events.
- `ark_oracle_vote_reports_total{status="empty"}` on its own. Some abstention is normal; N4 is the version that
  matters.
- A non-empty priority lane for one block. It empties when this node proposes.

### 8.4 Thresholds

N1's grace, N5's trickle, N7's fraction of block time, and every window above are placeholders, to be set from the
first weeks of data the way `docs/operations/PROTOCOL_MONITORING.md` §2.8 sets its own. The parameter names in the conditions are the
oracle module's, so a governance change to any of them moves the corresponding rule.

## Application mempool occupancy

CometBFT's `cometbft_mempool_size` and unconfirmed-transaction RPCs report its flood list.
Scrape `ark_mempool_transactions` and `ark_mempool_bytes` by `lane` (`normal`, `governance`, `committee`)
for application admission allocations, and `ark_mempool_admissions_total` by `status`
(`accepted`, `full`, `invalid`). A governance transaction stored in ordinary capacity counts
toward `normal`; surviving allocations remain fixed at insertion. Counts include SDK rechecks
and are not incoming transaction throughput. `full` identifies reservation-partition refusals;
the SDK's total-count error is reported as `invalid`. Gauges read pool occupancy at scrape
time. The app pool can conservatively retain entries absent from the flood list, including after
an SDK post-handler failure. These gauges are per process and use no account or proposal labels.

## Metric reduction migration

The reduction deliberately removes seven Ark-defined metric families:

| Removed family | Replacement |
| --- | --- |
| `ark_sdk_query_requests_total` | `ark_sdk_query_duration_milliseconds_count`, same route labels |
| `ark_pricefeed_provider_api_requests_total` | `ark_pricefeed_provider_api_request_duration_milliseconds_count`, same provider/status-code labels |
| `ark_abci_extended_commit_size_bytes` | None; the commit-size diagnostic histogram is removed |
| `ark_pricefeed_rpc_request_duration_milliseconds` | Node-side `ark_pricefeed_response_duration_milliseconds` for polling latency; server RPC outcome counts remain |
| `ark_pricefeed_provider_price` | No historical per-provider price series; use sample counts, skipped samples and provider errors for health, aggregate prices for output |
| `ark_pricefeed_route_price` | `ark_pricefeed_aggregate_price` for final output; individual route observations are removed for all configurations |
| `ark_pricefeed_sidecar_version` | Sidecar `target_info` build metadata and node-side version-change logs |

Provider and route price instrumentation is removed from the resolver, including its extra traversal, float
conversions and metric snapshot maps. Price calculation, sample counts and aggregate-price publication remain.
Version-change logging retains its endpoint history; the current/previous-version gauge bookkeeping is removed.
Update external dashboard and alert queries before adopting this reduction. The two histogram `_count`
replacements preserve their removed counters' label sets and counting semantics.

### Optional dependency exclusions at the scraper

Ark exports upstream SDK BaseApp, Wasm and Go/process metrics without custom filters. Keeping an exclusion policy
in Prometheus avoids maintaining dependency metric-name lists and collector wrappers in production Ark code.
No external Prometheus configuration is changed by this repository update.

To retain the earlier reduction in stored dependency series, add these optional rules to the **arkd application
scrape job** under `metric_relabel_configs` (merge with any existing rules):

```yaml
metric_relabel_configs:
  - source_labels: [__name__]
    regex: 'wasmvm_pinned_contract_(hits|size)'
    action: drop
  - source_labels: [__name__]
    regex: '(working_hash_time|internal_finalize_time|finalize_non_oe_internal_time|get_finalize_state_time|streaming_listener_time|oe_time|oe_abort_if_needed_time)_milliseconds_(bucket|sum|count)'
    action: drop
```

These rules omit the two per-contract Wasm families and seven SDK diagnostic histograms. Aggregate Wasm cache
metrics, complete block-stage and transaction-execution timings, block/transaction counts, and optimistic-execution
abort counts remain. Scrape exclusions reduce ingestion/storage only: the node still records and exports those
upstream metrics. If that storage saving is unnecessary, omit the rules and use the upstream defaults.
