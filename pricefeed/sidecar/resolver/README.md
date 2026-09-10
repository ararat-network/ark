# Price resolver

[ResolvePrices](resolver.go) maps one provider snapshot into prices for requested feeds. It owns no goroutines, listeners,
or provider fetch loop. [config.go](config.go) validates routes and bootstrap prices; [runtime](../runtime/README.md)
filters provider freshness and supplies the current requested feeds.

A feed resolves to `UNIT/NOAH`: NOAH per one unit, matching the [on-chain orientation](../../../x/oracle/README.md#14-rate-orientation-noah-per-unit).
Routes are ordered pair legs ending in NOAH. Provider observations can arrive in either orientation; the resolver
normalises reciprocals before taking the provider median for a leg. It multiplies legs to resolve each route and averages
successful configured routes. Missing/empty route definitions use the direct pair path.

Bootstrap values apply only when a route leg has no provider samples and its configured expiry has not passed. They are
stated in the leg's orientation. A missing or invalid route does not invent a price for the feed. The resolver returns
only requested outputs that it can compute and records aggregation metrics from that pass.

The resolver keeps the chain orientation end to end and re-keys resolved output by base denomination. Inverting only
at the feed boundary was rejected because it left two numerical conventions in one pipeline. Providers retain their
venue's pair orientation; normalisation happens when their samples serve a route leg.

## Development

Run `go test ./pricefeed/sidecar/resolver` from the root. `resolver_test.go` covers reciprocal observations, medians,
routes and bootstrap fallback; `config_test.go` covers route validation. Use `resolver_benchmark_test.go` when changing
route/aggregation work. Keep arithmetic details and copying requirements beside their implementation rather than
repeating the oracle's consensus aggregation rules here: the on-chain tally is a different algorithm.
