# Legacy Pattern Audit

Audit files, directories, or the entire codebase for legacy/deprecated Cosmos SDK patterns and propose modernization fixes. This project is porting the full Terra Classic chain (not just one module) to cosmos-sdk v0.53.5.

If no specific path is provided, scan all `x/` modules and top-level app code. Use Task agents in parallel to audit multiple modules simultaneously for faster results.

## What to Check

Scan for these legacy patterns and flag each one found:

- **Logger**: `tendermint/libs/log` or `tendermint/log` — should be `cosmossdk.io/log`
- **Math types**: `sdk.Int`, `sdk.Dec`, `sdk.NewInt`, `sdk.NewDec` — should be `math.Int`, `math.LegacyDec` from `cosmossdk.io/math`
- **Params**: `x/params` ParamSubspace usage — should use collections-based module params
- **Protobuf**: gogoproto annotations or imports — should use pulsar/cosmos-proto
- **Struct embedding**: `UnimplementedQueryServer` or `UnimplementedMsgServer` embedded in Keeper instead of a separate Querier/MsgServer wrapper
- **Keeper constructors**: manual wiring instead of depinject-compatible patterns
- **Module registration**: `AppModuleBasic` instead of depinject module registration
- **Errors**: `sdkerrors.Wrap` from `cosmos-sdk/types/errors` — should use `cosmossdk.io/errors`
- **Store keys**: direct KVStore access vs collections

## Steps

1. Read the target file(s) specified by the user
2. Check for each legacy pattern listed above
3. For each finding, show the offending code and the modern replacement
4. Run `go build ./...` after applying any fixes to verify compilation

## Output Format

For each finding:
- **Pattern**: Which legacy pattern was found
- **Location**: File and line number
- **Current**: The legacy code
- **Modern**: What it should be replaced with
- **Risk**: Low/Medium/High — how likely this is to cause issues
