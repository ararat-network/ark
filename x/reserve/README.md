# Strategic Reserve

Reserve owns strategic custody, mandates, position records, the quantity journal, recognition policy, and authorised transfers/burns. Treasury sizes the target and consumes recognised capital. Reserve reads Treasury capital requirements only where its own action bounds need them.


[Custody and journal](#mandate-custody-and-journal) · [Recognition](#recognition-algorithm) · [Symbols and freshness](#external-symbols-and-freshness) · [Queries and genesis](#queries-events-and-genesis) · [Development](#development)

## Code map

| Entry point | Responsibility |
| --- | --- |
| [keeper/position.go](keeper/position.go) | Open/closed positions and their lifecycle. |
| [keeper/ledger.go](keeper/ledger.go) | Accounting journal and return attribution. |
| [keeper/recognition.go](keeper/recognition.go) | Live recognition calculation. |
| [keeper/fund_transfer.go](keeper/fund_transfer.go) | Reserve-to-Buffer and Reserve-to-Insurance transfers. |
| [keeper/burn.go](keeper/burn.go) | Burn authorisation and execution. |
| [keeper/feed_guard.go](keeper/feed_guard.go) | Feed dependencies of recognition entries. |

[keeper/keeper.go](keeper/keeper.go) declares the collections and narrow keeper dependencies.
[msg_server.go](keeper/msg_server.go) and [grpc_query.go](keeper/grpc_query.go) are the transaction/query boundaries;
[genesis.go](keeper/genesis.go) owns import/export. [module/depinject.go](module/depinject.go) wires dependencies,
[module/module.go](module/module.go) registers services and hooks, and [module/autocli.go](module/autocli.go) describes CLI exposure.

## State and integration

`OpenPositions`, `ClosedPositions`, `Ledger`, `RecognitionPolicy`, and `ReversedReturns` separate current positions, permanent records, and policy. `Mandate` and `AllowanceUsed` track delegated authority. Valuation is calculated from current inputs rather than stored as a cached capital figure. App wiring sets the Treasury reader after construction.

## Mandate, custody, and journal

The mandate contains an exact committee, term window, gross deployment allowance, minimum NOAH balance, and approved
destinations. Deployments consume allowance permanently for the term, enforce the remaining balance floor, and send
only to those destinations. Recognition policy outlives appointments. There is no separate guardian or pause state:
governance replaces or disables the mandate. Per-transaction/rolling caps, slippage limits, and adapter pins were
rejected because the bounded on-chain outflow and the off-chain custody relationship have different evidence.

A `Position` holds attested quantity and venue reference beside proven deployed/returned amounts, impairment, and
opening/closing heights. Every mutation appends an `AccountingEntry`; corrections append a restatement rather than
editing history. Outflow is a Bank fact. Holdings are attestations. An inbound transfer is a Bank fact, but assigning
it to a position is an accounting act. Return value is crystallised at attribution with a fresh rate; realised profit
or loss is returned minus deployed at closure. Governance corrections cannot rewrite proven movements or their dates.

Closing a loss-making position is permitted: the loss was already visible, and closure removes credit. A rule blocking
loss closure would encourage stale credit. Close-and-reopen is not a correction mechanism, since opening requires a
real allowance-consuming deployment. No deployment veto window exists; governance pre-approves payees and prices its
veto budget through the term allowance. The [economic custody model](../../docs/ECONOMIC_DESIGN.md#73-recognition)
and [action bounds](../../docs/ECONOMIC_DESIGN.md#75-bounds-on-committee-acts) own the shared financial contract.

An inflated quantity can inflate recognised credit, but cannot itself transfer or mint coins. Credit is bounded by
haircuts, caps, and fresh rates; outflows remain allowance-, floor-, and destination-bounded. The permanent journal
makes the discrepancy inspectable and correctable. Committee attestation is the evidence model for off-chain custody;
typed deployment adapters cannot prove a custodian's wire-transfer holdings.

## Recognition algorithm

`RecognisedCapital` is a local read, not a stored valuation. It folds open, unimpaired positions under governance's
eligibility policy, valuing quantity through Oracle and applying haircut and share caps. The [economic recognition
formula](../../docs/ECONOMIC_DESIGN.md#73-recognition) defines the financial meaning of those caps.

The self-referential cap has a unique fixed point because the sum of policy ratios is strictly below one.
Sort each asset's raw-credit-to-cap-ratio threshold; the clipped assets form a suffix. For each candidate suffix,
solve total capital from the NOAH balance plus unclipped credit divided by one minus the clipped ratios. Comparisons
use cross-multiplied integers, postponing division and flooring credits conservatively. The total is bounded by the
NOAH balance divided by one minus all ratios; attested quantities cannot enlarge that bound. No NOAH means no asset
credit. A missing or stale feed gives zero credit and tightens neighbouring share ceilings, never a fallback valuation.

Treasury reads one capital number to size expansion gaps. Reserve reads Treasury's capital requirements through a
reader installed after construction only where an action needs a bound. It does not acquire a second liability
calculator. A total/liquid pair was rejected: the Buffer is already the liability-scaled liquid tranche, and the
appointment floor is Reserve's own operational liquidity rule.

## External symbols and freshness

External holdings use `<feed>-<tag>`; native issued denominations have no dash. Eligibility requires the external
shape, making the policy and registry namespaces disjoint by construction, even when listing precedes asset
registration. A position may name an external holding or protocol paper acquired through a buyback, but paper earns
no recognition credit. A cross-module registration guard would reintroduce temporal state checks for a property the
strings already guarantee.

The feed always derives from the prefix: `abtc-cb` and `abtc-osl` use the same series with distinct custody policy.
There is no alias or derive-if-present fallback that could silently re-point a position when a feed activates.
A different price series needs a different prefix. Governance sizes the combined cap ratios of entries sharing a series.

Each entry supplies its own positive `max_rate_age`, bounded by `MaxRecognitionRateAge`; Oracle evaluates that request
through `GetRateSetWithin` and keys the answer by the requested symbol. Cadence is a feed property; acceptable age is
consumer risk policy. Oracle-wide per-denomination age overrides were removed because one series can serve conversion
and reserve recognition with different tolerances.

Feed guards derive two kinds of claims at read time: a credited eligibility entry, and an open position whose returns
still need attribution. Custody-only entries do not claim a feed, while open positions do even without an eligibility
entry. Claims disappear when the corresponding policy or position stops needing the feed; no mirrored index exists.
Bank custody admits NOAH and registered protocol paper, never external-shaped tokens: external holdings remain at
attested destinations, and no native mint path produces those symbols.

## Queries, events, and genesis

The [query schema](../../proto/ark/reserve/v1/query.proto) exposes mandate, balances, positions, ledger, recognition
policy, and capital decomposition with per-row reasons. Bank transfer/burn events record custody movements;
`EventLedgerEntryRecorded` records accounting mutations. There is no redundant transfer-history query.
Genesis preserves open and closed positions and their unpruned history, then recomputes proven position figures and
term usage from the ledger. IDs, correction references, position references, and closure rules must agree. Oracle
imports first so recognition entries can validate their feeds. Non-NOAH Bank balances must be registry members.

## Development

Run from the repository root:

```sh
go test ./x/reserve/...
```

Keeper suites build their fixtures in [keeper/keeper_test.go](keeper/keeper_test.go); types tests cover parsing and
validation directly. [simulation/](simulation/) holds this module's simulation factories, while
[testutil/](testutil/) holds shared test helpers and mocks. Use [application tests](../../app/README.md) and
[integration tests](../../tests/README.md) when changing behaviour across module boundaries.

Edit schemas under [proto/ark/reserve/](../../proto/ark/reserve/), then follow the [generation guide](../../proto/README.md).
The `types/` package mixes handwritten domain code with generated Go; do not edit generated files directly.

## API schemas

The authoritative service and event definitions are [transactions](../../proto/ark/reserve/v1/tx.proto),
[queries](../../proto/ark/reserve/v1/query.proto), and [events](../../proto/ark/reserve/v1/event.proto).
Keep exact fields and method inventories in those schemas; the sections above explain their behaviour and constraints.

## Related documents

- [Reserve custody and recognition policy](../../docs/ECONOMIC_DESIGN.md).
- [Committee operations](../../docs/ECONOMIC_COMMITTEE_RUNBOOK.md).
- [Application wiring](../../app/README.md).
