# Application

`ArkApp` assembles the SDK application, Ark modules, and manually wired IBC/Wasm integrations.
[app.go](app.go) is the construction entry point; [app_config.go](app_config.go) declares depinject configuration,
module-account permissions, blocked addresses, and lifecycle order. Check [go.mod](../go.mod) before changing SDK integration.

## Construction and boundaries

- [config.go](config.go) initialises chain identity, denominations, address prefixes, and keyring service identity.
- [app.go](app.go) builds the runtime, injects keepers, and connects feed guards, reference consumers, send restrictions,
  and the execution-policy router.
- [ibc.go](ibc.go), [wasm.go](wasm.go), [wasm_query.go](wasm_query.go), and [gmp.go](gmp.go) own manual integrations.
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
   refreshes conversion factors and exposure in its own hook.
3. Market leads EndBlock and settles conversion facts before later modules move funds or enact lifecycle changes.
   Governance precedes Treasury's reward/base-fee settlement and Oracle attendance settlement. Oracle precedes staking
   so attendance jails affect that block's validator-set update. Claims settles after governance cancellations.

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

Nothing in `x/` or `abci/` reads the block gas meter, so removing the option changed no module logic.

**Do not re-add `EnableBlockGasMeter()`.** If a future requirement genuinely needs a block-level gas bound, set
consensus `max_gas` instead.

## Module entry points

[Asset](../x/asset/README.md), [Claims](../x/claims/README.md), [Disbursement](../x/disbursement/README.md), [Market](../x/market/README.md),
[Oracle](../x/oracle/README.md), [Reserve](../x/reserve/README.md), [Security](../x/security/README.md),
and [Treasury](../x/treasury/README.md) each document their behaviour, rationale, state, and development boundaries.

## IBC and Wasm integration

Decision IDs below refer to the [economic decision register](../docs/design/ECONOMIC_DECISIONS.md).
Ark is built as an interchain hub on `ibc-go` v11 (D45): IBC core, ICS-20 on both the Classic and the v2 route
sharing one transfer keeper, the 07-Tendermint client, packet forwarding on Classic, governance rate limiting on both
routes, callbacks on both routes as the transfer-and-call mechanism, ICA controller and host, ICS-27 v2 general message
passing, and the 08-Wasm light client installed dormant (D46, D47, D48, D50, D51, D52). The effective inbound Classic
stack is rate limit, packet forward, callbacks, transfer; v2 is rate limit, callbacks, transfer. Absent by decision:
ICS-29 relayer fees, ICS-721, IBC Hooks, a v2 packet-forward adapter, a generic contract-to-ICA authentication module,
06-Solo Machine, and the attestations client (D51, D52, D53). All of it is wired through the SDK runtime's manual
registration hooks beside the depinject-wired modules, in `app/ibc.go`.

[ibc.go](ibc.go) owns the manual IBC registration and middleware order; [wasm.go](wasm.go) constructs the contract
runtime; [gmp.go](gmp.go) installs general message passing. Verify dependency versions in [go.mod](../go.mod).
The [governance guide](../docs/governance/GOVERNANCE_OPERATIONS.md) owns opening the hub and setting permissions.

- **The policy router**, which wraps the SDK message router handed to Wasmd and to the GMP keeper. It receives the
  exact SDK message Wasmd's canonical encoder produces, prices it, charges the dispatching account, and calls the
  handler, in one cached execution. Wasmd's custom message encoder is unused: a contract calls Ark's modules
  with `CosmosMsg::Any` carrying the proto type URL and bytes, so there is one schema instead of a JSON mirror that
  nothing keeps in sync (D74). Impersonation is closed by Wasmd itself, which rejects a dispatched message whose signer
  is not the contract.
- **The query accept list** in `app/wasm_query.go`, hand-written as Osmosis, Neutron, Juno, and Archway keep theirs,
  because Wasmd ships no permissive default and a permissive list leaks nondeterministic reads into consensus. A path is
  listed only if it is annotated `module_query_safe`, which a test enforces, and listing says something further: its
  response shape is frozen for the life of the chain, because a value a contract reads is an input to consensus.
  Widening the list is a coordinated binary upgrade, never a vote. It carries forty-eight paths: the thirty-four the SDK
  annotates itself for auth, bank, and staking, admitted whole because the annotation is the review (D85), and fourteen
  of Ark's own: what a contract needs to price and route a conversion or a transfer, the tax estimate and the caps and
  gas prices behind it, the Oracle rates and reference unit, Market's quote, pool, policy, and Tobin tax, and the asset
  registry. The ICA host derives its own allowlist from the same annotation inside ibc-go, so the two surfaces are
  separate by construction.
- **Callbacks** on both ICS-20 stacks deliver source, acknowledgement, timeout, and destination callbacks into
  contracts under ibc-go's gas cap and authorisation rules: a destination callback failure fails the receive, while
  acknowledgement and timeout callback failures follow the non-blocking lifecycle and undo no protocol bookkeeping.
- **Addresses.** Ark's address verifier admits 32-byte addresses beside 20-byte ones, because CosmWasm derives both
  contract address forms to 32 bytes and a verifier accepting 20 alone made every message naming a contract fail.

The [economic transfer contract](../docs/design/ECONOMIC_DESIGN.md#111-the-execution-tax-contract) owns who pays tax and when
it remains payable. These adapters enforce that contract at distinct signed-message and generated-message boundaries.

## Verification

From the repository root, run `go test ./app/...` for application changes. Integration scenarios live under
[tests/integration/](../tests/README.md); simulation commands and prerequisites are in the [test guide](../tests/README.md).
[testutil/](testutil/) supplies app, validator, funding, genesis, and IBC fixtures. Embedded contract fixtures and their
wrapper live in [testdata/contracts.go](testdata/contracts.go). Native grant flows are covered by
[disbursement_test.go](disbursement_test.go), including custody, failure rollback, founder limits, and continuation export/import.
Use `go build ./...` before claiming
repository-wide compilation.

## Simulation fixtures

[sim_overrides.go](sim_overrides.go) samples auth parameter proposals from the SDK's genesis ranges. The SDK's
broader proposal ranges can make its own fixed-gas transactions undeliverable: short memo limits reject generated
memos, and high byte costs exhaust the 10-million gas limit on Wasm uploads. The simulation process bypasses
fee and governance-vote stake gates because its random fees and voters cannot satisfy these consensus rules.

Reserve and Insurance receive seeded custody balances, with Bank supply increased equally. This makes deployments,
positions, and claims reachable without a circular dependency on transfers from initially empty funds. Imported
simulation genesis uses the newest exported oracle timestamp so rates do not appear dated after genesis.

Gas-estimate tests compare payable and fee-less simulation with finalisation for both NOAH-only and stable-plus-tip
fees. Wasm's counter skips simulation and SDK signature-size estimation differs from signed execution; the client gas
adjustment covers those differences. Fixtures omit random memos so unrelated gas cannot dilute the measured ratio.
