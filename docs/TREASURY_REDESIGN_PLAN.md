# Ark Treasury and Monetary Policy Redesign Plan

- Status: **Phases 1-3 reviewed; Phase 4 IBC foundation implemented, review pending**
- Last updated: 2026-07-21
- Target SDK: Cosmos SDK v0.54.3
- Launch state: **confirmed prelaunch / fresh genesis**

This document is the implementation contract for rebuilding Ark's Treasury and the monetary flows around it. It is
intentionally more detailed than a normal design note: each implementation phase must be completed, tested, reviewed,
and explicitly approved before work begins on the next phase.

The reviewed `x/treasury` implementation is authoritative and must be carried forward. Its Treasury protobuf and
generated API changes, required Phase 1 app account wiring, Oracle `anoah` identity quote support, and Phase 3 Market
settlement and pool-unit transition implementation are also retained as recorded in Section 21.1. Existing Phase 4 ante
and multi-denomination Oracle reward changes remain in the tree but are provisional and unreviewed until the Phase 4 gate. Implementers must
not replace whole files with an older baseline or treat the current branch state as disposable.

## 1. Intended outcome

Ark will have no scheduled or routine NOAH issuance:

- Remove the stock `x/mint` module and its staking-reward inflation.
- Remove Treasury's `Minter` permission and all Treasury seigniorage minting.
- Keep Market as the only module with `Minter`; retain Market's `Burner` permission for conversion settlement.
- Permit Market to mint only as part of atomic stablecoin conversion settlement.
- Keep each gross NOAH expansion offer in Market's transaction-local escrow. Treasury derives and executes the complete
  per-conversion fund-allocation waterfall from that escrow; a successful result is authoritative, and Market owns the
  resulting burn, stable mint, and receiver payment.
- Make Market's single virtual pool denomination-bearing: launch it in `asdr`, then permit one atomic live unit change
  to the future basket without changing which native stablecoins may be selected as outputs; `asdr` remains supported.
- Fund validator and Oracle launch subsidies from an initially genesis-funded NOAH subsidy pool. Permissionless
  transfers of already-issued NOAH may extend it, but no automatic refill, conversion, or issuance path exists.
- Fund Oracle rewards primarily from the fixed stability tax.
- Keep Treasury's root authority with governance, while allowing one governance-appointed threshold-multisig committee
  to update only the bounded, reversible monetary-policy subset during a fixed height term. Governance may override a
  committee policy directly or replace/disable the committee at any time.
- Build a coverage-based Redemption Buffer, a separate strategic Reserve, and Insurance from existing genesis NOAH and
  Treasury-routed expansion principal initially escrowed by Market.
- Permit governance to make a discrete, irreversible `anoah` commitment from the strategic Reserve into the shared
  Redemption Buffer. Keep source, destination, and denomination fixed in Treasury; expose no general Reserve withdrawal
  or Market-triggered Reserve path.
- Permit irreversible `anoah` deposits to all four Treasury fund accounts at launch. Reject every mixed or non-NOAH
  transfer atomically, regardless of whether it originates from a user, module, Wasm, or IBC transfer surface. A deposit
  grants no ownership, withdrawal, coverage, priority, governance, deployment, or special Buffer right.
- Put ordinary Insurance claims under a governance-owned, monotonically termed, height-scoped Claims Mandate. The exact
  active committee submits claims under the mandate's term, window, and held-balance rules; governance submits under
  the same validation and held-balance rules without depending on the mandate. Both origins share the cancellation
  period stored in Treasury `Params`; committee submissions additionally consume a fixed gross `anoah` allowance for
  that term. Governance may cancel any
  pending claim during that period regardless of the current mandate; the current active committee may cancel only
  non-governance-submitted claims using the current term. Neither receives Insurance custody or a generic Bank send.
- Remove Terra's adaptive tax, reward, mining-increment, seigniorage-burden, and rolling-indicator controllers.

This is **zero routine issuance**, not zero possible change in NOAH supply. Stable-to-NOAH conversion continuously
combines the Redemption Buffer's actual coverage share of the quoted output with residual Market minting. NOAH-to-stable
conversion can still burn NOAH after the Redemption Buffer, strategic Reserve, and Insurance allocations are made.

### 1.1 Explicit feedback-loop limitation

The proposed model retains a bounded endogenous conversion response:

- Expansion can contract NOAH supply when post-allocation expansion principal is burned.
- Every redemption can expand NOAH supply by the portion not funded by the Buffer's actual pre-trade liability coverage.
- The coverage-based draw recycles NOAH retained during prior expansions without creating a first-come depletion cliff.
- The strategic Reserve is never an automatic Market funding source. Governance may commit a discrete amount of its
  existing `anoah` to the shared Redemption Buffer between settlements, but Market cannot request, size, or trigger the
  transfer and no redeemer receives a direct Reserve payment.
- Virtual-pool pricing penalises sustained one-way flow and remembers imbalance.
- Time-based pool recovery limits how quickly cheap conversion capacity returns.
- No adaptive reward target/rate controller, tax-rate controller, price-reactive Buffer share, or automatic Reserve
  trigger amplifies these flows. The fixed-target reward-funding waterfall only allocates already-collected tax and
  already-issued subsidy NOAH within hard balance bounds.

A governed Reserve-to-Buffer commitment is a manual reclassification of already-issued NOAH, not a controller:
`Reserve -= amount`, `Buffer += amount`, and total supply, liabilities, quotes, spreads, and pool state do not change.
The lower Reserve balance may reopen its passive target gap, so later realised expansion principal can rebuild it under
the existing waterfall. That refill cannot cause expansion or mint NOAH, but repeated governance commitments can delay
Insurance funding and overflow burn; each proposal must make that consequence explicit.

The Redemption Buffer and virtual pools therefore **buffer, dampen, and rate-limit** Terra-style supply feedback. They
do not remove it. Buffer-funded redemption releases previously dormant NOAH into circulation, while residual minting
increases both circulating and total NOAH supply. The Buffer reduces dilution; it does not remove immediate sellable
NOAH from the redemption flow and must not be described as independent collateral or backing.

Literal elimination of NOAH supply feedback would instead require retaining enough expansion NOAH to fund every
redemption and rejecting or deferring any uncovered output rather than minting it. That creates a redemption gate and a
strong first-mover incentive. Ark therefore keeps unconditional conversion settlement with a continuous proportional
minting response, subject to the separately reviewed virtual-pool capacity envelope.

## 2. Decision register

The following decisions are the current recommended policy. Items marked **pending** must be resolved at the stated
phase gate.

| ID  | Decision                                                                                                                                                                                                                                                                                                                                                                                                        | Status                 |
| --- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------- |
| D1  | Remove `x/mint`; Ark has no scheduled staking-reward inflation.                                                                                                                                                                                                                                                                                                                                                 | Confirmed              |
| D2  | Market remains the sole minter; its mint/burn authority is used only for conversion settlement.                                                                                                                                                                                                                                                                                                                 | Confirmed              |
| D3  | Keep `BasePool`, `ArkPoolDelta`, and `PoolRecoveryPeriod` as virtual imbalance controls.                                                                                                                                                                                                                                                                                                                        | Confirmed              |
| D4  | With complete valuation, fund the quoted NOAH output by the Buffer's actual pre-trade liability-coverage share capped at 100%; otherwise draw zero; mint the residual.                                                                                                                                                                                                                                          | Confirmed              |
| D5  | Route eligible expansion principal to Buffer, strategic Reserve, Insurance, then burn the excess.                                                                                                                                                                                                                                                                                                               | Confirmed              |
| D6  | Burn the NOAH value attributable to spread and integer dust; never route it to any fund.                                                                                                                                                                                                                                                                                                                        | Confirmed              |
| D7  | Targets are passive routing thresholds and never trigger minting, trading, or automatic withdrawals.                                                                                                                                                                                                                                                                                                            | Confirmed              |
| D8  | Initially value total native stablecoin supply as exposure and count only NOAH separately in each fund.                                                                                                                                                                                                                                                                                                         | Confirmed              |
| D9  | Use a dedicated, balance-constrained, non-minting subsidy pool initially seeded at genesis.                                                                                                                                                                                                                                                                                                                     | Confirmed              |
| D10 | Give validator and Oracle reward funding separate per-block NOAH-value targets, but aggregate the targets and eligible validator fees over one parameter-initialised Treasury settlement countdown before calculating shortfalls.                                                                                                                                                                               | Confirmed              |
| D11 | Remove `oracle_tax_share`; accumulate stability tax for the same Treasury window, protect the aggregate Oracle reward target first, use tax above that floor for any aggregate validator target gap, and return every residual to Oracle.                                                                                                                                                                       | Confirmed              |
| D12 | If the subsidy pool cannot cover both reward shortfalls, scale the shortfalls proportionally; rounding favours validators.                                                                                                                                                                                                                                                                                      | Confirmed              |
| D13 | Set Cosmos distribution `community_tax` to zero in Ark's canonical launch genesis.                                                                                                                                                                                                                                                                                                                                | Confirmed              |
| D14 | Keep protocol/community-pool residual accounting as an SDK concern; do not deliberately fund it from Treasury.                                                                                                                                                                                                                                                                                                  | Confirmed              |
| D15 | Insurance is a Treasury-owned module account paid only through unique, recorded claim IDs under the bounded, governance-owned Claims Mandate.                                                                                                                                                                                                                                                                   | Confirmed              |
| D16 | Ordinary redemption never debits strategic Reserve, and Reserve never directly pays redeemers, rewards, or claims; all arbitrary-recipient, conversion, investment, and external-asset deployment remains deferred.                                                                                                                                                                                             | Confirmed              |
| D17 | Apply each denomination's tax cap independently to each message input.                                                                                                                                                                                                                                                                                                                                          | Confirmed              |
| D18 | Tax every enabled user-facing stable transfer surface through one calculator, not internal bank movements.                                                                                                                                                                                                                                                                                                      | Confirmed              |
| D19 | Report Redemption Buffer, strategic Reserve, and Insurance separately; never present a combined backing ratio.                                                                                                                                                                                                                                                                                                  | Confirmed              |
| D20 | Launch from a clean genesis; discard legacy state/wire compatibility and implement no migration path.                                                                                                                                                                                                                                                                                                           | Confirmed              |
| D21 | Governance owns Treasury's complete denomination-bearing `reference_tax_cap` Coin in Params; launch it in `asdr` independently of Market's pool unit, and require no Treasury Params update when Market changes its pool denomination.                                                                                                                                                                           | Confirmed              |
| D22 | Make Market `BasePool` a denomination-bearing `sdk.DecCoin`; launch it in `asdr` and change its amount or denomination live only through Market's `MsgUpdateParams`. Oracle support is a prerequisite for a new denomination; Treasury never drives or intermediates the Market transition.                                                                                                                      | Confirmed              |
| D23 | Every applied `BasePool` amount or denomination change atomically rescales `ArkPoolDelta` to preserve `delta / BasePool.Amount`. On a denomination change, treat submitted `BasePool.Amount` as a non-binding audit expectation and apply the amount derived from one fresh deterministic Oracle conversion.                                                                                                   | Confirmed              |
| D24 | Value Treasury liabilities and targets directly in NOAH equivalents; stable-to-stable pricing does not use Market's pool denom.                                                                                                                                                                                                                                                                                 | Confirmed              |
| D25 | Permit irreversible, permissionless `anoah` transfers into the subsidy pool; reject other denoms and add no automatic refill mechanism.                                                                                                                                                                                                                                                                         | Confirmed              |
| D26 | At launch, permit only positive `anoah` deposits to all four Treasury fund accounts; reject every mixed or non-NOAH transfer atomically.                                                                                                                                                                                                                                                                        | Confirmed              |
| D27 | At launch, governance alone may irreversibly transfer a discrete `anoah` amount from strategic Reserve to the shared Redemption Buffer, subject to an execution-time minimum remaining Reserve balance; no target, price, Oracle, or Market trigger applies.                                                                                                                                                    | Confirmed              |
| D28 | Required Reserve and Insurance capital is based on each fund's covered risk exposure, never the gross value of assets held; future external assets may reduce a gap only through explicit fund-specific, risk-adjusted recognition plus a separate liquid-capital requirement, and Ark-issued stablecoins always receive zero credit.                                                                           | Confirmed              |
| D29 | Governance retains Reserve policy authority; any future fast execution uses a governance-created, typed, bounded, expiring mandate executed by a threshold multisig with a separate pause-only guardian, never a generic Reserve sender or Treasury parameter authority.                                                                                                                                        | Confirmed              |
| D30 | Governance owns the Insurance Claims Mandate. The shared cancellation period is a Treasury parameter applying to both origins; committee submissions are bound to the active mandate window and exact term and consume a fixed gross term allowance, while governance submissions depend only on params and record no mandate term; governance may cancel any pending claim regardless of the current mandate, while the current committee may cancel only non-governance-submitted claims. | Confirmed              |
| D31 | Monetary policy, Insurance claims, and future Reserve operations use distinct role addresses and typed authority domains even if human memberships overlap; no role receives Treasury's general authority.                                                                                                                                                                                                      | Confirmed              |
| D32 | Claims use submit-then-pay with encumbered pending amounts; cancellation ends for every actor at the executable height, and no authority can claw back a paid claim or bypass held-balance, denomination, uniqueness, no-mint, no-borrow, or no-cross-fund invariants.                                                                                                                                          | Confirmed              |
| D33 | Market escrows each gross expansion offer and owns conversion burn/mint/payout; Treasury derives and executes the complete per-conversion waterfall, returns an error if it cannot complete it, and otherwise returns the authoritative allocation Market uses to finish settlement.                                                                                                                                | Confirmed              |
| D34 | Govern `reward_funding_window`, default it to one chain week, initialise `blocks_remaining` from it when an empty Treasury window records its first observation, and apply later parameter changes only after the active countdown settles.                                                                                                                                                                     | Confirmed              |
| D35 | Keep `FundStatus` limited to fund stocks, liabilities, and targets; expose active reward-funding accounting through an independent direct-state query that remains available when fund valuation is unavailable.                                                                                                                                                                                                | Confirmed              |
| D36 | Governance may appoint one exact threshold-multisig monetary-policy committee under a bounded, height-scoped, chain-termed mandate. The committee controls only the six reversible policy fields; governance may override policy and replace or disable the mandate at any time.                                                                                                                               | Confirmed              |
| D37 | Governance owns the complete denomination-bearing `reference_tax_cap` Coin together with `reward_funding_window` and `claim_cancellation_period_blocks` in `Params`. Persist the six reversible economic levers once in `MonetaryPolicy`; Claims Mandate remains claims-only.                                                                                                                                                                          | Confirmed              |
| D38 | Keep launch Claims minimal: the mandate stores its monotonic term, committee, half-open activation/expiry window, and fixed gross committee claim limit; the shared cancellation period lives in Treasury `Params`; claims have no category or per-claim cap, and there is no guardian or governance-cancellation flag.                                                                                                                               | Confirmed              |
| D39 | Derive aggregate liability lazily on the first settlement that needs it each block, cache only a complete transient snapshot, and advance it from every Market burn/mint; an incomplete valuation is not cached and may be retried by a later settlement.                                                                                                                                                       | Confirmed              |
| D40 | Begin Phase 4 by fixing execution-time tax payer, exactly-once identity, rollback, and fee-sponsorship semantics; then implement IBC foundations before Wasm because contracts may dispatch IBC messages. Design the Treasury execution hook into both paths, but keep both transfer surfaces production-disabled until the complete tax and recipient-restriction activation gate passes.                                                                                  | Confirmed              |
| D41 | Use call-path ownership for exactly-once tax assessment: ante owns signed top-level inputs, while the Wasm dispatcher owns only execution-generated Bank sends, IBC sends, execute funds, and instantiate funds. Add no persistent transfer IDs, context markers, global Bank tax hook, or implicit execution-time feegrant. The sending contract pays the tax in addition to the complete requested principal.                                                              | Confirmed              |
| D42 | Send execution-generated tax directly to `stability_tax_collector` and execute its collection with the matching transfer in one Wasm submessage cache. Synchronous or caught failures roll both back; a successfully created IBC packet retains its tax through later acknowledgement, timeout, refund, or return bookkeeping, none of which is a new taxable transfer.                                                                                                          | Confirmed              |
| D43 | Expose a read-only contract-facing tax query through Ark's Wasm bindings. Parse the proposed execution-generated message through the same message adapter and invoke Treasury's canonical calculator; duplicate no rate/cap math. The result is an advisory current-state estimate only: it reserves no funds, grants no authority, and never replaces execution-time recomputation.                                                                                              | Confirmed              |
| D44 | Treat `MonetaryPolicy.stability_tax_rate` as the sole tax activation switch. An explicit zero reference or derived tax cap means uncapped taxation, while a missing configured-denomination cap remains an error. Keep the complete derived cap map populated independently of the rate, and never rebuild it from either policy-update message; reject any positive reference-cap conversion that truncates to the zero sentinel.                                       | Confirmed              |
| D45 | Build Ark's hub foundation against `github.com/cosmos/ibc-go/v11`, targeting v11.2.0 subject to dependency-resolution and compile verification. Wire IBC Classic and IBC v2 core/ICS-20 routes, the 07-Tendermint light client, and the transfer module account with minter/burner permissions. Keep standard module genesis defaults in application code; Ark's canonical launch genesis must allow only `07-tendermint` and launch transfer with send and receive disabled. | Confirmed              |
| D46 | Put governance-controlled rate limiting and packet forwarding in the IBC Classic transfer stack, and apply the v11 rate limiter to the IBC v2 transfer path. Configure reviewed per-denomination, per-channel/client limits before enabling production transfer. A packet-forwarded hop is protocol-generated continuation of the original transfer, not a new user-facing taxable input; acknowledgement, timeout, refund, and return bookkeeping likewise receive no second tax. | Confirmed              |
| D47 | Add IBC callbacks to both the Classic and v2 ICS-20 stacks when the Wasm keeper is wired. Callbacks are Ark's canonical transfer-and-call mechanism; add no separate IBC Hooks middleware. Callback-triggered execution uses the same Wasm/Treasury execution adapter, while acknowledgement, timeout, and callback delivery alone are not new taxable transfers.                                                                                                  | Confirmed              |
| D48 | Add IBC v2 GMP together with the Wasm foundation and route its derived-account SDK-message execution through the same Treasury-aware router used by contract-generated messages. The derived GMP account pays any execution-generated tax in addition to principal; outer fee payers and feegrant do not sponsor it. Do not expose a partially integrated GMP route before its authorization, tax, rollback, and recipient-restriction tests pass.                 | Confirmed              |
| D49 | Use upstream Wasmd `v0.70.x` and `wasmvm/v3`, subject to clean resolution and compile verification against Ark's SDK `v0.54.3` and IBC-Go `v11.2.0`; do not maintain a Wasmd fork or replacement-directive compatibility layer. Remove Ark's unused legacy `wasmvm` v1 parser/query interfaces when the real runtime is installed.                                                                                                                         | Confirmed              |
| D50 | Consider 08-Wasm only with the Wasm foundation and only if its exact dependency and VM family integrates cleanly. If installed, keep it dormant at launch: the allowed-client list remains exactly `07-tendermint` and the launch genesis contains no Wasm-client checksums. `09-localhost` remains unavailable through that launch allowlist; add neither 06-Solo Machine nor the experimental attestations client.                                                   | Confirmed              |
| D51 | Keep packet forwarding Classic-only until upstream provides reviewed v2 support; do not invent a v2 PFM adapter. Add no ICS-29 relayer-fee wiring because that application was removed from IBC-Go.                                                                                                                                                                                                                                                              | Confirmed              |
| D52 | Retain standard user ICA controller and host support but launch both disabled and the host with an empty message allowlist. Defer a generic custom ICA authentication module; if contracts later need ICA control, prefer a narrowly scoped Wasm-to-ICA adapter with explicit authorization.                                                                                                                                                                       | Confirmed              |
| D53 | Add neither ICS-721 NFT transfer nor a separate NFT module. Native Wasm IBC channels cover custom contract protocols, callbacks cover ICS-20 transfer-and-call, and GMP covers arbitrary remote SDK-message execution; revisit IBC Hooks only for a concrete requirement for Osmosis-compatible `wasm` memo or intermediary-address semantics.                                                                                                                       | Confirmed              |
| P1  | Choose launch tax rate, reference cap Coin, three target ratios, subsidies, and genesis fund balances.                                                                                                                                                                                                                                                                                                          | Pending before launch  |
| P2  | Phase 3A found no recipient-output, fixed-price cycle, or split residual-mint amplification under coverage-based Buffer funding; add no residual-mint limiter.                                                                                                                                                                                                                                                    | Confirmed              |
| P3  | Apply the deterministic live-derived pool amount without comparing the submitted expectation to a rejection threshold; retain the submitted expectation in the transaction and emit the old and applied pool state for audit.                                                                                                                                                                                    | Confirmed              |
| P4  | Choose the launch monetary-policy committee and bounds, Claims committee multisig and appointment window, the shared cancellation-period Treasury param, fixed gross committee claim limit, and operational fee funding.                                                                                                                                                                                                           | Pending before launch  |

Whenever a decision changes, update this table before changing code.

## 3. Terminology

### 3.1 Routine issuance

NOAH minted independently of a user conversion, such as staking inflation, block rewards, Oracle rewards, Treasury
seigniorage, fund-target top-ups, or deficit funding. Routine issuance is prohibited.

### 3.2 Conversion issuance

NOAH or stablecoins minted as the output of an atomic Market conversion while the offered asset is destroyed or retained
according to the settlement rules. Conversion issuance remains permitted only in Market.

### 3.3 Expansion principal

For a NOAH-to-stable conversion, the NOAH value of the **final integer stablecoin output** after spread, calculated by
Treasury from the same Oracle rate snapshot used by Market and rounded down. Market supplies the gross offer, final
output, and execution-local quote rates; it never supplies a precomputed eligible amount. Only Treasury-derived expansion
principal is eligible for the Redemption Buffer, strategic Reserve, and Insurance.

### 3.4 Spread and dust burn

The difference between gross NOAH offered and eligible expansion principal. This amount is always burned. When fund
targets are full, the unused portion of eligible principal is also burned.

### 3.5 Subsidy pool

The `treasury_subsidy_pool` module account's spendable NOAH balance. It is initially funded from genesis total supply,
has no mint permission, and covers only gaps between aggregate organic validator and Oracle funding and the accumulated
per-block targets at each completed reward-funding window. Anyone may irreversibly extend the pool by sending
already-issued `anoah` to the module account. Deposits change neither total supply nor the reward targets; they only
extend or restart shortfall coverage. There is no automatic refill, target balance, refund, withdrawal, or conversion
path.

### 3.6 Redemption Buffer

A Treasury-owned operational module account containing liquid NOAH retained from earlier expansions. With complete
aggregate valuation, each redemption receives the Buffer's actual pre-trade liability-coverage share of its quoted
NOAH output; otherwise it uses the conservative zero-draw fallback. It is endogenous conversion inventory, not collateral, solvency capital, or a
first-come redemption pool. Anyone may irreversibly deposit already-issued `anoah`. A deposit increases the inventory
available to the same coverage formula and may reduce later residual minting, but never changes a conversion quote,
spread, minimum receive, or redemption eligibility and creates no withdrawal or ownership claim.

### 3.7 Strategic Reserve

A separate Treasury-owned module account retained for governed emergency capacity and future external-asset mandates. It
is never debited automatically or by an ordinary redemption and never directly pays a redeemer, validator or Oracle
reward, or Insurance claim. At launch its only outbound policy action is a governance-authorised, one-way transfer of an
exact `anoah` amount into `treasury_redemption_buffer`; source, destination, and denomination are fixed internally,
Market cannot call it, and it never pays a redeemer directly. Once committed, that NOAH is Buffer inventory and has no
Buffer-to-Reserve clawback path.

Anyone may irreversibly deposit already-issued `anoah`, but every mixed or non-NOAH transfer is rejected at launch. The
Reserve's live `anoah` balance is both its complete launch custody and the balance counted toward its target. A deposit
grants no authority, withdrawal, or special claim on a later Reserve-to-Buffer commitment. External custody and
deployment are introduced only with the first separately approved external-asset policy described in Section 20.

### 3.8 Insurance

A separate Treasury-owned module account used only for authorised covered-loss payments. It is never a peg-redemption
source. At launch it may receive and pay only already-issued `anoah`; every mixed or non-NOAH transfer is rejected. A
deposit grants no coverage, claim priority, refund, ownership, governance, or withdrawal right. If a later Insurance
policy needs exact in-kind payouts, governance must first approve an explicit custody/payout allowlist; payout
eligibility does not by itself create target credit.

Insurance funds remain in the module account. The Claims committee is an ordinary threshold-multisig account appointed
for one monotonically termed, half-open height window with a fixed gross `anoah` claim limit. It signs typed claim
submissions and cancellations; each committee submission permanently consumes that term allowance even if the claim is
later cancelled. It receives no custody, module-account permission, generic send authority, or ability to mint or
borrow. Governance owns the mandate, may submit claims without depending on it and without consuming the committee
allowance (the shared cancellation period is a Treasury parameter), and may cancel any claim during the shared
cancellation period even if the committee appointment has since changed or ended.

### 3.9 Market pool denomination

The denomination carried by Market's `BasePool` `sdk.DecCoin` is owned by Market. Ark launches with `asdr` as this
virtual-pool unit, but
the later basket-pegged flagship may replace it while the chain is live. `ArkPoolDelta` is a signed decimal amount in
the current `BasePool.Denom`; it must never exist without that unit being observable. The pool denomination is not
Treasury's liability numeraire, the tax-cap denomination, the Oracle native-stable registry, or a declaration that all
other stable liabilities have been retired. A candidate denomination must first be supported by Oracle, but the actual
pool-unit change is executed only by Market's governance-authorised `MsgUpdateParams` handler.

Treasury's `reference_tax_cap` is a separate governance-owned policy Coin. A Market pool-unit transition neither reads
nor changes it. Governance updates that Treasury denomination separately only when it deliberately wants the tax-cap
reference to follow the new flagship; it is not a prerequisite for the Market transition.

### 3.10 Live pool-unit transition

A denomination-changing `MsgUpdateParams` is a pure re-denomination of one virtual pool, not creation of a second pool.
The submitted `BasePool.Amount` is a non-binding audit expectation in the candidate denomination. At execution, Market
uses one fresh immutable Oracle snapshot to derive and store the live-equivalent amount, emits both the submitted and
applied values without comparing them to a rejection threshold, and rescales `ArkPoolDelta` by the same amount ratio. No
transition object, second message, parallel pool, or persistent transition state is introduced. The transition changes
no stablecoin's offer or output eligibility; `asdr` remains fully supported.

A same-denomination depth change also rescales `ArkPoolDelta` by `new_base / old_base`. Both paths preserve the relative
curve position instead of allowing a governance parameter update to create an implicit imbalance reset or immediate
repricing without conversion flow.

## 4. Ownership boundaries

| Component                 | Owns                                                                                                                                                                             | Must not own                                                               |
| ------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| Market                    | Quotes, spread, final integer output, denomination-labelled virtual-pool state, conversion escrow, conversion mint/burn, atomic swap settlement                                  | Tax policy, claims mandate, target or fund-allocation calculations         |
| Treasury                  | Tax policy/routing, expansion-principal valuation and complete waterfall, fixed fund credits, subsidy pool, fund targets, Redemption Buffer, strategic Reserve, Insurance claims | Quotes, spread, pool state, gross conversion custody, conversion mint/burn |
| Oracle                    | Consensus prices, Tobin taxes, participation scores, Oracle reward allocation                                                                                                    | Fiscal allocation decisions, minting                                       |
| Distribution              | Validator/delegator fee accounting and payouts                                                                                                                                   | Tax classification, Buffer, Reserve, Insurance                             |
| App ante                  | Fee validation, feegrant semantics, transaction priority, invoking exact Treasury tax collection                                                                                 | Target-based allocation, a second tax formula, or persistent fiscal state  |
| Governance                | Market Params, Treasury policy, Claims Mandate, Claims submission/cancellation, Reserve mandates, role rotation                                                                  | Routine operations or automatic price/revenue controllers                  |
| Claims committee multisig | Off-chain adjudication and exact on-chain claim approval within the live Claims Mandate                                                                                          | Policy changes, direct custody, generic sends, Reserve use                 |
| Reserve executor multisig | Future typed Reserve actions within a live governance mandate                                                                                                                    | Claims, parameters, generic sends, mandate changes                         |
| Future Reserve guardian   | Immediate pause of an assigned future Reserve mandate                                                                                                                            | Claims, approval, payment, deployment, resume, widening, withdrawal        |
| Asset emergency committee | Immediate issuance halt or suspension of a failing asset within a live Asset Emergency Mandate (see `docs/ASSET_MODULE_PLAN.md`)                                                 | Recovery, resumption, settlement, write-off, retirement, reference choice, mandate changes |

Treasury exposes narrow Market-facing settlement operations. Market remains the sole owner and writer of `BasePool` and
`ArkPoolDelta`; Treasury exposes no Market-parameter operation, receives no Market keeper, and cannot initiate or
intermediate a pool amount or denomination change. This preserves the one-way Market-to-Treasury settlement dependency
and avoids a reverse keeper/depinject cycle. Neither module receives general debit authority over the other's accounts:

```go
type TreasuryKeeper interface {
    RouteExpansion(
        ctx context.Context,
        grossOffer sdk.Coin,
        stableOutput sdk.Coin,
        quoteRates oracletypes.RateSet,
    ) (treasurytypes.ExpansionAllocation, error)

    DrawRedemptionBuffer(
        ctx context.Context,
        redeemedStable sdk.Coin,
        noahOutput math.Int,
        quoteRates oracletypes.RateSet,
    ) (treasurytypes.BufferDraw, error)

    RecordSupplyChange(
        ctx context.Context,
        burned sdk.Coin,
        minted sdk.Coin,
        quoteRates oracletypes.RateSet,
    ) error
}
```

`ExpansionAllocation` is a handwritten, execution-local Treasury type, not protobuf state:

```go
type ExpansionAllocation struct {
    EligiblePrincipalNoah  math.Int
    RedemptionBufferCredit math.Int
    StrategicReserveCredit math.Int
    InsuranceCredit        math.Int
    SpreadAndDustBurn       math.Int
    OverflowBurn            math.Int
    TargetValuationComplete bool
}

func (a ExpansionAllocation) TotalBurn() math.Int
```

`TotalBurn` sums the two separately reported burn causes; it is not a second authoritative field. `RouteExpansion`
constructs every allocation amount directly from the positive gross offer and a monotonically decreasing remainder.

`RouteExpansion` requires positive `anoah` as `grossOffer`, a positive native stable `stableOutput`, and the rate map
already used by Market's quote. Treasury may add missing liability rates to that map but never replaces an existing
quote rate. It derives eligible principal itself, captures any additional unchanged-Oracle rates needed for aggregate
target valuation, calculates the complete waterfall, and only then moves the three fixed NOAH credits directly from the
Market module account to the Redemption Buffer, strategic Reserve, and Insurance. Gross custody never passes through the
Treasury subsidy-pool account, Treasury never burns, and Market cannot supply eligible principal, gaps, credits, burn
amounts, module names, or destinations.

After `RouteExpansion` returns, Market neither revalues the output nor recomputes targets or routing. It burns
`TotalBurn()`, mints the final stable output, and pays the receiver. Any later settlement failure rolls back Treasury's
preceding fund credits in the same transaction cache.

`DrawRedemptionBuffer` accepts the execution-local rate map already used by Market for the quote, initializes the
block-local aggregate-liability snapshot from unchanged Bank and Oracle state when necessary, calculates actual Buffer
coverage against pre-burn liability, applies that capped coverage to the quoted NOAH output, and moves only that amount
from the Buffer to Market. It returns the valuation inputs, exact payment, and conservative-fallback flag needed for
audit events. It exposes no path to strategic Reserve or Insurance. The separately authorised Reserve-to-Buffer message
is not part of this Market-facing interface. Module names are fixed internally; Market callers never supply arbitrary
source or destination accounts.

`RecordSupplyChange` is not an economic policy hook. Market calls it after each successful conversion burn/mint,
including stable-to-stable conversion, so Treasury can advance an already-created complete block-local liability
snapshot by the exact supply delta. It creates no snapshot when the block has not produced a complete aggregate
valuation. Any error rolls the whole conversion back in the transaction cache.

## 5. Module accounts and permissions

Target module-account configuration:

| Account name                 | Purpose                                       | Permissions        | Direct user sends |
| ---------------------------- | --------------------------------------------- | ------------------ | ----------------- |
| `market`                     | Conversion escrow and settlement              | `Minter`, `Burner` | Blocked           |
| `treasury_subsidy_pool`      | Balance-constrained subsidy pool              | None               | `anoah` only      |
| `treasury_redemption_buffer` | Coverage-based operational redemption inventory | None               | `anoah` only      |
| `treasury_strategic_reserve` | Strategic/emergency Reserve                   | None               | `anoah` only      |
| `treasury_insurance`         | Covered-loss Insurance                        | None               | `anoah` only      |
| `stability_tax_collector`    | Current reward-funding-window stability tax   | None               | Blocked           |
| `oracle`                     | Oracle reward pool                            | None               | Blocked           |
| `fee_collector`              | Validator reward funding before Distribution  | None               | Blocked           |

Module-to-module transfers remain possible. The `BlockedModuleAccountsOverride` list in `app/app_config.go` replaces the
SDK default blocked set, so every intended blocked account must be listed explicitly. Leave all four Treasury custody
accounts out of that list so normal bank transfers can reach them. Market, Oracle, `stability_tax_collector`, and
`fee_collector` remain explicitly blocked.

Treasury provides one recipient-aware bank `SendRestrictionFn`:

- Transfers to any of `treasury_subsidy_pool`, `treasury_redemption_buffer`, `treasury_strategic_reserve`, or
  `treasury_insurance` succeed only for a positive `sdk.Coins` value consisting solely of `anoah`; mixed or non-NOAH
  transfers fail atomically.
- Transfers to every other address pass through unchanged.

Bank restrictions may rewrite a destination before returning it. Configure Treasury last in Bank's explicit
`RestrictionsOrder` so it always validates the final recipient. With Treasury as the only launch restriction, list it
explicitly rather than relying on alphabetical order; if another restriction is later added, it must precede Treasury
unless it can prove it never rewrites a recipient.

For ordinary direct `MsgSend`/`MsgMultiSend` deposits, Bank's send-enabled check still applies before Treasury's
recipient rule. Passing Bank's general admission rule is necessary but not sufficient for a Treasury fund: the final
credit must also be positive and `anoah`-only. The same Treasury restriction applies to module, Wasm, IBC, and every
other enabled ingress that ultimately credits a fund through Bank.

The restriction owns no state and performs no conversion or redirection. Deposits use ordinary irreversible bank rails,
need no custom Treasury message or ledger, and are accepted even when a fund is at or above target. A deposit grants no
refund, ownership, withdrawal, coverage, claim-priority, governance, or deployment right. A taxable native-stable
deposit remains subject to the ordinary stability-tax rules for its user-facing transfer surface; the recipient does not
create an exemption.

No launch asset registry is needed: all four fund accounts have one allowed custody denomination, and consensus target
and settlement paths read only `GetBalance(..., anoah)`. The future external-asset phase adds explicit per-fund custody
and recognition policy together with the first approved asset; it does not pre-authorise arbitrary bank denominations.

Only Market may retain `Minter`. Tests must inspect the configured module-account permissions, not merely search for
calls to `MintCoins`.

## 6. Monetary flows

### 6.1 NOAH to stablecoin expansion

Inputs:

- `gross_offer`: positive integer `anoah` received from the trader and held in Market's transaction-local escrow.
- `stable_output`: final positive integer native-stable output after Market spread and minimum-receive validation.
- `quote_rates`: the Oracle snapshot already used by Market to produce that exact output.

Market passes those three facts to `RouteExpansion`; it does not pass eligible principal or any allocation decision.
Treasury validates the input denominations and derives the complete result. The first settlement in a block that needs
aggregate liability enumerates every nonzero-supply native stable and adds only missing rates to the shared
execution-local `quote_rates` map from unchanged Oracle state in the same execution context and block time. Treasury
may augment that caller-provided map in place, but it never refetches or replaces an existing quote. Treasury caches the
resulting liability only when the valuation is complete. Later settlements in the block reuse that snapshot; after each
successful conversion Market advances it through `RecordSupplyChange`. An incomplete valuation is not cached, so a
later settlement may retry it. This does not make Market enumerate Treasury liability denoms. A missing or stale
unrelated rate activates the conservative Buffer-only branch for the current settlement; it does not change
conversion-time valuation of the priceable `stable_output`. Missing or invalid output/NOAH quote rates are fatal.

Calculation:

```text
stable_output_noah = quote_rates.Convert(stable_output, anoah).Amount
eligible_principal_noah = truncate(stable_output_noah)
reject if eligible_principal_noah > gross_offer.Amount

spread_and_dust_burn = gross_offer.Amount - eligible_principal_noah

if target_valuation_complete:
  target_exposure_noah = value all pre-mint native-stable supply
                         plus stable_output exactly once

  redemption_buffer_gap = max(
    redemption_buffer_target_noah - redemption_buffer_anoah_balance,
    0,
  )

  strategic_reserve_gap = max(
    strategic_reserve_target_noah - strategic_reserve_anoah_balance,
    0,
  )

  insurance_unencumbered_anoah =
    insurance_anoah_balance - insurance_reserved

  assert 0 <= insurance_reserved <= insurance_anoah_balance

  insurance_gap = max(
    insurance_target_noah - insurance_unencumbered_anoah,
    0,
  )

  buffer_credit = min(eligible_principal_noah, redemption_buffer_gap)

  strategic_reserve_credit = min(
    eligible_principal_noah - buffer_credit,
    strategic_reserve_gap,
  )

  insurance_credit = min(
    eligible_principal_noah - buffer_credit - strategic_reserve_credit,
    insurance_gap,
  )

  overflow_burn = eligible_principal_noah - buffer_credit - strategic_reserve_credit - insurance_credit

else:
  buffer_credit = eligible_principal_noah
  strategic_reserve_credit = 0
  insurance_credit = 0
  overflow_burn = 0

total_noah_burn = spread_and_dust_burn + overflow_burn
```

Each account's actual `anoah` bank balance is its complete launch custody balance. For Redemption Buffer and strategic
Reserve, that complete balance counts toward the target. Insurance uses its unencumbered balance after subtracting
approved pending claims. The allocator never converts deposited assets or substitutes another denomination for the NOAH
credits above. Treasury constructs the complete allocation before making the first fund transfer: each credit is
bounded by the current remaining eligible principal and the final remainder becomes overflow burn. There is no
separate Treasury `ExpansionAllocation.Validate` method. `RouteExpansion` returns an error when it cannot derive or
execute the allocation. Market treats a successful result as authoritative and does not duplicate Treasury's checks or
recompute its conservation result.

Settlement:

1. Market produces the final quote, enforces minimum receive, and applies the virtual-pool transition in the transaction
   cache.
2. Market transfers `gross_offer` from the user into the Market module account.
3. Market calls `RouteExpansion(ctx, gross_offer, stable_output, quote_rates)`.
4. Treasury derives the full allocation, transfers each positive fixed credit directly from Market to
   `treasury_redemption_buffer`, `treasury_strategic_reserve`, and `treasury_insurance`, emits
   `ark.treasury.v1.EventExpansionAllocated`,
   and returns the execution-local result.
5. Market burns the returned `total_noah_burn` from its remaining escrow without recomputing or revalidating Treasury's
   valuation, targets, or waterfall.
6. Market mints exactly `stable_output`.
7. Market calls `RecordSupplyChange` with the actual NOAH burn and stable mint. If a complete liability snapshot exists,
   Treasury adds `stable_output` to it exactly once.
8. Market sends `stable_output` to the receiver and emits the conversion-settlement event from the returned result.

Conservation:

```text
gross_offer.Amount = eligible_principal_noah + spread_and_dust_burn

eligible_principal_noah =
  buffer_credit + strategic_reserve_credit + insurance_credit + overflow_burn

total_noah_burn = spread_and_dust_burn + overflow_burn

gross_offer.Amount =
  buffer_credit + strategic_reserve_credit + insurance_credit + total_noah_burn

Delta NOAH supply = -total_noah_burn
Delta stable supply = +stable_output
```

The transaction is the atomic boundary. Treasury does not retain the gross offer, create a pending allocation record, or
defer the waterfall to BeginBlock, EndBlock, or an epoch. A Treasury error or a failure in Market's burn, mint, or
receiver payment rolls back the pool transition, escrow transfer, every fund credit, supply change, and event together.
No separate stablecoin fee is minted and then burned; spread and integer dust remain part of the NOAH burn and are
reported separately from target-overflow burn.

### 6.2 Stablecoin to NOAH redemption

Inputs:

- `stable_offer`: gross stablecoins received from the trader.
- `noah_output`: final integer NOAH output after Market spread.
- `buffer_balance_before`: integer NOAH in `treasury_redemption_buffer` before settlement.
- `aggregate_liability_before`: complete transient liability snapshot before burning the offer, initialized from full
  bank supplies on the first aggregate valuation in the block.
- `quote_rates`: fresh offered-stable/NOAH rates already present in Market's execution-local pricing snapshot.
- `aggregate_rates`: rates captured by Treasury when the block-local snapshot is first initialized. They are read from
  unchanged Oracle state before any relevant stable burn; whether that snapshot is complete is an explicit result.

When aggregate valuation is complete, calculate:

```text
total_liability_noah_before = checked LegacyDec sum(
  aggregate_rates.Convert(full_supply(denom), anoah).Amount
  for every native stable denom with nonzero supply
)

redeemed_liability_noah =
  quote_rates.Convert(stable_offer, anoah).Amount

buffer_coverage = min(
  1,
  checked LegacyDec(buffer_balance_before).Quo(total_liability_noah_before)
)

buffer_paid = truncate(
  checked LegacyDec(noah_output).Mul(buffer_coverage)
)
residual_mint = noah_output - buffer_paid
```

Treasury uses `math.LegacyDec` consistently for converted liabilities, aggregate liability, fund targets, and the
coverage calculation, truncating only the final integer principal or payment. Aggregate summation and coverage arithmetic use the checked
decimal helpers in `pkg/decimal`. If aggregate summation, or adding a pending stable output to aggregate liability,
returns `decimal.ErrOutOfRange`, Treasury treats aggregate valuation as incomplete and uses the conservative fallback.
Failure to convert the stable output or redeemed stable input itself remains fatal. Never use floating point or an
alternate direct-rate multiplication path with a different rounding order.

The coverage numerator uses the Buffer balance that actually exists and the denominator uses complete pre-burn
liability. The gross stable offer still determines `redeemed_liability_noah` because the complete offer is burned and
removed from outstanding liability, but the Buffer funds its coverage share of actual post-spread output rather than a
nominal-liability entitlement. Always require `0 < noah_output <= redeemed_liability_noah`. Whenever aggregate
valuation is complete, additionally require `redeemed_liability_noah <= total_liability_noah_before`.

Do not derive the draw share from `redemption_buffer_target_ratio`, the current NOAH price, a price trend, or a target
gap. The draw is the Buffer's actual liability-coverage percentage applied to the quoted output. It is not an adaptive
controller.

The calculation is performed against the block-local liability after all preceding successful Market supply changes and
before the current burn, plus the pre-draw Buffer balance. The first calculation in a block reads Bank supply; later
calculations use the transient snapshot maintained from exact burn/mint deltas. The user-to-Market transfer may already
have occurred because it changes neither bank supply nor the Buffer. Market and Treasury rate maps come from the same
immutable on-chain price state; no Oracle update may occur within the transaction.

Settlement:

1. Quote `noah_output`, spread, and minimum receive from the immutable Market snapshot.
2. Apply the virtual-pool state transition based on the economic conversion, independent of funding source.
3. Transfer the complete stablecoin offer from the user to Market without changing supply.
4. Call `DrawRedemptionBuffer` while stable supply is still pre-burn. Treasury captures aggregate rates, calculates the
   coverage-funded output, and transfers exactly `buffer_paid` when positive. Zero payment performs no bank send but still returns
   the draw result.
5. Calculate `residual_mint = noah_output - buffer_paid` and validate any separately approved mint limit against only
   `residual_mint`. Failure rolls back the user transfer, pool update, and Buffer draw in the transaction cache.
6. Market burns the complete stablecoin offer.
7. Market mints exactly `residual_mint`.
8. Market calls `RecordSupplyChange` with the stable burn and residual NOAH mint, subtracting the redeemed liability
   from an existing complete block-local snapshot.
9. Market sends the complete `noah_output` to the receiver.

Conservation:

```text
noah_output = buffer_paid + residual_mint

Delta stable supply = -stable_offer
Delta NOAH supply = +residual_mint
Delta Redemption Buffer balance = -buffer_paid
Delta strategic Reserve balance = 0
Delta Insurance balance = 0
```

Boundary rules:

- If the Buffer is empty, `buffer_paid = 0` and Market mints the complete output.
- If the Buffer balance is at least aggregate liability, cap coverage at 100% and pay the complete quoted output from
  Buffer without minting.
- Retiring the final outstanding native stable liability does not automatically drain the Buffer. Apply pre-trade
  coverage to the final quoted output and leave any excess in the Buffer; do not gift it to the last redeemer or
  reclassify it.
- With complete valuation, floor rounding conservatively retains less than one base-unit NOAH per redemption relative to
  the exact coverage-funded output. Do not add a persistent remainder accumulator in the initial implementation.
- If the offered pair itself cannot be priced, fail atomically under the normal Market rules.
- If the current offer is priceable but another nonzero-supply native stable lacks a fresh rate, set `buffer_paid = 0`,
  mint the complete `noah_output`, and emit `valuation_complete = false`. This preserves redemption availability without
  allowing incomplete information to drain the shared Buffer.

If an approved hard residual-mint limit exists and the requested mint would exceed it, the entire transaction fails
atomically. No stablecoin is burned and no Buffer NOAH is moved. Because a visible quota or gate creates first-mover
pressure, any such limit requires a separate policy review rather than an implementation-time addition.

Let `B` be the pre-trade Buffer, `L` complete pre-trade liability, `R` redeemed liability, and `Q` quoted NOAH output.
Market's nonnegative spread guarantees `0 < Q <= R <= L`. With exact arithmetic and `B < L`:

```text
c = B / L
buffer_paid = c * Q
buffer_after - c * liability_after
  = (B - c * Q) - c * (L - R)
  = c * (R - Q)
  >= 0
```

Flooring `buffer_paid` only retains more Buffer, so actual Buffer coverage cannot fall. If `B >= L`, 100% coverage pays
`Q`; `(B - Q) - (L - R) = (B - L) + (R - Q) >= 0`, so the Buffer remains fully covered. For consensus tests, define
`liability_after` with the same fixed `LegacyDec` snapshot shown above; do not re-fetch rates. Define actual
`buffer_after = buffer_balance_before - buffer_paid`; post-burn integer supplies may be checked separately. When
`liability_after > 0`, assert non-decreasing coverage by cross-multiplication rather than a separately rounded ratio.

This removes the former whole-Reserve depletion threshold. It does not guarantee the peg or remove sell pressure;
Buffer-funded NOAH and newly minted NOAH are both liquid in the recipient's hands.

### 6.3 Public ratios and no-cliff rule

All module balances and stable supplies are public, so participants can always calculate Buffer, strategic Reserve, and
Insurance ratios. Ark must make their meanings explicit rather than try to hide them:

- The Redemption Buffer ratio measures how much existing NOAH is recycled per unit of liability. It is not a backing or
  solvency ratio.
- The strategic Reserve target ratio measures the Reserve balance against its target at launch. After a governed
  Reserve-to-Buffer commitment, only the NOAH still held by Reserve counts here; the committed amount appears only as
  Buffer inventory and is never double-counted. Future external target credit follows Section 20 and never arises from
  custody alone.
- The Insurance target ratio measures the unencumbered Insurance balance available for covered claims, not
  peg-redemption capacity. Its raw bank balance, reserved amount, and unencumbered balance must be reported separately.
  At launch, authorised claims can pay only held `anoah`. Future in-kind payout eligibility is a separate allowlist and
  does not imply target recognition.

No ratio crossing zero, target, or another threshold changes the redemption quote, spread, minimum receive, or
eligibility. Strategic Reserve and Insurance are excluded from automatic redemption funding, while their passive target
gaps still govern the expansion routing in Section 6.1. Queries and user-facing documentation must report all three
balances separately and must not publish a combined "percent backed" health score. Participants may still use each
balance as a risk signal; the protocol cannot prevent that. The goal is to remove the first-come exhaustion rule and
make each signal correspond to one honest mandate.

### 6.4 Governed strategic Reserve commitment

At launch governance may commit already-issued Reserve NOAH to the existing shared Redemption Buffer through
`MsgTransferReserveToBuffer`. This is the only production path that may debit `treasury_strategic_reserve`. It is a
standalone Treasury policy action, never a Market callback or part of an individual redemption.

The request contains:

```text
authority
amount: sdk.Coin                    // positive anoah
minimum_reserve_balance: sdk.Coin   // nonnegative anoah after transfer
```

The general Treasury `authority`, configured as `x/gov` at launch, authorises the message. Do not use the Claims
committee or introduce a launch Reserve operator. Source `treasury_strategic_reserve`, destination
`treasury_redemption_buffer`, and denomination `anoah` are fixed in keeper code; the request has no recipient, purpose
selector, asset selector, conversion, or arbitrary call data.

Execution must:

1. Validate the authority and both canonical `anoah` Coins.
2. Require a positive transfer and a nonnegative minimum remaining balance.
3. Read the live Reserve `anoah` bank balance without consulting the Buffer balance, Oracle rates, fund targets, prices,
   or Market state.
4. Reject if the Reserve cannot fund the transfer or its post-transfer balance would be below `minimum_reserve_balance`.
5. Atomically call `SendCoinsFromModuleToModule` from `treasury_strategic_reserve` to
   `treasury_redemption_buffer`. The signed message and canonical Bank event are the audit trail; Treasury emits no
   custom Reserve-transfer event. A Bank failure changes neither balance and emits no successful transfer event.

Under the launch app order, governance executes passed proposal messages in a cached EndBlock context after that block's
transactions. The commitment therefore lands after every current-block Market settlement and before the next block's
settlements without a lock, queue, or Treasury EndBlocker. If any message in the proposal fails, governance commits none
of its message state. Keep the Reserve action outside Market so a redemption can never interleave with or trigger it.

The minimum remaining balance is a per-proposal stale-state guard, not a global protocol floor. Governance may approve
full emergency commitment by submitting `0anoah`. Do not compare against an exact expected pre-transfer balance because
an otherwise harmless permissionless Reserve deposit could make the proposal fail. Do not cap the action by the Buffer
target gap: targets are routing thresholds rather than custody caps, and an explicitly approved emergency commitment may
leave the Buffer above target or the Reserve below target.

For transfer `amount`:

```text
Delta strategic Reserve anoah = -amount
Delta Redemption Buffer anoah = +amount
Delta total NOAH supply = 0
Delta stable supply and consolidated liability = 0
Delta Market pools, quote, spread, and minimum receive = 0
Delta subsidy pool and Insurance balances = 0
```

The action succeeds even when Oracle valuation is stale or incomplete. The larger Buffer remains subject to the normal
redemption rules: incomplete aggregate valuation still draws zero, while a later complete-valuation redemption draws its
actual Buffer-coverage share and reduces residual mint without changing the quoted output or privileging a redeemer. Because
the visible governance proposal cannot change the quote or absolute output and the Buffer share follows live coverage, it
creates no protocol-level first-redeemer entitlement to the committed amount.

After the transfer, live balance-based gaps are recalculated normally. The Buffer gap may fall by up to `amount`; the
Reserve gap may rise by up to `amount`; and a Buffer above target remains above target. Later eligible expansion fills
whatever Buffer gap remains and then any Reserve gap before Insurance and overflow burn. Do not store a parallel
"deployed Reserve" credit or continue reporting committed NOAH as liquid Reserve capacity.

Commitment is one-way at launch. Do not add a Buffer-to-Reserve clawback: once transferred, the NOAH may leave the
Buffer only through the shared coverage-based redemption path. A public removal path would make committed inventory
reversible and create pressure to redeem before a proposed removal, especially if residual minting is ever bounded.
Governance proposal state already supplies a durable proposal ID, metadata, exact message, and execution outcome, so
Treasury stores no duplicate transfer ID or history collection. Live balances remain queryable through `FundStatus` and
Bank; the dedicated event and standard Bank transfer provide execution audit data.

### 6.5 Stablecoin to stablecoin conversion

Use one fresh offer/ask snapshot and convert the offer directly to the ask denomination. Do not route the calculation
through `BasePool.Denom`, and do not load `BasePool` or `ArkPoolDelta` for this path. Then:

- Burn the complete offered stablecoin.
- Mint only the integer ask amount after Tobin tax and dust.
- Use the larger of the offer and ask Tobin taxes.
- Do not touch NOAH, Redemption Buffer, strategic Reserve, Insurance, or `ArkPoolDelta`.

Direct stable-to-stable conversion makes the economic result independent of which flagship denomination currently labels
the virtual NOAH/stable pool. The basket and `asdr` remain valid in both directions; changing the pool unit does not
change stablecoin output eligibility.

### 6.6 Insurance claim

Governance stores one Claims Mandate containing a chain-derived monotonic term, exact committee, half-open activation
and expiry heights, and a fixed gross committee claim limit denominated in `anoah`. The shared cancellation period is
the governance-owned `claim_cancellation_period_blocks` Treasury parameter, and appointing a committee requires an
active span no shorter than the current period. An empty committee is the canonical disabled mandate while retaining
the latest term. Replacing or disabling the mandate always
advances that term, resets only the Claims allowance used, and never rewrites an existing claim or reservation.

The term, committee, and activation/expiry window are the shared mandate envelope: one `ark.mandate.v1` proto message
embedded here and by the Asset Emergency Mandate (`docs/ASSET_MODULE_PLAN.md`), with term, window-activity, and
disabled checks in `pkg/mandate`. The envelope is a shared type and helper library, deliberately not a module; the
claim limit and every Claims power remain Treasury-owned fields under the confirmed decisions above.

During the active window, the committee submits a positive `anoah` claim with the exact current expected term;
governance submits under the same validation and held-balance rules at any height, without an expected term and
without reading the mandate. Treasury assigns the claim a globally monotonic `uint64` ID from consensus state. A
committee submission must fit within the remaining Claims allowance and atomically increases the allowance used; a
governance submission does not consume that delegated allowance. Treasury reserves the amount and derives the
executable height from the params cancellation period; a committee claim additionally stores the mandate term and its
executable height must not exceed the mandate expiry, while a governance claim records term zero. During the half-open
cancellation period, governance may cancel any pending claim without depending on the current mandate term. The current
active committee may cancel only a non-governance-submitted claim and must supply the exact current term. At the
executable height, cancellation closes for both actors and any account may execute the immutable payment. Paid claims
are final.

Cancellation and execution release the Insurance reservation but never restore Claims allowance usage. There is no
guardian, category allowlist, per-claim cap, governance late-cancellation override, or governance-cancellation flag. The
immutable `origin` records whether the effective committee or governance authority submitted the claim, even if either
address later rotates; `submitter` records the exact address and `finalized_by` records cancellation or execution. The
committee term limit bounds delegated gross claims, while the Insurance balance and existing reservations remain the
hard aggregate funding bound. See Section 10.5 for the authoritative launch messages.

#### Deferred extended claims model

The following separate policy version, rolling allowances, configured pause/retirement state, execution deadline, and
exceptional payout path are not part of the launch implementation. They require a separate governance and protobuf
review before use.

The chain cannot determine whether an incident was truly a protocol bug, covered smart-contract failure, scam, key loss,
or user-authorised transfer. A governance-appointed committee adjudicates ordinary claims off chain, while Treasury
enforces a bounded Claims Mandate and records every decision. The Claims subsystem contains these governed terms:

```text
version                        // separate from the launch appointment term
configured_state               // disabled | active | paused | retired
term_start_height
payout_denom                   // anoah at launch
approval_window_blocks
approval_window_cap
minimum_uncommitted_insurance_balance
execution_period_blocks
```

and this consensus-owned accounting:

```text
approval_window_start_height
approval_window_used
insurance_reserved
```

Governance creates, renews, narrows, widens, or replaces the mandate, sets its configured state, and rotates either
role. Counters and Insurance reservations are consensus-owned accounting, never caller-supplied values and never reset
implicitly by a deposit, expansion allocation, same-term policy edit, cancellation, claim expiry, or payment.
Approval-window usage resets only at its deterministic policy-defined boundary. A deliberate renewal may establish a new
term allowance and approval-window anchor only through an explicit governance renewal and audit event; Insurance
reservations carry into the new term unchanged. The term cap remains the longer-horizon bound across adjacent
approval-window boundaries.

Approval windows are fixed non-overlapping height ranges anchored at `term_start_height`; they are not implemented by
scanning the Claims map. On approval, Treasury derives the current range from `approval_window_blocks`, advances the
stored window start and zeroes only the window usage when that range changes, then applies the cap. Changing the term
start or window length requires an explicit renewal. This makes the accounting bounded and deterministic, while the term
cap limits a burst split across one window boundary.

A read-only policy query derives the current window start from height. If no approval has advanced stored accounting
into that window, it reports current-window usage as zero without writing state; it does not present the prior window's
stored usage as current. The next successful approval commits the derived start and usage normally.

Use exact half-open height boundaries and checked unsigned addition:

```text
term_start_height <= activation_height < expiry_height

approvals_enabled =
  configured_state == active &&
  activation_height <= current_height < expiry_height

committee_execution_allowed =
  claim.status == pending &&
  configured_state != paused &&
  claim.executable_height <= current_height < claim.execution_deadline_height

claim_expirable =
  claim.status == pending &&
  current_height >= claim.execution_deadline_height
```

The query surface reports `configured_state`, a derived `approval_status` (`disabled`, `pending`, `active`, `paused`,
`retired`, or `expired`), and `execution_paused` separately. Derive approval status in that order: configured
`disabled`, `paused`, or `retired` wins; otherwise a height before activation is `pending`, a height at or after expiry
is `expired`, and the remaining interval is `active`. Policy expiry and retirement disable new approvals but do not
invalidate an immutable pending approval. Only the configured `paused` state blocks committee-claim execution;
governance cancellation and permissionless expiry remain available while paused.

#### Committee approval

The committee threshold-signs an exact approval containing the policy version, unique bounded claim ID, bounded incident
or case reference, recipient, positive `sdk.Coins` amount, and external evidence reference. At launch the amount must
contain only `anoah`; keeping `sdk.Coins` preserves a later additive payout path without weakening launch validation.

Approval must:

1. Require exact canonical equality with the active mandate committee after normal multisig authentication.
2. Reject disabled, pending, paused, retired, or expired approval status.
3. Enforce unique claim ID, valid recipient/reference, approval-window cap, and the live remaining committee term
   allowance.
4. Require `insurance_balance - existing_insurance_reserved - amount` to remain nonnegative and at or above the policy's
   minimum uncommitted balance.
5. Store the complete immutable approval, authorizer, policy version/term, approval height, policy-derived
   `executable_height = approval_height + challenge_period_blocks`, policy-derived
   `execution_deadline_height = executable_height + execution_period_blocks`, and `pending` status. The committee does
   not choose either height.
6. Increment `insurance_reserved`, `approval_window_used`, and the live Claims allowance usage atomically. A failed
   approval changes none.

No coin moves on approval. The reservation is an encumbrance: it reduces the Insurance unencumbered balance immediately,
opening only the passive target gap used by future realised expansion principal. It never mints, converts, trades,
liquidates, invokes Market, or draws another fund.

#### Execution and cancellation

After the challenge period, any fee-paying account may execute the exact stored approval while it remains pending,
unexpired, and unpaused. Execution rechecks the Insurance balance, atomically sends the held `anoah` from
`treasury_insurance`, reduces `insurance_reserved` by the same amount, marks the claim paid, and emits the audit event.
The committee never receives custody or chooses different execution fields.

Because approval already excluded the reserved amount from target eligibility, ordinary execution changes
`insurance_balance` and `insurance_reserved` by the same amount and creates no second target-gap shock. Governance may
cancel a pending claim; cancellation releases the reservation and closes the corresponding gap without moving coins.
Cancellation does not automatically restore approval-window or term allowance, preventing repeated approve/cancel cycles
from redirecting expansion funding. At or after a stored execution deadline, any fee-paying account may mark the claim
expired and release its reservation; expiry likewise restores no allowance. Governance may explicitly account for a
replacement only in a later policy action.

Expiry is deliberately lazy to avoid a BeginBlock scan or a second deadline index. Until `MsgExpireInsuranceClaim` or a
governance cancellation commits, an overdue claim remains `pending`, its reservation remains encumbered, and
`FundStatus` continues to subtract it. Launch operations must monitor the paginated Claims query/events and submit the
permissionless expiry message promptly; tests must preserve this interim accounting rather than silently treating an
unprocessed deadline as released.

Mandate expiry or retirement stops new approvals. Each already-approved claim follows its stored execution deadline
unless governance globally pauses execution or cancels it individually. Replacement and committee rotation affect new
approvals only; they do not silently widen, rewrite, cancel, or re-authorise an existing approval, whose exact terms
remain stored in the claim record.

A pause does not toll a pending claim's stored deadline. If a safety pause lasts through that deadline, the claim
becomes non-executable but remains pending and encumbered until permissionless expiry or governance cancellation
commits. Any replacement requires a later exact approval or governance override; neither a committee nor a state update
may extend the old claim implicitly.

#### Governance override

Governance may authorise one exact exceptional payout through a separate governance-signed message. This path may bypass
configured policy state, approval-window, term, challenge-delay, and retained-balance limits because the proposal itself
is the explicit policy decision. It must still enforce a unique claim ID, valid recipient/reference, positive held
`anoah` only, sufficient balance, no mint, no conversion, no borrowing, and no debit from Redemption Buffer, strategic
Reserve, or the subsidy pool. It may not consume NOAH reserved for pending claims unless the same atomic proposal first
cancels those claims.

Any future pause mechanism requires a new governance and protobuf review; launch Claims has no guardian or pause role.

Every claim ID is permanently single-use across pending, paid, cancelled, and expired records. Every claim record and
event identifies `committee` or `governance_override` authorization. Governance can cancel a pending claim or compensate
through a new exact payment, but no authority can claw back a settled payment. A deposit does not itself establish
coverage, priority, or entitlement. A future exact in-kind claim path must be introduced with an explicit Insurance
custody/payout allowlist whose target-credit allowlist remains a narrower, independently governed subset.

## 7. Redemption Buffer, strategic Reserve, and Insurance targets

### 7.1 Initial exposure definition

The initial exposure measure is the full outstanding bank supply of every native Ark stablecoin configured in Oracle's
Tobin-tax list. Coins held by users and module accounts all remain outstanding liabilities and are included.

Using one fresh Oracle snapshot and the same `LegacyDec` conversion path as Section 6.2:

```text
target_exposure_noah = checked LegacyDec sum(
  rates.Convert(full_supply(denom), anoah).Amount
  for every native stable denom with nonzero supply
)

target_noah(target_ratio) =
  target_ratio.MulRoundUp(target_exposure_noah).Ceil().TruncateInt()

redemption_buffer_target_noah =
  target_noah(redemption_buffer_target_ratio)

strategic_reserve_target_noah =
  target_noah(strategic_reserve_target_ratio)

insurance_target_noah =
  target_noah(insurance_target_ratio)
```

Target exposure and ratios remain `LegacyDec` values through multiplication; only the final target is rounded up to
integer `anoah`. If the aggregate exposure cannot be represented while summing converted supplies, valuation is
incomplete and the caller uses its documented fallback. Do not convert through SDR or divide by a separate NOAH/SDR
price.

For the expansion currently being settled, `RouteExpansion` gets the current pre-mint liability from the transient
snapshot, initializing it from Bank supply only when no snapshot exists, and adds the new integer stable output exactly
once before calculating the target. It values that output with the same `LegacyDec` conversion and quote snapshot
used to derive eligible principal. After mint, `RecordSupplyChange` advances the cached base for the next settlement.
The allocation therefore uses post-trade liability rather than lagging by one transaction. `RouteExpansion` runs and
settles once per successful NOAH-to-stable conversion; there is no pending-principal accumulator, periodic revaluation,
batch waterfall, or Treasury EndBlock settlement. The transient value is only a derived execution cache and resets at
the block boundary. Queries continue to derive liability from canonical Bank and Oracle state. The initial target base
is nominal liability and is not exposed as a duplicate generic target-exposure field. A later risk-exposure model must
add explicit per-fund exposure fields so it can change targets without changing nominal liability or redemption
coverage.

### 7.2 Asset recognition

Initially:

- Count the liquid `anoah` bank balance in Redemption Buffer and strategic Reserve toward their targets. For Insurance,
  count only `insurance_anoah_balance - insurance_reserved`; approved pending claims are encumbered and cannot
  simultaneously cover another loss.
- All four fund accounts reject mixed and non-NOAH deposits because every launch use is NOAH-denominated.
- Never count an outstanding Ark-issued stablecoin as backing for Ark's consolidated stablecoin liabilities, including
  if a future policy permits one to be held by the strategic Reserve or Insurance. It remains a liability until burned
  and is permanently ineligible for target credit against that consolidated exposure.
- Do not count the subsidy pool as Redemption Buffer, strategic Reserve, or Insurance.
- Do not count one fund's balance toward another fund.
- Never convert a deposited asset automatically.
- Keep launch target and settlement calculations denomination-specific: read `anoah` directly and never iterate a fund
  account or infer target credit from arbitrary custody.

Because every balance counted toward a target and every custodied asset is initially NOAH, the target system is
correlated and endogenous. The funds buy time, smooth conversion issuance, or cover defined claims; they do not
guarantee solvency if NOAH itself collapses. Claim reservations change availability, not custody or supply.

For strategic Reserve and Insurance, future external assets change recognised capital, not the exposure that creates the
capital requirement:

```text
required_capital = target_ratio * covered_risk_exposure

recognised_capital = liquid_unencumbered_anoah + sum(risk_adjusted_eligible_external_assets)

capital_gap = max(required_capital - recognised_capital, 0)
```

Do not calculate a target as a percentage of the fund's own holdings and do not count the gross value of every asset in
the account. For each explicitly eligible external asset, future recognition must use:

```text
risk_value = conservative_price
             * unencumbered_settled_balance
             * credit_and_custody_factor
             * liquidity_factor

recognised_asset_value = min(
  risk_value,
  remaining_asset_cap_headroom,
  remaining_correlated_group_cap_headroom,
)
```

Eligibility is fund-specific and identifies the exact native denom, token contract, or IBC trace. Unlisted assets,
pending IBC transfers, unsettled or externally uncontrolled balances, encumbered assets, and assets without a fresh
approved price receive zero current credit. Receipt tokens and their underlying assets must never both be counted.
Ark-issued stablecoins always receive zero target credit while outstanding, even if a future custody or payout policy
allows a fund to hold them. Recognition failures are conservative accounting events: they may reopen a gap, but they
must never trigger minting, forced selling, automatic conversion, or deployment.

External recognition must also preserve a separately configured liquid-capital requirement:

```text
total_gap = max(total_required_capital - recognised_total_capital, 0)
liquid_gap = max(liquid_required_capital - immediately_usable_capital, 0)
allocation_gap = max(total_gap, liquid_gap)
```

The same liquid NOAH may satisfy both requirements, so the gaps use `max`, not addition. Reserve and Insurance may use
different recognition factors, concentration caps, and liquid requirements because their loss horizons and permitted
uses differ. Concentration limits must also aggregate correlated exposure across both funds so splitting one risk
between accounts cannot evade the cap.

When Reserve spends 100 NOAH to acquire an asset recognised at 70 after all haircuts, only a 30-NOAH target gap reopens.
Giving the asset zero credit would reopen the entire 100 and could create a repeated acquisition/refill loop that
captures expansion principal; giving it unhaircutted 100 credit could falsely close the gap. This accounting rule is
therefore part of the anti-feedback policy, not only reporting.

### 7.3 Passive-target rules

- Targets decide only where new eligible expansion principal goes.
- Targets are not custody caps. Permissionless deposits remain valid when a fund is at or above target.
- A falling target never releases, burns, trades, or transfers an existing balance.
- Coverage-based Redemption Buffer settlement may spend below target; the target itself never triggers that draw.
- Strategic Reserve has no automatic withdrawal path. Its only launch debit is the discrete governance-authorised
  `anoah` commitment to the Redemption Buffer defined in Section 6.4; no target, price, trend, Oracle result, or Market
  request triggers or sizes it.
- The committee or governance may submit a claim only during the active Claims Mandate term. Submission may encumber
  Insurance below target and execution may spend the held balance, but the target itself authorises neither action.
- Expansion fills Redemption Buffer before strategic Reserve, strategic Reserve before Insurance, and burns only the
  remaining eligible principal after all three gaps are filled.
- A Reserve-to-Buffer commitment immediately changes only the two live balances. Any resulting Reserve gap participates
  in that same future expansion waterfall; no mint, tax route, conversion, or separate recapitalisation transaction is
  created. Repeated governance commitments may therefore delay Insurance funding and overflow burn.
- If complete liability valuation is unavailable during expansion, route all current eligible principal to the
  Redemption Buffer. `RouteExpansion` sets the Reserve, Insurance, and overflow-burn amounts to zero. Do not route
  uncertain principal elsewhere, burn it, or fail an otherwise priceable conversion. Failure to value the final output
  itself remains a hard conversion error before any fund transfer.
- If complete aggregate valuation is unavailable during a priceable redemption, draw zero from the Buffer and mint the
  complete quoted output as specified in Section 6.2.
- `FundStatus` should return an error when it cannot produce a complete live valuation rather than present a partial
  target as complete.
- Direct deposits trigger no target-driven transfer, trade, conversion, or deployment and create no depositor rights or
  authority over the governed Reserve-to-Buffer path.
- Claims Mandate submission and cancellation affect the Insurance reservation, unencumbered balance, and resulting
  coverage ratio/gap; neither changes required target exposure or ratio parameters, prices, issuance, another fund, or
  mandate term automatically.

Completeness is defined by outstanding liabilities, not by either configurable policy denomination. A missing SDR rate
alone is irrelevant when `asdr` supply is zero; nonzero `asdr` supply requires a fresh `asdr`/NOAH value like any other
liability. A stale `Params.reference_tax_cap.denom` rate affects cap refresh only. A stale Market pool-denom rate may
prevent a Market quote that needs the virtual pool, but it is not an additional Treasury aggregate-valuation dependency.

When the Redemption Buffer is at its target and prices are unchanged, an expansion increases both liability and Buffer
inventory according to the target ratio, while a later redemption releases the same actual Buffer fraction
proportionally. The response is continuous rather than zero issuance followed by a full-issuance cliff.

Keep nominal-liability valuation and target-exposure valuation as explicit concepts even while both initially use total
stable supply. Coverage-based redemption always uses nominal liability as the denominator for actual Buffer coverage. A
later risk-exposure model may replace the target denominator without silently changing redemption funding shares. Keep both
calculations single-sourced so that later target changes do not duplicate Market settlement logic.

## 8. Stability tax policy

### 8.1 Policy and governance settings

- `MonetaryPolicy.stability_tax_rate`: reversible rate in `[0, 1]`.
- `Params.reference_tax_cap`: governance-owned nonnegative Coin with a canonical Ark-native base denomination, launched in
  `asdr`; zero means no tax ceiling.
- Per-denom `TaxCaps`: derived state, not an adaptive policy controller.

The effective reference cap is stored directly in Params. Its denomination is only the unit for canonical tax-cap
policy. Treasury liability and target valuation is always a direct NOAH-equivalent calculation, while Market's current
virtual-pool unit is carried independently by `BasePool.Denom`. Changing the reference cap must not modify fund
balances, `BasePool`, `ArkPoolDelta`, conversion quotes, or redemption coverage. Changing Market's pool denomination
must likewise leave Treasury values and coverage-funded outputs unchanged when the underlying stable/NOAH rates are unchanged.

There is no tax-rate controller, revenue target, mining increment, change-rate limit, probation window, or rolling
revenue indicator.

### 8.2 Taxable denominations

- Native Ark stablecoins configured in Oracle's Tobin-tax list are taxable.
- NOAH is not taxable at inception.
- A configured stable denomination with no tax cap is a configuration error and fails closed.
- An explicit zero cap means the configured denomination is uncapped; it is distinct from a missing cap.
- Oracle's current Tobin-tax list is authoritative at calculation time. A retained cap for a denomination no longer in
  that list does not make the denomination taxable.
- A zero global tax rate disables tax without deleting denomination configuration.

### 8.3 Taxable operations

Initial signed-message coverage:

| Message                                        | Tax principal                                                       |
| ---------------------------------------------- | ------------------------------------------------------------------- |
| `bank.MsgSend`                                 | Sent stablecoins                                                    |
| `bank.MsgMultiSend`                            | Each input's stablecoins independently; never include outputs       |
| `market.MsgSwapSend`                           | Stablecoin offer, when the offer denomination is taxable            |
| `market.MsgSwap`                               | Exempt; self-returning conversion remains governed by Market spread |
| `authz.MsgExec`                                | Recursively inspect all nested messages                             |
| IBC `MsgTransfer`, when enabled                | Outbound taxable stablecoin token                                   |
| Wasm instantiate/execute, when enabled         | Attached taxable stablecoin funds                                   |
| Treasury, Oracle, governance, staking messages | No transfer-tax principal                                           |

Every user-facing stablecoin transfer surface enabled in production must use the same Treasury calculator. Phase 4
implements IBC foundations before Wasm, because contracts may dispatch IBC messages, and designs the Treasury execution
hook into both paths from the start. Neither surface may become production-accessible until the complete activation gate
passes. Top-level IBC transfers and Wasm attached funds are visible to ante inspection. Contract-generated bank or IBC
submessages are discovered only during execution, so the custom Wasm/IBC integration must invoke the same calculator at
that execution boundary rather than pretending the `TxFeeChecker` can see them in advance.

A launch deposit to any Treasury fund is `anoah` and has zero stability-tax principal. A mixed or non-NOAH deposit is
rejected by the fund restriction; it does not become admissible merely because the normal tax calculator could assess
its denomination. When an external asset is later allowlisted, its user-facing transfer remains subject to the same tax
classification as a transfer to an ordinary account; the fund recipient creates no exemption.

An execution-generated transfer is a Bank send, IBC send, contract execute with funds, or contract instantiate with
funds created by a contract while it runs and therefore absent from the original signed message list. It uses the same
per-input calculation at its execution boundary. The taxable input is the complete coin set supplied to that transfer.
The sending contract pays the resulting tax in addition to the complete requested principal; the recipient amount is
never reduced to fund tax. An ante fee payer or feegrant granter pays only ante-visible tax and does not implicitly
sponsor execution-generated tax. For each input/denomination pair:

```text
tax[input][denom] =
  floor(principal[input][denom] * stability_tax_rate)                    if TaxCaps[denom] == 0
  min(floor(principal[input][denom] * stability_tax_rate), TaxCaps[denom]) otherwise
```

Inputs are not combined merely because they share a source or occur in the same transaction. Each `MsgSend`,
`MsgSwapSend`, and `MsgMultiSend` input receives its own cap, as does each later execution-generated transfer input.
Ante owns the signed top-level path and the Wasm dispatcher owns only execution-generated messages, so exactly-once
assessment requires no persistent transfer ID or context marker. Failed or reverted submessages roll back their tax
with the transfer before either module is enabled.

Do not tax arbitrary `BankKeeper` sends. A global bank hook would tax Market settlement, Redemption Buffer draws, future
external strategic Reserve deployment, Insurance claims, Oracle funding, validator distribution, the governed
Reserve-to-Buffer commitment, and other protocol-internal movements unless it recreated a complex exemption system.

### 8.4 Tax calculation

The calculator recursively extracts taxable inputs and applies each denomination's cap independently to each input:

```text
uncapped_tax[input][denom] = floor(principal[input][denom] * stability_tax_rate)

tax[input][denom] = uncapped_tax[input][denom]                         if TaxCaps[denom] == 0
tax[input][denom] = min(uncapped_tax[input][denom], TaxCaps[denom])     otherwise

total_tax[denom] = sum(tax[input][denom] for every taxable input)
```

Two `MsgSend`s from the same source are two independently capped inputs. Distinct `MsgMultiSend` inputs are also capped
independently. Ante fee-payer and feegrant semantics determine only which account funds the complete declared fee.

Malformed nested messages fail closed. The consensus checker, router, and query must call the same implementation.
Candidate message inputs are validated before tax state is read. For each encountered denomination, Treasury checks the
current Oracle-configured taxable set and then loads the derived cap. A configured denomination with no cap returns an
explicit configuration error; a stale cap for an unconfigured denomination is ignored. Per-denomination totals use
checked addition and return an out-of-range error rather than panicking when the aggregate is not representable.

### 8.5 Tax-cap refresh and reference changes

Treasury periodically converts a positive `Params.reference_tax_cap` into every configured taxable stablecoin using one
fresh Oracle snapshot. A zero reference cap instead produces a complete map of explicit zero entries without requiring
prices.

Refresh rules:

1. Refresh on the fixed weekly boundary.
2. Also refresh when the configured stable-denom set differs from the stored cap-denom set.
3. Build the complete replacement map in memory before writing anything.
4. If any required price is missing, stale, invalid, or unrepresentable, retain the complete previous map.
5. Never partially update the cap set.
6. For a taxable denomination equal to `Params.reference_tax_cap.denom`, copy `Params.reference_tax_cap.amount` directly
   instead of converting it.
7. A positive reference cap must derive a positive integer cap for every denomination. Reject a conversion that
   truncates to zero so it cannot accidentally become the uncapped sentinel.

This conversion keeps caps approximately equal in value without making tax-rate policy reactive to revenue.

Changing the effective reference cap has stricter replacement semantics than a scheduled refresh:

1. The candidate Coin must be nonnegative and use a canonical Ark-native base denomination configured in Oracle's native-stable
   set. A positive candidate must already have every fresh, positive, representable consensus rate needed for conversion;
   a zero candidate needs no conversion rates.
2. Governance-only `MsgUpdateParams` rebuilds the complete candidate map whenever the reference Coin changes. Params and
   the cap map commit together or neither changes.
3. Neither `MsgUpdateMonetaryPolicy` nor `MsgCommitteeUpdateMonetaryPolicy` does more than validate and store the
   candidate policy. No rate change rebuilds caps because the complete cap map exists independently and cap values do
   not depend on the rate.
4. If a reference-cap change cannot derive every candidate cap, reject `MsgUpdateParams` and preserve the old Params and
   cap map. A scheduled or denomination-mismatch refresh likewise retains the old map when valuation is unavailable.
5. Emit the complete derived tax-cap replacement from `MsgUpdateParams` or BeginBlock whenever either path successfully
   replaces the map. InitGenesis likewise requires or derives a complete map even when tax is disabled. If a configured
   taxable denomination is nevertheless missing at calculation time, `ComputeTax` fails closed when tax is positive.

Governance must configure the candidate denomination in Oracle and wait for a fresh settled rate before proposing the
Treasury cap change. A reference-cap update never changes Market's pool denomination. Market performs that independent
live transition only through the denomination-changing `MsgUpdateParams` contract in Section 15.3.

### 8.6 Fee checking and routing

Use Cosmos SDK v0.54.3's stock ante handler with a custom `TxFeeChecker`, then wrap the completed stock ante handler to
route the tax. Do not copy the SDK decorator list into Ark.

The stock order remains:

```text
SetUpContext
ExtensionOptions
ValidateBasic
TxTimeoutHeight
ValidateMemo
ConsumeGasForTxSize
DeductFee(custom TxFeeChecker)
SetPubKey
ValidateSigCount
SigGasConsume
SigVerification
IncrementSequence
```

After the stock handler succeeds, the Ark wrapper:

1. Skips routing during simulation.
2. Recomputes the exact tax with the same Treasury calculator.
3. Moves the complete exact tax from `fee_collector` to `stability_tax_collector` for every denomination.
4. Leaves only gas fees and overpayment in `fee_collector`.

This ordering means invalid signatures never reach routing. If routing fails, BaseApp's ante cache rolls back feegrant
usage, fee deduction, account sequence, and routing together.

For a contract-generated transfer that was not knowable in ante, the custom Wasm/IBC execution adapter:

1. Calculates the tax for that execution-generated transfer input independently.
2. Debits that tax from the sending contract account in the transferred denomination, in addition to the complete
   requested principal.
3. Sends the complete exact input tax to `stability_tax_collector`.
4. Performs the tax collection and transfer in the same Wasm submessage cache.

Only the contract dispatcher calls this adapter; top-level handlers bypass it because ante already owns those inputs.
This structural separation replaces transfer IDs and context markers. Failure of any step changes no balances. This is
scoped transfer integration, not a global `BankKeeper` hook.

The custom fee checker must:

1. Require `tx` to implement `sdk.FeeTx`.
2. Compute mandatory tax in CheckTx, ReCheckTx, and block execution, but not simulation.
3. Require `declared_fee >= tax` in every non-simulation mode.
4. Calculate `gas_fee = declared_fee - tax` with `SafeSub`.
5. Apply validator-local minimum gas prices to `gas_fee` only, and only in CheckTx.
6. Calculate transaction priority from `gas_fee` only.
7. Return the complete declared fee so the stock decorator deducts gas, tax, and overpayment together.
8. Guard zero gas and values that cannot be represented by the SDK-compatible priority calculation.

Feegrant semantics remain standard for ante-visible tax: the granter pays the complete declared fee and its allowance is
charged for gas, tax, and overpayment. Feegrant does not make a granter or outer transaction fee payer responsible for
execution-generated tax incurred later by a contract; the sending contract pays it from its own balance.

Ante-visible tax and gas fees are retained when message execution fails after a valid ante, matching normal
transaction-fee semantics. A failure inside ante retains neither. Execution-generated tax commits only with its
corresponding transfer and rolls back when that transfer or its enclosing cached execution is reverted. A successfully
created IBC packet retains its tax if later acknowledgement, timeout, refund, or return bookkeeping occurs; those later
operations are not new taxable transfers.

SDK simulation skips the custom checker, so wallets must use the `ComputeTax` query rather than infer ante-visible tax
from a simulation response. Contracts use Ark's Wasm tax query for a proposed execution-generated message. The binding
parses that proposed message through the same execution adapter and calls Treasury's canonical calculator; it does not
reimplement rate or cap math. The result is advisory current-state data only: it reserves no funds, proves no future
balance, grants no execution authority, and does not replace recomputation immediately before dispatch.

## 9. Validator and Oracle funding

### 9.1 Revenue lanes

The launch model keeps the services separate:

- Gas fees go to `fee_collector` and then Cosmos distribution every block. Before Distribution empties the collector,
  Treasury values the eligible balances from the completed block and adds that value to its current funding window; it
  does not retain or move those gas coins.
- Complete stability tax remains in `stability_tax_collector` until Treasury settles the current funding window.
- Validator and Oracle funding retain separate per-block NOAH-value parameters, but Treasury adds the applicable target
  once for every observed completed block and compares only the aggregate window totals.
- The balance-constrained subsidy pool covers only aggregate shortfalls at settlement, not transient block-by-block
  gaps.
- Expansion proceeds fund the Redemption Buffer, strategic Reserve, and Insurance, not routine rewards.

Gas is the validator lane's first recurring source. Stability tax protects the Oracle target before it can fund a
validator gap. Tax above both target needs remains Oracle-performance-weighted validator/delegator compensation. The
allocation never changes the tax rate, either target, or total collected revenue.

`reward_funding_window` is a governed Treasury parameter whose default is one chain week. It does not reuse Oracle's
`RewardWindow`: Treasury shortfall accounting and Oracle score distribution remain separate clocks. When an empty
Treasury window records its first completed-block observation, it initializes `blocks_remaining` from the current
parameter, records the observation, and decrements the countdown. A governance update cannot rewrite a positive active
countdown; it takes effect when the next empty window records its first observation. Height 1 observes no earlier block.
Beginning at height 2, every Treasury BeginBlock observes the completed prior block, so the default clean-genesis first
settlement occurs at BeginBlock `DefaultRewardFundingWindow + 1`. After each successful settlement Treasury resets the
aggregate state and countdown to zero.

This windowing removes the asymmetry where a quiet block could spend subsidy immediately even though a busy block in the
same period would later overfund validators. For example, validator fee values of `0` and `2V` against two per-block
targets of `V` produce a zero aggregate validator shortfall. It smooths funding without changing collection, recipient,
supply, or target policy.

### 9.2 Reward-funding top-up algorithm

Treasury persists the current `RewardFundingState`:

```text
blocks_remaining       // observations still required before settlement
validator_target       // sum of the per-block validator target for observed blocks
oracle_target          // sum of the per-block Oracle target for observed blocks
validator_fee_value    // sum of eligible completed-block fee values in anoah
valuation_complete     // false after any required fee valuation is unavailable
```

For each completed block, add the then-current reward-target parameters. Parameter changes therefore affect only the
observations to which the new values are applied; previously accumulated target values are never recomputed. Value each
non-empty `fee_collector` balance before Distribution using that BeginBlock's accepted Oracle snapshot. `anoah` has
identity value. Only then-configured native stable denominations receive target credit; unrelated fee denominations
continue through Distribution but do not count toward the validator target.

The window-length parameter is the deliberate exception to per-observation parameter application: the first observation
initializes the countdown and each observation decrements it once. `MsgUpdateParams` never rewrites a positive countdown
or previously accumulated values. Each observation adds the then-current validator and Oracle targets with
`math.Int.SafeAdd`; an unrepresentable accumulated target fails that BeginBlock transition rather than being accepted,
clamped, or partially stored.

Let the completed window totals be:

```text
V = accumulated validator_target
O = accumulated oracle_target
B = current spendable subsidy-pool NOAH balance
G = accumulated validator_fee_value
T = NOAH value of eligible stability_tax_collector balances

validator_pre_tax_gap = max(0, V - G)
protected_oracle_tax = min(T, O)
remaining_tax = T - protected_oracle_tax
desired_validator_tax = min(remaining_tax, validator_pre_tax_gap)
```

Derive one validator allocation ratio from `desired_validator_tax / T`. Apply it independently to every eligible tax
denomination, rounding the validator amount down; Oracle receives the exact remainder of every coin. Tax in an
unexpected or no-longer-configured denomination also goes entirely to Oracle and receives no target credit. No tax coin
is converted merely to perform this accounting.

Revalue the actual post-rounding coin allocations from the same snapshot:

```text
VT = NOAH value of actual validator tax coins
OT = NOAH value of actual Oracle tax coins

validator_organic = G + VT
oracle_organic = OT
validator_shortfall = max(0, V - validator_organic)
oracle_shortfall = max(0, O - oracle_organic)
total_shortfall = validator_shortfall + oracle_shortfall
```

Consequently, gas at or above `V` sends all tax to Oracle. Tax at or below `O` also goes entirely to Oracle. Only tax
above the protected Oracle floor may replace finite subsidy spending on a validator gap, and every residual after both
targets are covered returns to Oracle.

`B` includes the initial genesis allocation and every completed permissionless `anoah` deposit. Deposits never change
`V` or `O`, so a large transfer extends shortfall coverage rather than increasing either reward target.

If `B >= total_shortfall`, pay both shortfalls in full.

If `B < total_shortfall`:

```text
oracle_paid = floor(B * oracle_shortfall / total_shortfall)
validator_paid = B - oracle_paid
```

This shares scarce coverage according to the actual shortfalls and assigns the final integer remainder to validators.
`total_shortfall` is an execution-local `math.Int` denominator. Each target, shortfall, balance, intermediate product,
and final payment must remain representable; arithmetic outside that deliberately bounded domain is unsupported. Handle
zero shortfall without division.
Subsidy-pool depletion never mints, borrows from the Redemption Buffer, strategic Reserve, or Insurance, or stops block
production merely because the balance is empty.

At settlement, value `anoah` directly and value the accumulated tax pot from one current accepted Oracle snapshot. Only
currently configured native stable denominations receive target credit. Any unexpected or no-longer-configured tax
denomination goes entirely to Oracle.

If a required validator-fee valuation is unavailable during any observation, set `valuation_complete = false` and do not
attempt to reconstruct or partially trust the window later. At the boundary, send the complete accumulated tax pot to
Oracle, spend no subsidy, emit `ark.treasury.v1.EventBlockRewardTopUpSkipped`, and reset the state. Use the same fallback
when tax valuation is unavailable at settlement. The window is never retained for a later period. An individually valid
conversion whose aggregate decimal value or cross-block integer accumulator is not representable is also
valuation-unavailable for this reward-only fallback. An empty fee collector needs no Oracle valuation and does not make
a window incomplete.

The settlement flow runs in Treasury BeginBlock immediately before that block's Distribution execution:

```text
stability_tax_collector -> fee_collector: validator_tax coins
stability_tax_collector -> oracle:       oracle_tax coins
treasury_subsidy_pool   -> fee_collector: validator_paid anoah
treasury_subsidy_pool   -> oracle:        oracle_paid anoah
```

Organic funding above a target remains with its recipient and never causes a negative top-up or refund. The subsidy-pool
balance is already part of total supply, so top-up transfers change circulation but not total supply.

Until Phase 2 removes `x/mint`, the existing mint provision reaches `fee_collector` and is counted in `G` together with
gas because the collector does not label sources. This preserves the current boundary during Phase 1; after Phase 2, `G`
is organic transaction-fee value only.

### 9.3 Cosmos distribution genesis

Cosmos SDK v0.54.3 generates distribution genesis with `community_tax` set to 2%. Ark does not override that SDK default
in application code. The canonical Ark launch genesis must explicitly set it to zero; otherwise 2% of gas fees and
validator top-ups is deliberately diverted from validators. Generic `arkd init` output is a development scaffold until
the canonical launch-genesis configuration is applied.

Even with `community_tax = 0`, distribution can retain rounding, non-voter, or zero-previous-power residuals in its
community-pool accounting and may forward whole coins to `x/protocolpool`. This redesign does not fork distribution or
remove protocolpool. It only ensures Treasury does not deliberately fund a community pool and the configured community
skim is zero. Treat complete removal or redirection of SDK residuals as a separate policy decision.

### 9.4 Oracle reward distribution

Oracle must distribute every positive denomination in its module account, not only `anoah`:

1. Read all Oracle balances.
2. For each denomination, calculate the existing reward-window share.
3. Allocate that amount across validator reward weights.
4. Truncate each validator's per-denom allocation deterministically.
5. Call Distribution's existing multi-denom allocation API.
6. Transfer exactly the summed integer allocations from Oracle to Distribution.
7. Leave rounding dust and skipped-validator shares in Oracle.

The Oracle reward and reward-distribution windows remain meaningful smoothing parameters and are not removed by the
Treasury rewrite.

### 9.5 Required lifecycle order

```text
PreBlock:
  app-owned Oracle pipeline applies the block's accepted price snapshot

BeginBlock:
  treasury       // refresh caps; observe prior fees; settle the active funding window when due
  distribution   // distribute prior fees plus any settlement-time validator allocation/top-up
  protocolpool and remaining SDK begin blockers

Transaction ante:
  deduct complete declared fee
  after successful signature/sequence ante, collect exact tax in stability_tax_collector

EndBlock:
  market         // recover virtual-pool imbalance
  oracle         // settle reward period when due
```

Tax collected in block `h` remains in `stability_tax_collector` until the current Treasury window settles. Gas fees
remain in `fee_collector` only until Distribution BeginBlock `h+1`; Treasury first records their eligible value, so it
does not need to retain the coins. At a settlement boundary, target-aware validator tax and subsidy allocations join the
current `fee_collector` balance immediately before Distribution, while Oracle allocations move directly to Oracle. The
tax collector's Bank balance is the exact multi-block coin accumulator; Treasury stores only aggregate targets, eligible
validator-fee value, observed-block count, and valuation completeness.

Keep the lifecycle ownership explicit in code: `UpdateRewardFunding` only observes the completed block, persists the
updated aggregate, and returns it. `BeginBlocker` owns the height-one gate, completed-window check, call to
`SettleRewardFunding`, and reset to the canonical empty state. Settlement helpers never clear the window themselves.

## 10. Target Treasury protocol surface

Phase 1 is the only planned Treasury protobuf-changing phase. Every rewritten Treasury proto uses compact launch-only
field numbering; do not retain deprecated fields or compatibility wrapper messages.

### 10.1 `Params` and `MonetaryPolicy`

`Params` stores only governance-owned settings:

```text
1 reference_tax_cap                cosmos.base.v1beta1.Coin, launch denom asdr
2 reward_funding_window            uint64
```

`MonetaryPolicy` is independent persisted state and is the complete reversible lever set:

```text
1 stability_tax_rate               cosmos.Dec / math.LegacyDec
2 validator_block_reward_target    cosmos.Int / math.Int, implicit base-unit NOAH
3 oracle_block_reward_target       cosmos.Int / math.Int, implicit base-unit NOAH
4 redemption_buffer_target_ratio   cosmos.Dec / math.LegacyDec
5 strategic_reserve_target_ratio   cosmos.Dec / math.LegacyDec
6 insurance_target_ratio           cosmos.Dec / math.LegacyDec
```

Remove the unused `PolicyConstraints` message.

Validation:

- Every Monetary Policy `LegacyDec` and `math.Int` must be set and representable.
- Rate/share/ratio fields must be in `[0, 1]`.
- The three target ratios are independent stock targets; their sum may exceed one. The allocation waterfall, not their
  sum, determines how scarce expansion principal is routed.
- `Params.reference_tax_cap` must be a nonnegative Coin with a canonical lowercase Ark-native base denomination, and
  `reward_funding_window` must be positive. Each policy reward target must be set and nonnegative.
- Reward-target/window compatibility is not precomputed during Params, Monetary Policy, or genesis validation. The
  active reward-funding state uses checked addition for each observation and fails the BeginBlock transition if an
  accumulated target is unrepresentable.
- Keeper-level genesis verifies the reference-cap denomination belongs to Oracle's configured native-stable set.
  `MsgUpdateParams` performs the same cross-module check while rebuilding caps when the reference Coin changes; pure
  Params validation does not pretend it can validate cross-module state. Both policy-update messages validate only the
  candidate policy, plus the mandate bounds on the committee path.

Safe defaults:

- Monetary Policy tax rate, reward targets, and fund target ratios default to zero until launch economics are
  configured.
- The Params reference cap defaults to zero `asdr`, producing explicit uncapped entries without genesis Oracle prices;
  the zero Monetary Policy tax rate remains the inert default.
- Reward-funding window defaults to one chain week.
- Production genesis must explicitly set all nonzero launch values.

### 10.2 Persistent Treasury state

Keep only:

```text
prefix 0: Params
prefix 1: TaxCaps map[string]math.Int
prefix 2: ClaimsMandate
prefix 3: InsuranceReserved
prefix 4: Claims map[uint64]Claim
prefix 5: RewardFundingState
prefix 6: MonetaryMandate
prefix 7: MonetaryPolicy
prefix 8: ClaimsAllowanceUsed
prefix 9: NextClaimID
```

`MonetaryPolicy` is the single stored source for the six reversible economic levers. It is read directly by tax,
reward-funding, fund-target, and expansion-routing paths; those values are not mirrored in Params or the mandate.

`MonetaryMandate` stores the current committee address, chain-derived `uint64` term, half-open activation and expiry
heights, and complete minimum/maximum bounds for the reversible monetary-policy fields. Its zero-bounded,
empty-committee form is the canonical disabled state. Replacement derives `current.Term + 1` with the wrap behavior
specified in Section 10.5.

`ClaimsMandate` stores the chain-derived monotonic term, current exact committee, half-open activation and expiry
heights, and the fixed gross committee claim limit; the shared cancellation period is the
`claim_cancellation_period_blocks` Treasury parameter. Its empty-committee form is the
canonical disabled state and retains the latest term. `ClaimsAllowanceUsed` stores the gross amount of committee-origin
claims submitted in the current term across every status; mandate replacement resets it, while cancellation and payment
do not. `InsuranceReserved` stores the aggregate amount encumbered by pending claims across all terms. Mandate edits
never rewrite that reservation or any existing claim.

Each `Claim` stores:

```text
claim_id                       // keeper-assigned globally monotonic uint64
submitter                      // committee or governance authority
origin                         // committee | governance
mandate_term                   // committee appointment; zero for governance
incident_reference
recipient
amount                         // sdk.Coin; anoah-only at launch
evidence_reference
status                         // pending | paid | cancelled
submitted_height
executable_height
finalized_height
finalized_by
cancellation_reason
cancellation_reference
```

`RewardFundingState` stores one remaining-observation countdown, accumulated validator and Oracle targets,
contemporaneously valued eligible validator fees, and a valuation-completeness flag. It stores neither tax coins nor
fund balances. An empty state is canonical zero accounting with `blocks_remaining = 0` and complete valuation. An active
partial state has `blocks_remaining > 0`. The final observation reaches zero and settles before the successful
BeginBlock commits, then the state resets atomically. `Params.reward_funding_window` configures the next countdown;
changing it never rewrites a positive active countdown.

Cancelled and paid records retain the original immutable submission. `origin`, `submitter`, and `finalized_by` expose
the authority decisions without a cancellation-specific override flag.

Do not add a strategic Reserve transfer collection. `x/gov` already stores each proposal's unique ID, metadata, exact
message, status, and failure reason; the passed proposal is the authorisation record. Unlike an Insurance claim, a
Reserve-to-Buffer commitment has no external claim identity whose duplicate payment Treasury must prevent. A second
passed proposal is a second explicit authorisation, regardless of any caller-chosen label.

No old store layout is preserved. Remove:

- `TaxRate` item; the rate lives in `MonetaryPolicy`.
- `RewardWeight`.
- `EpochTaxProceeds`.
- `EpochInitialIssuance`.
- `EpochStates` and all unbounded indicator history.
- Stored fund balances or targets.
- Revenue-total counters that do not drive policy.

Bank balances are the source of truth for the subsidy pool, Redemption Buffer, strategic Reserve, and Insurance. Every
launch balance automatically reflects accepted `anoah` deposits. No deposit ledger, receipt, ownership record, or refill
state is stored. Claims state records authorization and encumbrance rather than duplicating custody. Fund targets,
Reserve balance, and Insurance unencumbered balance are calculated live from Bank plus the Insurance reservation.
Tax-routing and ordinary bank-transfer events provide observation without one additional consensus write per
transaction; the Reserve commitment relies on its signed message and canonical Bank event and adds no history
collection or custom event.

### 10.3 Genesis

Rewrite `GenesisState` with compact launch-only fields:

```text
1 params
2 tax_caps
3 claims_mandate
4 claims_allowance_used
5 insurance_reserved
6 next_claim_id
7 claims
8 reward_funding
9 monetary_mandate
10 monetary_policy
```

Subsidy pool, Redemption Buffer, strategic Reserve, and Insurance balances live only in bank genesis. Every fund may
begin only with `anoah`. Redemption Buffer and strategic Reserve count their complete balances toward their targets;
Insurance subtracts any imported reservation to derive its unencumbered balance. Treasury InitGenesis never mints them.

Default genesis disables Claims and monetary delegation, stores the zero Monetary Policy and zero reference cap, derives
a complete explicit-zero uncapped map, and uses zero accounting, no claims, and the canonical empty reward-funding state.
Production genesis must supply the approved P4 policy, zero accounting, and no pending/completed claims unless an explicit
test or export/import case requires otherwise. InitGenesis validates the canonical committee address, role separation,
unique claims, claim-status transitions, reservation sum, and `insurance_reserved <= insurance_anoah_balance`.
Export/import preserves the mandate, Insurance reservation, immutable claim fields, executable heights, finalizers, and
statuses.

Reserve-to-Buffer history requires no Treasury genesis field. Bank genesis/export preserves the resulting account
balances, while governance genesis/export preserves the authorising proposals and outcomes.

Run Bank InitGenesis before Treasury, then have Treasury InitGenesis inspect all four fund-account balances and reject
genesis if any contains a non-NOAH coin. This closes the one path that does not pass through the runtime bank send
restriction.

Keeper-level InitGenesis must validate the Params reference-cap denomination against Oracle's configured native-stable
set and establish a complete cap set regardless of the Monetary Policy tax rate. It verifies that a supplied set covers
every configured native stable and exactly matches the effective reference Coin, or fully derives it from valid genesis
Oracle prices. A zero reference cap instead derives a complete explicit-zero uncapped set without prices. Supplied
derived caps must all be zero when the reference cap is zero and positive when the reference cap is positive.

### 10.4 Queries

The launch Query service exposes only:

- `Params`.
- `MonetaryPolicy` returning the current six reversible lever values.
- `MonetaryMandate` returning the governed appointment and whether it is active at the current height.
- `TaxCap(denom)`.
- `TaxCaps`.
- `ComputeTax(messages)` using repeated `google.protobuf.Any` annotated as SDK messages.
- `FundStatus` returning:
  - `nominal_liability_noah_equivalent` as an `sdk.DecCoin` explicitly denominated in `anoah`;
  - `subsidy_pool_balance` as the current subsidy-pool `anoah` `sdk.Coin`, including accepted deposits;
  - `redemption_buffer_balance` and `redemption_buffer_target` as `sdk.Coin` values in `anoah`;
  - `strategic_reserve_balance` and `strategic_reserve_target` as separate `sdk.Coin` values;
  - `insurance_balance`, `insurance_reserved`, `insurance_unencumbered_balance`, and `insurance_target` as separate
    `sdk.Coin` values.
- `RewardFunding` returning the stored aggregate state, including `blocks_remaining`, without requiring Oracle prices or
  fund valuation. The separate `Params` query exposes the configured length that the next empty state will use.
- `ClaimsMandate` returning the current committee appointment, Insurance reservation, allowance used/remaining, and
  whether the appointment is active at the current height; the shared cancellation period is exposed by the `Params`
  query.
- `Claim(claim_id)`. Its canonical REST binding is `GET /ark/treasury/v1/claims/{claim_id}`.
- `Claims` as a paginated audit and operations view of claim records.

Do not add a Treasury Reserve-transfer history query. `FundStatus` and Bank queries expose current balances; the
authorising governance proposal, signed Treasury message, and standard Bank transfer event expose the action history.

At launch, the Buffer and Reserve balances count in full toward their targets. `FundStatus` must additionally return
`insurance_balance`, `insurance_reserved`, `insurance_unencumbered_balance`, and `insurance_target` as separate
`sdk.Coin` values, with the unencumbered balance equal to balance minus reservations. The standard paginated Bank query
remains the canonical custody view. `FundStatus` must not return a combined backing or health percentage. The launch
query exposes only nominal liability; a later risk-exposure target model must add explicit fund-specific exposure fields
rather than overloading that value. Future external-asset queries must report gross custody, recognised value,
recognition haircut, liquid value, applied encumbrance, and zero-credit reasons separately rather than folding them into
one opaque balance.

Remove:

- `TaxRate` as a separate state query; read it through `MonetaryPolicy`.
- `RewardWeight`.
- `SeigniorageProceeds`.
- `TaxProceeds`.
- `Indicators`.

`ComputeTax` should use a POST HTTP binding with body `*`. Do not add a misleading AutoCLI command for an arbitrary
`Any` list; clients can use gRPC or a later purpose-built tx-decoding command. Its query status distinguishes malformed
messages (`InvalidArgument`), configured denominations lacking a cap (`FailedPrecondition`), unrepresentable aggregate
tax (`OutOfRange`), and unexpected state failures (`Internal`).

All Treasury liability and target-exposure query/event values are direct NOAH equivalents explicitly denominated in
`anoah`. Clients must infer neither value from `Params.reference_tax_cap.denom` nor from Market's current
`BasePool.Denom`. Construct each `sdk.DecCoin` from the complete `LegacyDec` aggregate; never truncate the displayed
aggregate to integer `anoah`.

Do not store or return a second authoritative subsidy-duration value. Clients derive the current estimate from the live
subsidy pool balance and reward targets, using zero organic funding for a conservative estimate; the balance is
authoritative and may change through an ordinary deposit at any time.

### 10.5 Messages

The launch Msg service exposes:

Governance-signed messages:

- `MsgUpdateParams`.
- `MsgSetMonetaryMandate`.
- `MsgUpdateMonetaryPolicy`.
- `MsgSetClaimsMandate`.
- `MsgSubmitClaim`.
- `MsgCancelClaim`.
- `MsgTransferReserveToBuffer`.

Committee-signed messages (term-checked, one exact appointed committee each):

- `MsgCommitteeUpdateMonetaryPolicy`.
- `MsgCommitteeSubmitClaim`.
- `MsgCommitteeCancelClaim`.

Permissionless messages:

- `MsgExecuteClaim`.

Every action a committee may take is its own message type, so the complete committee surface is enumerable from the
proto service alone rather than by reading handler branches. This matches `x/asset`'s emergency mandate and the future
Reserve mandate in Section 20.2. The governing principle: **roles with disjoint powers or different execution semantics
get separate messages per role; one message serves several roles only when they are the same action under the same
rules.** In Treasury no role passes that second test — governance and each committee differ in staleness guard,
allowance metering, or cancellable set — so every role gets its own message. Each handler is one positive authorization
assertion followed by a shared keeper core (`applyMonetaryPolicy`, `submitClaim`, `cancelClaim`), so effect logic
cannot drift between roles while authorization stays legible per message.

Two consequences of the split are deliberate. Governance messages carry no `expected_term`, because no governance
authorization depends on a mandate: the cancellation period `MsgSubmitClaim` once pinned through its term now comes
from the governance-owned Treasury params. And a committee message
naming the wrong signer is rejected before any term or window reasoning, because identity is checked in the wrapper;
a disabled mandate therefore reports a committee mismatch rather than an inactive mandate.

Do not add `MsgFundSubsidyPool`, `MsgFundRedemptionBuffer`, `MsgFundReserve`, or `MsgFundInsurance`. Deposits use
ordinary bank `MsgSend`/`MsgMultiSend` paths plus Treasury's recipient-specific restriction, so they need no Treasury
message, signer rule, receipt, or persistent record.

`MsgSetMonetaryMandate` is governance-signed and replaces the complete committee appointment. Treasury derives the next
term as `current.Term + 1`. The reviewed implementation has no separate maximum-term exhaustion check: a nonempty
appointment whose increment wraps to zero is rejected by configured-mandate validation, while an empty disablement can
store the wrapped zero term. An empty committee otherwise disables the mandate; a configured message supplies the exact
committee address, half-open activation/expiry heights, and complete minimum/maximum policy bounds.

`MsgUpdateParams` is governance-only and replaces the complete governance-owned settings, including the reference-cap
Coin and `claim_cancellation_period_blocks`, the shared claim veto window for both origins. It cannot change a tax
rate, reward target, or fund target ratio.

`MsgCommitteeUpdateMonetaryPolicy` contains one complete candidate policy signed by the exact stored committee. It is
accepted only during the active term and window, with the current expected term and every field inside the mandate
bounds. `MsgUpdateMonetaryPolicy` is the governance form of the same effect: it applies any structurally valid
candidate, overriding those bounds without depending on the mandate at all. Neither path may change the
governance-owned reference cap in Params. Because the protobuf-derived Amino name of the committee message exceeds the
SDK type-name limit, it registers under the compact identifier `ark/x/treasury/MsgCommitteeUpdatePolicy`, following the
`MsgTransferToBuffer` precedent; its protobuf message name, RPC name, signer, and semantics are unchanged.

`MsgSetClaimsMandate` is governance-signed and replaces or disables the complete committee appointment. Treasury derives
a new monotonically increasing term. An empty committee disables the mandate; otherwise the message supplies the exact
committee, half-open activation/expiry heights, and a positive fixed `anoah` committee claim limit. The Claims
committee, monetary-policy committee, and Treasury authority must be distinct addresses. The current params
cancellation period must not exceed `expiry_height - activation_height`; equality permits a claim at the activation
boundary to become executable exactly at expiry. A successful replacement resets Claims allowance used to zero without
changing the Insurance reservation.

`MsgCommitteeSubmitClaim` is signed by the exact committee and `MsgSubmitClaim` by the governance authority. Both
validate the same positive `anoah` amount, recipient, references, and live Insurance coverage, and both derive the
executable height from the params cancellation period. Only the committee message carries `expected_term`: it is
accepted only during the active window, its executable height must not pass mandate expiry, and its claim stores the
mandate term, while a governance submission never reads the mandate and stores term zero. The recipient must be
neither the Insurance account itself nor any address Bank currently blocks from receiving module-account sends.
Treasury assigns the next globally monotonic `uint64` claim ID, returns it in the response, and reserves the amount
without moving coins. The message type decides the immutable stored origin: committee submissions must fit within and
permanently consume the current term allowance; governance submissions do not consume it. `MsgExecuteClaim` is permissionless at or after
the stored executable height and pays only the immutable stored recipient and amount, independent of later mandate
replacement, disablement, or expiry. Genesis persists the next claim ID and enforces the same Bank-receivability
invariant for pending claims; an app upgrade that changes the blocked set must explicitly migrate any affected pending
claim.

`MsgCancelClaim` is signed by the governance authority and `MsgCommitteeCancelClaim` by the exact current committee.
Both may cancel only before the stored executable height. Governance may cancel any pending claim without depending on
the current Claims Mandate, so its message carries no expected term. The committee must be in the current active
appointment, supply its exact expected term, and may cancel only a claim whose immutable origin is not governance.
Cancellation releases the reservation and records the signer in `finalized_by`; no separate governance-cancellation
flag is stored.

`MsgTransferReserveToBuffer` contains:

```text
authority
amount: sdk.Coin
minimum_reserve_balance: sdk.Coin
```

Annotate `authority` as the signer and Cosmos address scalar. Generate both Coin fields as non-nullable `sdk.Coin`
values with `amino.dont_omitempty = true`, following the launch schema's standard value-field annotations.

It validates the general Treasury authority, which is `x/gov` at launch. Both Coins must use `anoah`; `amount` must be
positive and `minimum_reserve_balance` nonnegative. The handler enforces sufficient Reserve balance and
`post_transfer_reserve_balance >= minimum_reserve_balance`, then performs the fixed module-to-module transfer described
in Section 6.4. It must not read Oracle rates, calculate targets, accept an arbitrary recipient, convert an asset, call
Market, or create transfer-history state. The message has zero stability-tax principal as a Treasury governance action,
and its internal Bank movement is not independently taxed.

Do not add `MsgWithdrawReserve`, a Buffer-to-Reserve message, a generic deployment recipient, or any other Reserve
action at launch. External-asset acquisition, liquidity positions, grants, claims, rewards, and direct redeemer funding
remain outside this message.

### 10.6 Module config

Keep only field 1 `authority`, defaulting to the governance module account. The Claims committee address lives in
governance-controlled Claims Mandate state, and the monetary-policy committee lives in its governed mandate state, not
immutable app module configuration. The Reserve-to-Buffer message uses the general authority and adds no
`reserve_authority` configuration. Do not reserve or retain `claims_authority` or `reward_collector_name` in this
fresh-genesis schema.

### 10.7 Events

Replace legacy policy/seigniorage events with:

- `ark.treasury.v1.EventTaxCapsUpdated` with the complete typed `tax_caps` set, and
  `ark.treasury.v1.EventTaxCapsUpdateSkipped` with an `EventSkipReason`; detailed errors remain in logs.
- `ark.treasury.v1.EventBlockRewardsToppedUp` with the `anoah` denomination and separate validator/Oracle target,
  organic-funding, and exact-payment amounts. Shortfall is derived from target and organic funding, while the remaining
  subsidy balance is queryable Bank state.
- `ark.treasury.v1.EventBlockRewardTopUpSkipped` with an `EventSkipReason`; detailed errors remain in logs, and collected
  stability tax is sent entirely to Oracle.
- `ark.treasury.v1.EventExpansionAllocated` with the `anoah` denomination, separate Buffer, strategic Reserve, and
  Insurance credits, separate spread/dust and overflow burns, and the target-valuation-complete flag. Eligible
  principal, gross offer, total burn, and conservative fallback are derived from these fields; Market's
  conversion-settlement event owns the stable output.
- `ark.treasury.v1.EventRedemptionBufferDrawn` with the `anoah` denomination, Buffer payment, and
  aggregate-valuation-complete flag. Market's conversion-settlement event owns the stable offer, quoted NOAH output,
  and residual mint; liability valuations remain execution-local.
- `ark.treasury.v1.EventClaimSubmitted` with the keeper-assigned claim ID.
- `ark.treasury.v1.EventClaimPaid` with claim ID, recipient, amount denomination, and amount.

Policy updates, claim cancellations, and governed Reserve transfers rely on their signed messages and stored state or
canonical Bank events. `EventClaimSubmitted` records the generated ID that is absent from the signed submission;
stability-tax collection and allocation rely on their fixed module-to-module Bank movements. `EventClaimPaid` remains
as the domain link between a claim ID and its Bank payment.

Do not add a Treasury-specific deposit event for any fund. The canonical bank transfer event already records sender,
recipient, and coins; standard Bank queries report custody, while `FundStatus` reports target comparison balances. Every
accepted launch deposit is `anoah` and therefore has no stability-tax principal.

On incomplete aggregate valuation, `EventRedemptionBufferDrawn` records Buffer payment zero and sets
`aggregate_valuation_complete = false`, distinguishing the conservative fallback from a complete zero draw without
exporting internal liability valuations.

Likewise, an incomplete expansion target valuation records the complete Buffer credit, spread/dust burn, zero
Reserve/Insurance/overflow amounts, and `target_valuation_complete = false`. Market owns the gross offer, stable output,
and final burn; Treasury's event remains authoritative for allocation and the conservative fallback branch.

## 11. Phase workflow

Every phase uses the same protocol:

### Before editing

1. Read this document and confirm the phase is approved.
2. Run `git status --short` and inspect path-scoped diffs for every file the phase may touch.
3. Treat existing changes as user-owned; merge with them rather than replacing them.
4. Record pre-existing test failures separately from phase regressions.
5. Re-check `go.mod` for the exact SDK version before SDK-specific edits.

### During implementation

- Stay within the approved file list and semantics.
- Explain and receive approval for any newly discovered protobuf change before editing it.
- Keep the repository compiling at the completed phase boundary.
- Add tests with the implementation, not in a later phase.
- Do not begin opportunistic cleanup outside the phase.

### Review gate

At the end of each phase, provide:

1. A concise policy/behavior summary.
2. The exact path-scoped diff.
3. Tests run and exact failures or skips.
4. Remaining risks and deviations from this document.
5. A request for explicit approval before the next phase.

Update the phase-status table in this document only after approval.

## 12. Phase 0: Baseline and policy approval

Status: **Approved and complete**

No implementation files change in this phase. The output is an approved version of this document.

### Required baseline checks

```sh
GOCACHE=/private/tmp/ark-gocache go test ./x/treasury/...
GOCACHE=/private/tmp/ark-gocache go test ./x/market/...
GOCACHE=/private/tmp/ark-gocache go test ./x/oracle/...
GOCACHE=/private/tmp/ark-gocache go test ./app/...
```

Failures caused by the current dirty refactor are baseline findings, not automatically redesign regressions.

Recorded 2026-07-15 baseline: all four suites passed. The app suite initially encountered the local sandbox's loopback
bind restriction and passed when rerun with loopback access.

### Approval record

All core launch-policy decisions D1 through D33 are confirmed. This includes:

- Fresh-genesis replacement with no compatibility or migration work.
- Zero routine issuance, removal of Mint, and Market-only conversion minting.
- Use coverage-based Redemption Buffer funding with continuous residual minting instead of whole-Reserve-first settlement.
- Route expansion principal Buffer → strategic Reserve → Insurance → burn.
- Keep gross expansion custody in Market, let Treasury derive and execute the entire per-conversion waterfall from the
  gross offer, final integer output, and quote snapshot, then have Market consume a successful authoritative result and
  own burn/mint/payout.
- Use the zero-Buffer/full-mint fallback when another nonzero-supply native stable lacks a fresh direct NOAH valuation.
- Treat public fund ratios as separate capacity measures, never as a combined solvency or backing claim.
- Defer external-asset custody, target recognition, valuation/haircuts, and every strategic Reserve action other than
  the launch governance-only `anoah` commitment to the shared Redemption Buffer. Introduce them with the first approved
  external asset, not as dormant launch authority. Ark-issued stablecoins remain liabilities and always receive zero
  target credit.
- Let governance invoke the fixed Reserve-to-Buffer action through Treasury's general authority with a positive amount
  and per-proposal minimum remaining Reserve balance. Keep it independent of Oracle/target completeness, irreversible,
  untaxed, uncallable by Market, and audited through governance state plus Treasury and Bank events rather than a
  duplicate Treasury history collection.
- Use the balance-constrained subsidy pool, Oracle-first tax lane, proportional pool shortfall, and zero launch
  `community_tax` defined in Section 9. Seed the pool at genesis, allow irreversible permissionless `anoah` deposits,
  and add no automatic refill or conversion mechanism.
- Allow irreversible permissionless `anoah` deposits to all four Treasury fund accounts and reject mixed or non-NOAH
  deposits atomically. Deposits create no ownership, withdrawal, coverage, priority, or deployment rights.
- Base required Reserve and Insurance capital on each fund's covered risk exposure rather than its holdings. When
  external assets are introduced, reduce target gaps only by explicit fund-specific risk-adjusted recognition, preserve
  a separate liquid-capital requirement, apply cross-fund concentration caps, and never let missing recognition trigger
  automatic action.
- Retain governance as policy authority. If fast Reserve execution is later needed, governance creates a typed, bounded,
  expiring mandate for a threshold multisig and a separate pause-only guardian; neither receives Treasury's general
  parameter authority or a generic bank-send power.
- Put launch Insurance claims under a governance-owned Claims Mandate. The exact committee and governance submit under
  the same active appointment and expected-term rules. Committee submissions consume the fixed gross term allowance;
  governance submissions do not. Governance may cancel during the same period regardless of the current mandate; the
  current active committee may cancel only non-governance-submitted claims using the current term. Submissions become
  encumbered pending claims before payment, and settled payments are irreversible.
- Keep the Claims committee and future Reserve executor accounts distinct even if the human roster overlaps. Neither
  role receives custody, general Treasury authority, generic Bank send, cross-fund access, or authority whose allowance
  automatically resets when a fund is refilled.
- Apply one tax cap per taxable input/denomination pair across every enabled user-facing bank, Market-send, Wasm, and
  IBC transfer boundary, without taxing arbitrary internal bank movements.
- Store the canonical tax cap as a governance-changeable Coin launching in `asdr`, independent from Treasury's direct
  NOAH-equivalent liability valuation and Market's changeable pool unit.
- Make `BasePool` a denomination-bearing `sdk.DecCoin` launching in `asdr`, and support one live pool-unit transition
  through the existing `MsgUpdateParams` without adding a transition object, message, or parallel pool.
- Treat submitted `BasePool.Amount` as a non-binding audit expectation on a denomination change; retain it in the
  transaction, derive and store the amount from one fresh deterministic conversion, emit the old and applied pool state,
  and atomically rescale `ArkPoolDelta`.
- Price stable-to-stable conversion directly and keep Treasury fund calculations independent of Market's pool unit.
- Keep `asdr` as a normal supported offer and output after the basket becomes the flagship and Market pool unit; the
  pool-denomination change does not alter stablecoin mint eligibility.

P1 numerical launch configuration and P4 Claims Mandate operating values remain pending before launch. P2 virtual-pool
capacity analysis and P3's deterministic pool-transition audit policy are complete. None reopens the confirmed
architecture or blocks Phase 1 implementation.

Do not begin Phase 1 until the baseline test results above are recorded and the user explicitly approves implementation.

## 13. Phase 1: Treasury core and account model

Status: **Reviewed**

This is the only planned Treasury proto phase. It replaces the controller-era state with the final minimal surface and
implements Treasury-owned operations without yet changing Market settlement or ante handling.

This is a clean schema replacement, not a migration. Delete superseded fields and messages outright, use the compact
numbering in Section 10, and regenerate all gogo and Pulsar outputs from the new definitions. Do not create frozen
legacy proto packages or conversion helpers.

### 13.1 Protobuf files

Modify:

- `proto/ark/treasury/v1/treasury.proto`.
- `proto/ark/treasury/v1/genesis.proto`.
- `proto/ark/treasury/v1/query.proto`.
- `proto/ark/treasury/v1/tx.proto`.
- `proto/ark/treasury/module/v1/module.proto`.

Regenerate:

- `x/treasury/types/treasury.pb.go`.
- `x/treasury/types/genesis.pb.go`.
- `x/treasury/types/query.pb.go`.
- `x/treasury/types/query.pb.gw.go`.
- `x/treasury/types/tx.pb.go`.
- `api/ark/treasury/v1/treasury.pulsar.go`.
- `api/ark/treasury/v1/genesis.pulsar.go`.
- `api/ark/treasury/v1/query.pulsar.go`.
- `api/ark/treasury/v1/query_grpc.pb.go`.
- `api/ark/treasury/v1/tx.pulsar.go`.
- `api/ark/treasury/v1/tx_grpc.pb.go`.
- `api/ark/treasury/module/v1/module.pulsar.go`.

No Market or Oracle proto changes belong in this phase.

Every committee-, executor-, or ordinary-account-signed Treasury message must declare its exact `cosmos.msg.v1.signer`,
`amino.name`, and address scalar; be registered through `legacy.RegisterAminoMsg` and as an `sdk.Msg`; and have
deterministic Amino JSON for every nested field. Tests must construct and authenticate an actual Legacy Amino
threshold-multisig claim approval rather than proving only direct handler invocation.

### 13.2 Treasury types

Rewrite:

- `x/treasury/types/params.go` and tests.
- `x/treasury/types/genesis.go` and tests.
- `x/treasury/types/keys.go`.
- `x/treasury/types/events.go`.
- `x/treasury/types/expected_keepers.go`.
- `x/treasury/types/codec.go`.

Add:

- `x/treasury/types/allocation.go` and tests for the handwritten, non-protobuf `ExpansionAllocation` and `BufferDraw`
  execution results and derived total burn. These types contain amounts and audit classification only; they store no
  account names, addresses, rates, mutable maps, or keeper references. Treasury constructs the result through checked,
  bounded arithmetic and returns an error instead of exposing a separate result-validation API.

Add only if stable module error codes are needed:

- `x/treasury/types/errors.go`.

Delete:

- `x/treasury/types/constraints.go`.
- `x/treasury/types/constraints_test.go`.

Expected keeper dependencies after the rewrite:

- Account keeper: module-account lookup.
- Bank keeper: supply reads, denom-specific `GetBalance` for launch target logic, `GetAllBalances` for all four
  NOAH-only genesis checks, and module/account transfers.
- Oracle keeper: Tobin-tax denom registry and execution-local rate snapshots.

Remove staking and protocol-pool interfaces. Do not include `MintCoins` or `BurnCoins` in Treasury's BankKeeper
interface.

### 13.3 Treasury keeper

Rewrite:

- `x/treasury/keeper/keeper.go`.
- `x/treasury/keeper/genesis.go` and tests.
- `x/treasury/keeper/abci.go` and tests.
- `x/treasury/keeper/grpc_query.go` and tests.
- `x/treasury/keeper/msg_server.go` and tests.

Add:

- `x/treasury/keeper/tax.go` and tests.
- `x/treasury/keeper/funds.go` and tests.
- `x/treasury/keeper/liability.go` and tests for the lazy block-local liability snapshot and exact supply-delta updates.
- `x/treasury/keeper/reward_funding.go` and tests.
- `x/treasury/keeper/claims.go` and claims coverage in the message-server tests.

Claims Mandate tests must cover the disabled default, governance-only replacement and disablement, monotonic term
advancement and overflow rejection, a canonical committee distinct from every other Treasury authority, an appointment
span no shorter than the params cancellation period and a positive fixed gross claim limit, exact half-open
activation/expiry
behavior, committee rotation, non-restoring cancellation/payment usage, reset only on a new term, and exact address
matching that is not silently replaced by the SDK consensus-authority override.

Claim-submission tests must cover an actual Legacy Amino threshold multisig; wrong signer, insufficient signatures,
nonexistent signer account, account sequence and fee behavior; expected-term mismatch for the committee;
role disjointness in both directions, proving neither role can act through the other's message even while both are
live; not-yet-active and expired mandate for the committee alongside governance submission that ignores the window
and the disabled sentinel; keeper-assigned globally monotonic claim IDs, sequence exhaustion, bounded
incident/evidence references, and numeric REST lookup; recipient validation; rejection of Insurance and Bank-blocked
recipients before any accounting write; positive `anoah`-only amounts; pending-reservation and held-balance boundaries;
checked executable-height addition; rejection when the cancellation period would cross mandate expiry; atomic rollback;
no Bank send on submission; and pending-genesis recipient validation.

Execution tests must prove that any fee-paying caller can execute only the immutable stored claim at or after its
executable height, including after mandate replacement, disablement, or expiry; cannot alter recipient, amount,
reference, origin, or mandate term; cannot execute twice; and rolls back claim state, reservation, balance, and events
on Bank failure. Cancellation tests must prove governance can cancel any pending claim before the executable height
without depending on the current mandate, while the committee must hold the current active appointment and exact term
and cannot cancel a governance-origin claim. Both paths release the exact reservation and move no coin.

Target tests must prove `insurance_unencumbered_balance = insurance_balance - insurance_reserved`: submission opens the
gap without moving coins; execution decreases balance and reservation equally without opening a second gap; and
cancellation releases the reservation and closes the corresponding gap. None changes required exposure or the target.

Reserve-to-Buffer message and fund tests must cover general-authority validation; positive exact `anoah`; rejection of
zero, malformed, or wrong-denomination Coins; insufficient Reserve balance; the per-proposal minimum remaining balance;
partial and complete Reserve commitment; Buffer above target; Reserve below target or at zero when explicitly allowed;
Bank-send rollback; exact pre/post Bank balances; and no custom Treasury event. The keeper must use fixed module names
and expose no recipient or reverse path. A successful transfer changes no supply, liability, target, Params, pool state,
quote, tax balance, subsidy pool, or Insurance balance. Missing or stale Oracle rates must not block the transfer.
Subsequent target-gap
calculation must observe the new bank balances without a deployment ledger, and future expansion must naturally route
through any resulting gaps.

An app-level governance test must execute the message through a passed proposal, verify the proposal's cached atomic
rollback on handler failure, and prove that the commitment occurs after current-block transaction settlements and is
visible to the next complete-valuation redemption. Governance proposal export/import must retain the exact message and
outcome without a Treasury-owned transfer record.

Keep tax extraction and calculation in one Treasury-owned implementation. Ante, query, Wasm, and IBC adapters may use
lower-level pure primitives from that implementation, but none may copy the formula or own separate policy state. The
calculator must accept each execution-generated transfer as a separate taxable input so execution boundaries preserve per-input caps
without double-assessing any transfer already covered by ante.

Keep target gaps, `RouteExpansion`, and coverage-based Buffer funding in `funds.go`. Keep the shared aggregate
valuation and transient snapshot mechanics in `liability.go`. Keep reward-window observation and settlement in
`reward_funding.go`, Claims lifecycle handling in `claims.go`, and the governed Reserve-to-Buffer handler in
`msg_server.go`. Inject Treasury's module-scoped `store.TransientStoreService`; the cache is derived state with no
genesis field, export surface, or migration. Use the shared `RateSet.Convert` path and checked `LegacyDec`
aggregation consistently for nominal liability, target exposure, expansion-principal valuation, and redemption
coverage. Treasury fund logic must have no permanent `SDRBaseDenom` dependency.

`RouteExpansion` must validate positive `anoah` gross input and native-stable output, preserve every existing caller
quote rate, add only missing liability rates, derive eligible principal from the final integer output, reject output
value above the gross offer, count the pending output exactly once in post-trade exposure, and calculate the complete
Buffer → Reserve → Insurance → overflow waterfall from bounded remainders before performing only the positive fixed
Market-to-fund transfers. It must never accept caller-computed principal, target gaps, credits, burn amounts, sources,
or destinations; debit the Treasury subsidy-pool account; retain gross offer; or invoke Bank mint/burn. Its result keeps
spread/dust and overflow burn
distinct and contains no redundant stored total burn. An unrelated missing aggregate rate uses the Buffer-only fallback,
while inability to value `stable_output` itself fails before any transfer.

The first `RouteExpansion` or `DrawRedemptionBuffer` that needs aggregate liability in a block must scan the configured
stable supplies and capture missing rates, then write the NOAH-equivalent value to transient storage only when the
valuation is complete. An incomplete valuation is not cached and may be retried by a later settlement. A complete
snapshot must advance only through `RecordSupplyChange` after Market has successfully applied the corresponding burn and
mint. The update ignores NOAH supply changes, values every stable delta with the conversion quote's fixed rate values,
rejects underflow or unrepresentable arithmetic, and is included for stable-to-stable conversions. With no snapshot it
is a no-op. Direct queries must not read the transient cache.

Expansion tests must cover invalid offer/output denoms; nil, zero, negative, and unrepresentable inputs; exact and
inexact output valuation; eligible-principal flooring and rejection when output value exceeds the gross offer;
post-trade output counted exactly once; each target gap and waterfall boundary; overfunded funds; pending Insurance
reservations; separate spread/dust and overflow burns; complete conservation; unrelated-rate Buffer fallback; hard
failure when output valuation is unavailable; no
zero-value Bank sends; each fixed transfer failure; atomic rollback after an injected intermediate transfer; in-place
augmentation of missing aggregate rates without refetching or replacing caller quote denoms; and no Treasury mint,
burn, gross custody, pending allocation, or periodic settlement state.

The redemption calculator must compute from pre-trade supply with `LegacyDec` conversion and checked aggregate addition,
then perform the fixed-source Buffer transfer only after every bound is validated. Redemption and shared-valuation tests
must cover multiple stable denoms, order-independent liability sums, exact and inexact target ceiling, scale boundaries,
large representable values, overflow/unrepresentable values, zero and overfunded Buffer, final liability, conservative
incomplete-valuation fallback, split-versus-unsplit floor rounding, zero aggregate liability, redeemed liability
exceeding aggregate liability, quoted output exceeding redeemed liability, non-decreasing post-redemption coverage, and
zero draw without a bank-send call. For identical pre-trade balances and liabilities, changing
`redemption_buffer_target_ratio` must not change the coverage-based draw. An absent SDR rate must be irrelevant
when `asdr` supply is zero, while nonzero `asdr` supply still requires a fresh `asdr`/NOAH valuation like every other
outstanding liability.

Target-gap calculation must read each fund account's `anoah` Bank balance at launch and subtract `InsuranceReserved`
from Insurance only. Tests must prove that mixed or non-NOAH credits to every fund are rejected atomically and cannot
change a target gap, expansion routing, or coverage-based Buffer settlement. Buffer `anoah` deposits must be included
naturally in its live pre-redemption balance without any deposit ledger or special settlement branch.

Keep the fixed Reserve-to-Buffer balance movement directly in the authorised handler in `msg_server.go`. Do not add it
to Market's expected Treasury keeper interface. Read only the live Reserve `anoah` balance needed for validation; do not
read the Buffer balance, invoke the valuation helper, emit a custom event, or store pre/post balances as consensus state.

Phase 1 must deliver the complete final calculator and `ComputeTax` query described in Section 8, including bank send,
multi-send inputs, `MsgSwapSend`, `MsgSwap`, recursive authz, denomination filtering, independent per-input and
per-denomination caps, and malformed-message behavior. It must also atomically rebuild all derived caps when the
effective reference Coin changes; tests cover a successful `asdr`-to-another-denom switch, direct reference-cap
copying, missing/stale-rate rollback, invalid candidate denoms, and export/import. Phase 4 wires the reviewed calculator
into ante and every enabled Wasm/IBC execution boundary; it does not invent the tax semantics later.

Delete:

- `x/treasury/keeper/indicator.go` and tests.
- `x/treasury/keeper/policy.go` and tests.
- `x/treasury/keeper/seigniorage.go` and tests.

Treasury BeginBlock after this phase may:

- Refresh tax caps on the fixed schedule or denom-set mismatch.
- Evaluate zero/default reward targets without spending subsidy funds.

Treasury no longer has an EndBlocker.

### 13.4 Treasury module/support files

Modify:

- `x/treasury/module/depinject.go` to remove staking/protocolpool/reward-collector dependencies, keep only the general
  governance authority, and provide Treasury's bank `SendRestrictionFn` through depinject.
- `x/treasury/module/module.go` to implement `HasBeginBlocker` instead of `HasEndBlocker`, report consensus version 1,
  and register no state migrations.
- `x/treasury/module/autocli.go` to expose only meaningful queries/messages.
- `x/treasury/module/simulation.go` as required by the new message set.
- `x/treasury/simulation/genesis.go` and tests.
- `x/treasury/simulation/msg_factory.go`.
- `x/treasury/testutil/expected_keepers_mocks.go` by regeneration.

Do not simulate `MsgTransferReserveToBuffer` as an ordinary user-signed transaction: its launch signer is the governance
module account. Exercise it through governance integration tests, or omit it from weighted message simulation until the
simulator has a real proposal-execution path.

Likewise, do not simulate governance Claims Mandate or claim messages as ordinary user transactions or pretend a random
single key is the claims committee. Either build a real multisig-aware simulation path with bounded mandate state or
omit those weighted messages and cover them through keeper and app integration tests. Permissionless claim execution or
may be simulated only against a real pending claim at the correct height.

Add:

- `x/treasury/module/send_restriction.go` and tests: for all four Treasury fund recipients, accept only a positive
  `sdk.Coins` value consisting solely of `anoah` and reject every mixed or non-NOAH set; pass through every unrelated
  recipient. The restriction owns no state, conversion, or redirection.

Restriction tests must also compose a preceding address-rewriting restriction and prove Treasury applies the NOAH-only
rule to the rewritten final destination. Because Treasury is recipient-based, outbound reward top-ups and Insurance
claims remain unrestricted by their source account, while every inbound non-NOAH module transfer to any fund must fail.
The Reserve transfer passes the final-recipient rule because it sends only `anoah`.

The currently unwired `x/treasury/wasm` adapter contains a stub general query and references deleted state. Remove it in
this phase if a production call path is still absent. Reintroduce a real adapter when custom Wasm is wired; do not keep
a misleading half-wired surface.

### 13.5 Account wiring required for Phase 1 to compile and operate

Modify `app/app_config.go` narrowly:

- Register `treasury_subsidy_pool`, `treasury_redemption_buffer`, `treasury_strategic_reserve`, and
  `treasury_insurance`, all with no permissions. Do not register a module account named exactly `treasury`.
- Register blocked `stability_tax_collector` with no permissions; it is the active reward-funding-window staging
  account, not a Treasury fund.
- Remove Treasury's `Minter` permission.
- Leave the subsidy pool, Redemption Buffer, strategic Reserve, and Insurance accounts out of the explicit bank blocked
  list so ordinary deposits may reach them; enforce the fund-specific denomination rules through Treasury's send
  restriction.
- Keep every required infrastructure account blocked and explicitly include at least Market, Oracle,
  `stability_tax_collector`, and `fee_collector`; the override replaces rather than augments the SDK default list.
- Set Bank `RestrictionsOrder` explicitly with Treasury last. At launch Treasury is the only planned provider, so the
  list is exactly `[treasury]`.
- Preserve Bank-before-Treasury InitGenesis ordering so Treasury can reject non-NOAH genesis balances in all four fund
  accounts.
- Add Treasury before Distribution in BeginBlock order.
- Remove Treasury from EndBlock order.
- Keep Treasury's general authority at its default governance module address for the launch Reserve-to-Buffer action;
  add no Reserve operator, Claims committee, Reserve guardian, or hot-key address to app module configuration. Claims
  roles come from Treasury genesis/governance state.
- Ensure the launch Claims committee has a BaseAccount and an explicit non-Treasury way to pay transaction fees, either
  its own genesis `anoah` balance or a separately approved narrow fee grant. Insurance funds never pay role-account gas
  automatically.

Account-wiring tests must prove all four fund accounts exist, have no permissions, accept only the intended inbound
deposits, and are distinct from one another and from the Treasury subsidy-pool account. Ordinary `MsgSend` and
`MsgMultiSend` tests must prove:

- Every fund accepts positive `anoah` and rejects every non-NOAH or mixed-denom deposit atomically.
- A rejection leaves the rejected principal transfer and every `MsgMultiSend` output unchanged atomically. In a full
  transaction after Phase 4, already-valid ante gas fees and stability tax retain their normal message-failure behavior.
- Unrelated recipients retain ordinary Bank behavior.
- Deposits change no total supply and create ordinary bank events; after Phase 4 activates taxation, taxable stable
  deposits additionally use the ordinary stability-tax events.
- Intended protocol `anoah` credits from Market to Buffer, Reserve, and Insurance pass the same final-recipient
  restriction; a non-NOAH module credit to any fund fails.
- A preceding restriction that rewrites an address into any fund cannot bypass the NOAH-only rule.
- Outbound reward top-ups and authorised Insurance claims are not rejected merely because of their source module
  account.
- A passed governance message can move only positive `anoah` from `treasury_strategic_reserve` to
  `treasury_redemption_buffer`; no user-supplied source, destination, denomination, or Market call path exists.

Do not remove `x/mint` until Phase 2.

### 13.6 Phase 1 invariants

- Treasury has no mint or burn dependency or permission.
- The subsidy pool accepts only irreversible transfers of already-issued `anoah`; deposits create no Treasury message,
  stored ledger, refund claim, target, or automatic refill trigger.
- A subsidy-pool deposit changes only sender/pool balances and ordinary bank events; total supply, reward targets, tax
  policy, and every other Treasury fund remain unchanged.
- Redemption Buffer accepts only irreversible `anoah`; its deposits create no special settlement path or depositor claim
  and never change conversion quotes or eligibility.
- Strategic Reserve and Insurance accept only `anoah` at launch; there is no dormant external custody, recognition,
  conversion, deployment, or in-kind claim path.
- Deposits remain valid above target; targets are not account caps.
- Once Phase 4 activates taxation, a taxable stable deposit is assessed like the same user-facing transfer to any
  ordinary account.
- Treasury consensus version is 1 and no migrator or legacy codec/type package exists.
- Protobuf tags and collection prefixes match the compact launch-only schema in Section 10.
- Target calculation itself moves no coins.
- A falling target causes no automatic outflow.
- `RouteExpansion` derives eligible principal from the gross `anoah` offer, final integer stable output, and Market
  quote rates; Market cannot supply that principal or any target, gap, credit, burn, source, or destination.
- Post-trade stable liability is used for expansion allocation by adding the not-yet-minted `stable_output` exactly
  once.
- Complete aggregate liability is scanned at most once per block after the first successful valuation. Its transient
  snapshot tracks every later successful Market stable burn/mint; incomplete valuations are not cached and may retry.
- Complete expansion settlement occurs per conversion. Treasury stores no pending principal and has no periodic or
  EndBlock allocation path.
- Incomplete unrelated expansion-target pricing routes all eligible principal to the Redemption Buffer, but failure to
  value the output itself fails before any fund transfer.
- Treasury transfers only fixed fund credits from Market and never receives the gross offer, mints, or burns.
- A priceable redemption with incomplete aggregate valuation returns a zero Buffer draw and the fallback flag; Phase 3
  Market then mints its complete quoted output.
- Redemption Buffer, strategic Reserve, and Insurance balances come only from Bank state; Claims state records
  encumbrance and authorization but never a second custody balance.
- `FundStatus` exposes raw Insurance balance, Insurance reservation, unencumbered balance, and target separately;
  standard paginated Bank queries remain the canonical custody view.
- Redemption Buffer, strategic Reserve, and Insurance never share an account.
- Market has no automatic interface for debiting strategic Reserve or Insurance.
- The only launch production path naming `treasury_strategic_reserve` as a bank-send source is
  `MsgTransferReserveToBuffer`; it is general-authority gated, `anoah`-only, uses the fixed Buffer destination, and
  checks its per-proposal minimum remaining Reserve balance.
- A successful Reserve-to-Buffer action satisfies `Delta Reserve = -amount`, `Delta Buffer = +amount`, and zero change
  to total supply, consolidated stable liability, Params, targets, Market state, quote, tax balances, subsidy pool, and
  Insurance. It succeeds without complete Oracle valuation and creates no duplicate Treasury history state.
- No Buffer-to-Reserve, arbitrary-recipient, conversion, claim, reward, or direct-redeemer Reserve path exists.
- Treasury's general authority remains governance. The Claims committee is a mandate role with exact canonical matching;
  it cannot update Params, change policy, access Reserve, send generically, mint, or borrow.
- Claim submission enforces exact role authorization, unique ID, positive `anoah`-only amount, recipient/reference,
  derived executable height, live Insurance coverage, remaining Claims allowance when committee-origin, and audit record
  before encumbering funds.
- Insurance reservation never exceeds Insurance balance. Approval changes `insurance_reserved` and the unencumbered
  balance but no Bank balance or supply; execution changes balance and reservation equally; cancellation or expiry
  changes only reservation and claim status; no path changes target exposure or performs conversion.
- At the executable height cancellation closes for committee and governance, and permissionless execution opens. No
  target or query releases the reservation before cancellation or successful execution.
- Committee and governance submissions reject zero, mixed, and non-NOAH amounts at launch.
- Claims allowance usage is gross: cancellation and execution do not restore it, deposits do not increase it, and only
  an explicit new mandate term resets it.
- Tax-cap refresh is all-or-nothing and stale prices preserve the old map.
- Empty reward-funding state has `blocks_remaining = 0` and canonical zero aggregates. The first observation initializes
  the countdown from `Params.reward_funding_window` before decrementing it. A successful non-boundary block commits a
  positive countdown; the boundary reaches zero, settles, and resets atomically. A mid-window parameter change cannot
  alter the active countdown.
- For every complete funding window, accumulated targets equal the sum of the per-block target values applied to its
  observations, and accumulated validator-fee value equals the sum of successful contemporaneous eligible valuations.
- Once any required validator-fee valuation is unavailable, that window cannot spend subsidy or allocate tax to
  validators; settlement sends all accumulated tax to Oracle and resets the window.
- Treasury observes `fee_collector` before Distribution every completed block but never retains or moves those organic
  fee coins. Stability tax remains in its collector until the funding window settles.
- A reference-cap update commits the candidate Params and complete derived cap map together or changes neither.
  InitGenesis establishes the complete map even while tax is disabled, and no stability-tax-rate change rebuilds it.
  Tax computation fails closed if the stored map nevertheless lacks a configured taxable denomination.
- Changing `Params.reference_tax_cap` changes no Market parameter/state, fund balance/target, conversion quote, or
  redemption coverage result.
- Treasury liability, target, and coverage-funded output results are unchanged by a Market pool-denomination change
  under the same stable/NOAH rate snapshot.
- Genesis export/import preserves params, caps, Claims Mandate, Claims allowance used, Insurance reservation, every
  pending/cancelled/paid claim, all fund bank balances, and governance proposals that authorised Reserve-to-Buffer
  commitments or exact claims, plus the exact in-progress Reward Funding state; Treasury stores no separate
  Reserve-commitment history.

### 13.7 Phase 1 verification

```sh
make proto-format
make proto-lint
make proto-gen
GOCACHE=/private/tmp/ark-gocache go test ./x/treasury/...
GOCACHE=/private/tmp/ark-gocache go test ./app/...
git diff --check
```

Recorded 2026-07-15 implementation gate:

- `make proto-format`, `make proto-lint`, and `make proto-gen` passed; the gogo and Pulsar Treasury outputs were
  regenerated from the approved compact schema.
- `GOCACHE=/private/tmp/ark-gocache go test -count=1 ./x/treasury/...` passed.
- `go test -count=1 ./app/...` passed with the local loopback access required by the app test harness.
- Scope-adjacent regressions passed with `GOCACHE=/private/tmp/ark-gocache go test -count=1 ./x/market/...` and
  `GOCACHE=/private/tmp/ark-gocache go test -count=1 ./x/oracle/...`.
- `GOCACHE=/private/tmp/ark-gocache go build -o /private/tmp/arkd-phase1 ./cmd/arkd` and `git diff --check` passed.
- App acceptance tests exercise actual Bank send/multisend/module-credit restrictions and atomic rejection, a passed
  governance proposal containing the exact Reserve-to-Buffer and governance-claim messages with cached rollback and
  export visibility, and an actual 2-of-3 Legacy Amino Claims committee transaction including insufficient-signature,
  non-member, missing-account, wrong-sequence, and unaffordable-fee failures.

Recorded 2026-07-16 reward-funding revision:

- Treasury now commits `RewardFundingState` at collection prefix 5 and exports/imports it through genesis. The
  `RewardFunding` query exposes the current state, including `blocks_remaining`, through a direct collection read;
  `Params` exposes the configured next-window length. `FundStatus` remains limited to funds and may fail independently
  when complete liability valuation is unavailable.
- `reward_funding_window` is governed and defaults to one chain week. The first observation initializes the countdown
  before decrementing it, so a mid-window update affects only the next window. Accumulated reward targets use checked
  addition at each observation; parameter and policy updates do not precompute remaining-window compatibility.
- BeginBlock still observes `fee_collector` before Distribution on every completed block, but subsidy and tax allocation
  occur only after a complete window. Stability tax remains in its collector between settlements.
- `UpdateRewardFunding` owns only observation and persistence. `BeginBlocker` owns the genesis-height gate, boundary
  decision, settlement call, and state reset; `SettleRewardFunding` is settlement-only.
- The target-aware waterfall is unchanged after replacing per-block `V`, `O`, and `G` with their window aggregates.
  Required fee valuation failure marks the window incomplete; settlement then sends all tax to Oracle and spends no
  subsidy.
- Focused Treasury tests cover accrual, exact boundary/reset behavior, quiet/busy-block netting, target/tax allocation,
  scarce subsidy, multi-denomination rounding, valuation fallbacks, query/genesis state, and Bank-failure rollback.

Implementation note: `MsgTransferReserveToBuffer` uses the compact Legacy Amino identifier
`ark/x/treasury/MsgTransferToBuffer` because its full protobuf-derived name exceeds the SDK type-name limit. Its
protobuf message name, RPC name, signer, and semantics are unchanged.

Phase 1 retained `x/mint` at its approved boundary. Phase 2 has now removed its Ark app wiring. The
outside-`x/treasury` dispositions remain recorded in Section 21.1.

## 14. Phase 2: Remove routine inflation and activate the subsidy pool

Status: **Reviewed**

### 14.1 Remove `x/mint`

Modify:

- `app/app_config.go`:
  - remove Mint side-effect/module imports;
  - remove Mint module config;
  - remove the Mint module account and `Minter` permission;
  - remove Mint from BeginBlock, InitGenesis, and ExportGenesis orders.
- `app/app.go`:
  - remove `MintKeeper` import, field, and depinject target.
- `app/app_test.go`:
  - remove Mint module-version expectations;
  - assert Mint is absent and Market retains the only native stable-conversion mint path. When IBC is installed later,
    separately assert that the transfer account has only the standard voucher minter/burner permissions.
- `app/test_helpers.go`:
  - delete the unused generic Mint-based account-funding helpers; existing active setup uses genesis balances;
  - permit direct Market minting only in tests explicitly exercising Market.
- `cmd/arkd/cmd/testnet_test.go`:
  - remove `mint.AppModuleBasic`.

Do not remove the SDK mint dependency from `go.mod`; it may remain transitively required.

### 14.2 Record the launch distribution setting

Do not add an Ark Distribution module-basic wrapper, fork the Distribution keeper, or change generic `arkd init` and
testnet defaults during Phase 2. Ark's canonical launch `genesis.json` will be assembled and reviewed during launch
readiness. It must set `app_state.distribution.params.community_tax` to zero. Section 18 tracks that later deliverable.

### 14.3 Activate balance-constrained reward top-ups

Treasury BeginBlock must run immediately before Distribution. It observes the completed block's eligible validator-fee
value every block, retains stability tax while the active reward-funding countdown is positive, and settles when it
reaches zero. Test:

- Height 1 records no observation; under the default, the first clean-genesis settlement occurs after exactly
  `DefaultRewardFundingWindow` completed blocks.
- A mid-window `reward_funding_window` update leaves the active countdown and boundary unchanged; the first observation
  after reset initializes the countdown from the new value.
- Before the boundary, targets and eligible validator-fee value accrue, stability tax remains untouched, and no subsidy
  is spent.
- Quiet and busy blocks net across the window; aggregate gas at or above the aggregate validator target sends all
  accumulated tax to Oracle and spends no validator subsidy.
- Tax first protects the aggregate Oracle target, then funds only the remaining aggregate validator gap, then returns
  every residual to Oracle.
- Multi-denom validator allocation uses one value-derived ratio, rounds down independently per denomination, and is
  revalued before subsidy calculation.
- Organic funding completely covers one or both aggregate targets without spending subsidy funds.
- Exact partial shortfalls are paid without adding the full targets.
- A scarce subsidy-pool balance is divided proportionally between actual shortfalls with the integer remainder to
  validators.
- `stability_tax_collector` is emptied even when targets are zero or valuation is unavailable; the fallback sends all
  tax to Oracle.
- One unavailable required fee valuation marks the whole window incomplete; later valid prices do not reconstruct it,
  settlement spends no subsidy, and the state resets. Settlement-time tax valuation failure uses the same fallback.
- A per-block target change contributes the value in effect for each later observation without rewriting earlier
  accumulated targets.
- Empty Treasury balance no-op.
- An accepted `anoah` deposit before depletion extends shortfall coverage without changing either reward target.
- An accepted `anoah` deposit after depletion restarts shortfall coverage on the next eligible BeginBlock.
- Exact transfers to `fee_collector` and Oracle.
- No supply change.
- No Redemption Buffer, strategic Reserve, or Insurance debit.
- Bank failure returns an error and rolls the block state back.
- Window settlement ordering relative to Distribution and any resulting previous-power SDK residual accounting.

Production genesis must allocate the initial already-issued NOAH balances through bank genesis to:

- Treasury subsidy pool.
- Redemption Buffer seed, if any.
- Strategic Reserve seed, if any.
- Insurance seed, if any.

All four allocations must contain only already-issued `anoah`. Each complete launch balance is counted according to its
fund-specific target rule. Treasury InitGenesis must never mint these balances.

### 14.4 Phase 2 invariants

- Only Market has `Minter`.
- With no Market conversions, NOAH supply is identical before and after every BeginBlock and EndBlock.
- Reward top-ups never exceed the subsidy-pool balance or either calculated shortfall.
- Subsidy decisions use only aggregate complete-window targets and organic funding; no intermediate block gap spends
  subsidy.
- Every complete or skipped settlement resets Reward Funding state only after all Bank transfers succeed; a failure
  rolls back transfers and the state transition together.
- Subsidy-pool depletion never causes minting, borrowing, automatic conversion, or an automatic refill.
- Permissionless subsidy-pool deposits never change total supply, the configured per-block reward targets, or the
  current accumulated target values.
- Any validator reward top-up reaches `fee_collector` before Distribution executes.
- Any Oracle reward top-up reaches Oracle without passing through Distribution.

### 14.5 Phase 2 verification

```sh
GOCACHE=/private/tmp/ark-gocache go test ./x/treasury/...
GOCACHE=/private/tmp/ark-gocache go test ./app/...
GOCACHE=/private/tmp/ark-gocache go test ./cmd/arkd/cmd/...
GOCACHE=/private/tmp/ark-gocache go build -o /private/tmp/arkd-phase2 ./cmd/arkd
git diff --check
```

Recorded 2026-07-19: the focused Treasury, app, and `arkd` command suites passed; app and command tests required the
known loopback-capable rerun after the sandbox rejected a local listener. The Phase 2 binary built successfully at
`/private/tmp/arkd-phase2`, the source tree contains no direct Go import of `x/mint`, and `git diff --check` passed. The
SDK Mint types may remain a transitive dependency. A direct `arkd init` smoke check generated no Mint genesis section
and retained the SDK's 2% Distribution scaffold default, which the later canonical launch-genesis process must replace
with zero as specified in Sections 9.3 and 18.

Phase 2 was reviewed and approved on 2026-07-20. Phase 3 proceeded under the subsequent explicit implementation approval
recorded below.

## 15. Phase 3: Market settlement and live pool-denomination support

Status: **Reviewed**

### 15.1 Phase 3A: read-only capacity analysis

Before editing Market, quantify the current virtual-pool mechanism:

- Maximum single-redemption output at representative `ArkPoolDelta` states.
- Cumulative residual NOAH issuance under sustained redemption at representative Buffer-to-liability ratios.
- Capacity restored per block and over one `PoolRecoveryPeriod`.
- Sensitivity to `BasePool`, `MinStabilitySpread`, and Oracle price movement.
- Alternating expansion/redemption behavior.
- Adversarial splitting across accounts and blocks.
- Split-versus-unsplit coverage funding: each complete-valuation floor retains less than one base-unit NOAH relative to its
  exact coverage share. Measure adversarial transaction-count effects and prove splitting cannot increase residual mint
  merely by changing the settlement partition.
- Whether any proposed quota or gate would create a public threshold and first-mover incentive.

`PoolRecoveryPeriod` is an exponential decay parameter, not a time to full recovery. The current update is
approximately:

```text
delta_next = delta - delta / PoolRecoveryPeriod
```

After `PoolRecoveryPeriod` blocks, roughly 36.8% of an initial imbalance remains, subject to decimal truncation.

The pools provide a virtual price/imbalance curve and a soft output bound, not actual liquidity, backing, or an explicit
rolling mint quota.

Review gate:

- The denomination-aware `BasePool` and labelled delta query in this phase are required regardless of the measured
  issuance envelope.
- If the measured issuance envelope is acceptable, add no residual-mint limiter or limiter fields.
- If it is not acceptable, stop and propose an explicit residual-mint window/cap as an additional Market proto and
  policy change, including its first-mover and redemption-availability consequences.
- Do not silently add a second limiter during implementation.

Recorded 2026-07-20: the deterministic fixed-point Phase 3A runner covered 1,152 table cases, 5,000 seeded boundary
cases, same-block splits of 1/10/100/1,000 settlements, recovery schedules, parameter sensitivity, and fixed-price
expansion/redemption cycles. It used the intended Phase 3 rule that floors a negative raw spread to
`MinStabilitySpread`; the production handler now follows the same rule, and direct app cross-checks of the real
settlement paths matched the runner exactly. The measured results are:

- With a 2% minimum spread, every tested redemption output was at most 98% of the redeemed liability's fresh Oracle
  value. The maximum grid residual was `9.8 * BasePool` when liability was `10 * BasePool`, the Buffer was unavailable,
  and all liability was redeemed. This is a liability-relative bound, not a per-block quota: an empty Buffer or
  incomplete aggregate valuation can mint the complete quoted output in one block.
- From zero delta, redeeming liability equal to `0.5 * BasePool` with no Buffer returned `0.333333333333 * BasePool`
  immediately, `0.363205266023 * BasePool` across one default recovery period, and `0.386760348994 * BasePool` across
  two recovery periods. Recovery restores output capacity continuously but does not create stable liability.
- No equal same-block split increased aggregate recipient output. Fixed-price immediate, recovered, and 100 repeated
  expansion/redemption cycles all returned less NOAH than their expansion input, so the runner found no recipient-value
  or round-trip amplification.
- The initial liability-entitlement formula failed the partition test: splitting could preserve Buffer and substitute
  material residual mint without increasing recipient output. The approved correction instead applies actual pre-trade
  Buffer coverage, capped at 100%, to each quoted output. The complete table and seeded rerun found no split that
  increased recipient output or residual mint, and every complete non-final redemption preserved or increased actual
  Buffer coverage.
- A full-app cross-check confirmed the corrected funding behavior through the real Market, Treasury, Oracle, and Bank
  keepers. At `delta = 0.9 * BasePool`, liability `BasePool`, Buffer `0.25 * liability`, and total redemption
  `0.5 * liability`, one settlement used `0.027412280701 * BasePool` of Buffer and minted
  `0.082236842106 * BasePool`. Ten equal settlements returned four fewer base-unit NOAH, used
  `0.033529886425 * BasePool` of Buffer, and minted less: `0.076119236378 * BasePool`.
- BasePool scaling was linear apart from base-unit truncation, Oracle-rate changes scaled NOAH output inversely, and
  changing the recovery-period block count while observing exactly one corresponding period produced effectively the
  same exponential recovery envelope.

Recorded 2026-07-25 after moving Ark-native denominations to exponent 18: the same-block split sweep remained within
the explicit eight-operation `LegacyDec` rounding envelope. Its maximum positive recipient-output and residual-mint
difference was `3000000anoah`, or `0.000000000003noah`. At launch BasePool scale, the one-third scaling cases differed
from the exact integer quotient by at most `333334anoah`. These are bounded fixed-point effects, not value-level
amplification; the tests now assert the relative rounding envelope instead of exact base-unit monotonicity.

P2 is complete. The corrected measurements support no hard residual-mint limiter: they found no value-level recipient
output or residual-mint amplification from partitioning and no profitable fixed-price cycle, while a limiter would add
the known public threshold and failed-redemption incentive. Keep coverage-based Buffer funding and add no limiter fields.

### 15.2 Market implementation files

Preserve the active minimum-receive, arithmetic, and rate-snapshot refactors already in the dirty worktree.

Modify protobuf definitions:

- `proto/ark/market/v1/market.proto`:
  - change `base_pool` from a naked decimal to a non-nullable `cosmos.base.v1beta1.DecCoin` / `sdk.DecCoin`;
  - document that `ArkPoolDelta` is measured in `BasePool.Denom`.
- `proto/ark/market/v1/query.proto`: add `pool_denom` beside the signed decimal in `QueryArkPoolDeltaResponse`. A signed
  delta cannot be represented as a valid `sdk.DecCoin`, so the amount and unit remain separate fields.
- `proto/ark/market/v1/tx.proto`: update `MsgUpdateParams` comments with the denomination-change contract, but add no
  new RPC, transition message, or transition object.
- `proto/ark/market/v1/genesis.proto`: label the delta unit in comments; Params remains the sole source of that unit.
- `proto/ark/market/v1/event.proto`: keep `EventSwap` limited to trader, recipient, offer, final output, and fee. Keep
  Treasury allocation and Buffer-draw data in Treasury-owned events. `EventPoolUpdated` records the old and applied
  BasePool plus old/new delta; the successful transaction already contains the submitted expectation, and the enclosing
  ABCI result supplies block height.

Regenerate:

- `x/market/types/market.pb.go`.
- `x/market/types/genesis.pb.go`.
- `x/market/types/query.pb.go`.
- `x/market/types/tx.pb.go`.
- `api/ark/market/v1/market.pulsar.go`.
- `api/ark/market/v1/genesis.pulsar.go`.
- `api/ark/market/v1/query.pulsar.go`.
- `api/ark/market/v1/query_grpc.pb.go`.
- `api/ark/market/v1/tx.pulsar.go`.
- `api/ark/market/v1/tx_grpc.pb.go`.
- `x/market/types/event.pb.go`.
- `api/ark/market/v1/event.pulsar.go`.

Modify:

- `x/market/types/params.go`: construct the launch `BasePool` in `asdr` and validate positive canonical DecCoin data.
  Keep the cross-module Oracle-native check in the keeper rather than pure Params validation.
- `x/market/types/pool.go`: continue deriving effective pools from `BasePool.Amount` and the signed delta.
- `x/market/types/genesis.go` and tests.
- `x/market/types/expected_keepers.go`: add only the narrow `RouteExpansion`, `DrawRedemptionBuffer`, and
  `RecordSupplyChange` Treasury interface. Import the handwritten Treasury result types; do not reproduce their structs
  in Market.
- `x/market/types/errors.go`: only if stable settlement-specific error codes are necessary.
- `x/market/keeper/keeper.go`: store the Treasury dependency.
- `x/market/module/depinject.go`: inject Treasury.
- `x/market/keeper/msg_server.go`: branch settlement by direction; keep the gross expansion offer in Market escrow; call
  `RouteExpansion` exactly once before expansion burn/mint/payout; propagate its error or treat its successful result as
  authoritative without duplicate validation; call `DrawRedemptionBuffer` before redemption burn; after every
  conversion burn/mint, call `RecordSupplyChange` before payout; and implement the `MsgUpdateParams` transition contract
  in Section 15.3.
- `x/market/keeper/swap.go`: use `BasePool.Denom` for NOAH/stable pool math, price stable-to-stable directly, and
  correct spread flooring.
- `x/market/keeper/grpc_query.go`: return the current pool denomination with `ArkPoolDelta`.
- `x/market/keeper/keeper_test.go`.
- `x/market/keeper/msg_server_test.go`.
- `x/market/keeper/swap_test.go`.
- `x/market/keeper/grpc_query_test.go`.
- `x/market/testutil/expected_keepers_mocks.go` by regeneration.
- `x/market/simulation/genesis.go`, `x/market/simulation/msg_factory.go`, and their tests.
- `x/market/module/autocli.go` only if the labelled query output requires a description update.
- Constructor and simulation callers affected by the new dependency or Params shape.
- `app/export.go`: preserve `ArkPoolDelta` in ordinary post-launch exports; reset it only when an operator explicitly
  requests fresh-genesis/zero-height reset semantics.

This is a clean prelaunch schema change. Do not add compatibility fields, a migration, a second pool, or persistent
transition progress. Phase 3A decides only whether a separate residual-mint limiter is necessary.

### 15.3 Required virtual-pool corrections

1. Floor a negative raw rebalancing spread to `MinStabilitySpread` before validating the final upper bound. A balancing
   trade must not fail merely because the raw curve spread is negative.
2. Preserve the invariant `finalSpread in [MinStabilitySpread, 1]` for NOAH-to-stable and stable-to-NOAH swaps.
3. Branch stable-to-stable pricing before loading virtual-pool state. Capture only fresh offer/ask rates, convert
   directly from offer to ask, and apply the larger Tobin tax. Its quote and settlement must be independent of
   `BasePool`, `ArkPoolDelta`, and the current pool denomination.
4. For NOAH/stable pricing, read `Params.BasePool.Denom` and use it everywhere the current implementation hard-codes
   `chain.SDRBaseDenom`: rate-snapshot capture, offer normalisation, constant-product inputs, ask normalisation, and
   delta updates. The pool denomination is a unit of account, not an intermediate mint or transfer.
5. Implement `MsgUpdateParams` with one branch inside the existing Market handler. This is the sole pool amount and
   denomination transition path; it makes no Treasury call:

   ```text
   old_base  = current_params.BasePool       // B old_denom
   expected  = msg.Params.BasePool           // E new_denom
   old_delta = ArkPoolDelta

   if expected.Denom == old_base.Denom:
     applied_base = expected
   else:
     require expected.Denom is an Oracle-configured native stable
     rates = one fresh immutable snapshot(old_base.Denom, expected.Denom)
     derived = rates.Convert(old_base, expected.Denom) // C new_denom
     applied_base = derived

   new_delta = checked_quo(
     checked_mul(old_delta, applied_base.Amount),
     old_base.Amount,
   )

   validate NewEffectivePools(applied_base.Amount, new_delta)
   write msg.Params with BasePool = applied_base, and write new_delta
   ```

   `BasePool.Amount` in a denomination-changing proposal is therefore a non-binding audit expectation, not a second
   stored transition value. The fresh consensus Oracle snapshot is authoritative for the applied amount. The successful
   transaction retains the submitted value, while `EventPoolUpdated` records old and applied state. Market does not
   compare the two amounts to a rejection threshold because ordinary Oracle movement during a governance voting delay
   does not make the deterministic re-denomination unsafe.

   `PoolRecoveryPeriod` and `MinStabilitySpread` are dimensionless policy inputs and may change in the same message as
   the denomination. They are applied atomically with the transition and affect subsequent recovery and pricing in the
   ordinary way; neither changes the conversion or delta-rescaling calculation. The supported stablecoin set and mint
   eligibility do not change. A deliberate depth change follows as a same-denomination update because the submitted
   amount remains a non-binding expectation during a denomination change. A same-denomination amount update performs no
   Oracle lookup and treats the submitted amount as the intended depth.

   Rescale delta for both branches to preserve relative imbalance. This is required for every effective `BasePool`
   amount change, not only a denomination transition:

   ```text
   new_delta / applied_base.Amount = old_delta / old_base.Amount
   ```

   Perform every rate, denomination, arithmetic, and effective-pool check before writing. Params and delta must commit
   together or not at all. Never reset delta on a unit change, and never mint, burn, or move coins as part of the
   parameter update. Emit old BasePool, live-derived/applied BasePool, and old/new delta. Observers pair that event with
   the successful `MsgUpdateParams` transaction for the submitted expectation and with the enclosing ABCI result for
   height; do not duplicate either value in the event.

6. Pool-delta updates depend on economic conversion flow, not whether NOAH output came from the Redemption Buffer or
   minting.

#### Planned live basket flagship transition

The future flagship change uses ordinary module configuration and one Market parameter update:

1. Add the new basket denomination to Oracle's native-stable/Tobin configuration, then wait for fresh settled basket
   and `asdr` rates. Treasury observes the shared Oracle configuration and derives the corresponding tax cap; no
   Treasury Params change is required merely to make the denomination Market's pool unit.
2. Submit one Market `MsgUpdateParams` whose `BasePool` carries the basket denom and governance's expected basket
   amount, together with any intended recovery-period or minimum-spread changes.
3. At execution, retain the submitted value in the successful transaction, emit the old and applied pool state, apply
   the fresh live-derived basket amount, and rescale `ArkPoolDelta` atomically. Do not alter stablecoin output eligibility.
4. Keep `asdr` in Oracle, Tobin-tax, Treasury-tax, and Market support. Both NOAH-to-`asdr` and basket-to-`asdr`
   conversions remain valid, so `asdr` may continue to be minted as an ordinary Ark stablecoin.
5. Change `Params.reference_tax_cap` to a basket-denominated Coin later only if governance wants the basket to become
   the tax-cap reference; that separate Treasury update neither drives nor repeats the Market transition.

Do not add a Treasury orchestration message, a Treasury-to-Market keeper dependency, or a requirement that both modules
change Params together. Governance may coordinate the two independent policy choices operationally, but each module
validates and writes only its own state.

Ark never runs parallel SDR and basket virtual pools. Basket and `asdr` liabilities may coexist indefinitely and both
remain valid outputs, while all NOAH/stable pressure updates the one active pool expressed in the basket denomination
after the transition.

### 15.4 Settlement failure matrix

Use mocked keeper tests for distinct error-propagation boundaries and real app/cache tests for transaction rollback.
Do not add fault-injection abstractions to production keepers solely to repeat the SDK cache contract at every call.

Mocked Market tests cover:

1. User-to-Market transfer.
2. `RouteExpansion` and `DrawRedemptionBuffer` errors.
3. Market burn and mint.
4. `RecordSupplyChange`.
5. Market-to-recipient payout.

Treasury keeper tests separately inject each fixed expansion transfer failure, including Reserve failure after Buffer
credit and Insurance failure after Buffer and Reserve credits. Quote, rate, checked-arithmetic, effective-pool, and
minimum-receive failures are tested before settlement.

For each of the three settlement directions, one full-app test executes against an SDK cache context and deliberately
fails the final payout to a blocked module account. Before that failure, assert the cached settlement changed state and
emitted events. Discard the cache and assert the parent context retained its exact:

- User, Market, and Treasury-fund balances.
- NOAH and affected stablecoin supplies.
- Market Params and `ArkPoolDelta`.
- Complete transient Treasury liability snapshot and derived targets.
- Event set.

Separately cover `MsgUpdateParams` transition failures for invalid/non-native candidate denom, missing or stale old/new
rate, checked-arithmetic failure, and nonpositive/unrepresentable effective pools. Each pre-write failure preserves both
old Params and old delta. Cover positive, negative, and zero deltas, a successful same-denomination depth update, and
denomination changes with zero, small, and arbitrarily large differences between submitted and applied amounts; that
difference alone never rejects an otherwise valid transition. Params and delta use the same transaction cache and commit
together; do not introduce injectable collection-write wrappers solely to manufacture store failures.

### 15.5 Phase 3 invariants

- Expansion allocation fields are nonnegative and representable.
- Expansion: `eligible_principal_noah = buffer_credit + strategic_reserve_credit + insurance_credit + overflow_burn`.
- Expansion: `gross_offer_noah = eligible_principal_noah + spread_and_dust_burn`.
- Expansion: `total_noah_burn = spread_and_dust_burn + overflow_burn`.
- Expansion: `gross_offer_noah = buffer_credit + strategic_reserve_credit + insurance_credit + total_noah_burn`.
- Market calls `RouteExpansion` exactly once per NOAH-to-stable conversion and never for an ordinary transfer,
  stable-to-NOAH redemption, stable-to-stable conversion, BeginBlock, EndBlock, or epoch.
- Market calls `RecordSupplyChange` exactly once after the burn/mint of every successful conversion, including
  stable-to-stable conversion, and before receiver payout. Failed settlement rolls the transient delta back with the
  canonical Bank changes.
- The first aggregate-liability consumer in a block performs the supply/rate scan. Every later consumer reuses the
  transient complete/incomplete result; a complete result equals canonical supply valued at the block's fixed rates
  after all preceding successful Market deltas.
- Gross expansion NOAH remains in Market escrow until Treasury moves only the three fixed credits and Market burns the
  remainder; the Treasury subsidy-pool account never temporarily receives it.
- Treasury derives eligible principal and the complete waterfall; Market consumes the returned allocation without
  recomputing its policy.
- Treasury has no `BurnCoins` dependency and never burns during routing. Market performs one NOAH burn equal to the
  `ExpansionAllocation.TotalBurn()` returned by Treasury.
- Redemption: `noah_output = buffer_paid + residual_mint`.
- Stable offers are burned exactly once.
- Stable outputs are minted exactly once.
- Spread/dust never enters Redemption Buffer, strategic Reserve, or Insurance.
- Expansion fills Redemption Buffer, then strategic Reserve, then Insurance.
- Every priced redemption requires `0 < noah_output <= redeemed_liability_noah`. Complete valuation additionally
  requires `redeemed_liability_noah <= aggregate_liability_noah`.
- With complete valuation, `buffer_coverage = min(1, pre_trade_buffer / aggregate_liability_noah)` and `buffer_paid`
  equals `floor(noah_output * buffer_coverage)`.
- For non-final redemptions with complete valuation, cross-multiplication proves the post-redemption actual
  Buffer-per-liability coverage does not fall because quoted output cannot exceed redeemed liability.
- A priceable redemption with incomplete aggregate valuation draws zero Buffer and mints its full quoted output.
- Strategic Reserve is never debited directly by ordinary redemption.
- Insurance never services redemption.
- Buffer usage never changes the quote, minimum receive, or economic pool-delta calculation.
- A permissionless Buffer deposit is included in the next pre-trade Buffer balance and can increase `buffer_paid` and
  reduce `residual_mint`; it never changes the quote, spread, minimum receive, eligibility, or pool-delta transition.
- No Redemption Buffer or strategic Reserve balance threshold changes redemption eligibility or price.
- Buffer balance and funding source never change an individual Market quote. Split-versus-unsplit output differences
  remain governed solely by Market's sequential virtual-pool state, spread, and integer truncation. The Phase 3A sweep
  proves that the coverage-based draw does not let a split path increase either aggregate recipient output or aggregate
  residual mint; each extra Buffer floor can affect aggregate funding by less than one base-unit NOAH.
- Stable-to-stable conversion touches none of the three funds or NOAH supply.
- Stable-to-stable output is calculated directly from offer/ask rates and is unchanged by `BasePool.Denom`,
  `BasePool.Amount`, or `ArkPoolDelta` when the offer/ask snapshot and Tobin taxes are fixed.
- Every NOAH/stable pool input and delta update uses the current `BasePool.Denom`; generic Market pricing contains no
  hard-coded SDR unit.
- `ArkPoolDelta` is always reported and interpreted in `BasePool.Denom`.
- A same-denomination BasePool resize and a denomination change both preserve `delta / BasePool.Amount` within the
  documented fixed-point rounding bound; neither resets the delta.
- Market's `MsgUpdateParams` is the only path that changes `BasePool` or rescales `ArkPoolDelta`; it makes no Treasury
  call, and Treasury exposes no operation that changes Market state.
- A denomination change applies one fresh deterministic derived amount regardless of its difference from submitted
  `BasePool.Amount`; the transaction retains the submitted expectation, the event emits old and applied state, and every
  actual validation failure leaves Params and delta unchanged.
- A Market pool-denomination transition leaves Treasury nominal liability, target exposure, targets, and
  coverage-based Buffer funding unchanged under the same stable/NOAH rates. It also leaves `reference_tax_cap`
  unchanged unless governance later submits the independent Treasury Params update.
- Changing `BasePool.Denom` changes no stablecoin's offer/output eligibility. `asdr` remains a valid output after the
  basket transition and its supply may continue to increase through ordinary conversion settlement.
- There is exactly one active virtual pool and no transition object, parallel delta, or persistent transition phase.
- Same-direction pressure never improves the quoted price.
- Recovery moves delta toward zero without crossing its sign.
- Effective pools remain positive and representable.
- Market ends a successful settlement without an unintended residual escrow balance.

### 15.6 Phase 3 verification

```sh
make proto-format
make proto-lint
make proto-gen
GOCACHE=/private/tmp/ark-gocache go test -count=1 ./x/treasury/...
GOCACHE=/private/tmp/ark-gocache go test -count=1 ./x/market/...
GOCACHE=/private/tmp/ark-gocache go test -count=1 ./x/oracle/...
GOCACHE=/private/tmp/ark-gocache go test -count=1 ./app/...
GOCACHE=/private/tmp/ark-gocache go test -count=1 ./cmd/arkd/...
GOCACHE=/private/tmp/ark-gocache go vet ./x/market/... ./x/treasury/...
GOCACHE=/private/tmp/ark-gocache go build -o /private/tmp/arkd-phase3 ./cmd/arkd
git diff --check
```

Recorded 2026-07-20: all commands above passed. The Market suite covers same-denomination delta rescaling, positive,
negative, and zero deltas, deterministic live denomination changes with simultaneous recovery-period and minimum-spread
updates and arbitrarily large expectation differences, failure atomicity, direct stable-to-stable pricing,
negative-spread flooring including an extreme valid pool state that previously overflowed before flooring, labelled
queries, mocked settlement error propagation, settlement event accounting, and the Phase 3A capacity model. Full-app
cache tests force a late payout failure after each of the three directional settlement paths and confirm balances,
supplies, pool state, transient Treasury liability, targets, and events all roll back. The full-app transition test
changes the pool unit from `asdr` to `ausd`,
applies the live-derived amount, rescales delta, preserves stable-to-stable quotes, confirms `asdr` remains a valid
NOAH conversion output, and confirms an ordinary app-genesis export preserves the denomination-bearing Params and
post-settlement delta. The binary built at `/private/tmp/arkd-phase3`; the known listener-dependent app and command
suites passed in the loopback-capable test environment.

The user review and independent call-path review completed on 2026-07-20. The independent review's arithmetic boundary,
rollback-coverage, and stale-plan findings were remediated and verified; Phase 3 is reviewed.

## 16. Phase 4: Wasm/IBC foundations, stability-tax integration, and multi-denom Oracle rewards

Status: **IBC foundation implemented; focused review and the remaining Phase 4 integrations are pending**

This phase first fixes the execution contract that every transfer adapter must implement, then adds IBC foundations,
then Wasm foundations, and finally completes tax integration across the real execution paths. IBC precedes Wasm because
contracts may dispatch IBC messages and the Wasm adapter must target the actual IBC boundary rather than a hypothetical
one. The foundation work and tax adapters may be developed in this order, but neither transfer surface may become
production-accessible or pass the phase review boundary until its complete tax and Treasury recipient-restriction tests
pass.

Tax activation remains coupled to Oracle's ability to distribute stablecoin-denominated rewards. Do not activate
taxation without multi-denom Oracle distribution, and do not activate Wasm or IBC with an untaxed stablecoin transfer
path.

### 16.1 Tax design gate

Approved 2026-07-20. This gate changes no tax rate, cap, or calculator policy and establishes the following execution
contract before implementation files are selected:

- The normal SDK fee payer or feegrant granter pays every ante-visible tax as part of the declared transaction fee.
- The sending contract account pays every execution-generated tax in addition to the complete requested principal. An
  insufficient balance for `principal + tax` fails that execution-generated message without reducing recipient output.
- Ante owns signed top-level `MsgTransfer` and Wasm attached funds. The Wasm dispatcher owns only contract-generated Bank
  sends, IBC sends, execute funds, and instantiate funds. This structural ownership provides exactly-once assessment;
  Ark adds no persistent transfer ID, context marker, or global Bank tax hook.
- The dispatcher sends tax directly to `stability_tax_collector` and collects it in the same Wasm submessage cache as
  the corresponding transfer. Synchronous, caught, and uncaught execution failures roll back both at that boundary.
- Feegrant and the outer transaction fee payer do not sponsor execution-generated tax. A separate explicit sponsorship
  mechanism would require later policy approval.
- A successfully created IBC packet retains its tax through later acknowledgement, timeout, refund, or return
  bookkeeping. Those operations, IBC escrow, Market settlement, Treasury movement, claims, and reward distribution are
  not new user-facing transfer inputs and receive no second tax.

The IBC dependency family, hub scope, and file-level foundation plan are approved below.

### 16.2 IBC foundations and hub architecture

Approved 2026-07-21. Implement the IBC core and transfer execution path before Wasm. Target
`github.com/cosmos/ibc-go/v11` v11.2.0, whose SDK v0.54 and Go 1.25 dependency family matches the current app, but treat
the exact resolved dependency graph and successful compile as part of the file-level implementation gate. Do not copy
legacy `ParamSubspace` wiring or introduce a second tax calculator.

Ark is intended to act as an interchain hub. Build the static routes and stateful modules that are expensive to add after
launch, but distinguish installed capability from production activation:

- Wire IBC core, ICS-20 transfer, both the IBC Classic and IBC v2 transfer routes, and their stores, module manager,
  genesis, query, and CLI surfaces. The v2 route reuses the same transfer keeper rather than introducing duplicate
  transfer state.
- Give the transfer module account only its required minter and burner permissions.
- The implemented foundation registers only the 07-Tendermint light-client module. Keep the standard IBC module genesis
  defaults in generic application genesis construction, but set Ark's canonical launch genesis allowed-client list to
  exactly `07-tendermint`, not the upstream wildcard. The conditional 08-Wasm installation below does not change that
  launch authorization; any other client type requires a separate policy and implementation decision.
- Build the IBC Classic transfer stack from the base application outward as transfer, callbacks when Wasm is added,
  packet forwarding, and rate limiting. The effective inbound stack order is therefore rate limit, packet forward,
  callbacks when present, then transfer. Packet forwarding is initially a Classic capability; do not invent a v2 PFM
  adapter absent upstream support.
- Wrap the IBC v2 transfer route with the v11-compatible rate limiter. Add its compatible callbacks during the Wasm
  phase; fix and test the exact middleware order at that file-level gate.
- Include the ICA controller and host keepers, stores, modules, and static routes, but launch both disabled. The
  controller initially exposes only the standard user ICA path; add no generic custom authentication module. Launch the
  host with an empty message allowlist; any later activation must name an explicit set of allowed protobuf message type
  URLs and must never use the `"*"` wildcard. A future contract-facing controller integration should be a narrowly
  scoped Wasm-to-ICA adapter with explicit authorization rather than a general contract authentication surface.
- Add the IBC v2 GMP application together with the Wasm foundation, not as an independent partially integrated route.
  GMP's derived-account SDK messages and contract-generated SDK messages must use one Treasury-aware execution router,
  so the derived GMP account pays any execution-generated tax in addition to principal and the complete operation rolls
  back on dispatch failure. GMP requires authorization, tax, recipient-restriction, acknowledgement, and timeout tests
  before it becomes production-accessible.
- Consider the 08-Wasm light client only while integrating the real Wasm runtime and only if its exact dependency and VM
  family resolve and compile cleanly. If installed, keep it dormant at launch: Ark's allowed-client list remains exactly
  `07-tendermint` and the canonical launch genesis contains no Wasm-client checksums. `09-localhost` is part of the core
  client machinery but remains unavailable under that launch policy; add neither 06-Solo Machine nor the experimental
  attestations client.
- Keep packet forwarding Classic-only until upstream supplies reviewed v2 support. Add no custom v2 PFM adapter, no
  ICS-29 relayer-fee application because it was removed from IBC-Go, no ICS-721 NFT transfer module, and no separate IBC
  Hooks middleware. Callbacks are the canonical ICS-20 transfer-and-call path; native Wasm IBC channels serve custom
  contract protocols, and GMP serves arbitrary remote SDK-message execution. Revisit IBC Hooks only for a concrete
  requirement for Osmosis-compatible `wasm` memo or intermediary-address semantics.
- Set both ICS-20 `send_enabled` and `receive_enabled` to false in Ark's canonical launch genesis. Before changing either
  value, configure reviewed rate limits for every enabled transfer denomination and route, and pass the complete tax,
  recipient-restriction, relay, acknowledgement, timeout, refund, and packet-forward activation tests.

Foundation implementation recorded 2026-07-21:

- `app/ibc.go` owns the manually wired IBC keepers, stores, Classic and v2 routes, 07-Tendermint registration, redundant
  relay ante decorator, module basics, and IBC-Go testing-app accessors. It uses the SDK runtime's `RegisterStores` and
  `RegisterModules` hooks so IBC can coexist with Ark's depinject-wired modules without duplicating the app lifecycle.
- The currently implemented Classic ICS-20 stack is transfer, packet forwarding, then rate limiting; the v2 route is
  transfer wrapped by the v11 rate limiter. ICA controller and host keepers and routes are installed. The approved Wasm
  slice will insert callbacks between transfer and packet forwarding in Classic and between transfer and rate limiting
  in v2, and will add GMP through the shared execution router. Those callbacks and GMP are not wired yet. 08-Wasm remains
  conditional on the compatibility gate above.
- `app/app_config.go` owns module accounts and lifecycle order. `cmd/arkd/cmd` merges IBC module basics into genesis and
  client encoding and exposes their query and transaction commands.
- Generic `DefaultGenesis()` deliberately retains the upstream module defaults. Building and reviewing Ark's canonical
  launch `genesis.json` remains a Phase 5 TODO; that artifact must apply the disabled transfer and ICA settings, empty
  ICA host allowlist, `07-tendermint`-only client policy, and no 08-Wasm checksums before production launch.

The IBC design must:

- Include top-level outbound Classic or v2 `MsgTransfer` principal in ante-visible tax.
- Call the Treasury execution adapter for execution-generated outbound transfers that ante could not observe.
- Treat a packet-forwarded hop as protocol-generated continuation, not a new user-facing transfer or a second taxable
  input. The forwarding path must not call the Treasury execution adapter merely because it creates the next packet.
- Keep escrow, packet commitment, acknowledgement, timeout, refund, and return bookkeeping outside the user-transfer
  tax principal unless a distinct new user-facing transfer occurs.
- Route every Bank credit through the configured Bank keeper so Treasury fund recipient restrictions cannot be bypassed.
- Preserve the Section 16.1 structural ownership and atomic rollback before the transfer surface becomes
  production-accessible.

IBC foundation code may precede the final ante refactor in development, but it must remain production-disabled until the
complete Phase 4 activation matrix passes.

### 16.3 Wasm foundations

Approved 2026-07-21. Implement Wasm after the real IBC transfer boundary exists. Use upstream Wasmd `v0.70.3` and
`wasmvm/v3`: that Wasmd release targets the SDK v0.54 and IBC-Go v11 dependency family used by Ark. The initial
read-only dependency resolution found no family conflict, but the final resolved graph and compilation against Ark's
newer patch versions remain implementation gates. Do not add replacement directives to force compatibility and do not
maintain an Ark Wasmd fork. If the clean integration fails, stop the slice and reassess the dependency rather than
copying the runtime.

The file-level implementation should:

- Add the upstream Wasm keeper, store, module, module account permission, node configuration, CLI/genesis basics,
  snapshot extension, pinned-code initialization, and native Classic and v2 contract IBC routes through Ark's existing
  manual runtime-registration boundary.
- Replace the unused `x/wasm/exported` `wasmvm` v1 parser/query interfaces with the real `wasmvm/v3` integration; retain
  no parallel legacy adapter.
- Wrap the SDK message router supplied to Wasmd rather than forking Wasmd's message parser or dispatcher. The wrapper
  receives the exact SDK message produced by Wasmd's canonical encoder, invokes Treasury's canonical tax calculator,
  collects execution-generated tax from the unique sending contract account, and then calls the existing handler in
  the same cached execution boundary.
- Supply that same Treasury-aware SDK message router to the v2 GMP keeper. GMP performs its own derived-account signer
  authentication; the shared wrapper then charges that derived account for any taxable execution-generated principal
  before dispatch. Top-level signed messages continue to use the ordinary application router and remain ante-owned.
- Wrap Wasmd's query handler to expose one Ark custom tax query. Intercept the proposed `wasmvm/v3` `CosmosMsg`, encode
  it with the same canonical encoder used by execution and the calling contract as sender, and invoke Treasury's
  calculator. Do not parse through a second Ark-owned message model.
- Install compatible callbacks around both transfer applications only after the Wasm keeper can receive them. The
  Classic effective inbound order is rate limit, packet forward, callbacks, transfer; the v2 effective order is rate
  limit, callbacks, transfer. Native Wasm IBC channels remain the path for custom contract protocols.
- Add the v2 GMP route only after the shared execution router is present. Add no separate IBC Hooks, ICS-721, or generic
  Wasm-to-ICA authentication surface in this slice.

The Wasm design must:

- Include instantiate/execute attached funds in ante-visible tax.
- Assess contract-generated Bank sends, IBC sends, execute funds, and instantiate funds at execution using the Section
  16.1 contract.
- Expose one read-only Ark custom query that accepts a proposed execution-generated message, parses it through the same
  message adapter used by dispatch, and returns the exact current per-denomination tax from Treasury's canonical
  calculator. Add no Wasm-owned tax formula or duplicate rate/cap query path.
- Treat the query response as advisory: it mutates and reserves nothing, does not validate future balances or authorise
  execution, and cannot be supplied back as an authoritative tax amount. Dispatch always recomputes against current
  state.
- Preserve independent per-input caps across multiple contracts and nested submessages.
- Keep top-level handlers outside the execution adapter without exempting a later, distinct execution-generated
  transfer.
- Roll back execution-generated tax with the matching transfer and enclosing cached execution according to Wasm reply
  semantics.
- Finish every Treasury fund credit through the restricted Bank send path.
- Add compatible callbacks around the installed Classic and v2 transfer applications only after the Wasm keeper can
  receive them. Callback-triggered execution follows the same execution-generated tax and rollback contract; callback
  delivery itself is not a taxable transfer.
- Preserve the middleware gas cap and callback authorization rules supplied by IBC-Go and Wasmd. A destination callback
  failure returns a failed receive result and rolls back the receive; acknowledgement and timeout callback failures
  follow the upstream non-blocking lifecycle semantics and must not undo completed protocol bookkeeping.
- Keep 08-Wasm client activation independent from contract-runtime activation. A compatible installation does not
  authorize the client at launch and adds no launch checksum; an incompatible dependency is omitted without blocking
  Wasm contracts.

Wasm foundation code may precede the final ante refactor in development, but it must remain production-disabled until
the complete Phase 4 activation matrix passes.

### 16.4 Ante and Treasury tax integration

Review the currently retained `app/treasury_ante.go` implementation against the approved Section 16.1 contract. Decide at
the file-level gate whether to retain that direct app layout or move the behavior into `app/ante`; do not refactor solely
to match a planned directory.

If the local ante package is selected, add:

- `app/ante/ante.go`: construct the stock SDK handler with Ark's checker, then wrap successful ante execution.
- `app/ante/fee_checker.go` and tests.
- `app/ante/tax_router.go` and tests.
- `app/ante/expected_keepers.go`.
- `app/ante/ante_test.go`.

Modify:

- `app/app.go`: replace the direct SDK `NewAnteHandler` call with the local builder and supply Treasury.

Do not copy or fork SDK signature decorators.

Reuse the Phase 1 calculator without changing its policy semantics. Complete only the ante-facing routing integration or
execution adapters, or correct a reviewed Phase 1 defect:

- `x/treasury/keeper/tax.go` and its existing concrete-message tests.
- `x/treasury/keeper/grpc_query.go` and tests for explicit `Any` unpacking.
- `x/treasury/types/expected_keepers.go` if routing needs an additional bank method.

Test:

- `MsgSend`.
- `MsgMultiSend` inputs receiving independent caps.
- Several messages from the same source receiving separate per-message caps.
- Multiple inputs receiving independent caps for the same denomination.
- Multiple taxable denoms.
- Nested and multiply nested `authz.MsgExec`.
- Malformed `Any` or authz contents.
- `MsgSwapSend` taxable stable offer.
- `MsgSwap` exemption.
- `anoah` fund deposits create no stability-tax principal.
- Taxable native-stable, mixed, and other non-NOAH fund principal is rejected atomically; tax classification does not
  bypass the recipient restriction, and ante-assessed tax retains the message-failure semantics defined in Section 8.6.
- NOAH and non-native-denom exclusion.
- Zero tax rate.
- Exact cap boundary and cap truncation.
- Explicit zero cap applies the full rate-derived tax without a ceiling.
- Missing configured cap fails closed.
- Checker, router, and query equality.
- Top-level IBC `MsgTransfer` and Wasm attached funds.
- Contract-generated Bank and IBC transfers assessed through the approved execution adapter.
- Ante-visible and execution-generated inputs sharing a source or denomination receive independent caps without double
  taxation.

### 16.5 Oracle reward files

Modify narrowly around the active Oracle refactor:

- `x/oracle/types/expected_keepers.go`: add `GetAllBalances`.
- `x/oracle/keeper/reward.go`: distribute every positive balance denomination.
- `x/oracle/keeper/reward_test.go`.
- `x/oracle/testutil/expected_keepers_mocks.go` by regeneration.
- Test setup files only where the interface change requires it.

No Oracle proto change is planned.

### 16.6 Activation test matrix and invariants

Before either transfer surface is production-enabled:

- Top-level IBC `MsgTransfer` and Wasm attached-funds principal must be included in ante tax computation.
- Contract-generated bank and IBC transfers must call the Phase 1 Treasury calculator at their actual execution
  boundary.
- Ante and execution adapters must treat each newly observed transfer input independently and must identify transfers
  already assessed by ante so no input is taxed twice.
- The sending contract must pay execution-generated tax in addition to principal; an insufficient balance rejects the
  transfer and tax atomically, and the adapter must never mint or borrow the tax.
- Every transfer must be assessed exactly once. Internal escrow, packet accounting, Market settlement, fund movement,
  claims, and reward distribution must not be mistaken for a second user transfer.
- Every enabled Wasm, IBC, authz, or other ingress that can credit a Treasury custody address must finish through the
  restricted Bank send path; it must not write balances directly or bypass final-recipient validation. Tests must prove
  that mixed and non-NOAH credits to every fund fail atomically.
- Failed or reverted execution-generated transfers must roll back their tax and transfer together.
- Tests must cover mixed top-level and execution-generated inputs from the same source receiving separate caps; distinct
  inputs; multiple contracts; nested Wasm submessages; outbound IBC; caught and uncaught submessage failures;
  insufficient tax balance; and no double taxation.

The exact adapter files follow the IBC and Wasm wiring approved under Sections 16.2 and 16.3. This activation gate itself
is not optional.

Ante and routing tests:

- Exact tax plus gas fee accepted.
- Insufficient tax rejected in CheckTx and block execution.
- Tax-only fee fails validator-local minimum gas price in CheckTx when configured.
- Gas overpayment remains in `fee_collector`.
- Priority excludes mandatory tax.
- Feegrant granter pays complete gas plus tax.
- Simulation performs no collection and no persistent writes.
- Invalid signature produces no persistent fee, tax, sequence, or feegrant change.
- Collection failure rolls back the complete ante.
- Message execution failure retains valid fee and tax but rolls back message state.
- Multi-denom collection moves the exact per-denom tax into `stability_tax_collector`.
- Target-aware allocation conserves every denomination exactly between validator and Oracle destinations.
- Validator tax rounds down per denomination and Oracle receives every integer remainder.

IBC and Wasm execution tests:

- Ark's canonical launch genesis starts ICS-20 send and receive disabled, permits only the 07-Tendermint client type,
  leaves both ICA sides disabled, gives ICA host no allowed messages, and contains no 08-Wasm checksums. If 08-Wasm is
  installed after its compatibility gate, its client type remains unavailable under this launch policy.
- Classic and v2 transfer reuse one transfer keeper and preserve their distinct routing and timeout semantics.
- Classic transfer has the effective inbound order rate limit, packet forward, callbacks, transfer; v2 has rate limit,
  callbacks, transfer. Classic packet forwarding still works through callbacks, while no v2 PFM route exists.
- Governance rate limits apply per denomination and channel/client, reject an excess before transfer execution, restore
  accounting correctly on failure/timeout, and are configured for every route before activation.
- Packet forwarding covers multi-hop success, downstream error, retry, timeout, and refund without assessing a second
  stability tax or bypassing the final Bank recipient restriction.
- ICA controller and host traffic fails while disabled; host activation accepts only its explicit type-URL allowlist and
  never a wildcard policy. No generic custom ICA authentication route exists.
- Classic and v2 callbacks cover source send, destination receive, acknowledgement, timeout, malformed metadata,
  callback rejection, out-of-gas behavior, and the exact rollback or non-blocking lifecycle semantics defined for each
  callback stage.
- GMP derived-account authentication rejects incorrect or multi-signer payloads. Every accepted taxable GMP SDK message
  uses the shared Wasm execution router, charges the derived account exactly once, and rolls tax back with failed
  dispatch or packet receipt.
- Top-level IBC and Wasm principal is assessed by ante exactly once.
- Execution-generated Bank, IBC, execute-funds, and instantiate-funds principal is assessed at execution exactly once.
- The Wasm tax query matches immediate execution for each supported message shape and denomination, including cap
  boundaries and multiple inputs, while malformed or unsupported messages and missing required cap state fail closed.
- The Wasm tax query performs no writes, reserves no balance, and cannot override the tax recomputed at dispatch.
- Multiple contracts, nested submessages, and distinct execution-generated inputs receive independent caps.
- Caught and uncaught submessage failures, IBC send failures, and insufficient execution-generated-tax balances roll
  back the tax and matching transfer at the approved boundary.
- Escrow, packet accounting, acknowledgement, timeout, refund, and internal module movements create no duplicate tax.
- Every attempted mixed or non-NOAH Treasury fund credit fails atomically through the Bank restriction.
- The application registers no 06-Solo Machine, attestations, ICS-721 NFT transfer, IBC Hooks, or v2 PFM support and no
  ICS-29 relayer-fee wiring. `09-localhost` is unavailable under the launch allowed-client policy.

Oracle reward invariants:

- Per-denom distribution never exceeds available Oracle balance.
- Sum of validator allocations equals the Oracle-to-Distribution transfer exactly.
- Rounding dust remains in Oracle.
- A missing validator's share is not redirected.
- No scores or no funds means no payout.
- Score state clears only after successful settlement according to the existing Oracle lifecycle.

### 16.7 Phase 4 verification

```sh
GOCACHE=/private/tmp/ark-gocache go test ./app/...
GOCACHE=/private/tmp/ark-gocache go test ./x/treasury/...
GOCACHE=/private/tmp/ark-gocache go test ./x/oracle/...
GOCACHE=/private/tmp/ark-gocache go test ./x/wasm/...
GOCACHE=/private/tmp/ark-gocache go test ./...
git diff --check
```

Also run every new local IBC/Wasm adapter package test selected by the approved file-level plan. Review tax semantics,
the real block lifecycle, execution-generated transfer rollback, and production activation state before Phase 5.

## 17. Phase 5: Full-system invariants, simplification, and launch readiness

Status: **Pending Phase 4 approval**

### 17.1 Full-app tests

Add:

- `app/monetary_policy_test.go` for deterministic lifecycle and supply invariants.
- `app/monetary_policy_sim_test.go` if a bounded stateful simulation is stable and maintainable.
- Property tests under Market or Treasury only where they provide more signal than table tests.

Exercise:

- No activity across many blocks.
- Sustained expansion until all three targets fill and overflow burns.
- Same-block sequential expansions, proving the first aggregate valuation reads canonical supply, each later
  `RouteExpansion` observes preceding fund balances and the delta-maintained transient liability, adds only its own
  output for allocation, settles immediately, and leaves no pending amount for a block or epoch hook.
- Sustained coverage-based Buffer use across full, underfunded, near-zero, empty, and overfunded states.
- Same-block sequential redemptions and split-versus-unsplit rounding behavior, proving each successful burn advances
  the transient liability before the next draw.
- Block-boundary reset, proving the next block rebuilds the snapshot from canonical Bank supply and produces the same
  result as a fresh direct valuation.
- Final outstanding stable liability with Buffer smaller than, equal to, and larger than quoted output.
- Alternating expansion and redemption.
- Price and liability changes that move targets up and down.
- Stale rate for one native stablecoin during expansion.
- Priceable redemption while an unrelated nonzero-supply stable rate is stale.
- Strategic Reserve isolation throughout ordinary conversion and reward flows.
- Empty and nearly empty subsidy pool.
- Permissionless `anoah` subsidy-pool deposits before and after depletion, including subsidy extension/restart,
  unchanged payout rates, unchanged total supply, and rejection of mixed/non-NOAH deposits.
- Permissionless Buffer `anoah` deposits before redemption, including unchanged quotes and pool state, increased
  coverage-based existing-NOAH funding where applicable, reduced residual mint, unchanged total supply, and rejection of
  mixed/non-NOAH deposits.
- Permissionless Reserve and Insurance `anoah` deposits, including over-target deposits, unchanged total supply, no
  automatic conversion or Reserve action, and no depositor ownership, withdrawal, authority, coverage, or claim-priority
  rights; mixed and non-NOAH deposits from every ingress fail atomically.
- Governance Reserve-to-Buffer commitments before and after ordinary redemptions, including partial and complete
  commitment, Buffer over-target, Reserve below-target, an explicitly permitted zero remaining Reserve balance,
  insufficient balance, minimum-remaining failure after another proposal, Bank-send rollback, and stale/incomplete
  Oracle valuation. Each successful action is one-way, changes no supply or quote, and becomes shared coverage-based
  Buffer inventory for subsequent complete-valuation redemptions.
- Repeated Reserve-to-Buffer commitments followed by expansion, proving that live balance gaps—not a deployment ledger—
  govern recapitalisation and that any Reserve refill is bounded by realised eligible expansion principal while
  Insurance funding and overflow burn may be delayed.
- Claims Mandate disabled/default and configured states, monotonic replacement/disable terms, half-open appointment
  windows, committee rotation, and rejection of cross-role use, including a Claims committee attempting Reserve or
  parameter messages and a future Reserve executor attempting Claims.
- Actual threshold-multisig Insurance submission followed by the shared cancellation delay and permissionless execution;
  exact half-open cancellation/execution boundary; overlapping pending claims; exact reservation accounting; committee
  and governance cancellation; stale expected-term rejection; committee rejection for governance-submitted claims;
  governance cancellation after mandate replacement or expiry; Bank rollback; replay rejection; and immutable
  recipient/amount/reference/mandate-term fields.
- Insurance approval during active Market flow, followed by expansion before execution, proving approval opens the
  passive gap, eligible expansion may fill it, and execution reduces balance/reservation together without a second gap
  shock or any supply change.
- Repeated submit/cancel, submit/execute, deposit, and expansion-refill sequences proving reservations cannot become a
  claims/refill drain loop and mandate replacement never rewrites an existing claim or reservation.
- Zero, mixed, non-NOAH, over-balance, wrong-mandate-term, inactive-mandate, expiry-crossing, and duplicate claims fail
  at the correct approval or execution boundary.
- Mixed stablecoin tax balances in Oracle.
- Mixed bank, Market-send, Wasm, and IBC transfers receiving independent caps per taxable input, including multiple
  inputs from the same source.
- Successful and failed Monetary Policy cap-amount changes, plus governance-only Params denomination changes.
- Successful and failed same-denom BasePool resizes and live `asdr`-to-basket pool-denom changes, including zero, small,
  and arbitrarily large differences between submitted and applied amounts that do not independently reject execution.
- Stable-to-stable quote equality before and after a pool-denom transition.
- Treasury liability, target, and coverage-funded-output equality before and after both a pool-denom transition and a
  reference-tax-cap denomination change.
- Missing SDR pricing with zero `asdr` supply versus nonzero `asdr` supply.
- `FundStatus` with basket pricing missing when basket is only the Market pool unit versus when basket supply is
  outstanding; separately prove a NOAH/stable Market quote still fails when its required pool-unit rate is stale.
- `RewardFunding` remains queryable without Oracle calls when `FundStatus` cannot produce a complete valuation.
- Bidirectional `asdr` conversions remain valid after the basket transition, including NOAH-to-`asdr` and
  basket-to-`asdr` outputs, and every flow updates the single basket-denominated virtual pool where applicable.
- Target ceiling and redemption-floor rounding errors each remain below one base-unit NOAH where their respective caps do
  not bind.
- Governance parameter updates.
- Every bank/mint/burn failure point.
- Genesis export/import, preserving every `anoah`-only fund balance, Claims Mandate term and appointment, pending Claims
  reservations, every pending/cancelled/paid claim and its origin and mandate term, the exact in-progress Reward Funding
  state, and governance proposals that authorised a Reserve-to-Buffer commitment or exact claim; imported non-NOAH fund
  balances and inconsistent claim reservations fail genesis validation.

### 17.2 Global invariants

After every relevant transition assert:

```text
Only Market has Minter; Market retains Burner for conversion settlement.

No Market conversion:
  Delta total NOAH supply = 0

NOAH -> stable:
  eligible_principal_noah = buffer_credit + strategic_reserve_credit + insurance_credit + overflow_burn
  gross_offer_noah = eligible_principal_noah + spread_and_dust_burn
  total_noah_burn = spread_and_dust_burn + overflow_burn
  gross_offer_noah = buffer_credit + strategic_reserve_credit + insurance_credit + total_noah_burn
  RouteExpansion is called exactly once before burn/mint/payout
  Treasury subsidy-pool balance receives none of gross_offer_noah
  Treasury has no pending or periodic expansion-allocation state

stable -> NOAH:
  redeemed_liability_noah = value(gross_stable_offer, NOAH)
  0 < noah_output <= redeemed_liability_noah
  complete valuation => redeemed_liability_noah <= pre_trade_aggregate_liability_noah
  complete valuation => buffer_coverage = min(1, pre_trade_buffer / pre_trade_aggregate_liability_noah)
  complete valuation => buffer_paid = floor(noah_output * buffer_coverage)
  incomplete valuation => buffer_paid = 0
  noah_output = buffer_paid + residual_noah_mint
  0 <= buffer_paid <= min(pre_trade_buffer, noah_output)

Tax:
  total_assessed_tax = ante_tax + sum(execution_generated_input_tax)
  before settlement: stability_tax_collector accumulates every committed assessed-tax coin exactly
  at settlement: window_tax = oracle_tax_credit + validator_tax_credit
  aggregate_validator_target = sum(applied validator_block_reward_target for each observed block)
  aggregate_oracle_target = sum(applied oracle_block_reward_target for each observed block)
  aggregate_eligible_gas_value = sum(contemporaneously valued eligible fee_collector balances)
  validator_pre_tax_gap = max(aggregate_validator_target - aggregate_eligible_gas_value, 0)
  protected_oracle_tax_value = min(eligible_window_tax_value, aggregate_oracle_target)
  desired_validator_tax_value = min(eligible_window_tax_value - protected_oracle_tax_value, validator_pre_tax_gap)
  stability_tax_collector balance = 0 after every successful allocation
  if aggregate_eligible_gas_value >= aggregate_validator_target: validator_tax_credit = 0
  if eligible_window_tax_value <= aggregate_oracle_target: validator_tax_credit = 0
  empty reward-funding state => blocks_remaining = 0 and canonical zero aggregates
  successful non-boundary reward-funding state => blocks_remaining > 0
  first observation initializes blocks_remaining from Params.reward_funding_window before decrementing
  positive blocks_remaining is unchanged by a reward_funding_window parameter update
  unavailable required valuation => validator_tax_credit = 0 and subsidy_paid = 0 at settlement
  TaxCaps[denom] = 0 => assessed_tax[input][denom] = floor(taxable_principal[input][denom] * rate)
  TaxCaps[denom] > 0 => assessed_tax[input][denom] = min(floor(taxable_principal[input][denom] * rate), TaxCaps[denom])
  total_assessed_tax[denom] = sum(assessed_tax[input][denom] for every taxable input)
  each economic transfer is assessed exactly once

Reference tax cap change:
  Params reference Coin and the complete derived TaxCaps map change together or neither changes
  Monetary Mandate does not change
  Market BasePool, ArkPoolDelta, quotes, fund balances, and coverage-based redemption funding do not change

Market BasePool change:
  new ArkPoolDelta / new BasePool.Amount = old ArkPoolDelta / old BasePool.Amount within the rounding bound
  every amount or denomination change rescales ArkPoolDelta atomically with Market Params
  Market owns the complete transition and makes no Treasury keeper call

Market pool denomination change:
  submitted BasePool.Amount is a non-binding expected new-denom amount for audit
  one fresh deterministic live-derived amount is applied regardless of the submitted expectation
  stored BasePool.Amount is the live-derived amount
  ArkPoolDelta is rescaled into stored BasePool.Denom without a reset
  Market Params and ArkPoolDelta change together or neither changes
  Delta bank balances and supplies = 0
  Treasury Params.reference_tax_cap does not change
  Treasury nominal liability, target exposure, targets, and coverage-based redemption funding do not change
  stable-to-stable quote does not change under the same offer/ask rates

Supported stable outputs:
  BasePool.Denom does not determine mint eligibility
  asdr remains valid as both offer and ask after the basket transition
  changing the pool denomination adds no output ban

Subsidy pool:
  validator_paid + oracle_paid <= pre_block_subsidy_pool_balance
  validator_paid <= validator_shortfall
  oracle_paid <= oracle_shortfall
  accepted subsidy-pool deposit contains only anoah
  Delta total supply from a subsidy-pool deposit = 0
  subsidy-pool deposit changes neither validator_block_reward_target nor oracle_block_reward_target
  subsidy pool receives no automatic tax, fee, expansion, Buffer, Reserve, Insurance, mint, or conversion refill

Funds:
  Redemption Buffer >= 0
  strategic Reserve >= 0
  Insurance >= 0
  every accepted fund deposit contains only anoah
  final recipient after the complete Bank restriction chain satisfies the same rule
  mixed or non-anoah inbound user, module, Wasm, or IBC credit to any fund fails atomically
  a direct user deposit rejected by Bank send-enabled policy never reaches any fund
  Delta total supply from every direct fund deposit = 0
  each fund's complete launch custody is anoah
  Buffer and Reserve balance counted toward target = their anoah Bank balance
  Insurance unencumbered balance = Insurance anoah Bank balance - Insurance reserved
  deposits create no refund, ownership, withdrawal, coverage, priority, governance, or deployment right
  no deposited asset is converted automatically
  targets alone move no money
  only Redemption Buffer funds Market automatically
  ordinary redemption never directly debits strategic Reserve
  strategic Reserve never directly pays a redeemer, reward, or claim
  governance Reserve commitment: Delta Reserve anoah = -amount
  governance Reserve commitment: Delta Buffer anoah = +amount
  governance Reserve commitment: Delta total supply, liability, quote, pool state, tax, subsidy pool, and Insurance = 0
  only MsgTransferReserveToBuffer names strategic Reserve as a launch bank-send source
  Reserve commitment source, destination, and anoah denomination are fixed internally
  Reserve post-transfer balance >= the proposal's minimum_reserve_balance
  Reserve commitment succeeds without Oracle or target valuation and stores no duplicate Treasury history
  no Buffer-to-Reserve or arbitrary-recipient Reserve path exists
  Insurance never funds Market
  Claims Mandate committee is distinct from Treasury general authority and the monetary-policy committee
  each Claims Mandate replacement or disablement advances the chain-derived term
  Claims Mandate active iff committee is nonempty and activation_height <= h < expiry_height
  committee and governance submit under the same active term, validation, and held-balance rules
  0 <= committee term used <= committee claim limit
  committee claim submission: Delta committee term used = amount
  governance claim submission: Delta committee term used = 0
  claim cancellation or execution: Delta committee term used = 0
  each submitted claim stores its mandate_term and executable_height <= mandate expiry_height
  current active committee with exact current term may cancel only non-governance-origin pending claims before executable_height
  governance may cancel any pending claim before executable_height regardless of the current Claims Mandate
  cancellation is unavailable to both roles at or after executable_height
  permissionless execution is available at or after executable_height
  0 <= Insurance reserved <= Insurance anoah Bank balance
  claim submission: Delta Insurance Bank balance = 0
  claim submission: Delta Insurance reserved = amount
  claim submission: Delta Insurance unencumbered balance = -amount
  claim execution: Delta Insurance Bank balance = -amount
  claim execution: Delta Insurance reserved = -amount
  claim execution: Delta Insurance unencumbered balance = 0 relative to immediately before execution
  claim cancellation: Delta Insurance Bank balance = 0
  claim cancellation: Delta Insurance reserved = -amount
  mandate replacement or disablement resets Claims allowance used to zero without rewriting claims or Insurance reserved
  claim execution consumes exactly its stored reservation and leaves Insurance balance >= remaining reservations
  every settled claim is irreversible
  every Insurance claim performs no mint, borrow, cross-fund debit, conversion, or Market call
  an Insurance deposit alone never authorises a claim
  liability and target-exposure values are direct anoah equivalents
```

Also assert every successful `MintCoins` call belongs to Market conversion settlement, distinguishing stablecoin output
minting from residual NOAH minting. For complete aggregate valuation and a non-final redemption, define
`liability_after = liability_before - redeemed_liability` from the same fixed rate snapshot and assert non-decreasing
actual coverage without division:

```text
buffer_after * liability_before >= buffer_before * liability_after
```

### 17.3 Simplification pass

After behavior is proven:

- Remove obsolete mocks, helpers, comments, events, queries, and simulations left by deleted controller code.
- Remove wrappers that only proxy public collections without domain logic.
- Keep tax calculation single-sourced.
- Keep target calculation single-sourced.
- Keep expansion-principal valuation and the complete Buffer → Reserve → Insurance → overflow waterfall single-sourced
  in Treasury. Market consumes the successful `ExpansionAllocation`, including its derived total burn, without
  revalidating or recomputing Treasury policy.
- Keep the coverage-based Buffer draw and its `LegacyDec` conversion path single-sourced.
- Keep Market's three settlement paths explicit rather than hiding materially different supply effects behind generic
  transfer helpers.
- Confirm no unused Treasury-to-protocolpool or Treasury-to-staking dependencies remain.
- Confirm no dead custom Wasm Treasury adapter remains.
- Refresh package documentation to describe current ownership rather than Terra inheritance.

### 17.4 Final verification

```sh
gofmt -w <changed-go-files>
make proto-format
make proto-lint
make proto-gen
GOCACHE=/private/tmp/ark-gocache go test ./x/treasury/...
GOCACHE=/private/tmp/ark-gocache go test ./x/market/...
GOCACHE=/private/tmp/ark-gocache go test ./x/oracle/...
GOCACHE=/private/tmp/ark-gocache go test ./app/...
GOCACHE=/private/tmp/ark-gocache go test ./...
GOCACHE=/private/tmp/ark-gocache go build -o /private/tmp/arkd-phase5 ./cmd/arkd
git diff --check
```

If Docker access blocks proto lint/generation, report that environment failure separately and do not claim those gates
passed.

## 18. Fresh-genesis launch rules

Ark is confirmed prelaunch. No existing chain state, client wire compatibility, or historical module account balance
must survive this redesign. Implementation must therefore:

- Set Treasury consensus version to 1 and register no state migrator.
- Use only the compact protobuf tags and collection prefixes defined in Section 10.
- Delete controller-era proto fields, generated compatibility surfaces, store keys, codecs, and keeper logic outright.
- Remove Mint entirely from app configuration and genesis; do not preserve its store, query service, module account, or
  module-version entry.
- Allocate the initial subsidy pool, Redemption Buffer seed, strategic Reserve seed, and Insurance seed through bank
  genesis.
- Set `app_state.distribution.params.community_tax` to zero in the canonical launch genesis. Do not treat the SDK's
  generic `arkd init` output as Ark's launch configuration.
- Launch with all four Treasury custody accounts unblocked for inbound bank sends and the recipient-aware restriction
  active: every fund accepts only positive `anoah`; every mixed or non-NOAH credit fails atomically after the complete
  restriction chain.
- Require every fund's bank-genesis balance to contain only `anoah`; reject genesis otherwise.
- Initialise the approved Claims Mandate term, committee, half-open activation/expiry window, fixed gross claim limit,
  zero Claims allowance used, and zero Insurance reservation; the shared cancellation period ships in Treasury
  `Params`. The committee is an ordinary account
  distinct from Treasury authority and every other Treasury role; it is not a fund custodian.
- Create the committee BaseAccount and give it an explicit non-Treasury fee path. Do not seed it from the Insurance
  account or grant it a generic fee or Bank authorization.
- Initialise Market `BasePool` as the approved positive `sdk.DecCoin` in `asdr` and `ArkPoolDelta` to zero in that unit.
- Supply launch Params with the intended `reference_tax_cap` Coin in `asdr` and a complete derived cap map when genesis
  Oracle prices cannot derive a positive reference cap deterministically. A zero reference cap derives a complete
  explicit-zero uncapped map without Oracle prices.
- Ensure bank supply exactly equals user balances plus every module-account allocation.
- Discard and regenerate any developer or test genesis that uses the old Treasury or Mint schema.

Do not implement frozen legacy types, old-to-new converters, reserved compatibility tags, store-prefix shims, or an
upgrade handler. If such code appears during implementation, remove it rather than treating it as a future requirement.

Fresh launch does not weaken post-launch export correctness. Export/import tests must preserve the new Treasury params,
tax caps, Claims Mandate, Claims allowance used, Insurance reservation, every Insurance claim status/submission
snapshot, all bank-held fund balances, total supplies, denomination-bearing `BasePool`, and labelled `ArkPoolDelta`;
governance export/import must preserve any Reserve-to-Buffer or exact-claim authorising proposal and outcome. Only the
initial launch genesis starts with zero `ArkPoolDelta`.

Launch-readiness TODO: define the reproducible build process for Ark's canonical `genesis.json`, generate the artifact,
and review it before launch. At minimum, the review must confirm zero Distribution `community_tax`, absence of Mint,
approved module-account permissions, initial Treasury fund balances, economic parameters, total-supply conservation,
an IBC allowed-client list containing only `07-tendermint`, disabled ICS-20 send and receive, disabled ICA controller and
host, an empty ICA host message allowlist, and no 08-Wasm client checksums. This is a Phase 5 launch-readiness
deliverable, not Phase 2 application wiring.

## 19. Launch economic configuration

Architecture tests cannot choose sustainable economic values. Before launch, explicitly approve:

| Parameter / balance            | Required decision                                                                             |
| ------------------------------ | --------------------------------------------------------------------------------------------- |
| Stability tax rate             | Fixed launch percentage                                                                       |
| Reference tax cap              | Launch Coin amount and denomination; recommended initial denomination `asdr`                  |
| Validator block reward target  | Minimum aggregate validator-funding value per block in base-unit NOAH                             |
| Oracle block reward target     | Minimum aggregate Oracle-funding value per block in base-unit NOAH                                |
| Reward funding window          | Settlement observations per window; default recommendation is one chain week                  |
| Subsidy pool genesis balance   | Must imply intentional zero-revenue shortfall coverage; later `anoah` deposits may extend it  |
| Redemption Buffer target ratio | Operational NOAH inventory target as a percentage of stable liability                         |
| Strategic Reserve target ratio | Retained emergency-capacity target as a percentage of stable liability                        |
| Insurance target ratio         | Covered-loss capacity target as a percentage of stable liability                              |
| Redemption Buffer genesis seed | Existing NOAH assigned to coverage-based redemption funding                                   |
| Strategic Reserve genesis seed | Existing NOAH retained from automatic use; governance may commit `anoah` to the shared Buffer |
| Insurance genesis seed         | Existing NOAH recommended for immediate target capacity                                       |
| Claims committee               | Role-specific Legacy Amino threshold-multisig address, membership, threshold, and role keys   |
| Claims timing                  | Half-open committee activation/expiry heights and cancellation blocks before execution        |
| Claims role fees               | Own genesis balance or separately approved narrow fee grants; never automatic Insurance spend |
| BasePool                       | Launch virtual conversion depth as a positive `sdk.DecCoin` denominated in `asdr`             |
| Pool-denom submitted amount  | Non-binding expectation retained in the transaction; event exposes old and applied state; no rejection threshold |
| MinStabilitySpread             | Minimum conversion friction                                                                   |
| PoolRecoveryPeriod             | Exponential imbalance decay rate                                                              |
| Residual mint limit            | None; Phase 3A and P2 found no need for a hard cap                                             |
| Oracle reward windows          | Tax-reward smoothing horizon                                                                  |
| Distribution community tax     | Zero at launch                                                                                |
| Transfer-surface activation    | Phase 4 implements IBC before Wasm; both remain production-disabled until their complete tax and recipient-restriction integrations pass |

There is no global launch Reserve-withdrawal floor, rolling deployment cap, or price/target trigger to configure. Each
governance Reserve-to-Buffer proposal states its exact `anoah` amount and minimum remaining Reserve balance. A future
operational executor would require a separate bounded mandate; do not preconfigure one for launch.

The Claims Mandate values above are P4 launch decisions. The Claims committee multisig address controls only typed claim
submission and eligible cancellation during its active appointment and current term. Its balance is for gas, not
Insurance custody; deposits or expansion allocations to Insurance increase available held coverage but grant the
committee no generic send or custody authority. Governance may replace or disable the appointment at any time, submits
under the same active term and deadline, and may cancel a pending claim before its executable height regardless of the
current appointment.

The zero-organic-revenue coverage estimate, when the sum of reward targets is positive, is:

```text
worst_case_coverage_blocks = current_subsidy_pool_noah /
                             (validator_block_reward_target + oracle_block_reward_target)
```

This is a conservative display estimate, not stored consensus state. Actual duration is longer whenever fees or Oracle
tax cover part of either target and changes as organic revenue and deposits vary. Tax revenue, fees, expansion
principal, and fund balances do not automatically refill the subsidy pool. Stablecoin tax cannot become NOAH subsidy
funding without a conversion, and no automatic conversion belongs in the initial design. Permissionless direct `anoah`
transfers are the only launch replenishment path. Permissionless deposits to the other funds are donations to their
distinct mandates and never refill the subsidy pool.

## 20. Deferred extensions and required post-launch sequence

The launch implementation must not contain dormant multi-asset custody, generic Reserve send authority, an operational
hot key, or placeholder recognition policy. Add those capabilities only when the chain has a concrete risk model and a
first external asset or action to approve. The required sequence is:

1. Launch and verify the NOAH-only model in Phases 0 through 5, including the bounded Claims Mandate and claims
   committee multisig. Governance remains the only actor that can invoke the fixed Reserve-to-Buffer message; no Reserve
   executor exists yet.
2. Replace total native stable supply as the Reserve and Insurance target-exposure proxy with an approved fund-specific
   covered-risk-exposure model while custody and recognised capital remain NOAH-only. This changes the required-capital
   side of the equation, not the assets counted against it. The Redemption Buffer remains an operational ratio against
   outstanding stable liability unless separately reconsidered.
3. With the first proposed external asset, approve and implement its exact custody identity, valuation source, haircuts,
   liquidity treatment, concentration limits, and per-fund eligibility. Recognition must be live and audited before any
   Reserve mandate can deploy NOAH to acquire that asset.
4. Add the smallest typed Reserve action and bounded mandate needed for the approved deployment. Governance creates the
   mandate; a threshold multisig may execute only inside it; a separate guardian may only pause it.
5. Activate the mandate only after its adapter, accounting lifecycle, failure rollback, queries, events, and invariants
   pass their own review gate. A successful deployment receives zero target credit until the acquired asset is settled,
   controlled, fresh-priced, and otherwise recognisable.
6. Add each later asset, venue, counterparty, action, or Insurance payout denomination through another explicit policy
   approval. Never turn the first allowlist into an `any bank denom` or arbitrary-call facility.

Phases 6 through 8 in Section 21 record this order but are not authorised launch work.

### 20.1 Risk-exposure targets and external-asset recognition

Required Reserve and Insurance capital remains a percentage of the risk that each fund covers. Their exposure models may
differ: Reserve capacity and covered Insurance loss exposure are not automatically the same scalar. Assets held by a
fund satisfy its requirement; they do not define or inflate it. Section 7.2's `required_capital`, `recognised_capital`,
`capital_gap`, and liquid-gap formulas are the mandatory accounting contract.

Each of Reserve and Insurance gains two distinct governed policy sets only when its first non-NOAH asset is approved:

1. A custody/payout allowlist defining exact assets that the account may receive and, for Insurance, potentially pay.
2. A target-credit eligibility subset defining which allowed assets reduce that fund's target gap and by how much.

The second set is always no broader than the first. Custody, payout eligibility, and target recognition are separate
facts. An external asset may be allowed for in-kind Insurance payment but receive zero capital credit. Conversely, a
recognised Reserve asset may be prohibited from direct Insurance payout. Ark-issued stablecoins always receive zero
target credit because moving a consolidated liability into a fund does not extinguish it.

For an affected fund, Phase 7 changes the recipient restriction from `anoah`-only to `anoah` plus its exact
custody-allowlist entries. Any unlisted coin makes the complete transfer fail atomically; whether one transfer may
contain several listed assets is decided explicitly with the first asset. The change must handle native Bank, IBC, Wasm,
and contract-token custody through explicit adapters rather than assuming every asset is represented by an ordinary Bank
denomination.

An eligibility entry must identify:

- the exact native denomination, token contract, or complete IBC denomination trace;
- the permitted fund or funds and whether custody, payout, and target-credit eligibility apply;
- an approved conservative price source and maximum age;
- decimal and unit conversion rules;
- credit/custody and liquidity factors;
- per-asset, counterparty, and correlated-group concentration caps, including aggregate caps across Reserve and
  Insurance;
- encumbrance, settlement, receipt-token, and external-control rules;
- the immediately usable portion for the fund's liquid-capital requirement;
- explicit behavior for stale price, halted venue, IBC in-flight state, custodian impairment, and governance removal.

Recognition reads only explicit eligibility entries. It must never iterate arbitrary custody and decide that every
priced balance is capital. Missing, stale, pending, encumbered, uncontrolled, unlisted, or over-cap value receives zero
current credit. Loss of recognition may reopen an expansion-allocation gap but must never itself mint, trade, liquidate,
withdraw, or invoke a Reserve action. Queries and events must expose gross held amount, conservative value, recognised
value, liquid value, applied caps/haircuts, and any zero-credit reason separately.

Changing the target-exposure model or recognition policy requires deterministic replay tests showing that:

- required capital changes only with covered risk exposure and its configured ratio;
- held assets change only recognised capital and the resulting gap;
- one NOAH balance can satisfy both total and liquid requirements without double-counting the two gaps;
- an asset cannot be counted in two economic forms, such as both an underlying balance and its receipt token;
- splitting correlated exposure across Reserve and Insurance cannot evade a system-wide cap;
- an Ark stablecoin remains a full liability and receives zero target credit wherever it is held;
- no recognition transition changes total supply or triggers a movement of assets; and
- spending NOAH for a haircut-recognised asset reopens only the unrecognised portion of the gap, preventing both a
  full-credit solvency illusion and a zero-credit acquisition/refill feedback loop.

### 20.2 Committee roles, monetary-policy authority, Reserve policy authority, and fast execution

Treasury's general `authority` remains the governance module account because it also controls `MsgUpdateParams` and
other policy decisions. Do not assign it to an emergency key, multisig, Claims Mandate role, or operational executor. Do
not add a global `reserve_authority` or a generic `MsgSendReserve`.

Ark uses role-scoped authority accounts:

```text
root policy authority         = x/gov
monetary-policy committee     = threshold multisig A
Insurance claims committee    = threshold multisig B
future Reserve executor       = threshold multisig C
future Reserve guardian       = separate account or multisig D
```

Human signers may overlap initially, but every role uses a distinct on-chain address so monetary policy, claims, and
future Reserve actions can have different thresholds, rotation, audit history, and compromise boundaries. No role
inherits another merely because its human membership overlaps.

The implemented monetary mandate delegates only:

```text
stability_tax_rate
validator_block_reward_target
oracle_block_reward_target
redemption_buffer_target_ratio
strategic_reserve_target_ratio
insurance_target_ratio
```

Governance appoints, replaces, or disables one exact committee address through `MsgSetMonetaryMandate`. Each appointment
stores the chain-derived `uint64` term with the increment behavior specified in Section 10.5, half-open
`activation_height <= height < expiry_height`, and complete minimum and maximum policies. Committee transactions name
the expected term and replace the entire reversible policy subset atomically; stale-term, inactive, out-of-bounds, and
partial updates fail. Normal account authentication enforces the configured threshold multisig, after which Treasury
uses exact address equality for role authorization.

Governance retains both override levers. It may sign its own `MsgUpdateMonetaryPolicy` to apply a structurally valid
candidate outside committee bounds, or replace/disable the mandate immediately. `MsgUpdateParams` remains governance-only for
`reward_funding_window` and the complete reference-cap Coin. Reference-cap changes rebuild the derived cap map without
changing the committee mandate. Separate queries expose the current Monetary Policy and the stored mandate; mandate
activity is derived from the current height.

The Claims committee and governance may submit any unique positive `anoah` claim covered by the live Insurance balance.
Committee submissions additionally consume the fixed gross allowance for the current mandate term; governance
submissions do not. Both may cancel before the same executable height, but the committee cannot cancel a
governance-submitted claim. Governance may replace the committee but cannot cancel after the shared deadline or claw
back a paid claim. The monetary-policy committee cannot submit/cancel claims, move fund custody, change governance-owned
Params, or invoke the governance-only Reserve transfer.

For future fast action, governance may create a Treasury-native Reserve mandate with:

```text
policy authority = x/gov
mandate executor = ordinary threshold multisig account
mandate guardian = separate pause-only address
```

The executor is an ordinary Legacy Amino threshold-multisig account address whose signatures are checked by the normal
account/authentication path; no new app module is required merely to use it. Do not grant a broad `bank.MsgSend`
authorization through `x/authz`, and do not add `x/group` or `x/circuit` merely to recreate policy that Treasury must
enforce itself. Before mandate activation, ensure every new executor or guardian signer account exists and has an
explicit non-Reserve fee path; Reserve custody never pays operational gas automatically.

Governance alone may create, widen, renew, resume, replace, or revoke a mandate. It approves the purpose, exact assets,
pricing and haircuts, typed adapters, venues and counterparties, destinations and IBC routes, caps, retained liquid
floor, activation/expiry, executor, and guardian. The executor may perform only a typed action satisfying the live
mandate. It cannot add an asset or destination, change a haircut, extend time or limits, unpause itself, pay validators
or Oracle rewards, pay Insurance claims, transfer to users, or alter Treasury parameters. The guardian may pause only;
governance must resume or replace the mandate.

Governance override remains typed. It may create and execute a one-shot exact Reserve mandate or use a dedicated
governance-signed typed action, but it may not bypass adapters, settlement accounting, asset/destination restrictions,
held-balance checks, or no-mint/no-borrow/no-cross-fund invariants through a generic send. Likewise, the claims
committee cannot invoke Reserve actions and the Reserve executor cannot approve Insurance claims.

A new or widened mandate may include a governance-approved activation delay so changes are visible before authority
becomes usable. Narrowing applies the narrower terms immediately to every later new or risk-increasing deployment;
pausing, revocation, and expiry block all such deployments. None may strand an already-open position: authenticated
acknowledgements, timeouts, inbound returns, accounting reconciliation, and tightly bounded risk-reducing actions follow
the separate lifecycle rules below. Once an approved mandate is active, individual risk-increasing executions need no
governance vote or artificial cooldown; rolling caps and the multisig threshold bound their speed. A standing emergency
mandate should therefore be approved before stress rather than widened during it.

The future policy must choose either heights or timestamps consistently and use the half-open predicate:

```text
new_or_risk_increasing_allowed =
  configured_state == active &&
  activation <= current_point < expiry
```

Queries expose configured state separately from a derived effective status: before activation it is `pending`, within
the interval it follows configured `active` or `paused`, configured revocation is `revoked`, and at or after expiry it
is `expired`. Resume changes only configured `paused` to `active`; it cannot make an expired mandate usable. Governance
must renew or replace the mandate with a newly reviewed expiry.

The deferred Treasury API should use narrow messages rather than arbitrary call data:

```text
MsgCreateReserveMandate     // governance signer
MsgExecuteReserveMandate    // configured executor signer
MsgPauseReserveMandate      // configured guardian signer
MsgRevokeReserveMandate     // governance signer

ReserveMandates Map[mandate_id]ReserveMandate
ReserveDeployments Map[(mandate_id, execution_id)]ReserveDeployment // only for asynchronous or open positions
```

Whether resume is a dedicated governance message or part of governance-only mandate replacement is decided with the
future protobuf review. No launch proto or state slot is reserved for these messages.

### 20.3 Minimum mandate contents and accounting

A mandate must contain at least:

- bounded unique ID, purpose, activation time/height, expiry, configured active/paused/revoked state, and derived
  pending/active/paused/revoked/expired effective status;
- executor and separate guardian addresses;
- exact typed actions, assets, adapters, destinations, counterparties, and IBC channels/ports/receivers; an upgradeable
  adapter must have its code identity/checksum or administrative upgrade boundary pinned by policy;
- per-transaction, rolling-window, and lifetime gross-outflow caps;
- maximum outstanding exposure and per-asset/counterparty/correlated-group limits;
- minimum immediately liquid `anoah` remaining in Reserve;
- required Oracle sources, maximum price age, maximum slippage, minimum receive, and transaction deadline;
- gross spent, returned capital, outstanding principal/exposure, realised loss, and remaining allowances; and
- event fields sufficient to connect each execution, acknowledgment, return, write-down, pause, and revocation to the
  governance mandate.

Each execution ID is unique within its mandate. Asynchronous actions and open positions persist the terms effective at
execution plus their pending/settled/returned/timed-out/impaired state; later mandate edits must not rewrite the
historical authorization or permit the same execution ID to spend twice. Queries must expose current mandate budgets and
every unresolved deployment from consensus state rather than relying on events as the current safety record.

Gross or lifetime outflow does not automatically reset when capital returns. Returned value may reduce outstanding
exposure, but governance must explicitly decide whether it also restores reusable allowance. This prevents repeated
round trips from silently multiplying a nominal deployment cap.

Every new or risk-increasing execution revalidates the live mandate and fails atomically if any cap, floor, deadline,
price, slippage, destination, or status constraint fails. The mandate cannot mint, borrow, debit another Treasury fund,
or count an expected output before settlement. A pause, revocation, or mandate expiry does not discard settlement state
or forbid risk reduction. Authenticated protocol callbacks, acknowledgements, timeout reconciliation, and inbound
returns remain processable without the executor because they settle an existing deployment rather than authorise a new
one. While paused, the executor may invoke only an explicitly allowlisted close action that cannot increase gross
outflow or outstanding exposure and returns assets to a destination fixed by the stored deployment terms. After
revocation or expiry, the executor has no discretionary authority: only a field-free permissionless return/close whose
complete result is fixed by stored terms may remain, otherwise a typed governance message must authorise the close.

Allowed action types are added individually. Examples include:

- a fixed Reserve-to-Redemption-Buffer commitment;
- an atomic same-chain swap with exact maximum input, minimum receive, and deadline, whose output is credited directly
  to Reserve;
- a transfer to an exact approved custodian under explicit counterparty exposure;
- an exact IBC deployment over an approved route; and
- a close/return action that brings controlled assets back to Reserve.

If Ark needs fast Reserve-to-Buffer support before external deployment, the first and safest operational mandate is a
capped, expiring, fixed-destination `anoah` commitment with a minimum liquid Reserve floor. That authority does not
imply any external-transfer, swap, recipient, claim, or payment authority. The launch governance-only message remains
the only such path until this separate mandate phase is approved.

### 20.4 External settlement lifecycle

A same-chain swap may be atomic only when the approved adapter performs input debit, minimum-output enforcement, and
direct Reserve output credit in one cached state transition. A plain transfer to an external custodian is not an atomic
asset purchase; it creates trusted counterparty exposure and remains zero-credit unless and until the approved custody
and recognition conditions are proven.

IBC and external custody require explicit pending, acknowledged, timed-out, returned, impaired, and written-down states.
An IBC acknowledgement proves packet delivery, not that backing was purchased or remains controlled. In-flight or
unresolved value receives zero target credit. Timeout and return handling must restore or reconcile mandate accounting
without resetting lifetime gross outflow. Any manual reconciliation or write-down is governance-authorised and audited.

### 20.5 Other deferred policy work

The following also remains outside the launch phases:

- A formal claims adjudication/voting module and any non-NOAH Insurance custody or in-kind payout allowlist.
- Automated, revenue-routed, target-based, mint-funded, or conversion-funded subsidy-pool refill mechanisms beyond
  permissionless direct `anoah` transfers.
- The future basket's composition, weighting, rebalance rules, denomination, Oracle derivation, and governance launch
  schedule. The generic live Market pool-unit transition is implemented in Phase 3; activating it still requires a
  separate operational proposal once the basket policy exists, and it leaves `asdr` fully supported.
- Any future stablecoin retirement or output-disable policy. It is not implied by a flagship or pool-denomination change
  and requires its own authority, lifecycle, holder-exit, and reactivation decisions.
- A hard rolling residual-mint cap if virtual-pool analysis requires one, with explicit review of the redemption queue
  and first-mover incentive it would create.
- Replacing or redirecting Cosmos distribution/protocolpool residual accounting.

Each extension requires its own policy decision, file-level implementation plan, protobuf approval where applicable,
tests, and review gate. None should be inferred while implementing the launch phases in this document.

## 21. Phase status

| Phase | Scope                                                      | Status                                  | Approval evidence                                          |
| ----- | ---------------------------------------------------------- | --------------------------------------- | ---------------------------------------------------------- |
| 0     | Policy, assumptions, baseline                              | Complete                                | D1-D33 confirmed; baseline passed; implementation approved |
| 1     | Treasury proto, core, accounts                             | Reviewed                                | Treasury and required support accepted 2026-07-18          |
| 2     | Remove Mint and activate subsidy-pool funding               | Reviewed                                | Verification passed; user approved closure 2026-07-20      |
| 3     | Market settlement, labelled pool unit, and live transition | Reviewed                                | User and independent review completed 2026-07-20           |
| 4     | IBC/Wasm foundations, stability-tax integration, and multi-denom Oracle rewards | IBC foundation implemented; review and remaining integrations pending | Sections 16.1-16.3 approved; IBC keepers, routes, lifecycle, ante, CLI, and testing accessors implemented 2026-07-21; Wasm/GMP slice approved but not implemented |
| 5     | Full-system invariants and simplification                  | Not started                             | Pending Phase 4                                            |
| 6     | Reserve/Insurance covered-risk-exposure target models      | Deferred                                | Separate post-launch policy and implementation approval    |
| 7     | First external asset: custody and recognition              | Deferred                                | Pending Phase 6 and exact asset-policy approval            |
| 8     | Bounded Reserve mandate and first typed deployment adapter | Deferred                                | Pending Phase 7 recognition and separate approval          |

### 21.1 Current outside-`x/treasury` disposition

The following working-tree changes are outside the reviewed `x/treasury` directory. This table records which supporting
changes are accepted and which later-phase changes remain provisional; retaining partial code does not mark its phase
complete:

| Area | Current working-tree state | Disposition |
| ---- | -------------------------- | ----------- |
| Treasury protobuf/API | `proto/ark/treasury/**` defines the compact reviewed Treasury contract and `api/ark/treasury/**` contains its generated Pulsar/grpc output. | Accepted as part of the reviewed Treasury implementation; retain. |
| Phase 1 app support | `app/app_config.go` registers the four fund accounts and tax collector, orders the Treasury send restriction, places Treasury before Distribution in BeginBlock, and removes Treasury EndBlock. `app/treasury_test.go` and `app/treasury_multisig_test.go` exercise this integration. | Accepted as required Phase 1 support; retain. |
| Oracle quote support | `x/oracle/keeper/conversion.go` always includes the `anoah` identity rate in `GetRateSet`, with corresponding keeper-test changes. | Accepted as the shared quote behavior used by Treasury reward and liability valuation; retain. |
| Phase 3 Market settlement and pool-unit transition | Market injects a Treasury keeper and calls `RouteExpansion`, `DrawRedemptionBuffer`, and `RecordSupplyChange`; a successful Treasury result is authoritative. `BasePool` is a denomination-bearing `sdk.DecCoin`, delta queries return its unit, NOAH/stable math uses that unit, and stable-to-stable pricing is independent of virtual-pool state. `MsgUpdateParams` applies one fresh deterministic Oracle-derived amount on denomination changes, retains the submitted expectation in the transaction, emits old and applied pool state, atomically rescales delta on every amount change, and ordinary export preserves both values. | Reviewed and accepted. Treasury errors and actual rate, denomination, arithmetic, or effective-pool failures abort atomically. Do not add a submitted-versus-applied rejection threshold, duplicate audit fields, or a second transition path. |
| Phase 4 IBC foundation | IBC-Go v11.2 keepers, stores, Classic/v2 ICS-20 routes, Classic PFM, Classic/v2 rate limiting, 07-Tendermint, ICA controller/host, module accounts, lifecycle, redundant-relay ante, CLI/genesis basics, and IBC testing accessors are wired through the SDK runtime's manual registration hooks. | Implemented under the approved Section 16.2 scope; focused review pending. Section 16.3 now approves the next Wasm/callback/GMP slice, but none of that later wiring is recorded as implemented. Production activation remains blocked on canonical launch genesis and the complete tax, recipient-restriction, rate-limit, relay, acknowledgement, timeout, refund, packet-forward, callback, and GMP gates. |
| Partial Phase 4 ante | `app/app.go` installs `treasuryFeeChecker` and wraps the stock ante handler with `routeStabilityTax`; the implementation and tests live directly in `app/treasury_ante.go` rather than the planned `app/ante` package. | Leave unchanged and treat as provisional, unreviewed Phase 4 code. Revisit its layout, semantics, activation, and Wasm/IBC transfer-surface gate against the original Phase 4 plan when Phase 4 begins. |
| Partial Phase 4 Oracle rewards | Oracle's Bank interface now uses `GetAllBalances`, and `x/oracle/keeper/reward.go` distributes every positive denomination with updated mocks and tests. | Leave unchanged and treat as provisional, unreviewed Phase 4 code. Review it together with tax activation when Phase 4 begins. |

Untracked `build/` and `oracle.test` outputs are local artifacts, not plan or phase work, and must remain untouched.
