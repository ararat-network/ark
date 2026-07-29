# Asset Consolidation: Derived Membership And Owned References

Date: 2026-07-29
Status: approved design, pending implementation plan

## Problem

`x/asset` maintains `AssetLocks`, an inverse dependency index
(`x/asset/keeper/asset_locks.go`): downstream modules record per-denom
back-pointers so asset lifecycle transitions can refuse to break foreign
state. Four kinds exist (`proto/ark/asset/v1/asset.proto:43-56`): two live
references (`MARKET_BASE_POOL`, `TREASURY_REFERENCE_TAX_CAP`) and two policy
enrollments (`MARKET_ASSET_POLICY`, `TREASURY_STABLE_POLICY`).

Planning the Tobin-tax move from `x/oracle` to `x/market` surfaced the
question the index exists to answer: which denoms belong in each consumer's
per-denom set (Market Tobin entries, Treasury tax caps), and who coordinates
those sets. Interrogating that question dissolved the index:

1. **Membership is identical by construction.** Every oracle-priced ACTIVE
   asset is swap-enabled and tax-capped, by design decision. Consumer
   membership sets are therefore derivable from registry state, not
   independent state needing protection.
2. **Liability inclusion is also derivable.** A suspended or written-off
   stable remains a liability until retirement, and retirement already
   requires zero supply — zero supply means zero liability, so the enrollment
   lock's retirement guard duplicates an existing precondition.
3. **The two references are one reference.** The base-pool reference and the
   reference-tax-cap denomination are the same denom by design decision (both
   SDR today), permanently. One chain-level reference with one fallback is
   asset-owned state, not two foreign back-pointers.

With no foreign per-denom state left to index, `AssetLocks` protects nothing.

This reverses a documented decision. `docs/ASSET_MODULE_PLAN.md` ("Each
consumer owns the fallback for the reference it owns") argued the references
are independent policy questions with independent fallbacks. The designer
override recorded here — the references are always the same denom — collapses
both questions into one, which `x/asset` can own without judging consumer
policy.

## Non-goals

- Consumer wiring. Market and Treasury derivation logic (Tobin move, rate
  storage, tax-cap refresh restructure, reward-funding sourcing) is specified
  under "Recorded decisions" but deliberately not built. `x/asset` remains
  dormant; consumers cut over at activation, per the scoping precedent in
  `docs/superpowers/specs/2026-07-28-oracle-target-transitions-design.md`.
- Changing lifecycle semantics, settlement mechanics, or the emergency
  mandate's committee powers.
- Deleting `x/oracle`'s `TobinTaxes` param (activation milestone).

## Design

### Deleted: the lock index

- `AssetLocks` KeySet (`x/asset/keeper/keeper.go:29`) and
  `x/asset/keeper/asset_locks.go` entirely.
- `AssetLock`, `AssetLockKind`, and their `Validate`, `Key`, and
  `RequiresOraclePricing` helpers (`x/asset/types/asset.go:130-157`),
  `AssetLockKindKey`.
- `EventAssetLockAdded`, `EventAssetLockRemoved`.
- The `AssetLocks` query RPC, its gateway route, and its autocli entry.
- `ErrAssetLocked` (code 5; the code stays burned).
- Proto hygiene: deleted messages, enum, and RPC leave `reserved` numbers and
  names. Collection prefix 1 (`AssetLocksKey`) stays burned.

Genesis needs no changes for the deletion: locks were reconstructed from
consumer genesis and never exported (`proto/ark/asset/v1/genesis.proto:10`).

### Added: owned reference state

    message ReferenceState {
      // reference_denom prices Market's base pool and denominates Treasury's
      // reference tax cap. Empty only before first configuration.
      string reference_denom = 1;
      // fallback_denom is promoted when the reference asset is suspended.
      // May be empty; suspension of the reference then fails atomically.
      string fallback_denom = 2;
    }

Stored as `Reference collections.Item[types.ReferenceState]`.

Stateless validation: denoms valid native base denoms, not NOAH, fallback ≠
reference, fallback empty when reference empty. Stateful eligibility (msg
handler and genesis): a named denom must satisfy the former live-lock
acquisition rules — `oracle_required`, status ACTIVE or non-finalized
ISSUANCE_HALTED, active Oracle-target phase.

`MsgSetReference{authority, reference}` (governance-gated) replaces both
consumer-owned fallback configurations. When it changes a non-empty
`reference_denom`, the handler invokes the rebase executors (below)
atomically; fallback-only updates do not rebase. First-time configuration
(empty → set) does not rebase.

`EventReferenceUpdated{reference_denom, fallback_denom}` is emitted on every
`Reference` write, from both the msg handler and suspension promotion.

A `Reference` query RPC (`module_query_safe`) exposes the item. Genesis
imports and exports it; an empty registry may carry an empty reference.

### Rebase executors

The emergency fallback interfaces (`x/asset/types/expected_keepers.go`)
change shape. Today each consumer chooses and returns its own fallback:

    ExecuteBasePoolFallback(ctx, from) (string, error)
    ExecuteTaxCapFallback(ctx, from) (string, error)

Asset now owns the choice and passes it:

    // MarketReferenceKeeper re-denominates Market state held in reference
    // units (the base-pool delta) in the same transaction.
    type MarketReferenceKeeper interface {
        RebaseBasePool(ctx context.Context, from, to string) error
    }

    // TreasuryReferenceKeeper re-expresses Treasury state held in reference
    // units (the reference tax cap amount) in the same transaction.
    type TreasuryReferenceKeeper interface {
        RebaseTaxCap(ctx context.Context, from, to string) error
    }

Both routine (`MsgSetReference`) and emergency (mandate suspension) reference
moves run the same executors. A nil executor or executor error fails the
whole action atomically — unchanged escalation semantics
(`x/asset/keeper/emergency_mandate.go:191-196`). While the module is
dormant, executors are exercised through mocks, as the mandate tests do
today.

### Guard rewrites

Every lock check becomes an own-state check:

- `suspendAsset` (`x/asset/keeper/lifecycle.go:289`): replace
  `requireNoLivePricingLocks` with "denom is not the current
  `reference_denom`". The emergency SUSPEND action satisfies this by rebasing
  first: if the target is the reference, promote `fallback_denom` to
  reference (clearing the fallback), re-validating the fallback's eligibility
  at execution time, run both executors, then suspend — all in one
  transaction. An empty or ineligible fallback fails the whole action.
  Routine governance achieves the same with `MsgSetReference` followed by
  `MsgSuspendAsset` in one proposal.
- `finaliseRetirement` (`x/asset/keeper/lifecycle.go:568`): replace
  `requireNoAssetLocks` with "denom is neither `reference_denom` nor
  `fallback_denom`". Retiring the fallback asset fails until governance
  re-points it. Invariant: `ReferenceState` never names a retired asset. No
  other retirement path needs the check — reference eligibility requires a
  live status, which PENDING-path cancellation can never have held.
- `requireMarketSettlementPolicy` (`x/asset/keeper/settlement.go:424`):
  delete. Under identical-by-construction membership, any suspended or
  written-off stable was necessarily once market-convertible; the settlement
  path asserts `oracle_required` explicitly instead.
- `SetAssetLock`'s acquisition matrix (Removing-phase, status, and pricing
  checks) disappears with the acquisition path. Its rules survive only as
  `ReferenceState` eligibility above.

Suspending the asset named by `fallback_denom` is allowed and leaves the
fallback in place; eligibility is re-validated whenever the fallback is used.

### Priced-live view and epoch (dormant mechanism)

The consumer-facing membership source lands now, unwired:

    func (k Keeper) PricedLiveDenoms(ctx context.Context) ([]string, error)
    func (k Keeper) IsPricedLive(ctx context.Context, denom string) (bool, error)

Predicate: `oracle_required && status ∈ {ACTIVE, ISSUANCE_HALTED} && denom ∈
materialized OracleTargets.Denoms`. Implementation iterates the materialized
target set (sorted, ≤ `MaxOracleTargets`) with one asset read per denom —
deterministic, sorted output for free.

The target-set intersection exists for the wind-down case: an ISSUANCE_HALTED
asset whose target removal has activated stops receiving prices while its
status is unchanged. Status alone would keep it in membership unpriced
forever, and one unpriced member poisons Treasury's whole cap rebuild into a
permanent skip (`x/treasury/keeper/tax_caps.go:33-45`). The intersection
drops the denom at exactly the height its prices stop. During the two-block
Removing window the denom stays a member (prices still flow); a targeted
PENDING asset is excluded by status; an ACTIVE asset always has target
membership by lifecycle invariant, so the intersection only ever filters
wind-down.

`PricedLiveVersion collections.Item[uint64]` is the membership epoch: bumped
by any write that changes `PricedLiveDenoms` output. Sites: `activateAsset`
(entry), `suspendAsset` (exit), recovery completion (re-entry), and
target-transition promotion affecting a live-status asset (wind-down exit;
addition promotion for a recovered live asset). Over-bumping is explicitly
allowed — a spurious consumer rebuild is harmless; under-bumping is the bug
class. Tests assert the property, not an op list: any transition changing the
view's output bumps the version.

Consumers (later, at activation) compare a stored last-applied version each
BeginBlocker instead of structural mismatch scans — event-driven refresh
signaled through state, without hooks coupling asset lifecycle success to
consumer cache health.

### Plan-document updates

`docs/ASSET_MODULE_PLAN.md` is rewritten as part of implementation:

- "Asset locks" section → reference ownership, eligibility, and the rebase
  executor contract.
- The consumer-owned-fallback rationale paragraph → the single-reference
  reversal recorded here.
- State-model collection list: remove `AssetLocks`; add `Reference` and
  `PricedLiveVersion`.
- Ownership lists for `x/market` / `x/treasury`: drop per-module fallback
  ownership; note reference-unit state rebases on executor calls.
- Genesis section: drop lock reconstruction; add reference import.

## Recorded decisions (deferred to activation)

Settled now so consumer milestones inherit them without relitigating:

- **Membership identity.** Swap eligibility and tax-cap membership are the
  priced-live predicate. No consumer stores a membership set.
- **Liability set.** `oracle_required && status ∉ {PENDING, RETIRED}`.
  Suspended and written-off stables remain liabilities until retirement.
- **Tobin rates.** Terra Classic never derived rates on-chain — governance
  judgment only (default 0.25%, MNT 8× in genesis;
  `classic-core/x/oracle/types/params.go:37`). Market therefore stores a
  default-rate param plus a sparse per-denom override map. A newly activated
  asset swaps at the default immediately; overrides are rare governance acts.
  Stale overrides for non-members are inert.
- **Treasury refresh.** BeginBlocker derives `PricedLiveDenoms` once (feeding
  tax caps and reward funding), rebuilds caps when the epoch advanced or at
  the weekly boundary, and stores the last-applied version only after a
  successful rebuild so the valuation-unavailable skip retries naturally.
  Treasury's dependency on Tobin taxes disappears entirely — it only ever
  consumed the denom list (`x/treasury/keeper/tax_caps.go:75-95` uses
  `tax.Denom` exclusively).
- **Activation gap.** Msg-driven membership entry reaches consumers next
  block (BeginBlocker ordering), during which taxed transfers of the new
  denom hard-error (`ErrTaxCapUnavailable`) — parity with today's tobin-list
  flow, not a regression. Optional treasury-side hardening: on cap
  `ErrNotFound`, check membership and compute-and-persist that denom's cap on
  demand.

## Testing

Types (plain table-driven functions, mutate pattern from defaults):

- `ReferenceState.Validate`: empty state, reference-only, fallback equal to
  reference, invalid denoms, NOAH, fallback without reference.
- Genesis validation with reference state present, empty, and naming a
  missing or ineligible asset.

Keeper (suite):

- Priced-live view: status × target-phase matrix, including targeted PENDING
  (excluded), Removing window (included), post-removal ISSUANCE_HALTED
  (excluded), sorted determinism.
- Epoch property: for every lifecycle transition and promotion path, if
  `PricedLiveDenoms` output changes, the version bumped.
- `MsgSetReference`: authority check, first set, fallback-only update (no
  rebase), reference change invoking both executors, executor error and nil
  executor failing atomically, ineligible denom rejection.
- Suspension: of a non-reference asset (no rebase), of the reference with
  eligible fallback (promotion + rebase + suspension in one transaction), with
  empty fallback (atomic failure), with ineligible fallback (atomic failure),
  of the fallback asset (allowed, fallback retained).
- Retirement: refusal while named as reference or fallback; success after
  re-pointing.
- Settlement: plan opening asserts `oracle_required`; market-policy lock
  check removed.
- Genesis round-trip including reference state and epoch.

## Deferred work

- Consumer cutover at activation: Tobin move to Market, Market rate storage
  and swap gating, Treasury refresh-on-epoch, reward-funding sourcing,
  `x/oracle` Tobin deletion, app wiring, and the executor implementations
  behind `MarketReferenceKeeper` / `TreasuryReferenceKeeper`.
- Optional read-through tax-cap hardening (above).
- A `PricedLiveDenoms` query RPC if the sidecar or CLI needs it; consumers
  use the keeper view.
