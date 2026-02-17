## Project Context

This is a Cosmos SDK blockchain project porting the full Terra Classic chain to modern Cosmos SDK conventions. The market module is the first being ported, with additional modules to follow. The chain uses **cosmos-sdk v0.53.5** with depinject and `cosmossdk.io/*` packages.

Key differences between legacy (Terra Classic / cosmos-sdk v0.45) and modern patterns to always consider:
- **Dependency injection**: depinject-based module wiring, not manual constructor calls
- **Logging**: `cosmossdk.io/log` not `tendermint/libs/log`
- **Protobuf codegen**: pulsar-based, not gogoproto
- **Params**: collections-based module params, not `x/params`
- **Math types**: `cosmossdk.io/math` (math.Int, math.LegacyDec), not `sdk.Int`/`sdk.Dec`
- **Genesis**: keeper-level InitGenesis/ExportGenesis
- **Module registration**: depinject module.Manager patterns, not legacy AppModuleBasic

Reference codebases:
- **New chain**: `x/market/` (this repo)
- **Terra Classic reference**: `../classic-core/` (cosmos-sdk v0.45)
- **Upstream Cosmos SDK reference**: `../cosmos-sdk/`

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

## Build & Verification

- Always run `go build ./...` after making code changes to verify compilation
- If proto/buf files are modified, also run the buf generation command and verify output
