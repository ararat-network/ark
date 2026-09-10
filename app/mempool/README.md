# Application mempool

`Pool` wraps the SDK priority/nonce index with authenticated lane reservations and original-wire-byte accounting.
CometBFT's flood mempool owns gossip, duplicate tracking, RPC broadcast, and recheck scheduling.
This README owns lane eligibility, reservations, operating limits, and proposal policy.
[Node operations](../../docs/NODE_OPERATIONS.md) covers setup; [telemetry](../../docs/PROCESS_MONITORING.md) covers monitoring.

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

Ark uses Cosmos SDK v0.54.3's `PriorityNonceMempool` through `baseapp.SetMempool`.
CometBFT v0.39.3 runs its standard flood mempool for gossip, duplicate tracking,
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
never run during recheck, proposal verification, simulation, or finalisation.

The start command requires `type = "flood"`, `recheck = true`, and CometBFT `size`
at least the app's transaction count. Both pools receive the same positive
`max_tx_bytes` (default 1 MiB) and `max_txs_bytes` (default 64 MiB). CometBFT checks
wire size before decoding; the app also checks it in the reservation wrapper.
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
entries through the normal SDK callback. There is no second recheck, reinsertion,
or lane refresh; surviving priority and storage allocations remain those captured
at insertion, as with SDK priority. CometBFT's local ABCI client and commit/update lock
supply lifecycle synchronisation.

`Pool` protects its index and metadata with a mutex; callbacks run outside that lock.
Proposal construction snapshots pending entries outside validation callbacks and builds
one disposable SDK priority/nonce index using the current proposal's resource limits.
A small SDK `TxSelector` independently caps committee and governance service at 5% of
block gas and available protobuf transaction bytes. A transaction that fits its entire
lane allowance but exceeds the remaining allowance waits; it cannot overflow into normal
service. Unused lane capacity remains available to normal transactions. Entries past a
lane's allowance in pool order stay out of the proposal index, because the SDK verifies
every candidate it iterates before the selector can decline it: a saturated lane costs
no signature checks beyond what it serves. An ordered sender's later entries wait with
the first one left out. The selector remains the authority; an entry that fails
verification leaves its share unused for that proposal.

An individual privileged transaction larger than its entire byte or gas allowance uses
normal fee priority in this proposal. This avoids permanently excluding a large action,
while withholding preferential service from it. The exception uses SDK-encoded bytes
and the actual budget after the oracle envelope; it changes neither the transaction's
pending admission allocation nor its stored lane or fee priority. Deliberately large
transactions and mixed batches can therefore compete as normal: the cap limits privileged
service, not all committee messages or votes in a block.

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
SDK CheckTx responses, including CometBFT rechecks. The application pool can conservatively
retain entries absent from the flood list, for example following an SDK post-handler
failure; transport/application occupancy need not be identical at every instant.

Validation compares ordinary admission, failures, replacement, ordering and proposal
output with the unmodified SDK. Reservation regressions cover saturation, independent
byte/gas caps, oversized ordinary fallback, commit headroom, and three-peer public propagation. Initial shares
still require workload calibration; they do not guarantee inclusion latency or prevent
validator censorship.

## Verification

From the root:

```sh
go test ./app/mempool/... ./app/ante/...
go test ./app -run Mempool
```

The package tests cover reservation accounting, selector budgets, rollback, and SDK ordering. App tests cover the actual
ABCI lifecycle and public propagation. Run focused race checks when changing concurrent access. Compare normal traffic
against the SDK's behaviour before introducing a custom scheduling or validation path.

[Process monitoring](../../docs/PROCESS_MONITORING.md) explains occupancy metrics; [ABCI](../../abci/README.md) documents the oracle envelope.
