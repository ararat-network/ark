# Economic decisions

This is Ark's economic decision history. [Economic design](ECONOMIC_DESIGN.md) describes the current contract;
[launch genesis](../governance/GENESIS.md) owns the unresolved launch values and appointments. D/P identifiers are stable citation
keys, not implementation stages or a claim that every recorded mechanism still exists.

The recorded text and original statuses are preserved below, including amendments. Historical section numbers and
API names inside that text refer to the design at the time; consult current design and module READMEs before using
those passages as implementation guidance. Lifecycle notes identify replaced decisions without erasing their rationale.
When amending a decision, retain its prior text and link its replacement rather than silently changing its meaning.

D80 was assigned to two entries in the source register. Both are retained under [D80](#d80), with separate subheadings;
neither is renumbered. Its original "Proposed" label records history, not the current fee contract's implementation status.
The fee contract is described in [client fee construction](../clients/CLIENT_FEES.md).

## Find a decision

- D1–D19: [D1](#d1), [D2](#d2), [D3](#d3), [D4](#d4), [D5](#d5), [D6](#d6), [D7](#d7), [D8](#d8), [D9](#d9), [D10](#d10), [D11](#d11), [D12](#d12), [D13](#d13), [D14](#d14), [D15](#d15), [D16](#d16), [D17](#d17), [D18](#d18), [D19](#d19).
- D20–D38: [D20](#d20), [D21](#d21), [D22](#d22), [D23](#d23), [D24](#d24), [D25](#d25), [D26](#d26), [D27](#d27), [D28](#d28), [D29](#d29), [D30](#d30), [D31](#d31), [D32](#d32), [D33](#d33), [D34](#d34), [D35](#d35), [D36](#d36), [D37](#d37), [D38](#d38).
- D39–D53: [D39](#d39), [D40](#d40), [D41](#d41), [D42](#d42), [D43](#d43), [D44](#d44), [D45](#d45), [D46](#d46), [D47](#d47), [D48](#d48), [D49](#d49), [D50](#d50), [D51](#d51), [D52](#d52), [D53](#d53).
- D54–D71: [D54](#d54), [D55](#d55), [D56](#d56), [D57](#d57), [D58](#d58), [D59](#d59), [D60](#d60), [D61](#d61), [D62](#d62), [D63](#d63), [D64](#d64), [D65](#d65), [D66](#d66), [D67](#d67), [D68](#d68), [D69](#d69), [D70](#d70), [D71](#d71).
- D72–D111: [D72](#d72), [D73](#d73), [D74](#d74), [D75](#d75), [D76](#d76), [D77](#d77), [D78](#d78), [D79](#d79), [D80](#d80), [D81](#d81), [D82](#d82), [D83](#d83), [D84](#d84), [D85](#d85), [D86](#d86), [D87](#d87), [D88](#d88), [D89](#d89), [D90](#d90), [D91](#d91), [D92](#d92), [D93](#d93), [D94](#d94), [D95](#d95), [D96](#d96), [D97](#d97), [D98](#d98), [D99](#d99), [D100](#d100), [D101](#d101), [D102](#d102), [D103](#d103), [D104](#d104), [D105](#d105), [D106](#d106), [D107](#d107), [D108](#d108), [D109](#d109), [D110](#d110), [D111](#d111).
- D112–D138: [D112](#d112), [D113](#d113), [D114](#d114), [D115](#d115), [D116](#d116), [D117](#d117), [D118](#d118), [D119](#d119), [D120](#d120), [D121](#d121), [D122](#d122), [D123](#d123), [D124](#d124), [D125](#d125), [D126](#d126), [D127](#d127), [D128](#d128), [D129](#d129), [D130](#d130), [D131](#d131), [D132](#d132), [D133](#d133), [D134](#d134), [D135](#d135), [D136](#d136), [D137](#d137), [D138](#d138).
- Launch and analysis: [P1](#p1), [P2](#p2), [P3](#p3), [P4](#p4).

## D1

**Recorded status:** Confirmed.

Remove `x/mint`; Ark has no scheduled staking-reward inflation.

## D2

**Recorded status:** Confirmed.

Market remains the sole minter; its mint/burn authority is used only for conversion settlement.

## D3

**Recorded status:** Confirmed.

Keep `BasePool`, `ArkPoolDelta`, and `PoolRecoveryPeriod` as virtual imbalance controls.

## D4

**Recorded status:** Confirmed.

Fund the quoted NOAH output by the Buffer's actual pre-trade coverage share of the claimable liability, capped at
100%; mint the residual. The claimable aggregate excludes supply that cannot currently redeem — a member without a
fresh feed, suspended supply without an activated plan — because unredeemable and unvaluable coincide by
construction, so the draw never switches off; `valuation_complete` records partial information for audit only. This
supersedes the original zero-draw fallback, which retired the Buffer chain-wide during exactly the stress it exists
to dampen.

## D5

**Recorded status:** Confirmed.

Route eligible expansion principal to Buffer, strategic Reserve, Insurance, then burn the excess.

## D6

**Recorded status:** Confirmed.

Burn the NOAH value attributable to spread and integer dust; never route it to any fund. Amended 2026-09-02: spread
and dust enter the expansion waterfall as principal does — fund targets first, overflow burned — and route to
neither the subsidy pool nor the fee collector. The unconditional burn was inherited from Terra's pre-Columbus-5
fee burn rather than reasoned. The principle now is two circuits. Gas and transfer tax fund security, so validator
income never depends on the money-supply cycle; conversions insure themselves, so spread — a premium against
stale-oracle and adverse-selection risk — capitalises the funds that bear that risk, whose targets already scale
with the volatility and flow indicators that widen it. With backing full this is the original burn; with backing
short it is capital arriving as the requirement rises. The subsidy pool remains a bootstrap with no protocol
inflow, so its countdown stays honest, and nothing is paid from the funds beyond target, so no constituency gains
from a wider spread. Redemption spread is NOAH never issued and stays unissued.

## D7

**Recorded status:** Confirmed.

Targets are passive routing thresholds and never trigger minting, trading, or automatic withdrawals.

## D8

**Recorded status:** Confirmed.

Initially value total native stablecoin supply as exposure and count only NOAH separately in each fund.

## D9

**Recorded status:** Confirmed.

Use a dedicated, balance-constrained, non-minting subsidy pool initially seeded at genesis.

## D10

**Recorded status:** Confirmed.

Give validator and Oracle reward funding separate per-block NOAH-value targets, but aggregate the targets and
eligible validator fees over one parameter-initialised Treasury settlement countdown before calculating shortfalls.

## D11

**Recorded status:** Confirmed.

Remove `oracle_tax_share`; accumulate transfer tax for the same Treasury window, protect the aggregate Oracle
reward target first, use tax above that floor for any aggregate validator target gap, and return every residual to
Oracle.

## D12

**Recorded status:** Confirmed.

If the subsidy pool cannot cover both reward shortfalls, scale the shortfalls proportionally; rounding favours
validators.

## D13

**Recorded status:** Confirmed.

Zero Cosmos distribution `community_tax` in Ark's default genesis by overriding the distribution module basic.

## D14

**Recorded status:** Confirmed.

Keep protocol/community-pool residual accounting as an SDK concern; do not deliberately fund it from Treasury.

## D15

**Lifecycle:** Ownership amended by [D54](#d54): Claims owns Insurance custody and claim settlement.

**Recorded status:** Confirmed.

Insurance is a Treasury-owned module account paid only through unique, recorded claim IDs under the bounded,
governance-owned Claims Mandate.

## D16

**Lifecycle:** Deployment deferral amended by [D56](#d56) and [D57](#d57); ordinary redemptions still cannot debit Reserve.

**Recorded status:** Confirmed.

Ordinary redemption never debits strategic Reserve, and Reserve never directly pays redeemers, rewards, or claims;
all arbitrary-recipient, conversion, investment, and external-asset deployment remains deferred.

## D17

**Recorded status:** Confirmed. A `MsgMultiSend` is capped per output under [D138](#d138).

Apply each denomination's tax cap independently to each message input.

## D18

**Recorded status:** Confirmed.

Tax every enabled user-facing stable transfer surface through one calculator, not internal bank movements.

## D19

**Recorded status:** Confirmed.

Report Redemption Buffer, strategic Reserve, and Insurance separately; never present a combined backing ratio.

## D20

**Recorded status:** Confirmed.

Launch from a clean genesis; discard legacy state/wire compatibility and implement no migration path.

## D21

**Lifecycle:** The independent reference-unit design was replaced by the shared Oracle-owned reference. See the [current unit contract](ECONOMIC_DESIGN.md#39-market-pool-denomination) and [D76](#d76).

**Recorded status:** Confirmed.

Governance owns Treasury's complete denomination-bearing `reference_tax_cap` Coin in Params; launch it in `axdr`
independently of Market's pool unit, and require no Treasury Params update when Market changes its pool
denomination.

## D22

**Lifecycle:** The Market-params re-denomination entry point was replaced by Oracle reference changes. The denomination-bearing pool remains; see the [current transition contract](ECONOMIC_DESIGN.md#310-live-pool-unit-transition).

**Recorded status:** Confirmed.

Make Market `BasePool` a denomination-bearing `sdk.DecCoin`; launch it in `axdr` and change its amount or
denomination live only through Market's `MsgUpdateParams`. Oracle support is a prerequisite for a new denomination;
Treasury never drives or intermediates the Market transition.

## D23

**Lifecycle:** The rescaling invariant remains; the historical submitted-amount interface was replaced by the [Oracle reference transition](ECONOMIC_DESIGN.md#310-live-pool-unit-transition).

**Recorded status:** Confirmed.

Every applied `BasePool` amount or denomination change atomically rescales `ArkPoolDelta` to preserve `delta /
BasePool.Amount`. On a denomination change, treat submitted `BasePool.Amount` as a non-binding audit expectation
and apply the amount derived from one fresh deterministic Oracle conversion.

## D24

**Recorded status:** Confirmed.

Value Treasury liabilities and targets directly in NOAH equivalents; stable-to-stable pricing does not use Market's
pool denom.

## D25

**Recorded status:** Confirmed.

Permit irreversible, permissionless `anoah` transfers into the subsidy pool; reject other denoms and add no
automatic refill mechanism.

## D26

**Lifecycle:** The Reserve deposit restriction and collector exception were superseded by [D65](#d65) and [D70](#d70). Other funds remain NOAH-only.

**Recorded status:** Confirmed.

At launch, permit only positive `anoah` deposits to all four Treasury fund accounts; reject every mixed or non-NOAH
transfer atomically. The single exception is Treasury's own settlement routing from `transfer_tax_collector` into
`strategic_reserve`, which carries derecognized (written-off or retired) transfer tax into inert Reserve custody
pending a separate governed disposal decision.

## D27

**Lifecycle:** Governance commitments remain; bounded committee commitments were added by [D56](#d56) and [D61](#d61).

**Recorded status:** Confirmed.

At launch, governance alone may irreversibly transfer a discrete `anoah` amount from strategic Reserve to the
shared Redemption Buffer, subject to an execution-time minimum remaining Reserve balance; no target, price, Oracle,
or Market trigger applies.

## D28

**Recorded status:** Confirmed.

Required Reserve and Insurance capital is based on each fund's covered risk exposure, never the gross value of
assets held; future external assets may reduce a gap only through explicit fund-specific, risk-adjusted recognition
plus a separate liquid-capital requirement, and Ark-issued stablecoins always receive zero credit.

## D29

**Lifecycle:** Superseded by [D56](#d56): one committee, without a separate guardian or pause state.

**Recorded status:** Confirmed.

Governance retains Reserve policy authority; any future fast execution uses a governance-created, typed, bounded,
expiring mandate executed by a threshold multisig with a separate pause-only guardian, never a generic Reserve
sender or Treasury parameter authority.

## D30

**Recorded status:** Confirmed. Its window and the cancellation period are block time under [D133](#d133).

Governance owns the Insurance Claims Mandate. The shared cancellation period is a module parameter applying to both
origins (Treasury's when written; `x/claims` Params since D54); committee submissions are bound to the active
mandate window and exact term and consume a fixed gross term allowance, while governance submissions depend only on
params and record no mandate term; governance may cancel any pending claim regardless of the current mandate, while
the current committee may cancel only non-governance-submitted claims.

## D31

**Recorded status:** Confirmed.

Economic policy, Insurance claims, and future Reserve operations use distinct role addresses and typed authority
domains even if human memberships overlap; no role receives Treasury's general authority.

## D32

**Recorded status:** Confirmed.

Claims use submit-then-pay with encumbered pending amounts; cancellation ends for every actor at the closing
height, and no authority can claw back a paid claim or bypass held-balance, denomination, uniqueness, no-mint,
no-borrow, or no-cross-fund invariants.

## D33

**Recorded status:** Confirmed.

Market escrows each gross expansion offer and owns conversion burn/mint/payout; Treasury derives and executes the
complete per-conversion waterfall, returns an error if it cannot complete it, and otherwise returns the
authoritative allocation Market uses to finish settlement. Amended 2026-08-08: the exchange happens once per block
from Market's EndBlocker, not inside each conversion. Market escrows as before and still burns each conversion's
spread in the conversion that charged it — the spread owes nothing to liability or fund state, so nothing about it
defers — records the block's conversion facts in transient accumulators, mints each redemption's complete quoted
output, and executes the burn settlement returns. Treasury values liability once against final state and derives
and executes the complete waterfall and coverage draw over the block's totals. Every clause above survives at block
granularity: Market owns custody, mint, and burn, Treasury owns valuation and allocation, and the return value is
still how Market finishes settlement. What moved with the cadence is the failure class — a settlement error now
fails the block rather than one transaction — and the deletion of every cached-valuation mechanism this decision's
per-conversion timing required (D39, D68). Amended 2026-09-02: with D6 amended, Market no longer burns spread in
the conversion. It escrows the gross offer whole, accumulates gross rather than eligible principal, and Treasury's
settlement takes the block's gross total through the waterfall; the burn Market executes at settlement is the
overflow alone. Spread and dust thereby share principal's fallback — an incomplete valuation parks the whole gross
total in the Reserve — which is the all-or-nothing rule already settled. Amended 2026-10-02: governance's EndBlocker
runs before Market's, so a proposal's conversions are recorded in the block's transient totals and settle with it, and
settlement reads the block's final state with proposal effects included. Why: a swap executed after settlement would
strand its offer, which barred governance from converting at all, and carrying facts into the next block would have
broken block-level settlement. What it costs: proposals observe pre-settlement balances within their block, so the
subsidy-return minimum and a Reserve commitment's shortfall read one block conservative.

## D34

**Recorded status:** Confirmed.

Govern `reward_funding_window`, default it to one chain week, initialise `blocks_remaining` from it when an empty
Treasury window records its first observation, and apply later parameter changes only after the active countdown
settles.

## D35

**Recorded status:** Confirmed.

Keep `FundStatus` limited to fund stocks, liabilities, and targets; expose active reward-funding accounting through
an independent direct-state query that remains available when fund valuation is unavailable.

## D36

**Recorded status:** Confirmed.

Governance may appoint one exact threshold-multisig economic-policy committee under a bounded, height-scoped,
chain-termed mandate. The committee controls only the eight reversible policy fields (six at first; widened to nine
on 2026-08-10 by the D72 amendment, which moved the three exposure weights here; narrowed to eight by D80, which
moved `transfer_tax_rate` to `Params`); governance may override policy and replace or disable the mandate at any
time.

## D37

**Recorded status:** Confirmed. The Claims period is `claim_cancellation_period`, seconds of block time, under
[D133](#d133).

Governance owns the complete denomination-bearing `reference_tax_cap` Coin together with `reward_funding_window` in
`Params` (`claim_cancellation_period_blocks`, once named here too, moved to `x/claims` Params by D54;
`tax_cap_refresh_period_blocks` is no longer a field — caps derive per block from the conversion-factor table,
§8.4). Persist the reversible economic levers once in `EconomicPolicy`; the boundary between the two messages is
stance against machinery, not a field count — a lever stating how much of something the protocol wants, reversible
and safe to clamp between a mandate's minimum and maximum, belongs to the committee, while the instrument that
computes it, the cadence it runs on, and the rails bounding how far and how fast it may move stay with governance
(amended 2026-08-10 alongside D72); Claims Mandate remains claims-only. Amended 2026-09-03 (D80):
`transfer_tax_rate` is `Params`, not a committee lever — a stance quantity, but one that lives in the signed fee.

## D38

**Lifecycle:** Claims parameter and custody ownership moved from Treasury to Claims under [D54](#d54); the bounded appointment remains.

**Recorded status:** Confirmed. The window is block time under [D133](#d133).

Keep launch Claims minimal: the mandate stores its monotonic term, committee, half-open activation/expiry window,
and fixed gross committee claim limit; the shared cancellation period lives in Treasury `Params` (moved to
`x/claims` Params by D54); claims have no category or per-claim cap, and there is no guardian or
governance-cancellation flag.

## D39

**Lifecycle:** Superseded by amended [D33](#d33): block settlement removed the snapshot and its priming.

**Recorded status:** Confirmed.

Prime the claimable-liability snapshot in the preblocker each block (the first settlement scans lazily only when no
snapshot exists), cache the claimable aggregate together with a completeness flag, and advance it from every Market
burn/mint. The snapshot always carries a value: incompleteness is disclosure about excluded unclaimable supply,
never an uncached state. Superseded 2026-08-08 by D33's block-granular settlement: with the valuation reduced to
one consumer at one point after every write, there is nothing to cache. The snapshot, its codec and gas constant,
the priming preblocker, and the supply-delta maintenance are deleted, and settlement scans canonical Bank and
Oracle state once per block that converts — an idle block now values nothing, where priming scanned the registry
every block. Only the completeness flag survives, unchanged in meaning and emitted at most once per block.

## D40

**Lifecycle:** The IBC half lapsed with [D118](#d118); the execution-tax semantics and the Wasm path stand.

**Recorded status:** Confirmed.

Begin Phase 4 by fixing execution-time tax payer, exactly-once identity, rollback, and fee-sponsorship semantics;
then implement IBC foundations before Wasm because contracts may dispatch IBC messages. Design the Treasury
execution hook into both paths, but keep both transfer surfaces production-disabled until the complete tax and
recipient-restriction activation gate passes.

## D41

**Lifecycle:** IBC sends left the dispatcher's inputs with [D118](#d118); the ownership split stands.

**Recorded status:** Confirmed.

Use call-path ownership for exactly-once tax assessment: ante owns signed top-level inputs, while the Wasm
dispatcher owns only execution-generated Bank sends, IBC sends, execute funds, and instantiate funds. Add no
persistent transfer IDs, context markers, global Bank tax hook, or implicit execution-time feegrant. The sending
contract pays the tax in addition to the complete requested principal.

## D42

**Lifecycle:** The IBC packet clause lapsed with [D118](#d118); single-cache collection stands.

**Recorded status:** Confirmed.

Send execution-generated tax directly to `transfer_tax_collector` and execute its collection with the matching
transfer in one Wasm submessage cache. Synchronous or caught failures roll both back; a successfully created IBC
packet retains its tax through later acknowledgement, timeout, refund, or return bookkeeping, none of which is a
new taxable transfer.

## D43

**Recorded status:** Confirmed.

Give contracts a read-only tax estimate through Treasury's own `Query/ComputeTax`, admitted to the Wasm query
accept list at the §11 gate rather than served by a custom querier (D74). A contract prices the proto form of the
message it will dispatch, so the calculator and its rate/cap math are never duplicated; for the JSON-native
`CosmosMsg` variants the contract builds that proto itself, and keeping it identical to what it returns is the
author's responsibility. The result is an advisory current-state estimate only: it reserves no funds, grants no
authority, and never replaces execution-time recomputation.

## D44

**Recorded status:** Confirmed.

Treat `Params.transfer_tax_rate` as the sole tax activation switch (moved from `EconomicPolicy` by D80,
2026-09-03). An explicit zero reference or derived tax cap means uncapped taxation, while a missing
configured-denomination cap remains an error. Keep the complete derived cap map populated independently of the
rate, and never rebuild it from either policy-update message; reject any positive reference-cap conversion that
truncates to the zero sentinel.

## D45

**Lifecycle:** Superseded by [D118](#d118): IBC removed.

**Recorded status:** Confirmed.

Build Ark's hub foundation against `github.com/cosmos/ibc-go/v11`, targeting v11.2.0 subject to
dependency-resolution and compile verification. Wire IBC Classic and IBC v2 core/ICS-20 routes, the 07-Tendermint
light client, and the transfer module account with minter/burner permissions. Keep standard module genesis defaults
in application code; Ark's canonical launch genesis must allow only `07-tendermint` and launch transfer with send
and receive disabled. Amended 2026-09-06: the launch allowed-client list is empty, so no client, connection,
channel, v2 counterparty, or packet of any kind can exist until governance admits `07-tendermint` by
`MsgUpdateClientParams`; the transfer and ICA flags stay off as well, so that vote opens client creation and
nothing more.

## D46

**Lifecycle:** Superseded by [D118](#d118): IBC removed.

**Recorded status:** Confirmed.

Put governance-controlled rate limiting and packet forwarding in the IBC Classic transfer stack, and apply the v11
rate limiter to the IBC v2 transfer path. Configure reviewed per-denomination, per-channel/client limits before
enabling production transfer. A packet-forwarded hop is protocol-generated continuation of the original transfer,
not a new user-facing taxable input; acknowledgement, timeout, refund, and return bookkeeping likewise receive no
second tax. Amended 2026-09-03: the raw v2 `MsgSendPacket` is the same outbound leg as `MsgTransfer` and prices
identically. The transfer app authenticates the payload sender against the message signer and escrows through the
same `SendTransfer`, so the two are one taxable surface reached by two doors, and the source port is what decides
whether a payload carries principal at all — Wasm v2 ports and GMP move no coins. The calculator decodes with the
transfer module's own decoder and its own arguments, so a payload it escrows is one the tax prices and an
undecodable transfer payload fails the transaction rather than passing untaxed. Acknowledgement, timeout, and
refund remain untaxed on both paths.

## D47

**Lifecycle:** Superseded by [D118](#d118): IBC removed.

**Recorded status:** Confirmed.

Add IBC callbacks to both the Classic and v2 ICS-20 stacks when the Wasm keeper is wired. Callbacks are Ark's
canonical transfer-and-call mechanism; add no separate IBC Hooks middleware. Callback-triggered execution uses the
same Wasm/Treasury execution adapter, while acknowledgement, timeout, and callback delivery alone are not new
taxable transfers.

## D48

**Lifecycle:** Superseded by [D118](#d118): IBC removed.

**Recorded status:** Confirmed.

Add IBC v2 GMP together with the Wasm foundation and route its derived-account SDK-message execution through the
same Treasury-aware router used by contract-generated messages. The derived GMP account pays any
execution-generated tax in addition to principal; outer fee payers and feegrant do not sponsor it. Do not expose a
partially integrated GMP route before its authorization, tax, rollback, and recipient-restriction tests pass.

## D49

**Recorded status:** Confirmed.

Use upstream Wasmd `v0.70.x` and `wasmvm/v3`, subject to clean resolution and compile verification against Ark's
SDK `v0.54.3` and IBC-Go `v11.2.0`; do not maintain a Wasmd fork or replacement-directive compatibility layer.
Remove Ark's unused legacy `wasmvm` v1 parser/query interfaces when the real runtime is installed.

## D50

**Lifecycle:** Superseded by [D118](#d118): IBC removed.

**Recorded status:** Confirmed.

Consider 08-Wasm only with the Wasm foundation and only if its exact dependency and VM family integrates cleanly.
If installed, keep it dormant at launch: the allowed-client list admits no client type at launch (D45 as amended)
and the launch genesis contains no Wasm-client checksums. `09-localhost` remains unavailable through that launch
allowlist; add neither 06-Solo Machine nor the experimental attestations client.

## D51

**Lifecycle:** Superseded by [D118](#d118): IBC removed.

**Recorded status:** Confirmed.

Keep packet forwarding Classic-only until upstream provides reviewed v2 support; do not invent a v2 PFM adapter.
Add no ICS-29 relayer-fee wiring because that application was removed from IBC-Go.

## D52

**Lifecycle:** Superseded by [D118](#d118): IBC removed.

**Recorded status:** Confirmed.

Retain standard user ICA controller and host support but launch both disabled and the host with an empty message
allowlist. Defer a generic custom ICA authentication module; if contracts later need ICA control, prefer a narrowly
scoped Wasm-to-ICA adapter with explicit authorization.

## D53

**Lifecycle:** The IBC clauses were superseded by [D118](#d118); no NFT module remains the decision.

**Recorded status:** Confirmed.

Add neither ICS-721 NFT transfer nor a separate NFT module. Native Wasm IBC channels cover custom contract
protocols, callbacks cover ICS-20 transfer-and-call, and GMP covers arbitrary remote SDK-message execution; revisit
IBC Hooks only for a concrete requirement for Osmosis-compatible `wasm` memo or intermediary-address semantics.

## D54

**Recorded status:** Confirmed.

Extract Insurance claims from `x/treasury` into `x/claims`, which owns the Claims mandate, the claim record, the
Insurance reservation, and the `claims_insurance` custody account. Treasury keeps liability valuation, all three
fund target ratios, the expansion waterfall, tax, and reward funding, and reads Insurance through a one-way
`ClaimsKeeper.RecognisedCapital` interface. The boundary is §7.3's own accounting contract: Treasury owns
`required_capital`, the operating module owns `recognised_capital`, and per §12 held assets satisfy a requirement
without defining it. Done pre-launch because the move carries five populated collections and is a real store
migration afterwards. The future Reserve mandate (§12) lands as `x/reserve` on this same pattern, implementing the
same interface, which also returns Treasury to one mandate per module — the convention `x/security`, `x/asset`, and
`x/market` already follow.

## D55

**Recorded status:** Confirmed.

Extract the strategic Reserve from `x/treasury` into `x/reserve` on the D54 pattern, pre-launch, ahead of the
mandate it will hold. The module owns the `strategic_reserve` custody account, the governance-only
`MsgTransferReserveToBuffer` (now `MsgFundBuffer`, beside `MsgFundInsurance`), and the send restriction over that
account — including its one exempt pair, `transfer_tax_collector` to Reserve, which admits the derecognized
transfer tax Treasury settlement routes there. It answers the same `RecognisedCapital` contract as `x/claims` —
through Treasury's own `ReserveKeeper` interface, since depinject resolves module inputs by type and needs the two
named apart — so Treasury sizes both committee-operated funds identically and holds one mandate. Migration cost is
nil either way (the module has no collections, and the account rename to `strategic_reserve` is free pre-launch);
the exemption is what argued for doing it now rather than inside the §12 mandate work.

## D56

**Recorded status:** Confirmed. The window is block time under [D133](#d133).

One-committee Reserve mandate. The §12 executor/guardian split collapses into a single Reserve committee appointed
through `MsgSetReserveMandate` on the shared envelope pattern: chain-derived monotone term, half-open activation
window, empty committee disables, replacement resets allowance usage. Mandate contents reduce to the term
deployment allowance (consumed permanently — closing a position never restores it), the minimum liquid `anoah`
floor (absolute and appointment-scoped; the liability-scaled liquid tranche is the Redemption Buffer, per the §7.3
amendment), and an exact destination list. There is no guardian and no pause state — governance replacing or
disabling the mandate at proposal latency is the brake, exactly as for every other mandate on this chain — and no
governance deployment message, because deployment needs an off-chain counterparty relationship only a committee
operationally has. §12's per-transaction/rolling-window/lifetime caps, price/slippage/deadline constraints, adapter
pins, and pause state are each deliberately omitted (accounting spec §2). Implemented 2026-08-05.

## D57

**Recorded status:** Confirmed.

Quantity ledger with read-time feed pricing. The Reserve records what is held, never what it is worth: an
append-only journal (kinds DEPLOYMENT / QUANTITY_UPDATE / RETURN_ATTRIBUTION / IMPAIRMENT / CORRECTION / CLOSURE)
carries proven coin movements and committee-attested quantities with bounded off-chain references, and valuation
happens only at read time against the Oracle's available rates. Corrections restate — the journal keeps the error
and the fix — and genesis re-derives every stored aggregate from the journal and requires equality. Return
valuation is crystallised at attribution time by the keeper, so realised P&L at closure is arithmetic over proven
legs, which is why closure needs no governance gate (narrowing §12's write-down rule). Supersedes the
attested-valuation machinery of §12. Implemented 2026-08-05.

## D58

**Recorded status:** Confirmed.

Recognition policy as combined eligibility and custody allowlist. One governed list (`EligibilityEntry`: asset
denomination, haircut factor in [0, 1], recognition cap ratio in [0, 1]), replaced whole by
`MsgSetRecognitionPolicy` and enforced at two points: the Reserve send restriction admits a denomination only if
listed — §12's "anoah plus custody allowlist" restriction change, arriving for exactly one fund — and
`RecognisedCapital` credits `min(haircut × attested open unimpaired quantities × rate, cap_ratio ×
recognised_capital)` per listed asset on top of the par-counted balance, the self-reference solved per block in
closed form (D64). The multiplication is the chain's one rate orientation: every oracle rate quotes NOAH per one
unit of its asset (D75, flipped 2026-09-02; it was units per NOAH before, when this valuation divided), so valuing
that asset in NOAH multiplies, and the conversion goes through `RateSet.Convert` like every other valuation on the
chain rather than being re-derived here — it was re-derived once, as a multiplication, and published the reciprocal
of every figure until 2026-08-08. Every rate comes from the Oracle, so a dark or stale feed zeroes exactly one
asset's credit and recognition degrades to zero rather than to a frozen number, with no exception: there is no
governance-supplied fallback price, and a slow-cadence feed is served by the per-denomination staleness window of
D60 instead. §12's cross-fund concentration caps land as per-asset Reserve caps because Insurance stays NOAH-only
(claims plan D4). The `RecognisedCapital` query decomposes every row — including attested-but-unlisted holdings —
so each zero credit shows its reason. Implemented 2026-08-05; the cap became a share of recognised capital itself
under D64 on 2026-08-06. Amended 2026-08-09: both factors are strictly positive and the custody-allowlist role is
gone — admission stopped reading the policy under D70's narrowing, so a custody-only entry became indistinguishable
from no entry while still occupying the policy and raising no feed-guard claim; refusing zero factors makes every
stored entry a live claim and the guard total over the policy. Amended 2026-08-10: the on-chain balance leaves the
credit formula and `AssetRecognition.onchain_quantity` is deleted. The term dated from the allowlist role, when a
listed denomination could sit in the Reserve account; under D70 admission is NOAH-or-member and a listed name is an
external symbol, so the two shapes are disjoint and the term was provably zero on every row that could earn. The
field went with it rather than staying as a custody column: its only non-zero class is registry-member paper, which
can never be recognised, is already published by Bank and by Treasury as self-held supply (D66), and — for paper
bought back through a position — appeared in the same row twice, once attested and once held. Bank custody is
therefore not a subject of the decomposition, and the fold no longer reads account balances at all.

## D59

**Recorded status:** Confirmed.

No adapter or evidence-upgrade machinery ([Reserve evidence
model](../../x/reserve/README.md#mandate-custody-and-journal)) is planned. Evidence upgrades can verify only value
that routes through chain-visible machinery — an IBC acknowledgement proves delivery over a channel, a contract
read proves wasm state — and the Reserve's plausible asset universe is custodian-held off-chain instruments reached
by wire transfer, which none of that can ever see. Manual committee attestation with bounded references is
therefore the permanent evidence model, not an interim one; the same-chain case needs no adapter because a listed
asset in the Reserve account is bank custody the recognition fold prices directly. The journal's append-only design
keeps this reversible without migration: machine-authored entries could later land beside committee-authored ones
if a chain-verifiable venue ever became real, but that requires its own spec and the [first-asset governance
gate](../governance/GOVERNANCE_OPERATIONS.md#3-ark-governance-operations). Closes §12's adapter machinery beyond the D57 narrowing.

## D60

**Lifecycle:** The override mechanism was superseded by [D71](#d71): staleness tolerance is consumer policy.

**Recorded status:** Confirmed.

Exchange-rate staleness is per denomination. Oracle `Params` carries `max_exchange_rate_age_overrides`, a sorted
governed list replacing the default window for named denominations; every freshness check resolves its window
through `GetMaxAge(denom)`. How long a rate stays meaningful is a property of the feed rather than of the consumer
reading it — a slow-moving instrument priced against a daily published figure is not stale at an age that would
make an FX rate dangerous to quote against — so the window lives beside the rate and every consumer inherits one
answer per denomination. This is what let D58 delete its governance-supplied fallback price: "valuation is never
stored" and "recognition degrades to zero, never to a stale number" both hold without exception, and the only way
to price a slow feed is still an Oracle feed. Implemented 2026-08-05. Amended 2026-08-09 (D71): the override
machinery is deleted — feed sharing under D70 splits cadence, a feed fact, from tolerance, consumer policy, and the
premise held only while every feed had one consumer class; the default window survives as the conversion-grade read
and the Reserve's tolerance moves to each eligibility entry.

## D61

**Lifecycle:** The burn roles remain; the parked-principal accounting call in the recorded text is retired. Read the [current Reserve contract](../../x/reserve/README.md#mandate-custody-and-journal).

**Recorded status:** Confirmed.

Reserve burn authority is split by what a burn can destroy. Governance (`MsgBurnReserveAssets`) may burn any
Reserve custody including NOAH, under the same per-proposal stale-state floor as the Buffer commitment: a NOAH burn
is the chain's only discretionary supply contraction and no committee on this chain moves supply, mirroring §12's
"the mandate cannot mint". The committee may burn credit-zero, non-NOAH custody (`MsgCommitteeBurnResidue`, now
`MsgCommitteeBurnPaper`) — unlisted, or listed with a zero haircut or cap — because destroying what the capital
system already counts at nothing cannot reduce recognised capital, and that residue is in practice Ark-issued
stablecoin whose destruction reduces consolidated liability. The committee may also burn NOAH surplus
(`MsgCommitteeBurnSurplus`), bounded by the keeper at `min(recognised_capital - required_capital, balance - mandate
floor)`: burning reduces recognised capital one for one, so the power exhausts itself exactly at the target line
and cannot reach through it, leaving the committee timing rather than size. That burn refuses while valuation is
incomplete, since the requirement is then unavailable — the same condition that parks principal in the Reserve
freezes disposal out of it. Burns open no position and append no journal entry; the signed message and Bank's
canonical burn event are the audit trail. `strategic_reserve` gains `Burner` and never `Minter`. Treasury
additionally calls `RecordParkedPrincipal` on the incomplete-valuation branch so the fund books the lifetime
principal no target sized — an accounting call only, since Treasury credits the fund through Bank as it does the
other two and the Reserve interface exchanges numbers rather than transfer capability. Implemented 2026-08-05.

## D62

**Lifecycle:** The keeper-side membership check was superseded by [D69](#d69); the namespace now makes the forbidden overlap unrepresentable.

**Recorded status:** Confirmed.

Ark-issued denominations are enforced credit-ineligible, not merely declared so (D28), and no Reserve eligibility
entry may name an asset-registry member at all. The entry was once legal in custody-only form because an in-kind
return of Ark paper could not otherwise enter the account; D65 removed that necessity by admitting members through
the send restriction on membership alone, so the entry itself is now the error and only its shape needed policing
before. Checked against `AssetKeeper.HasAsset` in `MsgSetRecognitionPolicy` and in Reserve `InitGenesis`, which
runs after `x/asset`. Membership is the test rather than lifecycle status, since a written-off or retired asset is
still Ark-issued. The check sits at the write points only, so the per-block recognition fold takes no registry
read; the accepted residual is that a denomination listed before it is ever registered keeps its credit until the
next whole-for-whole policy replacement re-validates the set. Amended 2026-08-08. Amended again 2026-08-09 (D69):
the check and its residual are deleted together — the external-symbol shape makes a member entry unrepresentable,
so the invariant stops being a keeper concern at all.

## D63

**Recorded status:** Confirmed.

`x/reserve` registers as an Oracle feed-removal guard, joining `x/asset` in the wiring-owned guard set. It reports
two claims, both derived from Reserve state at call time: a credited eligibility entry, because recognised capital
counts what the fund holds valued through that feed, so removing the feed would shrink Reserve capital and change
what a committee may burn with nothing in the proposal disclosing it; and an open position denominated in the
feed's asset, because a return attributed while the feed is dark crystallises zero recovery into the journal
permanently, overstating that position's realised loss forever. The first claim is about a recoverable number and
exists to force sequencing — delist, then remove; the second is about an irreversible record. A closed position
raises no claim. Implemented 2026-08-05. Amended 2026-08-09 (D70): both claims map through the prefix derivation —
eligibility entries by a filtered pass over the policy's `F-` keys, open positions through their resolved feed;
both rationales unchanged. Amended 2026-08-10: the entry claim drops its credit-granting test and every stored
entry claims its series. Under D58's strict positivity the test is vacuous, and it is deleted rather than kept as a
redundant filter because the two readings differ in direction — an entry crediting nothing, could one exist, would
have its feed removed silently, where walking the whole policy blocks removal until governance delists it. The
guard gates an act recoverable only at the cost of the activation delay and a re-warm, so it fails closed.

## D64

**Recorded status:** Confirmed.

Recognition caps are each asset's share of recognised capital itself: `EligibilityEntry.recognition_cap_ratio` in
[0, 1) clips credit at `ratio × recognised_capital`, the self-reference resolved per block in closed form. The
denominator was chosen against two alternatives. A flat `anoah` amount states a tolerance at one balance-sheet size
and decays into a binding constraint or a vacuous one — and a binding cap reopens the gap at full value, the §7.3
acquisition/refill loop. A ratio of the capital requirement scales the ceiling with the size of the need rather
than the fund's substance, so one corrupted attestation against an almost-empty fund could manufacture credit up to
a share of a large target. Against the certificate itself, credit levers only the provable NOAH base: a lone
corrupted input can never push the total past honest ÷ (1 − ratio), a fund holding no NOAH counts no asset at all,
and the invariant is checkable against the answer — no clipped asset exceeds its ratio of the reported figure, and
a clipped asset holds exactly it. Clipping one asset shrinks the total every other share is measured against (the
shrinking-denominator effect), so the clipped set and the total are found together: sorting assets by `raw ÷ ratio`
makes the clipped set a suffix, and the one consistent split gives `T = (base + Σ unclipped raw) ÷ (1 − Σ clipped
ratios)` — exact cross-multiplied big-integer arithmetic, no iteration, no stored valuation, totality by `T ≤ base
÷ (1 − Σ ratios)` with no attested figure in the bound. Existence requires the policy-wide ratio sum strictly below
one, enforced at the policy write and genesis import: at one the denominator dies and the caps admit everything.
The couplings the self-reference introduces all point conservatively — a dark feed zeroes its own asset and
tightens every neighbour's ceiling — and `RecognisedCapital(ctx)` stays a pure local read with the §7.3 split
untouched: nothing is threaded from Treasury, and the surplus a committee may burn is monotone in the requirement
again. The EVM warning against on-chain waterfall caps (float nondeterminism, gas-bounded recursion) does not
govern a Cosmos keeper fold: the arithmetic is integer, the policy is governance-written rather than
attacker-supplied, and the closed form replaces iteration — while the query still publishes each block's resolved
`effective_cap` per asset, the absolute ceilings a debt-ceiling design would have governance push by hand.
Implemented 2026-08-06.

## D65

**Recorded status:** Confirmed.

The Reserve's send restriction admits any asset-registry member by membership alone, alongside NOAH and
eligibility-listed denominations. Ark paper is protocol liability wherever it sits, can never earn recognition
credit (D28), and the Reserve is the one account with a committee able to retire it, so refusing it never protected
the fund's figures and only pushed it somewhere the chain cannot see. This subsumes and deletes the
transfer-tax-collector sender exemption — written-off and retired assets are still members — which strictly
tightens the rule, since the collector loses its bypass for coins that are neither NOAH nor members. Accepted cost:
anyone may push member dust into the account, bounded by registry size and burnable by the committee. Implemented
2026-08-08. Amended 2026-08-09 (D70): admission narrows to NOAH-or-member. The eligibility-listed clause never
admitted anything a mint path could produce, and its external-shape successor lasted one review — both implied an
on-chain external-custody lane that genesis and movement valuation refuse and D59 forecloses. In-kind return legs
were always served by the surviving clauses: returns arrive as NOAH or members.

## D66

**Recorded status:** Confirmed.

Treasury's liability partition measures and discloses self-held supply: for every member the aggregate counts, the
strategic Reserve's balance of it, valued through the same branch that counted it (fresh, settlement, or last
known). Accrual happens inside each counted branch rather than over the registry, so the two sides of the
subtraction can never price on different bases and `net = gross - self_held` is non-negative by construction.
Reported by `FundStatus` as `self_held_supply`, `self_held_liability`, and `net_liability`. Implemented 2026-08-08.

## D67

**Recorded status:** Confirmed.

The claimable aggregate resolves to two bases, and one principle assigns every consumer: a flow serves claims that
can arrive, and no claim can arrive from self-held paper; a bound serves discretion, and the discretion-holder can
re-issue that paper, so bounds price it as if they already had. Flows take net — `calculateFundStatus` gaps and
`DrawRedemptionBuffer` coverage. Bounds take gross — `targetBasis` and everything derived from it:
`RequiredReserveCapital` (hence `BurnableSurplus`), `InsuranceShortfall`, `RedemptionBufferShortfall`. This
finishes the exclusion the partition already performs for written-off and untrusted supply, reaching the last
unclaimable supply it still counted. Implemented 2026-08-08.

## D68

**Lifecycle:** Superseded by amended [D33](#d33): the snapshot and both invalidators were removed.

**Recorded status:** Confirmed.

Mid-block coherence of the primed liability snapshot is maintained by asymmetric invalidation. Outbound crossings
of the Reserve boundary by member coins drop the snapshot through the registry-cache invalidator Treasury already
implements: every Reserve burn touching a member, and a `CommitteeDeploy` of member paper, which returns netted-out
supply to circulation and would otherwise leave the block undersizing targets. Inbound crossings invalidate nothing
— an inbound send can only make true net liability lower than the snapshot, which is staleness in the conservative
direction, corrected at the next prime — so the send restriction stays a pure admission predicate with no
cross-module side effects. This also fixes a standing violation of the hazard note in `PrimeLiabilitySnapshot`: a
residue burn of an active member previously left the rest of the block sizing targets on pre-burn supply.
Implemented 2026-08-08; superseded the same day by D33's block-granular settlement, which removes the snapshot this
decision kept coherent. Both invalidator interfaces, their wiring, and every call site are deleted. The hazard the
decision managed — a mid-block supply move that forgets to drop the snapshot, mis-allocating funds with no error —
is now closed by construction rather than by each new writer remembering, which is the reason the change was made:
the asymmetric rule below was correct, but it had to be re-derived by every future path that touches member supply.

## D69

**Recorded status:** Confirmed.

External holdings are named by external symbols — `<feed>-<tag>`, the prefix passing the priced-denom rule, the tag
bounded lowercase alphanumeric — partitioning the namespace from Ark-issued assets by shape rather than by state.
The registry side has been enforced since the priced shape existed (its charset never admitted a dash); the
eligibility side requires the external shape at the types level, so policy ∩ registry = ∅ is a theorem about
strings with no read, no ordering, and no cross-module cooperation to keep true. `validateExternalEligibility` is
deleted rather than extended; the burn-residue credit refusal is kept as a provably unreachable backstop; the
journal becomes permanently unambiguous about which instrument its history names — a property no write-time guard
provides, because closed history raises no claim. An external symbol can never be protocol paper — not registrable
by shape, and conversion mints registry members alone — while custody the chain does hold under such a name stays
ordinary bank state the fold counts; an off-chain NOAH holding is unrepresentable via the prefix rule. Implemented
2026-08-09; see [external-symbol rationale](../../x/reserve/README.md#external-symbols-and-freshness).

## D70

**Recorded status:** Confirmed.

An external symbol's pricing feed derives from its prefix, unconditionally: `ExternalFeed(symbol)` is a pure
function, never derive-if-exists, which would silently re-point every external symbol on a series the moment a twin
feed activates. The derivation is many-to-one — several tags share one series with per-entry haircut, cap, and
window, so multi-custodian holdings need no aliasing machinery and no duplicate feed, and a holding honestly priced
on a different series is a different prefix. Listing requires the derived feed Active — exactly, not merely
scheduled: an entry's haircut and window are judgments about how a series behaves, voted blind if it has never
printed a rate, and a series scheduled for removal has already passed its guard — matching registration's rule and
moving the Reserve's genesis ordering dependency from `x/asset` to `x/oracle`. The feed guard maps external symbols
by a filtered pass over the contiguous `F-` policy keys and open positions through their resolved feed.
Deployment's acquired leg and any denomination-changing correction pass external-or-current-member, closing the
bare-issuable-name side door. The send restriction narrows to NOAH-or-member: the external-shape clause admitted a
name class no mint path can produce, genesis refuses, and movement valuation cannot price — a door to an on-chain
external-custody lane D59 forecloses — so a future module making such custody real widens admission, genesis, and
valuation together in its own spec. Cap ratios sum per series family, and governance sizes the family rather than
the row. Implemented 2026-08-09. Amended 2026-09-06: the refusal is permanent; on-chain custody of an external
token will never be built.

## D71

**Recorded status:** Confirmed.

Staleness tolerance is entry policy, not feed state: `EligibilityEntry.max_rate_age` — required positive, capped at
`MaxRecognitionRateAge` (30 days) — states how old the derived feed's rate may be and still back this entry's
recognition credit, never inherited from the Oracle default, which answers for conversion-grade reads. The Oracle
judges per request: `GetRateSetWithin` takes one request per entry — the entry's denomination and window — derives
the series from the name itself, and answers under the name, so two tags on one series may state different windows
and receive different verdicts, keyed apart rather than colliding on the shared feed, and no request can route a
name to any series but its own. Nothing unjudged crosses the module boundary, and the window never enters Oracle
state. D60's override machinery is deleted end to end — collection, `MsgAddFeed.max_age`, genesis field, events,
query — while `Params.MaxExchangeRateAge` survives as the single default behind every gate-enforcing read,
including `valueMovement`'s conversion-grade pricing of proven movements. `GetLastKnownRateSet` and its fence are
untouched. Implemented 2026-08-09.

## D72

**Recorded status:** Confirmed. Volatility is annualised over the seconds its returns spanned under [D133](#d133).

Risk-scaled fund targets. Every target is sized on `m x` its existing liability basis — net for the expansion
waterfall, gross for the bounds on committee acts — where `m` is a governance-parameterised multiplier over three
indicators the chain already maintains: the liability ratio (net liability over circulating NOAH, the reflexivity
term, which collapses to that quotient because liability is already valued in NOAH under D24), annualised realised
volatility of the protocol reference rate, and an EMA of per-block net redemption flow. Samples fold in
`SettleConversions`, which Market's EndBlocker calls every block with the flow facts already NOAH-valued in
`ConversionTotals`; the multiplier itself recomputes on a governed period, step-limited and clamped to `[1, cap]`.
The three indicator weights live in `EconomicPolicy` and the guardrails behind them in `Params`. An earlier draft
put all eight fields in `Params` to avoid widening a threshold-multisig's mandate, which mistook D36's field count
for its principle; the criterion that actually sorted the two messages is stance against machinery, and a weight
stating how much extra capital a unit of leverage should demand is the same kind of lever as the target ratios it
modifies — reversible within hours by zeroing it, and the most time-sensitive knob the module has, which is the
committee's founding use case. What stays with governance is what makes the delegation safe: the decays define the
measuring instrument and are not reversible in any useful sense, because a series folded under one memory is not
recoverable by restoring the old value; the cap is the ceiling on how far the response may go; the step is the
anti-gaming rate limiter, which a committee able to set it could raise to reach the cap in a single update; and the
cadence follows D37. A committee at the maximum of its mandate can therefore move the multiplier no faster and no
further than organic stress already can, so the delegation adds no blast radius. Weights inherit the mandate's
minimum and maximum clamp for free, letting governance floor them above zero to deny the committee the power to
switch the model off, or leave the floor at zero to grant it, per appointment. All three funds scale on one basis:
the ratios are voted as a set and read as relative fund sizing, so scaling a subset would move those proportions
without a vote, and the indicator applies to every fund sized against the same liability. The per-fund covered-risk
basis of section 7.2 and D28 remains the later refinement; D8 already records the single shared basis as an initial
simplification. Every weight defaults to zero, which pins `m` at one and reproduces current behaviour exactly. The
liability aggregate plays three roles and only one is scaled: requirement bases read `m x L`, the redemption draw's
payment denominator reads raw `L` (D73), and the multiplier's own liability-ratio input reads raw `L`, because a
controller that fed its own output back into its input would compound.

## D73

**Recorded status:** Confirmed.

The redemption draw is never scaled by `m`, in either direction. Dividing its basis rations the run's opening phase
— the phase that decides whether a spiral ignites — to guard against an exhaustion that cannot happen: proportional
coverage gives `B = B0 x (L/L0)`, so the Buffer depletes exactly in step with the liability it covers and reaches
zero only when the last stablecoin is redeemed. Multiplying coverage fails the other way: it double-counts one
signal in the payment path, since `m` has already raised coverage by raising the Buffer through the targets, and it
front-loads finite inventory on a bet about run depth (`B = B0 x (L/L0)^m`), leaving a thinner Buffer after any
partial episode and pinning coverage at one until depletion releases it mid-run. Both directions reintroduce the
price-reactive Buffer share section 1.1 forecloses and D4 exists to prevent. The draw's risk response is mediated
by capital instead: the scaled targets grow the Buffer before a run, and the `m`-widened Redemption Buffer
shortfall widens the committee's Reserve-to-Buffer injection cap during one.

## D74

**Recorded status:** Confirmed.

Contracts reach Ark's own modules through proto, not a hand-written JSON surface. Wasmd's custom **message**
encoder goes unused: a contract calls Market with `CosmosMsg::Any` carrying `/ark.market.v1.MsgSell` and proto
bytes, and `x/market/wasm` and `x/wasm/exported` are deleted rather than ported to `wasmvm/v3`. The custom
**querier** goes unused as well: D43's estimate is Treasury's `Query/ComputeTax`, reached through the accept list,
so contracts hold one proto surface for reads and one for writes. The argument is one schema instead of two: a
custom encoder mirrors each message as JSON on the Go side and again by hand in every contract, and nothing detects
the drift — the same objection D43 raises against a second message model and CLAUDE.md raises against a second
validation site, already visible in the dead binding's stale duplicate checks. Three things that looked like costs
are not. The parser's `Trader = contractAddr` override is not protection: Wasmd rejects any dispatched message
whose signers are not exactly the contract, so impersonation is closed either way. Tax does not differ:
custom-encoded messages route through the same message handler the Treasury wrapper decorates. And no installed
base breaks, because genesis is fresh (D20) and a Terra contract cannot port unmodified regardless — `anoah`
renames the denomination, 18 decimals move the money math, and the type URLs are Ark's. What the JSON envelope
preserved was its own shape, not compatibility. Ark also already made this choice twice: Classic shipped market,
oracle, and treasury bindings and the port kept only market's, unwired and on `wasmvm` v1. Stargate/gRPC
**queries** require an explicit accept list — Wasmd ships no permissive default — written by hand as Osmosis,
Neutron, Juno, and Archway keep theirs, every entry additionally required to carry the
`cosmos.query.v1.module_query_safe` annotation, which keeps contracts to deterministic reads; the ICA host derives
its own allow list from that annotation inside ibc-go and cannot be pointed at the Wasm list, so the two surfaces
are separate by construction. Messages need no such list; the signer check is the gate. A Rust bindings crate is
deliberately **not** committed to here: authors can generate from Ark's published protos, and publishing later is
additive. That asymmetry decides the whole entry — adding a custom encoder later breaks nothing, removing one later
breaks every contract using it, so the reversible direction is the one to start in.

## D75

**Recorded status:** Confirmed.

Oracle rates are NOAH per one unit of the feed's denomination (`USD/NOAH`, not `NOAH/USD`); `RateSet.Convert` is
`amount × rate[offer] / rate[ask]`, so valuing in NOAH multiplies. Settlement plan rates and the
governance-supplied outgoing reference rate carry the same orientation. Every stored rate — settlement plan rates
included, since a plan sits beside oracle rates in the registry's rate sets — is bounded at `MaxExchangeRate`, the
magnitude a direct report is held to by `MaxEncodedVoteRateBytes`, and the tally omits a derived price above it, so
the halt-class folds that multiply a 2^128-capped quantity by a rate stay inside the LegacyDec domain by
construction. Flipped 2026-09-02; see `x/oracle/README.md` §1.4.

## D76

**Recorded status:** Confirmed.

Prices and quantities rebase differently. `RateSet.Convert` re-expresses a quantity (`q × rate[old] / rate[new]`);
a stored price of the reference unit — the exposure anchor — moves by the reciprocal factor (`p × rate[new] /
rate[old]`), which is the same `Convert` call with the two units passed in the opposite order, so the chain keeps
one arithmetic path and reversed arguments at a price site are the operation rather than a bug. Every rebase site
states which it holds: the Market base pool, the Treasury tax cap, the base gas price, and the conversion-factor
table (via the one-unit cross) are quantities; the anchor is the one price.

## D77

**Recorded status:** Confirmed.

The sidecar resolves in the chain's orientation end to end: a feed's output pair is `UNIT/NOAH`, routes end at NOAH
(`KRW/USD × USD/NOAH`), bootstrap prices are stated in leg orientation, and `PricesByFeed` re-keys a resolved price
without inverting. Provider markets stay in the orientation their venue quotes; the resolver's per-sample
normalisation (`providerSamples`) is the one place a venue quote meets a leg, so nothing after the provider cache
inverts, and route averaging is an arithmetic mean of the published figure. First implemented 2026-09-02 with a
single reciprocal at the feed boundary (`types.FeedPrice`); amended 2026-09-03.

## D78

**Recorded status:** Confirmed.

The signed fee declares the transfer tax. A transaction's fee is gas plus the exact tax its messages owe; the ante
refuses a fee short of the tax before deducting anything and charges the tax only from what was declared, so a
signer sees the whole charge in the fee they sign and is never taxed past it. The fee field carries the declaration
because it is the one slot every wallet already builds and every sign mode renders, amino included; an extension
option would not survive amino signing, and the ante rejects them. A wallet that omits the tax is refused with an
error naming it, which forces every client to price the tax before signing — the deliberate cost. The direct charge
to `transfer_tax_collector` and the deletion of the routing step stand; only the subtraction returns, and the gas
remainder alone is deducted — by an Ark-owned fee decorator in place of the SDK's, so simulation deducts what
execution deducts. Amended 2026-09-03. Amended again 2026-09-03 (D80): the fee is a ceiling and the tip rides NOAH;
the subtraction that made excess into gas is gone, the declaration is unchanged.

## D79

**Recorded status:** Confirmed.

Rename `MonetaryPolicy` to `EconomicPolicy`, and the stability tax to the transfer tax. The committee's six levers
are two fiscal — the two block reward targets, which set what the protocol pays out — and four capital: the three
fund target ratios and the D72 exposure weights, which set how much capital it holds against its liabilities and
how fast it accumulates. The transfer tax rate was a third fiscal lever when this decision was written; D80 moved
it to `Params`, so the committee now holds no lever over what the protocol takes in. None is strictly monetary. The
committee cannot mint, quote, set spread, or move the pool, so it holds no rate or supply instrument; spread and
pool depth are Market's, and the Reserve-to-Buffer commitment is a governance vote. `Economic` is the accurate
umbrella over both halves, where `Fiscal` would misname the capital half and `Monetary` misnames the whole. The tax
rename follows the same argument. Terra's stability tax funded validators through an adaptive controller holding
unit mining rewards stable against the seigniorage cycle; §1 removed that controller, `x/mint`, and all
seigniorage, and D11 gave the proceeds to the Oracle target first. What the tax does here is fund security and the
price feed out of stablecoin usage, and compensate the dilution stakers still bear under §1.1. It moves no peg,
quote, spread, or supply figure, so "stability" claimed a role it does not have, while "transfer" names the base
D18 already taxes: every user-facing stable transfer surface. Bare `Tax` spellings stay — `TaxCap`, `ComputeTax`,
`reference_tax_cap` — because inside Treasury the tax is the transfer tax and the Tobin tax belongs to Market and
Oracle. Done now because it is free now: fresh genesis (D20) means the `transfer_tax_collector` account rename
carries no migration, collection prefixes and proto field numbers are unchanged, and the cost after launch would be
a store migration plus every client's message and query names.

## D80

### Fee ceiling and NOAH tip

**Recorded status:** Proposed.

The signed fee is a ceiling, and the tip is NOAH. Every fee leg but NOAH is charged at most what the chain computes
it owes — the exact transfer tax in that denomination, plus the base fee if it is the first leg in denomination
order whose slack above the tax covers the requirement — and the rest never leaves the payer. The NOAH leg is
charged whole: it pays the base fee when no stable leg did, and the remainder is the tip, ranked in reference units
per gas through NOAH's factor. NOAH is the one denomination the tax is never owed in (`GetTaxCap` excludes the
numeraire), so it is the second signed number the fee field lacked: the chain can always tell a tip from a padded
tax, which one denomination split by subtraction could not, and a wallet pads a stable leg for rate drift at no
cost. Under D78's subtraction one percent over on a tax the size of Terra's cap was half a gas fee, paid to
validators. What it costs: priority needs NOAH, so a pure-stable payer gets base-fee inclusion in arrival order
within its lane, which the base-fee ramp already guarantees; and a NOAH-paid gas fee has no free headroom, its pad
being a tip bounded by the fee it pads rather than by a tax. A NOAH leg is refused while NOAH's factor is absent,
as a NOAH gas fee already is. D78's declaration stands: a fee short of the tax is refused before anything moves.
The single-denomination rule and the excess refusal go with the subtraction. This is not a refund: nothing is
escrowed or returned, the gas limit is still charged whole at the base price, and the ante deducts less rather than
handing anything back. Amended 2026-09-03.

### Transfer-tax parameter ownership

**Recorded status:** Confirmed. Amended 2026-10-10: the pre-launch renumbering dropped the reserved `EconomicPolicy`
field 1; the rate stays `Params` field 12.

Move `transfer_tax_rate` from `EconomicPolicy` to `Params`. The rate passes D37's test — a lever stating how much
the protocol wants, reversible and safe to clamp — but D78 changed what kind of number it is. The rate is part of
the fee every wallet signs, so a raise refuses every in-flight transfer until clients re-query, and a committee
update lands the instant the message does; every other lever in the message moves protocol-internal allocation.
Market's committee band over the Tobin tax is not a precedent: a swap carries the trader's own `minimum_receive`,
so a Tobin raise only fails a swap that breaches a floor the user chose, where a transfer tax raise fails a
transfer the chain itself refuses. Governance's voting period is the notice a raise needs and it already exists;
the rate rejoins `reference_tax_cap`, so the one calculator D18 insists on has one owner and one proposal can
activate rate and cap together; and the committee keeps the two reward targets without being able to raise the
charge that funds them. The fast-cut argument for the committee is weaker than it looks: under D78 a cut never
refuses an in-flight transaction, since the declared fee still covers the lower tax and the remainder is deducted
as gas, so the only fast move worth having is in the safe direction and an expedited proposal covers it; a
calculator fault is an upgrade, not a rate change. D79's classification stands — the rate is fiscal — only its
holder changes. `EconomicPolicy` field 1 is reserved and the rate is `Params` field 12 under the same `[0, 1]`
domain cap; the mandate bounds lose the field; `Query/Params` carries it and `Query/EconomicPolicy` does not;
wallets are unaffected because they price through `ComputeTax`.

## D81

**Recorded status:** Confirmed.

Hold `transfer_tax_rate` at or below the conversion spread floor. NOAH carries no tax cap, so it is the one untaxed
denomination (D44), and `market.MsgSell` is exempt (§8.2), so `MsgSell` stable→NOAH, `bank.MsgSend` NOAH, `MsgSell`
NOAH→stable delivers the same value to the same recipient for spread instead of tax. The cap neither softens the
requirement nor changes its shape: with `tax = min(rate × A, cap)` against a spread cost of `floor × A`, the
substitution pays exactly when `rate > floor`, at every principal — below the cap crossover both charges scale with
the amount, and above it a capped tax meets an uncapped spread. The bound is on the corridor rather than the live
floor, because a conversion committee may lower `min_stability_spread` to `ConversionMandate.minimum_policy`; that
minimum is the number governance must stay under when it sets either side. Nothing enforces this in code — Market
depends on Treasury, so Treasury cannot read the floor, and neither module's stateless `Validate` sees the other's
state — so it is a governance-time constraint on P1's launch rate and on every conversion appointment.

## D82

**Recorded status:** Confirmed.

Charge the transfer tax after the messages, on success only. The ante prices the tax, holds the declared fee to it
(D78), and deducts the gas fee and tip alone; a post decorator on the messages' branch charges the tax and draws
the granter's allowance for it. Amended 2026-09-07: tax affordability and the tax allowance draw are
execution-only. Admission, recheck, and both proposal stages enforce the signed tax declaration and upfront gas
fee, but do not rehearse tax collection. The post decorator collects only during finalisation and simulation, after
successful messages, reading the balances and allowances those messages left. Proposal acceptance changes and
requires coordinated activation on a running network; no stored-state migration is needed. A transaction whose
messages fail pays gas and no tax; a payer the messages leave short of the tax fails the transaction at the charge,
gas kept, principal unmoved; an allowance records what left the granter and nothing more. The declaration, the
ceiling and NOAH tip (D80), priority, and the direct charge to `transfer_tax_collector` stand. Not a refund —
nothing is escrowed or returned, which is what the 2026-08-29 no-refunds finding foreclosed; the post chain returns
for deferral, not settlement. Signed transactions thereby meet the terms D42 already set for a contract's
dispatches, and the two seams agree. The ante hands the figure it held the declaration to on through the context,
and the post charges that figure: the tax is computed once, the charge cannot exceed the declaration by
construction, and a post chain finding no figure fails the transaction rather than passing a transfer untaxed. What
it gains beyond fairness: a transfer whose tax the payer cannot afford until an earlier message in the same
transaction has funded it now succeeds, where the ante charge this replaces refused it. What it costs: an IBC
packet that later times out keeps its tax (D42, D46 unchanged); a doomed transaction pays gas for messages that
then revert, rather than being refused before they run; and feegrant emits two use events per sponsored
transaction. An upfront tax affordability check, in ante or a post rehearsal, can refuse transactions execution
would have funded and repeats balance and allowance work without reserving either. Transactions that remain unable
to pay tax may enter blocks, fail execution, and pay gas with their sequence consumed. Rationale in §8.5 and [tax
commitment rationale](../../app/ante/README.md#transfer-tax-commitment-and-rollback).

## D83

**Recorded status:** Confirmed 2026-09-11.

Permit governance to return subsidy NOAH to the community pool through `MsgReturnSubsidy`, a fixed-endpoint message
carrying an amount and a per-proposal minimum remaining balance, evaluated at execution as `MsgFundBuffer` evaluates
its own (§3.5). The pool's only outflow was the settlement draw, so a chain whose gas and tax cover the targets never
touched it, while the community pool could already top it up by a spend to its address (D25); the return closes that
asymmetry without changing the pool's default. No floor parameter: a floor read from the live targets is a
projection governance can move under by lowering the targets first, and an over-return leaves only an unfunded
shortfall the next window reports and a deposit reverses, never a halt. No automatic sweep: a rule that judges
revenue sufficient reads targets governance sets, and every flow between funds is a governance act. No committee
power: the return is fiscal, as the tax rate is (D80). Governance already reached the pool in one direction by
raising the targets, so the message adds a bounded, auditable exit rather than a new class of access. D9 and D25
stand.

## D84

**Recorded status:** Decided 2026-09-22. Amends the launch review's seat admission recorded in
[GENESIS.md](../governance/GENESIS.md#3-accounts-supply-and-validator-seats) (decided 2026-09-11). Seat sizing amended by [D94](#d94) and set per seat again, 5M, by [D111](#d111); the float by [D96](#d96).

Validator entry after launch is open. The ten founding seats are the only granted stake: equal permanently locked
grants and floats drawn from the community pool at assembly, each adding one seat's share to both reward targets. A
later validator bonds NOAH it holds through the ordinary staking transaction; no proposal grants it a seat, and the
seat-admission procedure and its e2e coverage are removed. Equality is therefore a launch fact rather than an enforced
property, and the reward targets no longer track the set by construction: they are the founding count's shares at
assembly and move afterwards only by `MsgUpdatePolicy`, inside the committee corridor or by governance beyond it.
Why: a granted seat made the founders the gate to consensus for as long as they held the vote, which is the control
the [disbursement plan](../governance/DISBURSEMENT_PLAN.md) exists to hand over, and a seat budget drawn from the
pool competed with that distribution for the same NOAH. The seat command, the assembly sequence, and the testnet
tooling that seats validators from the artefact stand. D9, D13, and the gov thresholds stand. Amended 2026-09-22: the
seat vests rather than locking for good, a continuous vesting account from four years after genesis to ten, so a
founder can leave with what a decade of running the chain paid. The four-year cliff keeps the floats the only liquid
NOAH a founder holds through the launch years; the block line, the escrow's 50M subtraction, and the cap's own-stake
count stand, the subtraction turning conservative once a founder sells.

## D85

**Recorded status:** Decided 2026-09-22. Amends the accept-list practice recorded in [D74](#d74).

**Lifecycle:** The contract escrow that motivated this expansion was replaced by the native Disbursement module in
[D87](#d87). Disbursement now reads staking through its keeper; the Wasm query accept list remains available to contracts.

The contract query accept list admits every query the SDK annotates `module_query_safe` for auth, bank, and staking,
thirty-four paths, beside the fourteen hand-written Ark entries. The annotation is upstream's review, and the test
that requires it of every entry stands; the hand-written review is for Ark's own modules, whose response shapes Ark
freezes by listing them. Distribution, gov, and slashing carry no annotation in the pinned SDK and stay out until
upstream annotates them. Why: the grant escrow in the [disbursement plan](../governance/DISBURSEMENT_PLAN.md) reads
total bonded stake through the staking pool query, and a list widened one path at a time for each contract need would
re-review what upstream already has. Wasmd's native bank, staking, and distribution queriers were already on; the
listed paths add what they lack: the pool total, both modules' params, unbonding and redelegation views, historical
info, spendable balances, send-enabled, and account lookups. D74 stands.

## D86

**Recorded status:** Superseded by [D87](#d87) for custody and execution; recorded 2026-09-22. The schedule and ownership amendments below remain the economic baseline, except the seat-excluding ceiling, which [D90](#d90) replaces.

**Lifecycle:** The following is the historical contract design. Its Rust workspace, Wasm fixture, and deployment
workflow have been removed. Current custody, messages, and authorities are defined by the native
[Disbursement module](../../x/disbursement/README.md); use the [grant runbook](../governance/GOVERNANCE_OPERATIONS.md#5-grant-tranches)
for operations.

The [disbursement plan](../governance/DISBURSEMENT_PLAN.md)'s grants are created by a CosmWasm contract,
`contracts/grant`, funded one tranche at a time by `MsgCommunityPoolSpend` and instructed by `MsgSudoContract`, not by
proposals that name recipient addresses and not by a chain module. Members are issued at registration by a registrar
key governance sets; contributor grants are escrowed in the same contract and released by the plan's cap and bloc rule
against the staking pool query. Why: a proposal listing addresses publishes them for its voting window, and one base
unit sent to any of them creates a plain account the vesting message refuses, failing the whole proposal at trivial
cost to the sender. The contract creates each account in the registrar's transaction as a submessage with a reply on
error, so a dusted address costs one member a re-registration, never a tranche. A module that wrapped an existing plain
account was built and reverted the same day: it would have stayed in the binary for the life of the chain for a
distribution that ends, and it kept the member roll in proposals. The pool's single exit stands (D9, D25); the contract
has no admin because wasmd's governance policy lets a vote sudo or migrate any contract; it grants each account it
creates a fee allowance from its own balance, so no platform account funds gas and no grantee holds a float; its cap
counts everything a person has had from the pool as bonded, the conservative reading of the plan's rule; and Rust is
pinned in `mise.toml` to the optimizer's version, since newer wasm defaults are outside what the chain's wasmvm
accepts. D85 stands and is what the contract reads. Amended 2026-09-22: the contract bounds issuance to a
governance-set number of members per window, so a stolen registrar key costs a window's issuance rather than a
tranche before a vote replaces it, and a grant too small to give every vesting period a coin is refused where it is
set rather than dispatched to a chain that rejects it. Amended again 2026-09-22: the founder's first 5M, the one grant
paid outside the contract by a proposal naming an address, goes through it under the bloc rule like every seat
holder's, and the field that restated pay outside the contract is gone with nothing left to restate. Amended
2026-09-23: a member grant is no longer a vesting account but a stream the contract holds, the tenth sent at
registration and the rest paid by bank send as each month elapses, because a vesting account has no clawback and a
sybil found after registration would still collect the whole grant; the registrar can suspend or resume a member's
pay and nothing more, a resumption paying the months since, governance cancels in batches with the rest staying in the
tranche, and a void lifts a stolen registrar's suspensions in one write. The dusted-address rejection went with the
vesting message, since a send lands in an existing account.

Amended again 2026-09-23: the contributor ceiling limits cumulative ownership grants to 60M, excluding the founding
seat. The 5M seat still counts in `own` and in the fifth-of-others concentration limit, so a seat holder may receive
the full 60M in grants only when that limit and the bloc rule admit it. Their total ceiling is 65M; a contributor
without a seat remains capped at 60M. This matches the founder's full grant in the disbursement plan.

Amended again 2026-09-23: contributor ownership grants use one contract-held schedule from approval, four years with
25% after a year and the rest monthly. Each bank payment is the smaller of accrued unpaid entitlement and the live
cap/bloc allowance. Accrual continues through cap delays, which may outlast the schedule; no new vesting clock starts
when coins leave. Payments are tracked in base units, including partial periods, without million-NOAH rounding.
Unpaid grants cannot stake or vote; paid coins are spendable. Founding seats retain their native vesting and launch
stake. Ownership grants and pay streams share the clock and payment path; streams remain outside ownership limits.
The payee is reusable, the vesting-account/reply machinery and gas reserves are gone, and anyone can trigger the first
payment for an unfunded recipient. A cancel returns only unaccrued funds, retaining accrued pay under the same limits.
Why: a stake-dependent wait followed by a fresh four-year vesting schedule made a grant's timing unnecessarily hard
to understand. A single clock removes that second wait without relaxing the live ownership checks. Handover now
counts actual scheduled payments and recipients' staking choices, with no early contributor stake assumed. Contract
0.2 changes the message/state layout and refuses migration from 0.1 until an explicit conversion policy exists.

## D87

**Recorded status:** Decided 2026-09-23; funding amended by [D88](#d88), the registrar by [D89](#d89). Amended
2026-10-01: the compensation allowlist is removed; NOAH or an active registry asset is the only denomination gate, and
the committee's allowance is the bound. Payment amended by [D105](#d105): the block pays due grants, and there is no
release message. The immediate first member payment waits two weeks under [D126](#d126).

Replace the prelaunch grant contract with native [`x/disbursement`](../../x/disbursement/README.md). The same transparent commitment
ledger will serve member and contributor distribution now and ongoing compensation, including stablecoins, as the system matures. Distribution
still funds it through `MsgCommunityPoolSpend`; governance creates fully funded contributor awards through typed
messages, and a bounded registrar registers members outside proposals. Preserve D86's immediate first member payment,
single contributor clock, live ownership and founder-bloc caps, retained earned debt, and distinct treatment of pay.
Stablecoin compensation is an untaxed protocol disbursement, funded and paid in its own denomination, with no
conversion guarantee. It never increases NOAH ownership counters. A new stablecoin award requires an active Asset
registry entry; funded obligations survive subsequent lifecycle changes.

Founding status is an immutable genesis seat record, not an award input that a later grant can overwrite. The ratio
and ceiling are genesis ownership policy rather than mutable operational parameters. Beneficiary identity and
cumulative ownership persist across all controllers, payees, and grants. A member may change a payee using their
original registered key; the registrar has no redirection authority, and the original address can never register
again. Cancelled members retain accrued unpaid entitlement under the effective suspension cutoff, while unearned
funds become unallocated; cancellation never attempts a member payment. Contributor unearned funds return to the pool.
The registrar holds a term-limited committee mandate; a suspension records its term, and governance voids a replaced
term in one write, so bulk recovery is bounded and a fresh suspension under the new term stands even in the same block.

The module records permanent terms, work references, payments, destinations, cancellations, and administrative changes
in an append-only journal with indexed pagination and typed events. Releases maintain founder aggregates and perform
no scans of historical grants; no block hook pays recipients. Export/import preserves absolute clocks and history and
rebuilds derived state after validating records and Bank backing. There are no live contract commitments to migrate:
remove its Rust workspace, embedded fixture, and deployment workflow. CosmWasm remains available for other applications;
D85's existing query accept list is unchanged.

## D88

**Recorded status:** Decided 2026-09-26. The community pool remainder is 35M after ten seats under [D96](#d96), [D111](#d111), and [D115](#d115). The contributor tranche is removed by [D117](#d117). The pools are 469.2M and 312.8M, 782M in all, under [D125](#d125).

Hold the distribution in `x/disbursement` from genesis. The artefact moves 732M NOAH out of the community pool into
two Disbursement module accounts, a 439.2M member account and a 292.8M contributor account, and leaves the community
pool 50M after ten seats, which still draw on it. An account's pool is its NOAH outside grants and orders; the module
records the part governance has opened. Each step of the plan is a governance `MsgOpenTranche`, which moves no coins;
only genesis grows the member pool, and a NOAH deposit joins the contributor pool unopened. Members draw on the open
member tranche; ownership awards and NOAH compensation on the open contributor tranche; a cancel reopens unearned NOAH
in the tranche it was charged to. The member account has no exit. `MsgReturnUnallocated` returns from the contributor
account: NOAH from its pool, unopened first, or any other denomination outside grants. Why: with the whole
distribution in the community pool, the 60/40 split and the member pool were a promise the founders, the only voters
at launch, could revoke by one proposal; held by the module, the member pool pays members or nobody. Tranches keep
the pace a governance decision, where the gates belong while their ratio is open. What it costs: the custody
accounting and tranche checks need audit before launch rather than before the first tranche, and a member remainder
nobody registers leaves only by a chain upgrade.

Contributor NOAH funds stablecoin compensation in the award itself. Amended 2026-10-02: a stablecoin award draws
uncommitted custody first and converts the shortfall from the open contributor tranche through Market's ordinary swap,
inside the message that creates it, offering at most the `max_offer` it carries; the module sizes the smallest offer
that buys the shortfall, so the pay and the worst price are one decision and the NOAH cost is what the market charges.
Market quotes, mints, and settles it at EndBlock and Treasury allocates the offer as for any expansion, which
governance's own awards reach now that proposals execute before settlement (D33). The standing conversion orders,
their executor role, and the spread cap are removed: an order sized days ahead of its fills paid for timing nobody
owned, and a proposal could not convert at all. Cancelled stablecoin compensation stays in custody and funds the next
award before anything converts. An award never converts after it is funded: obligations stay funded in their own
denomination (D87).

## D89

**Recorded status:** Decided 2026-09-26. Amended 2026-10-08: a term has no award-count limit, the allowance and the
per-record cap bounding its value, and `MsgCancelTermGrants` is removed: governance lists a term's awards by the grants
query and cancels them by ID, and a second, expedited proposal cancels any made while the replacing vote was open,
inside the first period. The committee's award kinds and its seat refusal are amended by [D108](#d108);
its per-denomination allowance is replaced by [D110](#d110). Registration leaves the committee for a registrar it
appoints in [D120](#d120).

Fold compensation into Disbursement's one committee rather than appoint a second. The registrar becomes the grants
committee under a `GrantsMandate`: every module holds one mandate and every mandate's group is a committee, so the type
names the power, as `ClaimsMandate` and `ConversionMandate` do, within amino's 39-character message-name limit. The appointment adds a compensation power: a per-denomination allowance for the
term, each stablecoin denomination an active asset at appointment, and a minimum first period, positive whenever there is
an allowance. `MsgCommitteeCompensate` creates compensation only, funded like a governance award, never to a founding
seat holder, with a first period at least the minimum. A term's usage is the sum of its awards, cancelled ones
included, so a cancel never restores allowance and a new term starts at zero; a term makes at most 100 awards, one
cancel batch. Governance's `MsgCancelTermGrants` cancels every unfinished award of one term in one write, catching any
a key made while the vote replacing it was open. Bounties leave the ownership bands and become compensation. Why: one
committee keeps one appointment, term, and key process for the module's delegated work, and the first period is what
makes combining safe, since a stolen key's added exposure is awards committed but unpaid, which the replacing vote
cancels. What it costs: one key carries both powers, so recovery is three messages, appoint, void, and cancel the term,
and the term cancel also catches the term's legitimate awards, which governance re-awards.

## D90

**Recorded status:** Decided 2026-09-28. Amends the ownership ceiling in [D86](#d86)'s 2026-09-23 amendment. The 20M and 100M figures re-derive under [D94](#d94) to 19.5M and 90M; the 60M ceiling is 20M under [D97](#d97) and 30M under [D99](#d99), and the tenth is read as a tenth of the vote under [D98](#d98). The bloc rule is removed by [D106](#d106).

Size the founder's founding-band grant at 15M NOAH, 20M with the seat, and bound seat holders so that ownership
payments cannot restore the founders' block. At 15M the founder holds under the tenth of bonded stake one seat carries
at launch at every payment; at the earlier 60M the cap would have paid the founder up to the goal's sixth and held
them there from about 180M to 280M of public bonded stake. The bloc rule holds the ten at 3/10 of bonded stake rather
than a third: the tally passes a proposal only when yes exceeds 66.7%, so a bloc at a third still blocks, and under
the old rule the earlier grant's payments would have kept the founders a blocking minority to about 181M of public
bonded stake, past the plan's 100M handover line. The 60M ceiling now counts the seat; excluding it existed only to fit the earlier 65M, and was the one
rule that gave a seat holder more ownership room than another contributor. The founder's grant is proposed once public
bonded stake passes 100M, when the founders' own stake can neither pass nor block, so the public rather than the other
nine decides it; the bloc rule releases nothing to a seat holder before then. What it costs: the founder's clock
starts at that later proposal, other seat holders' grants wait for more public stake, and the founding band tops at
15M. The [disbursement plan](../governance/DISBURSEMENT_PLAN.md) carries the figures.

## D91

**Recorded status:** Decided 2026-10-01. The pool reaches about 66,300 members under [D125](#d125).

Set the member grant per step rather than fixing it: 20,000 NOAH at launch, then 15,000, 10,000, 7,500, and 5,000, each
step's figure carried by the proposal that opens its member tranche as a `MsgUpdateParams` beside the `MsgOpenTranche`.
A member keeps the amount and schedule they registered under, so a later step never touches an earlier member, and a
tranche remainder that rolls into the next step is consumed at that step's amount. Nothing on chain forces the
schedule, and no oracle is involved. Why: a fixed NOAH amount fixes votes but not value, so what a member receives and
what a faked member is worth both float with the price, and the open tranche bounds how many fakes a stolen key
registers but not what each is worth. A falling schedule keeps both flatter if the chain succeeds and pays the first
cohort for carrying the most risk and doing the vouching. Anchoring to a stablecoin value was considered and rejected
for launch: the oracle has no NOAH price until NOAH trades somewhere, a cheap NOAH would concentrate the pool into a
few large grants, and it would put the member pool behind an oracle it has never depended on. What it costs: members
from earlier steps hold more votes each, step 1 seats 1,500 people rather than 3,000, and the pool reaches about
60,300 members rather than 43,900. The [disbursement plan](../governance/DISBURSEMENT_PLAN.md) carries the schedule.

## D92

**Recorded status:** Decided 2026-10-02. A stolen key's registrations pay nothing for two weeks under [D126](#d126), so the
tranche bounds what it commits rather than what it takes.

Make the open member tranche the only bound on registration, and remove the rolling issuance window, its two params,
its log, and its query. Why: every other committee in the system is bounded by a figure governance sets at appointment
and consumes for the term, and none carries a rate limit; the member tranche is drawn by the committee alone, so it
already is that figure, in NOAH, set by the vote that opens a step and refilled only by another vote. A second count in
the mandate would restate it in another unit, and a rolling window paces by a clock no other mandate has. What it
costs: a stolen key can commit the tenths of whatever is open rather than a week's worth, so governance paces a step by
opening it in slices, one message per slice, and nothing closes an open tranche short of registering it, so the lever
against a committee governance wants to slow is to open no more, and against a stolen key the re-appointment that
already kills the old term's transactions.

## D93

**Recorded status:** Decided 2026-10-02.

The founding seats do not grow with the distribution. No step moves NOAH from the pools into the seats, and no rule
weights a seat's vote above its stake. Resistance to a buyer of distributed NOAH comes from the cost and visibility of
the purchase, the unbonding period, validators voting the delegations they carry, and the relaunch, never from founder
stake. Why: a buyer who stakes what they buy holds a third of the vote, enough to block any proposal or halt the chain,
with about half of what everyone else has bonded, roughly 85M at a 20% public staking rate and 160M at 50%; passing
alone takes twice that, a third of supply or more, bought from tens of thousands of holders on an open market. Founder
stake barely moves the first figure: each NOAH moved from the member pool into the seats raises the buyer's bar by a
fifth to a third of a NOAH, and for the founders to keep a veto once a buyer holds one they must hold more than the
whole public bonded stake, the founders' chain D84 hands over. Larger seats also enlarge the cheaper route: four
founders can halt the chain from block one, so a hostile actor pressures four known people rather than buying a tenth
of the supply, and seats transfer from year four, so a larger seat is a larger block for the same buyer. What it
costs: at a 20% public staking rate the founders end near 30% of bonded stake, a few points short of a veto the plan
never promised them. Open from this decision, each its own change: a minimum lead time, fixed in code, between a
vote and an upgrade or a large pool spend, so a bought vote is visible before it executes; and whether the
distribution schedule opens once, with the on-chain gate releasing each step, since today a third of the vote stops
every later `MsgOpenTranche` and every committee re-appointment (D88, D92). A member tally over the irreversible
message classes, the one electorate a buyer cannot purchase, is the direction if those prove insufficient; it is not
planned. The [threat model](THREAT_MODEL.md#27-governance-and-committees) carries the figures.

## D94

**Recorded status:** Decided 2026-10-02. Amends the seat sizing in [D84](#d84), the community pool remainder in
[D88](#d88), and the figures [D90](#d90) and [D93](#d93) derive from the founding stake. The float is 50k and the pool
57.5M under [D96](#d96); the gates are 15% of paid and the planning rate 20% under [D99](#d99); the fraction is a tenth
under [D100](#d100). The fixed total is replaced by a 5M seat, 50M at ten, under [D111](#d111).

Fix the founders' total at 45M NOAH, 4.5% of supply, split equally among the seats the launch review admits, 4.5M each
at ten. The per-seat grant follows from the total; the 300k float stays per seat, and both still come out of the
community pool, which keeps 55M after ten seats. Why: the total is what the plan argues from. The founders pass alone
while the public has bonded under half of it and block alone under twice it, the early attack cost is half of it, and
the first contributor cap is a fifth of it, so a per-seat constant let the seat count move all four without a
decision. The total is the one seat figure genesis fixes for good, so it is sized to hold under both staking rates the
comparables show: about 30% on the original Terra, whose rewards came from fees as Ark's do, and 15% on Terra Classic
today. Step one pays out about 20M liquid in its first year, so a total under 40M lets a buyer reach a third of the
vote on paper in year one; at 15% staked, a total above 47M leaves the founders over the 30% the bloc rule holds them
under once the distribution is paid. At 45M year one is covered, the founders end at 29% in the worse case and 17% in
the better, and the block falls at 300M paid at the original Terra rate. Every other lever is a vote and needs no
genesis figure: the step gates, 30% of paid distribution bonded, are the convention each tranche vote applies and come
down if members stake less; the reward targets move by policy message; the member amount is set per step (D91). At
15% staked the gates as written never open step two and the bloc rule never pays a seat holder, so a low measured
rate is answered by lowering the gates or raising the targets, never by the total. What it costs: 500k less deferred
pay a seat at ten, a 225,000 double-sign slash rather than 250,000, and the founder's grant re-derives to 19.5M with the
seat, proposed at 90M of public bonded stake. Assembly divides the total by the operators it seats, any base-unit
remainder staying in the pool. The [disbursement plan](../governance/DISBURSEMENT_PLAN.md) and
[launch genesis](../governance/GENESIS.md) carry the figures.

## D95

**Recorded status:** Decided 2026-10-02. Amends the asset registry, Market depth, and conversion-factor rows of
[GENESIS.md](../governance/GENESIS.md) (decided 2026-09-11) and the bootstrap sizing of `base_pool` in [P1](#p1).
Depth sizing added by [D104](#d104). The NOAH leg's placeholder is one NOAH a XDR, not a dollar, under [D128](#d128).

Conversion is closed at launch and opens by vote once NOAH has a market price. The artefact registers no asset; the
eleven feeds, the sidecar's placeholder NOAH leg, and the seeded gas factor stay, and Market's depth is 100,000 XDR
rather than 5M. Governance registers the ten launch currencies in one proposal once the oracle reads a live NOAH
price, after operators have added NOAH venues to their sidecars together and only once a venue has depth. Treasury
refreshes the NOAH gas factor from the reference rate whether or not a currency is registered, so fees track the first
live price block by block rather than jumping at registration. Depth stays a human lever, raised by the Market
committee inside its corridor and by governance beyond it, with the first appointment's corridor reaching about ten
times the genesis depth. Why: every oracle rate is NOAH per unit and its NOAH leg is a configured placeholder, one
NOAH per dollar, until any provider reports a live sample. A stablecoin is a claim on NOAH at the oracle rate, so a
currency minted at the placeholder is re-marked when the live price arrives: at five cents an XDR that cost 1.37 NOAH
redeems for 27, the difference is minted, and every holder pays whoever minted early; the liability and every fund
target are valued at the placeholder until then; and the first live price comes from the first venue to list NOAH, so
whoever controls a thin venue sets the redemption rate of everything minted before it. An empty registry is the closed
position because Market mints only an `ACTIVE` ask and there is no status between registration and admission, and it
is the asset module's own admission cycle, feed first and registration once it prints, applied to the launch set.
Nothing else on day one needs a priced NOAH: gas is seeded, the oracle lane grades FX legs that are real, tax and the
Treasury waterfall key off registered currencies, and the funds hold NOAH at face value; closing conversion makes the
placeholder harmless, so it stays at one dollar and the oracle lane is the only reason to keep it. The depth is set in
the artefact rather than by a later vote because it is inert until registration and sized for members converting
real amounts, about 2k XDR a swap at the floor and 1k a day, small enough that a pushed thin price mints little at the
wrong rate; it never grows on its own, since depth is the throttle on how fast NOAH mints and burns and the 2022 Terra
expansion is the whitepaper's accelerant. Rejected: registering the currencies and relying on the missing rate, which
opens conversion by itself at a thin price the moment any provider samples NOAH, at the launch depth and without a
vote; a low placeholder with conversion open, which bounds the loss without removing it; and `ISSUANCE_HALTED` at
genesis, a wind-down status that admits redemption. What it costs: no stablecoins before NOAH trades, when there is
nowhere to spend them yet; Disbursement's stablecoin compensation waits for registration; the seat floats play no
bootstrap role; and the testnet artefact keeps the registered currencies and the 5M depth, since a testnet exists to
exercise conversion and the e2e suites run on it. The opening sequence is
[governance operations §6](../governance/GOVERNANCE_OPERATIONS.md#6-opening-conversion).

## D96

**Recorded status:** Decided 2026-10-02. Amends the seat float in [D84](#d84) and the pool remainder in [D94](#d94); the remainder is 35M at ten seats under [D111](#d111) and [D115](#d115).

Set the seat float at 50k NOAH, per seat, granted at genesis beside the seat as before. Why: the float's job is the
week before seat income starts, when unvested coins cannot pay fees: gas at a few hundredths of a NOAH a transaction,
proposal deposits of 1,000 and 5,000 refunded in four days unless vetoed, and a committee account's gas, under 10k in
a busy week. 50k covers that five times over and is four and a half months of the income that carries everything
after. 300k was sized for a job that no longer exists: the founders minting the first currencies, and conversion is
closed at launch (D95); above operations the float buys only sellable supply on day one and a larger sum for a
coerced founder to lose. It stays per seat rather than a fixed total because it is an operating cost per
operator, not a share of the vote, and it stays a genesis grant rather than a later award because nothing else could
pay for the proposal that awarded it. What it costs: ten floats are 500k NOAH, a twentieth of a percent of supply,
and the pool keeps 57.5M after ten seats. The [launch genesis](../governance/GENESIS.md) carries the figure.

## D97

**Recorded status:** Decided 2026-10-02. Amends the ownership ceiling in [D90](#d90). Resized to 30M, a sixth at the
20% fallback rate, by [D99](#d99).

Set the ownership ceiling at 20M NOAH, seat included: the founder's whole position, 15M and a 4.5M seat. Why: 60M was
the founder's grant when D86 set it and stayed when D90 cut that grant to 15M, so it matched nothing. The bands top
at 15M, the founder holds 19.5M, and the ten seats together hold 45M, which left one contributor free to out-hold the
founder three times over and the founding set once. The fraction, a fifth of what others have bonded, keeps everyone
under a sixth of the vote at the moment of payment and no longer: payments are permanent and bonded stake is not, so
a person paid 100M against 600M bonded holds a third of a chain whose stake later falls to 200M. The ceiling is the
one bound that survives the stake moving, and the only absolute bound on chain, since the bands are a proposal
convention the module never checks. 20M is the figure the founder's grant was sized on, nobody leaves the pools
heavier than the founder, and the founder's own position fits it exactly. What it costs: at 15% staked it trims a
few million from the largest positions at the very end of the distribution and is otherwise idle; it binds once
bonded stake passes about 120M rather than 360M; and a seat holder's room is the ceiling less the seat, 15.5M at ten
seats, so a seat above 20M, which takes fewer than three seats, leaves its holder no ownership room at all. Deriving the
ceiling at assembly as one launch seat plus 15.5M, so the grant stayed 15M at any count, was considered and closed by
[D98](#d98): it reads the founder's line as a launch seat's share rather than a tenth of the vote. The
[disbursement plan](../governance/DISBURSEMENT_PLAN.md) carries the figures.

## D98

**Recorded status:** Decided 2026-10-02. Amends the founder's line in [D90](#d90) and closes the open item in
[D97](#d97). The tenth is the module's fraction for everyone under [D100](#d100), which binds the founder's grant
rather than the ceiling less the seat ([D101](#d101)). The seat is 5M under [D111](#d111), leaving 25M of room.

Read the founder's line as a tenth of the vote, not a launch seat's share. The founder holds under a tenth of bonded
stake at every payment whatever the seat count, so the grant is at most the ceiling less the seat, 15M at ten seats,
and the proposal that makes it, once the public has bonded 90M, sizes it from the count and the stake at the time.
The ceiling stays a fixed 20M and is not derived at assembly. Why: the two readings agree only at ten seats. A launch
seat's share grows as seats shrink, so under it the founder's grant would grow to 36M at five seats, while a tenth
of the vote is a statement about the chain that stays true whatever the launch review does, and it is what a reader
understands by neutral. The fixed ceiling already tracks the absolute reading, 11M of room at five seats against
11.2M under the line, so nothing in the module changes. What it costs: with fewer than ten seats the founder enters
heavier and is granted less, the whole position staying near 20M; with more seats the grant may exceed 15M only up to
the ceiling's room. The grant is a governance vote years after assembly, so nothing about it is fixed at genesis. The
[disbursement plan](../governance/DISBURSEMENT_PLAN.md) carries the figures.

## D99

**Recorded status:** Decided 2026-10-02. Amends the ceiling in [D97](#d97) and the gates in [D94](#d94) and the
[disbursement plan](../governance/DISBURSEMENT_PLAN.md); confirms the 45M total of D94 and the 15M grant of D90. The
gates are fixed minimums and 15% a scenario the figures survive under [D101](#d101). The total is replaced by a 5M seat
under [D111](#d111); the grant stays 15M.

Size the plan at a 20% staking rate of paid coins: the rate a live chain paying what Ark pays falls back to in a bad
year, with 30%, where the original Terra ran on fee-funded rewards, as the active rate, and 15%, Terra Classic after
its collapse, as what a dead chain shows and not a case to size for. Genesis figures are sized to hold at 20% and
improve on their own at 30%; figures set by vote are set so the plan works at 20%. Three figures follow. The
ownership ceiling is 30M, seat included, a sixth of the bonded stake at 20%, so even in the bad year no two holders
can block; the 20M of D97 was a sixth at a dead-chain rate, and 45M, a sixth at the active rate, would coincide with
the fraction and never bind, since the ceiling earns its place only below the rate the fraction is measured at. The
step gates are 15% of paid distribution bonded, three quarters of the planning rate, 7.5M, 22.5M, 45M and 67.5M for
steps two to five, where the 30% gates never opened step two at 20%; step five now opens before the founders lose the
block rather than after. The founders' total stays 45M and the founder's grant 15M: at 20% the total is pinned from
below by year one, a buyer needs half of it against about 20M of liquid supply, and from above by the bloc rule,
since the founder's 15M pays in full by the end of the distribution at 45M and 12.6M at 50M, and the tenth of the
vote allows 16M, which 15M is the round figure under. Why one rate: a figure sized at 30% has no margin and cannot be
lowered after genesis, while one sized at 20% costs nothing at 30% beyond trimming positions above 30M; the reward
targets, which the committee can double and governance raise further at the subsidy pool's cost, are the margin if
the rate comes in under 20%. What it costs: a contributor may hold half again the founder's position, and at 15%
staked the founders end at 29% and the bloc rule pays no seat holder, which the targets answer.

## D100

**Recorded status:** Decided 2026-10-02. Amends the ownership fraction recorded in [D86](#d86) and [D87](#d87) and
makes the founder's line of [D98](#d98) the rule for everyone.

Set the ownership fraction at a tenth of bonded stake: numerator 1, denominator 9 on the stake the person does not
hold, which is a tenth of the whole and keeps a person's own bonding from raising their own limit. Why: the fifth of
others held every wallet paid from the pools at a sixth of the vote, so blocking took three people, and the founder's
tenth was a sentence in the plan that nothing on chain checked. One fraction at a tenth makes the founder's line the
chain's rule for anyone paid from the pools: blocking takes four, any three at cap stay under the third, and the
founder needs no separate term, since the 15M was sized on exactly this fraction. The 30M ceiling stays, because the
two guard different moments: the fraction holds everyone at a tenth while stake is live, the ceiling at a sixth if
stake later falls. What it costs: contributors are paid more slowly early. The genesis stake admits 5M per person
rather than 9M, and a 10M founding-band grant completes once about 100M is bonded, roughly step three at the planning
rate, where before it paid in full against the genesis stake; accrual is unaffected, so nothing is lost, only delayed,
and the delay falls on the people who built the chain before launch. The
[disbursement plan](../governance/DISBURSEMENT_PLAN.md) carries the figures.

## D101

**Recorded status:** Decided 2026-10-02. Amends the gates and step conditions of the [disbursement
plan](../governance/DISBURSEMENT_PLAN.md) as recorded in [D99](#d99), and wording [D98](#d98) left behind. A step's
contributor budget is a guideline rather than a limit under [D131](#d131). The gates are 3.75M, 18.75M, 41.25M and
63.75M, and public bonded stake excludes the founder's grant and stream, under [D132](#d132).

Make the step gates fixed minimums of public bonded stake, the plan table's 7.5M, 22.5M, 45M and 67.5M, 15% of each
step's full distribution, and make them the only step condition: the previous step's contributor budget need not be
exhausted, its remainder rolls forward with the member remainder, and a contributor over the cap no longer holds the
step. Why: a gate read as 15% of payments was satisfied by the first members' tenths, 3M paid and 600k bonded, and
stayed satisfied while later steps committed grants that pay nothing for a year, so the same 600k could open every
step with the founders holding 98% of bonded stake. A fixed figure makes each step prove new participation, at the
cost of pace: at 20% staked step two waits for about 37M of public payments, roughly two years in, which is the
handover's real speed rather than a new delay. Requiring a step's contributor budget to be fully granted before the
next opened rewarded awarding ownership to unlock a stage, so the bands become ceilings sized to the work and the
unawarded balance rolls forward. A contributor goes over the cap when others unbond, and holding everyone's
distribution then blocks the dilution that would repair it; the per-payment check already does the work. Wording
corrected with it: the other founders' grants share bloc room pro rata by remaining escrow and can delay the
founder's rather than queueing behind it; the seat is a founding allocation with long vesting that a founder who
leaves keeps, not pay for service; members hold the largest share of bonded stake once grants are delegated, about
43% at equal 20% staking, and the majority only with the other contributors, since founders bond everything and
members a fifth; the founder's grant is bounded by the ownership fraction at every payment (D100), not by the ceiling
less the seat; the founder's proposal waits for 91M of public bonded stake, since at the 0.667 threshold the exact
block line is 90.14M; and 15%, Terra Classic today, is a scenario every genesis figure must survive rather than a case
to dismiss, since Ark's rewards, 1.3M NOAH a year across ten seats, do not make 20% a floor. The module changes for
none of this. What it costs: slow member recruitment can delay later contributor rounds, the price of a gate that
measures handover; and a step number again says how far the handover has come, since escrow cannot lower a gate.

## D102

**Recorded status:** Decided 2026-10-03. Settles the "what a member gives in return" item of the
[disbursement plan](../governance/DISBURSEMENT_PLAN.md) and restates what bounds the member grant in [D91](#d91).

Membership obliges nothing: a member holds, stakes, votes, or sells by choice, and the chain needs both choices, since
those who stake carry security and the vote and those who sell are the float. In the first year member payments are
nearly all the liquid NOAH there is, beside the founders' 50k floats and about 1.3M of seat income, so the market
outside the chain that opens pay streams forms from member sellers, and the pace governance opens a member tranche at
is also the pace of the float. The grant is therefore ownership rather than pay, and the joiner and the faker bound it,
not the work: the floor is the cost of joining, an identity check, a voucher, and a wallet, under which it reads as an
airdrop; the ceiling is the price of a bought identity, since with nothing owed a fake does nothing a real seller does
not, so each step's vote holds the tenth, the part no finding recovers, under that price; and the pool over the grant
is the roll. The launch figure stays at 20,000: nothing is priced at launch and the first cohort is founder-vouched, so
it risks least, and the later figures in D91 are illustrations each step's vote applies the rule to. Why: a labour
anchor for the floor, the skilled-year rate or the bounty range, was considered and rejected, since a grant conditioned
on participation pays for votes, which the app's rule of casting none forbids, and a motive test cannot separate a
mercenary from a fake once selling is legitimate; identity and vouch carry the whole guard. What it costs: the member
vote self-selects to those who stay, and to buyers who paid to, which is the handover by market the plan already
accepts; the step-1 cohort holds four times step 5 in NOAH and sells into the thinnest market, the price of having
carried the most risk; and counsel's question narrows to how a grant with nothing owed is characterised.

## D103

**Recorded status:** Decided 2026-10-03. Amends the suspension recovery recorded in [D86](#d86) and [D89](#d89).

Drop the voided-term set and `MsgVoidSuspensions`. A suspension stands until the committee or governance reinstates
it, across a committee replacement or a mandate expiry alike, and it records only the time it was made, which a
later cancellation uses as its cutoff. Why: lifting suspensions as a side effect of a term change, by a void or by
scoping them to the term, made a routine re-appointment drop the outgoing committee's legitimate pauses, and the
explicit lifts already exist; a stolen key's mass suspension costs governance one `MsgReinstateMembers` per hundred
members, the price the key paid to make them. What it costs: recovery from a stolen key is a proposal of three
message kinds, appoint, cancel the term's awards, and reinstate, and the committee term a suspension was made under
is no longer stored, since the actor and height identify it.

## D104

**Recorded status:** Decided 2026-10-04. Adds the sizing rule to the depth lever in [D95](#d95).

Size Market's depth from the conversion it should pass at the spread floor: the larger of a hundred times the daily
net one-way flow and fifty times the largest routine swap, each valued in the pool unit at the oracle rate. The flow
is estimated from distribution, liquid NOAH times its live price times the share expected to convert in a month, over
thirty. The rule is therefore run with a live price before conversion opens, again when a tranche adds liquid NOAH
(D102), and when the pool holds an expansion-side imbalance beyond a hundredth of its depth. A raise is checked
against what it exposes: a mint at a wrong price stays profitable up to roughly the mispricing times the depth, so
while the venues pricing NOAH are thin the raise is cut and the spread carries the excess. Depth is not raised while
the imbalance is on the redemption side, and never by shortening recovery (D95). Why: a swap of `x` from a rested
pool pays `x / (depth + x)`, which reaches the 2% floor at about a fiftieth of the depth; each block returns one part
in the recovery period of the imbalance, so steady one-way flow holds the imbalance at one period's flow, and the
marginal spread, about twice the imbalance over the depth, stays at the floor while a day's flow is within a
hundredth of the depth. Under another policy the factors are two over the floor times the recovery period in days,
and one over the floor. Both earlier figures were sized this way without the rule being written, 5M for founders
minting from their floats and 100k for members converting in the first months (D95). Distribution is the input
because liquid NOAH is all that can be offered to mint; liability is what that flow leaves behind, so it confirms a
sizing and does not produce one. Rejected: a larger depth in the artefact, which serves no holder at opening, scales
what the first thin price can mint, and would be set before the price or the float is known; sizing to swap volume,
which is what depth throttles and part of what Terra's January 2022 raise cited; sizing to liability alone, which
lags the flow and yields no figure; and any automatic adjustment (D95). What it costs: the converting share is a
judgement, not a measurement; flow above the sizing pays more than the floor until someone acts; and no alert reports
the imbalance, so whoever sizes reads `Query/Pool`. The procedure is the
[committee runbook](../governance/ECONOMIC_COMMITTEE_RUNBOOK.md#conversion-depth).

## D105

**Recorded status:** Decided 2026-10-07. Amends the payment path in [D86](#d86) and [D87](#d87), which pulled payments
by an any-sender release and had no block hook pay recipients. Its capped-grant retry is removed by [D121](#d121).
Registration queues a member for the end of its hold under [D126](#d126).

The block pays. Every grant that owes principal and is not suspended carries the time the module next looks at it,
and the end of each block pays the grants whose time has come, oldest first and at most a thousand a block, each what
its schedule has accrued and its caps admit. A paid-in-full grant waits for its next boundary; one a cap held back
waits for the sooner of that boundary and a day. Registration queues the boundary after the tenth, a contributor
award its first boundary, cancellation its retained debt and reinstatement its arrears for the same block, a payee
change the next block's look, and suspension leaves the queue. `MsgRelease` is removed; `Query/Releasable` becomes
`Query/Payable`. A send Bank refuses at the payee is deferred a day or to the next boundary and published, since the
payee is the one input no write can vet against every later restriction; every other failure in the hook is an
accounting invariant and fails the block. Why: a member grant is the handover, and a payment that needs a signature
is a payment some members never collect, a transaction the app has to make for them, and a gas cost on receiving;
with the block paying, every member is paid in full by doing nothing, the app's monthly transaction is a delegation,
and the chain has one fewer any-sender message. The queue is bounded by one entry a grant, so a halt of any length
costs one catch-up payment each, and the per-block bound drains a backlog at a thousand a block. Rejected: lockup
accounts in the payee's own account, which would delegate and vote unpaid principal against D86 and offer no
clawback for suspension or cancellation; a retry every block for capped grants, which would churn the state for a
founder's grant held years under the ceiling; and keeping the release message as an escape hatch, which keeps the
surface the decision removes for at most a day's wait on a capped grant. What it costs: Disbursement gains a block
hook, which the write-time refusals and the deferral keep from halting; a capped ownership grant waits up to a day
after room opens; and a cancellation now pays its retained debt in the same block rather than leaving it to a later
claim.

## D106

**Recorded status:** Decided 2026-10-07. Amends the bloc rule in [D90](#d90), restated in [D101](#d101). The committee's seat refusal it keeps is removed by [D108](#d108).

Remove the founder bloc rule. An ownership payment to a seat holder is bound by the record cap alone, seat included,
and nothing else distinguishes it: the module keeps no founder totals and the beneficiary query reports the record's own limit, shared with no one.
Why: the rule bound the ten to each other, one founder's grant paying less because another's was paid, which assumes
the founding seats act in unison. They are independent validators who may vote in different directions, and the chain
cannot tell a coalition from ten strangers, so a cap that names one by genesis identity is not neutral: the plan's
working limit, that blocking takes four wallets at a tenth each, is accepted for every other set of validators, and
the ten were held to a stricter standard for who they are rather than what they hold. What the rule protected was the
launch window, when the ten hold the whole vote and could grant themselves; that stays policy, which it already was:
the founder's grant is proposed once public bonded stake passes 91M, the grantee abstains, and the cliff is in the
schedule. Rejected: dropping only the pro-rata share, which keeps the group assumption; a per-seat fraction, which
keeps it and pays the founder's grant at 650M of bonded stake rather than 195M. What it costs: the ten together are
bounded by what the public votes them and by ten ceilings, not by a group rule; the founder's grant is paid in full at
195M of bonded stake rather than 200M; and the committee's refusal to compensate a seat holder stays, since it is
about which path decides founder pay, not about a bloc.

## D107

**Recorded status:** Decided 2026-10-07. Amends the member account's deposit rule in [D88](#d88).

The member pool accepts NOAH deposits from anyone. The account leaves Bank's blocklist; the Disbursement send
restriction, which admits exactly one positive NOAH coin into it on every Bank path, module-to-module included, is the
guard. A deposit joins the pool unopened: it is spendable only through a tranche governance opens, and the account
still has no exit, so it pays members or nobody. Why: the contributor account already accepts deposits in what it may
hold, and a NOAH-only guard that binds every path makes the two accounts symmetric, so a donor or a later proposal
can top up either pool without a module change, while the blocklist guarded only the message paths. What it costs:
"only genesis funds the member pool" is no longer a rule; the pool's size is its genesis figure plus what arrives,
and governance's tranche votes remain the only thing that turns any of it into grants.

## D108

**Recorded status:** Decided 2026-10-07. Amends the committee surface in [D89](#d89) and the seat refusal kept by
[D106](#d106). The per-record compensation cap is [D109](#d109); the single allowance is split by kind in [D110](#d110).
The committee's ownership awards are removed by [D113](#d113); its power to compensate a seat holder stands.

The grants committee awards ownership as well as compensation, and to a founding seat holder as to anyone.
`MsgCommitteeGrant` carries the kind; one allowance per denomination caps the term's awards, ownership under its NOAH
entry beside NOAH compensation, and the record limit binds a committee ownership award at payment as it binds a
governance one. The minimum first period, the hundred-a-term cap, and the term cancel are unchanged and apply to both
kinds. Why: on chain the two labels differ in one thing, ownership counts toward `own` and is capped by the record
limit while compensation is not, so the refusal kept the more bounded label off the committee's menu and nothing
else: a NOAH compensation award is stakeable NOAH under the same four bounds. The seat refusal bound a record, not a
person, and the kickback it named, the appointing ten paying themselves through their delegate, is bounded by the
allowance governance sets and undone by the vote that replaces the committee; a seat holder paid on the seat record
is visible there, where a second address would not be. Rejected: a separate ownership allowance, which needs the kind
in the term index and buys a split the record cap already makes small at the committee's scale; keeping the seat
refusal, which binds the honest path only. What it costs: governance sizes the NOAH allowance as the most the
committee may hand any one record under either label; a seat holder's pay no longer needs a vote, so the plan's
"grantee abstains" check applies to proposals alone; and the committee key carries a third power, recovered by the
same three messages.

## D109

**Recorded status:** Decided 2026-10-07. Bounds the compensation [D89](#d89) delegated and [D108](#d108) widened.
[D129](#d129) sets the window's year to 26 fortnights, 31,449,600 seconds.

Cap compensation per record: one beneficiary record may be awarded at most `compensation_cap`, 1M in the reference unit
at launch, inside a rolling `compensation_window` of a year, on the proposal and committee paths alike. An award is
valued in the reference unit at the fresh oracle rates when it is created, rounded up, and the value is written on the
grant; the check sums the record's compensation grants created inside the window, cancelled ones included, so a cancel
never restores room. Both figures are operational params. Why: compensation had no per-person bound: the term allowance
caps the committee's total and the tranche the pool, and nothing stopped one record taking all of either. The window
sum is the pay analogue of the ownership record limit; it bounds the quantity itself, composes with streams and
bounties, and sits at the write, where a human is in the loop, rather than in the block. The reference unit holds the
figure steady where a NOAH cap would move with NOAH's price, and locking the value at award time keeps a past award's
counted size from moving afterwards. Params rather than genesis policy because pay scale follows the market where
ownership scale follows supply. Rejected: one live award per record, which bounds concurrency rather than quantity and
chains short awards to many times the figure; a per-award cap alone, which overlapping awards defeat; revaluing past
awards at current rates, which reintroduces the volatility; a window accumulator in state, which the grants already
hold. What it costs: a compensation award needs a fresh rate for its denomination and the reference unit, so none can
be awarded before the first reference observation and a stale feed refuses pay until it recovers; a pushed stablecoin
rate changes how much of the allowance one record can take, within the oracle's own bounds; a record at its cap waits
for the window to roll or a params vote; and a record with many grants pays a longer walk at award time.

## D110

**Recorded status:** Decided 2026-10-07. Replaces the per-denomination term allowance of [D89](#d89), shared across
kinds by [D108](#d108). The ownership allowance is removed by [D113](#d113); the compensation allowance stands.

Split the grants committee's term allowance by kind: an ownership allowance in NOAH and a compensation allowance in the
reference unit, each one figure; the denomination list goes. An ownership award charges its NOAH amount; a compensation
award in any active asset, NOAH included, charges the reference value D109 writes on the grant, so the check reads no
rate the award did not already need. Why: ownership is a share of supply and compensation is pay, and D109 settled the
unit for pay the day D108 shared one NOAH figure between them, a figure that floated with NOAH's price and that
governance had to size as the most one record could take under either label. Rejected: one figure in NOAH, which
charges nothing for an award custody already holds; one figure in the reference unit, which puts ownership awards
behind the feed and shrinks their budget when NOAH rises; keeping the denomination list, since D109 already needs a
live reference rate for every compensation award, so restricting pay to NOAH bounded nothing. What it costs: the live
term's usage is two figures the module keeps and genesis carries, zeroed at appointment as Claims zeroes its allowance
and checked against the term's grants at import; there is no per-asset bound on what the committee converts through
Market, which the compensation figure bounds in value; and a NOAH award's label picks its allowance as it already picks
its per-record cap.

## D111

**Recorded status:** Decided 2026-10-07. Amends the seat sizing in [D94](#d94), restoring the per-seat grant of
[D84](#d84) at 5M; the float stays 50k under [D96](#d96), the ceiling 30M under [D99](#d99), and the founder's grant
15M under [D98](#d98).

Size the founding seat per seat: 5M NOAH vesting and a 50k float whatever count the launch review admits, so ten seats
hold 50M, 5% of supply, and the community pool keeps 103M less 5.05M a seat, 52.5M at ten. Why: D94 fixed the total so
the count could not move the handover lines, the early attack cost, the first contributor cap, and the bloc rule's room
without a decision. The bloc rule is gone (D106) and the module keeps no founder total, so the seat is the one founder
figure the chain still reads, and it is pay for a decade of running the chain: what a founder is promised at the review
should not shrink because the review admitted another. The other levers are analysis of what the seats could do
together, not rules, and are stated in the count: the founders pass alone under half the seats' sum and block alone
under twice it, 25M and 100M at ten, with the founder's grant proposed at 101M. 5M is the top of the band at the 20%
planning rate, not its floor: with the founder's grant the ten end at 31% of bonded stake, where 6M ends them over a
third and the handover reverses when the grant lands, and above about 7M the block line sits past the whole
distribution. The grant stays 15M: the tenth of the vote allows a ninth of public bonded stake whatever the seat, paid
in full at 135M, and the seat leaves 25M of room under the ceiling, paid by the same path as any contributor's. What it
costs: the year-one floor, that a buyer cannot reach a third against about 20M liquid, needs eight seats or more, a
condition on the launch review rather than a genesis guarantee; the step gates, fixed minimums by vote, no longer sit
on the lines, the step 3 gate opening 2.5M before the pass line at ten; the pool keeps 5M less at ten; a double-sign
takes 250,000. The [disbursement plan](../governance/DISBURSEMENT_PLAN.md) and
[launch genesis](../governance/GENESIS.md) carry the figures.

## D112

**Recorded status:** Decided 2026-10-07. Amends the founder's grant and its proposal line in [D90](#d90), [D98](#d98)
and [D106](#d106), the seat derivation and the ten-seat assumption of [D111](#d111), and the goal's one-party line in
the [disbursement plan](../governance/DISBURSEMENT_PLAN.md). The founder's excess over one seat is drawn from the
community pool, like every seat, under [D115](#d115).

Size the founder's position per seat, at genesis. Every seat stays 5M, and the founder's seat is half the other seats'
sum, a third of the founding stake at any count, 22.5M at ten: one continuous vesting account on the seat schedule, the
whole position on the founder's beneficiary record so the record cap counts it, the excess over one seat drawn from the
contributor pool, 275.3M left at ten, and the community pool unchanged at 103M less 5.05M a seat. The founder's gentx
self-delegates one seat, and the founder delegates the rest across the other seats in the first blocks, so no node
holds the third and four nodes still halt. The 15M founding-band grant and its 101M proposal line go; the band table
stays for other contributors. Launch assembly seats seven to thirteen: under seven the founding stake falls under the
40M a buyer of the first year's liquid NOAH could take a third against, and over thirteen the founder's seat passes the
30M ceiling. Why: the ten-seat assumption and the unison assumption fell together. With independent seats the founder
has neither assurance of a grant voted years later by strangers nor weight in the launch electorate, so the founding
allocation becomes genesis state sized by the threshold rather than a band grant; "a third" is a share, so its NOAH
amount following the count is the rule, while every other seat stays the absolute promise of D111. The third is the
veto, chosen on purpose: at genesis the founder blocks alone and the other seats together cannot pass over an active
no, since two thirds exactly is under the strict 66.7%; the founder passes nothing alone, a third being under quorum;
passing takes the founder plus half the other seats, and blocking the founder alone or half the others without. It
dissolves on a per-seat schedule that does not depend on the count: the solo veto at about 0.3% of the founder's seat
of public bonded stake, roughly 68k NOAH at ten, and the founder plus m seats at 15M times m. Spreading the delegations
removes the accidental halt; a deliberate one stays one redelegation away, inherent to a third. Rejected: sizing every
seat from a fixed total, which reverses D111 for D111's reason and makes the dissolution schedule move with the review;
a seat that grows below seven, since a six-seat launch is a decision rather than a silent adjustment; the largest
position under a third, which gives up the veto for one base unit. What it costs: the year-one floor becomes a count
rule at seven rather than eight; the founder is the largest wallet until bonded stake passes ten times the seat, 225M
at ten, just past the end of the distribution at the planning rate; the goal paragraph excepts the founder at
genesis; the 25M and 100M handover lines become the dissolution table; and the halt key is removed by a practice
genesis cannot hold, since validators exist only once the gentxs execute. The
[disbursement plan](../governance/DISBURSEMENT_PLAN.md) and [launch genesis](../governance/GENESIS.md) carry the
figures.

## D113

**Recorded status:** Decided 2026-10-07. Amends the committee's award kinds of [D108](#d108) and removes the ownership
allowance of [D110](#d110); the compensation allowance, the per-record cap of [D109](#d109), and the committee's power
to compensate a seat holder stand. Amended 2026-10-08: a member registration carries its registering term too, so the
term query lists a stolen key's registrations beside its awards; ownership still carries none, and the mandate query's
award count is removed. Its month-long first period became the fortnight compensation period, and its reserved
field numbers were dropped, under [D116](#d116); [D89](#d89)'s 2026-10-08 amendment removes the hundred-a-term limit.

Take ownership off the grants committee's menu. Every committee award is compensation, NOAH or an active asset,
charged at its reference value against the term's compensation allowance, under the 1M-a-year record cap, the
month-long first period, the hundred a term, and the term cancel; the award message carries no kind, the mandate no
ownership allowance, and the module one usage figure. Ownership is a proposal only, in a review round with the
reasoning in the summary and the grantee abstaining when the grantee votes. Why: a contributor grant is a decision
about who owns the chain, argued in public, and the committee is a body the launch set appoints. D108 put ownership on
the menu because a NOAH compensation award is stakeable NOAH too, so the kinds differ only in the record cap; that is
true of the coins and false of the scale. Under the compensation label the committee's output to one record is bounded
at pay scale, 1M in the reference unit a year; under the ownership label it was bounded by the record cap, 7.5M at the
founding stake and a tenth of bonded stake as it grows, within an allowance governance sized, an order of magnitude
more and exercised without a vote per grant. The month-long first period and the term cancel bound a stolen key, not
an appointed committee's judgment about ownership. What it costs: a hire's ownership grant needs a proposal while its
pay stream can still come from the committee, the shape the plan's hire package already had; and the retired field
numbers, 5 on the appointment message, 2 on the mandate, 3 on the award message and the mandate query, 12 on genesis,
stay reserved. The [disbursement plan](../governance/DISBURSEMENT_PLAN.md) and
[module README](../../x/disbursement/README.md) carry the surface.

## D114

**Recorded status:** Decided 2026-10-07. Amends the seat vesting window of [D84](#d84), restated in [D111](#d111) and
[D112](#d112). The step-5 gate is 63.75M under [D132](#d132), a little under the seats.

Vest every founding seat, the founder's included, behind a four-year cliff and over a four-year tail, fully vested at
year eight rather than ten. Why: vesting gates transfer alone, since unvested seats delegate and vote from block one, so
the schedule decides when founding stake can be sold and nothing in the handover arithmetic. The cliff is doing the
protecting and stays: it covers the launch window, when the founding set is the electorate deciding every grant and the
founder's coalitions still block (D112), and it keeps the floats the only liquid NOAH a founder holds while the first
member cohorts are the whole float. The tail paces the exit after that, and the decade framing it carried went with
D101, which made the seat an allocation a leaver keeps rather than pay for ten years of service. Sized against the
member flow, roughly 30M to 45M a year through the middle of the distribution at the planning pace: a one-year tail
unlocks the whole founding stake inside year five, above that year's member flow and with the founder's seat still
about a fifth of the vote; two years leaves founders about half of what is sold; three about even; four keeps them a
minority, 16.9M a year at ten seats, and finishes about when public bonded stake passes the seats at the step-5 gate,
so the founding stake is fully for sale only once the public outweighs it and a buyer assembling a third faces the open
market rather than a few known sellers. Rejected: the one-plus-three norm, sized for teams who are not the electorate;
tails of one to three years, for the reasons above; shortening the cliff. What it costs: founder unlocks a year rise by
half, 5.6M for the founder's seat at ten; the founding stake is fully for sale two years sooner, the founder's seat
near an eighth of the vote then rather than a tenth; and the signal is a little weaker, though four years locked and
four more vesting is still double the norm on both counts. One constant in `pkg/chain/launch.go` carries it; the
module reads no schedule. The [disbursement plan](../governance/DISBURSEMENT_PLAN.md) and
[launch genesis](../governance/GENESIS.md) carry the figures.

## D115

**Recorded status:** Decided 2026-10-07. Replaces the ownership fraction of [D86](#d86), [D87](#d87) and [D100](#d100),
the ceiling of [D97](#d97) and [D99](#d99), the seat inside own of [D106](#d106), and the ceiling-derived seat bound and
the founder's grant reading of [D112](#d112); restores the founder's 15M founding-band grant of [D90](#d90) beside the
seat. The cap is checked at award by [D121](#d121); the figure stands. The bands it was the top of are removed by
[D127](#d127): the cap is the founder's grant and the one sizing figure. The grant is written at genesis rather than
proposed under [D136](#d136).

Bound ownership with one figure: `ownership_cap`, a Disbursement parameter governance sets, 15M NOAH at launch, the most
one beneficiary record is paid in ownership from the pools over every grant. A payment is the smaller of accrued unpaid
entitlement and the cap less what the record has been paid. The module reads neither bonded stake nor the seat: the
staking dependency, the fraction, the ceiling, and the live limit go, and the beneficiary query reports the allowance
alone. The seat stays on the record as a record nothing reads. Why: the fraction bounded a record's share of the vote
by assuming the record staked everything it was paid, the one assumption the module cannot replace with a measurement,
and against a public that sells most of what it is paid the fraction held contributors to a tenth of a small vote while
the founding band promised figures it would not pay for years. With the founder's seat a third of the founding stake,
any fraction up to a third left the founder's record no room at all, so the founder's 15M grant for the work before
launch could only exist if the seat left the fraction's base, and a fraction that excludes the seat bounds nothing a seat
holder does. One figure says what the plan means: nobody is paid more ownership than the founding band's top, which is
the founder's grant, so the founder sits at the edge and no record leaves the pools heavier. The seat is consensus
weight, from the community pool like every seat and vesting on the seat schedule; the 15M is pay for work, from the
contributor pool, proposed in the first review round with the founder abstaining, on the contributor schedule, and
paid as the cap admits like anyone's. What the figure means for the vote is stated rather than enforced: at the founding
stake a record at the cap is about 18% of the vote, two are 31%, and three block; at 150M of bonded stake a record is a
tenth; at the planning-rate end about 7%. The founder's seat plus grant blocks alone while public bonded stake is under
twice what the grant has paid, 30M once it is paid in full, so the goal's "nothing restores" becomes this schedule.
Rejected: a fraction with the seat outside it, which bounds pool payments but not a seat holder's vote; a fraction with
the seat inside it, which pays the founder nothing in any projected case; a discount on own for an assumed selling
rate, which is a projection in a halt-class rule and equivalent to a larger fraction for the one holder who does not
sell; removing the seat from the record, kept as a record. What it costs: the cap no longer tracks the vote, so a
sell-heavy chain has records at the cap that are a larger share of a small vote, and the working limit of four wallets
to block becomes three at the founding stake; governance can raise the cap by a parameter vote, bounded only by the
positive 128-bit check, which the launch electorate could do for itself; and the seat-count bound at thirteen stands on
the founder's seat reaching 3% of supply rather than on a ceiling. The
[disbursement plan](../governance/DISBURSEMENT_PLAN.md), [launch genesis](../governance/GENESIS.md), and the module
README carry the figure and the formula.

## D116

**Recorded status:** Decided 2026-10-07. Amends the contributor schedule of the [disbursement
plan](../governance/DISBURSEMENT_PLAN.md), four years with a one-year cliff then monthly, the pay-stream shape of
twenty-four monthly periods, and the committee's first-period rule of [D89](#d89) and [D92](#d92), which the period
itself replaces. A compensation clock's reach is a quarter by [D122](#d122). Every first period, a member's hold and the
compensation period included, is at least two weeks under [D126](#d126). The band's timing multiplier it leaves the past
to is removed by [D127](#d127); a grant's amount credits past work. The ownership cliff is 26 fortnights and the
ten-year bound 260 fortnights under [D130](#d130).

Fix each grant kind's shape as a module parameter and take the schedule out of the messages. Ownership is a one-year
cliff paying a quarter, weighted as the 26 fortnights it spans, then biweekly for three years, 79 payments. Compensation
is a count of fortnights the award sets, in equal shares, one for a bounty, together at most ten years at any cadence.
Members take the same shape, a first period paid at registration then twelve monthly periods, so both are one
`Schedule` of five integers: the first period's length and weight, each further period's length and weight, and the
count of further periods. The grant stores that shape rather than an expanded period list, the `Period` type is gone,
and accrual is one division rather than a walk, so its cost does not grow with the schedule. The grantor sets the clock's start, up to a century ahead: an ownership clock no earlier than
its award, since the cliff is the point and the band's timing multiplier credits the past; a compensation clock at
most a compensation window back, by proposal or committee alike, so a stream aligns to the day a hire started. Each
grant records the block time it was awarded in beside its start, and nothing accrues before one period after it,
wherever the clock starts: a backdated award pays its elapsed periods at that first look, a clock already run out is
due then, and a cancel before it returns everything. That period is the window governance has against a stolen
committee key, so the mandate's minimum first period goes and the compensation period is floored at a week; the
compensation window measures from the same time. Why: the messages carried a free
schedule, so every award was a shape decision in a proposal or a committee transaction, and the plan's "one schedule
from approval" was a convention nothing checked; a shape in params is the convention as a rule, and governance moves it
for future grants without touching live ones, which keep their copy. Biweekly is pay's cadence and puts ownership on
the same clock, so the block pays everything fortnightly, and a count of periods rather than years keeps the cadence
out of the count, so a six-month engagement is thirteen fortnights rather than a rounding. A fourteen-day period makes
a 364-day year; the day's drift is accepted over a 14.05-day fortnight. Rejected: a settable count for ownership, since
ownership is the plan's one stated instrument and the cap bounds it; a shape per award kept in a policy table, which is
the free schedule again; bounties as a kind of their own, since a bounty is one period; and a backdated ownership clock, which pays the
cliff at the first look and would let the founding set pass itself grants with clocks already run out. What it costs:
the smallest award is one fortnight out; a proposal that took weeks to pass cannot reach an ownership clock back to the
agreement, which sizing answers; a committee award pays nothing for a fortnight rather than a month;
and ten years is the longest award, so a longer engagement renews. With the schedule list gone the disbursement protos were
also swept: every reserved field and name was dropped and every message renumbered contiguously, which the chain can
afford once before launch and never after. The plan's §4, the module README, and the governance runbook carry the
shapes and the messages.

## D117

**Recorded status:** Decided 2026-10-07. Amends the contributor tranche of [D88](#d88) and the roll-forward wording of
[D101](#d101); the member tranche of [D92](#d92) stands. [D116](#d116)'s renumbering dropped the field numbers it
reserves, and [D89](#d89)'s 2026-10-08 amendment the hundred-a-term limit it cites. A grant past its step's budget no
longer waits for the next step under [D131](#d131).

Drop the contributor tranche. The contributor account's NOAH is one pool: every award draws it at creation,
governance's by proposal and the committee's within its term allowance; a stablecoin award converts its shortfall
from the pool; a cancel returns unearned contributor NOAH to the pool; a return draws the pool. `MsgOpenTranche`
names no account and opens the member pool alone. Why: since [D113](#d113) every draw on the contributor tranche was
either a proposal, which the same electorate opened in the message that needed it, or a committee award already
bounded by the allowance at reference value, the record cap, the hundred a term, and the first-period minimum with
the term cancel. A ceiling governance raises in the message that needs it bounds nothing, and the step accounting it
kept, what the contributor side of a step has distributed, is the reservations and payments the balance query shows.
The member tranche stays because it bounds a key that is not governance. Given up: an oracle-independent NOAH ceiling
over committee awards, since the allowance and the record cap are reference-valued and a pushed rate stretches both;
the first-period minimum and the term cancel are the answer D113 already gives. What it costs: a NOAH deposit into
the contributor account is grantable the block it lands; a step's contributor budget is plan arithmetic rather than a
chain record; and the retired field numbers, 2 on the message and 1 on the event, stay reserved. The
[disbursement plan](../governance/DISBURSEMENT_PLAN.md), [module README](../../x/disbursement/README.md), and
[governance runbook](../governance/GOVERNANCE_OPERATIONS.md#5-grant-tranches) carry the surface.

## D118

**Recorded status:** Decided 2026-10-08. Supersedes [D45](#d45) to [D48](#d48) and [D50](#d50) to [D53](#d53); the
IBC clauses of [D40](#d40) to [D42](#d42) lapse and their Wasm clauses stand.

Remove IBC from the chain: core, both transfer routes, packet forwarding, rate limiting, callbacks, interchain
accounts, general message passing, the 07-Tendermint and 08-Wasm clients, their stores and genesis sections, the
redundant-relay ante decorator, the Treasury tax inputs for `MsgTransfer` and the v2 `MsgSendPacket`, and the
security committee's client recovery. Wasmd stays and keeps ibc-go as a transitive dependency, so the contract
runtime is wired with no channel keepers: a contract's IBC messages and queries are refused with an explicit error,
its IBC entry points are unreachable, and the `ibc2` capability is withheld so a contract requiring it fails at
upload. Why: the hub shipped shut and no counterparty route was planned, while the dormant stack still had to be
carried by the tax calculator, the policy router, the launch artefact, the threat model, and a two-chain e2e tier.
What it costs: interoperability, if ever wanted, starts from a decision and a store migration rather than the
one-switch opening sequence D45 as amended described, which no longer has a switch.

## D119

**Recorded status:** Decided 2026-10-08. Amends the measure of the compensation cap in [D109](#d109); the figure and
the window stand. A backdated award's first window is charged by [D122](#d122).

Charge a compensation award against the record's cap at its yearly rate. An award whose duration is within the window
charges its whole value, so a bounty counts whole in the year it is paid; a longer stream charges its value spread over
its years, `value × window / duration` rounded up as a bound. The check at award sums the charges of every compensation
award to the record that is still running or was made inside the trailing window, cancelled ones included for that
window, and refuses an award that would take the sum over the cap. Why: the cap counted an award's whole value in the
year it was awarded, so once streams could run ten years (D116) it had become a bound on a stream's total, 1M however
long it ran, about 100,000 a year, under the plan's own skilled-year rate; the plan says "1M a year" and means a rate.
Charging by rate makes a ten-year stream at the design rate fit, 131,000 a year, and seven of them beside each other,
while an eighth does not; a bounty is unchanged; and award-cancel-reaward cycling stays bounded, since a cancelled
stream's charge persists for the window after its award. Rejected: a cap scaling with duration, which lets two long
awards in one year take twice the cap; counting a stream's later years only as they arrive, which lets every year's
awards each take the cap. What it costs: a running stream counts against the record for its whole life, so a record on
a long stream has that much less room for bounties each year, and the check reads a stream's duration beside its
value. The module README and the plan carry the rule.

## D120

**Recorded status:** Decided 2026-10-08. Amends [D89](#d89): registration leaves the grants committee; the committee's
compensation power, term, and recovery stand. A stolen registrar's registrations pay nothing for two weeks under
[D126](#d126). Its window is block time under [D133](#d133).

Appoint a registrar for member registration, apart from the grants committee and under it. The registrar is a second
appointment in Disbursement, a bare mandate envelope the grants committee sets under its own term with
`MsgCommitteeRegistrar`, for a height window inside the committee's; the committee may name itself, and each
appointment advances the registrar term. The registrar is the committee's delegate, so a committee appointment,
replacement or disabling, clears it, and governance reaches it only through the committee. `MsgRegisterMembers`, the
one registration message, is the registrar's: it carries the registrar term and draws on the open member tranche, which
stays the only bound on registration. A member grant records the registrar term and a compensation award the
committee's, and the grants query lists each by its own term. Why: at a hundred addresses a batch, fifty thousand
members is at least five hundred committee transactions, each a multisig signing ceremony, and the member platform
registers as admissions clear, not on a signers' calendar; D89 weighed one key process for compensation and
registration and never weighed a multisig against five hundred batches. What it costs: the registrar is the hottest key
on the chain, a single key on the platform's signing service, and a stolen one pays out the tenths of whatever
governance has opened before one committee transaction advances its term, so the standing rule that governance opens a
step in slices sized to the pace it wants now sizes them to that exposure too; and each committee appointment is
followed by one committee transaction naming the registrar again. The committee stops a stolen registrar in one
transaction rather than two votes, the proposal that replaces a stolen committee clears its registrar in the same write
with nothing more to carry, and governance cancels the fakes from the old registrar term's list. Rejected: a governance
message setting the registrar beside the committee's, which made the registrar free-standing and needed its own place
in every recovery proposal, when one appointer makes the delegate that appointer's term-scoped state; a per-term
registration allowance the appointer sets, which would let the committee meter a step opened whole but adds a usage
figure, a refresh cadence, and a second bound to reason about when the tranche already is one and sizing it is
governance's job; an `x/authz` grant from the committee, which outlives the term and lets one key act under a shape
recorded for a quorum. `MsgRegisterMembers` rides the normal lane: the lane is the property of being a committee
message, registration is routine traffic rather than incident response, and the appointment, `MsgCommitteeRegistrar`,
is the committee message that rides it. It is the first appointment a committee may make; the
[mandate package README](../../pkg/mandate/README.md) carries the departure, the
[module README](../../x/disbursement/README.md), the [plan](../governance/DISBURSEMENT_PLAN.md), and the
[governance runbook](../governance/GOVERNANCE_OPERATIONS.md#5-grant-tranches) the surface.

## D121

**Recorded status:** Decided 2026-10-09. Amends the timing of [D115](#d115) and removes the capped-grant retry of
[D105](#d105).

Check the ownership cap when a grant is awarded, not when it pays. A record's committed ownership is what its ownership
grants have paid or still owe, kept on the beneficiary record as `ownership_committed`; an award that takes it past
`ownership_cap` is refused, and every ownership grant then pays in full on its schedule. A cancel drops the grant's
unearned principal from the figure, so a grant cancelled inside its one-year cliff frees all its room and one cancelled
later keeps what it earned committed. A cap vote changes what later awards admit and leaves existing grants on their
terms; governance stops a grant by cancelling it. Why: D115 fixed the figure but kept the timing of the live stake
limit it replaced, which could only be measured at payment. With a fixed figure a record's room only shrinks unless
governance raises the cap, so whether a grant will be paid in full is known at award; checking later let a proposal
award what the module would never pay, left the excess reserved in the contributor pool, and looked at a capped grant
every day indefinitely. Award time also matches the compensation cap and the rule that a parameter change leaves
existing grants on their terms. Rejected: counting a cancelled grant's whole principal, which would make a mistaken
award spend the record's room until a cap vote, when ownership is governance's alone (D113) and the award-cancel cycle
the compensation cap guards against has no committee to run it; walking the record's grants at each award as the
compensation cap does, which a running total the award and the cancel both touch makes unnecessary. What it costs: a
lowered cap no longer stops grants already made, so governance cancels them; a raised cap resumes nothing, since
nothing is held; and with the partial payment gone, a payment Bank refuses is the only one that waits a retry. The
[module README](../../x/disbursement/README.md) and the [plan](../governance/DISBURSEMENT_PLAN.md) carry the rule.

## D122

**Recorded status:** Decided 2026-10-09. Amends the compensation clock's reach in [D116](#d116), a compensation window
back, and the charge of [D119](#d119) in an award's first window; the cap's figure and window stand. The quarter is 13
weeks under [D130](#d130).

Charge a backdated compensation award, in the window after it is made, what it pays there, and let a compensation
clock start at most a quarter before its award. In that first window an award charges its value spread over a window
plus its backdate, `value × min(duration, window + backdate) / duration` rounded up, where the backdate is how far its
clock starts before the award; after it, a stream charges `value × window / duration` while it runs, as D119 set. An
award whose clock starts at or after the award charges as before. The reach is `MaxCompensationBackdate`, three plan
months, a constant checked when the award is made, by proposal or committee alike. Why: D119 spread an award's value
over its whole duration, the backdated part included, but counted the award only from when it was made, while the
first look pays the backdated periods at once, so a record was paid faster than it was charged. A stream two windows
long and backdated one paid its whole value inside the window after its award while charging half of it, and once it
had ended and aged out an identical award fit again, so one record drew twice the cap every window on ordinary awards,
the committee's included, with no cancel; shorter and longer streams drew less. Charging the first window what it pays
closes that at any reach. The quarter follows practice: vesting commonly starts at a hire date weeks or months before
the board approves the grant, and back pay covers the gap, while a year of reach was longer than practice. Rejected:
no backdating, which drops paying for work a proposal took weeks to approve; the shorter reach alone, which still let a
record draw a quarter more than the cap a window at launch; a parameter for the reach, which a params vote could
widen; and checking the reach in grant validation, which every payment runs, so a later binary lowering the constant
would fail stored grants in the block. What it costs: a backdated award uses more of the record's room in its first
window, so a design-rate ten-year stream a quarter back counts an eighth of its value there rather than a tenth, and
seven such streams no longer fit in one window; and arrears older than a quarter need a separate award, which counts
in the window it is made. The [module README](../../x/disbursement/README.md) and the
[plan](../governance/DISBURSEMENT_PLAN.md) carry the rule.

## D123

**Recorded status:** Decided 2026-10-10. Extends the reference re-point of [D23](#d23) and [D76](#d76) to
Disbursement's compensation figures; one consumer contract replaces the per-module executors.

Rebase Disbursement's compensation figures with the protocol reference: the cap, every compensation award's value, the
live term's usage, and its allowance, in the same `MsgSetReferenceDenom` transaction as Market's pool and Treasury's
reference state, or fail whole. All four are quantities, so each converts as `Convert(from → to)` and rounds up as the
award-time valuation does; the usage is re-derived as the sum of the live term's rebased awards rather than converted,
so the import equality between the two holds exactly; the allowance is then raised to the usage where the per-award
ceilings outran its own ceiling, by under one base unit an award. A figure past the module's 128-bit amount bound
refuses the re-point at the write rather than the next export. Oracle dispatches through one `ReferenceRebaser`
contract and a variadic registration in the app, mirroring the feed-removal guards, so a new consumer is one method and
one name on one line. Why: the figures were bare Ints in whatever unit the reference named when they were written, so
a re-point left new awards, valued in the new unit, charged against old-unit records and bounds, loosening or
tightening the cap and the allowance by the cross rate; and each consumer had its own interface, keeper field, setter
slot, nil check, call, and mock in Oracle, seven places for a third consumer to touch. Rejected: stranding the
allowance until reappointment as Market strands its corridor, since the allowance is a value budget whose meaning
survives the move, not a depth corridor governance should re-size, and stranding would force a grants-committee
reappointment into every re-point; a stored unit on Disbursement's params, which is another guarded copy of the
reference; floor rounding of the charges, which can take a dust award to the zero a compensation value may not hold;
and depinject self-registration of consumers, which removes the app line at the cost of a second wiring convention
beside the hand-wired reverse dependencies. What it costs: a consumer left off the wiring line rebases nothing and
fails nothing, where a missing executor failed loudly before, so the real-app re-point tests require every consumer's
event; and the clamp lets a term's charges stand a few base units past a strict conversion of its allowance.

## D124

**Recorded status:** Decided 2026-10-10. Restates the per-seat income in [launch genesis](../governance/GENESIS.md)
under the founder's seat of [D112](#d112). At NOAH's opening price of one XDR ([D128](#d128)) a regular seat's income is
about 11,300 USD a month.

Keep the reward targets at one share a validator, the founder's included, and state what each seat earns. Assembly adds
0.0175 and 0.0075 NOAH a block a seat, sized to fund one highly available operation, but both lanes pay by bonded
stake: Distribution by power, the Oracle by power times rewarded targets. With the founder's seat at half the other
seats' sum, the pot that paid ten equal seats 131,400 NOAH a year each pays the founder's 22.5M a third of it: at ten
seats each other seat earns about 99,000 with its commission on the founder's delegation, and the founder about
421,000. Why: the targets fund operations, and the founder runs one node. Rejected: weighting a seat's share by its
stake, 13.5 shares at ten, which restores 131,400 to every other seat but raises the pot 35%, shortens the subsidy's
runway by a quarter, and pays the founder about 568,000 a year of subsidy for weight the plan calls consensus, not pay.
No rule restores the other seats without paying the founder more, since bonded stake earns pro rata. What it costs:
each other operator runs on about three quarters of the sized budget, about 8,300 USD a month at the opening price, and
the economic committee's corridor, up to twice the launch targets, is the margin if that proves short. The
[launch genesis](../governance/GENESIS.md) and the [disbursement plan](../governance/DISBURSEMENT_PLAN.md) carry the
figures.

## D125

**Recorded status:** Decided 2026-10-10. Amends the subsidy seed of [P1](#p1) and the pool sizes of [D88](#d88).

Seed the subsidy pool with 50M NOAH rather than 100M and move the other 50M into the disbursement pools at 60/40: the
member pool holds 469.2M and the contributor pool 312.8M, 782M in all. Why: the seed was argued from fifteen and a
hundred validators, but the targets follow the founding seats, not the set, so at ten seats 100M lasted 76 years, past
any horizon a bootstrap serves. 50M runs 38 years at the launch targets, 19 at the economic committee's doubled
ceiling, and about 13 if governance funds thirty validators at the seat rate; a shortfall is refilled by any NOAH
deposit, a community-pool spend included ([D25](#d25)), and an excess returns by `MsgReturnSubsidy` ([D83](#d83)). The
excess goes to the pools because the subsidy pool is restricted money and the launch electorate is the founders: the
community pool would make it spendable on anything by that electorate, while the member pool, which has no exit, hands
it to members. Splitting it 60/40 keeps the split the plan argues from; steps 1 to 4 and every gate are unchanged, and
step 5 grows to 782M, about 66,300 members at its amount. What it costs: half the runway at every target level, so the
plan's first lever, raising the targets to draw public stake, spends a smaller pool; members' share of supply rises
from 43.9% to 46.9%, and the founding set's is unchanged. The [launch genesis](../governance/GENESIS.md), the
[disbursement plan](../governance/DISBURSEMENT_PLAN.md), and the whitepaper carry the figures.

## D126

**Recorded status:** Decided 2026-10-10. Amends the immediate first member payment of [D86](#d86) and [D87](#d87), the
member shape and the one-week compensation floor of [D116](#d116), and the registrar exposure of [D92](#d92) and
[D120](#d120). The member balance pays in thirteen four-week periods under [D130](#d130).

Make two weeks the floor on every grant's first period, so nothing pays before two weeks after any award: a member's
first payment waits out a two-week hold, `member_schedule.first_length` 1,209,600 seconds at launch, and pays from the
block when it ends like every other grant's first period, so registration pays nothing; the compensation period's floor
rises from a week to two weeks, its launch value already; and the ownership cliff, a year at launch, may never fall
under it. `MinFirstPeriod` is checked on every grant and every shape in params, so it is an invariant of stored state,
not a launch setting. Governance opens each step's member budget in quarters, twenty tranche votes over the
distribution: 7.5M at step 1, then 15M, 22.5M, 22.5M, and 49.8M. Why: the open tranche is the registrar's only bound,
and registration paid the tenth at once, so a stolen key took a tenth of whatever was open within minutes, and the only
defence was tranches small enough to lose, at a vote each. With the hold the committee suspends what a stolen key
registered inside it, rotates the key, and governance cancels the grants whole, so the tranche bounds what a thief
commits rather than what it takes; a stolen committee key's registrar is answered by the replacement vote, two days and
an expedited day, inside the same fortnight. Every other grant already paid nothing before one period after its award
([D116](#d116)); one floor for every kind makes that window a property of the module rather than of each parameter. With
the hold the tranche is a backstop behind the daily check of registrations against admissions, so its size is set by the
vote load it costs: a quarter of a step keeps what an undetected theft could take to a tenth of the quarter, 750k at
step 1, at about four votes a year at the plan's pace. Rejected: tranches sized to an accepted loss alone, about a
hundred votes over the distribution at 5M each, each needing half the bonded stake to turn out; monthly tranches, about
sixty votes over the distribution, which train validators to pass them unread; a one-week hold, which leaves a stolen
committee key's registrar under a week of slack after two votes; a hold without a floor, which governance could set to a
second; a rolling registration limit, which [D92](#d92) removed. What it costs: a member waits two weeks for the first
coins and cannot pay gas until then, since the module grants no fee allowance, and the member year runs two weeks
longer; the testnet cannot compress the hold, so neither it nor the end-to-end suites see a member paid, and in-process
tests carry that path; raising the floor in a later binary would refuse stored grants inside the block, so a raise needs
a migration, while lowering it is always safe; and the hold protects only if someone checks the chain's registrations
against the platform's admission records inside it, which each batch's reference makes possible. Sybils the member
process admits are unaffected: their tenth pays after the hold unless found inside it. The [disbursement
plan](../governance/DISBURSEMENT_PLAN.md), the [module README](../../x/disbursement/README.md), and the [governance
runbook](../governance/GOVERNANCE_OPERATIONS.md#5-grant-tranches) carry the floor, the hold, and the tranche sizing.

## D127

**Recorded status:** Decided 2026-10-10. Removes the contributor bands and their multipliers from the [disbursement
plan](../governance/DISBURSEMENT_PLAN.md); the cap of [D115](#d115) stands as a parameter.

Size contributor ownership grants by a written case under the cap, with no scale. The founder's 15M grant for the work
before launch is `ownership_cap`, so no record is paid more ownership than the founder, and every grant under it is
argued case by case: its proposal summary states the work, its period, and why this amount, against the founder's grant
and the grants already made, and the grantee abstains. Earlier and riskier work may earn more than the same work later,
said in the amount rather than through a multiplier. Why: the bands' ranges were invented, a fivefold spread in the core
band and a timing multiplier of two to three that nothing measures, so they lent a rigour they did not have; a band
anchors every case to its top, and the founding band ended at the founder's own grant, which read as a rubric built back
from one number; and the chain checks none of it, so the precedent the first review round sets was always the real
scale. While the founding set votes its own members' grants, the written case is what carries legitimacy. Considered and
not taken: a ceiling on `ownership_cap` in params validation at the founder's grant, which would make "never more than
the founder" a rule no vote could lift; the cap stays a parameter governance sets ([D115](#d115)). What it costs: no
number to place a grant against before the first round, so that round's cases carry the whole weight of precedent; and
"never more than the founder" holds while governance keeps the cap at 15M, binds a record rather than a person, and
leaves compensation outside it, at most 1M in the reference unit a year a record. The [disbursement
plan](../governance/DISBURSEMENT_PLAN.md) and the [governance
runbook](../governance/GOVERNANCE_OPERATIONS.md#5-grant-tranches) carry the case requirement.

## D128

**Recorded status:** Decided 2026-10-10. Amends the opening-price placeholder of [D95](#d95), one NOAH per dollar, and
the dollar figures [D124](#d124) states at it.

Open NOAH at one XDR rather than one dollar. `pkg/chain/launch.go` holds the bootstrap pair, one NOAH a XDR and the
1.371 dollar/XDR cross; Treasury's genesis gas factor is one NOAH per XDR, and the sidecar's `USD/NOAH` bootstrap leg is
the same price in dollars, 0.729394602479941648 NOAH per dollar, which times the cross rounds up to exactly one. Why:
NOAH is the neutral unit every currency is a claim on, and the protocol references XDR, so its opening price belongs in
XDR rather than in one member currency; it also makes the reference-unit figures read as NOAH at the opening price, so
the compensation cap's 1M a year is 1M NOAH. Rejected: an exact anchor that routes every default feed through XDR/USD to
an XDR/NOAH bootstrap, which keeps NOAH at exactly one XDR until it trades but makes all eleven rates depend on the
XDR/USD sample, where today only the XDR feed does; and the resolver averages every route that resolves, so the anchor
cannot sit beside the dollar route as a fallback. What it costs: until NOAH trades, the bootstrap is a fixed dollar leg,
so NOAH's XDR price drifts with XDR/USD by a few percent, and fees and the valuation of any NOAH compensation award
drift with it; every dollar figure rises by the cross, a seat's target to about 15,000 USD a month, a regular seat's
income to about 11,300, the deposits to about 1,400 and 6,900 USD, and a member grant to about 27,400; and gas costs
about a quarter less NOAH, 0.02 rather than 0.027 a transfer, since its price is set in XDR. Conversion stays closed at
launch ([D95](#d95)), so no currency is issued at the bootstrap price. The [launch genesis](../governance/GENESIS.md),
the whitepaper, and the [price-feed runbook](../operations/PRICEFEED_OPERATIONS.md) carry the price.

## D129

**Recorded status:** Decided 2026-10-10. Adds the founder's compensation stream beside the grant of [D115](#d115), sets
the compensation window to 26 fortnights, and drops the plan's rule that pay streams wait for a NOAH market. The stream
is written at genesis rather than proposed under [D136](#d136).

Pay the founder's work after launch by a compensation stream at the cap: 10M NOAH over 260 fortnights, ten years, 1M a
year, a governance `MsgCompensate` in the first review round beside the 15M grant, the founder abstaining. The figure is
what the cap, 1M in the reference unit a window, admits one record over the longest award at NOAH's opening price of one
XDR ([D128](#d128)), and it is fixed in NOAH: never more, and less if the cap admits less when it is awarded, as it will
if XDR has weakened against the dollar since the bootstrap cross. It pays for the founder's work, never the hours seat
income pays, and fills the founder's compensation cap for its ten years. `compensation_window` becomes 26 fortnights,
31,449,600 seconds, from twelve plan months, so the cap's year is the pay cadence's: the longest stream then charges
exactly a tenth of its value a window, where a 365-day window against a 364-day fortnight year capped it at 9.97M. Pay
streams no longer wait for a NOAH market outside the chain: a hire's stream pays in NOAH from launch, and taking pay
that cannot yet be converted is the hire's choice; stablecoin pay still waits for conversion ([D95](#d95)). Why: the
grant pays for the work before launch and the seat is consensus weight, so nothing paid the founder's work from launch
on; fixing the amount in NOAH keeps it from growing as NOAH's price falls, where the cap's whole room at a tenth of the
opening price would be 100M NOAH; and the market rule bounded nothing, since the cap, the committee's allowance, and the
two-week first period bound NOAH pay whatever it can be spent on. Rejected: maxing the cap at the price on the day of
the award, which grows the stream as NOAH falls. What it costs: the stream is ownership-scale NOAH under the
compensation label, the case the threat model names, so the plan counts it as the founder's stake: seat, grant, and
stream block alone until public bonded stake reaches twice what grant and stream have paid, 50M once both are paid where
the grant alone reached 30M; the founder is the largest wallet until bonded stake passes 475M rather than 375M; at the
20% planning rate the founder holds about a fifth of bonded stake and members lead the founding set only narrowly, 38.5%
to 37.9%; no other compensation reaches the founder's record for ten years; and step 1's contributor spend runs to 25M
against its 20M budget. Before NOAH trades the cap values NOAH at the bootstrap price, so it bounds every record's NOAH
pay at about 1M NOAH a window whatever NOAH is worth, committee awards included; and the window is a day shorter,
loosening the cap by under a third of a percent. The [disbursement plan](../governance/DISBURSEMENT_PLAN.md), the
[threat model](THREAT_MODEL.md), the [launch genesis](../governance/GENESIS.md), and the whitepaper carry the stream.

## D130

**Recorded status:** Decided 2026-10-10. Extends the 26-fortnight year of [D129](#d129) to every disbursement duration:
amends the ownership cliff and the ten-year bound of [D116](#d116), the quarter of [D122](#d122), and the member periods
of [D126](#d126).

Put every disbursement duration on whole weeks, a year being 26 fortnights, 364 days, the pay cadence's. The ownership
cliff becomes 26 fortnights, 31,449,600 seconds, so the ownership shape's 104 weights are one a fortnight and the grant
runs exactly 104 fortnights, 1,456 days, where the 365-day cliff paid 26 fortnights' weight over 26 fortnights and a
day. `MaxCompensationDuration` becomes 260 fortnights, 3,640 days, so the longest award at the launch period is the
bound itself and exactly ten windows, where 3,650 days left 260 as what fit under it with ten days spare.
`MaxCompensationBackdate` becomes 13 weeks, 91 days, a quarter of the year, from three plan months of 30.4 days. A
member's balance pays in thirteen four-week periods, 2,419,200 seconds each, weighted 13 for the tenth then 9 each of
130, from twelve plan months weighted 4 then 3 of 40: the tenth stands and the year after the hold is 52 weeks. Nothing
on chain is tied to calendar dates: every clock runs from its own award and the compensation window rolls, so the
364-day year accumulates no drift against anything the chain checks. Why: the plan month, 2,628,000 seconds, was the one
duration not a whole number of days and, with the window on fortnights, the only unit the pay cadence did not divide;
one grid makes every schedule a count of the cadence, keeps each payment at its award's time of day, and turns the
bounds into definitions rather than remainders of a division. Not changed: `chain.BlocksPerYear`, 365 days of blocks,
since block time strays by more than a day a year and its uses are caps and Treasury's variance annualisation, where 365
is the convention; seat vesting, four and eight calendar years after genesis, continuous and with no cadence; and
`MaxPeriodLength`, an overflow bound. What it costs: the year is about 1.24 days short of the calendar's, so a ten-year
stream ends about twelve days before the tenth anniversary of its award and a four-year grant about five, and 1M a year
admits about 0.34% more a calendar year; an agreement off chain states pay a fortnight rather than a year. A member is
paid fourteen times rather than thirteen, each payment after the tenth 6.9% of the grant rather than 7.5%. Nothing is
live, so no state migrates.

## D131

**Recorded status:** Decided 2026-10-10. Amends the plan's rule, from [D101](#d101) and [D117](#d117), that a
contributor grant its step's budget cannot hold waits for the next step.

Treat each step's contributor budget, 40% of the step's distribution, as a guideline rather than a limit. A review round
aims to stay near it; it may run over when the work warrants, stating the overrun and the reason in its summary, and
later rounds carry less, so contributors stay roughly in step with members and 60/40 holds over the whole distribution.
Why: the chain does not record the budget ([D117](#d117)), and a strict wait would hold back an award the plan has
already decided, the founder's stream of [D129](#d129), which with the grant takes step 1 to 25M against 20M, over an
accounting line. The budget's real job survives as a published pace for awards while the founding set is the only voter.
What it costs: only governance limits a run of overruns, and at launch that is the founding set; the step table and each
proposal summary make an overrun visible.

## D132

**Recorded status:** Decided 2026-10-10. Amends the measure of public bonded stake and the gate figures of
[D101](#d101).

Count the founder's grant and stream out of public bonded stake, and set each gate at 15% of the step's distribution
less them. Public bonded stake is bonded stake less the genesis seats and less what the founder's 15M grant and 10M
stream have paid, counted as bonded since the founder bonds everything; the disbursement journal shows the paid figure.
The gates before steps two to five become 3.75M, 18.75M, 41.25M and 63.75M, each 3.75M, 15% of the founder's 25M, under
its D101 figure. Why: the gates measure public participation, and the founder's own payments counted toward them: at 20%
member staking, with step 1's members registering over the first year, step two's 7.5M was met about the month the
founder's cliff paid, more than half of it the founder's grant and stream. The handover figures of the [disbursement
plan](../governance/DISBURSEMENT_PLAN.md) already count them as the founder's ([D129](#d129)). Excluding them alone
would not do: the founder's 25M takes step 1's contributor share, so step 1's public distribution is the members' 30M,
whose 20% fully paid is 6M, under 7.5M. Netting the founder out of the base keeps each gate three quarters of the
planning rate on what the public received, and step two opens about month fourteen, near where it did, on members'
stake. Other seat holders' grants stay public, since the plan treats the seats as independent ([D106](#d106)). What it
costs: each gate is 3.75M lower, so a step opens on less public stake; the step-5 gate falls under the seats' 67.5M and
no longer marks where public stake passes them, though the founder plus five seats still block at it, 47.5M of 131.25M;
and reading a gate needs the journal beside the staking pool.

## D133

**Recorded status:** Decided 2026-10-10. Moves the Claims window of [D30](#d30) and [D38](#d38), every committee window
of the shared envelope, the registrar's window of [D120](#d120), and the settlement activation delay to block time, and
amends the volatility annualisation of [D72](#d72). The settlement activation delay is a week under [D134](#d134).

Measure every window a governance vote must fit in seconds of block time, the clock the vote itself runs on, and keep
per-block processes in blocks. The Claims cancellation period becomes `claim_cancellation_period`, 604,800 seconds, with
claims queued by closing time; the settlement activation delay becomes `settlement_activation_delay`, 259,200 seconds,
with plans carrying activation, earliest closing, and opening times; and the shared committee envelope carries
`activation_time` and `expiry_time`, Unix seconds, for all eight appointments. Treasury's annualised volatility divides
its EWMA of squared per-block returns by an EWMA of the seconds each return spanned, under the same retention, and
scales by a year of seconds: variance per second over one window. Unchanged, as per-block processes: the oracle reward
and attendance windows, Treasury's per-block reward, funding window, and refresh cadence, Market's pool recovery,
vote-extension activation heights, the Security committee's upgrade height, which `x/upgrade` counts in heights, and
recorded heights. Why: block time is node-local, set by each validator's `timeout_commit`, and CometBFT gives the app no
say over it, so a block count promising to outlast a vote held only while blocks stayed near six seconds: the three-day
delay fell inside the two-day vote below four-second blocks, and the week of claims missed a deposit period and vote
below about 3.4. Committee terms are dates people plan reappointment around, and a year of blocks ran from about 335 to
395 days across plausible paces, moving whenever validators retuned. The volatility constant read about 9% low at
five-second blocks and 4% high at 6.5, and the first return after a halt arrived as one block's move: a one-day halt
lifted the reading about 31% for a day. The time-weighted fold removes both, starts reading correctly from its second
sample rather than warming up, and keeps the bound a theorem, since each squared return is at most one and each interval
counts at least a second. What it costs: block time runs through an outage as the vote does, so a halt takes time from
the veto and cancel windows that no one could use; a coalition able to halt the chain already holds a third of the vote
and every veto with it, so it gains nothing, and the relaunch runbook has operators decide whether to push pending
closing and activation times forward by an outage. The fields are renamed and retyped before launch, so no state
migrates.

## D134

**Recorded status:** Decided 2026-10-10. Amends the settlement activation delay of [D133](#d133).

Make the settlement activation delay a week, `settlement_activation_delay` 604,800 seconds, the Claims cancellation
period; the testnet's becomes four hours, as Claims'. Why: the clock starts when the opening vote passes, so three days'
single spare day also had to absorb noticing the mistake and any deposit period, and a cancel opened at the 10% initial
deposit could run four days and land after activation. A week fits the full deposit period and a vote with three days
to notice, and both correction windows rest on one rule. What it costs: holders of a suspended asset wait a week, not
three days, before redemption opens. The delay is read at opening, so the change never reaches an open plan, and
nothing is live.

## D135

**Recorded status:** Decided 2026-10-10. Replaces the quarter the [launch genesis](../governance/GENESIS.md#8-oracle)
recorded for `reward_distribution_window`.

Make the Oracle reward distribution window four weeks, 403,200 blocks, from a quarter; the testnet's becomes four days of
daily windows, the same quarter of the pool per settlement. Why: Treasury tops the Oracle's account up to its target
from the subsidy every funding window, so oracle income has a floor while the subsidy lasts, and with conversion closed
at launch there is no tax above it. The window does not protect voters from a drought; it only spreads tax windfalls
above the target. A quarter paid 40% of the target in the first quarter and 77% in the first year for smoothing nothing
yet needs; four weeks pays 77% and 94% and still damps weekly windfall noise about 2.6 times. Revisit with the
[D72](#d72) weights once tax is measured. What it costs: a tax windfall reaches voters over a month rather than a
quarter, and the pool's cushion against a quiet tax week is a month of income.

## D136

**Recorded status:** Decided 2026-10-10. Amends the proposal path of the founder's grant in [D115](#d115) and of the
founder's stream in [D129](#d129); their amounts, schedules, pool, and caps stand.

Write the founder's 15M grant and 10M stream into genesis as governance awards rather than proposing them in the first
review round. Seat assembly, `arkd genesis add-validator-seats`, adds both to the founder's beneficiary record beside
the seat: grant 1 an ownership grant at the cap on the ownership schedule, grant 2 a compensation stream of 260
fortnights valued at one XDR a NOAH ([D128](#d128)), both reserved in the contributor pool, created by the governance
authority at the first block, and clocked from `genesis_time`, so the stream first pays two weeks in and the grant a
year in. Why: with the founder abstaining, the other seats decided the awards after launch, two thirds of them needed;
instead every one of them signs a gentx against the published file, so the founding set approves the awards
unanimously and before launch, and the founder no longer waits on a vote, the assurance [D112](#d112) gave for making
the seat genesis state. The bootstrap valuation fixes the stream at 10M exactly, where an award voted later took
whatever the cap admitted at the XDR of the day. Governance keeps the cancel: one proposal returns either grant's
unearned principal to the pool, so the founding set's approval becomes the public's standing veto. Rejected: genesis
vesting accounts, which pay the same coins but leave behind the cap, the journal [D132](#d132) reads, the cancel, and
controller rotation; a flag, since the awards are policy fixed in `pkg/chain` like the seat. What it costs: both clocks
start at genesis, earlier than the first round's vote by however long that round would have taken; the written case
[D127](#d127) asks of every grant lives in the plan rather than a proposal summary, each grant's reference pointing
to it; a validator who objects to either must refuse the genesis or later propose the cancel; and disbursement import
admits a stamp at height one though InitChain runs at height zero, as Oracle's already does. The
[disbursement plan](../governance/DISBURSEMENT_PLAN.md), [launch genesis](../governance/GENESIS.md), module README,
[threat model](THREAT_MODEL.md), and whitepaper carry the path.

## D137

**Recorded status:** Decided 2026-10-10. Floors the `max_exchange_rate_age` the [launch genesis](../governance/GENESIS.md#8-oracle)
records.

Leave user transactions out of a block whose prices are stale by its proposer's clock, and floor `max_exchange_rate_age`
at 30 seconds. A block applies the prices attached to the previous commit and carries that commit's median time, so
after a stall or a halt both predate the outage while its transactions are chosen after it: a swap sent during a
two-hour upgrade traded on prices two hours old that the age check read as new, since nothing in the block records the
outage. A proposer whose clock finds the proposal's time older than `max_exchange_rate_age` proposes commit-only, and
waiting transactions trade in the next block on prices observed after the outage. The floor keeps the rule off ordinary
blocks, about five seconds behind the clock: under that gap every block would be empty, the vote to undo it included.
Why: currency crosses carry only the 0.25% Tobin spread and no pool, so a move over a halt larger than that was free to
take at the protocol's expense, and conversion opens by vote, so the guard has to precede it. Rejected: settling orders
on the next block's prices, which closes the gap deterministically but ends single-transaction swaps, buy-and-send
payments, contract use of a fill, and award-time funding; re-checking in `ProcessProposal`, which would make acceptance
depend on validator clocks, so a third of stake running fast would stop every transaction; a wider spread, which charges
every trade for a rare event; proposer-based timestamps, absent from the pinned CometBFT. What it costs: a stall or halt
costs one empty block; the rule binds honest proposers alone, so a validator proposing the first block after an outage
can still trade it, on chain and attributably; and a proposer's clock now decides whether its block carries
transactions. Nothing is live, so no state migrates.

## D138

**Recorded status:** Decided 2026-10-10. Amends the launch tax cap of [P1](#p1) and the per-input cap of [D17](#d17).

Launch `reference_tax_cap` at 100 XDR, from 1,000, and tax each `MsgMultiSend` output as its own principal with its own
cap. Why: the tax sees only the Bank ledger, so a ledger built on it, a contract's balances, an exchange, a custodian,
moves value between its users untaxed, and what a large mover saves by leaving the rail is the cap per move. Taking a
contract's risk at 1% a year, at 1,000 XDR a 100,000 XDR balance moved twice a year already pays for wrapping; at 100 it
needs ten moves, and a 1M balance a hundred. The chain mints nothing, so after the subsidy tax and gas are the
validators' pay and the cap cannot fall far: under a heavy-tailed size mix 10 XDR collects about a third of what 100
does, while under a retail mix the cap barely matters. Above 100 the extra comes only from transfers over 20,000 XDR,
the movers best placed to leave, so it is the least reliable share of that pay. A MultiSend input capped once let one
batch share a single cap, 1,000 XDR for 500 payouts that would pay 5,000 as sends; per output a batch pays what its
sends would, and the SDK's single input makes the outputs the whole principal. Rejected: refusing registered currencies
to contracts outside a governance code list, which takes them out of the open runtime and still needs every listed
contract's claims, LP and vault shares included, to be untransferable; 10 XDR; a cap controller, which [D79](#d79)
rejects. Revisit once conversion opens: if the share of each currency held by contract addresses stays small, large
movers are staying and a higher cap can be tried, at the cost of refusing the large transfers in flight at the change
([D80](#d80)). What it costs: a large transfer pays a tenth of the old ceiling, and a payout batch pays per recipient.
Nothing is taxed before conversion opens, so nothing migrates.

## P1

**Recorded status:** Decided 2026-09-11; every value is recorded with its reasoning in [GENESIS.md](../governance/GENESIS.md). The subsidy seed is 50M under [D125](#d125). The tax cap is 100 XDR under [D138](#d138).

Choose launch tax rate, reference cap Coin, three target ratios, subsidies, genesis fund balances, and the D72
exposure weights (zero at launch keeps `m` at one and the targets unscaled).

## P2

**Recorded status:** Confirmed.

Phase 3A found no recipient-output, fixed-price cycle, or split residual-mint amplification under coverage-based
Buffer funding; add no residual-mint limiter.

## P3

**Lifecycle:** The historical submitted-expectation interface was replaced by the [Oracle reference transition](ECONOMIC_DESIGN.md#310-live-pool-unit-transition).

**Recorded status:** Confirmed.

Apply the deterministic live-derived pool amount without comparing the submitted expectation to a rejection
threshold; retain the submitted expectation in the transaction and emit the old and applied pool state for audit.

## P4

**Lifecycle:** The recorded Treasury parameter name predates [D54](#d54).

**Recorded status:** Decided 2026-09-11: no committee is appointed in genesis. Appointments follow the first-cycle
sequence in [GENESIS.md](../governance/GENESIS.md#13-committee-appointments), and the committee corridors are set there.

Choose the launch economic-policy committee and bounds, Claims committee multisig and appointment window, the
shared cancellation-period Treasury param, fixed gross committee claim limit, and operational fee funding.

## Launch configuration correction

The curated `vote_extensions_enable_height` was corrected from zero to one on 2026-09-06. Zero disables the oracle
vote-extension path. This is a configuration correction, not a new D-numbered economic decision. The current setting
and its verification live in [genesis](../governance/GENESIS.md#2-chain-and-consensus).

The Oracle's `reward_window_start_height` and `attendance_window_start_height` were corrected from zero to one on
2026-10-10, with the code default and the Oracle's genesis check, which reads height zero on a chain starting at one.
Zero named a block that never runs, so the first windows were a block short and the first reward settlement ran a block
before Treasury's first funding, found the account empty, and discarded the week's weights. At one, every reward
settlement shares a funding block. Also a configuration correction; the setting lives in
[genesis](../governance/GENESIS.md#8-oracle).
