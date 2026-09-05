# Ark Treasury and Economic Policy Redesign Plan

- Status: **Phases 1-4 reviewed; Phase 5 down to the genesis fill**
- Last updated: 2026-09-06
- Target SDK: Cosmos SDK v0.54.3, with CometBFT v0.39.3, Go 1.25.9, and ibc-go v11.2.0. A v0.55.0 move was built and
  then reverted on 2026-08-11 to unblock the Wasm slice, whose dependency gate v0.55.0 fails (§16.3).
- Launch state: **confirmed prelaunch / fresh genesis**

This document is the implementation contract for rebuilding Ark's Treasury and the monetary flows around it. It is
intentionally more detailed than a normal design note: each implementation phase must be completed, tested, reviewed,
and explicitly approved before work begins on the next phase.

The reviewed `x/treasury` implementation is authoritative and must be carried forward. Its Treasury protobuf and
generated API changes, required Phase 1 app account wiring, Oracle `anoah` identity quote support, and Phase 3 Market
settlement and pool-unit transition implementation are also retained as recorded in Section 21.1, as is the Phase 4
IBC, Wasm, ante, and multi-denomination Oracle reward implementation, which passed its review gate on 2026-09-06.
Implementers must not replace whole files with an older baseline or treat the current branch state as disposable.

Read `Treasury` in Phase 1-3 text as the module before the D54 and D55 extractions of 2026-08-05: Insurance custody,
the Claims mandate, and the claim record now live in `x/claims`, and the strategic Reserve in `x/reserve`. Both answer
Treasury through the one-way `RecognisedCapital` contract, and Treasury keeps liability valuation, the three target
ratios, the expansion waterfall, tax, and reward funding.

## 1. Intended outcome

Ark will have no scheduled or routine NOAH issuance:

- Remove the stock `x/mint` module and its staking-reward inflation.
- Remove Treasury's `Minter` permission and all Treasury seigniorage minting.
- Keep Market as the only module with `Minter`; retain Market's `Burner` permission for conversion settlement.
- Permit Market to mint only as part of atomic stablecoin conversion settlement.
- Keep each gross NOAH expansion offer in Market's transaction-local escrow. Treasury derives and executes the complete
  per-conversion fund-allocation waterfall from that escrow; a successful result is authoritative, and Market owns the
  resulting burn, stable mint, and receiver payment.
- Make Market's single virtual pool denomination-bearing: launch it in `axdr`, then permit one atomic live unit change
  to the future basket without changing which native stablecoins may be selected as outputs; `axdr` remains supported.
- Fund validator and Oracle launch subsidies from an initially genesis-funded NOAH subsidy pool. Permissionless
  transfers of already-issued NOAH may extend it, but no automatic refill, conversion, or issuance path exists.
- Fund Oracle rewards primarily from the fixed transfer tax.
- Keep Treasury's root authority with governance, while allowing one governance-appointed threshold-multisig committee
  to update only the bounded, reversible economic-policy subset during a fixed height term. Governance may override a
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
- The response is block-granular (D33, amended 2026-08-08): every expansion in a block allocates under one fund-status
  read and every redemption draws at one coverage ratio, settled once from Market's EndBlocker. No quote, payout, or
  spread changes. What it removes is intra-block path dependence — no conversion settles ahead of another, so there is
  no within-block ordering advantage to compete for — and the difference from per-conversion settlement is bounded by
  one block's own flow.
- The strategic Reserve is never an automatic Market funding source. Governance may commit a discrete amount of its
  existing `anoah` to the shared Redemption Buffer between settlements, but Market cannot request, size, or trigger the
  transfer and no redeemer receives a direct Reserve payment.
- Virtual-pool pricing penalises sustained one-way flow and remembers imbalance.
- Time-based pool recovery limits how quickly cheap conversion capacity returns.
- No adaptive reward target/rate controller, tax-rate controller, price-reactive Buffer share, or automatic Reserve
  trigger amplifies these flows. The fixed-target reward-funding waterfall only allocates already-collected tax and
  already-issued subsidy NOAH within hard balance bounds.
- The D72 exposure multiplier is a bounded target controller and leaves every claim above intact. It moves one number:
  the liability basis the three fund targets are sized on, which decides how much expansion principal is retained
  rather than overflow-burned, and how much capital a committee may move or destroy. It mints nothing, triggers no
  trade, and changes no quote, spread, payout, or draw — the draw's coverage basis stays raw by D73 — so it cannot
  amplify a flow. It is also bounded on both ends and slow on purpose: floored at one, so its inert state is exactly
  today's sizing, capped by governance, and rate-limited per update so no single period can move it far.

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
| D4  | Fund the quoted NOAH output by the Buffer's actual pre-trade coverage share of the claimable liability, capped at 100%; mint the residual. The claimable aggregate excludes supply that cannot currently redeem — a member without a fresh feed, suspended supply without an activated plan — because unredeemable and unvaluable coincide by construction, so the draw never switches off; `valuation_complete` records partial information for audit only. This supersedes the original zero-draw fallback, which retired the Buffer chain-wide during exactly the stress it exists to dampen. | Confirmed              |
| D5  | Route eligible expansion principal to Buffer, strategic Reserve, Insurance, then burn the excess.                                                                                                                                                                                                                                                                                                               | Confirmed              |
| D6  | Burn the NOAH value attributable to spread and integer dust; never route it to any fund. Amended 2026-09-02: spread and dust enter the expansion waterfall as principal does — fund targets first, overflow burned — and route to neither the subsidy pool nor the fee collector. The unconditional burn was inherited from Terra's pre-Columbus-5 fee burn rather than reasoned. The principle now is two circuits. Gas and transfer tax fund security, so validator income never depends on the money-supply cycle; conversions insure themselves, so spread — a premium against stale-oracle and adverse-selection risk — capitalises the funds that bear that risk, whose targets already scale with the volatility and flow indicators that widen it. With backing full this is the original burn; with backing short it is capital arriving as the requirement rises. The subsidy pool remains a bootstrap with no protocol inflow, so its countdown stays honest, and nothing is paid from the funds beyond target, so no constituency gains from a wider spread. Redemption spread is NOAH never issued and stays unissued.                                                                                                                                                                                                                                                                                                                        | Confirmed              |
| D7  | Targets are passive routing thresholds and never trigger minting, trading, or automatic withdrawals.                                                                                                                                                                                                                                                                                                            | Confirmed              |
| D8  | Initially value total native stablecoin supply as exposure and count only NOAH separately in each fund.                                                                                                                                                                                                                                                                                                         | Confirmed              |
| D9  | Use a dedicated, balance-constrained, non-minting subsidy pool initially seeded at genesis.                                                                                                                                                                                                                                                                                                                     | Confirmed              |
| D10 | Give validator and Oracle reward funding separate per-block NOAH-value targets, but aggregate the targets and eligible validator fees over one parameter-initialised Treasury settlement countdown before calculating shortfalls.                                                                                                                                                                               | Confirmed              |
| D11 | Remove `oracle_tax_share`; accumulate transfer tax for the same Treasury window, protect the aggregate Oracle reward target first, use tax above that floor for any aggregate validator target gap, and return every residual to Oracle.                                                                                                                                                                       | Confirmed              |
| D12 | If the subsidy pool cannot cover both reward shortfalls, scale the shortfalls proportionally; rounding favours validators.                                                                                                                                                                                                                                                                                      | Confirmed              |
| D13 | Zero Cosmos distribution `community_tax` in Ark's default genesis by overriding the distribution module basic.                                                                                                                                                                                                                                                                                                  | Confirmed              |
| D14 | Keep protocol/community-pool residual accounting as an SDK concern; do not deliberately fund it from Treasury.                                                                                                                                                                                                                                                                                                  | Confirmed              |
| D15 | Insurance is a Treasury-owned module account paid only through unique, recorded claim IDs under the bounded, governance-owned Claims Mandate.                                                                                                                                                                                                                                                                   | Confirmed              |
| D16 | Ordinary redemption never debits strategic Reserve, and Reserve never directly pays redeemers, rewards, or claims; all arbitrary-recipient, conversion, investment, and external-asset deployment remains deferred.                                                                                                                                                                                             | Confirmed              |
| D17 | Apply each denomination's tax cap independently to each message input.                                                                                                                                                                                                                                                                                                                                          | Confirmed              |
| D18 | Tax every enabled user-facing stable transfer surface through one calculator, not internal bank movements.                                                                                                                                                                                                                                                                                                      | Confirmed              |
| D19 | Report Redemption Buffer, strategic Reserve, and Insurance separately; never present a combined backing ratio.                                                                                                                                                                                                                                                                                                  | Confirmed              |
| D20 | Launch from a clean genesis; discard legacy state/wire compatibility and implement no migration path.                                                                                                                                                                                                                                                                                                           | Confirmed              |
| D21 | Governance owns Treasury's complete denomination-bearing `reference_tax_cap` Coin in Params; launch it in `axdr` independently of Market's pool unit, and require no Treasury Params update when Market changes its pool denomination.                                                                                                                                                                           | Confirmed              |
| D22 | Make Market `BasePool` a denomination-bearing `sdk.DecCoin`; launch it in `axdr` and change its amount or denomination live only through Market's `MsgUpdateParams`. Oracle support is a prerequisite for a new denomination; Treasury never drives or intermediates the Market transition.                                                                                                                      | Confirmed              |
| D23 | Every applied `BasePool` amount or denomination change atomically rescales `ArkPoolDelta` to preserve `delta / BasePool.Amount`. On a denomination change, treat submitted `BasePool.Amount` as a non-binding audit expectation and apply the amount derived from one fresh deterministic Oracle conversion.                                                                                                   | Confirmed              |
| D24 | Value Treasury liabilities and targets directly in NOAH equivalents; stable-to-stable pricing does not use Market's pool denom.                                                                                                                                                                                                                                                                                 | Confirmed              |
| D25 | Permit irreversible, permissionless `anoah` transfers into the subsidy pool; reject other denoms and add no automatic refill mechanism.                                                                                                                                                                                                                                                                         | Confirmed              |
| D26 | At launch, permit only positive `anoah` deposits to all four Treasury fund accounts; reject every mixed or non-NOAH transfer atomically. The single exception is Treasury's own settlement routing from `transfer_tax_collector` into `strategic_reserve`, which carries derecognized (written-off or retired) transfer tax into inert Reserve custody pending a separate governed disposal decision. | Confirmed              |
| D27 | At launch, governance alone may irreversibly transfer a discrete `anoah` amount from strategic Reserve to the shared Redemption Buffer, subject to an execution-time minimum remaining Reserve balance; no target, price, Oracle, or Market trigger applies.                                                                                                                                                    | Confirmed              |
| D28 | Required Reserve and Insurance capital is based on each fund's covered risk exposure, never the gross value of assets held; future external assets may reduce a gap only through explicit fund-specific, risk-adjusted recognition plus a separate liquid-capital requirement, and Ark-issued stablecoins always receive zero credit.                                                                           | Confirmed              |
| D29 | Governance retains Reserve policy authority; any future fast execution uses a governance-created, typed, bounded, expiring mandate executed by a threshold multisig with a separate pause-only guardian, never a generic Reserve sender or Treasury parameter authority.                                                                                                                                        | Confirmed              |
| D30 | Governance owns the Insurance Claims Mandate. The shared cancellation period is a module parameter applying to both origins (Treasury's when written; `x/claims` Params since D54); committee submissions are bound to the active mandate window and exact term and consume a fixed gross term allowance, while governance submissions depend only on params and record no mandate term; governance may cancel any pending claim regardless of the current mandate, while the current committee may cancel only non-governance-submitted claims. | Confirmed              |
| D31 | Economic policy, Insurance claims, and future Reserve operations use distinct role addresses and typed authority domains even if human memberships overlap; no role receives Treasury's general authority.                                                                                                                                                                                                      | Confirmed              |
| D32 | Claims use submit-then-pay with encumbered pending amounts; cancellation ends for every actor at the closing height, and no authority can claw back a paid claim or bypass held-balance, denomination, uniqueness, no-mint, no-borrow, or no-cross-fund invariants.                                                                                                                                          | Confirmed              |
| D33 | Market escrows each gross expansion offer and owns conversion burn/mint/payout; Treasury derives and executes the complete per-conversion waterfall, returns an error if it cannot complete it, and otherwise returns the authoritative allocation Market uses to finish settlement. Amended 2026-08-08: the exchange happens once per block from Market's EndBlocker, not inside each conversion. Market escrows as before and still burns each conversion's spread in the conversion that charged it — the spread owes nothing to liability or fund state, so nothing about it defers — records the block's conversion facts in transient accumulators, mints each redemption's complete quoted output, and executes the burn settlement returns. Treasury values liability once against final state and derives and executes the complete waterfall and coverage draw over the block's totals. Every clause above survives at block granularity: Market owns custody, mint, and burn, Treasury owns valuation and allocation, and the return value is still how Market finishes settlement. What moved with the cadence is the failure class — a settlement error now fails the block rather than one transaction — and the deletion of every cached-valuation mechanism this decision's per-conversion timing required (D39, D68). Amended 2026-09-02: with D6 amended, Market no longer burns spread in the conversion. It escrows the gross offer whole, accumulates gross rather than eligible principal, and Treasury's settlement takes the block's gross total through the waterfall; the burn Market executes at settlement is the overflow alone. Spread and dust thereby share principal's fallback — an incomplete valuation parks the whole gross total in the Reserve — which is the all-or-nothing rule already settled.                                                                                                                                | Confirmed              |
| D34 | Govern `reward_funding_window`, default it to one chain week, initialise `blocks_remaining` from it when an empty Treasury window records its first observation, and apply later parameter changes only after the active countdown settles.                                                                                                                                                                     | Confirmed              |
| D35 | Keep `FundStatus` limited to fund stocks, liabilities, and targets; expose active reward-funding accounting through an independent direct-state query that remains available when fund valuation is unavailable.                                                                                                                                                                                                | Confirmed              |
| D36 | Governance may appoint one exact threshold-multisig economic-policy committee under a bounded, height-scoped, chain-termed mandate. The committee controls only the nine reversible policy fields (widened from six on 2026-08-10 by the D72 amendment, which moved the three exposure weights here); governance may override policy and replace or disable the mandate at any time.                                                                                                                               | Confirmed              |
| D37 | Governance owns the complete denomination-bearing `reference_tax_cap` Coin together with `reward_funding_window` in `Params` (`claim_cancellation_period_blocks`, once named here too, moved to `x/claims` Params by D54; `tax_cap_refresh_period_blocks` is no longer a field — caps derive per block from the conversion-factor table, §8.5). Persist the reversible economic levers once in `EconomicPolicy`; the boundary between the two messages is stance against machinery, not a field count — a lever stating how much of something the protocol wants, reversible and safe to clamp between a mandate's minimum and maximum, belongs to the committee, while the instrument that computes it, the cadence it runs on, and the rails bounding how far and how fast it may move stay with governance (amended 2026-08-10 alongside D72); Claims Mandate remains claims-only. Amended 2026-09-03 (D80): `transfer_tax_rate` is `Params`, not a committee lever — a stance quantity, but one that lives in the signed fee.                                                                                                                                                                          | Confirmed              |
| D38 | Keep launch Claims minimal: the mandate stores its monotonic term, committee, half-open activation/expiry window, and fixed gross committee claim limit; the shared cancellation period lives in Treasury `Params` (moved to `x/claims` Params by D54); claims have no category or per-claim cap, and there is no guardian or governance-cancellation flag.                                                                                                                               | Confirmed              |
| D39 | Prime the claimable-liability snapshot in the preblocker each block (the first settlement scans lazily only when no snapshot exists), cache the claimable aggregate together with a completeness flag, and advance it from every Market burn/mint. The snapshot always carries a value: incompleteness is disclosure about excluded unclaimable supply, never an uncached state. Superseded 2026-08-08 by D33's block-granular settlement: with the valuation reduced to one consumer at one point after every write, there is nothing to cache. The snapshot, its codec and gas constant, the priming preblocker, and the supply-delta maintenance are deleted, and settlement scans canonical Bank and Oracle state once per block that converts — an idle block now values nothing, where priming scanned the registry every block. Only the completeness flag survives, unchanged in meaning and emitted at most once per block.                                     | Confirmed              |
| D40 | Begin Phase 4 by fixing execution-time tax payer, exactly-once identity, rollback, and fee-sponsorship semantics; then implement IBC foundations before Wasm because contracts may dispatch IBC messages. Design the Treasury execution hook into both paths, but keep both transfer surfaces production-disabled until the complete tax and recipient-restriction activation gate passes.                                                                                  | Confirmed              |
| D41 | Use call-path ownership for exactly-once tax assessment: ante owns signed top-level inputs, while the Wasm dispatcher owns only execution-generated Bank sends, IBC sends, execute funds, and instantiate funds. Add no persistent transfer IDs, context markers, global Bank tax hook, or implicit execution-time feegrant. The sending contract pays the tax in addition to the complete requested principal.                                                              | Confirmed              |
| D42 | Send execution-generated tax directly to `transfer_tax_collector` and execute its collection with the matching transfer in one Wasm submessage cache. Synchronous or caught failures roll both back; a successfully created IBC packet retains its tax through later acknowledgement, timeout, refund, or return bookkeeping, none of which is a new taxable transfer.                                                                                                          | Confirmed              |
| D43 | Give contracts a read-only tax estimate through Treasury's own `Query/ComputeTax`, admitted to the Wasm query accept list at the §16.6 gate rather than served by a custom querier (D74). A contract prices the proto form of the message it will dispatch, so the calculator and its rate/cap math are never duplicated; for the JSON-native `CosmosMsg` variants the contract builds that proto itself, and keeping it identical to what it returns is the author's responsibility. The result is an advisory current-state estimate only: it reserves no funds, grants no authority, and never replaces execution-time recomputation.                                                                                              | Confirmed              |
| D44 | Treat `Params.transfer_tax_rate` as the sole tax activation switch (moved from `EconomicPolicy` by D80, 2026-09-03). An explicit zero reference or derived tax cap means uncapped taxation, while a missing configured-denomination cap remains an error. Keep the complete derived cap map populated independently of the rate, and never rebuild it from either policy-update message; reject any positive reference-cap conversion that truncates to the zero sentinel.                                       | Confirmed              |
| D45 | Build Ark's hub foundation against `github.com/cosmos/ibc-go/v11`, targeting v11.2.0 subject to dependency-resolution and compile verification. Wire IBC Classic and IBC v2 core/ICS-20 routes, the 07-Tendermint light client, and the transfer module account with minter/burner permissions. Keep standard module genesis defaults in application code; Ark's canonical launch genesis must allow only `07-tendermint` and launch transfer with send and receive disabled. Amended 2026-09-06: the launch allowed-client list is empty, so no client, connection, channel, v2 counterparty, or packet of any kind can exist until governance admits `07-tendermint` by `MsgUpdateClientParams`; the transfer and ICA flags stay off as well, so that vote opens client creation and nothing more. | Confirmed              |
| D46 | Put governance-controlled rate limiting and packet forwarding in the IBC Classic transfer stack, and apply the v11 rate limiter to the IBC v2 transfer path. Configure reviewed per-denomination, per-channel/client limits before enabling production transfer. A packet-forwarded hop is protocol-generated continuation of the original transfer, not a new user-facing taxable input; acknowledgement, timeout, refund, and return bookkeeping likewise receive no second tax. Amended 2026-09-03: the raw v2 `MsgSendPacket` is the same outbound leg as `MsgTransfer` and prices identically. The transfer app authenticates the payload sender against the message signer and escrows through the same `SendTransfer`, so the two are one taxable surface reached by two doors, and the source port is what decides whether a payload carries principal at all — Wasm v2 ports and GMP move no coins. The calculator decodes with the transfer module's own decoder and its own arguments, so a payload it escrows is one the tax prices and an undecodable transfer payload fails the transaction rather than passing untaxed. Acknowledgement, timeout, and refund remain untaxed on both paths. | Confirmed              |
| D47 | Add IBC callbacks to both the Classic and v2 ICS-20 stacks when the Wasm keeper is wired. Callbacks are Ark's canonical transfer-and-call mechanism; add no separate IBC Hooks middleware. Callback-triggered execution uses the same Wasm/Treasury execution adapter, while acknowledgement, timeout, and callback delivery alone are not new taxable transfers.                                                                                                  | Confirmed              |
| D48 | Add IBC v2 GMP together with the Wasm foundation and route its derived-account SDK-message execution through the same Treasury-aware router used by contract-generated messages. The derived GMP account pays any execution-generated tax in addition to principal; outer fee payers and feegrant do not sponsor it. Do not expose a partially integrated GMP route before its authorization, tax, rollback, and recipient-restriction tests pass.                 | Confirmed              |
| D49 | Use upstream Wasmd `v0.70.x` and `wasmvm/v3`, subject to clean resolution and compile verification against Ark's SDK `v0.54.3` and IBC-Go `v11.2.0`; do not maintain a Wasmd fork or replacement-directive compatibility layer. Remove Ark's unused legacy `wasmvm` v1 parser/query interfaces when the real runtime is installed.                                                                                                                         | Confirmed              |
| D50 | Consider 08-Wasm only with the Wasm foundation and only if its exact dependency and VM family integrates cleanly. If installed, keep it dormant at launch: the allowed-client list admits no client type at launch (D45 as amended) and the launch genesis contains no Wasm-client checksums. `09-localhost` remains unavailable through that launch allowlist; add neither 06-Solo Machine nor the experimental attestations client.                                                   | Confirmed              |
| D51 | Keep packet forwarding Classic-only until upstream provides reviewed v2 support; do not invent a v2 PFM adapter. Add no ICS-29 relayer-fee wiring because that application was removed from IBC-Go.                                                                                                                                                                                                                                                              | Confirmed              |
| D52 | Retain standard user ICA controller and host support but launch both disabled and the host with an empty message allowlist. Defer a generic custom ICA authentication module; if contracts later need ICA control, prefer a narrowly scoped Wasm-to-ICA adapter with explicit authorization.                                                                                                                                                                       | Confirmed              |
| D53 | Add neither ICS-721 NFT transfer nor a separate NFT module. Native Wasm IBC channels cover custom contract protocols, callbacks cover ICS-20 transfer-and-call, and GMP covers arbitrary remote SDK-message execution; revisit IBC Hooks only for a concrete requirement for Osmosis-compatible `wasm` memo or intermediary-address semantics.                                                                                                                       | Confirmed              |
| D54 | Extract Insurance claims from `x/treasury` into `x/claims`, which owns the Claims mandate, the claim record, the Insurance reservation, and the `claims_insurance` custody account. Treasury keeps liability valuation, all three fund target ratios, the expansion waterfall, tax, and reward funding, and reads Insurance through a one-way `ClaimsKeeper.RecognisedCapital` interface. The boundary is §7.2's own accounting contract: Treasury owns `required_capital`, the operating module owns `recognised_capital`, and per §20.1 held assets satisfy a requirement without defining it. Done pre-launch because the move carries five populated collections and is a real store migration afterwards. The future Reserve mandate (§20.2) lands as `x/reserve` on this same pattern, implementing the same interface, which also returns Treasury to one mandate per module — the convention `x/security`, `x/asset`, and `x/market` already follow. | Confirmed |
| D55 | Extract the strategic Reserve from `x/treasury` into `x/reserve` on the D54 pattern, pre-launch, ahead of the mandate it will hold. The module owns the `strategic_reserve` custody account, the governance-only `MsgTransferReserveToBuffer`, and the send restriction over that account — including its one exempt pair, `transfer_tax_collector` to Reserve, which admits the derecognized transfer tax Treasury settlement routes there. It answers the same `RecognisedCapital` contract as `x/claims` — through Treasury's own `ReserveKeeper` interface, since depinject resolves module inputs by type and needs the two named apart — so Treasury sizes both committee-operated funds identically and holds one mandate. Migration cost is nil either way (the module has no collections, and the account rename to `strategic_reserve` is free pre-launch); the exemption is what argued for doing it now rather than inside the §20.2 mandate work. | Confirmed |
| D56 | One-committee Reserve mandate. The §20.2 executor/guardian split collapses into a single Reserve committee appointed through `MsgSetReserveMandate` on the shared envelope pattern: chain-derived monotone term, half-open activation window, empty committee disables, replacement resets allowance usage. Mandate contents reduce to the term deployment allowance (consumed permanently — closing a position never restores it), the minimum liquid `anoah` floor (absolute and appointment-scoped; the liability-scaled liquid tranche is the Redemption Buffer, per the §7.2 amendment), and an exact destination list. There is no guardian and no pause state — governance replacing or disabling the mandate at proposal latency is the brake, exactly as for every other mandate on this chain — and no governance deployment message, because deployment needs an off-chain counterparty relationship only a committee operationally has. §20.3's per-transaction/rolling-window/lifetime caps, price/slippage/deadline constraints, adapter pins, and pause state are each deliberately omitted (accounting spec §2). Implemented 2026-08-05. | Confirmed |
| D57 | Quantity ledger with read-time feed pricing. The Reserve records what is held, never what it is worth: an append-only journal (kinds DEPLOYMENT / QUANTITY_UPDATE / RETURN_ATTRIBUTION / IMPAIRMENT / CORRECTION / CLOSURE) carries proven coin movements and committee-attested quantities with bounded off-chain references, and valuation happens only at read time against the Oracle's available rates. Corrections restate — the journal keeps the error and the fix — and genesis re-derives every stored aggregate from the journal and requires equality. Return valuation is crystallised at attribution time by the keeper, so realised P&L at closure is arithmetic over proven legs, which is why closure needs no governance gate (narrowing §20.4's write-down rule). Supersedes the attested-valuation machinery of §20.3–20.4. Implemented 2026-08-05. | Confirmed |
| D58 | Recognition policy as combined eligibility and custody allowlist. One governed list (`EligibilityEntry`: asset denomination, haircut factor in [0, 1], recognition cap ratio in [0, 1]), replaced whole by `MsgSetRecognitionPolicy` and enforced at two points: the Reserve send restriction admits a denomination only if listed — §20.1's "anoah plus custody allowlist" restriction change, arriving for exactly one fund — and `RecognisedCapital` credits `min(haircut × attested open unimpaired quantities × rate, cap_ratio × recognised_capital)` per listed asset on top of the par-counted balance, the self-reference solved per block in closed form (D64). The multiplication is the chain's one rate orientation: every oracle rate quotes NOAH per one unit of its asset (D75, flipped 2026-09-02; it was units per NOAH before, when this valuation divided), so valuing that asset in NOAH multiplies, and the conversion goes through `RateSet.Convert` like every other valuation on the chain rather than being re-derived here — it was re-derived once, as a multiplication, and published the reciprocal of every figure until 2026-08-08. Every rate comes from the Oracle, so a dark or stale feed zeroes exactly one asset's credit and recognition degrades to zero rather than to a frozen number, with no exception: there is no governance-supplied fallback price, and a slow-cadence feed is served by the per-denomination staleness window of D60 instead. §20.1's cross-fund concentration caps land as per-asset Reserve caps because Insurance stays NOAH-only (claims plan D4). The `RecognisedCapital` query decomposes every row — including attested-but-unlisted holdings — so each zero credit shows its reason. Implemented 2026-08-05; the cap became a share of recognised capital itself under D64 on 2026-08-06. Amended 2026-08-09: both factors are strictly positive and the custody-allowlist role is gone — admission stopped reading the policy under D70's narrowing, so a custody-only entry became indistinguishable from no entry while still occupying the policy and raising no feed-guard claim; refusing zero factors makes every stored entry a live claim and the guard total over the policy. Amended 2026-08-10: the on-chain balance leaves the credit formula and `AssetRecognition.onchain_quantity` is deleted. The term dated from the allowlist role, when a listed denomination could sit in the Reserve account; under D70 admission is NOAH-or-member and a listed name is an external symbol, so the two shapes are disjoint and the term was provably zero on every row that could earn. The field went with it rather than staying as a custody column: its only non-zero class is registry-member paper, which can never be recognised, is already published by Bank and by Treasury as self-held supply (D66), and — for paper bought back through a position — appeared in the same row twice, once attested and once held. Bank custody is therefore not a subject of the decomposition, and the fold no longer reads account balances at all. | Confirmed |
| D59 | No adapter or evidence-upgrade machinery (`docs/DESIGN_NOTES.md` §6.2, Phase C) is planned. Evidence upgrades can verify only value that routes through chain-visible machinery — an IBC acknowledgement proves delivery over a channel, a contract read proves wasm state — and the Reserve's plausible asset universe is custodian-held off-chain instruments reached by wire transfer, which none of that can ever see. Manual committee attestation with bounded references is therefore the permanent evidence model, not an interim one; the same-chain case needs no adapter because a listed asset in the Reserve account is bank custody the recognition fold prices directly. The journal's append-only design keeps this reversible without migration: machine-authored entries could later land beside committee-authored ones if a chain-verifiable venue ever became real, but that requires its own spec and the §20 first-asset gate. Closes §20.4's adapter machinery beyond the D57 narrowing. | Confirmed |
| D60 | Exchange-rate staleness is per denomination. Oracle `Params` carries `max_exchange_rate_age_overrides`, a sorted governed list replacing the default window for named denominations; every freshness check resolves its window through `GetMaxAge(denom)`. How long a rate stays meaningful is a property of the feed rather than of the consumer reading it — a slow-moving instrument priced against a daily published figure is not stale at an age that would make an FX rate dangerous to quote against — so the window lives beside the rate and every consumer inherits one answer per denomination. This is what let D58 delete its governance-supplied fallback price: "valuation is never stored" and "recognition degrades to zero, never to a stale number" both hold without exception, and the only way to price a slow feed is still an Oracle feed. Implemented 2026-08-05. Amended 2026-08-09 (D71): the override machinery is deleted — feed sharing under D70 splits cadence, a feed fact, from tolerance, consumer policy, and the premise held only while every feed had one consumer class; the default window survives as the conversion-grade read and the Reserve's tolerance moves to each eligibility entry. | Confirmed |
| D61 | Reserve burn authority is split by what a burn can destroy. Governance (`MsgBurnReserveAssets`) may burn any Reserve custody including NOAH, under the same per-proposal stale-state floor as the Buffer commitment: a NOAH burn is the chain's only discretionary supply contraction and no committee on this chain moves supply, mirroring §20.3's "the mandate cannot mint". The committee may burn credit-zero, non-NOAH custody (`MsgCommitteeBurnResidue`) — unlisted, or listed with a zero haircut or cap — because destroying what the capital system already counts at nothing cannot reduce recognised capital, and that residue is in practice Ark-issued stablecoin whose destruction reduces consolidated liability. The committee may also burn NOAH surplus (`MsgCommitteeBurnSurplus`), bounded by the keeper at `min(recognised_capital - required_capital, balance - mandate floor)`: burning reduces recognised capital one for one, so the power exhausts itself exactly at the target line and cannot reach through it, leaving the committee timing rather than size. That burn refuses while valuation is incomplete, since the requirement is then unavailable — the same condition that parks principal in the Reserve freezes disposal out of it. Burns open no position and append no journal entry; the signed message and Bank's canonical burn event are the audit trail. `strategic_reserve` gains `Burner` and never `Minter`. Treasury additionally calls `RecordParkedPrincipal` on the incomplete-valuation branch so the fund books the lifetime principal no target sized — an accounting call only, since Treasury credits the fund through Bank as it does the other two and the Reserve interface exchanges numbers rather than transfer capability. Implemented 2026-08-05. | Confirmed |
| D62 | Ark-issued denominations are enforced credit-ineligible, not merely declared so (D28), and no Reserve eligibility entry may name an asset-registry member at all. The entry was once legal in custody-only form because an in-kind return of Ark paper could not otherwise enter the account; D65 removed that necessity by admitting members through the send restriction on membership alone, so the entry itself is now the error and only its shape needed policing before. Checked against `AssetKeeper.HasAsset` in `MsgSetRecognitionPolicy` and in Reserve `InitGenesis`, which runs after `x/asset`. Membership is the test rather than lifecycle status, since a written-off or retired asset is still Ark-issued. The check sits at the write points only, so the per-block recognition fold takes no registry read; the accepted residual is that a denomination listed before it is ever registered keeps its credit until the next whole-for-whole policy replacement re-validates the set. Amended 2026-08-08. Amended again 2026-08-09 (D69): the check and its residual are deleted together — the external-symbol shape makes a member entry unrepresentable, so the invariant stops being a keeper concern at all. | Confirmed |
| D63 | `x/reserve` registers as an Oracle feed-removal guard, joining `x/asset` in the wiring-owned guard set. It reports two claims, both derived from Reserve state at call time: a credited eligibility entry, because recognised capital counts what the fund holds valued through that feed, so removing the feed would shrink Reserve capital and change what a committee may burn with nothing in the proposal disclosing it; and an open position denominated in the feed's asset, because a return attributed while the feed is dark crystallises zero recovery into the journal permanently, overstating that position's realised loss forever. The first claim is about a recoverable number and exists to force sequencing — delist, then remove; the second is about an irreversible record. A closed position raises no claim. Implemented 2026-08-05. Amended 2026-08-09 (D70): both claims map through the prefix derivation — eligibility entries by a filtered pass over the policy's `F-` keys, open positions through their resolved feed; both rationales unchanged. Amended 2026-08-10: the entry claim drops its credit-granting test and every stored entry claims its series. Under D58's strict positivity the test is vacuous, and it is deleted rather than kept as a redundant filter because the two readings differ in direction — an entry crediting nothing, could one exist, would have its feed removed silently, where walking the whole policy blocks removal until governance delists it. The guard gates an act recoverable only at the cost of the activation delay and a re-warm, so it fails closed. | Confirmed |
| D64 | Recognition caps are each asset's share of recognised capital itself: `EligibilityEntry.recognition_cap_ratio` in [0, 1) clips credit at `ratio × recognised_capital`, the self-reference resolved per block in closed form. The denominator was chosen against two alternatives. A flat `anoah` amount states a tolerance at one balance-sheet size and decays into a binding constraint or a vacuous one — and a binding cap reopens the gap at full value, the §7.2 acquisition/refill loop. A ratio of the capital requirement scales the ceiling with the size of the need rather than the fund's substance, so one corrupted attestation against an almost-empty fund could manufacture credit up to a share of a large target. Against the certificate itself, credit levers only the provable NOAH base: a lone corrupted input can never push the total past honest ÷ (1 − ratio), a fund holding no NOAH counts no asset at all, and the invariant is checkable against the answer — no clipped asset exceeds its ratio of the reported figure, and a clipped asset holds exactly it. Clipping one asset shrinks the total every other share is measured against (the shrinking-denominator effect), so the clipped set and the total are found together: sorting assets by `raw ÷ ratio` makes the clipped set a suffix, and the one consistent split gives `T = (base + Σ unclipped raw) ÷ (1 − Σ clipped ratios)` — exact cross-multiplied big-integer arithmetic, no iteration, no stored valuation, totality by `T ≤ base ÷ (1 − Σ ratios)` with no attested figure in the bound. Existence requires the policy-wide ratio sum strictly below one, enforced at the policy write and genesis import: at one the denominator dies and the caps admit everything. The couplings the self-reference introduces all point conservatively — a dark feed zeroes its own asset and tightens every neighbour's ceiling — and `RecognisedCapital(ctx)` stays a pure local read with the §7.2 split untouched: nothing is threaded from Treasury, and the surplus a committee may burn is monotone in the requirement again. The EVM warning against on-chain waterfall caps (float nondeterminism, gas-bounded recursion) does not govern a Cosmos keeper fold: the arithmetic is integer, the policy is governance-written rather than attacker-supplied, and the closed form replaces iteration — while the query still publishes each block's resolved `effective_cap` per asset, the absolute ceilings a debt-ceiling design would have governance push by hand. Implemented 2026-08-06. | Confirmed |
| D65 | The Reserve's send restriction admits any asset-registry member by membership alone, alongside NOAH and eligibility-listed denominations. Ark paper is protocol liability wherever it sits, can never earn recognition credit (D28), and the Reserve is the one account with a committee able to retire it, so refusing it never protected the fund's figures and only pushed it somewhere the chain cannot see. This subsumes and deletes the transfer-tax-collector sender exemption — written-off and retired assets are still members — which strictly tightens the rule, since the collector loses its bypass for coins that are neither NOAH nor members. Accepted cost: anyone may push member dust into the account, bounded by registry size and burnable by the committee. Implemented 2026-08-08. Amended 2026-08-09 (D70): admission narrows to NOAH-or-member. The eligibility-listed clause never admitted anything a mint path could produce, and its external-shape successor lasted one review — both implied an on-chain external-custody lane that genesis and movement valuation refuse and D59 forecloses. In-kind return legs were always served by the surviving clauses: returns arrive as NOAH or members. | Confirmed |
| D66 | Treasury's liability partition measures and discloses self-held supply: for every member the aggregate counts, the strategic Reserve's balance of it, valued through the same branch that counted it (fresh, settlement, or last known). Accrual happens inside each counted branch rather than over the registry, so the two sides of the subtraction can never price on different bases and `net = gross - self_held` is non-negative by construction. Reported by `FundStatus` as `self_held_supply`, `self_held_liability`, and `net_liability`. Implemented 2026-08-08. | Confirmed |
| D67 | The claimable aggregate resolves to two bases, and one principle assigns every consumer: a flow serves claims that can arrive, and no claim can arrive from self-held paper; a bound serves discretion, and the discretion-holder can re-issue that paper, so bounds price it as if they already had. Flows take net — `calculateFundStatus` gaps and `DrawRedemptionBuffer` coverage. Bounds take gross — `targetBasis` and everything derived from it: `RequiredReserveCapital` (hence `BurnableSurplus`), `InsuranceShortfall`, `RedemptionBufferShortfall`. This finishes the exclusion the partition already performs for written-off and untrusted supply, reaching the last unclaimable supply it still counted. Implemented 2026-08-08. | Confirmed |
| D68 | Mid-block coherence of the primed liability snapshot is maintained by asymmetric invalidation. Outbound crossings of the Reserve boundary by member coins drop the snapshot through the registry-cache invalidator Treasury already implements: every Reserve burn touching a member, and a `CommitteeDeploy` of member paper, which returns netted-out supply to circulation and would otherwise leave the block undersizing targets. Inbound crossings invalidate nothing — an inbound send can only make true net liability lower than the snapshot, which is staleness in the conservative direction, corrected at the next prime — so the send restriction stays a pure admission predicate with no cross-module side effects. This also fixes a standing violation of the hazard note in `PrimeLiabilitySnapshot`: a residue burn of an active member previously left the rest of the block sizing targets on pre-burn supply. Implemented 2026-08-08; superseded the same day by D33's block-granular settlement, which removes the snapshot this decision kept coherent. Both invalidator interfaces, their wiring, and every call site are deleted. The hazard the decision managed — a mid-block supply move that forgets to drop the snapshot, mis-allocating funds with no error — is now closed by construction rather than by each new writer remembering, which is the reason the change was made: the asymmetric rule below was correct, but it had to be re-derived by every future path that touches member supply. | Confirmed |
| D69 | External holdings are named by external symbols — `<feed>-<tag>`, the prefix passing the priced-denom rule, the tag bounded lowercase alphanumeric — partitioning the namespace from Ark-issued assets by shape rather than by state. The registry side has been enforced since the priced shape existed (its charset never admitted a dash); the eligibility side requires the external shape at the types level, so policy ∩ registry = ∅ is a theorem about strings with no read, no ordering, and no cross-module cooperation to keep true. `validateExternalEligibility` is deleted rather than extended; the burn-residue credit refusal is kept as a provably unreachable backstop; the journal becomes permanently unambiguous about which instrument its history names — a property no write-time guard provides, because closed history raises no claim. An external symbol can never be protocol paper — not registrable by shape, and conversion mints registry members alone — while custody the chain does hold under such a name stays ordinary bank state the fold counts; an off-chain NOAH holding is unrepresentable via the prefix rule. Implemented 2026-08-09; see `docs/DESIGN_NOTES.md` §6.4. | Confirmed |
| D70 | An external symbol's pricing feed derives from its prefix, unconditionally: `ExternalFeed(symbol)` is a pure function, never derive-if-exists, which would silently re-point every external symbol on a series the moment a twin feed activates. The derivation is many-to-one — several tags share one series with per-entry haircut, cap, and window, so multi-custodian holdings need no aliasing machinery and no duplicate feed, and a holding honestly priced on a different series is a different prefix. Listing requires the derived feed Active — exactly, not merely scheduled: an entry's haircut and window are judgments about how a series behaves, voted blind if it has never printed a rate, and a series scheduled for removal has already passed its guard — matching registration's rule and moving the Reserve's genesis ordering dependency from `x/asset` to `x/oracle`. The feed guard maps external symbols by a filtered pass over the contiguous `F-` policy keys and open positions through their resolved feed. Deployment's acquired leg and any denomination-changing correction pass external-or-current-member, closing the bare-issuable-name side door. The send restriction narrows to NOAH-or-member: the external-shape clause admitted a name class no mint path can produce, genesis refuses, and movement valuation cannot price — a door to an on-chain external-custody lane D59 forecloses — so a future module making such custody real widens admission, genesis, and valuation together in its own spec. Cap ratios sum per series family, and governance sizes the family rather than the row. Implemented 2026-08-09. Amended 2026-09-06: the refusal is permanent; on-chain custody of an external token will never be built. | Confirmed |
| D71 | Staleness tolerance is entry policy, not feed state: `EligibilityEntry.max_rate_age` — required positive, capped at `MaxRecognitionRateAge` (30 days) — states how old the derived feed's rate may be and still back this entry's recognition credit, never inherited from the Oracle default, which answers for conversion-grade reads. The Oracle judges per request: `GetRateSetWithin` takes one request per entry — the entry's denomination and window — derives the series from the name itself, and answers under the name, so two tags on one series may state different windows and receive different verdicts, keyed apart rather than colliding on the shared feed, and no request can route a name to any series but its own. Nothing unjudged crosses the module boundary, and the window never enters Oracle state. D60's override machinery is deleted end to end — collection, `MsgAddFeed.max_age`, genesis field, events, query — while `Params.MaxExchangeRateAge` survives as the single default behind every gate-enforcing read, including `valueMovement`'s conversion-grade pricing of proven movements. `GetLastKnownRateSet` and its fence are untouched. Implemented 2026-08-09. | Confirmed |
| D72 | Risk-scaled fund targets. Every target is sized on `m x` its existing liability basis — net for the expansion waterfall, gross for the bounds on committee acts — where `m` is a governance-parameterised multiplier over three indicators the chain already maintains: the liability ratio (net liability over circulating NOAH, the reflexivity term, which collapses to that quotient because liability is already valued in NOAH under D24), annualised realised volatility of the protocol reference rate, and an EMA of per-block net redemption flow. Samples fold in `SettleConversions`, which Market's EndBlocker calls every block with the flow facts already NOAH-valued in `ConversionTotals`; the multiplier itself recomputes on a governed period, step-limited and clamped to `[1, cap]`. The three indicator weights live in `EconomicPolicy` and the guardrails behind them in `Params`. An earlier draft put all eight fields in `Params` to avoid widening a threshold-multisig's mandate, which mistook D36's field count for its principle; the criterion that actually sorted the two messages is stance against machinery, and a weight stating how much extra capital a unit of leverage should demand is the same kind of lever as the target ratios it modifies — reversible within hours by zeroing it, and the most time-sensitive knob the module has, which is the committee's founding use case. What stays with governance is what makes the delegation safe: the decays define the measuring instrument and are not reversible in any useful sense, because a series folded under one memory is not recoverable by restoring the old value; the cap is the ceiling on how far the response may go; the step is the anti-gaming rate limiter, which a committee able to set it could raise to reach the cap in a single update; and the cadence follows D37. A committee at the maximum of its mandate can therefore move the multiplier no faster and no further than organic stress already can, so the delegation adds no blast radius. Weights inherit the mandate's minimum and maximum clamp for free, letting governance floor them above zero to deny the committee the power to switch the model off, or leave the floor at zero to grant it, per appointment. All three funds scale on one basis: the ratios are voted as a set and read as relative fund sizing, so scaling a subset would move those proportions without a vote, and the indicator applies to every fund sized against the same liability. The per-fund covered-risk basis of section 7.2 and D28 remains the later refinement; D8 already records the single shared basis as an initial simplification. Every weight defaults to zero, which pins `m` at one and reproduces current behaviour exactly. The liability aggregate plays three roles and only one is scaled: requirement bases read `m x L`, the redemption draw's payment denominator reads raw `L` (D73), and the multiplier's own liability-ratio input reads raw `L`, because a controller that fed its own output back into its input would compound. | Confirmed              |
| D73 | The redemption draw is never scaled by `m`, in either direction. Dividing its basis rations the run's opening phase — the phase that decides whether a spiral ignites — to guard against an exhaustion that cannot happen: proportional coverage gives `B = B0 x (L/L0)`, so the Buffer depletes exactly in step with the liability it covers and reaches zero only when the last stablecoin is redeemed. Multiplying coverage fails the other way: it double-counts one signal in the payment path, since `m` has already raised coverage by raising the Buffer through the targets, and it front-loads finite inventory on a bet about run depth (`B = B0 x (L/L0)^m`), leaving a thinner Buffer after any partial episode and pinning coverage at one until depletion releases it mid-run. Both directions reintroduce the price-reactive Buffer share section 1.1 forecloses and D4 exists to prevent. The draw's risk response is mediated by capital instead: the scaled targets grow the Buffer before a run, and the `m`-widened Redemption Buffer shortfall widens the committee's Reserve-to-Buffer injection cap during one. | Confirmed              |
| D74 | Contracts reach Ark's own modules through proto, not a hand-written JSON surface. Wasmd's custom **message** encoder goes unused: a contract calls Market with `CosmosMsg::Any` carrying `/ark.market.v1.MsgSwap` and proto bytes, and `x/market/wasm` and `x/wasm/exported` are deleted rather than ported to `wasmvm/v3`. The custom **querier** goes unused as well: D43's estimate is Treasury's `Query/ComputeTax`, reached through the accept list, so contracts hold one proto surface for reads and one for writes. The argument is one schema instead of two: a custom encoder mirrors each message as JSON on the Go side and again by hand in every contract, and nothing detects the drift — the same objection D43 raises against a second message model and CLAUDE.md raises against a second validation site, already visible in the dead binding's stale duplicate checks. Three things that looked like costs are not. The parser's `Trader = contractAddr` override is not protection: Wasmd rejects any dispatched message whose signers are not exactly the contract, so impersonation is closed either way. Tax does not differ: custom-encoded messages route through the same message handler the Treasury wrapper decorates. And no installed base breaks, because genesis is fresh (D20) and a Terra contract cannot port unmodified regardless — `anoah` renames the denomination, 18 decimals move the money math, and the type URLs are Ark's. What the JSON envelope preserved was its own shape, not compatibility. Ark also already made this choice twice: Classic shipped market, oracle, and treasury bindings and the port kept only market's, unwired and on `wasmvm` v1. Stargate/gRPC **queries** require an explicit accept list — Wasmd ships no permissive default — written by hand as Osmosis, Neutron, Juno, and Archway keep theirs, every entry additionally required to carry the `cosmos.query.v1.module_query_safe` annotation, which keeps contracts to deterministic reads; the ICA host derives its own allow list from that annotation inside ibc-go and cannot be pointed at the Wasm list, so the two surfaces are separate by construction. Messages need no such list; the signer check is the gate. A Rust bindings crate is deliberately **not** committed to here: authors can generate from Ark's published protos, and publishing later is additive. That asymmetry decides the whole entry — adding a custom encoder later breaks nothing, removing one later breaks every contract using it, so the reversible direction is the one to start in. | Confirmed |
| D75 | Oracle rates are NOAH per one unit of the feed's denomination (`USD/NOAH`, not `NOAH/USD`); `RateSet.Convert` is `amount × rate[offer] / rate[ask]`, so valuing in NOAH multiplies. Settlement plan rates and the governance-supplied outgoing reference rate carry the same orientation. Every stored rate — settlement plan rates included, since a plan sits beside oracle rates in the registry's rate sets — is bounded at `MaxExchangeRate`, the magnitude a direct report is held to by `MaxEncodedVoteRateBytes`, and the tally omits a derived price above it, so the halt-class folds that multiply a 2^128-capped quantity by a rate stay inside the LegacyDec domain by construction. Flipped 2026-09-02; see `docs/DESIGN_NOTES.md` §1.4. | Confirmed |
| D76 | Prices and quantities rebase differently. `RateSet.Convert` re-expresses a quantity (`q × rate[old] / rate[new]`); a stored price of the reference unit — the exposure anchor — moves by the reciprocal factor (`p × rate[new] / rate[old]`), which is the same `Convert` call with the two units passed in the opposite order, so the chain keeps one arithmetic path and reversed arguments at a price site are the operation rather than a bug. Every rebase site states which it holds: the Market base pool, the Treasury tax cap, the base gas price, and the conversion-factor table (via the one-unit cross) are quantities; the anchor is the one price. | Confirmed |
| D77 | The sidecar resolves in the chain's orientation end to end: a feed's output pair is `UNIT/NOAH`, routes end at NOAH (`KRW/USD × USD/NOAH`), bootstrap prices are stated in leg orientation, and `PricesByFeed` re-keys a resolved price without inverting. Provider markets stay in the orientation their venue quotes; the resolver's per-sample normalisation (`providerSamples`) is the one place a venue quote meets a leg, so nothing after the provider cache inverts, and route averaging is an arithmetic mean of the published figure. First implemented 2026-09-02 with a single reciprocal at the feed boundary (`types.FeedPrice`); amended 2026-09-03. | Confirmed |
| D78 | The signed fee declares the transfer tax. A transaction's fee is gas plus the exact tax its messages owe; the ante refuses a fee short of the tax before deducting anything and charges the tax only from what was declared, so a signer sees the whole charge in the fee they sign and is never taxed past it. The fee field carries the declaration because it is the one slot every wallet already builds and every sign mode renders, amino included; an extension option would not survive amino signing, and the ante rejects them. A wallet that omits the tax is refused with an error naming it, which forces every client to price the tax before signing — the deliberate cost. The direct charge to `transfer_tax_collector` and the deletion of the routing step stand; only the subtraction returns, and the gas remainder alone is deducted — by an Ark-owned fee decorator in place of the SDK's, so simulation deducts what execution deducts. Amended 2026-09-03. Amended again 2026-09-03 (D80): the fee is a ceiling and the tip rides NOAH; the subtraction that made excess into gas is gone, the declaration is unchanged. | Confirmed |
| D79 | Rename `MonetaryPolicy` to `EconomicPolicy`, and the stability tax to the transfer tax. The committee's seven levers are three fiscal — the tax rate and the two block reward targets, which set what the protocol takes in and pays out — and four capital: the three fund target ratios and the D72 exposure weights, which set how much capital it holds against its liabilities and how fast it accumulates. None is strictly monetary. The committee cannot mint, quote, set spread, or move the pool, so it holds no rate or supply instrument; spread and pool depth are Market's, and the Reserve-to-Buffer commitment is a governance vote. `Economic` is the accurate umbrella over both halves, where `Fiscal` would misname the capital half and `Monetary` misnames the whole. The tax rename follows the same argument. Terra's stability tax funded validators through an adaptive controller holding unit mining rewards stable against the seigniorage cycle; §1 removed that controller, `x/mint`, and all seigniorage, and D11 gave the proceeds to the Oracle target first. What the tax does here is fund security and the price feed out of stablecoin usage, and compensate the dilution stakers still bear under §1.1. It moves no peg, quote, spread, or supply figure, so "stability" claimed a role it does not have, while "transfer" names the base D18 already taxes: every user-facing stable transfer surface. Bare `Tax` spellings stay — `TaxCap`, `ComputeTax`, `reference_tax_cap` — because inside Treasury the tax is the transfer tax and the Tobin tax belongs to Market and Oracle. Done now because it is free now: fresh genesis (D20) means the `transfer_tax_collector` account rename carries no migration, collection prefixes and proto field numbers are unchanged, and the cost after launch would be a store migration plus every client's message and query names. | Confirmed |
| D80 | The signed fee is a ceiling, and the tip is NOAH. Every fee leg but NOAH is charged at most what the chain computes it owes — the exact transfer tax in that denomination, plus the base fee if it is the first leg in denomination order whose slack above the tax covers the requirement — and the rest never leaves the payer. The NOAH leg is charged whole: it pays the base fee when no stable leg did, and the remainder is the tip, ranked in reference units per gas through NOAH's factor. NOAH is the one denomination the tax is never owed in (`GetTaxCap` excludes the numeraire), so it is the second signed number the fee field lacked: the chain can always tell a tip from a padded tax, which one denomination split by subtraction could not, and a wallet pads a stable leg for rate drift at no cost. Under D78's subtraction one percent over on a tax the size of Terra's cap was half a gas fee, paid to validators. What it costs: priority needs NOAH, so a pure-stable payer gets base-fee inclusion in arrival order within its lane, which the base-fee ramp already guarantees; and a NOAH-paid gas fee has no free headroom, its pad being a tip bounded by the fee it pads rather than by a tax. A NOAH leg is refused while NOAH's factor is absent, as a NOAH gas fee already is. D78's declaration stands: a fee short of the tax is refused before anything moves. The single-denomination rule and the excess refusal go with the subtraction. This is not a refund: nothing is escrowed or returned, the gas limit is still charged whole at the base price, and the ante deducts less rather than handing anything back. Amended 2026-09-03. | Proposed |
| D80 | Move `transfer_tax_rate` from `EconomicPolicy` to `Params`. The rate passes D37's test — a lever stating how much the protocol wants, reversible and safe to clamp — but D78 changed what kind of number it is. The rate is part of the fee every wallet signs, so a raise refuses every in-flight transfer until clients re-query, and a committee update lands the instant the message does; every other lever in the message moves protocol-internal allocation. Market's committee band over the Tobin tax is not a precedent: a swap carries the trader's own `minimum_receive`, so a Tobin raise only fails a swap that breaches a floor the user chose, where a transfer tax raise fails a transfer the chain itself refuses. Governance's voting period is the notice a raise needs and it already exists; the rate rejoins `reference_tax_cap`, so the one calculator D18 insists on has one owner and one proposal can activate rate and cap together; and the committee keeps the two reward targets without being able to raise the charge that funds them. The fast-cut argument for the committee is weaker than it looks: under D78 a cut never refuses an in-flight transaction, since the declared fee still covers the lower tax and the remainder is deducted as gas, so the only fast move worth having is in the safe direction and an expedited proposal covers it; a calculator fault is an upgrade, not a rate change. D79's classification stands — the rate is fiscal — only its holder changes. `EconomicPolicy` field 1 is reserved and the rate is `Params` field 12 under the same `[0, 1]` domain cap; the mandate bounds lose the field; `Query/Params` carries it and `Query/EconomicPolicy` does not; wallets are unaffected because they price through `ComputeTax`. | Confirmed |
| D81 | Hold `transfer_tax_rate` at or below the conversion spread floor. NOAH carries no tax cap, so it is the one untaxed denomination (D44), and `market.MsgSwap` is exempt (§8.3), so `MsgSwap` stable→NOAH, `bank.MsgSend` NOAH, `MsgSwap` NOAH→stable delivers the same value to the same recipient for spread instead of tax. The cap neither softens the requirement nor changes its shape: with `tax = min(rate × A, cap)` against a spread cost of `floor × A`, the substitution pays exactly when `rate > floor`, at every principal — below the cap crossover both charges scale with the amount, and above it a capped tax meets an uncapped spread. The bound is on the corridor rather than the live floor, because a conversion committee may lower `min_stability_spread` to `ConversionMandate.minimum_policy`; that minimum is the number governance must stay under when it sets either side. Nothing enforces this in code — Market depends on Treasury, so Treasury cannot read the floor, and neither module's stateless `Validate` sees the other's state — so it is a governance-time constraint on P1's launch rate and on every conversion appointment. | Confirmed |
| D82 | Charge the transfer tax after the messages, on success only. The ante prices the tax, holds the declared fee to it (D78), and deducts the gas fee and tip alone; a post decorator on the messages' branch charges the tax and draws the granter's allowance for it. Every judgement of whether the payer can afford the tax sits at the charge, none in the ante: the post decorator runs in CheckTx too, where the messages do not run, so it reads there what an ante check would have read and mempool admission is unchanged, while in a block it reads the balance the messages actually left. A transaction whose messages fail pays gas and no tax; a payer the messages leave short of the tax fails the transaction at the charge, gas kept, principal unmoved; an allowance records what left the granter and nothing more. Only the moment of the charge moves: the declaration, the ceiling and NOAH tip (D80), priority, and the direct charge to `transfer_tax_collector` stand. Not a refund — nothing is escrowed or returned, which is what the 2026-08-29 no-refunds finding foreclosed; the post chain returns for deferral, not settlement. Signed transactions thereby meet the terms D42 already set for a contract's dispatches, and the two seams agree. The ante hands the figure it held the declaration to on through the context, and the post charges that figure: the tax is computed once, the charge cannot exceed the declaration by construction, and a post chain finding no figure fails the transaction rather than passing a transfer untaxed. What it gains beyond fairness: a transfer whose tax the payer cannot afford until an earlier message in the same transaction has funded it now succeeds, where the ante charge this replaces refused it. What it costs: an IBC packet that later times out keeps its tax (D42, D46 unchanged); a doomed transaction pays gas for messages that then revert, rather than being refused before they run; and feegrant emits two use events per sponsored transaction. An ante affordability pre-check was written and removed: it bought no mempool protection the charge does not already give, refused transactions execution would have funded, and made a doomed transaction fail for free and stay valid to resubmit, since an ante refusal discards the sequence increment. Rationale in §8.6 and `docs/DESIGN_NOTES.md` §4.4. | Confirmed |
| P1  | Choose launch tax rate, reference cap Coin, three target ratios, subsidies, genesis fund balances, and the D72 exposure weights (zero at launch keeps `m` at one and the targets unscaled).                                                                                                                                                                                                                                                                                                          | Pending before launch  |
| P2  | Phase 3A found no recipient-output, fixed-price cycle, or split residual-mint amplification under coverage-based Buffer funding; add no residual-mint limiter.                                                                                                                                                                                                                                                    | Confirmed              |
| P3  | Apply the deterministic live-derived pool amount without comparing the submitted expectation to a rejection threshold; retain the submitted expectation in the transaction and emit the old and applied pool state for audit.                                                                                                                                                                                    | Confirmed              |
| P4  | Choose the launch economic-policy committee and bounds, Claims committee multisig and appointment window, the shared cancellation-period Treasury param, fixed gross committee claim limit, and operational fee funding.                                                                                                                                                                                                           | Pending before launch  |

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

**Amended 2026-08-08 (D33):** Market derives this figure and supplies it. The conversion already converts its output
through its quote rates in order to charge the spread — eligible principal is what remains of the offer after that
burn — so leaving the derivation with Treasury would mean re-converting at the same rates for the same answer, and
settlement now runs once per block over summed principal, holding no individual conversion's rates at all. The
definition, the rounding, and the bound are unchanged: the same truncated conversion of the final integer output,
refused when it exceeds the gross offer. Only the arithmetic moved; every allocation decision made from the figure is
still Treasury's.

### 3.4 Spread and dust

The difference between gross NOAH offered and eligible expansion principal: the spread the quote charged plus integer
dust. Amended 2026-09-02 (D6): it enters the expansion waterfall with the principal — fund targets first, overflow
burned — rather than burning unconditionally. When fund targets are full it burns exactly as before, together with the
unused portion of eligible principal.

### 3.5 Subsidy pool

The `treasury_subsidy_pool` module account's spendable NOAH balance. It is initially funded from genesis total supply,
has no mint permission, and covers only gaps between aggregate organic validator and Oracle funding and the accumulated
per-block targets at each completed reward-funding window. Anyone may irreversibly extend the pool by sending
already-issued `anoah` to the module account. Deposits change neither total supply nor the reward targets; they only
extend or restart shortfall coverage. There is no automatic refill, target balance, refund, withdrawal, or conversion
path.

### 3.6 Redemption Buffer

A Treasury-owned operational module account containing liquid NOAH retained from earlier expansions. Each redemption
receives the Buffer's actual pre-trade coverage share of its quoted NOAH output, measured against the claimable
liability — the supply that can currently redeem. Supply that cannot claim (a member whose feed is stale, suspended
supply without an activated plan) is excluded from that denominator and disclosed, so a suspension elsewhere never
switches the Buffer off for the healthy exits it exists to dampen. It is endogenous conversion inventory, not
collateral, solvency capital, or a first-come redemption pool. Anyone may irreversibly deposit already-issued `anoah`. A deposit increases the inventory
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
Reserve's live `anoah` balance is the balance counted toward its target. Beyond that balance, the Reserve may custody
derecognized (written-off or retired) transfer tax routed from `transfer_tax_collector` at settlement; that residue
earns no target credit, cannot move through the Reserve-to-Buffer commitment, and waits for a separate governed
disposal decision. A deposit grants no authority, withdrawal, or special claim on a later Reserve-to-Buffer commitment.
External custody and deployment are introduced only with the first separately approved external-asset policy described
in Section 20.

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

The denomination carried by Market's `BasePool` `sdk.DecCoin` is owned by Market. Ark launches with `axdr` as this
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
no stablecoin's offer or output eligibility; `axdr` remains fully supported.

A same-denomination depth change also rescales `ArkPoolDelta` by `new_base / old_base`. Both paths preserve the relative
curve position instead of allowing a governance parameter update to create an implicit imbalance reset or immediate
repricing without conversion flow.

## 4. Ownership boundaries

| Component                 | Owns                                                                                                                                                                             | Must not own                                                               |
| ------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| Market                    | Quotes, spread, final integer output, denomination-labelled virtual-pool state, conversion escrow, conversion mint/burn, atomic swap settlement                                  | Tax policy, claims mandate, target or fund-allocation calculations         |
| Treasury                  | Tax policy/routing, expansion-principal valuation and complete waterfall, fixed fund credits, subsidy pool, fund targets (`required_capital`), Redemption Buffer                                           | Quotes, spread, pool state, gross conversion custody, conversion mint/burn, claim adjudication or payment, a fund operator's `recognised_capital` |
| Claims (`x/claims`)       | Claims Mandate, immutable claim record, Insurance reservation and `claims_insurance` custody, Insurance `recognised_capital`                                                                               | Tax policy, fund targets, the expansion waterfall, liability valuation, strategic Reserve |
| Reserve (`x/reserve`)     | `strategic_reserve` custody, the governed Reserve-to-Buffer commitment, the Reserve committee mandate, the accounting journal, the recognition policy (eligibility and custody allowlist), Reserve `recognised_capital` (D56–D58)                                                  | Tax policy, fund targets, the expansion waterfall, liability valuation, claims |
| Oracle                    | Consensus prices, Tobin taxes, participation scores, Oracle reward allocation                                                                                                    | Fiscal allocation decisions, minting                                       |
| Distribution              | Validator/delegator fee accounting and payouts                                                                                                                                   | Tax classification, Buffer, Reserve, Insurance                             |
| App ante                  | Fee validation, feegrant semantics, transaction priority, invoking exact Treasury tax collection                                                                                 | Target-based allocation, a second tax formula, or persistent fiscal state  |
| Governance                | Market Params, Treasury policy, Claims Mandate, Claims submission/cancellation, Reserve mandates, role rotation                                                                  | Routine operations or automatic price/revenue controllers                  |
| Claims committee multisig | Off-chain adjudication and exact on-chain claim approval within the live Claims Mandate                                                                                          | Policy changes, direct custody, generic sends, Reserve use                 |
| Reserve committee multisig | Deployment to mandate destinations within the live Reserve mandate, and the fund's bookkeeping: quantity updates, return attribution, impairment marking, position closure (D56–D57) | Claims, parameters, recognition policy, corrections, impairment clearing, generic sends, mandate changes |
| Asset emergency committee | Immediate suspension of a failing asset within a live Asset Emergency Mandate (see `docs/ASSET_MODULE_PLAN.md`)                                                 | Recovery, resumption, settlement, write-off, retirement, reference choice, mandate changes |

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
    ) (sdk.Coin, error)

    DrawRedemptionBuffer(
        ctx context.Context,
        redeemedStable sdk.Coin,
        noahOutput math.Int,
        quoteRates oracletypes.RateSet,
    ) (math.Int, error)

    RecordSupplyChange(
        ctx context.Context,
        burned sdk.Coin,
        minted sdk.Coin,
        quoteRates oracletypes.RateSet,
    ) error
}
```

`RouteExpansion` returns `total_noah_burn` as an `anoah` coin and nothing else. The eligible principal, the three
credits, the two burn causes, and the target-valuation completeness flag are all execution-local: they are constructed
directly from the positive gross offer and a monotonically decreasing remainder, published through
`EventExpansionAllocated`, and then discarded. Market owns burn, mint, and payout, never a decision about which fund was
short, so returning the split would invite settlement to branch on Treasury policy. The returned total is exactly the
residue left in the Market module account once the credits are sent, which is why burning it settles the escrow rather
than acting on that policy.

`RouteExpansion` requires positive `anoah` as `grossOffer`, a positive native stable `stableOutput`, and the rate map
already used by Market's quote. Treasury may add missing liability rates to that map but never replaces an existing
quote rate. It derives eligible principal itself, captures any additional unchanged-Oracle rates needed for aggregate
target valuation, calculates the complete waterfall, and only then moves the three fixed NOAH credits directly from the
Market module account to the Redemption Buffer, strategic Reserve, and Insurance. Gross custody never passes through the
Treasury subsidy-pool account, Treasury never burns, and Market cannot supply eligible principal, gaps, credits, burn
amounts, module names, or destinations.

After `RouteExpansion` returns, Market neither revalues the output nor recomputes targets or routing. It burns the
returned coin, mints the final stable output, and pays the receiver. Any later settlement failure rolls back Treasury's
preceding fund credits in the same transaction cache.

`DrawRedemptionBuffer` accepts the execution-local rate map already used by Market for the quote, initializes the
block-local aggregate-liability snapshot from unchanged Bank and Oracle state when necessary, calculates actual Buffer
coverage against pre-burn liability, applies that capped coverage to the quoted NOAH output, and moves only that amount
from the Buffer to Market. It returns that payment alone. The liability figures behind the coverage share and the
completeness flag stay execution-local, reaching observers through `EventRedemptionBufferDrawn` rather than the caller:
Market owns burn, mint, and payout, never a decision about how the output was funded, and handing settlement a
valuation-state flag would invite it to branch on exactly what the claimable denominator exists to stop gating.
It exposes no path to strategic Reserve or Insurance. The separately authorised Reserve-to-Buffer message
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
| `strategic_reserve` | Strategic/emergency Reserve                   | None               | `anoah` only      |
| `claims_insurance`           | Covered-loss Insurance (owned by `x/claims`)  | None               | `anoah` only      |
| `transfer_tax_collector`    | Current reward-funding-window transfer tax   | None               | Blocked           |
| `oracle`                     | Oracle reward pool                            | None               | Blocked           |
| `fee_collector`              | Validator reward funding before Distribution  | None               | Blocked           |

Module-to-module transfers remain possible. The `BlockedModuleAccountsOverride` list in `app/app_config.go` replaces the
SDK default blocked set, so every intended blocked account must be listed explicitly. Leave all four Treasury custody
accounts out of that list so normal bank transfers can reach them. Market, Oracle, `transfer_tax_collector`, and
`fee_collector` remain explicitly blocked.

Each module provides a recipient-aware bank `SendRestrictionFn` over its own accounts: Treasury over the subsidy pool
and Redemption Buffer, `x/claims` over `claims_insurance`, `x/reserve` over `strategic_reserve`. Bank collects them into a module-keyed map, so every module
providing one must also be named in bank's `RestrictionsOrder` or app construction fails on a length mismatch. The
shared predicate lives in `pkg/chain.ValidateNoahOnlyDeposit`; per-fund custody allowlists (§20.1) are exactly why each
module owns its own restriction rather than sharing one flat address set.

- Transfers to any of `treasury_subsidy_pool`, `treasury_redemption_buffer`, `strategic_reserve`, or
  `claims_insurance` succeed only for a positive `sdk.Coins` value consisting solely of `anoah`; mixed or non-NOAH
  transfers fail atomically.
- The single exception is the exact sender/recipient pair `transfer_tax_collector` to `strategic_reserve`.
  It is enforced by `x/reserve`, which owns the recipient, even though the settlement flow that relies on it is
  Treasury's. It
  which passes through unchanged so settlement can route derecognized (written-off or retired) transfer tax into
  Reserve custody. The collector is a blocked account whose outflows are Treasury settlement code alone, and every
  consensus Reserve path reads only `anoah`, so the routed residue is inert custody: it earns no target credit, cannot
  move through `MsgTransferReserveToBuffer`, and waits for a separate governed disposal decision.
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
deposit remains subject to the ordinary transfer-tax rules for its user-facing transfer surface; the recipient does not
create an exemption.

No launch asset registry is needed: all four fund accounts have one allowed custody denomination, and consensus target
and settlement paths read only `GetBalance(..., anoah)`. The future external-asset phase adds explicit per-fund custody
and recognition policy together with the first approved asset; it does not pre-authorise arbitrary bank denominations.

Only Market may retain `Minter`. Tests must inspect the configured module-account permissions, not merely search for
calls to `MintCoins`.

## 6. Monetary flows

### 6.1 NOAH to stablecoin expansion

**Amended 2026-08-08 (D33): the exchange below happens once per block, not once per conversion.** The conversion keeps
everything that does not depend on liability or fund state. Market escrows the gross offer, converts `stable_output`
through `quote_rates`, truncates to `eligible_principal_noah`, refuses the conversion if that exceeds the offer, burns
`gross_offer.Amount - eligible_principal_noah` as spread exactly as it does today, mints and pays `stable_output`, and
adds the eligible principal to a transient block accumulator. `RouteExpansion` and `RecordSupplyChange` are gone from
the path; no Treasury call remains in it. Market's EndBlocker hands the block's totals to `SettleConversions`, which
performs the block's one liability valuation, runs the waterfall below against the summed eligible principal, executes
the fund credits from Market's account, and returns the overflow burn Market executes.

The calculation, the boundary branches, and every conservation identity below hold unchanged with
`eligible_principal_noah` read as the block's sum. Only `total_noah_burn` splits across two moments — its spread half
burned in the conversion that charged it, its overflow half at settlement — and the incomplete-valuation branch now
parks the whole block's principal rather than one conversion's. The closing paragraph's rule against deferring the
waterfall to BeginBlock, EndBlock, or an epoch is what this amendment reverses. What that rule protected still holds:
Treasury retains no gross offer, no pending allocation record exists — the accumulators are transient and die with the
block — and settlement stays atomic, with the atom now the block. A settlement failure fails the block rather than one
transaction, which is the deliberate trade: the hazard it replaces mis-allocated silently.

Amended 2026-09-02 (D6): the spread half no longer burns in the conversion. Market escrows the gross offer whole and
accumulates `gross_offer.Amount`; the waterfall below runs against the block's gross total, so `spread_and_dust_burn`
leaves the identities and the waterfall input is read as the gross sum wherever `eligible_principal_noah` appears.
Conservation becomes `gross_offer.Amount = buffer_credit + strategic_reserve_credit + insurance_credit + overflow_burn`
with `total_noah_burn = overflow_burn`. The eligibility check `eligible_principal_noah <= gross_offer.Amount` stays as
the bound that the stable minted is worth no more than the NOAH offered. Liability created is still `stable_output`, so
Buffer credit and coverage keep their meaning; the funds are credited the premium as well as the principal. The
incomplete-valuation branch parks the gross total. `EventExpansionAllocated` carries no spread field; its credits and
overflow now sum to the gross offer, and its note that the spread burns per conversion goes.

Inputs:

- `gross_offer`: positive integer `anoah` received from the trader and held in Market's transaction-local escrow.
- `stable_output`: final positive integer native-stable output after Market spread and minimum-receive validation.
- `quote_rates`: the Oracle snapshot already used by Market to produce that exact output.

Market passes those three facts to `RouteExpansion`; it does not pass eligible principal or any allocation decision.
Treasury validates the input denominations and derives the complete result. The first settlement in a block that needs
aggregate liability enumerates every nonzero-supply native stable and adds only missing rates to the shared
execution-local `quote_rates` map from unchanged Oracle state in the same execution context and block time. Treasury
may augment that caller-provided map in place, but it never refetches or replaces an existing quote. Treasury caches
the claimable aggregate together with its completeness flag. Later settlements in the block reuse that snapshot; after
each successful conversion Market advances it through `RecordSupplyChange`. Rates are fixed at preblock, so an
incomplete valuation cannot become complete within the block and is cached like any other. This does not make Market
enumerate Treasury liability denoms. A missing or stale unrelated rate excludes that supply from the claimable
aggregate and activates the conservative Buffer-only branch for the current expansion's routing; it does not change
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
bounded by the current remaining eligible principal and the final remainder becomes overflow burn. There is no separate
Treasury allocation-validation method. `RouteExpansion` returns an error when it cannot derive or execute the
allocation. Market treats a successful result as authoritative and does not duplicate Treasury's checks or
recompute its conservation result.

Settlement:

1. Market produces the final quote, enforces minimum receive, and applies the virtual-pool transition in the transaction
   cache.
2. Market transfers `gross_offer` from the user into the Market module account.
3. Market calls `RouteExpansion(ctx, gross_offer, stable_output, quote_rates)`.
4. Treasury derives the full allocation, transfers each positive fixed credit directly from Market to
   `treasury_redemption_buffer`, `strategic_reserve`, and `claims_insurance`, emits
   `ark.treasury.v1.EventExpansionAllocated`,
   and returns the execution-local result.
5. Market burns the returned `total_noah_burn` from its remaining escrow without recomputing or revalidating Treasury's
   valuation, targets, or waterfall.
6. Market mints exactly `stable_output`.
7. Market calls `RecordSupplyChange` with the actual NOAH burn and stable mint. If a liability snapshot exists,
   Treasury adds `stable_output` to it exactly once, preserving its completeness flag.
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
No separate stablecoin fee is minted and then burned; spread and integer dust remain in NOAH and, since the
2026-09-02 amendment of D6, enter the waterfall rather than burning outright.

### 6.2 Stablecoin to NOAH redemption

**Amended 2026-08-08 (D33): the draw happens once per block, and the conversion mints first.** Market burns the
complete stablecoin offer, mints the complete `noah_output` to the redeemer, and records the pair `noah_output` and
`redeemed_liability_noah` in a transient block accumulator, each valued at the rate its own path quoted — the fresh
oracle set for a swap, the plan's committed rate for a settlement conversion. `DrawRedemptionBuffer` and
`RecordSupplyChange` are gone from the path. At settlement the Buffer's share of the block's summed output is
transferred into Market and burned there, so the end state is exactly the split below — the redeemer holds
`noah_output`, the Buffer is down `buffer_paid`, net new supply is `noah_output - buffer_paid` — reached by
mint-then-burn instead of by minting only the residual. `residual_mint` therefore names an end-state quantity rather
than an executed mint, and the hard mint limit of step 5, should one ever be approved, binds that net figure for the
block. The visible consequence is that a same-block reader sees supply the settlement has not yet netted; committed
state at every height is unchanged.

The coverage arithmetic below is unchanged in meaning and evaluated once per block, after the waterfall, so a block's
own expansions replenish the Buffer its redemptions then draw against. `claimable_liability_noah_before` is
reconstructed at settlement as the freshly scanned net liability plus the block's summed redeemed value: the scan runs
after every burn, so adding back what the block retired recovers the pre-burn basis without a snapshot. One arithmetic
detail differs deliberately — the draw multiplies the summed output by the Buffer balance before dividing by that
basis, where the per-conversion form below divides first. Forming the ratio first rounds an intermediate against its
own bound of one, and the output then amplifies the error past the balance; multiplying first leaves a single rounding
on a quantity whose bounds are whole base units, which monotone rounding cannot cross, so `buffer_paid <= buffer` and
`buffer_paid <= noah_output` become theorems. It also floors the block's draw to within one base unit rather than
losing up to one per redemption. The bounds and the coverage monotonicity proof below are unaffected; they now apply to
the block's aggregate.

Inputs:

- `stable_offer`: gross stablecoins received from the trader.
- `noah_output`: final integer NOAH output after Market spread.
- `buffer_balance_before`: integer NOAH in `treasury_redemption_buffer` before settlement.
- `claimable_liability_before`: transient snapshot of the claimable liability before burning the offer, initialized
  from full bank supplies on the first aggregate valuation in the block. Claimable means the supply can currently
  redeem: members with a fresh feed plus suspended supply under a stored settlement plan. Supply that cannot claim — a
  member whose feed is stale, suspended supply without a plan — is excluded and disclosed; because a denomination
  without a fresh rate also cannot quote, and suspension without an active plan closes both exits, unvaluable and
  unredeemable coincide by construction. The one divergence is safe-side: a stored-but-unactivated plan is counted
  while its holders cannot yet claim, which only overstates the denominator.
- `quote_rates`: fresh offered-stable/NOAH rates already present in Market's execution-local pricing snapshot.
- `aggregate_rates`: rates captured by Treasury when the block-local snapshot is first initialized. They are read from
  unchanged Oracle state before any relevant stable burn; whether every recognised liability was valued is recorded on
  the snapshot as `valuation_complete` and disclosed, but the draw below runs either way.

Calculate:

```text
claimable_liability_noah_before = checked LegacyDec sum(
  aggregate_rates.Convert(full_supply(denom), anoah).Amount
  for every claimable native stable denom with nonzero supply
)

redeemed_liability_noah =
  quote_rates.Convert(stable_offer, anoah).Amount

buffer_coverage = min(
  1,
  checked LegacyDec(buffer_balance_before).Quo(claimable_liability_noah_before)
)

buffer_paid = truncate(
  checked LegacyDec(noah_output).Mul(buffer_coverage)
)
residual_mint = noah_output - buffer_paid
```

Treasury uses `math.LegacyDec` consistently for converted liabilities, aggregate liability, fund targets, and the
coverage calculation, truncating only the final integer principal or payment. Aggregate summation and coverage arithmetic use the checked
decimal helpers in `pkg/decimal`. Every arithmetic failure in the valuation path — converting one supply, summing the
aggregate, adding a pending stable output to it, or converting the stable output or redeemed stable input — is fatal to
its caller: the block for the priming scan, the transaction for a settlement. Never use floating point or an alternate
direct-rate multiplication path with a different rounding order.

This supersedes the earlier rule that treated `decimal.ErrOutOfRange` from aggregate summation as an incomplete
valuation. Incompleteness now means exactly one thing — recognised exposure that no honest rate could value — so the
conservative fallback and the `valuation_complete = false` flag describe a missing price and nothing else. Leaving
arithmetic out of range inside that flag conflated two conditions that behave differently: a missing rate is expected,
self-healing, and has a safe conservative answer, while a value outside the representable domain is state the protocol
does not support, does not recover on the next block, and would otherwise retire the Buffer permanently behind a signal
that already fires for benign reasons. Both are unreachable at any supply the chain can mint, so this chooses the
failure mode for an impossible state rather than a live one.

The coverage numerator uses the Buffer balance that actually exists and the denominator uses pre-burn claimable
liability. The gross stable offer still determines `redeemed_liability_noah` because the complete offer is burned and
removed from outstanding liability, but the Buffer funds its coverage share of actual post-spread output rather than a
nominal-liability entitlement. Always require `0 < noah_output <= redeemed_liability_noah` and
`redeemed_liability_noah <= claimable_liability_noah_before`; the redeemed denomination is claimable by definition —
it just quoted — so its full supply is in the denominator and the second bound holds for every reachable redemption.

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
   from the existing block-local snapshot and preserving its completeness flag.
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
- If the current offer is priceable but another nonzero-supply native stable lacks a fresh rate — or suspended supply
  carries no plan — that supply is excluded from the claimable denominator, the draw proceeds against the claimable
  aggregate, and the event discloses `valuation_complete = false`. The excluded supply cannot itself redeem while in
  that state, so nothing it will later claim is being spent; a suspension or lapsed feed elsewhere must not retire the
  Buffer for the healthy exits it exists to dampen, which is exactly when contagion redemptions arrive. This supersedes
  the original zero-draw rule. The cost accepted: frozen supply exerts no drag on coverage, so the Buffer spends
  proportionally faster during a freeze, bounded by the frozen fraction — inventory doing its damping work earlier
  rather than being preserved into the same empty-Buffer end state.

If an approved hard residual-mint limit exists and the requested mint would exceed it, the entire transaction fails
atomically. No stablecoin is burned and no Buffer NOAH is moved. Because a visible quota or gate creates first-mover
pressure, any such limit requires a separate policy review rather than an implementation-time addition.

Let `B` be the pre-trade Buffer, `L` pre-trade claimable liability, `R` redeemed liability, and `Q` quoted NOAH output.
Market's nonnegative spread guarantees `0 < Q <= R <= L`. Within any span where the claimable set is fixed, with exact
arithmetic and `B < L`:

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

At a boundary where the claimable set itself changes, the coverage ratio steps while the Buffer balance stays
continuous. A suspension shrinks `L`, so coverage steps up; a plan activation or returning feed re-enters supply and
steps it down. Neither direction changes any quote, output, or minimum receive — the funding split is invisible to the
redeemer — so a step creates no first-mover entitlement, and inventory can never jump, only the rate it is spent at.

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
`MsgTransferReserveToBuffer`, which lives in the `x/reserve` Msg service (D55). This is the only production path that may debit `strategic_reserve`. It is a
standalone Treasury policy action, never a Market callback or part of an individual redemption.

The request contains:

```text
authority
amount: sdk.Coin                    // positive anoah
minimum_reserve_balance: sdk.Coin   // nonnegative anoah after transfer
```

The general Treasury `authority`, configured as `x/gov` at launch, authorises the message. Do not use the Claims
committee or introduce a launch Reserve operator. Source `strategic_reserve`, destination
`treasury_redemption_buffer`, and denomination `anoah` are fixed in keeper code; the request has no recipient, purpose
selector, asset selector, conversion, or arbitrary call data.

Execution must:

1. Validate the authority and both canonical `anoah` Coins.
2. Require a positive transfer and a nonnegative minimum remaining balance.
3. Read the live Reserve `anoah` bank balance without consulting the Buffer balance, Oracle rates, fund targets, prices,
   or Market state.
4. Reject if the Reserve cannot fund the transfer or its post-transfer balance would be below `minimum_reserve_balance`.
5. Atomically call `SendCoinsFromModuleToModule` from `strategic_reserve` to
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
redemption rules: every redemption draws its actual coverage share of the claimable liability and reduces residual mint
without changing the quoted output or privileging a redeemer. Because the visible governance proposal cannot change the
quote or absolute output and the Buffer share follows live coverage, it creates no protocol-level first-redeemer
entitlement to the committed amount.

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
the virtual NOAH/stable pool. The basket and `axdr` remain valid in both directions; changing the pool unit does not
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
closing height from the params cancellation period; a committee claim additionally stores the mandate term and its
closing height must not exceed the mandate expiry, while a governance claim records term zero. During the half-open
cancellation period, governance may cancel any pending claim without depending on the current mandate term. The current
active committee may cancel only a non-governance-submitted claim and must supply the exact current term. At the
closing height, cancellation closes for both actors and the chain itself pays the immutable claim in EndBlock. Paid
claims are final; a claim the chain cannot pay ends failed and releases its reservation.

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

The committee threshold-signs an exact approval containing the policy version, unique bounded claim ID, a single bounded
`reference` pointing at the off-chain case record, recipient, and positive `sdk.Coins` amount. At launch the amount must
contain only `anoah`; keeping `sdk.Coins` preserves a later additive payout path without weakening launch validation.

The reference is one field rather than a separate incident and evidence pair. The chain applies the identical rule to
both — required, bounded, never interpreted — so splitting them states a distinction only the submitter can enforce,
at the cost of a second way for a submission to fail validation.

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
`claims_insurance`, reduces `insurance_reserved` by the same amount, marks the claim paid, and emits the audit event.
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

**The execution deadline and its expiry path are retired, not merely deferred.** They exist above to answer one
question — what happens to a claim nobody executes — and §10.5's automatic settlement answers it instead, by paying
the claim the mandate already authorised rather than voiding it. The deadline was the cheaper answer only while
settlement needed a signer: an expiry sweep needs the same height-keyed index and the same block hook that settlement
now uses, so keeping it would buy a second mechanism at the same cost. Should the extended model be revived, it should
be revived without them.

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
integer `anoah`. An aggregate exposure that cannot be represented while summing converted supplies is fatal to the
scan's caller, not an incomplete valuation; see the arithmetic rule in Section 6.2. Do not convert through XDR or divide
by a separate XDR/NOAH price.

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

The two sides of the contract below have different owners, and that split is the `x/treasury` / `x/claims` /
`x/reserve` module boundary (D54). Treasury computes `required_capital`, because it needs consolidated liability and
the governed target ratio. Each fund's operating module computes `recognised_capital`, because it needs that fund's own
encumbrance and, later, its eligibility entries, haircuts, and caps; it returns the figure to Treasury through
`RecognisedCapital`. The formulas themselves are unchanged.

Initially:

- Count the liquid `anoah` bank balance in Redemption Buffer and strategic Reserve toward their targets. For Insurance,
  count only `insurance_anoah_balance - insurance_reserved`; approved pending claims are encumbered and cannot
  simultaneously cover another loss.
- All four fund accounts reject mixed and non-NOAH deposits because every launch use is NOAH-denominated. *(Amended
  2026-08-05, D58: the strategic Reserve account now admits eligibility-listed denominations beside `anoah` — its send
  restriction reads the recognition policy — while the other three funds keep the NOAH-only rule. The
  `transfer_tax_collector` → Reserve exempt pair is unchanged.)*
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

*(Amended 2026-08-06, D64: `remaining_asset_cap_headroom` above is each asset's share of recognised capital itself
rather than a flat amount — `recognition_cap_ratio × recognised_capital`, the self-reference solved per block in
closed form. The concentration limit is therefore stated against the certificate it disciplines, the same denominator
prudential regimes use when they cap a component at a share of the measured stock, and it needs no governance upkeep
as holdings or liability move. Asset credit levers only the liquid unencumbered `anoah` term, so recognition can
never certify more than that base times a bounded multiplier, whatever is attested. Ratios across the policy must sum
strictly below one for the solve to exist. The correlated-group headroom term remains unimplemented while Insurance
is NOAH-only.)*

*(Amended 2026-08-05, D62: the Ark-issued rule is enforced rather than declared — a Reserve eligibility entry naming an
asset-registry member must be custody-only, checked at the policy write and at genesis import. D61 adds the disposal
side the recognition rules imply: custody the fund holds at zero credit, and NOAH above the requirement, can be burned
under authority split by what the burn can destroy.)*

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

**Amended 2026-08-05 (D56–D58):** for the strategic Reserve the liquid requirement is discharged structurally, and the
`allocation_gap = max(total_gap, liquid_gap)` machinery is deliberately not implemented for it. The Redemption Buffer
is the system's liability-scaled liquid tranche — `RedemptionBufferTargetRatio × liability`, `anoah`-only, with no
operator and no deployment path, filled first in the waterfall and drawn by redemptions — one slot above the Reserve.
The Reserve's own floor is the mandate's `minimum_liquid_balance`: absolute and appointment-scoped because the need it
serves is operational rather than exposure-tracking, and enforced at deployment time on the only debit paths the
account has, so it cannot be breached passively. `RecognisedCapital` therefore stays a single figure per operating
module, with no total/liquid widening (reserve extraction plan D4, closed). The cross-fund aggregation above likewise
lands as per-asset Reserve caps while Insurance is NOAH-only (claims plan D4).

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
- A priceable redemption always draws the Buffer's coverage share of the claimable liability, as specified in Section
  6.2. Unclaimable supply is excluded from that denominator and disclosed; it never switches the draw off.
- `FundStatus` always answers, and never presents a partial target as complete. When live valuation is incomplete it
  reports every target as zero, and each valued bucket carries exactly what the block could price beside a list
  enumerating what it excludes: untrusted suspended supply, written-off exposure, and members whose feed is stale or
  absent. Incompleteness is read off those lists rather than a separate flag — a non-empty `untrusted_suspended_supply`
  or `stale_member_supply` is what zeroes the targets, while `written_off_exposure` does not, because a write-off
  extinguishes the obligation rather than leaving it unvalued. A bucket may be partial precisely because its shortfall
  is disclosed, so no reader can mistake a partial sum for a whole one. Refusing to answer during exactly the stress that makes valuation
  incomplete would blind operators when they most need the report.
- Direct deposits trigger no target-driven transfer, trade, conversion, or deployment and create no depositor rights or
  authority over the governed Reserve-to-Buffer path.
- Claims Mandate submission and cancellation affect the Insurance reservation, unencumbered balance, and resulting
  coverage ratio/gap; neither changes required target exposure or ratio parameters, prices, issuance, another fund, or
  mandate term automatically.

Completeness is defined by outstanding liabilities, not by either configurable policy denomination. A missing XDR rate
alone is irrelevant when `axdr` supply is zero; nonzero `axdr` supply requires a fresh `axdr`/NOAH value like any other
liability. A stale `Params.reference_tax_cap.denom` rate affects cap refresh only. A stale Market pool-denom rate may
prevent a Market quote that needs the virtual pool, but it is not an additional Treasury aggregate-valuation dependency.

When the Redemption Buffer is at its target and prices are unchanged, an expansion increases both liability and Buffer
inventory according to the target ratio, while a later redemption releases the same actual Buffer fraction
proportionally. The response is continuous rather than zero issuance followed by a full-issuance cliff.

Keep nominal-liability valuation and target-exposure valuation as explicit concepts even while both initially use total
stable supply. Coverage-based redemption always uses nominal liability as the denominator for actual Buffer coverage. A
later risk-exposure model may replace the target denominator without silently changing redemption funding shares. Keep both
calculations single-sourced so that later target changes do not duplicate Market settlement logic.

### 7.4 Exposure multiplier

D72 supplies the risk-exposure model §7.3 closes by anticipating: it replaces the target denominator without
changing redemption funding shares. Targets become `ratio x m x L` where `L` is the same liability basis each
consumer already uses, and the draw keeps dividing by raw `L`.

**Why a stock measure is not enough.** Total liability states the size of the claim, not how hard it is to
service. The same nominal liability is a different exposure at five percent of NOAH market capitalisation than
at fifty, on a violently moving asset, under one-directional flow. Three indicators scale it, each already
maintained by the chain:

- **Liability ratio** — net liability over circulating NOAH. This is the dilution term: redeeming one unit of
  liability mints NOAH in proportion to it. Because liability is valued in NOAH (D24), it needs no extra rate
  read; circulating NOAH is total supply less the four dormant custody accounts at their raw balances, since the
  Reserve's recognised figure includes haircut external credit that is not circulating NOAH.
- **Realised volatility** — an EWMA of squared per-block returns of the protocol reference rate, annualised at
  read time. Oracle rates quote NOAH per one unit (D75), so the reference rate is the reference unit's NOAH price; its
  squared return is NOAH's own to the order the model reads.
- **Flow pressure** — an EMA of per-block net redemption value, taken from `ConversionTotals`, which already
  carries both sides NOAH-valued.

**Guardrails.** Weights, decays, cap, step, and period are governance `Params` (D72). `m` is floored at one, so
the inert configuration is exactly the unscaled sizing; capped; and rate-limited per update, so no single period
can move it far. Every input is a vote-median rate, a Bank supply, or settled flow that cost spread to produce —
no spot price enters. Sampling folds into settlement, which already runs each block; application runs on the
governed period.

**Consumption.** Requirement bases scale: the expansion waterfall on the net basis, and the bounds on committee
acts on the gross basis, which tightens the Reserve's burnable surplus and widens the shortfalls bounding
committee transfers into the Buffer and Insurance — both conservative directions, and `m` is protocol-computed
state rather than anything the bounded actor can reverse. `FundStatus` reports both target families and the
multiplier behind them. The draw does not scale (D73), and neither does the multiplier's own liability-ratio
input.

## 8. Transfer tax policy

### 8.1 Policy and governance settings

- `Params.transfer_tax_rate`: governance-owned rate in `[0, 1]`; moved from the committee's `EconomicPolicy` by D80.
- `Params.reference_tax_cap`: governance-owned nonnegative Coin with a canonical Ark-native base denomination, launched in
  `axdr`; zero means no tax ceiling.
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
| IBC v2 `MsgSendPacket`, when enabled           | Outbound taxable token in each transfer-port payload; other ports carry none |
| Wasm instantiate/execute, when enabled         | Attached taxable stablecoin funds                                   |
| Treasury, Oracle, governance, staking messages | No transfer-tax principal                                           |

`MsgSwap`'s exemption is what couples the transfer tax to Market's spread floor. NOAH is untaxed and a
self-returning swap is untaxed, so `MsgSwap` stable→NOAH, `bank.MsgSend` NOAH, `MsgSwap` NOAH→stable reaches the
same recipient with the same value for spread instead of tax. The substitution pays exactly when `transfer_tax_rate`
exceeds `min_stability_spread`, at every principal: below the cap crossover both charges scale with the amount, and
above it the capped tax meets an uncapped spread. Governance therefore holds the rate at or under the floor a
conversion committee can reach — `ConversionMandate.minimum_policy.min_stability_spread`, not the live floor (D81).

Every user-facing stablecoin transfer surface enabled in production must use the same Treasury calculator. Phase 4
implements IBC foundations before Wasm, because contracts may dispatch IBC messages, and designs the Treasury execution
hook into both paths from the start. Neither surface may become production-accessible until the complete activation gate
passes. Top-level IBC transfers and Wasm attached funds are visible to ante inspection. Contract-generated bank or IBC
submessages are discovered only during execution, so the custom Wasm/IBC integration must invoke the same calculator at
that execution boundary rather than pretending the `TxFeeChecker` can see them in advance.

A launch deposit to any Treasury fund is `anoah` and has zero transfer-tax principal. A mixed or non-NOAH deposit is
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
  floor(principal[input][denom] * transfer_tax_rate)                    if TaxCaps[denom] == 0
  min(floor(principal[input][denom] * transfer_tax_rate), TaxCaps[denom]) otherwise
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
uncapped_tax[input][denom] = floor(principal[input][denom] * transfer_tax_rate)

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

1. Refresh on the `Params.tax_cap_refresh_period_blocks` boundary, which defaults to a chain week. The cadence governs
   only the drift between membership changes — rule 2 refreshes on those independently — so it trades how closely the
   derived ceilings track the amount governance voted for against how often the whole map is rebuilt. It is a parameter
   rather than a constant for the same reason the amount it derives from is one, and is refused at zero, which would
   divide by zero in the modular boundary check. Because the check is stateless arithmetic on the block height, a change
   simply moves the next boundary; there is no active countdown to protect, unlike `reward_funding_window`.
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
3. Neither `MsgUpdatePolicy` nor `MsgCommitteeUpdatePolicy` does more than validate and store the
   candidate policy. No rate change rebuilds caps because the complete cap map exists independently and cap values do
   not depend on the rate.
4. If a reference-cap change cannot derive every candidate cap, reject `MsgUpdateParams` and preserve the old Params and
   cap map. A scheduled or denomination-mismatch refresh likewise retains the old map when valuation is unavailable.
5. Emit the complete derived tax-cap replacement from `MsgUpdateParams` or BeginBlock whenever either path successfully
   replaces the map. InitGenesis likewise requires or derives a complete map even when tax is disabled. If a configured
   taxable denomination is nevertheless missing at calculation time, `ComputeTax` fails closed when tax is positive.

Governance must configure the candidate denomination in Oracle and wait for a fresh settled rate before proposing the
Treasury cap change. A reference-cap update never changes Market's pool denomination: the amount is Treasury's to move,
the unit is not. Both units move together, and only through Oracle's `MsgSetReferenceDenom`, which rebases Treasury's
cap and Market's pool in one transaction.

**Where the reference-denomination invariant is enforced.** `Params.reference_tax_cap.denom` equals Oracle's configured
reference at every height, and every guard sits at a mutation point rather than at a read:

1. Keeper `InitGenesis` refuses an empty reference or a cap naming anything else. This is the only place Treasury ever
   reads the reference — once per chain lifetime.
2. `MsgUpdateParams` rejects any denomination change outright, so governance can move the amount and nothing else.
3. `MsgSetReferenceDenom` is the sole denomination-moving path. It runs Treasury's and Market's rebase executors before
   storing the new reference, refuses to run at all with an executor unwired, and fails the whole message if either
   executor fails, so no partial re-point can commit.
4. `RebaseTaxCap` refuses to convert from a denomination Treasury does not currently hold, so it cannot quietly repair
   state that is already inconsistent.

Nothing re-checks the invariant at runtime. The cap rebuild once carried a derive-time guard that halted the chain on a
mismatch; it was removed deliberately, because it fired only when a rebuild was owed — leaving any desync live until
the next boundary regardless — and its Oracle read made the rebuild depend on state the derivation never used.

The obligation this leaves is on future writers. Any new path that writes `Params` — an upgrade handler, a store
migration, a keeper method that does not exist yet — carries the invariant itself. Breaking it does not fail: rates
for the abandoned denomination normally keep existing, so every conversion still succeeds and the derived caps stay
denominated in a unit the protocol has left, indefinitely and silently. An upgrade that touches either
`Params.reference_tax_cap` or Oracle's reference must therefore repeat the genesis check, and its test must assert the
pair rather than each side alone.

### 8.6 Fee checking and routing

**Amended 2026-09-02: the ante construction below is superseded; its tax and feegrant semantics survive.** Three rules
no longer hold. First, the decorator list *is* copied into Ark, in `app/ante/ante.go`, because the Wasm decorators must
sit immediately after context setup and `sdkante.NewAnteHandler` admits no insertion point; an SDK upgrade adding a
decorator must therefore be mirrored by hand, and `app/ante/order_test.go` pins the assembled order. Second, there is
no wrapper around a completed stock handler and no routing step, because the tax no longer travels in the fee field:
the ante charges it straight from the fee payer to `transfer_tax_collector` on the terms the execution policy router
already used for contract, GMP, and ICA dispatches, so `GetFee()` is pure gas payment and items 2 through 7 of the fee
checker's contract are gone with the subtraction they described
(`docs/DESIGN_NOTES.md` §4.3, Phase 1). The checker instead prices gas against
Treasury's consensus base fee, which replaces node-local minimum gas prices rather than applying them. Third, the tax
is charged immediately after fee deduction rather than after the signature decorators. The claim that motivated the
old position — invalid signatures must not reach the charge — never needed the position: BaseApp runs the whole ante
on a cache branch it writes only on success, so a failed signature moves nothing wherever the charge sits. The rule
that replaces it is that a check able to refuse without moving a balance precedes the deduction, and a charge follows
it in the order money moves, so an underpriced transaction is refused by the fee gate before it is charged any tax.

**Amended 2026-09-03: the tax returns to the fee field as a declaration; the direct charge stays (D78).** Charging the
tax off the fee field left a signer no way to know the tax before signing and no bound on it after: the query was
advisory, nothing signed carried the figure, and a rate change between signing and inclusion was charged silently. The
fee field is the one slot every wallet already builds and every sign mode renders, amino included, so it carries the
declaration. The signed fee is gas plus the exact tax, and one Ark-owned `FeeDecorator` in `app/ante/fee.go`, in place
of the SDK's, holds it to the tax coin for coin: a fee short of the tax is refused before anything is deducted, so a
signer is never taxed past what they signed. What remains once the tax is set aside is gated against the base fee
under the single-denomination rule and deducted, so only gas reaches `fee_collector`; excess in the gas denomination
is gas, excess in any other is refused. The same decorator then charges the tax straight to `transfer_tax_collector`
as before, and a granter sponsors both through one draw on the allowance. The decorator mirrors the SDK's deduction
from `x/auth/ante/fee.go` at v0.54.3 under the mirror-by-hand caveat the decorator list already carries; owning it is
what lets simulation deduct what execution deducts — the SDK skipped its checker under simulation and deducted a
declared fee whole, tax included — so under simulation nothing is refused on fee grounds, what the fee covers of the
tax is set aside, and the rest is deducted unpriced, while the gate's reads are made and not enforced and a fee-less
estimate consumes a stand-in for the transfer it cannot make, so estimates track execution to within about a percent.
Item 7 of the old contract — return the whole fee and route the
tax onward — is the one thing not restored: the gas remainder alone reaches `fee_collector`, so no routing step
exists. `app/client/fees.go` adds the chain's tax to the fee it builds; a wallet that does not is refused with an error
naming the tax rather than charged silently, the deliberate cost of the declaration.

**Amended 2026-09-03: the fee is a ceiling and the tip is NOAH (D80).** The subtraction D78 restored had one
consequence D78 did not price: with tax a multiple of gas, any headroom a wallet declared on the tax — a percent for a
cap that re-derives every block — was deducted as gas. `FeeDecorator` now settles the fee by denomination instead of
subtracting. A stable leg is held to the tax it declares and charged that tax, plus the base fee if it is the first
leg in denomination order whose slack above the tax covers its own requirement; the rest is never deducted. The NOAH
leg is charged whole — base fee if no stable leg covered it, remainder tip — and only the tip ranks. The
single-denomination rule and the refusal of excess in a second denomination are gone: an accepted fee is any set of
stable ceilings plus at most one NOAH leg. A NOAH leg the factor table cannot price is refused, as NOAH gas is.
`app/client/fees.go` declares headroom on every stable leg and none on NOAH, and takes a `--tip` in NOAH. Feegrant,
simulation parity, the gentx waiver, and the execution-generated path are unchanged.

`arkd` prices every module transaction command this way, not only `swap-send`, through `PriceTransactions`
(`app/client/pricing.go`, wired by `dressTxCommands` in `cmd/arkd/cmd/root.go`). The command runs once, as written;
what is replaced is the `TxConfig` it builds through. Every transaction the SDK's factory builds comes from
`TxConfig.NewTxBuilder`, so a builder handed out there sees the messages and the settled gas — all the fee needs,
and the one place both are known. The fee is settled on the first read of what was built — the confirmation, the
sign bytes, the generate-only print — each of which precedes signing: the chain is asked then, about the
transaction's own messages and for the account it names as payer, so no keyring is involved, and `ComputeTax`
answers with the principal it taxed beside the tax, so the client keeps no message walk of its own. The parts of
the fee are printed above the SDK's confirmation, which shows one figure. An explicit `--fees` is left as declared; the
auth utilities under `tx`, which carry a finished transaction, are left alone, and a command that builds no
transaction — `gov draft-proposal` — reaches no builder and is untouched. Wallets meet the same rules through `Query/ComputeTax` and `Query/GasPrices`, and stock wallet fee logic
clears neither the tax declaration nor a moving base fee on its own, so a front end passes the fee explicitly.

**Amended 2026-09-04: the tax is charged after the messages, on success only (D82).** The ante wrote its branch the
moment it succeeded, so a transaction that then failed — out of gas, a send past what the fee left, a blocked
recipient, a contract error, a closed channel, an exhausted quota, one bad message in a bundle — paid the gas fee and
the tax on a principal that never moved, while a contract's dispatch had been atomic with its tax since D42. The
charge now sits in a post decorator, `TransferTaxDecorator` in `app/ante/transfer_tax.go`, which BaseApp runs on the
messages' branch: on success it charges the tax the ante priced and handed on through the context, draws a granter's
allowance for it, and moves it to `transfer_tax_collector`; on failure it does nothing. `FeeDecorator` keeps every judgement that needs no balance — the declaration
and the settlement, both of which need the computed tax, since a stable leg's slack above its tax is what pays the base
fee — and deducts the gas fee and tip alone, drawing the allowance for the
gas fee alone. Affordability is left entirely to the charge, which reads the balance the messages left, and which runs
in CheckTx as well, where the messages do not run, so the mempool refuses an underfunded payer exactly as an ante check
would have. A payer the messages leave short of the tax fails at the charge; BaseApp reverts the
messages and keeps the gas fee. The reverse case is what the move gains beyond fairness: a payer who acquires the taxed
denomination from an earlier message in the same transaction is taxed and succeeds, where the ante charge refused it. The fee and tip are named on the ante's tx event, the tax on the post's, since an ante
event outlives a failed transaction. Nothing is escrowed or returned, so the no-refunds finding of the dynamic fees
spec stands; the post chain returns for deferral, not settlement. Rationale in
`docs/DESIGN_NOTES.md` §4.4.

Unchanged below: the execution-generated tax path and its Wasm-cache atomicity, the feegrant asymmetry between signed
and dispatched messages, the retention semantics on message failure (since changed by D82), and the advisory status of
the tax query. The text is kept as the record of the wrapper model that was considered and replaced.

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
3. Moves the complete exact tax from `fee_collector` to `transfer_tax_collector` for every denomination.
4. Leaves only gas fees and overpayment in `fee_collector`.

This ordering means invalid signatures never reach routing. If routing fails, BaseApp's ante cache rolls back feegrant
usage, fee deduction, account sequence, and routing together.

For a contract-generated transfer that was not knowable in ante, the custom Wasm/IBC execution adapter:

1. Calculates the tax for that execution-generated transfer input independently.
2. Debits that tax from the sending contract account in the transferred denomination, in addition to the complete
   requested principal.
3. Sends the complete exact input tax to `transfer_tax_collector`.
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

Feegrant semantics remain standard for ante-visible tax: the granter pays the gas fee and the tax, each drawn on its
allowance as it is charged, so the allowance is never charged for a tax a failed transaction did not pay (D82, amended
2026-09-04). Feegrant does not make a granter or outer transaction fee payer responsible for
execution-generated tax incurred later by a contract; the sending contract pays it from its own balance.

The gas fee is retained when message execution fails after a valid ante, matching normal transaction-fee
semantics; the tax is not, since it is charged after the messages and only with them (D82, amended 2026-09-04). A
failure inside ante retains neither. Execution-generated tax commits only with its
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
- Complete transfer tax remains in `transfer_tax_collector` until Treasury settles the current funding window.
- Validator and Oracle funding retain separate per-block NOAH-value parameters, but Treasury adds the applicable target
  once for every observed completed block and compares only the aggregate window totals.
- The balance-constrained subsidy pool covers only aggregate shortfalls at settlement, not transient block-by-block
  gaps.
- Expansion proceeds fund the Redemption Buffer, strategic Reserve, and Insurance, not routine rewards.

Gas is the validator lane's first recurring source. Transfer tax protects the Oracle target before it can fund a
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
T = NOAH value of eligible transfer_tax_collector balances

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

A fee or tax denomination the registry cannot price counts as zero for that observation rather than poisoning the
window: availability is settled per denomination by the pricing verdict, and the priced remainder still accrues. The
collector's unpriced coins are partitioned before settlement values the pot — derecognized supply routed to the
strategic Reserve, everything still awaiting a feed left in place — so no unvalued coin reaches an allocation. An empty
fee collector needs no Oracle valuation.

Arithmetic that leaves the representable domain is not a valuation-availability question and gets no reward-side
fallback: an unrepresentable aggregate fee value or cross-block accumulator fails that BeginBlock transition, matching
the liability rule in Section 6.2. A window valued from a partially summed pot would misallocate real coins between
validators and Oracle rather than degrade safely, which is why the reward lane has no conservative branch to take.

The settlement flow runs in Treasury BeginBlock immediately before that block's Distribution execution:

```text
transfer_tax_collector -> fee_collector: validator_tax coins
transfer_tax_collector -> oracle:       oracle_tax coins
treasury_subsidy_pool   -> fee_collector: validator_paid anoah
treasury_subsidy_pool   -> oracle:        oracle_paid anoah
```

Organic funding above a target remains with its recipient and never causes a negative top-up or refund. The subsidy-pool
balance is already part of total supply, so top-up transfers change circulation but not total supply.

Until Phase 2 removes `x/mint`, the existing mint provision reaches `fee_collector` and is counted in `G` together with
gas because the collector does not label sources. This preserves the current boundary during Phase 1; after Phase 2, `G`
is organic transaction-fee value only.

### 9.3 Cosmos distribution genesis

Cosmos SDK v0.54.3 generates distribution genesis with `community_tax` set to 2%; left alone, that skims 2% of gas fees
and validator top-ups away from validators. Ark overrides the default in application code: `app/genesis.go` decorates
the distribution module basic so its default genesis carries `community_tax = 0`, and `ProvideModuleBasics` in
`app/app_config.go` installs it in the one BasicManager that `arkd init`, testnet init-files, and the app's own
`DefaultGenesis` all compose from. Every generated genesis therefore starts at zero without a hand edit;
`app/genesis_test.go` pins that for the CLI and the app alike. A live chain changes the value only through a governance
`MsgUpdateParams` to distribution.

Even with `community_tax = 0`, distribution keeps its internal community-pool ledger, `FeePool.CommunityPool`: rounding
remainders from allocation and reward withdrawal, a removed validator's residue, and the zero-previous-power fallback
all land there, and anyone may still deposit through `MsgFundCommunityPool`. `x/protocolpool`, the SDK's external
custodian for that ledger, is not wired, so the ledger stays inside distribution and distribution's own pool messages
are the live ones. This redesign does not fork distribution. It only ensures Treasury never deliberately funds the pool
and the configured community skim is zero; whatever lands there, governance can sweep with `MsgCommunityPoolSpend`.
Treat complete removal or redirection of SDK residuals as a separate policy decision.

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
  remaining SDK begin blockers

Transaction ante:
  deduct complete declared fee
  after successful signature/sequence ante, collect exact tax in transfer_tax_collector

EndBlock:
  market         // recover virtual-pool imbalance
  oracle         // settle reward period when due
```

Tax collected in block `h` remains in `transfer_tax_collector` until the current Treasury window settles. Gas fees
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

### 10.1 `Params` and `EconomicPolicy`

`Params` stores only governance-owned settings:

```text
1 reference_tax_cap                cosmos.base.v1beta1.Coin, launch denom axdr
2 reward_funding_window            uint64
3 transfer_tax_rate                cosmos.Dec / math.LegacyDec, in [0, 1] (D80)
```

`EconomicPolicy` is independent persisted state and is the complete reversible lever set:

```text
1 (reserved; transfer_tax_rate until D80)
2 validator_block_reward_target    cosmos.Int / math.Int, implicit base-unit NOAH
3 oracle_block_reward_target       cosmos.Int / math.Int, implicit base-unit NOAH
4 redemption_buffer_target_ratio   cosmos.Dec / math.LegacyDec
5 strategic_reserve_target_ratio   cosmos.Dec / math.LegacyDec
6 insurance_target_ratio           cosmos.Dec / math.LegacyDec
```

Remove the unused `PolicyConstraints` message.

Validation:

- Every Economic Policy `LegacyDec` and `math.Int` must be set and representable.
- Rate/share/ratio fields must be in `[0, 1]`.
- The three target ratios are independent stock targets; their sum may exceed one. The allocation waterfall, not their
  sum, determines how scarce expansion principal is routed.
- `Params.reference_tax_cap` must be a nonnegative Coin with a canonical lowercase Ark-native base denomination, and
  `reward_funding_window` must be positive. Each policy reward target must be set and nonnegative.
- Reward-target/window compatibility is not precomputed during Params, Economic Policy, or genesis validation. The
  active reward-funding state uses checked addition for each observation and fails the BeginBlock transition if an
  accumulated target is unrepresentable.
- Keeper-level genesis verifies the reference-cap denomination belongs to Oracle's configured native-stable set.
  `MsgUpdateParams` performs the same cross-module check while rebuilding caps when the reference Coin changes; pure
  Params validation does not pretend it can validate cross-module state. Both policy-update messages validate only the
  candidate policy, plus the mandate bounds on the committee path.

Safe defaults:

- The Params tax rate and the Economic Policy reward targets and fund target ratios default to zero until launch economics are
  configured.
- The Params reference cap defaults to zero `axdr`, producing explicit uncapped entries without genesis Oracle prices;
  the zero Params tax rate remains the inert default.
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
prefix 6: EconomicMandate
prefix 7: EconomicPolicy
prefix 8: ClaimsAllowanceUsed
prefix 9: NextClaimID
```

`EconomicPolicy` is the single stored source for the six reversible economic levers. It is read directly by tax,
reward-funding, fund-target, and expansion-routing paths; those values are not mirrored in Params or the mandate.

`EconomicMandate` stores the current committee address, chain-derived `uint64` term, half-open activation and expiry
heights, and complete minimum/maximum bounds for the reversible economic-policy fields. Its zero-bounded,
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
origin                         // ClaimAuthority: committee | governance
mandate_term                   // committee appointment; zero for governance
reference                      // bounded pointer to the off-chain case record
recipient
amount                         // sdk.Coin; anoah-only at launch
status                         // pending | paid | cancelled | failed
submitted_height
closing_height                 // scheduled while pending, actual once not
cancelled_by                   // ClaimAuthority: unspecified unless vetoed
```

One height covers the whole lifecycle. A separate finalisation height would restate `closing_height` rather than add
to it: settlement happens in the EndBlock of the very block a claim comes due, so a paid or failed claim closes at
exactly the height submission scheduled — and a cancelled claim never reaches the height it was scheduled for, making
that height a counterfactual rather than a fact worth keeping. Cancellation therefore overwrites `closing_height` with
the height of the veto. The cost of the merge is that stored-record validation can no longer check that a cancelled
claim closed inside its own window; the live guard in the cancellation handler, which refuses from the closing height
onward, is what actually enforces it.

`origin` and `cancelled_by` are both `ClaimAuthority` — one enum naming the two authority domains, filling two roles:
the domain that submitted the claim and, if it was vetoed, the domain that ended it. `cancelled_by` is a domain rather
than an address because the domain is the whole of what a cancellation decides. The governance authority is a
chain-wide constant, so storing its address states only what the enum already does; and a committee cancellation is
valid solely from the mandate current at `closing_height`, which leaves that address recoverable from the appointment
history. Sharing the type also makes `cancelled_by == origin` a meaningful comparison — the committee withdrawing its
own claim — which two mismatched types could not express. What this gives up is the committee address in the record
itself, which drops from state to the `EventClaimsMandateSet` history.

`submitter` stays an address for a reason the cancel side does not share: submission is the value-moving act, and it
stores `mandate_term` beside the address, so the two together pin the exact accountable party for a payout with no
lookup at all. Cancellation moves no value, consumes no allowance, and records no term.

The record likewise stores no free-text cancellation reason. A cancelled claim already carries which authority vetoed
it, at what height, and every immutable term it was submitted under; a required reason no rule reads and no party can
verify adds a field the signer must satisfy without adding a fact anyone can rely on. Where a stated reason exists it
is already on chain in the governance proposal that carried the cancellation.

Payment records no authority because there is none: see the settlement paragraph below.

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

- `TaxRate` item; the rate lives in `EconomicPolicy`.
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
9 economic_mandate
10 economic_policy
```

Subsidy pool, Redemption Buffer, strategic Reserve, and Insurance balances live only in bank genesis. Every fund may
begin only with `anoah`. Redemption Buffer and strategic Reserve count their complete balances toward their targets;
Insurance subtracts any imported reservation to derive its unencumbered balance. Treasury InitGenesis never mints them.

Default genesis disables Claims and economic delegation, stores the zero Economic Policy and zero reference cap, derives
a complete explicit-zero uncapped map, and uses zero accounting, no claims, and the canonical empty reward-funding state.
Production genesis must supply the approved P4 policy, zero accounting, and no pending/completed claims unless an explicit
test or export/import case requires otherwise. InitGenesis validates the canonical committee address, role separation,
unique claims, claim-status transitions, reservation sum, and `insurance_reserved <= insurance_anoah_balance`.
Export/import preserves the mandate, Insurance reservation, immutable claim fields, closing heights, cancelling
authorities, and statuses.

Reserve-to-Buffer history requires no Treasury genesis field. Bank genesis/export preserves the resulting account
balances, while governance genesis/export preserves the authorising proposals and outcomes.

Run Bank InitGenesis before Treasury, then have Treasury InitGenesis inspect all four fund-account balances and reject
genesis if any contains a non-NOAH coin. This closes the one path that does not pass through the runtime bank send
restriction.

Keeper-level InitGenesis must validate the Params reference-cap denomination against Oracle's configured native-stable
set and establish a complete cap set regardless of the Params tax rate. It verifies that a supplied set covers
every configured native stable and exactly matches the effective reference Coin, or fully derives it from valid genesis
Oracle prices. A zero reference cap instead derives a complete explicit-zero uncapped set without prices. Supplied
derived caps must all be zero when the reference cap is zero and positive when the reference cap is positive.

### 10.4 Queries

The launch Query service exposes only:

- `Params`.
- `EconomicPolicy` returning the current six reversible lever values.
- `EconomicMandate` returning the governed appointment and whether it is active at the current height.
- `TaxCap(denom)`.
- `TaxCaps`.
- `ComputeTax(messages)` using repeated `google.protobuf.Any` annotated as SDK messages.
- `FundStatus` returning:
  - `priced_liability` and `settlement_liability` as `sdk.DecCoin` values in `anoah`,
    valuing ACTIVE/ISSUANCE_HALTED supply at Oracle rates and SUSPENDED supply under an open plan at its committed rate;
  - `stale_priced_liability` as an `sdk.DecCoin` valuing stale members at their last known rate, kept
    apart from the priced bucket because the rate behind it failed the freshness gate;
  - `stale_member_supply`, `untrusted_suspended_supply`, and `written_off_exposure` as the three disclosure lists
    naming every exposure excluded from those buckets — respectively members whose feed is stale or absent, suspended
    supply with no plan, and derecognized supply. A non-empty one of the first two is what says a recognized exposure
    went unvalued, below which every target is zero; the third does not bear on it. No separate availability flag is
    returned, because it would restate what those lists already say;
  - `nominal_liability` as an `sdk.DecCoin` explicitly denominated in `anoah`;
  - `redemption_buffer_balance` and `redemption_buffer_target` as `sdk.Coin` values in `anoah`;
  - `strategic_reserve_balance` and `strategic_reserve_target` as separate `sdk.Coin` values;
  - `insurance_balance` and `insurance_target` as separate `sdk.Coin` values;
  - `subsidy_pool_balance` as the current subsidy-pool `anoah` `sdk.Coin`, including accepted deposits.
- `RewardFunding` returning the stored aggregate state, including `blocks_remaining`, without requiring Oracle prices or
  fund valuation. The separate `Params` query exposes the configured length that the next empty state will use.
- `ClaimsMandate` returning the current committee appointment, allowance used/remaining, and whether the appointment is
  active at the current height; the shared cancellation period is exposed by the `Params` query. Every figure it returns
  is term-scoped and resets with a mandate replacement.
- `InsuranceBalance` returning the fund's custody balance and the amount reserved against pending claims, both `anoah`
  `sdk.Coin` values, as is every quantity the Claims Query service returns.
  These answer separately from the mandate because neither is term-scoped: the reservation spans the pending claims of
  every mandate and a replacement never clears it, so returning it beside figures that do reset invites reading the two
  at the same scope. Their difference is the recognised capital `FundStatus` reports.
- `Claim(claim_id)`. Its canonical REST binding is `GET /ark/treasury/v1/claims/{claim_id}`.
- `Claims` as a paginated audit and operations view of claim records.

Do not add a Treasury Reserve-transfer history query. `FundStatus` and Bank queries expose current balances; the
authorising governance proposal, signed Treasury message, and standard Bank transfer event expose the action history.

At launch, the Buffer and Reserve balances count in full toward their targets. `FundStatus` must additionally return
`insurance_balance` and `insurance_target` as separate `sdk.Coin` values, where the balance is the capital `x/claims`
recognises — its module balance less the reservation held against approved pending claims. Treasury asks `x/claims` for
that figure rather than reading the Insurance account and re-deriving it; the reservation itself is `x/claims` state,
exposed by its own `InsuranceBalance` query, and `FundStatus` does not restate it. The standard paginated Bank query
remains the canonical custody view. `FundStatus` must not return a combined backing or health percentage. The launch
query exposes only nominal liability; a later risk-exposure target model must add explicit fund-specific exposure fields
rather than overloading that value. Future external-asset queries must report gross custody, recognised value,
recognition haircut, liquid value, applied encumbrance, and zero-credit reasons separately rather than folding them into
one opaque balance.

Remove:

- `TaxRate` as a separate state query; read it through `EconomicPolicy`.
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

The claim messages moved to the `x/claims` Msg service with D54, under the `ark.claims.v1` package and the
`ark/claims/` Amino prefix. Their signers, validation, and semantics are unchanged; only their home is. Every name
stays inside the 39-character Amino limit, and the shorter path is what buys the headroom.

The launch Treasury Msg service exposes:

Governance-signed messages:

- `MsgUpdateParams`.
- `MsgSetEconomicMandate`.
- `MsgUpdatePolicy`.

Committee-signed messages (term-checked, one exact appointed committee each):

- `MsgCommitteeUpdatePolicy`.

The `x/reserve` Msg service exposes one governance-signed message,
`MsgTransferReserveToBuffer`, under the `ark.reserve.v1` package and the `ark/reserve/` Amino prefix (D55). Its
signer, validation, and semantics are unchanged; only its home is.

The `x/claims` Msg service exposes:

Governance-signed messages:

- `MsgUpdateParams`, carrying the governance-owned `claim_cancellation_period_blocks`, which moved out of Treasury
  `Params` with the mandate it governs.
- `MsgSetClaimsMandate`.
- `MsgSubmitClaim`.
- `MsgCancelClaim`.

Committee-signed messages (term-checked, one exact appointed committee each):

- `MsgCommitteeSubmitClaim`.
- `MsgCommitteeCancelClaim`.

There are no permissionless claim messages. Settlement is the only remaining transition and the chain performs it
itself, so no role signs for it.

Every action a committee may take is its own message type, so the complete committee surface is enumerable from the
proto service alone rather than by reading handler branches. This matches `x/asset`'s emergency mandate and the future
Reserve mandate in Section 20.2. The governing principle: **roles with disjoint powers or different execution semantics
get separate messages per role; one message serves several roles only when they are the same action under the same
rules.** In Treasury no role passes that second test — governance and each committee differ in staleness guard,
allowance metering, or cancellable set — so every role gets its own message. Each handler is one positive authorization
assertion followed by a shared keeper core (`applyEconomicPolicy`, `submitClaim`, `cancelClaim`), so effect logic
cannot drift between roles while authorization stays legible per message.

Two consequences of the split are deliberate. Governance messages carry no `expected_term`, because no governance
authorization depends on a mandate: the cancellation period `MsgSubmitClaim` once pinned through its term now comes
from the governance-owned Treasury params. And a committee message
naming the wrong signer is rejected before any term or window reasoning, because identity is checked in the wrapper;
a disabled mandate therefore reports a committee mismatch rather than an inactive mandate.

Do not add `MsgFundSubsidyPool`, `MsgFundRedemptionBuffer`, `MsgFundReserve`, or `MsgFundInsurance`. Deposits use
ordinary bank `MsgSend`/`MsgMultiSend` paths plus Treasury's recipient-specific restriction, so they need no Treasury
message, signer rule, receipt, or persistent record.

`MsgSetEconomicMandate` is governance-signed and replaces the complete committee appointment. Treasury derives the next
term as `current.Term + 1`. The reviewed implementation has no separate maximum-term exhaustion check: a nonempty
appointment whose increment wraps to zero is rejected by configured-mandate validation, while an empty disablement can
store the wrapped zero term. An empty committee otherwise disables the mandate; a configured message supplies the exact
committee address, half-open activation/expiry heights, and complete minimum/maximum policy bounds.

Treasury's `MsgUpdateParams` is governance-only and replaces the complete governance-owned settings, including the
reference-cap Coin. It cannot change a tax rate, reward target, or fund target ratio. The shared claim veto window for
both origins, `claim_cancellation_period_blocks`, is now `x/claims`' own governance-owned param (D54); keeping it
outside the mandate is what stops a mandate replacement from changing it.

`MsgCommitteeUpdatePolicy` contains one complete candidate policy signed by the exact stored committee. It is
accepted only during the active term and window, with the current expected term and every field inside the mandate
bounds. `MsgUpdatePolicy` is the governance form of the same effect: it applies any structurally valid
candidate, overriding those bounds without depending on the mandate at all. Neither path may change the
governance-owned reference cap in Params. Its Amino name is `ark/treasury/MsgCommitteeUpdatePolicy` — the
protobuf-derived name under the module prefix, inside the SDK 39-byte type-name limit.

`MsgSetClaimsMandate` is governance-signed and replaces or disables the complete committee appointment. Treasury derives
a new monotonically increasing term. An empty committee disables the mandate; otherwise the message supplies the exact
committee, half-open activation/expiry heights, and a positive fixed committee claim limit. That limit is an `sdk.Coin`
rather than a bare amount, so its `anoah` denomination is validated rather than assumed, and it reads at the same
shape as the allowance figures the Query service reports against it. The Claims
committee must be a distinct address from the Treasury authority; it may be the same address as the
economic-policy committee. The current params
cancellation period must not exceed `expiry_height - activation_height`; equality permits a claim at the activation
boundary to close exactly at expiry. A successful replacement resets Claims allowance used to zero without
changing the Insurance reservation.

`MsgCommitteeSubmitClaim` is signed by the exact committee and `MsgSubmitClaim` by the governance authority. Both
validate the same positive `anoah` amount, recipient, references, and live Insurance coverage, and both derive the
closing height from the params cancellation period. Only the committee message carries `expected_term`: it is
accepted only during the active window, its closing height must not pass mandate expiry, and its claim stores the
mandate term, while a governance submission never reads the mandate and stores term zero. The recipient must be
neither the Insurance account itself nor any address Bank currently blocks from receiving module-account sends.
Treasury assigns the next globally monotonic `uint64` claim ID, returns it in the response, and reserves the amount
without moving coins. The message type decides the immutable stored origin: committee submissions must fit within and
permanently consume the current term allowance; governance submissions do not consume it. Genesis persists the next
claim ID and enforces the same Bank-receivability invariant for pending claims; an app upgrade that changes the blocked
set must explicitly migrate any affected pending claim.

**Settlement is automatic and carries no message.** The `x/claims` EndBlocker pays every pending claim at or after its
stored closing height, using only the immutable stored recipient and amount, independent of later mandate
replacement, disablement, or expiry. Requiring a signer would have made payment a liveness problem rather than a
protocol one: an unsettled claim is not merely unpaid but permanently encumbering, since its reservation continues to
depress the capital `x/claims` reports to Treasury for as long as it stands — and the recipient, who by construction
has just suffered the covered loss, is the party least able to send the transaction. Nothing about paying a stored
claim is discretionary, so nothing is lost by removing the signer.

**The sweep is uncapped, deliberately.** A per-block execution limit would bound nothing consensus does not already
bound. Every claim costs a transaction, so the claims created in any one block already fit inside that block's limits;
and because the cancellation period is a single stored value, due heights are simply the submission heights shifted by
it. One settling block therefore answers exactly one submitting block, at comparable per-claim store cost — the
amplification is a constant factor over work a valid block already carried, not an unbounded burst. Shortening the
period does not break this: stored heights are never rewritten, so the worst collision puts one old block's claims and
one new block's on the same height. A cap would only defer payments the chain had already accepted the cost of, which
is the stall automatic settlement exists to remove. The one path the argument does not cover is genesis, which is not
gas-metered — but an import of N pending claims already validates and writes all N, so the import dominates the first
sweep, and its content is a deliberate operator artefact.

Each payment runs against a cache, so a failure writes nothing of its own. **A claim the chain cannot pay ends
`failed`, not pending**, and its reservation is released with it: nothing about a due claim changes with height — the
amount, the recipient, and the coverage test are all fixed — so a retry would re-run an identical computation, and a
claim left queued would encumber Insurance forever. A failure means an invariant broke and wants investigating.
`EventClaimFailed` carries no reason: the cause is a chain-generated error, and putting error text in state or in a
consensus-visible field would make message formatting consensus-critical. It goes to the node log. No reachable
failure is known in any case — Insurance pays out through this path alone, submission holds `balance >= reserved`, and
the Bank-blocked set is fixed at app wiring.

A queue entry that disagrees with its claim is dropped without touching the record, rather than treated as a failed
payment, so a divergence cannot overwrite the status of a claim already paid or cancelled. The sweep removes the key it
iterated rather than one rebuilt from the record, so such an entry cannot outlive its sweep.

Settlement and the veto window cannot contend for the same claim. Cancellation is refused from the closing height
onward, so a claim stops being cancellable when block H opens and is settled after that same block's transactions.
The EndBlocker ordering in `app_config.go` places Claims after `x/gov` for readability, but correctness does not
depend on it.

`MsgCancelClaim` is signed by the governance authority and `MsgCommitteeCancelClaim` by the exact current committee.
Both may cancel only before the stored closing height. Governance may cancel any pending claim without depending on
the current Claims Mandate, so its message carries no expected term. The committee must be in the current active
appointment, supply its exact expected term, and may cancel only a claim whose immutable origin is not governance.
Cancellation releases the reservation, drops the claim from the settlement queue, records the vetoing domain in
`cancelled_by`, and emits `EventClaimCancelled`; no separate governance-cancellation flag is stored. Neither message
carries a reason or reference: see the claim record above.

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
Market, or create transfer-history state. The message has zero transfer-tax principal as a Treasury governance action,
and its internal Bank movement is not independently taxed.

Do not add `MsgWithdrawReserve`, a Buffer-to-Reserve message, a generic deployment recipient, or any other Reserve
action at launch. External-asset acquisition, liquidity positions, grants, claims, rewards, and direct redeemer funding
remain outside this message.

### 10.6 Module config

Keep only field 1 `authority`, defaulting to the governance module account. The Claims committee address lives in
governance-controlled Claims Mandate state, and the economic-policy committee lives in its governed mandate state, not
immutable app module configuration. The Reserve-to-Buffer message uses the general authority and adds no
`reserve_authority` configuration. Do not reserve or retain `claims_authority` or `reward_collector_name` in this
fresh-genesis schema.

### 10.7 Events

Replace legacy policy/seigniorage events with:

- `ark.treasury.v1.EventTaxCapsRefreshed` with the typed `tax_caps` a pass wrote (renamed from
  `EventTaxCapsUpdated` on 2026-08-10 so the cap rebuild and the D72 exposure refresh, which share the
  period/pending/retry mechanism, share its vocabulary; the skip event was never built — a pass that cannot
  derive leaves `tax_cap_refresh_pending` raised and retries, and the detail stays in logs).
- `ark.treasury.v1.EventBlockRewardsToppedUp` with the `anoah` denomination and separate validator/Oracle target,
  organic-funding, and exact-payment amounts. Shortfall is derived from target and organic funding, while the remaining
  subsidy balance is queryable Bank state.
- `ark.treasury.v1.EventBlockRewardTopUpSkipped` with an `EventSkipReason`; detailed errors remain in logs, and collected
  transfer tax is sent entirely to Oracle.
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
transfer-tax collection and allocation rely on their fixed module-to-module Bank movements. `EventClaimPaid` remains
as the domain link between a claim ID and its Bank payment.

Do not add a Treasury-specific deposit event for any fund. The canonical bank transfer event already records sender,
recipient, and coins; standard Bank queries report custody, while `FundStatus` reports target comparison balances. Every
accepted launch deposit is `anoah` and therefore has no transfer-tax principal.

On incomplete aggregate valuation, `EventRedemptionBufferDrawn` records the actual claimable-coverage payment and sets
`aggregate_valuation_complete = false`, disclosing that the draw ran while some recognised exposure was excluded from
the denominator, without exporting internal liability valuations.

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
- Store the canonical tax cap as a governance-changeable Coin launching in `axdr`, independent from Treasury's direct
  NOAH-equivalent liability valuation and Market's changeable pool unit.
- Make `BasePool` a denomination-bearing `sdk.DecCoin` launching in `axdr`, and support one live pool-unit transition
  through the existing `MsgUpdateParams` without adding a transition object, message, or parallel pool.
- Treat submitted `BasePool.Amount` as a non-binding audit expectation on a denomination change; retain it in the
  transaction, derive and store the amount from one fresh deterministic conversion, emit the old and applied pool state,
  and atomically rescale `ArkPoolDelta`.
- Price stable-to-stable conversion directly and keep Treasury fund calculations independent of Market's pool unit.
- Keep `axdr` as a normal supported offer and output after the basket becomes the flagship and Market pool unit; the
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

No execution-result type is added. Both fund entry points return bare values — `RouteExpansion` the `anoah` burn coin,
`DrawRedemptionBuffer` the `anoah` payment — and publish the eligible principal, credit split, burn causes, and
completeness flags through their typed events instead. Treasury constructs those figures through checked, bounded
arithmetic and returns an error rather than exposing a separate result-validation API.

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
- `x/treasury/keeper/capital.go` and tests.
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
and the disabled sentinel; keeper-assigned globally monotonic claim IDs, sequence exhaustion, the bounded required
reference, and numeric REST lookup; recipient validation; rejection of Insurance and Bank-blocked
recipients before any accounting write; positive `anoah`-only amounts; pending-reservation and held-balance boundaries;
checked closing-height addition; rejection when the cancellation period would cross mandate expiry; atomic rollback;
no Bank send on submission; and pending-genesis recipient validation.

Settlement tests must prove the EndBlocker pays the immutable stored claim at, and never before, its closing
height, including after mandate replacement, disablement, or expiry; alters no recipient, amount, reference, origin, or
mandate term; never pays a cancelled claim or pays one twice; settles the whole due queue on one block, both for a
backlog already overdue and for many claims sharing a height; ends an unpayable claim `failed` with its reservation
released, terminally and without stalling the claims behind it; and drops a queue entry that disagrees with its claim
without rewriting that claim's status. Cancellation
tests must prove governance can cancel any pending claim before the closing height without depending on the current
mandate, while the committee must hold the current active appointment and exact term and cannot cancel a
governance-origin claim. Both paths release the exact reservation, clear the settlement queue entry, and move no coin.

Target tests must prove the recognised Insurance capital `x/claims` reports is its module balance less
`insurance_reserved`, which is the `insurance_balance` `FundStatus` returns: submission opens the gap without moving
coins; settlement decreases balance and reservation equally without opening a second gap; and cancellation releases the
reservation and closes the corresponding gap. None changes required exposure or the target. That settlement leaves
recognised capital unchanged is also why no EndBlocker ordering constraint arises against Treasury.

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

Keep target gaps, `RouteExpansion`, and coverage-based Buffer funding in `capital.go`. Keep the shared aggregate
valuation and transient snapshot mechanics in `liability.go`. Keep reward-window observation and settlement in
`reward_funding.go`, Claims lifecycle handling in `claims.go`, and the governed Reserve-to-Buffer handler in
`msg_server.go`. Inject Treasury's module-scoped `store.TransientStoreService`; the cache is derived state with no
genesis field, export surface, or migration. Use the shared `RateSet.Convert` path and checked `LegacyDec`
aggregation consistently for nominal liability, target exposure, expansion-principal valuation, and redemption
coverage. Treasury fund logic must have no permanent `XDRBaseDenom` dependency.

`RouteExpansion` must validate positive `anoah` gross input and native-stable output, preserve every existing caller
quote rate, add only missing liability rates, derive eligible principal from the final integer output, reject output
value above the gross offer, count the pending output exactly once in post-trade exposure, and calculate the complete
Buffer → Reserve → Insurance → overflow waterfall from bounded remainders before performing only the positive fixed
Market-to-fund transfers. It must never accept caller-computed principal, target gaps, credits, burn amounts, sources,
or destinations; debit the Treasury subsidy-pool account; retain gross offer; or invoke Bank mint/burn. Its result keeps
spread/dust and overflow burn
distinct and contains no redundant stored total burn. An unrelated missing aggregate rate uses the Buffer-only fallback,
while inability to value `stable_output` itself fails before any transfer.

The preblocker primes the block's snapshot; the first `RouteExpansion` or `DrawRedemptionBuffer` that finds none (the
lazy fallback) must scan the configured stable supplies and capture missing rates, then write the claimable
NOAH-equivalent value and its completeness flag to transient storage. Rates are fixed for the block, so an incomplete
valuation is cached like a complete one. The snapshot must advance only through `RecordSupplyChange` after Market has
successfully applied the corresponding burn and mint. The update ignores NOAH supply changes, values every stable delta with the conversion quote's fixed rate values,
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
`redemption_buffer_target_ratio` must not change the coverage-based draw. An absent XDR rate must be irrelevant
when `axdr` supply is zero, while nonzero `axdr` supply still requires a fresh `axdr`/NOAH valuation like every other
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
effective reference Coin changes; tests cover a successful `axdr`-to-another-denom switch, direct reference-cap
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
  recipient, and pass through the exact `transfer_tax_collector` to `strategic_reserve` pair so settlement
  can route derecognized transfer tax into Reserve custody. The restriction owns no state, conversion, or redirection.

Restriction tests must also compose a preceding address-rewriting restriction and prove Treasury applies the NOAH-only
rule to the rewritten final destination. Because Treasury is recipient-based, outbound reward top-ups and Insurance
claims remain unrestricted by their source account, while every inbound non-NOAH module transfer to any fund must fail.
The Reserve transfer passes the final-recipient rule because it sends only `anoah`.

The currently unwired `x/treasury/wasm` adapter contains a stub general query and references deleted state. Remove it in
this phase if a production call path is still absent. Reintroduce a real adapter when custom Wasm is wired; do not keep
a misleading half-wired surface.

### 13.5 Account wiring required for Phase 1 to compile and operate

Modify `app/app_config.go` narrowly:

- Register `treasury_subsidy_pool`, `treasury_redemption_buffer`, `strategic_reserve`, and
  `claims_insurance`, all with no permissions. Do not register a module account named exactly `treasury`.
- Register blocked `transfer_tax_collector` with no permissions; it is the active reward-funding-window staging
  account, not a Treasury fund.
- Remove Treasury's `Minter` permission.
- Leave the subsidy pool, Redemption Buffer, strategic Reserve, and Insurance accounts out of the explicit bank blocked
  list so ordinary deposits may reach them; enforce the fund-specific denomination rules through Treasury's send
  restriction.
- Keep every required infrastructure account blocked and explicitly include at least Market, Oracle,
  `transfer_tax_collector`, and `fee_collector`; the override replaces rather than augments the SDK default list.
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
  transaction after Phase 4, already-valid ante gas fees and transfer tax retain their normal message-failure behavior.
- Unrelated recipients retain ordinary Bank behavior.
- Deposits change no total supply and create ordinary bank events; after Phase 4 activates taxation, taxable stable
  deposits additionally use the ordinary transfer-tax events.
- Intended protocol `anoah` credits from Market to Buffer, Reserve, and Insurance pass the same final-recipient
  restriction; a non-NOAH module credit to any fund fails.
- A preceding restriction that rewrites an address into any fund cannot bypass the NOAH-only rule.
- Outbound reward top-ups and authorised Insurance claims are not rejected merely because of their source module
  account.
- A passed governance message can move only positive `anoah` from `strategic_reserve` to
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
- Aggregate liability is scanned exactly once per block, by the preblocker immediately after oracle price application
  and vote-target advancement; transactions never rescan. The transient snapshot tracks every later successful Market
  stable burn/mint. A single transient key holds either a marshalled valuation or a one-byte unavailable sentinel, so
  the two states are mutually exclusive by construction, every transaction-time lookup is one read, and priming is
  authoritative for the block without having to retract anything an earlier prime wrote. The sentinel cannot collide
  with a valuation because `LegacyDec.Marshal` emits big.Int decimal text, which never contains a NUL byte.
- An incomplete preblock valuation marks liability unavailable for the entire block. Rates are fixed at preblock, so an
  intra-block retry cannot succeed, and availability returns at the next block's preblock. A mid-block governance
  change to the tobin whitelist does not lift the unavailable state early; the block stays conservatively degraded. The
  lazy scan in `cachedLiabilityValue` remains as the fallback and itself records the block on incompleteness, bounding
  free-metered scans to one per block. That fallback is not dead code: simulation runs against `checkState`, whose
  transient store is always empty, so it takes the lazy scan on every call.
- Transaction-time liability valuation charges a flat 2,000 gas whatever it finds. Measured against the KV gas config
  that transient store-service reads are metered with, the single lookup costs 1,003 gas when unvalued, 1,006 on the
  unavailable sentinel, 1,066 for a typical valuation, and 1,291 for the largest storable `LegacyDec`, so the constant
  strictly covers every lookup with at least 709 gas of headroom. Swap gas is therefore position-independent within a
  block and independent of whitelist size, and simulation quotes exactly what execution consumes. Query-path valuation
  (`FundStatus`) stays normally metered so node query gas limits keep bounding its work.
- Only Market may change tobin-denom supply during a block. Preblock priming makes this a hard requirement rather than
  an implicit one, because the snapshot now precedes every transaction. Module account permissions enforce it: only
  `market` and `ibctransfer` may mint, and IBC mints only `ibc/` voucher denoms, never native listed denoms.
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
- The only launch production path naming `strategic_reserve` as a bank-send source is
  `MsgTransferReserveToBuffer`; it is general-authority gated, `anoah`-only, uses the fixed Buffer destination, and
  checks its per-proposal minimum remaining Reserve balance.
- A successful Reserve-to-Buffer action satisfies `Delta Reserve = -amount`, `Delta Buffer = +amount`, and zero change
  to total supply, consolidated stable liability, Params, targets, Market state, quote, tax balances, subsidy pool, and
  Insurance. It succeeds without complete Oracle valuation and creates no duplicate Treasury history state.
- No Buffer-to-Reserve, arbitrary-recipient, conversion, claim, reward, or direct-redeemer Reserve path exists.
- Treasury's general authority remains governance. The Claims committee is a mandate role with exact canonical matching;
  it cannot update Params, change policy, access Reserve, send generically, mint, or borrow.
- Claim submission enforces exact role authorization, unique ID, positive `anoah`-only amount, recipient/reference,
  derived closing height, live Insurance coverage, remaining Claims allowance when committee-origin, and audit record
  before encumbering funds.
- Insurance reservation never exceeds Insurance balance. Approval changes `insurance_reserved` and the unencumbered
  balance but no Bank balance or supply; execution changes balance and reservation equally; cancellation or expiry
  changes only reservation and claim status; no path changes target exposure or performs conversion.
- At the closing height cancellation closes for committee and governance, and automatic settlement runs. No target or
  query releases the reservation before cancellation, successful settlement, or failure.
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
  fee coins. Transfer tax remains in its collector until the funding window settles.
- A reference-cap update commits the candidate Params and complete derived cap map together or changes neither.
  InitGenesis establishes the complete map even while tax is disabled, and no transfer-tax-rate change rebuilds it.
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
  occur only after a complete window. Transfer tax remains in its collector between settlements.
- `UpdateRewardFunding` owns only observation and persistence. `BeginBlocker` owns the genesis-height gate, boundary
  decision, settlement call, and state reset; `SettleRewardFunding` is settlement-only.
- The target-aware waterfall is unchanged after replacing per-block `V`, `O`, and `G` with their window aggregates.
  Required fee valuation failure marks the window incomplete; settlement then sends all tax to Oracle and spends no
  subsidy.
- Focused Treasury tests cover accrual, exact boundary/reset behavior, quiet/busy-block netting, target/tax allocation,
  scarce subsidy, multi-denomination rounding, valuation fallbacks, query/genesis state, and Bank-failure rollback.

Implementation note: `MsgTransferReserveToBuffer` uses the compact Legacy Amino identifier
`ark/treasury/MsgTransferToBuffer` because its full protobuf-derived name exceeds the SDK type-name limit. Its
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

Do not fork the Distribution keeper. Ark zeroes `community_tax` at the module basic (`app/genesis.go`, wired through
`ProvideModuleBasics` in `app/app_config.go`), so generic `arkd init` and testnet defaults already carry the launch value
and the canonical launch `genesis.json` assembled during launch readiness inherits it rather than patching it in.
`docs/GENESIS.md` tracks that deliverable.

### 14.3 Activate balance-constrained reward top-ups

Treasury BeginBlock must run immediately before Distribution. It observes the completed block's eligible validator-fee
value every block, retains transfer tax while the active reward-funding countdown is positive, and settles when it
reaches zero. Test:

- Height 1 records no observation; under the default, the first clean-genesis settlement occurs after exactly
  `DefaultRewardFundingWindow` completed blocks.
- A mid-window `reward_funding_window` update leaves the active countdown and boundary unchanged; the first observation
  after reset initializes the countdown from the new value.
- Before the boundary, targets and eligible validator-fee value accrue, transfer tax remains untouched, and no subsidy
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
- `transfer_tax_collector` is emptied even when targets are zero or valuation is unavailable; the fallback sends all
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

**Amended 2026-08-08 (D33): the Treasury call surface described in this phase no longer exists.** `RouteExpansion`,
`DrawRedemptionBuffer`, and `RecordSupplyChange` were replaced by the single `SettleConversions` call Market's
EndBlocker makes once per block; the liability snapshot the phase's caching rules govern is deleted (D39, D68). The
phase text below stays as the as-built record of how Market settlement was first implemented — its pool, quoting,
minimum-receive, and transition work is untouched by the change. Read §6.1, §6.2, and
`docs/DESIGN_NOTES.md` §3.3 for the current settlement contract, including the
failure matrix of §15.4, whose per-transaction rows for the three deleted calls are now one block-level settlement
row.

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

- `x/market/types/params.go`: construct the launch `BasePool` in `axdr` and validate positive canonical DecCoin data.
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
   `chain.XDRBaseDenom`: rate-snapshot capture, offer normalisation, constant-product inputs, ask normalisation, and
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
   and `axdr` rates. Treasury observes the shared Oracle configuration and derives the corresponding tax cap; no
   Treasury Params change is required merely to make the denomination Market's pool unit.
2. Submit one Market `MsgUpdateParams` whose `BasePool` carries the basket denom and governance's expected basket
   amount, together with any intended recovery-period or minimum-spread changes.
3. At execution, retain the submitted value in the successful transaction, emit the old and applied pool state, apply
   the fresh live-derived basket amount, and rescale `ArkPoolDelta` atomically. Do not alter stablecoin output eligibility.
4. Keep `axdr` in Oracle, Tobin-tax, Treasury-tax, and Market support. Both NOAH-to-`axdr` and basket-to-`axdr`
   conversions remain valid, so `axdr` may continue to be minted as an ordinary Ark stablecoin.
5. Change `Params.reference_tax_cap` to a basket-denominated Coin later only if governance wants the basket to become
   the tax-cap reference; that separate Treasury update neither drives nor repeats the Market transition.

Do not add a Treasury orchestration message, a Treasury-to-Market keeper dependency, or a requirement that both modules
change Params together. Governance may coordinate the two independent policy choices operationally, but each module
validates and writes only its own state.

Ark never runs parallel XDR and basket virtual pools. Basket and `axdr` liabilities may coexist indefinitely and both
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
- Expansion (D6, 2026-09-02): `gross_offer_noah = buffer_credit + strategic_reserve_credit + insurance_credit + overflow_burn`.
- Expansion: `eligible_principal_noah <= gross_offer_noah`.
- Expansion: `total_noah_burn = overflow_burn`.
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
  `total_noah_burn` coin returned by `RouteExpansion`.
- Redemption: `noah_output = buffer_paid + residual_mint`.
- Stable offers are burned exactly once.
- Stable outputs are minted exactly once.
- Spread/dust never enters Redemption Buffer, strategic Reserve, or Insurance.
- Expansion fills Redemption Buffer, then strategic Reserve, then Insurance.
- Every priced redemption requires `0 < noah_output <= redeemed_liability_noah`. Complete valuation additionally
  requires `redeemed_liability_noah <= aggregate_liability_noah`.
- `buffer_coverage = min(1, pre_trade_buffer / claimable_liability_noah)` and `buffer_paid` equals
  `floor(noah_output * buffer_coverage)`, for every priceable redemption regardless of valuation completeness.
- For non-final redemptions within a fixed claimable set, cross-multiplication proves the post-redemption actual
  Buffer-per-liability coverage does not fall because quoted output cannot exceed redeemed liability.
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
  hard-coded XDR unit.
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
- Changing `BasePool.Denom` changes no stablecoin's offer/output eligibility. `axdr` remains a valid output after the
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
changes the pool unit from `axdr` to `ausd`,
applies the live-derived amount, rescales delta, preserves stable-to-stable quotes, confirms `axdr` remains a valid
NOAH conversion output, and confirms an ordinary app-genesis export preserves the denomination-bearing Params and
post-settlement delta. The binary built at `/private/tmp/arkd-phase3`; the known listener-dependent app and command
suites passed in the loopback-capable test environment.

The user review and independent call-path review completed on 2026-07-20. The independent review's arithmetic boundary,
rollback-coverage, and stale-plan findings were remediated and verified; Phase 3 is reviewed.

## 16. Phase 4: Wasm/IBC foundations, transfer-tax integration, and multi-denom Oracle rewards

Status: **Reviewed 2026-09-06, and the §16.6 matrix landed the same day. The hub ships shut behind an empty client allowlist and the contract runtime ships open; opening IBC is a governance decision**

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
- The dispatcher sends tax directly to `transfer_tax_collector` and collects it in the same Wasm submessage cache as
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
  defaults in generic application genesis construction, but set Ark's canonical launch genesis allowed-client list
  empty (amended 2026-09-06 from `07-tendermint`): it is the one switch behind every IBC surface, and governance admits
  `07-tendermint` when it opens the hub. The conditional 08-Wasm installation below does not change that launch
  authorization; any other client type requires a separate policy and implementation decision.
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
  family resolve and compile cleanly. If installed, keep it dormant at launch: Ark's allowed-client list admits nothing
  at launch and the canonical launch genesis contains no Wasm-client checksums. `09-localhost` is part of the core
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
- The Classic ICS-20 stack as first implemented was transfer, packet forwarding, then rate limiting; the v2 route was
  transfer wrapped by the v11 rate limiter. ICA controller and host keepers and routes are installed. The Wasm slice
  (2026-08-29, §16.3) inserted callbacks between transfer and packet forwarding in Classic and between transfer and rate
  limiting in v2, and added GMP through the shared execution router. 08-Wasm passed the compatibility gate and is
  installed dormant, as D50 requires.
- `app/app_config.go` owns module accounts and lifecycle order. `cmd/arkd/cmd` merges IBC module basics into genesis and
  client encoding and exposes their query and transaction commands.
- Generic `DefaultGenesis()` deliberately retains the upstream module defaults. The launch posture lives in
  `app/genesis/genesis.json`, whose tests pin the empty allowed-client list, the disabled transfer and ICA settings, the
  empty ICA host allowlist, the absence of 08-Wasm checksums, and the open contract runtime (2026-09-06). Filling in the P1 balances and
  parameters and reviewing the finished artifact remain Phase 5 launch-readiness work (`docs/GENESIS.md`).

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

IBC foundation code ships production-disabled behind the empty launch allowed-client list (D45 as amended). The vote
that admits `07-tendermint` is the activation, made after rate limits are configured for every route.

### 16.3 Wasm foundations

Approved 2026-07-21. Implement Wasm after the real IBC transfer boundary exists. Use upstream Wasmd `v0.70.3` and
`wasmvm/v3`: that Wasmd release targets the SDK v0.54 and IBC-Go v11 dependency family used by Ark. The initial
read-only dependency resolution found no family conflict, but the final resolved graph and compilation against Ark's
newer patch versions remain implementation gates. Do not add replacement directives to force compatibility and do not
maintain an Ark Wasmd fork. If the clean integration fails, stop the slice and reassess the dependency rather than
copying the runtime.

**Gate result, 2026-08-11: failed against SDK v0.55.0. The slice is stopped here pending a dependency decision.**

Module resolution is clean — every Ark version holds (SDK v0.55.0, CometBFT v0.40.0, ibc-go v11.2.0), Wasmd v0.70.3
brings `wasmvm/v3 v3.0.7` as specified, and the only movement in the graph is an indirect go-ethereum bump. The break is
at compile time: SDK v0.55.0 completed the deprecation of `x/params`, leaving the in-tree directory as a README, and
Wasmd still imports `github.com/cosmos/cosmos-sdk/x/params` in four non-test files — `x/wasm/exported/exported.go`,
`x/wasm/migrations/v2/params_legacy.go`, `x/wasm/keeper/test_common.go`, and that migration's test. The code moved to
`cosmossdk.io/x/params` under a different import path and is itself only at `v0.2.0-rc.1`, so requiring the extracted
module cannot satisfy those imports; only a Wasmd source change can.

The failure is shallow rather than architectural, which is what makes it a scheduling question instead of a redesign.
`x/wasm/types` compiles clean against the whole v0.55 family, so no deep API divergence exists; `keeper`, `exported`,
`migrations/v2`, `client/cli`, and `simulation` fail on nothing but `x/params`. What blocks Ark is legacy Subspace
migration scaffolding that a D20 fresh-genesis chain can never execute. Upstream has not fixed it — Wasmd `main` at
2026-07-28 still requires SDK v0.54.0 and carries the identical imports. Two verification traps are worth recording for whoever retries this: Wasmd's `v1.0.0` tag is from 2019
and sorts highest under semver, so a bare `go get github.com/CosmWasm/wasmd` silently fetches SDK v0.36-era code; and
`go get` reports success on the module graph alone, so the gate is only met by compiling Wasmd's packages.

**The same gate passes against SDK v0.54.3**, verified the same day in an isolated probe module. Wasmd v0.70.3 compiles
clean against Ark's pre-upgrade family exactly — SDK v0.54.3, CometBFT v0.39.3, ibc-go v11.2.0, `wasmvm/v3 v3.0.7` — so
D49's pin was sound and the slice is executable on v0.54.3 today. Holding there costs less than the first assessment
implied: the Oracle vote-accounting rotation fallback is not a fix that would be given up, because v0.54.3 has no
consensus key rotation at all. Neither `MsgRotateConsPubKey` nor a historical consensus-address index exists before
v0.55.0, so the fallback is unreachable by construction rather than merely unused. The v0.55 couplings in the tree are
correspondingly narrow: the removal of `x/protocolpool`, which v0.55.0 deletes upstream; the staking key-rotation fee
pool and its atto-denominated default; `auth.NewAppModule`, which still takes the legacy `exported.Subspace` on v0.54.3
and therefore still needs `app/params`; and that Oracle fallback. Everything else in flight is version-independent —
`abci/lanes` and `x/oracle/...` both build against v0.54.3 unchanged.

The file-level implementation should:

- Add the upstream Wasm keeper, store, module, module account permission, node configuration, CLI/genesis basics,
  snapshot extension, pinned-code initialization, and native Classic and v2 contract IBC routes through Ark's existing
  manual runtime-registration boundary.
- Delete the unused `x/wasm/exported` `wasmvm` v1 parser/query interfaces and `x/market/wasm` with them (D74); retain no
  parallel legacy adapter and register no custom message encoder.
- Admit 32-byte addresses. Ark's sealed `AddressVerifier` accepted 20 bytes alone, which is every key-derived and module
  address but not a contract: CosmWasm derives both the classic and the predictable form to 32. Left as it was, every
  message naming a contract fails address validation, so the runtime is unusable rather than merely restricted. Found by
  the application simulation once the Wasm module was registered, and fixed in `app/params/address.go`.
- Wrap the SDK message router supplied to Wasmd rather than forking Wasmd's message parser or dispatcher. The wrapper
  receives the exact SDK message produced by Wasmd's canonical encoder, invokes Treasury's canonical tax calculator,
  collects execution-generated tax from the unique sending contract account, and then calls the existing handler in
  the same cached execution boundary.
- Supply that same Treasury-aware SDK message router to the v2 GMP keeper. GMP performs its own derived-account signer
  authentication; the shared wrapper then charges that derived account for any taxable execution-generated principal
  before dispatch. Top-level signed messages continue to use the ordinary application router and remain ante-owned.
- Register no custom querier. The tax estimate D43 requires is Treasury's `Query/ComputeTax`, admitted through the
  accept list below like any other read; a contract prices the proto form of the message it will dispatch. Do not
  parse through a second Ark-owned message model.
- Supply the Stargate/gRPC query accept list by hand, as Osmosis, Neutron, Juno, and Archway keep theirs (D74). Wasmd
  ships no permissive default, so an absent list leaves contracts unable to read Ark state at all; a permissive one,
  Terra Classic's deny-list, leaks nondeterministic reads into consensus.
- List a path only when it is also annotated `module_query_safe`, which a test enforces. The annotation says a query is
  deterministic; listing says something further, that its response shape is frozen for the life of the chain, because a
  value a contract reads is an input to consensus and reshaping it afterwards is a coordinated upgrade rather than a
  patch. The list was populated on 2026-09-06 with fourteen paths (`app/wasm_query.go`): the tax estimate and the caps
  and gas prices behind it, the Oracle rates and reference unit, Market's quote, pool, policy, and Tobin tax, and the
  asset registry — what a contract needs to price and route a conversion or a transfer. Everything else stays unlisted,
  since widening is additive while narrowing breaks every contract that came to depend on a path. All 44 annotated
  queries were audited for determinism on 2026-08-11 and none failed; `Query/FeedReferents` is deliberately unannotated
  because its claims are operator prose, so rewording one would become a state-machine change. The annotated set is
  consensus-reachable regardless of the Wasm list, through the ICA host's `MsgModuleQuerySafe`, which ibc-go derives from
  the annotation with no chain-side override; the golden list of annotated paths, the `FeedReferents` exclusion, and the
  determinism smoke test guard that surface (`app/query_safe_test.go`).
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

Decided 2026-09-06: the contract runtime ships open at launch, anyone may upload and instantiate. The Section 16.6
matrix has landed, its tax seams are in place (the ante for attached funds, the policy router for dispatch), the accept
list is populated, and the empty IBC client allowlist leaves contract channels on both stacks unopenable.

### 16.4 Ante and Treasury tax integration

Review the currently retained `app/treasury_ante.go` implementation against the approved Section 16.1 contract. Decide at
the file-level gate whether to retain that direct app layout or move the behavior into `app/ante`; do not refactor solely
to match a planned directory. Decided 2026-09-02: the behaviour moved into the `app/ante` package, which owns the whole
chain and the message policy the ante and execution seams share; `app/treasury_ante.go` is gone.

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
- `anoah` fund deposits create no transfer-tax principal.
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

*(Landed 2026-09-06. `app/testutil/ibc.go` runs ibc-go's testing coordinator over Ark chains, signing through Ark's
own ante. On it, `app/ibc_relay_test.go` drives relayed transfers and packet-forward multi-hop, unknown-channel
refund, timeout refund, and the no-second-tax hop; `app/gmp_relay_test.go` executes, refuses, and fails GMP over a
real v2 client; `app/ibc_callbacks_test.go` delivers source, acknowledgement, timeout, and destination callbacks into
the CosmWasm ibc-callbacks contract (v2.2.2, checksum-verified, embedded from `app/testdata`), and covers destination
failure, source authorisation, non-blocking source failure under the gas cap, and malformed metadata.
`app/wasm_contract_test.go` covers, on Wasmd's reflect contract, contract-generated Bank and IBC transfers, attached
and contract-to-contract funds, nested submessages and independent caps, caught and uncaught rollback, insufficient
balance, and fund credits; `app/wasm_query_test.go` covers the tax query across every message shape, fail-closed
inputs, no writes, and contract-side reads of listed and unlisted paths. The contract runtime ships open (§16.3).
Opening IBC is a governance sequence: admit `07-tendermint`, configure rate limits for every route, then the transfer
flags.)*

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
- Feegrant granter pays gas plus tax, each drawn on the allowance as it is charged; a failed transaction draws the gas
  fee alone.
- Simulation performs no collection and no persistent writes.
- Invalid signature produces no persistent fee, tax, sequence, or feegrant change.
- Collection failure rolls back the complete ante.
- Message execution failure retains the valid gas fee, charges no tax, and rolls back message state; a payer that
  execution leaves short of the tax fails at the charge with the gas fee retained and no principal moved.
- Multi-denom collection moves the exact per-denom tax into `transfer_tax_collector`.
- Target-aware allocation conserves every denomination exactly between validator and Oracle destinations.
- Validator tax rounds down per denomination and Oracle receives every integer remainder.

IBC and Wasm execution tests:

- Ark's canonical launch genesis admits no client type, starts ICS-20 send and receive disabled, leaves both ICA sides
  disabled, gives ICA host no allowed messages, and contains no 08-Wasm checksums; the contract runtime is open to
  everybody. If 08-Wasm is installed after its compatibility gate, its client type remains unavailable under this launch
  policy.
- Classic and v2 transfer reuse one transfer keeper and preserve their distinct routing and timeout semantics.
- Classic transfer has the effective inbound order rate limit, packet forward, callbacks, transfer; v2 has rate limit,
  callbacks, transfer. Classic packet forwarding still works through callbacks, while no v2 PFM route exists.
- Governance rate limits apply per denomination and channel/client, reject an excess before transfer execution, restore
  accounting correctly on failure/timeout, and are configured for every route before activation.
- Packet forwarding covers multi-hop success, downstream error, retry, timeout, and refund without assessing a second
  transfer tax or bypassing the final Bank recipient restriction.
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

Status: **In progress since 2026-09-06**

### 17.1 Full-app tests

Skipped by decision on 2026-09-06, and §17.2 with it (§21).

Add:

- `app/economic_policy_test.go` for deterministic lifecycle and supply invariants.
- `app/economic_policy_sim_test.go` if a bounded stateful simulation is stable and maintainable.
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
- Actual threshold-multisig Insurance submission followed by the shared cancellation delay and automatic settlement;
  exact half-open cancellation/settlement boundary; overlapping pending claims; exact reservation accounting; committee
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
- Successful and failed Economic Policy cap-amount changes, plus governance-only Params denomination changes.
- Successful and failed same-denom BasePool resizes and live `axdr`-to-basket pool-denom changes, including zero, small,
  and arbitrarily large differences between submitted and applied amounts that do not independently reject execution.
- Stable-to-stable quote equality before and after a pool-denom transition.
- Treasury liability, target, and coverage-funded-output equality before and after both a pool-denom transition and a
  reference-tax-cap denomination change.
- Missing XDR pricing with zero `axdr` supply versus nonzero `axdr` supply.
- `FundStatus` with basket pricing missing when basket is only the Market pool unit versus when basket supply is
  outstanding; separately prove a NOAH/stable Market quote still fails when its required pool-unit rate is stale.
- `RewardFunding` remains queryable without Oracle calls when `FundStatus` cannot produce a complete valuation.
- Bidirectional `axdr` conversions remain valid after the basket transition, including NOAH-to-`axdr` and
  basket-to-`axdr` outputs, and every flow updates the single basket-denominated virtual pool where applicable.
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
  gross_offer_noah = buffer_credit + strategic_reserve_credit + insurance_credit + overflow_burn   (D6, 2026-09-02)
  eligible_principal_noah <= gross_offer_noah
  total_noah_burn = overflow_burn
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
  before settlement: transfer_tax_collector accumulates every committed assessed-tax coin exactly
  at settlement: window_tax = oracle_tax_credit + validator_tax_credit
  aggregate_validator_target = sum(applied validator_block_reward_target for each observed block)
  aggregate_oracle_target = sum(applied oracle_block_reward_target for each observed block)
  aggregate_eligible_gas_value = sum(contemporaneously valued eligible fee_collector balances)
  validator_pre_tax_gap = max(aggregate_validator_target - aggregate_eligible_gas_value, 0)
  protected_oracle_tax_value = min(eligible_window_tax_value, aggregate_oracle_target)
  desired_validator_tax_value = min(eligible_window_tax_value - protected_oracle_tax_value, validator_pre_tax_gap)
  transfer_tax_collector balance = 0 after every successful allocation
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
  Economic Mandate does not change
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
  axdr remains valid as both offer and ask after the basket transition
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
  Claims Mandate committee is distinct from Treasury general authority
  each Claims Mandate replacement or disablement advances the chain-derived term
  Claims Mandate active iff committee is nonempty and activation_height <= h < expiry_height
  committee and governance submit under the same active term, validation, and held-balance rules
  0 <= committee term used <= committee claim limit
  committee claim submission: Delta committee term used = amount
  governance claim submission: Delta committee term used = 0
  claim cancellation or execution: Delta committee term used = 0
  each submitted claim stores its mandate_term and closing_height <= mandate expiry_height
  current active committee with exact current term may cancel only non-governance-origin pending claims before closing_height
  governance may cancel any pending claim before closing_height regardless of the current Claims Mandate
  cancellation is unavailable to both roles at or after closing_height
  automatic settlement runs in the EndBlock of the first height at or after closing_height
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
  in Treasury. Market consumes the returned total burn without revalidating or recomputing Treasury policy, and never
  sees the credit split behind it.
- Keep the coverage-based Buffer draw and its `LegacyDec` conversion path single-sourced.
- Keep Market's three settlement paths explicit rather than hiding materially different supply effects behind generic
  transfer helpers.
- Confirm no unused Treasury-to-protocolpool or Treasury-to-staking dependencies remain.
- Confirm no dead custom Wasm Treasury adapter remains.
- Refresh package documentation to describe current ownership rather than Terra inheritance.

### 17.4 Final verification

Skipped by decision on 2026-09-06 (§21).

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

Moved to `docs/GENESIS.md` on 2026-09-06. That document records every launch setting with its value in
`app/genesis/genesis.json`, its status, and the decision behind it, and carries the fresh-genesis rules, the
export/import requirements, and the pre-launch review checklist that lived here.

## 19. Launch economic configuration

Moved to `docs/GENESIS.md` §12 on 2026-09-06, together with the table of what each target ratio sets and the
subsidy-runway estimate. The P1 and P4 values are chosen there and entered into the artefact.

## 20. Deferred extensions and required post-launch sequence

The operational sequence moved to `docs/POST_LAUNCH.md` on 2026-09-06: opening the hub (the client type, channels, rate
limits, then the transfer and ICA flags), what opens with the client type, the contract runtime's governance surface,
the Ark governance operations (appointments, Reserve-to-Buffer commitments, the tax, the exposure multiplier, the first
external asset), what is deferred on a trigger, and what will not be built. The subsections below are the design record
those operations rest on; where a later decision superseded a passage, its amendment note says so. Section 21 records
Phase 6 as retired and Phases 7 and 8 as complete.

### 20.1 Risk-exposure targets and external-asset recognition

*(Amended 2026-09-06: the custody allowlist, the widened recipient restriction, and the custody adapters below will never
be built. External assets are held at mandate destinations and attested (D59), and the Reserve account admits NOAH and
registry members only (D70). The eligibility-entry and recognition rules stand, implemented as D58, D62, D64, and
D69–D71. The target-exposure model paragraphs are retired with Phase 6; `docs/GENESIS.md` §12 records what the ratios mean instead.)*

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

### 20.2 Committee roles, economic-policy authority, Reserve policy authority, and fast execution

Treasury's general `authority` remains the governance module account because it also controls `MsgUpdateParams` and
other policy decisions. Do not assign it to an emergency key, multisig, Claims Mandate role, or operational executor. Do
not add a global `reserve_authority` or a generic `MsgSendReserve`.

Ark uses role-scoped authority accounts:

```text
root policy authority         = x/gov
economic-policy committee     = threshold multisig A
Insurance claims committee    = threshold multisig B
Reserve committee             = threshold multisig C
```

*(Amended 2026-08-05, D56: the table previously listed a future Reserve executor and a separate Reserve guardian; both
rows collapse into the single Reserve committee, which mirrors the Claims committee row exactly — appointed,
term-checked, and replaceable through the same envelope pattern.)*

Human signers may overlap initially, but every role uses a distinct on-chain address so economic policy, claims, and
future Reserve actions can have different thresholds, rotation, audit history, and compromise boundaries. No role
inherits another merely because its human membership overlaps.

The implemented economic mandate delegates only:

```text
validator_block_reward_target
oracle_block_reward_target
redemption_buffer_target_ratio
strategic_reserve_target_ratio
insurance_target_ratio
```

Governance appoints, replaces, or disables one exact committee address through `MsgSetEconomicMandate`. Each appointment
stores the chain-derived `uint64` term with the increment behavior specified in Section 10.5, half-open
`activation_height <= height < expiry_height`, and complete minimum and maximum policies. Committee transactions name
the expected term and replace the entire reversible policy subset atomically; stale-term, inactive, out-of-bounds, and
partial updates fail. Normal account authentication enforces the configured threshold multisig, after which Treasury
uses exact address equality for role authorization.

Governance retains both override levers. It may sign its own `MsgUpdatePolicy` to apply a structurally valid
candidate outside committee bounds, or replace/disable the mandate immediately. `MsgUpdateParams` remains governance-only for
`transfer_tax_rate`, `reward_funding_window`, and the complete reference-cap Coin. Reference-cap changes rebuild the derived cap map without
changing the committee mandate. Separate queries expose the current Economic Policy and the stored mandate; mandate
activity is derived from the current height.

The Claims committee and governance may submit any unique positive `anoah` claim covered by the live Insurance balance.
Committee submissions additionally consume the fixed gross allowance for the current mandate term; governance
submissions do not. Both may cancel before the same closing height, but the committee cannot cancel a
governance-submitted claim. Governance may replace the committee but cannot cancel after the shared deadline or claw
back a paid claim. The economic-policy committee cannot submit/cancel claims, move fund custody, change governance-owned
Params, or invoke the governance-only Reserve transfer.

The mandate landed in `x/reserve` (D56, 2026-08-05): the module owns the strategic Reserve custody account, the
governance-only commitment message, the committee mandate and its accounting journal, the recognition policy, and the
send restriction over that account, and it reports `recognised_capital` to Treasury through the same
`RecognisedCapital` contract `x/claims` answers. That keeps Treasury at one mandate and keeps §20.4's settlement
evidence out of the module that computes tax caps. See
`docs/DESIGN_NOTES.md` §6 for the as-built record.

**Amended 2026-08-05 (D56–D57): the machinery below is superseded.** The implemented design appoints a single Reserve
committee through `MsgSetReserveMandate` — no executor/guardian split, no pause state, no
`MsgCreateReserveMandate`/`MsgExecuteReserveMandate`/`MsgPauseReserveMandate`/`MsgRevokeReserveMandate`, no
configured-versus-effective status model, and one mandate at a time. The committee deploys to mandate destinations and
keeps the books — quantity updates, return attribution, impairment marking, closure — while governance corrects
records, clears impairment, owns the recognition policy, and replaces or disables the mandate at proposal latency. The
text below is kept as the record of the maximal model that was considered and pared down.

For future fast action, governance may create a Reserve mandate with:

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

**Amended 2026-08-05 (D56–D57):** the implemented `ReserveMandate` reduces this list to the shared envelope (term,
committee, half-open activation window) plus the term deployment allowance, the minimum liquid `anoah` floor, and the
exact destination list. Gross spent, returned capital, and outstanding exposure live as ledger aggregates
(`AllowanceUsed`, `GrossDeployed`, `TotalReturned`, and per-position folds re-derived from the journal at genesis), not
as mandate caps; the allowance is consumed permanently, which enforces the no-automatic-reset rule below by
construction rather than by a governance decision each time. The omitted fields — IDs and status models,
per-transaction and rolling-window caps, exposure limits, price/slippage/deadline constraints, adapter pins — are each
recorded with rationale in `docs/DESIGN_NOTES.md` §6.2 (its §2): they either bound the off-chain leg the chain cannot see or
reintroduce the roles D56 removed. The list below is kept as the checklist for any future widening (Phase C adapters).

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

**Amended 2026-08-05 (D57):** the status machine below is replaced by the journal's entry kinds — DEPLOYMENT,
QUANTITY_UPDATE, RETURN_ATTRIBUTION, IMPAIRMENT, CORRECTION, CLOSURE — with `impaired`/`closed` position flags instead
of a state enum. In-flight and unresolved value still earns zero credit, because recognition prices only quantities the
committee has booked and nothing is credited before it is recorded. "Any manual reconciliation or write-down is
governance-authorised" narrows to: corrections and impairment clearing are governance-authorised, while closure —
including at a loss — is the committee's, because both legs of realised P&L are proven coin movements the committee
cannot misstate (`docs/DESIGN_NOTES.md` §6.2, its §14). Adapters are redescribed as evidence upgrades that append to the same
journal (`docs/DESIGN_NOTES.md` §6.2, Phase C), not the settlement machinery itself — and D59 then closes Phase C as not planned: no major
off-chain asset is reachable through IBC, so the lifecycle below stays a record of what a chain-verifiable venue
would require, not pending work.

A same-chain swap may be atomic only when the approved adapter performs input debit, minimum-output enforcement, and
direct Reserve output credit in one cached state transition. A plain transfer to an external custodian is not an atomic
asset purchase; it creates trusted counterparty exposure and remains zero-credit unless and until the approved custody
and recognition conditions are proven.

IBC and external custody require explicit pending, acknowledged, timed-out, returned, impaired, and written-down states.
An IBC acknowledgement proves packet delivery, not that backing was purchased or remains controlled. In-flight or
unresolved value receives zero target credit. Timeout and return handling must restore or reconcile mandate accounting
without resetting lifetime gross outflow. Any manual reconciliation or write-down is governance-authorised and audited.

### 20.5 Other deferred policy work

*(Amended 2026-08-05, D61: the disposal path for non-NOAH Reserve custody is partly built. Burning credit-zero custody
exists — committee-authorised, since it destroys what the capital system counts at nothing — so derecognized residue
routed here is no longer strictly inert. Selling it does not exist and remains deferred: a disposal that receives value
needs a venue, a counterparty, and a price, none of which this module has.)*

The following also remains outside the launch phases:

- A formal claims adjudication/voting module and any non-NOAH Insurance custody or in-kind payout allowlist.
- Automated, revenue-routed, target-based, mint-funded, or conversion-funded subsidy-pool refill mechanisms beyond
  permissionless direct `anoah` transfers.
- The future basket's composition, weighting, rebalance rules, denomination, Oracle derivation, and governance launch
  schedule. The generic live Market pool-unit transition is implemented in Phase 3; activating it still requires a
  separate operational proposal once the basket policy exists, and it leaves `axdr` fully supported.
- Any future stablecoin retirement or output-disable policy. It is not implied by a flagship or pool-denomination change
  and requires its own authority, lifecycle, holder-exit, and reactivation decisions.
- A hard rolling residual-mint cap if virtual-pool analysis requires one, with explicit review of the redemption queue
  and first-mover incentive it would create.
- Replacing or redirecting Cosmos distribution's community-pool residual accounting.

Each extension requires its own policy decision, file-level implementation plan, protobuf approval where applicable,
tests, and review gate. None should be inferred while implementing the launch phases in this document.

## 21. Phase status

| Phase | Scope | Status | Evidence and amendments |
| ----- | ----- | ------ | ----------------------- |
| 0     | Policy, assumptions, baseline                              | Complete                                | D1-D33 confirmed at the gate; D34-D73 added through 2026-08-11 under the same register rule. Baseline passed; implementation approved. P1 and P4 launch values stay open until launch. |
| 1     | Treasury proto, core, accounts                             | Reviewed, since amended                 | Treasury and required support accepted 2026-07-18. Amended by the asset-registry reshape (2026-08-02), the D54/D55 extractions to `x/claims` and `x/reserve` (2026-08-05), the D66/D67 self-held partition and two-basis rule (2026-08-08), and the D72 exposure multiplier (2026-08-10). Each amendment carries its own design record; the 2026-07-18 gate is not reopened. |
| 2     | Remove Mint and activate subsidy-pool funding               | Reviewed                                | Verification passed; user approved closure 2026-07-20. `x/mint` is still absent and the subsidy pool is unchanged. |
| 3     | Market settlement, labelled pool unit, and live transition | Reviewed, settlement cadence amended    | User and independent review completed 2026-07-20. D33 amended 2026-08-08 to settle every conversion once per block from Market's EndBlocker, collapsing Treasury's three settlement calls into `SettleConversions` and deleting the D39 snapshot and D68 invalidators; implemented per `docs/DESIGN_NOTES.md` §3.3. Market's conversion policy separately moved under a committee mandate (2026-08-01). |
| 4     | IBC/Wasm foundations, transfer-tax integration, and multi-denom Oracle rewards | Reviewed 2026-09-06 | Sections 16.1-16.3 approved; IBC keepers, routes, lifecycle, CLI, and testing accessors implemented 2026-07-21. The D49 gate ran on 2026-08-11 against both SDK lines: it fails on v0.55.0, where Wasmd v0.70.3 cannot compile because that release finished removing `x/params`, and passes on v0.54.3, so the chain was held at v0.54.3 and the in-flight v0.55.0 move reverted (§16.3). The Wasm runtime, 08-Wasm client, callbacks, and GMP landed 2026-08-29, the `app/ante` package 2026-09-02, and the query accept list 2026-09-03; the multi-denom Oracle rewards had been in the tree since 2026-07-21. All of it reviewed and accepted 2026-09-06. Both transfer surfaces ship disabled and the contract runtime shut; the §16.6 matrix landed 2026-09-06 (§16.6, §21.1), so enabling either surface is a governance parameter change. |
| 5     | Full-system invariants and simplification                  | In progress                             | Opened 2026-09-06 on Phase 4 approval. First deliverable: the launch genesis holds the hub shut behind an empty client allowlist and ships the contract runtime open, and `app/genesis_test.go` pins both (`docs/GENESIS.md`). §17.1 and §17.2 skipped by decision the same day: the keeper suites carry the arithmetic and the app tests the wiring, and no full-app lifecycle driver is planned. The capital-accounting consolidation draft was retired the same day (§21.2), so §17.3 ran ungated: the checklist was clean except two uncalled methods on Treasury's bank interface, dropped, and package documentation, which was absent rather than stale and is now written for all seven modules. §17.4 skipped by decision the same day. The P1 and P4 genesis fill (`docs/GENESIS.md`) is deferred until the in-flight code lands, and is the only pre-launch item left; the §16.6 matrix and the Wasm query accept list, the two pre-launch code items, landed 2026-09-06. |
| 6     | Reserve/Insurance covered-risk-exposure target models      | Retired 2026-09-06                      | Under NOAH-only custody with attested positions, every fund's exposure has liability as its sole base, so a per-fund model reduces to the launch ratios (`docs/GENESIS.md` §12) scaled by D72; there is no second base for it to weigh. D28 stands as a principle and is enforced structurally: the target fold reads no fund balance. §20.1's recognition machinery landed as D58, D62, and D64. Revisit only if a fund carries material risk that does not scale with liability. |
| 7     | First external asset: custody and recognition              | Complete                                | External symbols (D69), prefix-derived pricing (D70), and per-entry staleness tolerance (D71) landed 2026-08-09. Onboarding an asset is three governance messages: `AddFeed`, `SetRecognitionPolicy`, `SetReserveMandate`. Decided 2026-09-06: on-chain custody of an external token will never be built; attestation is the model and the send restriction's refusal of external symbols is permanent. The first asset is a governance decision, not a phase. |
| 8     | Bounded Reserve mandate and first typed deployment adapter | Complete                                | The one-committee Reserve mandate (D56), quantity journal (D57), and split burn authority (D61) were implemented 2026-08-05, collapsing §20.2's executor/guardian split. D59 permanently closes the typed deployment adapter: committee attestation with bounded references is the permanent evidence model, not an interim one. Nothing remains but the appointment. |

### 21.1 Current outside-`x/treasury` disposition

This table records the disposition of code outside `x/treasury`, committed and working-tree alike: which supporting
changes are accepted, and which later-phase changes remain provisional. Retaining partial code does not mark its phase
complete.

| Area | Current state | Disposition |
| ---- | ------------- | ----------- |
| Treasury protobuf/API | `proto/ark/treasury/**` defines the Treasury contract and `api/ark/treasury/**` contains its generated Pulsar/grpc output. Reshaped since the review: the schema moved onto the asset lifecycle (2026-08-02), the claims and Reserve surfaces left with their modules (2026-08-03), `FundStatus` gained the self-held partition (D66), and the exposure instrument arrived (D72). | Accepted as part of the reviewed Treasury implementation; retain. |
| Phase 1 app support | `app/app_config.go` registers `subsidy_pool`, `redemption_buffer`, and `transfer_tax_collector` for Treasury, `strategic_reserve` for `x/reserve` (the one fund account holding `Burner`, never `Minter`), and `claims_insurance` for `x/claims`; it orders each module's send restriction, keeps Treasury ahead of Distribution in BeginBlock, and puts Market first and Claims last in EndBlock for D33 settlement. `app/treasury_test.go`, `app/treasury_multisig_test.go`, and the per-module app tests exercise this integration. | Accepted as required Phase 1 support, as amended by D54/D55 and D33; retain. |
| Oracle quote support | `x/oracle/keeper/conversion.go` always includes the `anoah` identity rate in `GetRateSet`, with corresponding keeper-test changes. | Accepted as the shared quote behavior used by Treasury reward and liability valuation; retain. |
| Phase 3 Market settlement and pool-unit transition | Market injects a Treasury keeper and, since D33's 2026-08-08 amendment, calls one `SettleConversions` from its EndBlocker over the block's accumulated `ConversionTotals`; the three per-conversion calls it replaced (`RouteExpansion`, `DrawRedemptionBuffer`, `RecordSupplyChange`) are gone. A successful Treasury result is still authoritative and still the value Market finishes settlement with. `BasePool` is a denomination-bearing `sdk.DecCoin`, delta queries return its unit, NOAH/stable math uses that unit, and stable-to-stable pricing is independent of virtual-pool state. `MsgUpdateParams` applies one fresh deterministic Oracle-derived amount on denomination changes, retains the submitted expectation in the transaction, emits old and applied pool state, atomically rescales delta on every amount change, and ordinary export preserves both values. | Reviewed and accepted. Treasury errors and actual rate, denomination, arithmetic, or effective-pool failures abort atomically. Do not add a submitted-versus-applied rejection threshold, duplicate audit fields, or a second transition path. |
| Phase 4 IBC foundation | IBC-Go v11.2 keepers, stores, Classic/v2 ICS-20 routes, Classic PFM, Classic/v2 rate limiting, 07-Tendermint, ICA controller/host, module accounts, lifecycle, redundant-relay ante, CLI/genesis basics, and IBC testing accessors are wired through the SDK runtime's manual registration hooks. | Implemented under the approved Section 16.2 scope on SDK v0.54.3; reviewed and accepted 2026-09-06. The Wasm slice below added the callbacks and GMP. `app/ibc_packet_test.go` drives packets through the routed stack: delivery, rate-limit refusal ahead of transfer, timeout and failed-acknowledgement refunds, and escrow. The §16.6 gates landed 2026-09-06 on an `ibctesting` coordinator of Ark chains (`app/testutil/ibc.go`, `app/ibc_relay_test.go`, `app/gmp_relay_test.go`); opening IBC is a governance sequence: admit `07-tendermint`, set rate limits per route, then the transfer flags. |
| Phase 4 ante | `app/ante` assembles the ante chain — the SDK's v0.54.3 decorators with Ark's fee checker, the Wasm and redundant-relay decorators, and Ark's message policies: transfer tax over Bank, Market, IBC, and Wasm attached-funds messages through one shared authz walker, the MultiSend fan-out guard, the gov-vote stake floor, and the lane privilege vouch — and the post handler that charges the tax once the messages have executed. `app/treasury_ante.go` is gone; `app/app.go` installs the package's builder. | Reviewed and accepted 2026-09-06 as the §16.4 layout decision. |
| Phase 4 Oracle rewards | Oracle's Bank interface uses `GetAllBalances`, and `SettleRewards` in `x/oracle/keeper/accounting.go` distributes every positive denomination of the Oracle balance. | Reviewed and accepted 2026-09-06 together with tax activation. |
| `x/asset` | The authoritative registry and lifecycle owner for every governance-managed Bank asset other than NOAH, on a five-status lifecycle, with an emergency-suspension committee mandate. Treasury derives its tax base, liability partition, and cap membership from it; Market reads it for conversion eligibility; it registers as an Oracle feed-removal guard. | Implemented, Phases 0-6 of `docs/ASSET_MODULE_PLAN.md`; accepted and load-bearing for Treasury. Not a Treasury-plan phase — the registry is the lifecycle authority this plan's liability text now assumes. |
| `x/claims` | Owns the Claims mandate, the claim record, the Insurance reservation, and `claims_insurance` custody; answers Treasury through one-way `RecognisedCapital`. | Implemented 2026-08-05 under D54. Accepted; the §6.6 and §10 claims text describes this module, not Treasury. |
| `x/reserve` | Owns `strategic_reserve` custody and its send restriction, the one-committee mandate (D56), the append-only quantity journal (D57), recognition policy with self-referential caps (D58, D64), split burn authority (D61), external symbols and prefix-derived pricing (D69, D70), and per-entry staleness tolerance (D71). Registers as an Oracle feed-removal guard (D63). | Implemented 2026-08-05 through 2026-08-10 under D55-D65 and D69-D71; see `docs/DESIGN_NOTES.md` §6. Accepted. This delivers most of the Phase 8 mandate ahead of schedule; §20.2-20.4 survive only where those decisions did not supersede them. |
| `x/security` | A security committee with a bounded fast path over the standard-module emergency surface — upgrade schedule/cancel, IBC client recovery, and door-closing halts — dispatching self-constructed upstream messages through `baseapp.MessageRouter` with the effective authority injected. Owns no domain state, no module account, no params. | Implemented 2026-08-11 per `docs/DESIGN_NOTES.md` §7. Accepted. Outside this plan's scope but shares `pkg/mandate.Envelope` with Treasury's economic and claims mandates, so envelope changes are cross-cutting. |
| Phase 4 Wasm slice | Wasmd v0.70.3 on `wasmvm/v3`: keeper, store, module, module account, node config, snapshot extension, native contract routes on both IBC stacks, and CLI/genesis basics (`app/wasm.go`). Contracts dispatch through a Treasury-aware message router that charges execution-generated tax from the sending contract (`app/wasm_tax_router.go`, D41/D42), read through a hand-written accept list of fourteen paths (`app/wasm_query.go`, D43/D74), and reach ICS-20 transfer-and-call through callbacks on the Classic and v2 stacks (D47). `x/wasm/exported` and `x/market/wasm` are deleted per D74. Treasury's calculator gained the `MsgTransfer`, execute-funds, and instantiate-funds cases D41 assigns to the dispatcher. ICS-27 v2 GMP is wired on the same router, so an account the chain derives for a remote caller pays execution-generated tax on a contract's terms (`app/gmp.go`, D48). | The complete Section 16.3 file-level slice, implemented on SDK v0.54.3 after the D49 gate. Open at launch (decided 2026-09-06): anyone may upload and instantiate; the query accept list carries the fourteen paths §16.3 names, and the empty IBC client allowlist leaves contract channels unopenable. The Wasm ante decorators are installed, which required spelling out the ante chain: they must sit immediately after context setup, and `ante.NewAnteHandler` admits no insertion point. Everything below them is the SDK's v0.54.3 default list in its original order, and an SDK upgrade adding a decorator must be mirrored by hand. `withIBCAnte` is gone, its redundant-relay decorator now last in the chain where it already effectively sat. Reviewed and accepted 2026-09-06, and the Section 16.6 matrix landed the same day: `app/wasm_contract_test.go` on Wasmd's reflect contract, `app/ibc_callbacks_test.go` on the CosmWasm ibc-callbacks contract (v2.2.2, checksum-verified, embedded from `app/testdata`), and `app/wasm_query_test.go` on the accept list, which now carries fourteen paths. The runtime is live from height one; nothing remains before IBC activation but governance's own decision. |
| Priority mempool lanes | `abci/lanes` configures the SDK `PriorityNonceMempool` on a two-field `{Lane, Fee}` key so committee and governance transactions drain ahead of other traffic; wired in `app/app.go` behind the app.toml mempool cap. Paired with `docs/EMERGENCY_SUBMISSION_RUNBOOK.md`. | Working tree, uncommitted. Executed per `docs/DESIGN_NOTES.md` §8 (Layers 1-2; Layer 3 deliberately deferred). No change to the oracle proposal/VE/preblock pipeline and no consensus-enforced ordering. |
| SDK v0.55.0 move | Built, then reverted on 2026-08-11 when the D49 gate failed against v0.55.0 (§16.3). The chain stays on cosmos-sdk v0.54.3, CometBFT v0.39.3, and Go 1.25.9. Reverting with it: the staking key-rotation fee pool, the `auth.NewAppModule` Subspace drop, the `ExportGenesisFileWithTime` consensus-param form, the ML-DSA-65 vote-extension sizing, and commit `db1fde3`'s Oracle rotation fallback — all of them v0.55/CometBFT v0.40 surfaces with no v0.54.3 equivalent, and the rotation fallback moot there because the SDK has no key rotation before v0.55. `x/protocolpool` stays unregistered by decision, not necessity: v0.54.3 still ships it, and Ark declines it. | Reverted in the working tree; `go build ./...`, `go vet`, and `go test ./...` all pass on v0.54.3. Redo the move once Wasmd compiles against v0.55.0; the reverted work is a patch, not a redesign. Block-STM, which v0.55 promotes from per-chain programmatic wiring to `app.toml` configuration, is scoped separately in `docs/BLOCK_EXECUTION.md`. That document also records the 2026-08-29 prelaunch removal of `baseapp.EnableBlockGasMeter`: the parallel executor cannot coexist with the meter, and dropping it is consensus-visible after genesis but free before it, so it was taken now rather than deferred to the move. |

Build output, test binaries, and fuzz crashers are gitignored as of 2026-08-11; they are local artifacts, not plan or
phase work, and must remain untouched.

### 21.2 Open design records

Recorded designs that are drafted or deferred rather than implemented, and therefore not reflected anywhere above:

- `2026-08-08-capital-accounting-consolidation-design.md` — retired 2026-09-06, nothing implemented. Block settlement
  removed the snapshot its invariant suite and custody chokepoints were built around, the basis rule became
  structural under D66/D67, the doctrine is written in `docs/DESIGN_NOTES.md` §4.1 and §6.3, and no duplicated
  arithmetic exists for a shared package to remove. Disposition in `docs/DESIGN_NOTES.md` §9; a shared arithmetic
  package is revisited only if Phase 6 adds a second consumer.
- `2026-08-10-exposure-observability.md` — draft, pending review. Off-chain observability for D72; the chain-side
  prerequisites already ship (`Query/ExposureStatus`, typed events), and the document deliberately adds no metrics.
- `2026-08-11-committee-realisation-design.md` — Stage 0 is the shipped default; Stages 1-3 are deferred, each gated
  on a stated trigger.
