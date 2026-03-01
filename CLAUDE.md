## Project Context

This is a Cosmos SDK blockchain project porting the full Terra Classic chain to modern Cosmos SDK conventions. Active modules: `x/market/`, `x/oracle/`, `x/treasury/`. The chain uses **cosmos-sdk v0.53.5** with depinject and `cosmossdk.io/*` packages.

Key differences between legacy (Terra Classic / cosmos-sdk v0.45) and modern patterns to always consider:
- **Dependency injection**: depinject-based module wiring, not manual constructor calls
- **Logging**: `cosmossdk.io/log` not `tendermint/libs/log`
- **Protobuf codegen**: dual generation — gogo (`x/*/types/*.pb.go`) + pulsar (`api/*.pulsar.go`), following upstream SDK v0.53 pattern
- **Proto annotations**: use all three together on Dec/Int fields: `cosmos_proto.scalar` + `gogoproto.customtype` + `gogoproto.nullable = false` (+ `amino.dont_omitempty` in Params/Msg types)
- **Params**: collections-based module params, not `x/params`
- **Math types**: `cosmossdk.io/math` (math.Int, math.LegacyDec), not `sdk.Int`/`sdk.Dec`
- **Genesis**: keeper-level InitGenesis/ExportGenesis
- **Module registration**: depinject module.Manager patterns, not legacy AppModuleBasic

Reference codebases:
- **New chain**: `x/market/`, `x/oracle/`, `x/treasury/` (this repo)
- **Terra Classic reference**: `../classic-core/` (cosmos-sdk v0.45)
- **Upstream Cosmos SDK reference**: `../cosmos-sdk/`

## Project Structure

```
x/market/       # DEX swap module (Luna ↔ stablecoins)
x/oracle/       # Price oracle module (validator price voting)
x/treasury/     # Macro policy module (tax rate, reward weight, seigniorage)
proto/noah/     # Proto definitions (market, treasury)
api/noah/       # Pulsar-generated code (runtime only, never import in module code)
app/            # App wiring, depinject config
```

## Cosmos SDK Conventions

When making code changes:
- Always check the exact cosmos-sdk version in go.mod before suggesting imports or patterns
- Use `cosmossdk.io/log` not `tendermint/libs/log`
- Prefer named fields over struct embedding in keeper/querier types to avoid method shadowing
- When editing proto files, explain proposed changes before applying them and wait for confirmation

## Git Workflow

- Before running any `git push` commands, verify that a remote is configured with `git remote -v`
- Never assume a remote exists

## General Rules

- Before making any changes, first outline exactly what files you'll modify and what the changes will be. Show the key diffs. Wait for approval before editing. This is especially important for proto files and keeper/module wiring.
- When the user references a specific file path or directory (e.g., "look at classic-core/types"), navigate to exactly that path. Do not substitute a similarly-named path from a different part of the codebase.

## Protobuf Generation

Dual generation pipeline matching upstream Cosmos SDK v0.53:
- `buf.gen.gogo.yaml`: gocosmos + grpc-gateway → `x/*/types/*.pb.go` (typed Go structs via `gogoproto.customtype`)
- `buf.gen.yaml`: go-pulsar + go-grpc → `api/*.pulsar.go` (standard protobuf, managed mode)
- Proto files use `option go_package = "noah/x/{module}/types"` to route gogo output
- Run `make proto-gen` to regenerate (runs Docker proto-builder)

Proto annotation pattern for Dec/Int fields (follow SDK mint module exactly):
```protobuf
string field_name = N [
  (cosmos_proto.scalar)  = "cosmos.Dec",           // runtime: signing, textual rendering
  (gogoproto.customtype) = "cosmossdk.io/math.LegacyDec", // codegen: typed Go field
  (gogoproto.nullable)   = false,                  // codegen: value type, not pointer
  (amino.dont_omitempty) = true                    // amino: always include in JSON (all nullable=false fields)
];
```

## Build & Verification

- Always run `go build ./...` after making code changes to verify compilation
- If proto/buf files are modified, also run the buf generation command and verify output
- Run tests: `go test ./x/{module}/...` for a single module, or `go test ./...` for all
- Run with verbose output: `go test -v ./x/{module}/...`
