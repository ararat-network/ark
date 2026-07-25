# Asset Module Plan

Status: Phases 0, 1, and 1B implemented; remaining architecture proposed

## Purpose

Build x/asset as the authoritative registry and lifecycle owner for governance-managed Bank assets other than NOAH. The module must support stablecoins, tokenized commodities such as gold, and unpriced assets without treating every asset as a stablecoin.

The immediate goal is safe denomination retirement. Normal retirement must not make an asset disappear from Oracle
pricing, Treasury liabilities, redemption, or Market policy while issued supply or downstream dependencies remain.
Emergency delisting is a separate, explicit governance path that freezes unsafe economic operations without silently
removing outstanding supply or treating an unavailable valuation as zero.

## Core decisions

- Asset is the general domain name. Stablecoin remains a Treasury policy classification, not the name of the registry.
- anoah remains the chain's native base denomination and is outside the governable asset lifecycle.
- Every Ark-native asset defines at least an `a<display>` base denom at exponent 0 and its `<display>` denom at
  exponent 18. Sorted intermediate denomination units are permitted; aliases are not supported.
- Every priced asset has exactly one authoritative Oracle rate keyed by its Bank denomination.
- Every asset rate is denominated directly against NOAH.
- x/asset owns asset identity, Bank metadata, lifecycle, Oracle-target epochs, AssetLocks, and lifecycle-aware valuation access.
- x/oracle owns validator voting, aggregation, accounting, denomination-keyed rates, and rate freshness.
- x/market owns conversion eligibility and Tobin-tax policy.
- x/treasury owns stable-liability classification and its reference-asset policy.
- Retirement stops new issuance first, preserves valuation and redemption, and removes pricing only after supply and dependency checks pass.
- Emergency delisting is reversible. It stops issuance, conversion, and ordinary protocol redemption while preserving
  Bank supply, metadata, denom history, and explicit liability disclosure.
- Governance may establish a denomination-keyed settlement plan that promises a one-way asset-to-NOAH redemption
  rate. Settlement terms are not Oracle observations and never enable issuance.
- The shared redemption buffer is drawn proportionally to aggregate recognized liability. A settlement determines the
  holder's total NOAH entitlement; buffer coverage determines only how much is transferred rather than minted.
- Governance may write off a DELISTED or SETTLING asset, and may later reinstate a WRITTEN_OFF asset for settlement or
  relisting. Every write-off remains permanently auditable even after reinstatement.
- Generalized feed IDs are deferred until there is a concrete need for non-asset observations, multiple prices per asset, or independently shared feed lifecycle.

## Scope boundaries

Version 1 does not provide:

- generalized on-chain price-feed identifiers separate from asset denoms;
- arbitrary non-asset Oracle observations;
- an on-chain provider or exchange-symbol registry;
- issuer, custody, proof-of-reserve, or physical redemption machinery for tokenized assets;
- a general-purpose decentralized exchange;
- lifecycle control over anoah;
- native assets with a display exponent other than 18;
- lifecycle control or repricing of IBC assets, which retain their source-chain denomination and precision;
- automatic write-off based only on elapsed time;
- live-chain compatibility unless deployment requires an upgrade from committed state.

Provider markets and resolver routes remain generalized off-chain. The sidecar may combine pairs such as NOAH/USD, USD/XAU, and a constant unit conversion, but consensus receives one final denomination-specific NOAH rate.

## Price contract

The consensus rate contract is:

    rate[denom] = base-denom units of denom per one anoah
    rate[anoah] = 1

Conversion remains:

    ask amount = offer amount * ask rate / offer rate

All Ark-native assets, including NOAH, use exponent 18. Therefore a provider price expressed as display asset units per
display NOAH has the same numeric value as the consensus base-unit rate. No exponent metadata or unit-normalization
layer is required in the target transport.

For example, the sidecar may derive tokenized gold from external XAU markets, but validators report the final result as:

    agold units per anoah

The sidecar is responsible for resolving the final economic pair before encoding a vote. Resolver configuration supplies
any transformation such as XAU to grams, while the fixed 18-decimal native-asset convention preserves the resulting
numeric rate at the consensus boundary.

Only Oracle-priced assets may enter ordinary Market conversion or active Treasury stablecoin policy. An unpriced asset
may be registered and transferred, but consensus monetary policy must not attempt to value, mint, burn, or redeem it.
The only exception is an explicit governance settlement rate:

    redemption_rate[denom] = NOAH paid per unit of the settled asset

Settlement quotes multiply the offered base-unit amount by the positive 18-decimal governance rate:

    output anoah = floor(input asset amount * redemption_rate)

Rounding down prevents payout amplification by splitting one redemption into many transactions. A zero output is
rejected. Because every Ark-native asset and NOAH use exponent 18, the rate has the same numeric value in display and
base units. The settlement redemption rate is a contractual entitlement, not an Oracle observation, and must not be
inserted into Oracle state or exposed to ordinary Market conversion.

## Ownership model

### x/asset

Owns:

- asset registration and Bank metadata;
- asset lifecycle status and versioning;
- whether an asset requires Oracle pricing;
- active and pending Oracle-target epochs;
- AssetLocks that prevent unsafe retirement;
- lifecycle-aware valuation snapshots for other modules;
- active governance settlement terms and append-only write-off history;
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
- one-way execution of x/asset settlement terms.

Market obtains lifecycle-checked valuation snapshots from x/asset rather than using Oracle configuration as an asset registry.

### x/treasury

Owns:

- the set of assets counted as stable liabilities;
- tax-cap and reference-asset policy;
- redemption accounting and solvency rules;
- separation of priced, settlement-priced, unpriced, and written-off exposure;
- AssetLocks protecting stable enrollment and the reference asset.

Treasury obtains lifecycle-checked valuation snapshots from x/asset and never infers liabilities from Oracle targets.

## State model

The initial x/asset collections should be conceptually equivalent to:

    Assets          collections.Map[string, types.Asset]
    AssetLocks      collections.KeySet[collections.Pair[string, types.AssetLockKind]]
    OracleTargets   collections.Item[types.OracleTargets]
    SettlementPlans collections.Map[string, types.SettlementPlan]
    WriteOffRecords collections.Map[collections.Pair[string, uint64], types.WriteOffRecord]

An asset contains at least:

    message Asset {
      string denom = 1;
      cosmos.bank.v1beta1.Metadata metadata = 2;
      AssetStatus status = 3;
      uint64 version = 4;
      bool oracle_required = 5;
    }

The denomination is the immutable asset identifier. version increments on every lifecycle or pricing-mode mutation. Governance messages that mutate an existing asset include expected_version so stale proposals cannot silently act on different state.

oracle_required means that the asset requires a direct NOAH-relative Oracle rate during normal listed operation. Such
an asset participates in target epochs while its lifecycle requires live or pending Oracle coverage. A DELISTED,
SETTLING, or WRITTEN_OFF asset retains oracle_required while deliberately absent from the active target set. The field
describes the asset's normal pricing mode, not current target membership, Market eligibility, or Treasury
classification.

The active and pending target sets contain sorted, unique asset denoms. They retain an explicit target version and activation vote height.

A SettlementPlan contains a positive 18-decimal NOAH-per-asset redemption rate, an activation height, an optional
earliest relisting closing height, and the asset version that last changed it. The rate becomes immutable once
redemption activates. Reaching earliest_closing_height does not close settlement automatically; it only permits
relisting completion once fresh pricing and policy prerequisites are satisfied. Before activation, governance may
cancel the plan through an expected-version update and return the asset to DELISTED. The plan remains separate from
Oracle rates and is consumed only by settlement redemption and Treasury liability accounting.

WriteOffRecords are append-only and keyed by denom and asset version so reinstatement cannot erase prior resolution
history. A record retains at least the remaining Bank supply, write-off height, and any active settlement plan. A direct
DELISTED write-off has no NOAH-equivalent value when no trustworthy price or settlement exists; it records the native
quantity rather than inventing a value.

## Asset lifecycle

Priced assets use:

    PENDING -> ACTIVE -> RETIRING -> REMOVAL_PENDING -> RETIRED
       |          ^          |
       |          |----------| cancel retirement
       +---------------------------------------------> RETIRED (cancel registration)

Unpriced assets skip target scheduling and move directly from PENDING to ACTIVE and from RETIRING to RETIRED.

Oracle-priced assets also have an emergency resolution path:

    ACTIVE or RETIRING -> DELISTING -> DELISTED
                                       |   |
                                       |   +-----------> WRITTEN_OFF
                                       |                    |       |
                                       +-> SETTLING --------+       +-> RELISTING -> ACTIVE
                                             |      |
                                             |      +-------------> RELISTING -> ACTIVE
                                             +--------------------> RETIRED (zero supply)

    WRITTEN_OFF -> SETTLING

Unpriced assets use only the normal lifecycle and cannot enter DELISTING, DELISTED, SETTLING, RELISTING, or WRITTEN_OFF.

Normal retirement and emergency resolution are distinct. RETIRING preserves live Oracle pricing and ordinary
redemption while supply is wound down. DELISTING freezes value-dependent operations because live pricing is no longer
trusted. Neither delisting nor write-off changes Bank balances or burns holder supply.

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

### DELISTING

- New issuance, ordinary Market conversion, and ordinary protocol redemption stop immediately.
- Existing Bank supply and ordinary transfers remain unchanged.
- Treasury retains the outstanding native-unit exposure but does not treat a missing valuation as zero.
- MARKET_BASE_POOL and TREASURY_REFERENCE_TAX_CAP must move before target removal can be scheduled.
- Dormant Market-asset and Treasury-stable policies may remain attached for disclosure and possible recovery.
- Oracle target removal follows the delayed epoch and consumes the old epoch before pruning its stored rate.

### DELISTED

- The asset is absent from active and pending Oracle targets and has no stored Oracle rate.
- Issuance, ordinary Market conversion, and protocol redemption remain disabled.
- Bank supply, metadata, policy enrollment, AssetLocks, and denomination history remain.
- Treasury reports stable supply as unpriced exposure rather than zero-valued or aggregate-priced liability.
- Governance may begin relisting, open a settlement, or write off the asset.

### SETTLING

- Governance has established a binding one-way asset-to-NOAH redemption rate.
- Issuance and ordinary Market conversion remain disabled.
- Settlement redemption burns the offered asset and pays the declared net NOAH entitlement.
- Treasury recognizes remaining supply at the settlement entitlement.
- The shared redemption buffer funds its proportional coverage share and Market mints the remainder.
- The plan may remain available indefinitely, allowing lost-key or inactive supply to remain a finite, priced liability.
- Supply reaching zero permits final retirement after remaining policies, plans, and locks are cleared.
- Governance may begin relisting or write off the remaining supply.

### RELISTING

- RELISTING is the persisted Oracle-target restoration state and therefore applies only to oracle_required assets.
- Issuance remains disabled while Oracle target addition, a fresh rate, and policy reactivation complete.
- A settlement inherited from SETTLING remains available through its earliest closing height and until relisting
  completion explicitly closes it.
- A WRITTEN_OFF asset re-enters Treasury disclosure as unpriced exposure immediately on reinstatement.
- The asset becomes ACTIVE only after its target is active, a fresh rate exists, and any settlement has closed.
- Fixed settlement redemption and unrestricted issuance must never be enabled simultaneously.

### WRITTEN_OFF

- Outstanding Bank supply may remain non-zero, but governance currently recognizes no protocol redemption obligation.
- Issuance, Market conversion, Oracle pricing, and protocol redemption are disabled.
- Treasury excludes the residual amount from recognized liabilities but reports it separately as written-off supply.
- Ordinary Bank transfers remain available unless a separate, explicit Bank-level freeze is introduced.
- Prior write-off records remain append-only and queryable.
- Governance may reinstate the same asset and existing balances for settlement or relisting. It must not reuse the denom
  for a different asset or erase the historical write-off.

## Asset locks

An AssetLock records that another module's configuration still depends on an asset. It is an inverse dependency index, not a balance, rate, ownership record, or governance-supplied label.

The initial lock kinds are:

- MARKET_ASSET_POLICY
- MARKET_BASE_POOL
- TREASURY_STABLE_POLICY
- TREASURY_REFERENCE_TAX_CAP

AssetLocks are consensus state maintained through explicit x/asset keeper operations called by the owning modules. Final retirement fails while any lock exists.

Lock changes and the policy changes they protect must occur in the same transaction. For example, changing Market's base-pool denom removes MARKET_BASE_POOL from the old asset and adds it to the new asset atomically.

Emergency target removal does not require dormant MARKET_ASSET_POLICY or TREASURY_STABLE_POLICY locks to disappear:
their owning modules must make those policies lifecycle-aware and stop consuming Oracle valuation while the asset is
DELISTING, DELISTED, SETTLING, RELISTING without a fresh rate, or WRITTEN_OFF. MARKET_BASE_POOL and
TREASURY_REFERENCE_TAX_CAP are different because they are live reference dependencies; they must move before
delisting can schedule target removal. Final RETIRED status still requires every policy, plan, and AssetLock to be
removed.

## Lifecycle operations

### RegisterAsset

- Validate the denomination and Bank metadata.
- Reject anoah and any previously registered denom, including a retired tombstone.
- Require the native metadata shape: `a<display>` at exponent 0 and `<display>` at exponent 18, with no aliases.
  Permit additional denomination units when normal Bank metadata validation accepts their denomination and ordering.
- Store the asset as PENDING with version 1 and the requested oracle_required value.
- Set Bank metadata through the Bank keeper.
- Do not grant mint authority or create initial supply.

### SetOracleRequired

- Require PENDING or RETIRED status and the expected version.
- Require Bank supply to be zero.
- Allow oracle_required to change only when no target addition or removal is scheduled for the asset.
- Never allow an ACTIVE, RETIRING, REMOVAL_PENDING, DELISTING, DELISTED, SETTLING, RELISTING, or WRITTEN_OFF asset
  to change normal pricing mode.

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

- Require RETIRING or SETTLING status and the expected version.
- Require Bank supply to be exactly zero.
- Require no AssetLocks.
- Close and remove any zero-supply SettlementPlan as part of finalization.
- For an unpriced asset, move directly to RETIRED.
- For a priced asset, schedule target removal and move to REMOVAL_PENDING.
- A SETTLING asset has already left the target set and therefore moves directly to RETIRED.
- Reject finalization while a different target transition is pending.

### BeginDelisting

- Require ACTIVE or RETIRING status, oracle_required, and the expected version.
- Immediately disable issuance, ordinary conversion, and ordinary redemption by moving to DELISTING.
- Require MARKET_BASE_POOL and TREASURY_REFERENCE_TAX_CAP to have moved to safe assets.
- Preserve supply, metadata, ordinary transfers, dormant policy locks, and liability disclosure.
- Schedule target removal through the normal delayed epoch.
- Reject delisting while a different target transition is pending.

### OpenSettlement

- Require DELISTED or WRITTEN_OFF status, positive outstanding supply, and the expected version.
- Require Market settlement eligibility; a WRITTEN_OFF asset must restore the necessary dormant Market policy before
  redemption can activate.
- Require a positive 18-decimal redemption rate denominated as NOAH paid per unit of the asset.
- Publish a future activation height and compute the maximum NOAH entitlement for current supply.
- Move immediately to SETTLING so Treasury recognizes the new obligation before redemption opens.
- Permit cancellation only before activation; replacement terms require cancellation and a new versioned plan.
- Never add the redemption rate to Oracle state or make it available to ordinary Market conversion.
- Never enable asset issuance while the plan exists.

### CancelSettlement

- Require SETTLING status, the expected version, and a current height before activation.
- Remove the SettlementPlan and move to DELISTED.
- Apply the same destination when settlement reinstated a WRITTEN_OFF asset; cancelling a newly recognized obligation
  must not silently restore the previous write-off.
- Opening replacement terms requires a new OpenSettlement transition and asset version.

### BeginRelisting

- Require DELISTED, SETTLING, or WRITTEN_OFF status and the expected version.
- Re-recognize WRITTEN_OFF supply as unpriced Treasury exposure immediately; stable-policy enrollment must be restored
  atomically when it was previously removed.
- Move to RELISTING and schedule target addition.
- Preserve an active SettlementPlan and its one-way redemption while target addition and fresh-rate collection proceed.
- If settlement is active, publish its earliest closing height before the asset can return to ACTIVE.
- Reject relisting while a different target transition is pending.

### Relisting completion

- Require RELISTING status, an active Oracle target, a fresh Oracle rate, and restored Market and Treasury policy.
- If a SettlementPlan exists, require its earliest closing height to have passed before removing it. Passing that height
  alone does not remove the plan or complete relisting.
- Move to ACTIVE only after settlement redemption has closed.
- Enable issuance only after the ACTIVE transition.
- Complete automatically in the application Oracle preblock pipeline once every condition is satisfied.

### WriteOffAsset

- Require DELISTED or SETTLING status and the expected version.
- Require an explicit governance resolution and protocol-defined notice process; never trigger from elapsed time alone.
- Close and remove any SettlementPlan.
- Append a WriteOffRecord containing the remaining Bank supply and, when present, the final settlement entitlement.
- Move to WRITTEN_OFF without burning, transferring, repricing, or otherwise modifying holder balances.
- Derecognize the residual Treasury liability while preserving separate written-off exposure reporting.
- A DELISTED write-off records no NOAH equivalent when no reliable valuation exists.

### ReinstateWrittenOffAsset

- Require WRITTEN_OFF status and the expected version.
- Reinstatement for live pricing uses BeginRelisting and applies to the same metadata, supply, and holders.
- Reinstatement for a fixed redemption uses OpenSettlement.
- Never erase, replace, or reinterpret prior WriteOffRecords.

### Target epoch activation

At the scheduled activation height:

- consume reports for the old epoch before promotion;
- promote the pending target set;
- move added PENDING assets to ACTIVE;
- keep added RELISTING assets issuance-disabled until a fresh rate exists and relisting completion succeeds;
- move removed REMOVAL_PENDING assets to RETIRED;
- move removed DELISTING assets to DELISTED;
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
        GetOracleTargets(ctx context.Context, voteHeight int64) (assettypes.OracleTargetSet, error)
        AdvanceOracleTargets(ctx context.Context) ([]string, error)
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

SettlementPlan access is a separate lifecycle-aware boundary. It must not be merged into Oracle RateSnapshot because
ordinary conversion and issuance must never consume a governance settlement rate. Market uses it only for explicit
one-way settlement redemption. Treasury may use both Oracle valuation and settlement entitlement when constructing its
partitioned liability report.

## Sidecar target mapping

The sidecar already resolves generalized provider pairs internally. The public price snapshot remains keyed by target denom.

For every target, sidecar configuration must resolve a final economic rate in asset units per NOAH. Native base denoms
map mechanically to display pairs, for example `ausd` to `NOAH/USD`. Resolver configuration remains responsible for
economic transformations such as XAU to grams.

The x/asset target query exposes only active and pending target-epoch state. It does not repeat asset metadata because
every Ark-native asset has the same exponent and target polling does not otherwise need metadata.

Validation must reject missing, non-positive, non-finite, or unrepresentable rates before vote encoding. Consensus
aggregation continues to operate only on the final denomination-keyed values.

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
- obtain one x/asset ValuationSnapshot for every denom used by an ordinary swap quote and settlement;
- settlement rechecks the ask asset immediately before minting;
- RETIRING assets may be consumed but never produced;
- normal policy removal requires RETIRING status and zero supply;
- emergency policy removal is permitted during zero-supply SETTLING finalization or after WRITTEN_OFF derecognition, while
  append-only resolution history remains;
- an asset carrying MARKET_BASE_POOL cannot remove its Market policy;
- parameter updates move MARKET_BASE_POOL atomically.

Emergency settlement rules:

- DELISTING, DELISTED, RELISTING without a live rate, and WRITTEN_OFF assets are excluded from ordinary swaps;
- SETTLING assets support only explicit asset-to-NOAH settlement redemption;
- the settlement redemption rate defines the holder's net NOAH entitlement without the ordinary constant-product
  spread;
- any haircut or premium is encoded in the governance-approved rate rather than hidden in Market spread;
- Market burns the redeemed asset, requests Treasury's proportional buffer draw, and mints only the unpaid NOAH
  remainder;
- per-block throughput limits may pace dilution but must not change the promised aggregate entitlement;
- settlement redemption remains available during RELISTING until its announced close;
- unrestricted issuance remains disabled until the settlement is closed and the asset is ACTIVE;
- a fixed settlement rate must never be exposed to asset-to-asset routing or NOAH-to-asset issuance.

## Treasury policy

Treasury should keep an explicit stable-liability set:

    StableAssets collections.KeySet[string]

Registration in x/asset does not make an asset a stablecoin. Tokenized gold, securities, and other assets remain outside stable-liability accounting unless governance explicitly enrolls them.

Treasury rules:

- enrollment requires an ACTIVE priced asset;
- enrolled supply remains a liability while the asset is RETIRING;
- ordinary Oracle liability valuation uses x/asset ValuationSnapshot;
- redemption remains supported throughout retirement;
- normal stable-policy removal requires RETIRING status and zero supply;
- DELISTED and SETTLING stable policy remains enrolled for unpriced or settlement-priced liability reporting;
- zero-supply SETTLING policy may be removed as part of final retirement;
- WRITTEN_OFF stable policy may be removed because its residual supply remains in separate write-off history;
- stable-policy re-enrollment is permitted during reinstatement before settlement or relisting activates;
- reference-tax-cap changes move TREASURY_REFERENCE_TAX_CAP atomically;
- the reference asset cannot retire until Treasury has moved the reference.

All liability, tax-cap, funding, redemption, ABCI, and query paths must enumerate Treasury's StableAssets rather than infer liabilities from Oracle targets or Market policy.

Treasury liability reporting must be partitioned rather than all-or-nothing:

    priced_liability_noah
    settlement_liability_noah
    unpriced_stable_assets[{denom, outstanding_supply}]
    written_off_assets[{denom, outstanding_supply, write_off_version}]
    total_liability_available

- priced_liability_noah is complete for stable assets with fresh Oracle valuation;
- settlement_liability_noah is complete for supply covered by active governance settlement terms;
- DELISTING, DELISTED, and RELISTING supply without settlement or fresh Oracle valuation remains explicit unpriced
  exposure and is never treated as zero;
- WRITTEN_OFF supply is excluded from recognized liabilities but remains separately disclosed;
- total liability is available only when every recognized liability has an Oracle or settlement valuation;
- calculations involving only a complete priced subset may continue, but code must not label that subtotal as total
  liability;
- policy that truly requires total recognized liability must use conservative behavior while valuation is incomplete.

The shared redemption buffer keeps its existing proportional draw:

    coverage = min(buffer_noah / aggregate_recognized_liability_noah, 1)
    buffer_paid = redemption_entitlement_noah * coverage
    minted_noah = redemption_entitlement_noah - buffer_paid

This preserves the same buffer coverage ratio for remaining holders and prevents early redeemers from exhausting the
shared reserve. If aggregate recognized liability is incomplete, Treasury cannot calculate a safe coverage ratio and
must preserve the buffer; Market mints the full settlement entitlement instead. If all liabilities eventually redeem,
proportional and buffer-first draws consume the same total buffer, but proportional draws avoid a first-exit advantage
and a later minting cliff.

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
- SetOracleRequired
- ActivateAsset
- CancelAssetRegistration
- BeginRetirement
- CancelRetirement
- FinalizeRetirement
- ReactivateAsset

Emergency-lifecycle additions to the contract:

- BeginDelisting
- OpenSettlement
- CancelSettlement
- BeginRelisting
- WriteOffAsset

SettlementPlan stores denom, redemption_rate, activation_height, earliest_closing_height, and version. WriteOffRecord
stores denom, write-off version and height, outstanding supply, and an optional settlement-plan snapshot. Direct
DELISTED write-off therefore represents an unknown NOAH value with an absent plan rather than a zero-valued field.
This prelaunch revision compacts the newly introduced settlement fields; established Asset fields and lifecycle enum
values remain unchanged.

Initial queries:

- Asset
- Assets with pagination
- AssetLocks
- OracleTargets

The emergency extension also needs lifecycle-aware SettlementPlan and WriteOffHistory queries. Asset and Assets continue
to expose the current lifecycle status; historical write-off records must not disappear when the current status changes.

OracleTargets is the single public target-epoch query. It includes the active
and pending target versions and activation vote height. Asset metadata remains
available from the Asset query. A second raw target-state query would duplicate
the same protocol state.

The public Valuation query is deferred until the standalone keeper and its
Oracle dependency exist in Phase 2. ValuationSnapshot remains the internal
lifecycle-aware boundary; its public representation should follow that
implementation rather than freeze a speculative API.

Emit state-oriented events for registration, status changes, Oracle-participation changes, target scheduling and
activation, AssetLock changes, settlement creation and activation, relisting completion, write-off, and reinstatement.

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
        settlement.go
        asset_locks.go
        oracle_targets.go
        valuation.go
      types/
        asset.go
        codec.go
        constants.go
        defaults.go
        errors.go
        expected_keepers.go
        genesis.go
        keys.go
        oracle_targets.go
        settlement.go
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

- seed the existing eight stable assets as ACTIVE and oracle_required;
- seed the current active and pending target epoch, version, and activation height in x/asset;
- keep stored denomination-keyed rates in x/oracle;
- move Bank metadata ownership from Oracle genesis to Asset genesis;
- seed Market policies from the current Tobin-tax configuration;
- seed Treasury StableAssets from the current stable set;
- rebuild AssetLocks deterministically from Market and Treasury genesis state;
- do not accept an independently supplied AssetLock list in Asset genesis.

For a clean genesis, no SettlementPlan or WriteOffRecord is required unless the launch state intentionally imports an
existing resolution. If those records are admitted, genesis must validate their exact lifecycle, supply, denom, version,
activation-height, and redemption-rate invariants rather than deriving them from Market or Treasury.

Asset genesis validation must ensure that active priced assets and target state agree exactly, pending lifecycle states
match the scheduled transition, Oracle-priced DELISTING and RELISTING states agree with the pending target epoch,
DELISTED, SETTLING, and WRITTEN_OFF assets are absent from target sets, metadata is valid, and no unpriced asset appears
in a target set.

## Application wiring

Add AssetKeeper to dependency injection and ArkApp. The intended genesis order is:

    bank -> asset -> oracle -> market -> treasury

x/asset does not need its own BeginBlocker or EndBlocker. The application Oracle preblock pipeline:

1. selects the target epoch for the committed vote height;
2. aggregates and stores rates in x/oracle;
3. records Oracle accounting;
4. advances x/asset target state after old-epoch consumption;
5. completes eligible RELISTING transitions only after a fresh rate exists and any earliest settlement-closing height
   has passed;
6. asks x/oracle to prune the returned denoms.

Expected integration touchpoints include:

- app_config.go
- app/app.go
- app/oracle.go
- app tests and keeper wiring tests
- ABCI Oracle and Asset interfaces and generated mocks
- vote extension, proposal, aggregation, and preblock consumers
- sidecar target polling and pair mapping
- transport, validation, and CLI target queries
- Market and Treasury expected keepers and generated mocks

## Implementation phases

### Phase 0: Contain the confirmed defect

Implemented: Oracle UpdateParams rejects any update that removes a configured target, including emptying a non-empty set. Additions and non-target parameter changes remain valid.

This guard stays until the old parameter-driven target-removal path no longer exists.

### Phase 1: Define the x/asset protocol contract

Implemented:

- Finalize Asset, AssetStatus, AssetLockKind, vote-target state, genesis, governance messages, queries, and events.
- Define the base-units-per-anoah rate contract and standardized 18-decimal native-asset convention.
- Define lifecycle and expected_version semantics.
- Explain the exact protobuf changes and receive approval before editing them.
- Generate gogo and Pulsar outputs and add types-level validation tests.
- Expose OracleTargets as the sole public target query and defer the public
  Valuation query until Phase 2.

### Phase 1B: Extend the contract for emergency resolution

Implemented:

- Add DELISTING, DELISTED, SETTLING, RELISTING, and WRITTEN_OFF lifecycle states without renumbering the implemented
  enum values.
- Define SettlementPlan, append-only WriteOffRecord, lifecycle messages, queries, and events.
- Define floor-rounded rate quoting, activation, immutability, earliest-closing notice, and expected_version rules.
- Extend types and genesis validation for emergency target removal, relisting, settlement, write-off, and reinstatement.
- Generate gogo and Pulsar outputs only after the exact protobuf packet is approved.

### Phase 2: Build x/asset standalone

- Implement Assets, AssetLocks, SettlementPlans, WriteOffRecords, target epochs, lifecycle operations, Bank metadata ownership, genesis, messages, queries, and events.
- Implement ValuationSnapshot over Oracle RateSnapshot.
- Add keeper suites for priced, unpriced, pending, active, retiring, removal-pending, retired, delisting, delisted,
  settling, relisting, and written-off states.
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
- Add one-way SettlementPlan redemption without ordinary Market spread or reverse issuance.
- Retain proportional Treasury buffer draw and mint only the uncovered NOAH entitlement.
- Add and maintain Market AssetLocks.
- Migrate Market genesis and queries.

### Phase 5: Move Treasury classification ownership

- Add StableAssets and governance operations to x/treasury.
- Replace every Oracle-target-derived liability enumeration.
- Preserve liability and redemption support throughout RETIRING.
- Partition Oracle-priced, settlement-priced, unpriced, and written-off exposure.
- Preserve the shared buffer when aggregate recognized liability is incomplete.
- Re-recognize written-off supply when governance begins settlement or relisting.
- Add and maintain Treasury AssetLocks.
- Migrate Treasury genesis and queries.

### Phase 6: Remove legacy coupling and complete integration

- Remove TobinTaxes, TobinTax, vote-target state, target scheduling, metadata ownership, and obsolete queries from x/oracle.
- Reserve removed protobuf field numbers.
- Remove the temporary Oracle target-removal guard after the old path is gone.
- Add full retirement, emergency resolution, reinstatement, and rate-contract integration tests.

## Required integration scenario

At minimum, an end-to-end test must prove:

1. Register a priced 18-decimal native asset such as `agold`.
2. Activate it through the delayed Oracle target epoch.
3. Resolve and report its rate as base-denom units per anoah.
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
- attempts to register or retire anoah;
- registration with a display exponent other than 18, aliases, or fewer than the required base and display units;
- zero and non-zero Bank supply boundaries;
- each AssetLock kind as an independent blocker;
- overflow, underflow, zero-rounding, and stale-rate failures.

Add a separate emergency-resolution scenario that proves:

1. Delisting immediately rejects issuance, ordinary conversion, and ordinary redemption.
2. Live Market base-pool and Treasury reference-tax-cap dependencies must move before target removal.
3. The old Oracle epoch is consumed before DELISTING becomes DELISTED and the rate is pruned.
4. Outstanding Bank supply remains transferable and appears as unpriced Treasury exposure.
5. Governance can open a rate-based one-way settlement and Treasury recognizes its maximum NOAH entitlement.
6. Redemption burns the asset, draws the buffer proportionally, and mints only the uncovered NOAH.
7. Incomplete aggregate liability preserves the shared buffer and still permits the promised settlement payout.
8. SETTLING can reach RETIRED at zero supply.
9. DELISTED and SETTLING can enter WRITTEN_OFF without modifying holder balances.
10. Direct DELISTED write-off records native-unit supply without an invented NOAH value.
11. WRITTEN_OFF supply remains disclosed and can be reinstated for SETTLING or RELISTING.
12. RELISTING keeps issuance disabled and any existing settlement open until a fresh Oracle rate exists, its announced
    earliest closing height has passed, and relisting completion explicitly closes it.
13. Fixed settlement redemption is closed before ACTIVE issuance resumes.
14. Historical WriteOffRecords remain unchanged after reinstatement.

## Live-chain upgrade variant

If this work ships after a live genesis, use a named software upgrade:

- add the x/asset store before loading the new application version;
- introduce Asset module version 1;
- bump Oracle, Market, and Treasury consensus versions;
- retain old protobuf decoding until migrations complete;
- seed assets, metadata, exact target version and activation height, Market policies, Treasury stable enrollment, and AssetLocks from committed state;
- seed any active settlements and historical write-offs exactly rather than reconstructing their economic terms;
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
3. Extend the x/asset contract for emergency resolution, settlement, write-off, and reinstatement.
4. Add the standalone x/asset keeper, lifecycle, locks, target scheduler, valuation, and tests.
5. Cut target ownership, ABCI, sidecar, metadata, and pricing consumers over to x/asset.
6. Move Market asset-policy ownership, lifecycle enforcement, and settlement execution.
7. Move Treasury stable enrollment and partitioned liability enumeration.
8. Remove legacy Oracle coupling and add full integration tests and documentation.
9. Add the live-chain upgrade and migrations only if required.

Each commit should keep generated code with its source protobuf change and preserve unrelated worktree changes.

## Completion criteria

The design is complete when:

- every priced asset has one denomination-keyed rate expressed as base-denom units per anoah;
- provider pairs remain an off-chain concern and cannot change consensus rate semantics;
- no module infers asset existence or liability status from Oracle Params;
- normal retirement cannot stop valuation or redemption for outstanding supply;
- emergency delisting never treats missing valuation as zero or silently removes outstanding supply;
- governance settlement is one-way, binding, auditable, and never enables issuance;
- the shared redemption buffer remains proportional and is preserved when aggregate liability is incomplete;
- write-off and reinstatement preserve Bank balances and append-only resolution history;
- fixed settlement redemption closes before ACTIVE issuance resumes;
- every issuance path checks lifecycle state immediately before settlement;
- final retirement is impossible with non-zero supply or an AssetLock;
- priced-asset removal remains height-correct and deterministic;
- unpriced assets cannot enter Market or Treasury monetary policy;
- tokenized commodities can be priced without becoming stable liabilities;
- asset registration, economic policy, and Oracle operation have one clear owner each;
- the full retirement and rate-contract integration scenario passes.
