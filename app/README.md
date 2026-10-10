# Application

`ArkApp` assembles the SDK application, Ark modules, and the manually wired Wasm integration.
[app.go](app.go) is the construction entry point; [app_config.go](app_config.go) declares depinject configuration,
module-account permissions, blocked addresses, and lifecycle order. Check [go.mod](../go.mod) before changing SDK integration.

## Construction and boundaries

- [config.go](config.go) initialises chain identity, denominations, address prefixes, and keyring service identity.
- [app.go](app.go) builds the runtime, injects keepers, and connects feed guards, reference rebasers, the Reserve's
  Treasury capital reader, and the execution-policy router. Send restrictions are depinject outputs ordered in
  [app_config.go](app_config.go).
- [wasm.go](wasm.go) and [wasm_query.go](wasm_query.go) own the manual integration.
- [ante/](ante/README.md) owns signed-transaction ante/post assembly. [execution_policy_router.go](execution_policy_router.go)
  applies message policy at execution-generated message boundaries.
- [mempool.go](mempool.go) registers the privileged message surface; [mempool/](mempool/README.md) owns pending storage and proposal service.
- [oracle.go](oracle.go) installs the [ABCI hooks](../abci/README.md) over the node-side [price client](../pricefeed/client/README.md).
- [upgrades.go](upgrades.go) and [upgrade/](upgrade/README.md) register upgrade handlers and store changes.
- [export.go](export.go) owns continuation export. [genesis/genesis.json](genesis/genesis.json) is the curated launch artefact;
  [GENESIS.md](../docs/governance/GENESIS.md) owns its settings and rationale.

## Block lifecycle

Read the lists in [app_config.go](app_config.go) for the exact order. The dependencies that matter are:

1. Module preblockers run before oracle decoding so upgrades have already applied. Oracle reports are consumed before
   due feed transitions are promoted.
2. BeginBlock runs after those prices are available. The SDK distribution/slashing ordering is retained; Treasury
   refreshes conversion factors and exposure in its own hook, and Security drops a committee plan record the
   upgrade slot no longer holds.
3. Governance executes the block's proposals first, so a proposal's conversions are part of the block's flow;
   Market then settles conversion facts against the block's final state, before later modules move funds.
   Governance precedes Treasury's reward/base-fee settlement and Oracle attendance settlement. Oracle precedes staking
   so attendance jails affect that block's validator-set update. Claims settles after governance cancellations.
   Disbursement pays last, from custody it already holds, so a cancellation executed in the block governs its payments.

Asset lifecycle transitions are message-driven. The ABCI wrapper does not complete asset transitions or prime Treasury
liability. [Economic design](../docs/design/ECONOMIC_DESIGN.md) and the [on-chain oracle](../x/oracle/README.md) describe their shared protocol contracts.

## Block gas-meter policy

[app.go](app.go) does not pass `baseapp.EnableBlockGasMeter()`. The SDK has defaulted the meter off since v0.54.
Ark follows that default for these reasons:

- **It is a redundant bound.** When consensus `max_gas > 0`, the SDK proposal handlers enforce the declared
  transaction-gas sum before consensus accepts the block. Each transaction also has its own gas meter.
  A cumulative execution meter adds another state-machine constraint at finalisation.
- **It is state-machine behavior, so removing it later is consensus-visible.** Prelaunch it is one line. After genesis
  it is a coordinated change at an upgrade boundary.
- **It cannot run under block-stm.** The SDK's parallel executor panics on a block gas meter, which is why upstream
  defaulted it off.

Nothing in `x/` or `abci/` reads the block gas meter, so removing the option changed no module logic.

**Do not re-add `EnableBlockGasMeter()`.** If a future requirement genuinely needs a block-level gas bound, set
consensus `max_gas` instead.

## Module entry points

[Asset](../x/asset/README.md), [Claims](../x/claims/README.md), [Disbursement](../x/disbursement/README.md), [Market](../x/market/README.md),
[Oracle](../x/oracle/README.md), [Reserve](../x/reserve/README.md), [Security](../x/security/README.md),
and [Treasury](../x/treasury/README.md) each document their behaviour, rationale, state, and development boundaries.

## Wasm integration

Decision IDs below refer to the [economic decision register](../docs/design/ECONOMIC_DECISIONS.md).
Ark runs upstream Wasmd `v0.70.x` on `wasmvm/v3` (D49), wired through the SDK runtime's manual registration hooks
beside the depinject-wired modules, in `app/wasm.go`. The chain has no IBC (D118): the Wasm keeper takes no channel
keepers, its default message handler and encoders are replaced by ones that refuse a contract's IBC and IBC v2
messages with an explicit error, its IBC query plugin refuses, no contract port is routed, and the `ibc2` capability
is withheld so a contract requiring it fails at upload. Wasmd itself still depends on ibc-go, which is why the
dependency remains in `go.mod`.

[wasm.go](wasm.go) constructs the contract runtime and registers the state-sync snapshot extension that carries contract
code, which lives outside the IAVL tree; [app.go](app.go) re-warms the pinned-code cache after loading the store, since
the pin list is state and the VM cache is per-process. Verify dependency versions in [go.mod](../go.mod).
The [governance guide](../docs/governance/GOVERNANCE_OPERATIONS.md) owns contract permissions.

- **The policy router**, which wraps the SDK message router handed to Wasmd. It receives the
  exact SDK message Wasmd's canonical encoder produces, prices it, charges the dispatching account, and calls the
  handler, in one cached execution. Wasmd's custom message encoder is unused: a contract calls Ark's modules
  with `CosmosMsg::Any` carrying the proto type URL and bytes, so there is one schema instead of a JSON mirror that
  nothing keeps in sync (D74). Impersonation is closed by Wasmd itself, which rejects a dispatched message whose signer
  is not the contract.
- **The query accept list** in `app/wasm_query.go`, hand-written as Osmosis, Neutron, Juno, and Archway keep theirs,
  because Wasmd ships no permissive default and a permissive list leaks nondeterministic reads into consensus. A path is
  listed only if it is annotated `module_query_safe`, which a test enforces, and listing says something further: its
  response shape is frozen for the life of the chain, because a value a contract reads is an input to consensus.
  Widening the list is a coordinated binary upgrade, never a vote. It carries sixty-three paths: the thirty-four the SDK
  annotates itself for auth, bank, and staking, admitted whole because the annotation is the review (D85), and
  twenty-nine of Ark's own. Most are what a contract needs to price and route a conversion or a transfer: the tax
  estimate and the caps and gas prices behind it, the Oracle rates, reference unit, and feed schedule, Market's quotes,
  pool, policy, and Tobin tax, and the asset registry with its settlement terms. The rest let a contract read its own
  standing or the system's: grants and beneficiary records, membership, a validator's oracle attendance and reward
  weight, reward funding, fund status and exposure, the Reserve's recognised capital, the Insurance balance, and a
  claim record. Params, mandates, and audit trails stay off the list.
- **Addresses.** Ark's address verifier admits 32-byte addresses beside 20-byte ones, because CosmWasm derives both
  contract address forms to 32 bytes and a verifier accepting 20 alone made every message naming a contract fail.

The [economic transfer contract](../docs/design/ECONOMIC_DESIGN.md#111-the-execution-tax-contract) owns who pays tax and when
it remains payable. These adapters enforce that contract at distinct signed-message and generated-message boundaries.

## Verification

From the repository root, run `go test ./app/...` for application changes. Integration scenarios live under
[tests/integration/](../tests/README.md); simulation commands and prerequisites are in the [test guide](../tests/README.md).
[testutil/](testutil/) supplies app, validator, funding, and genesis fixtures. Native grant flows are covered by
[disbursement_test.go](disbursement_test.go), including custody, failure rollback, founder limits, and continuation export/import.
Use `go build ./...` before claiming
repository-wide compilation.

## Simulation fixtures

[sim_overrides.go](sim_overrides.go) samples auth parameter proposals from the SDK's genesis ranges. The SDK's
broader proposal ranges can make its own fixed-gas transactions undeliverable: short memo limits reject generated
memos, and high byte costs exhaust the 10-million gas limit on Wasm uploads. The simulation process bypasses
fee and governance-vote stake gates because its random fees and voters cannot satisfy these consensus rules.

Reserve, Insurance, and both Disbursement accounts receive seeded custody balances, with Bank supply increased equally.
This makes deployments, positions, claims, and grants reachable without a circular dependency on transfers from
initially empty funds; Disbursement's generator opens part of the member balance as its genesis tranche. Imported
simulation genesis takes the exported chain's last block time and height, as a continuation export resumes, so no
record appears to come from after genesis. The after-import run then simulates on a fresh app, because the
simulator's own InitChain discards the uncommitted import.

Gas-estimate tests compare payable and fee-less simulation with finalisation for both NOAH-only and stable-plus-tip
fees. Wasm's counter skips simulation and SDK signature-size estimation differs from signed execution; the client gas
adjustment covers those differences. Fixtures omit random memos so unrelated gas cannot dilute the measured ratio.
