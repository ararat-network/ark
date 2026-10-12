# Governance operations

For governance proposal authors and committee appointers. This guide covers contract permissions, appointments, and
governance's economic operations throughout the network's life; the [committee runbook](COMMITTEE_RUNBOOK.md) covers
what an appointed committee does and the grants programme.
[Genesis](GENESIS.md) owns launch values; [economic design](../economics/ECONOMIC_DESIGN.md) owns financial rules.

Governance signs each message unless its row names another signer. Before submission, query current parameters and
authority, preserve fields a whole-object update retains, and check the stated dependencies. After execution, query
the resulting state and verify the intended route or appointment before proceeding to a dependent step. If a step
fails, re-read state and correct the proposal; do not enable a dependent service to bypass an unmet prerequisite.

## 1. Launch defaults and ongoing changes

Initial permissions are launch choices that later governance can change. The launch artefact ships the contract
runtime open (`docs/economics/ECONOMIC_DESIGN.md` §5.5). The following procedures apply when those services or
appointments are needed.

Initial economic values and permissions are fixed in [genesis](GENESIS.md); committee appointments follow its
[first-cycle sequence](GENESIS.md#13-committee-appointments). Validator entry after launch is open and needs no
proposal ([genesis §3](GENESIS.md#3-accounts-supply-and-validator-seats)). The same roles and permissions may be
changed later through the procedures below; the contract query accept list remains a code change rather than a
governance parameter.

## 2. The contract runtime

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

## 3. Ark governance operations

| Operation | Message | Signer | Notes |
| --- | --- | --- | --- |
| Appoint or replace a committee | `ark.treasury.v1.MsgSetEconomicMandate`, `ark.claims.v1.MsgSetClaimsMandate`, `ark.market.v1.MsgSetConversionMandate`, `ark.reserve.v1.MsgSetReserveMandate`, `ark.asset.v1.MsgSetEmergencyMandate`, `ark.security.v1.MsgSetSecurityMandate`, `ark.disbursement.v1.MsgSetGrantsMandate` | governance | The committee's shape is observed from the live account at appointment. The account must exist with its own NOAH for gas, never funded from a custody account. See §4 for account backing and appointment review. |
| Appoint, rotate, or disable the registrar | `ark.disbursement.v1.MsgCommitteeRegistrar` under the committee's term | committee | One operational key, bounded by the open tranche alone, for a window inside the committee's; every appointment advances the registrar term its batches carry, and a committee appointment clears it. See the [committee runbook](COMMITTEE_RUNBOOK.md#8-grants). |
| Commit Reserve NOAH to the Buffer | `ark.reserve.v1.MsgFundBuffer` with the exact amount and minimum remaining balance; `MsgCommitteeFundBuffer` up to the Buffer's shortfall and inside the mandate floor | governance; committee | One-way; changes no supply, quote, or pool state. Governance's message has no global floor, cap, or trigger: each proposal states its own. The committee's is sized in the [committee runbook](COMMITTEE_RUNBOOK.md#transfers-into-the-buffer-and-insurance). |
| Return idle subsidy to the community pool | `ark.treasury.v1.MsgReturnSubsidy` with the exact amount and minimum remaining balance | governance | One-way from the subsidy pool into the community pool; changes no supply or target. The minimum is the proposal's stale-state guard, not a floor, and a return that would breach it fails at execution. The community pool tops the subsidy pool up by an ordinary spend to its address. |
| Move the tax | `ark.treasury.v1.MsgUpdateParams` with `transfer_tax_rate`, `reference_tax_cap` | governance | The rate stays at or below the spread floor. The cap is denominated in the reference unit; zero means uncapped. |
| Switch the exposure multiplier on | `ark.treasury.v1.MsgUpdatePolicy`; `MsgCommitteeUpdatePolicy` inside the mandate bounds | governance; committee | Only after the observability calibration window has produced thresholds (`docs/operations/PROTOCOL_MONITORING.md` §2.1). `m` is floored at one, so zero weights are exactly the unscaled sizing. |
| Onboard the first external asset | `ark.oracle.v1.MsgAddFeed`, then `ark.reserve.v1.MsgSetRecognitionPolicy` (haircut, maximum credit per NOAH, rate and attestation windows), then `ark.reserve.v1.MsgSetReserveMandate` naming the destination | governance | Price-feed sidecars must serve the symbol before the feed can be Active, which listing requires. Custody is attested: the asset sits at the destination and the committee attests the quantity. The Reserve account never holds an external token. |
| Deploy, attest, impair, close | `ark.reserve.v1.MsgCommitteeDeploy`, `MsgCommitteeRecordUpdate`, `MsgCommitteeAttributeReturn`, `MsgCommitteeReverseReturn`, `MsgCommitteeMarkImpaired`, `MsgCommitteeClearImpairment`, `MsgCommitteeCorrectPosition`, `MsgCommitteeClosePosition`; governance's `MsgCorrectPosition`, `MsgClearImpairment`, `MsgReverseReturn`, `MsgMarkImpaired`, `MsgClosePosition` | committee; governance | The one-committee mandate. Governance's versions work with no mandate in force; governance alone owns the recognition policy. |
| Burn Reserve holdings | `MsgCommitteeBurnPaper`, `MsgCommitteeBurnSurplus`; `ark.reserve.v1.MsgBurnReserveAssets` | committee; governance | Split burn authority. The surplus bound is Treasury's required capital, which needs a complete valuation. |

## 4. Committee appointments

Default mandates are disabled. Governance can operate without a committee; appointment delegates a bounded faster
path and never becomes a prerequisite for governance's own messages. Choose appointments from incident exposure,
not chain age: security response may need a fast path before the deliberative mandates do.

An ordinary single-key account can be appointed, with its concentration recorded by `CommitteeShape`. A threshold
multisig can be appointed through the same message; its key should already be registered so governance can observe
its backing. Replacing the address or reappointing it advances the term and re-records shape. The existing multisig
integration tests exercise real ante authentication; no native committee module is needed.

1. Prepare the root account and its independent gas funding. Know what appointment will record: a key-backed
   account's kind and K-of-N, a contract only that it is one.
2. Draft the domain's `MsgSet*Mandate` with its activation and expiry times, Unix seconds of block time, and its
   policy bounds, allowance, floor, or destinations. Write the dates the term should run between; block pace does not
   move them.
3. After execution, verify the emitted appointment and query state, including term and shape. Construct actions for
   that exact term and send only while the appointment is active.
4. At replacement, review outstanding claims and positions as well as term usage. Pending commitments outlive terms;
   replacing the charter does not erase their custody or history.

Once appointed, a committee works from the [committee runbook](COMMITTEE_RUNBOOK.md), which also covers how its
transactions are signed and submitted.

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

Other engineering follow-ups are tracked in [future changes](../direction/FUTURE_CHANGES.md).

## 5. Exposure alerts

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

## 6. Opening conversion

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
   liquid NOAH is distributed need no vote (§4, [genesis §13](GENESIS.md#13-committee-appointments)). The committee
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
