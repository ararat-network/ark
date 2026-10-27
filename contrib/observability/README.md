# Observability templates

What runs beside a node, or once for the network, to watch it: the Prometheus scrape and Alertmanager routing
templates, the alert rules for every alert [process monitoring](../../docs/operations/PROCESS_MONITORING.md) and
[protocol monitoring](../../docs/operations/PROTOCOL_MONITORING.md) specify, and the Grafana dashboards. The guides
own each condition and why it matters; these files are the conditions as configuration, taken from the tag of the
`arkd` they watch, so a validator's rules match its binary. Routing destinations and the thresholds the guides leave
as placeholders stay with the operator.

| File | Holds |
| --- | --- |
| [prometheus.yml](prometheus.yml) | The scrape template: one job per endpoint kind with a `node` label on every target, the dependency-series exclusions, and the membership service's job. |
| [prometheus.localnet.yml](prometheus.localnet.yml) | The same for the localnet, by compose service name. |
| [alertmanager.yml](alertmanager.yml) | The routing template: `page` to on-call, the rest to a channel; placeholder receivers. |
| [rules/ark.yml](rules/ark.yml) | Every rule: node N1–N7, sidecar S1–S7, the generic process rules, and Disbursement D1, D3, and D4. |
| [dashboards/node.json](dashboards/node.json) | The node dashboard, one row per node, over the series the N and S alerts read. |
| [grafana/](grafana/) | Data-source and dashboard provisioning for the localnet profile. |

## Use

Copy `prometheus.yml`, `alertmanager.yml`, and `rules/`; replace the targets and keep the `node` label; run Prometheus
with 30 days of retention; fill the receivers. Import `dashboards/node.json` against a Prometheus data source whose uid
is `prometheus`, or re-point it on import. Severity is `page`, `warn`, or `notify`, and every rule from a guide cites
its alert ID in `ark_alert`, so routing and panels can name it.

```sh
make observability-check
```

runs `promtool check config` over both scrape files and their rules and `amtool check-config` over the routing
template, through the Prometheus and Alertmanager images the localnet profile runs.
`go test ./pkg/telemetry -run TestAlertRulesReadPinnedSeries` holds every `ark_*` series a rule reads to the
[exported-series fixtures](../../pkg/telemetry/README.md#exported-series-contract) or to the membership series the
protocol guide names, and every `ark_alert` to a condition in a guide, so a rename fails the build rather than
blanking a rule.

## Localnet

```sh
make localnet-observability
```

after `make localnet-start` runs Prometheus (`localhost:9095`), Alertmanager (`9096`), and Grafana (`3000`, anonymous
viewer; `admin` / `localnet` edits) behind the `observability` profile, scraping every node and sidecar with these
rules and the node dashboard provisioned. The probe holds Prometheus to the rules loaded and node0 scraped;
`make localnet-stop` takes the profile down with the rest.

## What waits

The protocol alerts A1 to C4 and D2, and the protocol dashboard, read series the
[indexer](../../docs/direction/TOOLING_DIRECTION.md#ark-indexer) exports and join here when it exists
([collection requirements](../../docs/operations/PROTOCOL_MONITORING.md#27-collection-requirements)). D1, D3, and D4
read the three series the membership service exports, named in
[protocol monitoring §7](../../docs/operations/PROTOCOL_MONITORING.md#7-disbursement). Prometheus, Alertmanager, and
Grafana are pinned in the [compose file](../localnet/docker-compose.yml) and the Makefile; move them together.
