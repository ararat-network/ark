# Migration Notes: Terra Classic → Modern Cosmos SDK

This document covers the key architectural differences between Terra Classic (cosmos-sdk v0.45) and our modern
implementation (cosmos-sdk v0.53.5). Use this as a reference when porting modules.

---

## Proto Generation: Dual Pipeline (gogo + pulsar)

### Terra Classic (gogoproto only)

Proto files in `classic-core/proto/terra/*/v1beta1/` use gogoproto annotations extensively:

```protobuf
message Params {
  option (gogoproto.equal)            = true;
  option (gogoproto.goproto_stringer) = false;

  bytes base_pool = 1 [
    (gogoproto.moretags)   = "yaml:\"base_pool\"",
    (gogoproto.customtype) = "github.com/cosmos/cosmos-sdk/types.Dec",
    (gogoproto.nullable)   = false
  ];
}
```

Legacy gogoproto annotations to remove:

- `gogoproto.equal` / `gogoproto.goproto_stringer` / `gogoproto.goproto_getters` — removed
- `gogoproto.moretags` with `yaml:` tags — replaced by `amino` annotations
- `customtype = "github.com/cosmos/cosmos-sdk/types.Dec"` — updated to `cosmossdk.io/math.LegacyDec`

### Modern (Dual: gogo + pulsar)

Following upstream SDK v0.53, we use **both** gogo and pulsar codegen. Proto files in `proto/noah/*/v1/` use the full
annotation pattern:

```protobuf
message Params {
  option (amino.name) = "noah/market/Params";

  string base_pool = 1 [
    (cosmos_proto.scalar)  = "cosmos.Dec",
    (gogoproto.customtype) = "cosmossdk.io/math.LegacyDec",
    (gogoproto.nullable)   = false,
    (amino.dont_omitempty) = true
  ];
  uint64 pool_recovery_period = 2 [(amino.dont_omitempty) = true];
  string min_stability_spread = 3 [
    (cosmos_proto.scalar)  = "cosmos.Dec",
    (gogoproto.customtype) = "cosmossdk.io/math.LegacyDec",
    (gogoproto.nullable)   = false,
    (amino.dont_omitempty) = true
  ];
}
```

Code generation uses two configs:

**`proto/buf.gen.gogo.yaml`** — gogo output to `x/*/types/` (typed Go structs + grpc-gateway):

```yaml
plugins:
  - name: gocosmos
    out: ..
    opt: plugins=grpc,Mgoogle/protobuf/any.proto=github.com/cosmos/gogoproto/types/any
  - name: grpc-gateway
    out: ..
    opt: logtostderr=true,allow_colon_final_segments=true
```

**`proto/buf.gen.yaml`** — pulsar output to `api/` (standard protobuf):

```yaml
plugins:
  - name: go-pulsar
    out: ../api
    opt: paths=source_relative
  - name: go-grpc
    out: ../api
    opt: paths=source_relative
```

### Annotation Pattern Reference

For `math.LegacyDec` / `math.Int` fields:

- `cosmos_proto.scalar` = runtime metadata (signing, textual rendering, amino JSON)
- `gogoproto.customtype` = compile-time codegen (typed Go field instead of `string`)
- `gogoproto.nullable = false` = value type, not pointer
- `amino.dont_omitempty = true` = only on fields in Params/Msg types (messages with `amino.name`)

For sub-message fields (Params, Coin):

- `gogoproto.nullable = false` = value type
- `amino.dont_omitempty = true` = on fields in Params/Msg/GenesisState

For repeated Coin fields:

- `gogoproto.nullable = false` + `gogoproto.castrepeated = "github.com/cosmos/cosmos-sdk/types.Coins"`

### Key Differences

| Aspect              | Classic gogoproto                     | Modern dual pipeline                                                                         |
| ------------------- | ------------------------------------- | -------------------------------------------------------------------------------------------- |
| Decimal fields      | `bytes` with `customtype = "sdk.Dec"` | `string` with `cosmos_proto.scalar` + `gogoproto.customtype = "cosmossdk.io/math.LegacyDec"` |
| Nullability         | `gogoproto.nullable = false`          | Same — still used for value types                                                            |
| Generated output    | In-place with module code             | gogo → `x/*/types/`, pulsar → `api/`                                                         |
| YAML tags           | `gogoproto.moretags`                  | `amino.dont_omitempty` + `amino.name`                                                        |
| Codec               | Protobuf v1 (gogo fork) only          | Both gogo (v1) and pulsar (v2)                                                               |
| Removed annotations | —                                     | `gogoproto.equal`, `goproto_stringer`, `moretags` with yaml                                  |

### Files

- Classic: `classic-core/proto/terra/*/v1beta1/{market,tx,query,genesis}.proto`
- Modern: `proto/noah/*/v1/{market,tx,query,genesis}.proto`
- Gogo config: `proto/buf.gen.gogo.yaml`
- Pulsar config: `proto/buf.gen.yaml`

---

## Keeper Patterns

### Terra Classic

`classic-core/x/{module}/keeper/keeper.go` (market example):

```go
type Keeper struct {
    storeKey   sdk.StoreKey
    cdc        codec.BinaryCodec
    paramSpace paramstypes.Subspace

    AccountKeeper types.AccountKeeper
    BankKeeper    types.BankKeeper
    OracleKeeper  types.OracleKeeper
}

func NewKeeper(
    cdc codec.BinaryCodec,
    storeKey sdk.StoreKey,
    paramstore paramstypes.Subspace,
    accountKeeper types.AccountKeeper,
    bankKeeper types.BankKeeper,
    oracleKeeper types.OracleKeeper,
) Keeper {
    if !paramstore.HasKeyTable() {
        paramstore = paramstore.WithKeyTable(types.ParamKeyTable())
    }
    return Keeper{
        cdc: cdc, storeKey: storeKey, paramSpace: paramstore,
        AccountKeeper: accountKeeper, BankKeeper: bankKeeper, OracleKeeper: oracleKeeper,
    }
}
```

### Modern

`x/{module}/keeper/keeper.go` (market example):

```go
type Keeper struct {
    cdc          codec.BinaryCodec
    storeService storetypes.KVStoreService
    authority    string

    AccountKeeper types.AccountKeeper
    BankKeeper    types.BankKeeper
    OracleKeeper  types.OracleKeeper

    Schema        collections.Schema
    Params        collections.Item[types.Params]        // gogo value type, not pointer
    NoahPoolDelta collections.Item[math.LegacyDec]
}

func NewKeeper(
    cdc codec.BinaryCodec,
    storeService storetypes.KVStoreService,
    accountKeeper types.AccountKeeper,
    bankKeeper types.BankKeeper,
    oracleKeeper types.OracleKeeper,
    authority string,
) *Keeper {
    sb := collections.NewSchemaBuilder(storeService)
    k := &Keeper{
        cdc: cdc, storeService: storeService, authority: authority,
        AccountKeeper: accountKeeper, BankKeeper: bankKeeper, OracleKeeper: oracleKeeper,
        Params:        collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
        NoahPoolDelta: collections.NewItem(sb, types.NoahPoolDeltaKey, "noah_pool_delta", sdk.LegacyDecValue),
    }
    schema, err := sb.Build()
    if err != nil { panic(err) }
    k.Schema = schema
    return k
}
```

### Key Differences

| Aspect           | Classic                                            | Modern                                                             |
| ---------------- | -------------------------------------------------- | ------------------------------------------------------------------ |
| Store access     | `sdk.StoreKey` + raw KVStore                       | `storetypes.KVStoreService`                                        |
| State management | Manual `store.Get()`/`store.Set()` with marshaling | `collections.Item` / `collections.Map`                             |
| Params           | `paramstypes.Subspace`                             | `collections.Item[types.Params]` (gogo value type)                 |
| Authority        | Not present (governance via param proposals)       | Explicit `authority string` for `MsgUpdateParams`                  |
| Return type      | Value `Keeper`                                     | Pointer `*Keeper`                                                  |
| Pool delta       | `DecProto` wrapper with manual marshal             | `collections.Item[math.LegacyDec]` with `sdk.LegacyDecValue` codec |

---

## Param Handling: x/params vs Collections

### Terra Classic (x/params)

Params defined in `classic-core/x/{module}/types/params.go` (market example):

```go
var (
    KeyBasePool           = []byte("BasePool")
    KeyPoolRecoveryPeriod = []byte("PoolRecoveryPeriod")
    KeyMinStabilitySpread = []byte("MinStabilitySpread")
)

func ParamKeyTable() paramstypes.KeyTable {
    return paramstypes.NewKeyTable().RegisterParamSet(&Params{})
}

func (p *Params) ParamSetPairs() paramstypes.ParamSetPairs {
    return paramstypes.ParamSetPairs{
        paramstypes.NewParamSetPair(KeyBasePool, &p.BasePool, validateBasePool),
        paramstypes.NewParamSetPair(KeyPoolRecoveryPeriod, &p.PoolRecoveryPeriod, validatePoolRecoveryPeriod),
        paramstypes.NewParamSetPair(KeyMinStabilitySpread, &p.MinStabilitySpread, validateMinStabilitySpread),
    }
}
```

Accessed in keeper via `classic-core/x/{module}/keeper/params.go`:

```go
func (k Keeper) BasePool(ctx sdk.Context) (res sdk.Dec) {
    k.paramSpace.Get(ctx, types.KeyBasePool, &res)
    return
}

func (k Keeper) GetParams(ctx sdk.Context) (params types.Params) {
    k.paramSpace.GetParamSet(ctx, &params)
    return params
}
```

### Modern (Collections)

Params storage defined directly in the keeper (`x/{module}/keeper/keeper.go`):

```go
Params: collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc))
```

Keys defined in `x/{module}/types/keys.go`:

```go
var (
    ParamsKey        = collections.NewPrefix(0)
    NoahPoolDeltaKey = collections.NewPrefix(1)
)
```

Accessed directly on the keeper:

```go
params, err := k.Params.Get(ctx)
err := k.Params.Set(ctx, msg.Params)
```

Updated via governance through `MsgUpdateParams` in `x/{module}/keeper/msg_server.go`:

```go
func (m msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
    if m.k.authority != msg.Authority {
        return nil, errors.Wrapf(govtypes.ErrInvalidSigner, "...")
    }
    if err := msg.Params.Validate(); err != nil {
        return nil, err
    }
    if err := m.k.Params.Set(ctx, msg.Params); err != nil {
        return nil, err
    }
    return &types.MsgUpdateParamsResponse{}, nil
}
```

### Key Differences

| Aspect           | x/params                               | Collections                               |
| ---------------- | -------------------------------------- | ----------------------------------------- |
| Storage          | ParamSubspace with key-value pairs     | `collections.Item` with typed codec       |
| Keys             | `[]byte` constants                     | `collections.Prefix`                      |
| Access           | `paramSpace.Get(ctx, key, &val)`       | `k.Params.Get(ctx)`                       |
| Mutation         | `paramSpace.SetParamSet(ctx, &params)` | `k.Params.Set(ctx, params)`               |
| Governance       | Param change proposals (legacy)        | `MsgUpdateParams` with authority check    |
| Validation       | `ParamSetPairs` with validators        | Validated in `MsgUpdateParams` handler    |
| Per-field access | Individual getter per param            | Single `Get()` returns full Params struct |

### Porting Checklist for Params

1. Remove `ParamKeyTable()`, `ParamSetPairs()`, and per-param byte keys
2. Add `collections.NewPrefix` keys in `types/keys.go`
3. Replace `paramstypes.Subspace` in keeper with `collections.Item[types.Params]` using
   `codec.CollValue[types.Params](cdc)`
4. Add `authority string` field to keeper
5. Implement `MsgUpdateParams` message and handler
6. Remove individual param getter methods; use `k.Params.Get(ctx)` instead

---

## Message Server Patterns

### Terra Classic

`classic-core/x/{module}/keeper/msg_server.go` (market example):

```go
type msgServer struct {
    Keeper  // embedded — direct struct embedding
}

func NewMsgServerImpl(keeper Keeper) types.MsgServer {
    return &msgServer{Keeper: keeper}
}
```

Message handlers unwrap context manually:

```go
func (k msgServer) Swap(goCtx context.Context, msg *types.MsgSwap) (*types.MsgSwapResponse, error) {
    ctx := sdk.UnwrapSDKContext(goCtx)
    // ... use ctx for store access, events, etc.
}
```

### Modern

`x/{module}/keeper/msg_server.go` (market example):

```go
type msgServer struct {
    k *Keeper  // named field — avoids method shadowing
}

func NewMsgServerImpl(k *Keeper) types.MsgServer {
    return msgServer{k: k}
}
```

Key changes in message handlers:

- `context.Context` used directly (no `sdk.UnwrapSDKContext` needed for collections)
- `MsgUpdateParams` added for governance-controlled param updates
- All module code uses gogo types from `x/{module}/types` — message fields are typed (`sdk.Coin`, `math.LegacyDec`), not
  strings/pointers
- Methods can be defined on gogo types (e.g. `Params.Validate()`, `MsgSwap.ValidateBasic()`) since they live in the same
  package
- Pulsar types in `api/` are only for the runtime/signing layer — never imported in module code

### Key Differences

| Aspect               | Classic                           | Modern                                                                             |
| -------------------- | --------------------------------- | ---------------------------------------------------------------------------------- |
| Keeper reference     | Embedded `Keeper` struct          | Named field `k *Keeper` (pointer)                                                  |
| Unimplemented server | Not embedded                      | `UnimplementedMsgServer` embedded for forward compat                               |
| Context              | `sdk.UnwrapSDKContext(goCtx)`     | `context.Context` passed directly to collections                                   |
| Generated types      | `x/{module}/types` (gogoproto)    | `x/{module}/types` (gogo, used in all module code) + `api/` (pulsar, runtime only) |
| Param updates        | Via legacy param change proposals | Explicit `MsgUpdateParams` handler                                                 |

### Porting Checklist for MsgServer

1. Change keeper from embedded struct to named `k *Keeper` field (pointer)
2. Embed `UnimplementedMsgServer` from gogo-generated `x/{module}/types` package
3. Add `MsgUpdateParams` handler with authority check + `msg.Params.Validate()`
4. Use gogo types throughout — `msg.OfferCoin` is `sdk.Coin` (value), `msg.Params` is `types.Params` (value)
5. Remove `sdk.UnwrapSDKContext` where not needed (collections accept `context.Context`)
6. Add `ValidateBasic()` methods on `MsgSwap`, `MsgSwapSend` etc. in `x/{module}/types/msgs.go`
7. Legacy Msg interface methods (`Route`, `Type`, `GetSignBytes`, `GetSigners`) are NOT needed — replaced by proto
   annotations

---

## Module Registration: AppModuleBasic vs Depinject

### Terra Classic

`classic-core/x/{module}/module.go` (market example):

```go
type AppModuleBasic struct {
    cdc codec.Codec
}

func (AppModuleBasic) Name() string                    { return types.ModuleName }
func (AppModuleBasic) RegisterLegacyAminoCodec(...)    { ... }
func (AppModuleBasic) RegisterInterfaces(...)           { ... }
func (AppModuleBasic) DefaultGenesis(...)               { ... }
func (AppModuleBasic) ValidateGenesis(...)              { ... }
func (AppModuleBasic) RegisterRESTRoutes(...)           { ... }  // REST API (deprecated)
func (AppModuleBasic) RegisterGRPCGatewayRoutes(...)    { ... }
func (AppModuleBasic) GetTxCmd() *cobra.Command         { ... }
func (AppModuleBasic) GetQueryCmd() *cobra.Command      { ... }

type AppModule struct {
    AppModuleBasic
    keeper        keeper.Keeper
    accountKeeper types.AccountKeeper
    bankKeeper    types.BankKeeper
    oracleKeeper  types.OracleKeeper
}

func NewAppModule(cdc codec.Codec, keeper keeper.Keeper, ...) AppModule {
    return AppModule{AppModuleBasic: AppModuleBasic{cdc}, keeper: keeper, ...}
}
```

Module wired manually in `app.go`:

```go
app.MarketKeeper = marketkeeper.NewKeeper(
    appCodec, keys[markettypes.StoreKey], app.GetSubspace(markettypes.ModuleName),
    app.AccountKeeper, app.BankKeeper, app.OracleKeeper,
)
```

### Modern

`x/{module}/module.go` — depinject registration (market example):

```go
func init() {
    appmodule.Register(
        &modulev1.Module{},
        appmodule.Provide(ProvideModule),
    )
}

type ModuleInputs struct {
    depinject.In
    Config       *modulev1.Module
    Cdc          codec.Codec
    StoreService store.KVStoreService
    AccountKeeper types.AccountKeeper
    BankKeeper    types.BankKeeper
    OracleKeeper  types.OracleKeeper
}

type ModuleOutputs struct {
    depinject.Out
    MarketKeeper *keeper.Keeper
    Module       appmodule.AppModule
}

func ProvideModule(in ModuleInputs) ModuleOutputs {
    authority := authtypes.NewModuleAddress(govtypes.ModuleName)
    if in.Config.Authority != "" {
        authority = authtypes.NewModuleAddressOrBech32Address(in.Config.Authority)
    }
    k := keeper.NewKeeper(in.Cdc, in.StoreService, in.AccountKeeper, in.BankKeeper, in.OracleKeeper, authority.String())
    m := NewAppModule(in.Cdc, k, in.AccountKeeper, in.BankKeeper, in.OracleKeeper)
    return ModuleOutputs{MarketKeeper: k, Module: m}
}
```

Module config proto in `proto/noah/{module}/module/v1/module.proto`:

```protobuf
message Module {
    option (cosmos.app.v1alpha1.module) = {
        go_import : "noah/x/market"
    };
    string authority = 1;
}
```

Module registered in `app/app_config.yaml` (not manually in app.go):

```yaml
modules:
  - name: market
    config:
      "@type": noah.market.module.v1.Module
      authority: cosmos10d07y265gmmuvt4z0w9aw880jnsr700j6zn9kn
```

### Key Differences

| Aspect              | Classic                        | Modern                                               |
| ------------------- | ------------------------------ | ---------------------------------------------------- |
| Wiring              | Manual in `app.go`             | Depinject via `ProvideModule`                        |
| Config              | Constructor args               | `module.proto` + `app_config.yaml`                   |
| REST routes         | `RegisterRESTRoutes` (mux)     | Removed (gRPC-gateway only)                          |
| Interfaces          | `AppModuleBasic` + `AppModule` | `appmodule.AppModule` + `appmodule.HasEndBlocker`    |
| BeginBlock/EndBlock | `abci.RequestBeginBlock` param | No params (uses `appmodule.HasEndBlocker` interface) |
| Module account      | Registered in `maccPerms` map  | Declared in module config                            |

### Porting Checklist for Module Registration

1. Create `proto/<chain>/<module>/module/v1/module.proto` with `cosmos.app.v1alpha1.module` option
2. Add `init()` with `appmodule.Register` and `appmodule.Provide(ProvideModule)`
3. Define `ModuleInputs` / `ModuleOutputs` structs with `depinject.In` / `depinject.Out`
4. Implement `ProvideModule` function that creates keeper and AppModule
5. Add module entry to `app/app_config.yaml`
6. Remove manual wiring from `app.go`
7. Remove `RegisterRESTRoutes` (legacy REST is deprecated)
8. Replace `BeginBlock(ctx, req)` / `EndBlock(ctx, req)` with parameterless versions via `appmodule.HasBeginBlocker` /
   `appmodule.HasEndBlocker`

---

## Quick Reference: Import Path Changes

| Classic Import                                  | Modern Import                                                                        |
| ----------------------------------------------- | ------------------------------------------------------------------------------------ |
| `github.com/cosmos/cosmos-sdk/types`            | `github.com/cosmos/cosmos-sdk/types` (unchanged)                                     |
| `github.com/cosmos/cosmos-sdk/x/params/types`   | `cosmossdk.io/collections`                                                           |
| `github.com/tendermint/tendermint/libs/log`     | `cosmossdk.io/log`                                                                   |
| `github.com/cosmos/cosmos-sdk/types` (Int, Dec) | `cosmossdk.io/math`                                                                  |
| `github.com/cosmos/cosmos-sdk/types/errors`     | `cosmossdk.io/errors`                                                                |
| `github.com/cosmos/cosmos-sdk/store/types`      | `cosmossdk.io/store/types`                                                           |
| `github.com/gogo/protobuf/...`                  | `github.com/cosmos/gogoproto/...` (gogo) + `google.golang.org/protobuf/...` (pulsar) |
| `github.com/cosmos/cosmos-sdk/types/module`     | `cosmossdk.io/core/appmodule` (for interfaces)                                       |
