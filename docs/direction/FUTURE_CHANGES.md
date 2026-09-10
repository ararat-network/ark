# Future Changes

For contributors considering deferred code and dependency changes. Each item distinguishes shipped foundations from
unimplemented work and states its revisit trigger. [Governance operations](../governance/GOVERNANCE_OPERATIONS.md) owns actions
available through existing messages and relayer procedures.

## Contents

- [1. Upgrade to Cosmos SDK v0.55](#1-upgrade-to-cosmos-sdk-v055)
- [2. Enable Block-STM](#2-enable-block-stm)
- [3. Basket index assets](#3-basket-index-assets)
- [4. Capital and protocol follow-ups](#4-capital-and-protocol-follow-ups)
- [Protocol monitoring tooling](#protocol-monitoring-tooling)

## 1. Upgrade to Cosmos SDK v0.55

- Status: deferred on Wasmd compatibility.
- Current SDK: v0.54.3.
- Trigger: an upstream Wasmd release that compiles against SDK v0.55 without a fork or replacement directives.

The v0.55 move was built and reverted rather than carried on a fork. The recorded blocker is Wasmd's remaining
imports of the SDK's retired `x/params` in four files: it compiles against v0.54.3 but not v0.55.0. Recheck that
dependency gate when revisiting the upgrade, then redo the move against compatible upstream dependencies.

SDK v0.55 also introduces consensus-key rotation. Keep persistent validator state keyed by operator address;
consensus addresses are rotatable credentials and belong at attribution boundaries. Audit remaining consensus-address
keys when revisiting the upgrade so rotation does not strand validator state.

## 2. Enable Block-STM

- Status: not wired; requires the SDK move in §1.
- Trigger: compatible upstream dependencies, followed by the audit and benchmarks below.

The upgrade makes the operator-facing Block-STM configuration available. Enabling parallel execution remains a
separate step after the SDK move, with the sequential executor as the default until the audit and benchmarks justify
rollout. The implementation notes below were recorded on 2026-08-29; recheck the upstream API and Ark's transaction
paths against the versions selected for the upgrade.

### 2.1. Keep the block gas meter off

Ark's current gas-meter policy is recorded in [Block gas-meter policy](../../app/README.md#block-gas-meter-policy). The meter is mutually
exclusive with Block-STM, in a way that can split a network. `SetBlockSTMTxRunner` panics if the meter is still enabled,
and v0.55's `blockexec.Apply` force-disables the meter on that node whenever `block-executor = "block-stm"` is selected.
Since the executor is per-node operator configuration, a chain that ships with the meter enabled and a validator who
flips the executor produce different execution for the same block the first time a block approaches the gas limit.
That is a consensus failure sourced from a config file.

Do not re-add `EnableBlockGasMeter()`: re-enabling it forecloses Block-STM.

### 2.2. What SDK v0.55 adds for Block-STM

The engine is not new. `baseapp/txnrunner` and the `SetBlockSTMTxRunner` hook shipped in v0.54.x, wired programmatically
per chain. v0.55.0 adds the operator-facing surface (upstream #26208):

- `app.toml` keys: `block-executor` (`"sequential"`, the default, or `"block-stm"`), `block-stm-workers` (defaults to
  `min(GOMAXPROCS, NumCPU)` when unset or non-positive), and `block-stm-pre-estimate`.
- `baseapp/blockexec.Apply`, which resolves those keys and installs the matching `TxRunner`.

v0.55 also carries the correctness work that makes the engine worth trusting: the lost-update fix in validation
scheduling (#26583), the `CancelAll` cancellation fix (#25893), an estimate panic guard (#26627), `Has()` existence
tracking to cut false conflicts (#26467), pre-state caching (#25909), and an `x/distribution` fix for recovered panics
under speculative execution (#26518).

Selecting an executor is a per-node choice with identical state-transition results. It is not consensus-breaking on its
own — the gas meter interaction in §2.1 is what makes it dangerous to leave half-decided.

### 2.3. Wiring, when the SDK move lands

`Apply` is safe to call unconditionally: with the default sequential executor it leaves BaseApp's lazy default runner in
place. Call it after the store keys exist:

```go
stores := make([]storetypes.StoreKey, 0, len(keys))
for _, k := range keys {
    stores = append(stores, k)
}
blockexec.Apply(bApp, appOpts, stores, txConfig.TxDecoder(), coinDenomFn)
```

The final argument resolves the fee denom from the multistore at runtime; it feeds the pre-estimate path, which
speculates the fee-payment write set before execution. Ark's reference denom is governance-mutable through
`MsgSetReferenceDenom`, so this function must read live state rather than close over a constant captured at wiring time.

Ark passes its own `baseapp.SetMempool` and `baseapp.SetOptimisticExecution` options; optimistic execution and the STM
runner operate at different layers (OE decides *when* FinalizeBlock work starts, STM decides how transactions inside it
are scheduled) and compose, but the combination should be exercised explicitly during rollout rather than assumed.

### 2.4. Ark-specific contention profile

Block-STM preserves sequential semantics — conflicting transactions re-execute rather than producing wrong state — so
contention costs throughput, not correctness. Ark's known serialization points, worth measuring before expecting gains:

- **Wasm tx counter.** `wasmkeeper.NewCountTXDecorator` in `app/ante.go` read-modify-writes a single per-block counter
  key on every Wasm transaction. Every Wasm transaction in a block therefore conflicts with every other one.
- **Fee deduction and the transfer-tax charge.** Every fee-paying transaction touches the fee collector in the ante
  and, when taxed, the tax collector in the post handler. This is the broadest conflict set in the chain and it scales
  with block fullness. The finalisation-only tax collection amendment in [tax commitment](../../app/ante/README.md#transfer-tax-commitment-and-rollback) leaves this
  conflict set unchanged.
- **Market swaps.** Transactions against a shared pool serialize against each other by construction. Independent
  denominations parallelize.
- **Oracle prevotes and votes.** Keyed per validator, so these parallelize well. Note that oracle aggregation runs in
  the ABCI pipeline (`abci/oracle`), not as parallel transaction execution, and is unaffected either way.

### 2.5. Speculative execution hygiene

Under Block-STM a transaction can execute against stale state and be re-run, so anything reachable from a transaction
handler must tolerate being executed more than once for one logical transaction:

- **No panics on absent state.** A speculative read can legitimately miss state a later incarnation will see. Return
  errors; do not panic. Upstream #26518 is the cautionary case.
- **No package-level mutable state, and no metrics from transaction paths.** A counter incremented in a handler fires
  once per incarnation, not once per transaction. As of 2026-08-29 Ark's module metrics live only in
  `x/*/keeper/abci.go` Begin/EndBlocker paths, which stay sequential — so there is nothing to fix today, but new
  metrics belong in ABCI paths, not in msg servers.
- **Rounding and arithmetic are unaffected.** The determinism rules in `CLAUDE.md` already forbid the constructs that
  would break under re-execution.

### 2.6. Enablement checklist

1. Complete the SDK v0.55 move (dependency gate: §1).
2. Add the `blockexec.Apply` call in `app/app.go`, leaving `block-executor` at its `sequential` default. Confirm no
   behavior change.
3. Audit msg-server paths against §2.5 — principally for panics on absent state.
4. Benchmark a representative transaction mix (Wasm, Market swaps, transfers, oracle votes) sequentially, then with
   `block-executor = "block-stm"`, on a non-validator node. §2.4 predicts where the wins are not.
5. Roll out to validators only after the benchmark justifies it. Confirm no node anywhere re-enables the block gas
   meter (§2.1).

## 3. Basket index assets

- Status: designed 2026-09-10, not built. Launch keeps `axdr`, a voted feed, as the protocol reference and lists no
  basket asset.
- Trigger: governance intends to list the flagship currency, a basket-priced asset that later becomes the protocol
  reference. Build the whole feature then; nothing below is worth shipping inert.

### 3.1 What was decided

A basket is a derived feed owned by `x/oracle`: a denomination in the priced-denom grammar whose rate is composed on
chain from voted component feeds and is never voted itself. Composition runs in the preblock after the tally writes
component rates, as `Σ quantityᵢ × rate[componentᵢ]`, through the ordinary rate store, so every consumer reads it
unchanged. The stored timestamp and height are the oldest input's, so under any consumer's window the basket is exactly
as stale as its weakest component; a component with no stored rate skips the write. A sum above `MaxExchangeRate` or an
unrepresentable product omits the write rather than halting, as the tally omits an unrepresentable derived price, and
domain caps on component count and quantity make that omission unreachable.

A defined basket answers `FeedPhase` Active, so asset registration, the reference re-point, and Reserve eligibility
entries accept it with no change to those modules. The basket pins its component feeds against removal, checked
oracle-internally ahead of the registered guards as the reference denom is; the basket itself is removable only when
`requireFeedUnreferenced` passes. `MsgAddFeed` refuses a basket denom and basket creation refuses a voted feed denom:
one namespace, two disjoint sets. The vote target set, the vote-extension codec, and the sidecar never see a basket.

Composition changes are in place and value-neutral. `MsgCreateBasket` takes literal quantities and requires the basket
not to exist; nothing references a new basket, so its starting value is free. `MsgRebalanceBasket` takes value shares
summing to one, may add or drop components, and derives quantities at proposal execution:

    V        = Σ q_old[i] × r[i]
    q_new[j] = floor(w[j] × V / r[j])

Every rate is fresh under the conversion window, except a governance-stated outgoing rate, accepted only for a dropped
component the Oracle cannot currently price; an override for a fresh or continuing component is refused. The voting
period is the notice window, a failure fails the proposal, the version advances, and one event carries the old
quantities, the new quantities, and V. Holders act on nothing: at the switch a unit is worth what it was worth the block
before, under the new composition. The successor path (new basket, new asset, halt the old, cross swap, retire) needs no
code and stays the route for a deliberate revaluation. `MsgRemoveBasket` prunes the stored rate.

Rejected: sidecar composition (every weight change becomes a fleet coordination event, `x/oracle/README.md` §1.2); a
successor denom as the ordinary rebalance (the old exposure outlives the decision to leave it and pins a dying series);
forced balance conversion (unbounded iteration in a halt-class path); nested baskets; and NOAH as a component (it prices
part of the basket at par with what mints it, and the priced-denom rule already refuses it).

### 3.2 Reserved identifiers

Store prefix 7 in `x/oracle/types/keys.go` and `GenesisState` field 9 (field 8 is burned; see the comment there).
Messages `MsgCreateBasket`, `MsgRebalanceBasket`, `MsgRemoveBasket`; queries `Basket`, `Baskets`; constants
`MaxBaskets`, `MaxBasketComponents`, `MaxBasketQuantity`.

### 3.3 Delivery after launch

Additive: a consensus-version bump on `x/oracle` with a no-op migration, one new `OracleKeeper` method,
`ComposeBaskets`, called in the preblock between `ProcessVoteExtensions` and `AdvanceFeeds`, no vote-extension change,
no sidecar release. Docs that move with it: `x/oracle/README.md`, `FUTURE_CHANGES.md`, `x/asset/README.md`,
`ECONOMIC_DECISIONS.md` D22, `THREAT_MODEL.md` §2.1 and §2.7, both ABCI READMEs.

### 3.4 Making the flagship the reference

Create the basket, let it print, register the asset, then `MsgSetReferenceDenom` re-points from `axdr` to it, rebasing
the pool, the tax cap, the factor table, and the exposure anchor at one fresh cross. The asset's lifecycle stays
independent of the reference, as it is for any denomination today. Once unreferenced, `axdr` may be removed as a feed.
Later rebalances of the reference need no rebase, because the basket's NOAH value is continuous across them.

## 4. Capital and protocol follow-ups

These are deferred engineering changes, not current subsystem contracts.

| Work | Trigger and retained constraint |
| --- | --- |
| Per-fund exposure model | A fund carries material risk that does not scale with liability. Current deployment risk is already handled by recognition haircuts/caps; otherwise a separate model collapses to the existing ratios and multiplier. |
| Recognised-capital block cache | Benchmarks at realistic position/entry counts show the recognition fold is material. Do not revive the removed liability snapshot or its invalidators. |
| Shared target arithmetic | A second independent consumer appears; the retired capital-accounting consolidation had no remaining duplicated arithmetic after block settlement. |
| Exposure poller/indexer/dashboard | Deploying protocol-state monitoring; see [delivery status and prerequisites](#protocol-monitoring-tooling). |
| Committee reference re-point | A concrete need for committee-speed unit changes. Ship only with a governance-pre-approved successor slot; the committee executes a unit choice governance already made. |
| Orientation-bearing `Rate` type | A second stored-price representation creates a demonstrated ambiguity. Keep one on-chain orientation meanwhile. |
| Recognition row age disclosure | Operators need the judged age beside each capital row. Retain consumer-owned staleness tolerances and Oracle-owned freshness judgement. |
| Machine-authored Reserve evidence | A concrete chain-verifiable venue exists. Design which individual journal facts can be proven; do not claim a channel acknowledgement proves off-chain custody. |

Recognition's 30-day maximum age and 12-character external tag remain reviewable before launch if operational evidence
requires different bounds. Exposure weights/cap calibration belongs to [protocol monitoring](../operations/PROTOCOL_MONITORING.md) and
[governance operations](../governance/GOVERNANCE_OPERATIONS.md); fee-controller and voting-floor launch choices belong to [genesis](../governance/GENESIS.md).

### Vote-extension-carried emergency orders

Status: deferred. Revisit if searcher/MEV infrastructure appears, one block of pre-suspension extraction becomes
catastrophic, the validator set outgrows the private-carrier trust model, or front-running can invalidate an action.
The current [submission runbook](../governance/EMERGENCY_SUBMISSION_RUNBOOK.md) remains the operating procedure.

The proposed stronger transport puts signed orders in vote extensions. If carriers hold more than a third of stake,
a valid injected commit cannot exclude every carrier; a valid order can then execute in the next preblock rather than
wait for ordinary proposer inclusion. The benefit must justify permanent preblock execution risk and carrier operations.
The existing suspension keeper does not need this transport to operate.

A future design starts with `EmergencyOrder{denom, term, expiry_height, signature}` and a domain-separated sign-doc,
per-order and aggregate bounds in `abci/codec`, a separate verification boundary, and deterministic execution order.
Honest ExtendVote handlers attach only self-verified orders, and VerifyVoteExtension must accept everything they emit.
Invalid/stale orders must skip without halting; state-failure handling needs an explicit audit. Require a coordinated,
two-phase rollout rather than treating this as a local mempool setting.

## Protocol monitoring tooling

Status: the protocol queries and typed events exist; the dedicated collector, indexer integration, and dashboard are
not delivered by this repository. Trigger: an operator is deploying protocol-state monitoring and choosing alert
thresholds. [Protocol monitoring](../operations/PROTOCOL_MONITORING.md#27-collection-requirements) owns polling cadence, indexed
events, retained history, panels, and calibration requirements. Implement those requirements without duplicating
consensus state in keeper metrics. Hosting, on-call ownership, and page thresholds remain operational decisions.

A possible `BurnableSurplus` query is separate chain work. Today the committee reconstructs the executable bound
using recognised capital, Treasury's required capital, and the mandate's NOAH floor. The A5 alert tracks the capital
surplus alone. Revisit a query if reliable operational tooling needs the keeper's full evaluated bound rather than
that reconstruction; define incomplete-valuation behaviour and add API/integration coverage with the change.
