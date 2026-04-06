# Repository Guidelines

## Project Context

This is a Cosmos SDK blockchain project porting the full Terra Classic chain to modern Cosmos SDK conventions. Active
modules: `x/market/`, `x/oracle/`, `x/treasury/`. The chain uses **cosmos-sdk v0.53.5** with depinject and
`cosmossdk.io/*` packages.

Key differences between legacy (Terra Classic / cosmos-sdk v0.45) and modern patterns to always consider:

- **Dependency injection**: depinject-based module wiring, not manual constructor calls
- **Logging**: `cosmossdk.io/log` not `tendermint/libs/log`
- **Protobuf codegen**: dual generation — gogo (`x/*/types/*.pb.go`) + pulsar (`api/*.pulsar.go`), following upstream
  SDK v0.53 pattern
- **Proto annotations**: key rules (detailed reference in `.claude/` memory files):
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

- **New chain**: `x/market/`, `x/oracle/`, `x/treasury/` (this repo)
- **Terra Classic reference**: `../classic-core/` (cosmos-sdk v0.45)
- **Upstream Cosmos SDK reference**: `../cosmos-sdk/`

## Project Structure

```text
x/market/       # DEX swap module (Ark ↔ stablecoins)
x/oracle/       # Price oracle module (validator price voting)
x/treasury/     # Macro policy module (tax rate, reward weight, seigniorage)
proto/noah/     # Proto definitions (market, oracle, treasury)
api/noah/       # Pulsar-generated code (runtime only, never import in module code)
app/            # App wiring, depinject config
```

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

## Git Workflow

- Before running any `git push` commands, verify that a remote is configured with `git remote -v`
- Never assume a remote exists
- When asked to commit all changes, split them into multiple reasonably sized commits grouped by logical area (e.g., by
  module, by concern like proto vs keeper vs tests). Never lump unrelated changes into a single giant commit.
- Every commit message must be detailed: a concise subject line, followed by a body explaining what changed and why.
- Do NOT add Co-Authored-By lines to commit messages.

## General Rules

- Before making any changes, first outline exactly what files you'll modify and what the changes will be. Show the key
  diffs. Wait for approval before editing. This is especially important for proto files and keeper/module wiring.
- When the user references a specific file path or directory (e.g., "look at classic-core/types"), navigate to exactly
  that path. Do not substitute a similarly-named path from a different part of the codebase.
- When discussing code, refer to functions, methods, types, constants, or interfaces by name first (for example,
  `msgServer.handleSwapRequest` or `Keeper.ComputeSwap`). Include file paths when helpful, but avoid line numbers unless
  the user explicitly asks for them or the symbol reference would be ambiguous.

## Protobuf Generation

Dual generation pipeline matching upstream Cosmos SDK v0.53:

- `proto/buf.gen.gogo.yaml`: gocosmos + grpc-gateway → `x/*/types/*.pb.go` (typed Go structs via `gogoproto.customtype`)
- `proto/buf.gen.yaml`: go-pulsar + go-grpc → `api/*.pulsar.go` (standard protobuf, managed mode)
- Proto files use `option go_package = "noah/x/{module}/types"` to route gogo output
- Run `make proto-gen` to regenerate (runs Docker proto-builder)
- Proto-gen uses a named Docker volume (`noah-proto-cache`) for BSR dependency caching
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
- Default param values: use `const` for compile-time literals (integers, strings); `var` only for runtime init (function
  calls, struct/slice literals)
- **Collection access convention** (matches upstream SDK: mint, gov, bank):
  - Collection fields are **public** — callers use `k.Params.Get(ctx)`, `k.TobinTax.Get(ctx, denom)` directly
  - Only wrap with a keeper method when there's **real logic**: default-on-not-found (e.g., `GetFeederDelegation`
    defaults to validator itself), domain checks (e.g., `GetExchangeRate` has MicroArkDenom identity), side effects
    (e.g., `SetExchangeRateWithEvent` emits events)
  - If a wrapper is just `return k.Collection.Get(ctx, key)` with no extra logic, delete it — the collection IS the
    getter

## Build & Verification

- Always run `go build ./...` after making code changes to verify compilation
- Proto generation: `make proto-gen` (also runs `go mod tidy`)
- Proto formatting: `make proto-format`
- Proto linting: `make proto-lint`
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
