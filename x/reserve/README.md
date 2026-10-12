# Strategic Reserve

Reserve owns strategic custody, mandates, position records, the quantity journal, recognition policy, and authorised transfers/burns. Treasury sizes the target and consumes recognised capital. Reserve reads Treasury capital requirements only where its own action bounds need them.


[Custody and journal](#mandate-custody-and-journal) · [Recognition](#recognition-algorithm) · [Symbols and freshness](#external-symbols-and-freshness) · [Queries and genesis](#queries-events-and-genesis) · [Development](#development)

## Code map

| Entry point | Responsibility |
| --- | --- |
| [keeper/position.go](keeper/position.go) | Open/closed positions, their lifecycle, and return attribution. |
| [keeper/ledger.go](keeper/ledger.go) | Append-only accounting journal. |
| [keeper/recognition.go](keeper/recognition.go) | Live recognition calculation. |
| [keeper/fund_transfer.go](keeper/fund_transfer.go) | Reserve-to-Buffer and Reserve-to-Insurance transfers. |
| [keeper/burn.go](keeper/burn.go) | Burn authorisation and execution. |
| [keeper/feed_guard.go](keeper/feed_guard.go) | Feed dependencies of recognition entries and open positions. |

[keeper/keeper.go](keeper/keeper.go) declares the collections and narrow keeper dependencies.
[msg_server.go](keeper/msg_server.go) and [grpc_query.go](keeper/grpc_query.go) are the transaction/query boundaries;
[genesis.go](keeper/genesis.go) owns import/export. [module/depinject.go](module/depinject.go) wires dependencies,
[module/module.go](module/module.go) registers services and hooks, and [module/autocli.go](module/autocli.go) describes CLI exposure.

## State and integration

`OpenPositions`, `ClosedPositions`, `Ledger`, `RecognitionPolicy`, and `ReversedReturns` separate current positions, permanent records, and policy. `LedgerByPosition` indexes the ledger by position, so the position-filtered `Ledger` query reads only that position's entries; it and `ReversedReturns` are derived from the ledger at import, never exported. `Mandate` and `AllowanceUsed` track delegated authority. Valuation is calculated from current inputs rather than stored as a cached capital figure. App wiring sets the Treasury reader after construction.

## Mandate, custody, and journal

The mandate contains an exact committee, term window, gross deployment allowance, minimum NOAH balance, and approved
destinations. A NOAH deployment consumes allowance permanently for the term and enforces the remaining balance floor;
any other held denomination deploys in kind against the holding alone, since that capital was charged when it first went
out. Every deployment sends only to those destinations. Recognition policy outlives appointments. There is no separate
guardian or pause state: governance replaces or disables the mandate. There is no governance deployment message either,
since deployment needs the off-chain counterparty relationship only a committee holds. Per-transaction/rolling caps,
slippage limits, and adapter pins were rejected because the bounded on-chain outflow and the off-chain custody
relationship have different evidence.

A `Position` holds attested quantity, the time it was last attested, and venue reference beside proven deployed/returned
amounts, impairment, and opening/closing heights. Every mutation appends an `AccountingEntry`; corrections append a
restatement rather than editing history. Outflow is a Bank fact. Holdings are attestations. An inbound transfer is a
Bank fact, but assigning it to a position is an accounting act. Return value is crystallised at attribution with a fresh
rate; realised profit or loss is returned minus deployed at closure. Governance corrections cannot rewrite proven
movements or their dates. Venue and entry references are free-form evidence the chain never interprets, at most 2,048
bytes (`chain.MaxReferenceBytes`); a venue reference is always required, an entry reference when the entry moves value
or attests a holding.

Closing a loss-making position is permitted: the loss was already visible, and closure removes credit. A rule blocking
loss closure would encourage stale credit. Close-and-reopen is not a correction mechanism: a NOAH reopening consumes
allowance again, and an in-kind reopening is booked at the live Oracle value. No deployment veto window exists;
governance pre-approves payees and prices its veto budget through the term allowance. The [economic custody model](../../docs/economics/ECONOMIC_DESIGN.md#43-recognition)
and [action bounds](../../docs/economics/ECONOMIC_DESIGN.md#45-bounds-on-committee-acts) own the shared financial contract.

Burn authority splits by what a burn can destroy. Governance's `MsgBurnReserveAssets` may burn any custody, NOAH
included, keeping the NOAH above a minimum the proposal states: a NOAH burn of chosen size is the protocol's one
discretionary supply contraction. The committee burns registry paper that carries no credit with
`MsgCommitteeBurnPaper`, which retires liability and cannot lower recognised capital, and NOAH surplus with
`MsgCommitteeBurnSurplus`, within the action bound. Burns open no position and append no journal entry; the signed
message and Bank's burn event are the record.

An attestation expires. `attested_time` is set when the committee states the whole holding: at opening, at a quantity
restatement, and at a return attribution, which states the remainder. Funding an existing position states only the
increment and corrections fix history, so neither moves it. Each entry's `max_attestation_age`, positive and at most
`MaxAttestationAge` (a year), bounds how old that statement may be and still earn credit; past it the position's
quantity reports as `stale_quantity` and counts for nothing until the committee restates it, which may repeat the same
quantity. A time ahead of the block, which only an edited genesis could carry, also earns nothing. The window forces the
diligence a quiet custodian failure would otherwise escape; it is no defence against a committee willing to re-attest
falsely, which the journal and its references exist to expose. Governance has no restatement message, and its correction
fixes a record without stating custody, so credit that lapses while no committee is seated stays lapsed until one is
appointed and restates each position.

An inflated quantity can inflate recognised credit, but cannot itself transfer or mint coins. Credit is bounded by
haircuts, caps, fresh rates, and fresh attestations; outflows remain allowance-, floor-, and destination-bounded. The
permanent journal makes the discrepancy inspectable and correctable. Committee attestation is the evidence model for
off-chain custody; typed deployment adapters cannot prove a custodian's wire-transfer holdings.

## Recognition algorithm

`RecognisedCapital` is a local read, not a stored valuation. It folds open, unimpaired positions under governance's
eligibility policy, valuing quantity through Oracle and applying haircut and cap. The [economic recognition
formula](../../docs/economics/ECONOMIC_DESIGN.md#43-recognition) defines the financial meaning of those caps.

Each asset's credit is the smaller of its haircut value and its `max_credit_per_noah` × the NOAH balance, floored to
whole anoah. Caps read only the balance, so credits are independent: a misreported quantity or a wrong rate changes its
own row and no other. The policy's values sum to at most one, so recognised capital is at most twice the NOAH balance
whatever is attested, and no NOAH means no asset credit. A missing or stale feed gives zero credit, never a fallback
valuation.

Caps read the balance rather than recognised capital on purpose. A cap measured against a total that includes the
credits it caps lets one asset's credit raise every other asset's ceiling: with 100 NOAH and gold capped at a half,
honest recognition is 200, and one fictitious entry capped at 0.4 takes it to 1,000. A flat amount decays into a binding
or vacuous bound as the balance sheet moves, and a share of the requirement lets a corrupted attestation against an
empty fund manufacture credit up to a share of a large target. Basel III's liquidity coverage ratio anchors its Level 2
limits to Level 1 the same way.

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
There is no alias or derive-if-present fallback that could silently re-point a position when a feed activates. A
different price series needs a different prefix. Governance sizes the combined maximum credit per NOAH of entries
sharing a series. Listing requires the derived feed Active, not merely scheduled, as registration does: an entry's
haircut and window judge how a series behaves, and a series scheduled for removal has already passed its guard.

Each entry supplies its own positive `max_rate_age` in seconds, bounded by `MaxRecognitionRateAge`; Oracle evaluates
that request through `GetRateSetWithin` and keys the answer by the requested symbol. Cadence is a feed property;
acceptable age is consumer risk policy. Oracle carries no per-denomination age override, because one series can serve
conversion and reserve recognition with different tolerances. Each `RecognisedCapital` row reports its entry's
window; added to the feed's `block_time` from Oracle's `Query/StoredRates`, it gives the time the credit lapses. The
row reports `max_attestation_age` the same way; added to each position's `attested_time`, it gives when that
position's credit lapses.

Feed guards derive two kinds of claims at read time: every eligibility entry, and every open position the feed prices,
external custody and Ark paper alike. Open positions claim a feed even without an eligibility entry. Claims disappear
when the corresponding policy or position stops needing the feed; no mirrored index exists. Bank custody admits NOAH and
registered protocol paper, never external-shaped tokens: external holdings remain at attested destinations, and no
native mint path produces those symbols. Paper is admitted from any sender because the Reserve is the one account whose
committee can retire it; refusing it would only push it where the chain cannot see it. Anyone may push member dust in,
which the committee can burn.

## Queries, events, and genesis

The [query schema](../../proto/ark/reserve/v1/query.proto) exposes mandate, balances, positions, ledger, recognition
policy, and capital decomposition with the inputs that explain each row, and the committee's surplus burn bound, sized
as the burn handler sizes it. Bank transfer/burn events record custody movements; `EventLedgerEntryRecorded` records
accounting mutations. There is no redundant transfer-history query. Genesis preserves open and closed positions and
their unpruned history, reconciles the proven position figures and term usage against the ledger, refusing a mismatch,
and rebuilds the per-position and reversal indexes. IDs, correction references, position references, and closure rules
must agree. Attested quantity and `attested_time` import as stated, since the journal records heights rather than times
and attestations are not proven figures. Positions and entries are permanent: IDs run from one without a gap and each
sequence sits one past its records, so an export that lost a record cannot import. Oracle imports first so recognition
entries can validate their feeds. Non-NOAH Bank balances must be registry members.

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

Errors are registered with their gRPC codes, so a query returns the keeper's error and the code follows it: a
missing position is `NotFound`, on the transaction path as on the query path, and an operation that needs an open
position reports a closed one the same way. Every other failure in the module is a plain error, since nothing
branches on it. Handlers classify only the request itself.
On a transaction path, wrap with `errorsmod.Wrapf` when the cause may carry a code: the SDK finds a transaction's
ABCI code only through its own wrapper, so a standard-library wrap keeps the text but reports internal.

## Related documents

- [Reserve custody and recognition policy](../../docs/economics/ECONOMIC_DESIGN.md).
- [Committee operations](../../docs/governance/ECONOMIC_COMMITTEE_RUNBOOK.md).
- [Application wiring](../../app/README.md).

## Recognition arithmetic and action bounds

Each ceiling is one checked multiplication of a ratio by the whole NOAH balance, exact in `LegacyDec`, and both ceiling
and credit truncate to whole anoah. The fuzz test matches every credit to the exact rational floor and checks that
removing any asset leaves every other credit unchanged.

`MaxAttestedQuantity` limits each position to 2^128 base units. NOAH valuation multiplies by the Oracle rate, bounded
by `MaxExchangeRate`; it does not divide by a tiny rate. Per-denomination sums and products retain checked arithmetic
because recognition errors during EndBlock fail the block. Boundary tests combine multiple capped attestations with
the maximum rate. Policy and position validation reject invalid inputs before this block-time fold.

Return reversals subtract the original booked value, including for closed positions. Repricing a reversal would allow
rate changes to create gains by reversing and re-attributing one receipt. Deployment basis and consumed gross term
allowance are not reversible. Corrections preserve a position's open or closed store and journal their target entry.

Incomplete liability valuation prevents a Reserve surplus burn, but Buffer and Insurance refill still use the gaps
Treasury can value. Fund transfers preserve the mandate's NOAH floor; governance transfers read custody only. Paper
burns require registry membership and no recognition credit. Registry denominations and external eligibility symbols
are disjoint, but the credit check remains a defensive bound on the committee's otherwise unbounded paper burn.
