# Asset Module Plan

Status: Phase 0 implemented; remaining architecture proposed

## Purpose

Build x/asset as the authoritative registry and lifecycle owner for governance-managed Bank assets other than NOAH. The module must support stablecoins, tokenized commodities such as gold, and unpriced assets without treating every asset as a stablecoin.

The immediate goal is safe denomination retirement. An asset must not disappear from Oracle pricing, Treasury liabilities, redemption, or Market policy while issued supply or downstream dependencies remain.

## Core decisions

- Asset is the general domain name. Stablecoin remains a Treasury policy classification, not the name of the registry.
- unoah remains the chain's native base denomination and is outside the governable asset lifecycle.
- Every priced asset has exactly one authoritative Oracle rate keyed by its Bank denomination.
- Every asset rate is normalized directly against NOAH.
- x/asset owns asset identity, Bank metadata, lifecycle, Oracle-target epochs, AssetLocks, and lifecycle-aware valuation access.
- x/oracle owns validator voting, aggregation, accounting, denomination-keyed rates, and rate freshness.
- x/market owns conversion eligibility and Tobin-tax policy.
- x/treasury owns stable-liability classification and its reference-asset policy.
- Retirement stops new issuance first, preserves valuation and redemption, and removes pricing only after supply and dependency checks pass.
- Generalized feed IDs are deferred until there is a concrete need for non-asset observations, multiple prices per asset, or independently shared feed lifecycle.

## Scope boundaries

Version 1 does not provide:

- generalized on-chain price-feed identifiers separate from asset denoms;
- arbitrary non-asset Oracle observations;
- an on-chain provider or exchange-symbol registry;
- issuer, custody, proof-of-reserve, or physical redemption machinery for tokenized assets;
- a general-purpose decentralized exchange;
- lifecycle control over unoah;
- live-chain compatibility unless deployment requires an upgrade from committed state.

Provider markets and resolver routes remain generalized off-chain. The sidecar may combine pairs such as NOAH/USD, USD/XAU, and a constant unit conversion, but consensus receives one final denomination-specific NOAH rate.

## Price contract

The consensus rate contract is:

    rate[denom] = atomic units of denom per one unoah
    rate[unoah] = 1

Conversion remains:

    ask amount = offer amount * ask rate / offer rate

Because amounts and rates use base-denom atomic units, the formula supports assets with different display exponents without adding an on-chain feed binding or unit-conversion layer.

For example, the sidecar may derive tokenized gold from external XAU markets, but validators report the final result as:

    ugold atomic units per unoah

The sidecar is responsible for converting provider output into this protocol unit before encoding a vote. Asset metadata defines the canonical display denom and exponent; resolver configuration supplies any economic conversion such as troy ounces to grams.

Only priced assets may enter Market conversion or Treasury stable-liability policy. An unpriced asset may be registered and transferred, but consensus monetary policy must not attempt to value, mint, burn, or redeem it.

## Ownership model

### x/asset

Owns:

- asset registration and Bank metadata;
- asset lifecycle status and versioning;
- whether an asset requires Oracle pricing;
- active and pending Oracle-target epochs;
- AssetLocks that prevent unsafe retirement;
- lifecycle-aware valuation snapshots for other modules;
- retirement-readiness queries.

Does not own:

- validator voting, aggregation, stored rates, or freshness policy;
- conversion curves or Tobin taxes;
- stable-liability classification;
- minting and burning authority.

### x/oracle

Owns:

- price reports and vote aggregation;
- denomination-keyed rate storage and pruning;
- rate validation and freshness;
- validator accounting and reward inputs;
- Oracle operational parameters unrelated to asset identity.

Does not own:

- the asset registry or Bank metadata;
- whether an asset may be issued or redeemed;
- conversion eligibility or stable-liability classification;
- target lifecycle policy.

### x/market

Owns:

- the set of assets supported by the native converter;
- per-asset conversion policy, including Tobin tax;
- the base-pool asset reference;
- issuance gates for conversion output.

Market obtains lifecycle-checked valuation snapshots from x/asset rather than using Oracle configuration as an asset registry.

### x/treasury

Owns:

- the set of assets counted as stable liabilities;
- tax-cap and reference-asset policy;
- redemption accounting and solvency rules;
- AssetLocks protecting stable enrollment and the reference asset.

Treasury obtains lifecycle-checked valuation snapshots from x/asset and never infers liabilities from Oracle targets.

## State model

The initial x/asset collections should be conceptually equivalent to:

    Assets       collections.Map[string, types.Asset]
    AssetLocks   collections.KeySet[collections.Pair[string, types.AssetLockKind]]
    VoteTargets  collections.Item[types.VoteTargets]

An asset contains at least:

    message Asset {
      string denom = 1;
      cosmos.bank.v1beta1.Metadata metadata = 2;
      AssetStatus status = 3;
      uint64 version = 4;
      bool oracle_enabled = 5;
    }

The denomination is the immutable asset identifier. version increments on every lifecycle or pricing-mode mutation. Governance messages that mutate an existing asset include expected_version so stale proposals cannot silently act on different state.

oracle_enabled means that the asset has a direct NOAH-relative Oracle rate and participates in target epochs. It does not imply Market conversion eligibility or Treasury stable classification.

The active and pending target sets contain sorted, unique asset denoms. They retain an explicit target version and activation vote height.

## Asset lifecycle

Priced assets use:

    PENDING -> ACTIVE -> RETIRING -> REMOVAL_PENDING -> RETIRED
       |          ^          |
       |          |----------| cancel retirement
       +---------------------------------------------> RETIRED (cancel registration)

Unpriced assets skip target scheduling and move directly from PENDING to ACTIVE and from RETIRING to RETIRED.

### PENDING

- The asset and Bank metadata exist.
- Market and Treasury policy may be prepared but cannot be used for issuance.
- A priced asset may be waiting for its scheduled target addition.
- An unscheduled registration can still be cancelled.

### ACTIVE

- Normal policy-dependent issuance, conversion, valuation, liability accounting, and redemption are available.
- Priced assets are active Oracle targets.
- New AssetLocks may be attached.

### RETIRING

- New issuance stops immediately.
- Existing supply remains transferable.
- Oracle pricing remains active.
- Treasury liability accounting and redemption remain active.
- Market may consume the asset in the retirement direction but cannot produce more of it.
- Governance may cancel retirement and return the asset to ACTIVE.

### REMOVAL_PENDING

- Bank supply is zero and no AssetLocks remain.
- Oracle target removal has been scheduled but has not activated.
- The asset remains priced until the old target epoch has been consumed.

### RETIRED

- Bank supply is zero.
- No AssetLocks remain.
- A priced asset is no longer an Oracle target and has no stored rate.
- The registry record remains as a tombstone so denomination history and version monotonicity are preserved.
- Reactivation begins a new PENDING flow rather than silently restoring old policy.

## Asset locks

An AssetLock records that another module's configuration still depends on an asset. It is an inverse dependency index, not a balance, rate, ownership record, or governance-supplied label.

The initial lock kinds are:

- MARKET_ASSET_POLICY
- MARKET_BASE_POOL
- TREASURY_STABLE_POLICY
- TREASURY_REFERENCE_TAX_CAP

AssetLocks are consensus state maintained through explicit x/asset keeper operations called by the owning modules. Final retirement fails while any lock exists.

Lock changes and the policy changes they protect must occur in the same transaction. For example, changing Market's base-pool denom removes MARKET_BASE_POOL from the old asset and adds it to the new asset atomically.

## Lifecycle operations

### RegisterAsset

- Validate the denomination and Bank metadata.
- Reject unoah and any previously registered denom, including a retired tombstone.
- Validate that metadata has a canonical display unit and exponent.
- Store the asset as PENDING with version 1 and the requested oracle_enabled value.
- Set Bank metadata through the Bank keeper.
- Do not grant mint authority or create initial supply.

### SetAssetPricing

- Require PENDING or RETIRED status and the expected version.
- Require Bank supply to be zero.
- Allow oracle_enabled to change only when no target addition or removal is scheduled for the asset.
- Never allow an ACTIVE, RETIRING, or REMOVAL_PENDING asset to change pricing mode.

### ActivateAsset

- Require PENDING status and the expected version.
- For an unpriced asset, move immediately to ACTIVE.
- For a priced asset, schedule target addition at the protocol activation height and remain PENDING.
- Reject activation while a different target transition is pending.

### CancelAssetRegistration

- Require PENDING status, the expected version, zero supply, and no AssetLocks.
- Require that no target addition is scheduled for the asset.
- Move directly to RETIRED while retaining the tombstone.
- Once target activation is scheduled, require activation followed by normal retirement so target-version history remains monotonic.

### BeginRetirement

- Require ACTIVE status and the expected version.
- Move immediately to RETIRING.
- Treat this transition as the issuance cutoff.
- Preserve pricing, liability accounting, transfers, and redemption.

### CancelRetirement

- Require RETIRING status and the expected version.
- Return to ACTIVE.
- Preserve Oracle, Market, and Treasury state.

### FinalizeRetirement

- Require RETIRING status and the expected version.
- Require Bank supply to be exactly zero.
- Require no AssetLocks.
- For an unpriced asset, move directly to RETIRED.
- For a priced asset, schedule target removal and move to REMOVAL_PENDING.
- Reject finalization while a different target transition is pending.

### Target epoch activation

At the scheduled activation height:

- consume reports for the old epoch before promotion;
- promote the pending target set;
- move added PENDING assets to ACTIVE;
- move removed REMOVAL_PENDING assets to RETIRED;
- return removed denoms to the Oracle application pipeline;
- prune their stored rates only after the old epoch has been consumed.

### ReactivateAsset

- Require RETIRED status and the expected version.
- Move to PENDING.
- Require Market and Treasury policy to be established again explicitly.
- If priced, require a new target activation before becoming ACTIVE.

## Oracle target epochs

x/asset should absorb the existing target scheduler and preserve its protocol semantics:

- target selection is based on vote height, not merely current block state;
- activation remains delayed by the existing two-height boundary;
- only one pending target transition exists at a time;
- aggregation consumes the old epoch before promotion;
- removed rates are pruned at promotion, not when governance requests retirement;
- target count, uniqueness, denom validity, version, and height checks remain consensus rules.

The sidecar warms the active and pending union, but the chain's vote height and target version select the consensus key set. Vote extensions continue to carry denom-keyed rates and target_version.

The primary interfaces should remain narrow:

    type ABCIAssetKeeper interface {
        GetVoteTargets(ctx context.Context, voteHeight int64) (assettypes.VoteTargetSet, error)
        AdvanceVoteTargets(ctx context.Context) ([]string, error)
    }

    type ABCIOracleKeeper interface {
        GetParams(ctx context.Context) (oracletypes.Params, error)
        SetExchangeRateWithEvent(ctx context.Context, rate oracletypes.ExchangeRate) error
        RemoveExchangeRate(ctx context.Context, denom string) error
        RecordVoteAccounting(...)
    }

    type AssetOracleKeeper interface {
        GetRateSnapshot(ctx context.Context, denoms ...string) (oracletypes.RateSnapshot, error)
    }

    type AssetKeeper interface {
        GetAsset(ctx context.Context, denom string) (assettypes.Asset, error)
        GetValuationSnapshot(ctx context.Context, denoms ...string) (assettypes.ValuationSnapshot, error)
    }

The exact interfaces should be declared at their consumer boundaries rather than as one broad shared keeper interface.

ValuationSnapshot combines one fresh Oracle RateSnapshot with immutable asset state. It ensures every requested non-NOAH denom is registered, priced, and in a lifecycle state permitted by the caller. Market and Treasury should use this boundary rather than duplicating asset lookup and Oracle freshness logic.

## Sidecar normalization

The sidecar already resolves generalized provider pairs internally. The public price snapshot remains keyed by target denom.

For every target, sidecar configuration must resolve a final economic rate and normalize it to atomic denom units per unoah. The normalization is:

    atomic rate = display units per display NOAH
                  * 10^asset display exponent
                  / 10^NOAH display exponent

The x/asset target query should expose enough immutable metadata for this conversion, at minimum denom, display denom, and display exponent. Resolver configuration remains responsible for economic transformations such as XAU to grams.

Validation must reject missing, non-positive, non-finite, or unrepresentable normalized rates before vote encoding. Consensus aggregation continues to operate only on the final normalized values.

## Market policy

Replace Oracle-owned Tobin-tax entries with a Market-owned policy map:

    AssetPolicies collections.Map[string, types.AssetPolicy]

    message AssetPolicy {
      string denom = 1;
      string tobin_tax = 2;
    }

Policy presence means that the native converter supports the asset. A tokenized commodity may be registered and priced without having a Market policy.

Market rules:

- policy enrollment requires a priced asset;
- offer assets must have a Market policy and be ACTIVE or RETIRING;
- ask assets must have a Market policy and be ACTIVE;
- obtain one x/asset ValuationSnapshot for every denom used by quote and settlement;
- settlement rechecks the ask asset immediately before minting;
- RETIRING assets may be consumed but never produced;
- policy removal requires RETIRING status and zero supply;
- an asset carrying MARKET_BASE_POOL cannot remove its Market policy;
- parameter updates move MARKET_BASE_POOL atomically.

## Treasury policy

Treasury should keep an explicit stable-liability set:

    StableAssets collections.KeySet[string]

Registration in x/asset does not make an asset a stablecoin. Tokenized gold, securities, and other assets remain outside stable-liability accounting unless governance explicitly enrolls them.

Treasury rules:

- enrollment requires an ACTIVE priced asset;
- enrolled supply remains a liability while the asset is RETIRING;
- liability valuation uses x/asset ValuationSnapshot;
- redemption remains supported throughout retirement;
- stable-policy removal requires RETIRING status and zero supply;
- reference-tax-cap changes move TREASURY_REFERENCE_TAX_CAP atomically;
- the reference asset cannot retire until Treasury has moved the reference.

All liability, tax-cap, funding, redemption, ABCI, and query paths must enumerate Treasury's StableAssets rather than infer liabilities from Oracle targets or Market policy.

## Protobuf surface

Create:

- proto/ark/asset/module/v1/module.proto
- proto/ark/asset/v1/asset.proto
- proto/ark/asset/v1/genesis.proto
- proto/ark/asset/v1/tx.proto
- proto/ark/asset/v1/query.proto
- proto/ark/asset/v1/event.proto

Initial governance messages:

- RegisterAsset
- SetAssetPricing
- ActivateAsset
- CancelAssetRegistration
- BeginRetirement
- CancelRetirement
- FinalizeRetirement
- ReactivateAsset

Initial queries:

- Asset
- Assets with pagination
- AssetLocks
- VoteTargets
- OracleTargets with denom and normalization metadata
- Valuation
- RetirementStatus

RetirementStatus should report current supply, lifecycle state, version, pricing mode, AssetLocks, pending target activation, and every blocker. This is the main governance preflight query.

Emit explicit events for registration, pricing changes, activation scheduling, activation, registration cancellation, retirement start, retirement cancellation, removal scheduling, final retirement, reactivation, and AssetLock changes.

Oracle protobuf changes should remove asset-registry policy from Params and Genesis only when the new ownership path is complete. Reserve every removed field number, including Params tag 5 and GenesisState vote_targets tag 5.

## Module implementation layout

The expected structure is:

    x/asset/
      keeper/
        keeper.go
        msg_server.go
        query_server.go
        genesis.go
        lifecycle.go
        asset_locks.go
        vote_targets.go
        valuation.go
      types/
        errors.go
        expected_keepers.go
        keys.go
        pricing.go
        vote_targets.go
      module.go
      depinject.go

The x/asset keeper needs narrow Bank capabilities:

- GetSupply
- GetDenomMetaData
- SetDenomMetaData

It also needs the narrow Oracle RateSnapshot interface for valuation. x/oracle does not depend back on x/asset; the application ABCI pipeline receives the two keepers separately.

x/asset does not need a module account or mint permissions.

## Genesis

For a clean prelaunch migration:

- seed the existing eight stable assets as ACTIVE and oracle_enabled;
- seed the current active and pending target epoch, version, and activation height in x/asset;
- keep stored denomination-keyed rates in x/oracle;
- move Bank metadata ownership from Oracle genesis to Asset genesis;
- seed Market policies from the current Tobin-tax configuration;
- seed Treasury StableAssets from the current stable set;
- rebuild AssetLocks deterministically from Market and Treasury genesis state;
- do not accept an independently supplied AssetLock list in Asset genesis.

Asset genesis validation must ensure that active priced assets and target state agree exactly, pending lifecycle states match the scheduled transition, metadata is valid, and no unpriced asset appears in a target set.

## Application wiring

Add AssetKeeper to dependency injection and ArkApp. The intended genesis order is:

    bank -> asset -> oracle -> market -> treasury

x/asset does not need its own BeginBlocker or EndBlocker. The application Oracle preblock pipeline:

1. selects the target epoch for the committed vote height;
2. aggregates and stores rates in x/oracle;
3. records Oracle accounting;
4. advances x/asset target state after old-epoch consumption;
5. asks x/oracle to prune the returned denoms.

Expected integration touchpoints include:

- app_config.go
- app/app.go
- app/oracle.go
- app tests and keeper wiring tests
- ABCI Oracle and Asset interfaces and generated mocks
- vote extension, proposal, aggregation, and preblock consumers
- sidecar target polling and normalization
- transport, validation, and CLI target queries
- Market and Treasury expected keepers and generated mocks

## Implementation phases

### Phase 0: Contain the confirmed defect

Implemented: Oracle UpdateParams rejects any update that removes a configured target, including emptying a non-empty set. Additions and non-target parameter changes remain valid.

This guard stays until the old parameter-driven target-removal path no longer exists.

### Phase 1: Define the x/asset protocol contract

- Finalize Asset, AssetStatus, AssetLockKind, vote-target state, genesis, governance messages, queries, and events.
- Define the atomic-units-per-unoah rate contract and sidecar normalization rules.
- Define lifecycle and expected_version semantics.
- Explain the exact protobuf changes and receive approval before editing them.
- Generate gogo and Pulsar outputs and add types-level validation tests.

### Phase 2: Build x/asset standalone

- Implement Assets, AssetLocks, target epochs, lifecycle operations, Bank metadata ownership, genesis, messages, queries, and events.
- Implement ValuationSnapshot over Oracle RateSnapshot.
- Add keeper suites for priced, unpriced, pending, active, retiring, removal-pending, and retired states.
- Keep production target and pricing consumers on their current paths until the replacement is complete.

### Phase 3: Cut target ownership over to x/asset

- Wire AssetKeeper through app genesis and dependency injection.
- Move target epoch state and scheduling from x/oracle to x/asset.
- Split ABCI dependencies into narrow Asset and Oracle interfaces.
- Update vote extension, proposal, aggregation, preblock, sidecar polling, validation, CLI, and mocks.
- Move Bank metadata registration from Oracle to Asset.
- Preserve target-version and two-height activation semantics exactly.
- Switch Market and Treasury pricing calls to x/asset ValuationSnapshot while retaining their existing classification policy temporarily.

### Phase 4: Move Market policy ownership

- Add AssetPolicies and governance operations to x/market.
- Move Tobin tax and conversion eligibility out of Oracle Params.
- Enforce lifecycle-aware offer, ask, and settlement rules.
- Add and maintain Market AssetLocks.
- Migrate Market genesis and queries.

### Phase 5: Move Treasury classification ownership

- Add StableAssets and governance operations to x/treasury.
- Replace every Oracle-target-derived liability enumeration.
- Preserve liability and redemption support throughout RETIRING.
- Add and maintain Treasury AssetLocks.
- Migrate Treasury genesis and queries.

### Phase 6: Remove legacy coupling and complete integration

- Remove TobinTaxes, TobinTax, vote-target state, target scheduling, metadata ownership, and obsolete queries from x/oracle.
- Reserve removed protobuf field numbers.
- Remove the temporary Oracle target-removal guard after the old path is gone.
- Add full lifecycle and normalization integration tests.

## Required integration scenario

At minimum, an end-to-end test must prove:

1. Register a priced asset with a non-default display exponent.
2. Activate it through the delayed Oracle target epoch.
3. Resolve and report its rate as atomic denom units per unoah.
4. Configure Market and Treasury policy where appropriate.
5. Issue non-zero supply.
6. Begin retirement and verify new issuance is rejected.
7. Verify Oracle valuation, Treasury liability accounting, transfers, and redemption continue.
8. Verify finalization fails while supply or AssetLocks remain.
9. Redeem or burn supply to zero.
10. Move Market and Treasury references and remove their policies atomically with their locks.
11. Finalize retirement and schedule target removal.
12. Verify the asset remains priced until the old epoch is consumed.
13. Verify its rate is pruned at target promotion.
14. Verify the RETIRED tombstone cannot be accidentally reused.

Add parallel cases for:

- an unpriced registered asset;
- a priced tokenized commodity that is not a stable liability;
- registration and retirement cancellation;
- attempts to change pricing mode after activation;
- stale expected_version values;
- concurrent pending target transitions;
- attempts to register or retire unoah;
- zero and non-zero Bank supply boundaries;
- each AssetLock kind as an independent blocker;
- sidecar normalization across different asset exponents;
- overflow, underflow, zero-rounding, and stale-rate failures.

## Live-chain upgrade variant

If this work ships after a live genesis, use a named software upgrade:

- add the x/asset store before loading the new application version;
- introduce Asset module version 1;
- bump Oracle, Market, and Treasury consensus versions;
- retain old protobuf decoding until migrations complete;
- seed assets, metadata, exact target version and activation height, Market policies, Treasury stable enrollment, and AssetLocks from committed state;
- verify every target maps to exactly one registered priced asset;
- reject or explicitly resolve any unsafe pending target removal;
- delete obsolete Oracle state only after migration validation succeeds.

Do not remove old protobuf fields or store decoders before the upgrade handler can read prior state.

For a prelaunch chain, prefer a clean genesis break and avoid temporary compatibility code.

## Verification

Run scope-matched tests after each phase and the complete suite at integration boundaries:

    make proto-format
    make proto-lint
    make proto-gen
    go test ./x/asset/...
    go test ./x/market/...
    go test ./x/treasury/...
    go test ./x/oracle/...
    go test ./abci/...
    go test ./oracle/...
    go test ./app/...
    go test ./...
    go build ./...
    git diff --check

Use GOCACHE=/private/tmp/ark-gocache where required. Treat listener sandbox failures and Docker-dependent protobuf generation as environment failures until rerun with the required capability.

## Suggested commit sequence

1. Add the temporary Oracle target-removal guard and tests. Completed.
2. Add x/asset protobuf contracts, generated code, and validation tests.
3. Add the standalone x/asset keeper, lifecycle, locks, target scheduler, valuation, and tests.
4. Cut target ownership, ABCI, sidecar, metadata, and pricing consumers over to x/asset.
5. Move Market asset-policy ownership and lifecycle enforcement.
6. Move Treasury stable enrollment and liability enumeration.
7. Remove legacy Oracle coupling and add full integration tests and documentation.
8. Add the live-chain upgrade and migrations only if required.

Each commit should keep generated code with its source protobuf change and preserve unrelated worktree changes.

## Completion criteria

The design is complete when:

- every priced asset has one denomination-keyed rate normalized to atomic units per unoah;
- provider pairs remain an off-chain concern and cannot change consensus rate semantics;
- no module infers asset existence or liability status from Oracle Params;
- governance cannot stop valuation or redemption for outstanding supply;
- every issuance path checks lifecycle state immediately before settlement;
- final retirement is impossible with non-zero supply or an AssetLock;
- priced-asset removal remains height-correct and deterministic;
- unpriced assets cannot enter Market or Treasury monetary policy;
- tokenized commodities can be priced without becoming stable liabilities;
- asset registration, economic policy, and Oracle operation have one clear owner each;
- the full retirement and normalization integration scenario passes.
