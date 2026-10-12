# Future Changes

For contributors considering deferred code and dependency changes. Each item distinguishes shipped foundations from
unimplemented work and states its revisit trigger. [Closed items](#closed-items) records work decided against and what
alone would reopen it. [Governance operations](../governance/GOVERNANCE_OPERATIONS.md) owns actions available through
existing messages.

## Contents

- [1. Upgrade to Cosmos SDK v0.55](#1-upgrade-to-cosmos-sdk-v055)
- [2. Enable Block-STM](#2-enable-block-stm)
- [3. Basket index assets](#3-basket-index-assets)
- [Protocol monitoring tooling](#protocol-monitoring-tooling)
- [Closed items](#closed-items)

## 1. Upgrade to Cosmos SDK v0.55

- Status: deferred on Wasmd compatibility.
- Current SDK: v0.54.4.
- Trigger: an upstream Wasmd release that compiles against SDK v0.55 without a fork or replacement directives.

The blocker is Wasmd's imports of the SDK's `x/params`, which v0.55 drops, in four files: Wasmd compiles against v0.54.4
but not v0.55.0, and Ark does not carry a fork. Recheck that dependency gate when revisiting the upgrade, then make the
move against compatible upstream dependencies.

SDK v0.55 also introduces consensus-key rotation. Keep persistent validator state keyed by operator address; consensus
addresses are rotatable credentials and belong at attribution boundaries. No Ark state is keyed by consensus address.
The one resolution point, `RecordVoteAccounting` in `x/oracle/keeper/accounting.go`, resolves vote-extension signers
through the live index, which upstream swaps at the height CometBFT adopts the new key. Add a rotation case to the e2e
tier with the upgrade, and add a `ValidatorByHistoricalConsAddr` fallback only if it shows votes dropped across the
swap.

## 2. Enable Block-STM

- Status: not wired. Needs an SDK release Ark can take that carries the cancellation fix #25893 (§2.2).
- Trigger: block execution time is a measured bottleneck at the block gas limit. Then the prerequisites, audit, and
  benchmarks below.

Enabling parallel execution is a separate step from any SDK bump, with the sequential executor as the default until the
prerequisites in §2.4, the audit, and the benchmarks justify rollout. Recheck the upstream API described below and Ark's
transaction paths against the versions selected for the upgrade.

### 2.1. Keep the block gas meter off

Ark's current gas-meter policy is recorded in [Block gas-meter policy](../../app/README.md#block-gas-meter-policy). The
meter is mutually exclusive with Block-STM: `SetBlockSTMTxRunner` panics if the meter is still enabled, and
`blockexec.Apply` force-disables it on any node that selects `block-executor = "block-stm"`. Proposals are capped at
`max_gas` by declared gas, so the meter cannot trip on an accepted block and the disable splits nothing today. It does
make the meter a per-node setting, and taking it out after launch is a coordinated upgrade.

Do not re-add `EnableBlockGasMeter()`: re-enabling it forecloses Block-STM.

### 2.2. What the SDK provides

`baseapp/txnrunner` and the `SetBlockSTMTxRunner` hook shipped in v0.54.x, wired programmatically per chain. Upstream
#26208 adds the operator-facing surface in both v0.54.4 and v0.55.0:

- `app.toml` keys: `block-executor` (`"sequential"`, the default, or `"block-stm"`), `block-stm-workers` (defaults to
  `min(GOMAXPROCS, NumCPU)` when unset or non-positive), and `block-stm-pre-estimate`.
- `baseapp/blockexec.Apply`, which resolves those keys and installs the matching `TxRunner`.

Both releases carry the lost-update fix in validation scheduling (#26583), an estimate panic guard (#26627),
worker-count normalisation (#26591), and an `x/distribution` fix for recovered panics under speculative execution
(#26518). Only v0.55.0 carries the `CancelAll` fix (#25893), `ExecuteBlock` input validation (#26592), and two
performance changes: `Has()` existence tracking (#26467) and pre-state caching (#25909).

#25893 is the gate. Optimistic execution abandons a mismatched proposal by cancelling its context, and BaseApp waits for
the abandoned run before executing the real block. Without the fix, a cancelled STM run can re-suspend on an ESTIMATE
mark and never return, stalling that node. The performance changes matter only to the benchmark.

Selecting an executor is a per-node choice with identical state-transition results. It is not consensus-breaking on its
own — the gas meter interaction in §2.1 is what makes it dangerous to leave half-decided.

### 2.3. Wiring, when the SDK move lands

`Apply` is safe to call unconditionally: with the default sequential executor it leaves BaseApp's lazy default runner in
place. Call it in `NewArkApp` after the hand-wired Wasm `RegisterStores` and before `Load`, passing every mounted key:

```go
blockexec.Apply(app.BaseApp, appOpts, app.GetStoreKeys(), app.txConfig.TxDecoder(),
    func(storetypes.MultiStore) string { return chain.NoahBaseDenom })
```

The list must be complete. The STM view finds each store's slot with a plain map read, so a key missing from the list
silently reads and writes slot 0, the alphabetically first KV store. Treasury's gas tally and Market's accumulators are
transient stores written inside transactions. Passing `kvStoreKeys()`, or simapp's KV-only `keys`, would commit their
writes into another module's store and diverge the app hash from sequential nodes. The sequential executor ignores the
list, so only the equivalence replay in §2.6 catches an omission.

The final argument resolves the fee denom from the multistore at runtime; it feeds the pre-estimate path, which
speculates one `(fee payer, denom)` balance as each transaction's fee-payment write set before execution. Ark has no
single fee denom: each transaction pays its base fee in NOAH or a member stablecoin of its choosing, plus a NOAH tip,
and the reference denom is a feed unit that never pays a fee. Pre-estimation can therefore cover one leg at most, NOAH
being the natural choice since every tip is NOAH, or stay off with `block-stm-pre-estimate`; a misprediction costs
re-execution, never correctness.

Ark passes its own `baseapp.SetMempool` and `baseapp.SetOptimisticExecution` options; optimistic execution and the STM
runner operate at different layers (OE decides _when_ FinalizeBlock work starts, STM decides how transactions inside it
are scheduled) and compose, but the combination should be exercised explicitly during rollout rather than assumed.

### 2.4. Ark-specific contention profile

Block-STM preserves sequential semantics — conflicting transactions re-execute rather than producing wrong state — so
contention costs throughput, not correctness. Every transaction writes three keys, and while any of them stands,
Block-STM is sequential execution plus overhead. Removing them is prerequisite work, each a consensus change shipped at
an upgrade:

- **Wasm tx counter.** `wasmkeeper.NewCountTXDecorator` in `app/ante/ante.go` read-modify-writes one per-block counter
  key on every non-simulated transaction, Wasm or not. Wasmd reads the counter only for `env.transaction.index`, so a
  decorator that sets it from `ctx.TxIndex()` without a store write removes the key. The value changes from an
  ante-reached count to the block position, which contracts can observe.
- **Block gas tally.** `GasTallyDecorator` in `app/ante/gas_tally.go` adds each transaction's declared gas to one
  transient Treasury key. One key per transaction index, summed in Treasury's EndBlock, removes the conflict. The sum
  already saturates rather than failing, so nothing halt-class moves into the EndBlocker.
- **Fee and tax collectors.** Every fee-paying transaction credits the fee collector in the ante and, when taxed, the
  tax collector in the post handler. The finalisation-only tax collection amendment in
  [tax commitment](../../app/ante/README.md#transfer-tax-commitment-and-rollback) leaves this unchanged. Upstream bank's
  virtual accounts (`SendCoinsFromAccountToModuleVirtual`, credited by bank's EndBlocker from an object store) remove
  both keys. Treasury's reward funding reads both collectors in its EndBlocker, so bank's EndBlocker must then run ahead
  of Treasury's. `app/app_config.go` currently marks that order as carrying no intent.

One conflict is inherent and stays:

- **Market orders.** Every `MsgSell` or `MsgBuy` with a NOAH leg writes the single `ArkPoolDelta` item and the block's
  transient expansion or redemption accumulators, whatever the stablecoin, so those orders serialise against each other.
  Only stablecoin-to-stablecoin crosses skip the pool. The accumulators keep their running totals: they fail the
  contributing transaction on overflow rather than leaving it to EndBlock.

Oracle prices arrive in vote extensions and are aggregated in the ABCI pipeline (`abci/oracle`), not in transactions, so
they are unaffected either way.

### 2.5. Speculative execution hygiene

Under Block-STM a transaction can execute against stale state and be re-run, so anything reachable from a transaction
handler must tolerate being executed more than once for one logical transaction:

- **No panics on absent state.** A speculative read can legitimately miss state a later incarnation will see. Return
  errors; do not panic. Upstream #26518 is the cautionary case.
- **No package-level mutable state, and no metrics from transaction paths.** A counter incremented in a handler fires
  once per incarnation, not once per transaction. Ark's module metrics live only in `x/*/keeper/abci.go`
  Begin/EndBlocker paths, which stay sequential, so there is nothing to fix; new metrics belong in ABCI paths, not in
  msg servers.
- **Rounding and arithmetic are unaffected.** Re-execution repeats the same deterministic arithmetic on whatever state
  the incarnation read; the [arithmetic rules](../../AGENTS.md#arithmetic-rules) govern rounding direction and bounds,
  not re-execution, and need no change for it.

### 2.6. Enablement checklist

1. Take an SDK release carrying #25893 (§2.2).
2. Add the `blockexec.Apply` call in `app/app.go` with the complete key list (§2.3), leaving `block-executor` at its
   `sequential` default. Confirm no behaviour change. `arkd` refuses any other `block-executor` value at config
   validation (`cmd/arkd/cmd/config.go`); lift that refusal only in this step.
3. Land the §2.4 prerequisites at an upgrade.
4. Audit msg-server paths against §2.5 — principally for panics on absent state.
5. Replay a recorded block range under both executors, with pre-estimation on and off, and require identical app hashes
   and transaction results. Exercise optimistic-execution aborts on a testnet with `block-stm` selected.
6. Benchmark a representative transaction mix (Wasm, Market orders, transfers, governance votes) sequentially, then with
   `block-executor = "block-stm"`, on a non-validator node.
7. Roll out to validators only after the benchmark justifies it. Confirm no node anywhere re-enables the block gas meter
   (§2.1).

## 3. Basket index assets

- Status: designed, not built. Launch keeps `axdr`, a voted feed, as the protocol reference and lists no basket asset.
- Trigger: governance intends to list Orbis (`aorbis`), the flagship currency: a basket-priced asset that later becomes
  the protocol reference. Build the whole feature then; nothing below is worth shipping inert.

### 3.1 The design

A basket is a derived feed owned by `x/oracle`: a denomination in the priced-denom grammar whose rate is composed on
chain from voted component feeds and is never voted itself. Composition runs in the preblock after the tally writes
component rates, as `Σ quantityᵢ × rate[componentᵢ]`, through the ordinary rate store, so every consumer reads it
unchanged. The stored timestamp and height are the oldest input's, so under any consumer's window the basket is exactly
as stale as its weakest component; a component with no stored rate skips the write. A sum above `MaxExchangeRate` omits
the write rather than halting, as the tally omits a derived price above it. Domain caps cannot make that unreachable:
quantities summing above one, as in any currency basket, let in-range component rates compose to an out-of-range sum.
The caps on component count and quantity do keep every product and the sum representable, so the unrepresentable case is
unreachable and its checked arithmetic is the backstop.

A defined basket answers `FeedPhase` Active, so asset registration, the reference re-point, and Reserve eligibility
entries accept it with no change to those modules. The basket pins its component feeds against removal, checked
oracle-internally ahead of the registered guards as the reference denom is; the basket itself is removable only when
`requireFeedUnreferenced` passes. Creation and rebalance require every component, kept or added, to answer
`FeedPhaseActive`. A feed already `Removing` passed its removal check before the basket existed, and its rate is pruned
when the removal activates, which the pin cannot stop. `MsgAddFeed` refuses a basket denom and basket creation refuses a
voted feed denom: one namespace, two disjoint sets. The vote target set, the vote-extension codec, and the sidecar never
see a basket.

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

### 3.2 Identifiers

Store prefix 7 in `x/oracle/types/keys.go` and `GenesisState` field 8, the next free numbers in `x/oracle`. Messages
`MsgCreateBasket`, `MsgRebalanceBasket`, `MsgRemoveBasket`; queries `Basket`, `Baskets`; constants `MaxBaskets`,
`MaxBasketComponents`, `MaxBasketQuantity`.

### 3.3 Delivery after launch

Additive: a consensus-version bump on `x/oracle` with a no-op migration, one new `OracleKeeper` method,
`ComposeBaskets`, called in the preblock between `ProcessVoteExtensions` and `AdvanceFeeds`, no vote-extension change,
no sidecar release. Docs that move with it: `x/oracle/README.md`, `FUTURE_CHANGES.md`, `x/asset/README.md`,
`ECONOMIC_DESIGN.md` §3.4, `THREAT_MODEL.md` §2.1 and §2.7, both ABCI READMEs.

### 3.4 Making Orbis the reference

Create the `aorb` basket, let it print, register the Orbis asset, then `MsgSetReferenceDenom` re-points from `axdr` to
it, running every wired rebaser at one fresh cross: Market's pool; Treasury's tax cap, base-fee floor and live price,
factor table, and exposure anchor; Disbursement's compensation cap, award values and charges, term usage, and allowance;
and Claims' committee claim limit, claim charges, and term usage. The asset's lifecycle stays independent of the
reference, as it is for any denomination today. Once unreferenced, `axdr` may be removed as a feed. Later rebalances of
the reference need no rebase, because the basket's NOAH value is continuous across them.

## Protocol monitoring tooling

Status: the protocol queries and typed events exist; the dedicated collector, indexer integration, and dashboard are not
delivered by this repository. Trigger: an operator is deploying protocol-state monitoring and choosing alert thresholds.
[Protocol monitoring](../operations/PROTOCOL_MONITORING.md#27-collection-requirements) owns polling cadence, indexed
events, retained history, panels, and calibration requirements. Implement those requirements without duplicating
consensus state in keeper metrics. Hosting, on-call ownership, and page thresholds remain operational decisions.

## Closed items

Work considered and decided against. Each entry gives the reason and names the condition that alone would reopen it; a
re-proposal starts by showing that condition holds.

| Item | Why not | Reopens only if |
| --- | --- | --- |
| Vote-extension-carried emergency orders | Suspension answers a failure the market can already see, the virtual pool caps what leaves each block however many race for it, and the wait a hidden order would shorten, a carrier's proposer slot, is small beside hours of detection and signing; the order travels as an ordinary committee transaction ([emergency submission](../governance/EMERGENCY_SUBMISSION_RUNBOOK.md)). | A mandate is expected to suspend ahead of public information. |
| Per-fund exposure model | Every fund counts NOAH against the one liability, and Reserve deployment risk is priced by recognition haircuts and caps, so a per-fund model collapses to the ratios and the multiplier ([economic design §4.5](../economics/ECONOMIC_DESIGN.md#45-bounds-on-committee-acts)). | A fund carries material risk that does not scale with liability. |
| Machine-authored Reserve evidence | Evidence upgrades see only chain-visible value, and the Reserve's holdings are custodian-held off chain, so committee attestation is the evidence model ([Reserve](../../x/reserve/README.md#mandate-custody-and-journal)). | A chain-verifiable venue becomes real; it then needs its own spec and the first-asset governance gate. |
| Shared capital-accounting package | Without a liability snapshot there is no common state and no duplicated arithmetic to extract ([Treasury](../../x/treasury/README.md#liability-and-capital-reads)). | A second independent consumer computes fund targets. |
| IBC, Ethereum light clients included | No counterparty route is planned, and a dormant stack would still be carried by the tax calculator, the policy router, the launch artefact, the threat model, and a two-chain e2e tier ([Wasm integration](../../app/README.md#wasm-integration)). | Interoperability is decided again, starting from a store migration rather than a switch. |
| NFT module | A non-fungible token is a contract protocol: CW721 contracts on the open runtime serve it with no new chain state, messages, or tax surface, and a native module would put a second schema beside them. | A token standard needs state or authority a contract cannot hold. |
