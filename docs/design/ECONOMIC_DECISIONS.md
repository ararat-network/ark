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
- D72–D86: [D72](#d72), [D73](#d73), [D74](#d74), [D75](#d75), [D76](#d76), [D77](#d77), [D78](#d78), [D79](#d79), [D80](#d80), [D81](#d81), [D82](#d82), [D83](#d83), [D84](#d84), [D85](#d85), [D86](#d86).
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

**Recorded status:** Confirmed.

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

**Recorded status:** Confirmed.

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
total in the Reserve — which is the all-or-nothing rule already settled.

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

**Recorded status:** Confirmed.

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

**Recorded status:** Confirmed.

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

**Recorded status:** Confirmed.

Begin Phase 4 by fixing execution-time tax payer, exactly-once identity, rollback, and fee-sponsorship semantics;
then implement IBC foundations before Wasm because contracts may dispatch IBC messages. Design the Treasury
execution hook into both paths, but keep both transfer surfaces production-disabled until the complete tax and
recipient-restriction activation gate passes.

## D41

**Recorded status:** Confirmed.

Use call-path ownership for exactly-once tax assessment: ante owns signed top-level inputs, while the Wasm
dispatcher owns only execution-generated Bank sends, IBC sends, execute funds, and instantiate funds. Add no
persistent transfer IDs, context markers, global Bank tax hook, or implicit execution-time feegrant. The sending
contract pays the tax in addition to the complete requested principal.

## D42

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

**Recorded status:** Confirmed.

Add IBC callbacks to both the Classic and v2 ICS-20 stacks when the Wasm keeper is wired. Callbacks are Ark's
canonical transfer-and-call mechanism; add no separate IBC Hooks middleware. Callback-triggered execution uses the
same Wasm/Treasury execution adapter, while acknowledgement, timeout, and callback delivery alone are not new
taxable transfers.

## D48

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

**Recorded status:** Confirmed.

Consider 08-Wasm only with the Wasm foundation and only if its exact dependency and VM family integrates cleanly.
If installed, keep it dormant at launch: the allowed-client list admits no client type at launch (D45 as amended)
and the launch genesis contains no Wasm-client checksums. `09-localhost` remains unavailable through that launch
allowlist; add neither 06-Solo Machine nor the experimental attestations client.

## D51

**Recorded status:** Confirmed.

Keep packet forwarding Classic-only until upstream provides reviewed v2 support; do not invent a v2 PFM adapter.
Add no ICS-29 relayer-fee wiring because that application was removed from IBC-Go.

## D52

**Recorded status:** Confirmed.

Retain standard user ICA controller and host support but launch both disabled and the host with an empty message
allowlist. Defer a generic custom ICA authentication module; if contracts later need ICA control, prefer a narrowly
scoped Wasm-to-ICA adapter with explicit authorization.

## D53

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

**Recorded status:** Confirmed.

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
gate](../governance/GOVERNANCE_OPERATIONS.md#5-ark-governance-operations). Closes §12's adapter machinery beyond the D57 narrowing.

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

**Recorded status:** Confirmed.

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
encoder goes unused: a contract calls Market with `CosmosMsg::Any` carrying `/ark.market.v1.MsgSwap` and proto
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

**Recorded status:** Confirmed.

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
denomination (D44), and `market.MsgSwap` is exempt (§8.2), so `MsgSwap` stable→NOAH, `bank.MsgSend` NOAH, `MsgSwap`
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
[GENESIS.md](../governance/GENESIS.md#3-accounts-supply-and-validator-seats) (decided 2026-09-11).

Validator entry after launch is open. The ten founding seats are the only granted stake: equal permanently locked
grants and floats drawn from the community pool at assembly, each adding one seat's share to both reward targets. A
later validator bonds NOAH it holds through the ordinary staking transaction; no proposal grants it a seat, and the
seat-admission procedure and its e2e coverage are removed. Equality is therefore a launch fact rather than an enforced
property, and the reward targets no longer track the set by construction: they are the founding count's shares at
assembly and move afterwards only by `MsgUpdatePolicy`, inside the committee corridor or by governance beyond it.
Why: a granted seat made the founders the gate to consensus for as long as they held the vote, which is the control
the [distribution plan](../governance/DISTRIBUTION_PLAN.md) exists to hand over, and a seat budget drawn from the
pool competed with that distribution for the same NOAH. The seat command, the assembly sequence, and the testnet
tooling that seats validators from the artefact stand. D9, D13, and the gov thresholds stand.

## D85

**Recorded status:** Decided 2026-09-22. Amends the accept-list practice recorded in [D74](#d74).

The contract query accept list admits every query the SDK annotates `module_query_safe` for auth, bank, and staking,
thirty-four paths, beside the fourteen hand-written Ark entries. The annotation is upstream's review, and the test
that requires it of every entry stands; the hand-written review is for Ark's own modules, whose response shapes Ark
freezes by listing them. Distribution, gov, and slashing carry no annotation in the pinned SDK and stay out until
upstream annotates them. Why: the grant escrow in the [distribution plan](../governance/DISTRIBUTION_PLAN.md) reads
total bonded stake through the staking pool query, and a list widened one path at a time for each contract need would
re-review what upstream already has. Wasmd's native bank, staking, and distribution queriers were already on; the
listed paths add what they lack: the pool total, both modules' params, unbonding and redelegation views, historical
info, spendable balances, send-enabled, and account lookups. D74 stands.

## D86

**Recorded status:** Decided 2026-09-22.

The [distribution plan](../governance/DISTRIBUTION_PLAN.md)'s grants are created by a CosmWasm contract,
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
holder's, and the field that restated pay outside the contract is gone with nothing left to restate.

## P1

**Recorded status:** Decided 2026-09-11; every value is recorded with its reasoning in [GENESIS.md](../governance/GENESIS.md).

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
