# Repository Guidelines

## Project Context

This is a Cosmos SDK blockchain project porting the full Terra Classic chain to modern Cosmos SDK conventions. Active
modules: `x/market/`, `x/oracle/`, `x/treasury/`, `x/asset/`, `x/claims/`, `x/reserve/`, `x/security/`, `x/wasm/`. The
chain currently uses **cosmos-sdk v0.54.3** with depinject and `cosmossdk.io/*` packages; always verify `go.mod` before
SDK-specific work because the SDK version can move.

Key differences between legacy (Terra Classic / cosmos-sdk v0.45) and modern patterns to always consider:

- **Dependency injection**: depinject-based module wiring, not manual constructor calls
- **Logging**: `cosmossdk.io/log` not `tendermint/libs/log`
- **Protobuf codegen**: dual generation — gogo (`x/*/types/*.pb.go`) + pulsar (`api/*.pulsar.go`), following current
  upstream SDK dual-generation patterns
- **Proto annotations**: key rules:
  - Dec/Int fields: `cosmos_proto.scalar` + `gogoproto.customtype` + `gogoproto.nullable = false` +
    `amino.dont_omitempty = true`
  - Address fields: `cosmos_proto.scalar` = `cosmos.AddressString` or `cosmos.ValidatorAddressString`
  - All `nullable = false` fields: must have `amino.dont_omitempty = true`
  - Msg types: `cosmos.msg.v1.signer`, `amino.name`, no legacy `gogoproto.equal`/`goproto_getters`
  - Query RPCs: `cosmos.query.v1.module_query_safe = true`
  - Params: `gogoproto.equal = true`, `amino.name`
- **Params**: collections-based module params, not `x/params`
- **Math types**: `cosmossdk.io/math` (math.Int, math.LegacyDec), not `sdk.Int`/`sdk.Dec`
- **Genesis**: keeper-level InitGenesis/ExportGenesis
- **Module registration**: depinject module.Manager patterns, not legacy AppModuleBasic

Reference codebases:

- **New chain**: `x/market/`, `x/oracle/`, `x/treasury/`, `x/asset/`, `x/claims/`, `x/reserve/`, `x/security/` (this
  repo)
- **Terra Classic reference**: `../classic-core/` (cosmos-sdk v0.45)
- **Connect reference**: `../connect/`; when the user says `connect`, use this repo.
- **Terra Feeder reference**: `../oracle-feeder/`; when the user says `feeder`, use this repo.
- **Upstream Cosmos SDK reference**: `../cosmos-sdk/`
- **Gaia reference**: `../gaia/`

## Project Structure

```text
x/market/       # DEX swap module (Noah ↔ stablecoins)
x/oracle/       # Price oracle module (validator price voting, feed registry)
x/treasury/     # Macro policy module (tax, reward funding, liability, fund targets)
x/asset/        # Asset registry and lifecycle owner for every non-NOAH Bank asset
x/claims/       # Insurance claims module (Claims mandate, claim record, Insurance custody)
x/reserve/      # Strategic Reserve module (custody, mandate, journal, recognition policy)
x/security/     # Security committee over the standard-module emergency surface
x/wasm/         # CosmWasm smart contract module (exported interfaces only; not wired)
abci/           # Vote-extension, proposal, preblock oracle pipeline, and mempool lanes
pricefeed/      # Off-chain price-feed sidecar, node-side client, providers, and transport API
pkg/            # Shared primitives such as encoding, telemetry, and minimal metrics
proto/ark/     # Proto definitions (modules, ABCI, pricefeed)
api/ark/       # Pulsar-generated code (runtime only, never import in module code)
app/            # App wiring, depinject config
```

## Oracle, Price Feed, And ABCI Boundaries

- Keep `x/oracle/` as the on-chain module. Top-level `pricefeed/` is split into `pricefeed/client` for the app-side cached
  client, `pricefeed/api` for generated transport API types, and `pricefeed/sidecar` for the off-chain sidecar.
- `pricefeed/sidecar` owns the sidecar process and transport: the sidecar `Service` implements the generated RPC server,
  owns gRPC/gateway listener machinery, and delegates provider/resolver price work to `pricefeed/sidecar/runtime`.
  `pricefeed/sidecar/runtime` owns the price-fetch loop and runtime updates.
- Keep `pricefeed/api` generated transport API only. Sidecar domain structs live under `pricefeed/sidecar/types`; routes use
  `/ark/pricefeed/v1/...`.
- `pricefeed/sidecar/providers` owns full provider config and construction. `pricefeed/sidecar/providers/base` owns provider
  runtime fields, fetch loop, ticker resolution, response ingestion, cached prices, runtime updates, `Fetcher`, and
  `TransportType`.
- `abci/` is fixed protocol code, not a pluggable strategy layer. Lifecycle hooks stay thin; `abci/oracle` owns vote
  extraction, aggregation, scoring, price application, and oracle-specific encoding policy.
- Keep primitive codecs and per-value encoding limits in `pkg/encoding`; keep aggregate vote-extension wire and decoded
  size limits in `abci/codec`.
- Prefer subsystem-owned package-level metrics: `abci/metrics`, `abci/oracle/metrics`, `pricefeed/client/metrics`,
  `pricefeed/sidecar/metrics`; keep `pkg/metrics` minimal and `pkg/telemetry` for startup wiring.

## Cosmos SDK Conventions

When making code changes:

- Always check the exact cosmos-sdk version in go.mod before suggesting imports or patterns
- **msgServer/queryServer pattern**: use named `k *Keeper` field + embed
  `UnimplementedMsgServer`/`UnimplementedQueryServer` in both. Query RPCs collide with public collection fields
  (`Params`, `TaxRate`, etc.); oracle msg RPCs also collide (`AggregateExchangeRatePrevote`). Named fields everywhere
  for consistency.
- When editing proto files, explain proposed changes before applying them and wait for confirmation

When porting from Classic, always modernize:

- `github.com/cosmos/cosmos-sdk/types.Dec` → `cosmossdk.io/math.LegacyDec`
- `gopkg.in/yaml.v2` → remove (YAML marshaling not needed in v0.53)
- `yaml.Marshal` custom `String()` methods → remove (gogo auto-generates `String()` via `proto.CompactTextString`)
- `github.com/tendermint/tendermint/crypto/tmhash` → `crypto/sha256` (stdlib)
- `gogoproto.moretags` yaml tags → remove (amino annotations replace these)
- `goproto_stringer = false` → remove unless providing custom `String()`
- `goproto_getters = false`, `equal = false` → remove (legacy no-ops)
- Legacy Msg methods (`Route`, `Type`, `GetSignBytes`, `GetSigners`) → remove entirely (replaced by proto annotations)
- `ValidateBasic()` on messages → remove; validate inline in msg_server handlers (no double address parsing)
- `sdk.ValAddress.MustLengthPrefix()` store keys → use collections with typed address keys (`sdk.ValAddressKey`,
  `sdk.AccAddressKey`)
- Cargo-culted methods on custom types (e.g., `Marshal`, `Unmarshal`, `MarshalJSON`, `Empty`, `Bytes`, `Format`) →
  remove unless actually used. Classic copied these from `sdk.AccAddress` onto types like `AggregateVoteHash`.

## Arithmetic Rules

These are consensus rules, not style. All three were violated in reviewed code before being written down here.

- **Round in the direction of what the number funds.** A division producing a payment floors; a division sizing a
  requirement ceils; round-to-nearest is only for figures nothing pays from. Fund targets use
  `MulRoundUp(...).Ceil()`; the subsidy split and the redemption coverage draw floor.
- **Multiply before dividing, and check the product.** Forming a ratio first rounds an intermediate against its own
  bound, and the other operand then amplifies that error past the bound — the coverage draw could exceed the Buffer
  it was paid from. Multiplying first leaves one rounding on a quantity whose bounds are whole base units, which
  monotone rounding cannot cross, so the bound becomes a theorem rather than a guarded hope. Use `SafeMul` for the
  product: unlike `Mul` it errors rather than panicking.
- **Bound governance inputs with domain caps, not projections.** For a governance-set number feeding halt-class
  arithmetic, cap the field in its own `Validate` with orders of magnitude of headroom
  (`MaxBlockRewardTarget`, `MaxRewardFundingWindow`). Reach for a projection — "this value plus live state over N
  blocks will still fit" — only when the domain genuinely cannot be bounded, and treat needing one as a signal that
  something in the state design is compounding: a projection has to be re-proved by every future writer of every
  input it reads, and its verdict moves with live state, so the same value can be valid today and invalid next month.
- **Oracle rates are NOAH per unit.** A rate is NOAH per one unit of its denomination, so valuing in NOAH
  multiplies and `RateSet.Convert` is `amount × rate[offer] / rate[ask]`. A stored figure that is a *price* of the
  reference unit (the exposure anchor) moves by the reciprocal of a quantity's factor, which is `Convert` with the
  two units passed in the opposite order; reversed arguments at such a site are the operation, not a bug. The store bounds a rate at `MaxExchangeRate` so the halt-class folds that multiply a 2^128-capped quantity
  by it stay representable. (D75–D77, `docs/superpowers/specs/2026-09-02-rate-orientation-flip-design.md`.)

The reason the third rule matters: **inside a BeginBlocker or EndBlocker a checked error and a panic are the same
outcome — the block fails and the chain halts.** Checked arithmetic buys a diagnosable message, never liveness. The
defence is refusing the input at the write, where a human is in the loop; `Safe*` is the loud backstop behind it.
Keep the backstop even when it is provably unreachable, and say so in a comment naming what makes it unreachable.

## Git Workflow

- Before running any `git push` commands, verify that a remote is configured with `git remote -v`
- Never assume a remote exists
- When asked to commit all changes, split them into multiple reasonably sized commits grouped by logical area (e.g., by
  module, by concern like proto vs keeper vs tests). Never lump unrelated changes into a single giant commit.
- Every commit message must be detailed: a concise subject line, followed by a body explaining what changed and why.
- Do NOT add Co-Authored-By lines to commit messages.

## General Rules

- Before making any changes, first outline exactly what files you'll modify and what the changes will be. Show the key
  diffs. Wait for approval before editing. This is especially important for proto files, keeper/module wiring, and
  parameter validation — a validation that is too loose, too strict, or the wrong shape is a halt or a governance
  deadlock, and both are hard to see in a diff.
- When the user references a specific file path or directory (e.g., "look at classic-core/types"), navigate to exactly
  that path. Do not substitute a similarly-named path from a different part of the codebase.

## Protobuf Generation

Dual generation pipeline matching the current upstream Cosmos SDK pattern:

- `proto/buf.gen.gogo.yaml`: gocosmos + grpc-gateway → `x/*/types/*.pb.go` (typed Go structs via `gogoproto.customtype`)
- `proto/buf.gen.yaml`: go-pulsar + go-grpc → `api/*.pulsar.go` (standard protobuf, managed mode)
- Proto files use `option go_package = "github.com/ararat-network/ark/x/{module}/types"` to route gogo output
- Run `make proto-gen` to regenerate (runs Docker proto-builder)
- Proto-gen uses a named Docker volume (`ark-proto-cache`) for BSR dependency caching
- `go mod tidy` runs on the host (in Makefile), not inside the container — avoids re-downloading Go modules every run
- Proto-builder image is Alpine — scripts must use `#!/bin/sh`, not `#!/usr/bin/env bash`
- If a proto message has `gogoproto.equal = true` and contains a sub-message field, that sub-message must also have
  `gogoproto.equal = true`

Proto annotation pattern for Dec/Int fields (follow SDK mint module exactly):

```protobuf
string field_name = N [
  (cosmos_proto.scalar)  = "cosmos.Dec",           // runtime: signing, textual rendering
  (gogoproto.customtype) = "cosmossdk.io/math.LegacyDec", // codegen: typed Go field
  (gogoproto.nullable)   = false,                  // codegen: value type, not pointer
  (amino.dont_omitempty) = true                    // amino: always include in JSON (all nullable=false fields)
];
```

## Collections Patterns

- Address keys: use typed codecs (`sdk.ValAddressKey`, `sdk.AccAddressKey`), not `collections.StringKey`
- Address as value: `collcodec.KeyToValueCodec(sdk.AccAddressKey)` — adapts a KeyCodec into a ValueCodec
- Key persistent state by operator address (`sdk.ValAddress`), never by consensus address. The operator address is the
  validator's identity; the consensus address is a rotatable credential, and SDK v0.55 key rotation makes that mapping
  mutable. Cons-addr-keyed state has to be migrated on every rotation; cons addrs belong at attribution boundaries only
- Default param values: use `const` for compile-time literals (integers, strings); `var` only for runtime init (function
  calls, struct/slice literals)
- **Collection access convention** (matches upstream SDK: mint, gov, bank):
  - Collection fields are **public** — callers use `k.Params.Get(ctx)`, `k.TobinTax.Get(ctx, denom)` directly
  - Only wrap with a keeper method when there's **real logic**: default-on-not-found (e.g., `GetFeederDelegation`
    defaults to validator itself), domain checks (e.g., `GetExchangeRate` has NoahBaseDenom identity), side effects
    (e.g., `SetExchangeRateWithEvent` emits events)
  - If a wrapper is just `return k.Collection.Get(ctx, key)` with no extra logic, delete it — the collection IS the
    getter

## Build & Verification

- Build the binary: `go build -o build/arkd ./cmd/arkd`
- Use scope-matched verification first. Run focused package tests for narrow changes; use `go build ./...` when the
  change should affect the whole repo or before claiming repo-wide compile. If unrelated checkout drift blocks repo-wide
  verification, report the exact blocker.
- Proto generation: `make proto-gen` (also runs `go mod tidy`)
- Proto formatting: `make proto-format`
- Proto linting: `make proto-lint`
- Go linting: `make lint` (golangci-lint); `make lint-fix` auto-fixes; `make format` runs gci and gofumpt
- Run tests: `go test ./x/{module}/...` for a single module, or `go test ./...` for all
- Run with verbose output: `go test -v ./x/{module}/...`

## Testing Conventions

- Types tests (`x/*/types/`): plain functions, not test suites. Test suites are for keeper tests only.
- Use table-driven tests with `t.Run()` for all test cases
- For params/genesis validation: use mutate pattern — start from `DefaultParams()`/`DefaultGenesisState()`, mutate one
  field per case
- Test validation logic and parsing, not trivial constructors (field assignments with no logic)
- Verify both error and boundary-valid cases (e.g., zero is valid for `[0, 1]` range checks)
- Use British spelling for function names (e.g., `Randomised` not `Randomized`)
