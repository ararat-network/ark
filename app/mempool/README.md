# Application mempool

`Pool` wraps the SDK priority/nonce index with authenticated lane reservations and original-wire-byte accounting.
CometBFT's flood mempool owns gossip, duplicate tracking, RPC broadcast, and recheck scheduling.
This README owns lane eligibility, reservations, operating limits, and proposal policy.
[Node operations](../../docs/operations/NODE_OPERATIONS.md) covers setup; [telemetry](../../docs/operations/PROCESS_MONITORING.md) covers monitoring.

## Code map

| File | Responsibility |
| --- | --- |
| [lanes.go](lanes.go) | Privilege registry, top-level message classification, vouches, and context lane. |
| [limits.go](limits.go) | Configuration vocabulary, validation, and count/byte/gas shares. |
| [admission.go](admission.go) | Capacity check inside the disposable ante branch. |
| [pool.go](pool.go) | SDK index wrapper, allocation metadata, insertion, snapshots, and deferred removal. |
| [proposal.go](proposal.go) | Disposable proposal index and delegation to the SDK proposal loop. |
| [selector.go](selector.go) | Selected-resource accounting for preferential service. |

## Admission, service, and SDK behaviour

These are local admission and proposal rules except where shared ante checks affect consensus execution.

Ark uses the Cosmos SDK's `PriorityNonceMempool` through `baseapp.SetMempool`.
CometBFT runs its standard flood mempool for gossip, duplicate tracking,
RPC broadcast, and post-commit rechecks. The application extension adds authenticated
committee/governance priority, reserved pending capacity, and bounded preferential
block service. An SDK priority comparator orders the pair (lane, fee priority),
without compressing the int64 fee range. Within that policy, ties, first-signer nonce
indexing, replacement, transaction validation, and recheck execution follow the SDK. There is no separate Ark ready heap,
all-signer scheduling graph, or application revalidation loop.

`app/mempool.go` builds the shared privilege registry. Ante checks every top-level
message against the same class after signature verification. Committee candidates
must pass current mandate authorisation. Governance priority covers votes on active,
unexpired proposals, submissions and deposits that meet x/gov's initial and per-deposit
ratios from spendable balance, and cancellations by the proposer; expected ineligibility
uses normal priority. Mixed classes and authz wrappers use normal priority. The existing recursive
vote floor, fee policy and execution-time authorisation remain independent validity
rules. The privilege decorator puts the authenticated lane in SDK context, alongside
fee priority; no internal lane event is required.

`app/mempool.Pool` wraps the SDK index and accounts for pending count and original
encoded bytes. `app/mempool/limits.go` reserves 5% independently for committee and
governance count and byte capacity. Normal admissions use the remaining 90%, including
integer rounding. An eligible transaction can overflow into normal storage. Reservations
have no additional per-sender quota or repeated-vote rule: one eligible sender can fill
its class's reservation. This protects classes against normal traffic, not against
other eligible members of the same class.

The app's ante wrapper checks authenticated capacity after the ordinary ante chain
succeeds, while SDK RunTx still owns its disposable ante branch. A reservation refusal
therefore discards the candidate's fee, sequence and unordered-replay writes. The SDK
then performs its ordinary pool insertion and post-handler call. Ark does not change
SDK rollback semantics for unrelated failures after successful ante. The current
transfer-tax post handler is execution/simulation-only. Node-local admission bounds
never run during recheck, proposal verification, simulation, or finalisation. Recheck does drop a
pending vote or deposit whose proposal has left the period x/gov accepts it in, through the ante
vote decorator's check-mode refusal, so a stale vote never spends its lane slot on a failing inclusion.

The start command requires `type = "flood"`, `recheck = true`, and CometBFT `size`
at least the app's transaction count. Both pools receive the same positive
`max_tx_bytes` (default 1 MiB) and `max_txs_bytes` (default 64 MiB). CometBFT checks
wire size before decoding; the app also checks it in the reservation wrapper. A `max_tx_bytes` above what normal
admission holds of `max_txs_bytes` is refused, since such a transaction could never be admitted.
`app.toml [mempool] max-txs` defaults to 5000; zero selects that bounded default;
negative values and values above 50000 are refused. Existing byte settings are
honoured, including the former CometBFT 1 GiB default. These settings bound encoded
storage, not the additional memory used by decoded objects and SDK indexes.

There is one reservation-specific removal exception. SDK removal during speculative
finalisation or proposal filtering cannot free application capacity while CometBFT's
flood list still holds the bytes: successive normal admissions could otherwise fill
the transport list and prevent privileged admission. The wrapper defers these removals.
`FinalizeBlock` records the actual block's transactions; the SDK's post-commit
`PrepareCheckStater` releases those entries under CometBFT's mempool lock. CometBFT
then updates its list and calls SDK `CheckTx(Recheck)`. Failed ante rechecks remove
entries through the normal SDK callback; a passing recheck marks the entry as still
listed. CometBFT can refuse a transaction after the app admitted it, when its list is
full, and nothing then rechecks that entry, so the post-commit release also drops any
entry two consecutive recheck rounds passed over; the second round is slack for a
timed-out recheck. There is no second recheck, reinsertion,
or lane refresh; surviving priority and storage allocations remain those captured
at insertion, as with SDK priority. CometBFT's local ABCI client and commit/update lock
supply lifecycle synchronisation.

`Pool` protects its index and metadata with a mutex; callbacks run outside that lock.
The SDK index keeps a skiplist, with its own random source, for every sender it has ever
held and never frees an empty one, about 6 KB per departed sender. The pool counts senders
whose last pending entry left and, at the post-commit release, rebuilds the index from its
own entries once they would outnumber the live entries, with a floor of 4096; an empty pool
rebuilds for free. Entries re-enter with their stored lane and priority, so SDK order is
unchanged, and retained index garbage never exceeds the live index.
Proposal construction snapshots pending entries outside validation callbacks and builds
one disposable SDK priority/nonce index using the current proposal's resource limits.
A small SDK `TxSelector` independently caps committee and governance service at 5% of
block gas and available protobuf transaction bytes. Unused lane capacity remains available
to normal transactions. The selector remains the authority; an entry that fails
verification leaves its share unused for that proposal.

A privileged transaction past what its lane has left in pool order, including one larger
than the whole allowance, spills to normal fee priority in this proposal: it lands in
capacity normal traffic leaves unused and competes by fee when the block is contested.
An ordered sender's later entries follow its first spilled one, even when they would fit,
so none outranks its predecessor; the lane's remaining room goes to other senders. A
spilled entry is verified like any normal candidate, so a full block bounds the work as it
does for normal traffic. Spilling uses SDK-encoded bytes and the actual budget after the
oracle envelope; it changes neither the transaction's pending admission allocation nor its
stored lane or fee priority. Mixed batches compete as normal too: the cap limits
privileged service, not all committee messages or votes in a block.

The SDK default PrepareProposal handler owns the complete selection loop, including
verification before resource filtering, selected-sequence bookkeeping for every signer,
encoding, and unordered handling. The SDK iterator preserves primary-signer nonce order;
first-signer ordering may defer multi-signer transactions whose other predecessor comes
later. Ark adds no dependency graph, eligibility refresh, or second verification pass.
The disposable index is separate from pending storage, so SDK callbacks acquire no live
index lock when touching application metadata.

Lane budgets and proposal ordering are local policy. The privilege decorator still runs
in the shared execution ante chain, so changing the vouch set changes the gas its reads
charge in execution as well. Deploying such a change to an existing chain must be
coordinated; it is not solely a local mempool update.

The oracle wrapper reserves space for its authenticated extended commit before ordinary
selection. The SDK default ProcessProposal verifies ordinary transactions and the
block gas bound independently of local pool occupancy or preferred order. Consensus
execution remains the same Ark ante/message/post path. Private carriers remain optional
for pre-inclusion confidentiality; ordinary public nodes retain normal gossip.

CometBFT unconfirmed-transaction RPCs report its flood list. Ark's transaction and byte
gauges report application storage by allocation; accepted/invalid/full counters observe
SDK CheckTx responses, including CometBFT rechecks, both from [metrics/](metrics/). The
application pool can hold an entry
the flood list refused after admission for up to two commits before the release above drops
it, and the list keys by hash, so it holds the superseded version of a replaced transaction
until a recheck fails it; the two occupancies need not match at every instant.

Validation compares ordinary admission, failures, replacement, ordering and proposal
output with the unmodified SDK. Reservation regressions cover saturation, independent
byte/gas caps, spill to ordinary priority, commit headroom, unlisted-entry release, and three-peer public propagation. Initial shares
still require workload calibration; they do not guarantee inclusion latency or prevent
validator censorship.

## Verification

From the root:

```sh
go test ./app/mempool/... ./app/ante/...
go test ./app
```

The package tests cover reservation accounting, selector budgets, rollback, and SDK ordering. App tests cover the actual
ABCI lifecycle and public propagation. Run focused race checks when changing concurrent access. Compare normal traffic
against the SDK's behaviour before introducing a custom scheduling or validation path.

[Process monitoring](../../docs/operations/PROCESS_MONITORING.md) explains occupancy metrics; [ABCI](../../abci/README.md) documents the oracle envelope.
