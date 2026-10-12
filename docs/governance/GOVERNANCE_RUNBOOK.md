# Governance runbook

For proposal authors and voters: what governance may do, how to draft and verify each proposal, and the order its
steps must run in. [Genesis](GENESIS.md) owns launch values, [economic design](../economics/ECONOMIC_DESIGN.md) the
financial rules, and each module's README the mechanism behind its messages; the [committee
runbook](COMMITTEE_RUNBOOK.md) covers appointed committees and the grants programme.

## Contents

- [1. Proposals](#1-proposals)
- [2. Chain and software](#2-chain-and-software)
- [3. Committees](#3-committees)
- [4. Oracle](#4-oracle)
- [5. Asset registry](#5-asset-registry)
- [6. Treasury](#6-treasury)
- [7. Reserve](#7-reserve)
- [8. Market](#8-market)
- [9. Claims](#9-claims)
- [10. Grants](#10-grants)

## 1. Proposals

Every message in this runbook is signed by the governance authority, the gov module account
(`arkd query auth module-account gov`), and reaches the chain inside a proposal. Draft one with
`arkd tx gov draft-proposal` or by hand, as in the [grants examples](COMMITTEE_RUNBOOK.md#8-grants), and submit it with
`arkd tx gov submit-proposal proposal.json --from KEY`; each module's `*-proposal` command submits a single message the
same way. A proposal's messages execute in order and together: if one fails, none applies and the proposal reads
`FAILED`. Bundle the steps a section says belong together, and never the ones it says to keep apart.

Before drafting, query current parameters and state, and check the section's dependencies. A `MsgUpdateParams` or policy
message replaces the whole object, so copy the current one and change only the fields the proposal is about.

Deposits, periods, quorum, and thresholds are in [genesis §11](GENESIS.md#11-cosmos-sdk-modules). An expedited proposal
(`"expedited": true`) takes the higher deposit and threshold for a one-day vote, and one that does not pass in that day
continues as an ordinary vote. Submissions and deposits that meet x/gov's initial and per-deposit ratios from spendable
balance, votes on active proposals, and a proposer's cancellation ride the governance lane ([mempool
policy](../../app/mempool/README.md#admission-service-and-sdk-behaviour)). An explicit vote needs at least one NOAH
bonded, and a vote or deposit arriving after its period has closed is refused at submission
([ante](../../app/ante/README.md#context-setup-and-message-guards)).

After execution, check `arkd query gov proposal ID`, then query the state the proposal changed and verify it before
starting a dependent step. If a step fails, re-read state and correct the proposal; do not enable a dependent service
to bypass an unmet prerequisite.

## 2. Chain and software

**Upgrades.** A governance upgrade is `cosmos.upgrade.v1beta1.MsgSoftwareUpgrade`. The plan name is the one the new
binary registers ([app/upgrade](../../app/upgrade/README.md)), proved first by the [upgrade
rehearsal](../../contrib/scripts/README.md#upgrade-rehearsal). Set the height to leave operators time to build or verify
the binary and install it under the [upgrade binary policy](../operations/NODE_OPERATIONS.md#upgrade-binary-policy);
`info` names the release and is never a download operators trust. A governance plan replaces any pending plan, the
Security committee's included, and `MsgCancelUpgrade` withdraws it. [Node
operations](../operations/NODE_OPERATIONS.md#7-upgrades) owns the coordinated restart.

**Consensus parameters.** `cosmos.consensus.v1.MsgUpdateParams` replaces the block, evidence, validator, ABCI, and
authority parameters together. Keep `block.max_bytes` at least `MinBlockDataBytes` (8 MiB) above `evidence.max_bytes`,
or ante refuses the update ([ante](../../app/ante/README.md#context-setup-and-message-guards)). The authority parameter
is the address every module's governance messages check, so changing it hands every power in this runbook to the new
address.

**Community pool.** `cosmos.distribution.v1beta1.MsgCommunityPoolSpend` is the pool's only exit
([genesis §3](GENESIS.md#3-accounts-supply-and-validator-seats)). Topping up Treasury's subsidy pool is an ordinary
spend to its address; `MsgReturnSubsidy` (§6) goes the other way. Grants draw on Disbursement's own accounts, not the
community pool.

### Contract runtime

Open from height one: anyone may upload and instantiate. What governance may still do:

| Action | Message |
| --- | --- |
| Change the permissions | `cosmwasm.wasm.v1.MsgUpdateParams` with `code_upload_access`, `instantiate_default_permission` |
| Curate uploaders without shutting the runtime | `MsgAddCodeUploadParamsAddresses`, `MsgRemoveCodeUploadParamsAddresses` |
| Keep a code cached in memory | `MsgPinCodes`, `MsgUnpinCodes` |
| Upload or operate a contract itself | `MsgStoreCode`, `MsgStoreAndInstantiateContract`, `MsgSudoContract`, `MsgMigrateContract` |

Under `Nobody`, governance still uploads: Wasmd hands the authority its own permission policy, so `Nobody` shuts out
everyone else and never freezes the runtime. The query accept list is the one thing on this surface a vote cannot
change: it is code (`app/wasm_query.go`), a listed path's response shape is frozen for the life of the chain, and
widening it is a coordinated binary upgrade.

## 3. Committees

Each committee has its own mandate message: `ark.security.v1.MsgSetSecurityMandate`,
`ark.asset.v1.MsgSetEmergencyMandate`, `ark.treasury.v1.MsgSetEconomicMandate`, `ark.reserve.v1.MsgSetReserveMandate`,
`ark.market.v1.MsgSetConversionMandate`, `ark.claims.v1.MsgSetClaimsMandate`, and
`ark.disbursement.v1.MsgSetGrantsMandate`. [Genesis §14](GENESIS.md#committee-appointments) sets the first cycle's
order, shapes, and corridors.

Default mandates are disabled. Governance can operate without a committee; appointment delegates a bounded faster
path and never becomes a prerequisite for governance's own messages. Choose appointments from incident exposure,
not chain age: security response may need a fast path before the deliberative mandates do.

An ordinary single-key account can be appointed, with its concentration recorded by `CommitteeShape`. A threshold
multisig can be appointed through the same message; its key should already be registered so governance can observe
its backing. Replacing the address or reappointing it advances the term and re-records shape. The existing multisig
integration tests exercise real ante authentication; no native committee module is needed.

1. Prepare the root account and its own NOAH for gas, never funded from a custody account. Know what appointment
   will record: a key-backed account's kind and K-of-N, a contract only that it is one.
2. Draft the domain's `MsgSet*Mandate` with its activation and expiry times, Unix seconds of block time, and its
   policy bounds, allowance, floor, or destinations. Write the dates the term should run between; block pace does not
   move them.
3. After execution, verify the emitted appointment and query state, including term and shape. Construct actions for
   that exact term and send only while the appointment is active.
4. At replacement, review outstanding claims and positions as well as term usage. Pending commitments outlive terms;
   replacing the charter does not erase their custody or history.

Once appointed, a committee works from the [committee runbook](COMMITTEE_RUNBOOK.md), which also covers how its
transactions are signed and submitted.

A CosmWasm contract can hold a deliberative mandate through the same message, for a committee that wants weighted
membership, visible proposals and votes, and a root address that survives personnel changes; the candidate is a
cw3-flex-multisig root over cw4-group membership. Appointment records it as a contract and nothing more. It acts by
dispatching the committee message itself from a `MsgExecuteContract` a proposer signs: Wasmd refuses the dispatch
unless the message names the contract, and the module authorises term and window as for a signed transaction. The
proposer supplies `expected_term`, so a replacement while the contract is still voting fails the execution; no
mandate query is on the contract accept list to spare it that guard. No such root is deployed or appointed yet.
Before proposing the first one:

- Choose and review the cw3/cw4 code, and cite the checksum the review covered in the proposal. The shape never
  records code or membership: the membership contract edits its own state, the admin migrates the code, and
  governance can migrate, sudo, or re-admin any contract whether or not it has an admin, none of which advances the
  term. Replacement is the remedy for a root that changes under its term.
- Confirm the contract's admin is governance or cleared, and that the membership contract's admin is the root itself.
- Fix who may propose on the root and who funds its gas; the executing transaction is theirs, not the committee's.
- Accept that its transactions ride the normal lane, never the committee lane: the lane classifier sees a
  `MsgExecuteContract`. That is tolerable for a deliberative mandate and one more reason emergency mandates stay on
  off-chain multisigs.
- Do not grant `x/authz` authority over committee messages, and do not build solo-officer roles into the root.
  Either is delegation state that outlives the term and lets one key act under a shape recorded for a quorum.

**Reference change.** When changing the protocol reference, put `MsgSetReferenceDenom` before
`MsgSetConversionMandate` in the same governance proposal. Appointment validates the corridor against the live pool
unit, so reversing the order fails; a combined proposal cannot leave only half the pair applied. Allow margin for
execution-time rates when sizing the new depth corridor. The corridor constrains future candidates, so an appointment
need not report that it fails to bracket the live rebased depth; review that explicitly.

The old corridor is not silently reinterpreted in new units. Treasury's corridor has no denomination and requires no
corresponding appointment; its reference-valued state rebases atomically. Disbursement's compensation figures, the cap,
every award's value and charge, and the live grants term's usage and allowance, rebase atomically too, so the grants
committee needs no reappointment, and so do the Claims committee's limit, every committee claim's charge, and the live
term's usage. Query fresh incoming pricing and either fresh outgoing pricing or the explicitly governed outgoing-rate
override under [Oracle's reference contract](../../x/oracle/README.md#13-protocol-reference-denom).

## 4. Oracle

**Adding a feed.** `MsgAddFeed` (`arkd tx oracle add-feed-proposal DENOM`) activates two blocks after execution.
Operators add provider markets and routes for the denomination before the vote ends: sidecars read scheduled additions
and warm prices ahead of activation ([pricefeed operations](../operations/PRICEFEED_OPERATIONS.md)). Watch
`arkd query oracle exchange-rate DENOM` print before anything depends on it; registering an asset or naming the feed in
the Reserve's recognition policy is a later proposal, never the same one.

**Removing a feed.** `MsgRemoveFeed` is refused while anything pins the feed: an asset it prices, a Reserve eligibility
entry or open position, or the protocol reference. `arkd query oracle feed-referents DENOM` lists every claim, so
resolve them first. A denomination with no feed fails rather than passing as a no-op.

**The protocol reference.** `MsgSetReferenceDenom` re-points the reference to an Active feed with a fresh rate; Market,
Treasury, Disbursement, and Claims rebase in the same act. When the outgoing feed is stale or has never priced, state
the conversion with `outgoing_rate`. The conversion committee's corridor needs a reappointment in the same proposal
(§3). [Oracle §1.3](../../x/oracle/README.md#13-protocol-reference-denom) owns the rules.

## 5. Asset registry

Governance owns every lifecycle message; the Asset emergency committee can only suspend
([committee runbook §3](COMMITTEE_RUNBOOK.md#3-asset-emergency)). Every message except registration carries
`expected_version` from `arkd query asset asset DENOM`, so a proposal drafted against an asset that has since changed
fails instead of acting. [Lifecycle operations](../../x/asset/README.md#8-lifecycle-operations) owns each message's
preconditions.

**Registering an asset.** `MsgRegisterAsset` needs an Active feed for the denomination, and no Bank supply or
metadata under it. Propose the feed, watch it print, then register in a later proposal. Verify with the asset query
and one small `MsgSell`. The first ten registrations open conversion, below.

**When an asset fails.**

1. **Contain.** Suspend: the committee's act, or `MsgSuspendAsset` when no mandate is active or the committee has
   already suspended this asset this term. Issuance, conversion, and ordinary redemption stop; balances and transfers
   continue. Until a settlement plan opens, the suspended supply is untrusted and liability valuation is incomplete
   (alert A4), which parks expansion principal in the Reserve and blocks surplus burns.
2. **Decide**, by proposal:
   - **Recover** with `MsgRecoverAsset` once the feed is Active and governance is satisfied it is stable. The asset
     lands `ISSUANCE_HALTED` and any plan closes; `MsgResumeIssuance` restores issuance when wanted.
   - **Settle** with `MsgOpenSettlement`, stating a fixed rate and the earliest closing time. The plan activates after
     the `settlement_activation_delay` parameter, a week at launch, and holders then redeem one way with `MsgSettle`.
     `MsgCancelSettlement` withdraws a plan before it activates, and nothing amends one.
   - **Write off** with `MsgWriteOffAsset`, which recognises no obligation and excludes the supply from liability.
     A plan's earliest closing time must have passed; with no plan, nothing waits.
3. **End.** `MsgFinaliseRetirement` ends any path in `RETIRED`: from `SUSPENDED` at zero supply, from `WRITTEN_OFF`
   with any residual, and from `ISSUANCE_HALTED` with residual at most `max_residual_supply`. Nothing leaves `RETIRED`;
   a successor takes a new denomination.

**Winding down a healthy asset.** `MsgHaltIssuance` stops issuance while redemption stays open at the full rate, then
`MsgFinaliseRetirement` once the supply has come home. Halting signals nothing in distress and contains nothing: it is
for a deliberate wind-down or an inflow-side problem such as a mint-path exploit, never for containment.

### Opening conversion

Conversion is closed at launch: the artefact registers the eleven price feeds and no asset, so Market has no `ACTIVE`
ask to mint. Until a provider samples a NOAH venue, every rate's NOAH leg is the configured placeholder of one
NOAH an XDR, and a stablecoin is a claim on NOAH at the oracle rate, so a currency minted at the placeholder is
re-marked when the live price arrives: if NOAH trades at five cents, an XDR that cost one NOAH redeems for about 27, and
every holder pays whoever minted early. The first live price also comes from the first venue to list NOAH, so a thin
venue would set the redemption rate of everything minted before it. Registering the currencies and relying on the
missing rate was rejected because conversion would open by itself the moment any provider sampled NOAH, without a vote,
and `ISSUANCE_HALTED` at genesis because it is a wind-down status that admits redemption. Conversion opens in this
order, once a venue trades NOAH with real depth.

1. **Sidecars read the venue.** Operators add NOAH markets to their provider configs in one coordinated window, not as
   each notices a listing. A live sample replaces the bootstrap NOAH price on every sidecar that carries the venue, and
   the `ark.pricefeed.bootstrap.price.uses` counter falling to zero shows the switch. From that block the NOAH gas
   factor tracks the live rate and fees in NOAH move to their XDR-targeted level; clients that build fees from the gas
   price query follow on their own ([client fees](../clients/CLIENT_FEES.md)).
2. **Confirm the depth.** The artefact sets `conversion_policy.base_pool` to 100,000 XDR. Run the
   [depth rule](COMMITTEE_RUNBOOK.md#sizing-depth) with the live price and the liquid NOAH outstanding,
and change the depth before opening, by `MsgUpdatePolicy`, only if the rule gives a reason: depth is the
   throttle on how fast NOAH mints and burns, and nothing grows it automatically.
3. **Register the currencies.** One proposal carrying ten `MsgRegisterAsset`, one per currency listed below.
   Registration requires each feed to be Active, which they have been since genesis; never bundle a feed change into
   the same proposal ([asset lifecycle](../../x/asset/README.md#7-the-lifecycle)). Conversion opens when the proposal
   executes. Verify with the asset registry query and one small `MsgSell`.
4. **Appoint the Market committee.** Its corridor should reach about ten times the genesis depth, so routine raises as
   liquid NOAH is distributed need no vote (§3, [genesis §14](GENESIS.md#committee-appointments)). The committee
   sizes each raise by the depth rule; governance raises depth beyond the corridor by the same rule, and nobody
   shortens recovery to do it.

The currencies step 3 registers:

| Denomination | Tracks |
| --- | --- |
| `aaud` | Australian dollar |
| `acad` | Canadian dollar |
| `acny` | Chinese yuan |
| `aeur` | euro |
| `agbp` | pound sterling |
| `ajpy` | Japanese yen |
| `akrw` | South Korean won |
| `amxn` | Mexican peso |
| `asgd` | Singapore dollar |
| `ausd` | United States dollar |

`axdr` is not one of them: it is the protocol reference, priced by its feed and never registered.

Stablecoin compensation in Disbursement becomes available at step 3 ([grants](COMMITTEE_RUNBOOK.md#8-grants)).

## 6. Treasury

**The tax.** `MsgUpdateParams` carries `transfer_tax_rate` and `reference_tax_cap`. Hold the rate at or below the lowest
spread floor a conversion committee can reach: NOAH is untaxed, so above it a swap through NOAH carries value for spread
instead of tax ([economic design §5](../economics/ECONOMIC_DESIGN.md#5-transfer-tax-and-fees)). Nothing enforces this in
code, so check it against the conversion mandate's minimum policy. The tax is part of the fee every wallet signs, so a
raise refuses every in-flight transfer declared at the old rate. The cap is in the reference unit; zero means uncapped.

**Economic policy.** `MsgUpdatePolicy` sets `EconomicPolicy` whole, without the committee's corridor ([committee runbook
§4](COMMITTEE_RUNBOOK.md#4-economic-policy)). The multiplier machinery, its decays, cap, step, and refresh period, is in
`Params` and governance's alone. Switch the multiplier on, with weights above zero, only after the calibration window
has produced thresholds ([protocol monitoring
§2.1](../operations/PROTOCOL_MONITORING.md#21-two-phases-and-the-first-one-is-not-alerting)); the multiplier is floored
at one, so zero weights are exactly the unscaled sizing.

**Returning subsidy.** `MsgReturnSubsidy` moves an exact amount from the subsidy pool to the community pool, with a
minimum remaining balance as the proposal's stale-state guard; it changes no supply or target.

### Exposure alerts

[Protocol monitoring §2.4](../operations/PROTOCOL_MONITORING.md#24-alerts) states each alert's condition; these
decisions are governance's. The Reserve committee answers A5 and A6 from the
[committee runbook](COMMITTEE_RUNBOOK.md#alerts).

**A1 — multiplier pinned at the cap.** Decide deliberately, by governance vote: raise `multiplier_cap`, or
accept that the model is saturated and act on the underlying exposure directly. Do not raise the cap
reflexively. A saturated model during a real crisis may be telling the truth, and raising the ceiling to make
the alarm stop is how a risk model gets talked out of its own finding.

**A4 — sustained incomplete valuation.** The two supply lists on the event say whether the cause is a stale feed
or a suspension without a settlement plan; they need different fixes. Expect a Reserve-to-Buffer proposal as
follow-up if the window was long, because retention accumulated in the Reserve rather than filling targets
throughout — the whole block's expansion principal parks there while valuation is incomplete.

**A2 and A3** are engineering conditions, diagnosed against `FundStatus` and the oracle rather than decided by vote. A
step limit that binds through calm means governance revisits the step and the weights together.

## 7. Reserve

**Onboarding an external asset.** Three proposals in order: `MsgAddFeed` for its series (§4), then, once the feed is
Active, `MsgSetRecognitionPolicy` with an entry named `<feed>-<tag>`, such as `abtc-cb`, carrying its haircut, maximum
credit per NOAH, and rate and attestation windows, then `MsgSetReserveMandate` naming the destination. Custody is
attested: the asset sits at the destination and the committee states the quantity, and the Reserve account never holds
an external token. The recognition policy carries the whole eligibility list, so copy
`arkd query reserve recognition-policy` first; its maximum credits per NOAH sum to at most one, and it outlives
appointments.

**Funding the Buffer and Insurance.** `MsgFundBuffer` and `MsgFundInsurance` move an exact amount and keep a minimum
remaining Reserve balance the proposal states. They are one-way and change no supply, quote, or pool state; governance's
versions have no global floor, cap, or trigger.

**Burns.** `MsgBurnReserveAssets` burns any custody, NOAH included, keeping the NOAH above the minimum the proposal
states: a NOAH burn of chosen size is the protocol's one discretionary supply contraction. The committee's burns are
bounded separately ([committee runbook §5](COMMITTEE_RUNBOOK.md#burns)).

**Positions.** `MsgCorrectPosition`, `MsgMarkImpaired`, `MsgClearImpairment`, `MsgReverseReturn`, and
`MsgClosePosition` work with no mandate in force. Governance has no deployment or restatement message: deployment needs
the custodian relationship only a committee holds, and credit that lapses while no committee is seated stays lapsed
until one is appointed and restates each position.

## 8. Market

**Policy beyond the corridor.** `MsgUpdatePolicy` sets `ConversionPolicy` whole, without the committee's corridor. Size
depth by the [depth rule](COMMITTEE_RUNBOOK.md#sizing-depth), and add capacity by depth, never by shortening
`pool_recovery_period`.

**Tobin overrides.** `MsgSetTobinTaxOverride` sets a registered asset's Tobin tax and `MsgRemoveTobinTaxOverride`
clears it. The conversion committee moves overrides between the default and its mandate's cap; overrides below the
default and removal stay with governance.

## 9. Claims

**Governance claims.** `MsgSubmitClaim` needs no mandate and charges no limit. A stablecoin claim buys its funding
inside the proposal at execution, so its `max_offer` is the worst price governance accepts then; size it from
`arkd query market buy`. The claim then waits out the same cancellation period as a committee claim.

**The veto.** `MsgCancelClaim` cancels any pending claim, a committee's included, before its closing time. The
cancellation period, a week at launch, is sized so a proposal fits inside it; keep it longer than a vote whenever
governance changes either.

## 10. Grants

Tranche proposals, contributor awards, cancellations, and committee recovery sit with the grants committee's work in
the [committee runbook](COMMITTEE_RUNBOOK.md#8-grants), since every step there interleaves with the committee's.
