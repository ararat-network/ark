# Governance operations

For governance proposal authors and committee appointers. This guide covers contract permissions, appointments, and
economic operations throughout the network's life.
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
| Appoint or replace a committee | `ark.treasury.v1.MsgSetEconomicMandate`, `ark.claims.v1.MsgSetClaimsMandate`, `ark.market.v1.MsgSetConversionMandate`, `ark.reserve.v1.MsgSetReserveMandate`, `ark.asset.v1.MsgSetEmergencyMandate`, `ark.security.v1.MsgSetSecurityMandate`, `ark.disbursement.v1.MsgSetGrantsMandate` (see §5) | governance | The committee's shape is observed from the live account at appointment. The account must exist with its own NOAH for gas, never funded from a custody account. See §4 for account backing and appointment review. |
| Appoint, rotate, or disable the registrar | `ark.disbursement.v1.MsgCommitteeRegistrar` under the committee's term | committee | One operational key, bounded by the open tranche alone, for a window inside the committee's; every appointment advances the registrar term its batches carry, and a committee appointment clears it. See §5. |
| Commit Reserve NOAH to the Buffer | `ark.reserve.v1.MsgFundBuffer` with the exact amount and minimum remaining balance; `MsgCommitteeFundBuffer` up to the Buffer's shortfall and inside the mandate floor | governance; committee | One-way; changes no supply, quote, or pool state. Governance's message has no global floor, cap, or trigger: each proposal states its own. The committee's is sized in the [economic committee runbook](ECONOMIC_COMMITTEE_RUNBOOK.md#3-transfers-into-the-buffer-and-insurance). |
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

One appointment is the committee's rather than governance's: the grants committee names the Disbursement registrar
under its own term with `MsgCommitteeRegistrar`, for a window of block time inside its own. The registrar is an
operational key, hardware-backed on the member platform's signing service, that registers batches as admissions
clear; the committee may name itself. The open tranche is its only bound, and nothing it registers pays before the
two-week hold ends: open each step in quarters, compare the chain's registrations against the
platform's admission records daily, and the moment the key is suspect rotate it by reappointment, which advances the
registrar term without a vote, and suspend what it registered before the hold ends. Every committee appointment clears the registrar, so a replaced or
reappointed committee names its registrar again in its first transaction; governance reaches the registrar only
through the committee.

For Security and Asset emergency mandates, use threshold signatures collected off-chain. A public voting contract
would disclose the target during deliberation; ordinary public transaction propagation can still expose a signed act,
so the [emergency submission runbook](EMERGENCY_SUBMISSION_RUNBOOK.md) remains relevant.

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

## 5. Grant tranches

The [disbursement plan](DISBURSEMENT_PLAN.md) uses the native [Disbursement module](../../x/disbursement/README.md),
which holds the member and contributor accounts from genesis. Inspect `arkd query disbursement params` and
`arkd query disbursement balance anoah`, which shows each account's pool and the member tranche, before proposing. `GOV`
below is the governance authority; a tranche opens the member pool alone, since the contributor pool has none. No
contract upload, instantiation, or sudo is involved. Amounts are denomination base units and timestamps are block time.

**A member tranche** is a `MsgOpenTranche` on the member account beside a `MsgUpdateParams` carrying the step's member
amount; it moves no coins, and members already registered keep the terms they joined under. Appoint the grants committee
with `/ark.disbursement.v1.MsgSetGrantsMandate`, naming the exact committee account, a half-open window of block time in
Unix seconds, and its award power: a compensation allowance in the reference unit; nothing pays before one period after
an award, a fortnight at launch in the example; an appointment that delegates no awards leaves the allowance zero.
Ownership is awarded by proposal only. The appointment is its own message, so operational params never travel with it;
every appointment advances the term the committee's transactions must carry, shown by
`query disbursement grants-mandate`. Launch artifacts set a 20,000 NOAH member amount and leave the mandate disabled;
the ownership cap is a param, 15,000,000 NOAH at launch, and founding identities are not.

```json
{
  "@type": "/ark.disbursement.v1.MsgSetGrantsMandate",
  "authority": "GOV",
  "committee": "COMMITTEE",
  "activation_time": "ACTIVATION",
  "expiry_time": "EXPIRY",
  "compensation_allowance": "250000000000000000000000"
}
```

```json
{
  "title": "Member tranche, step 2, first quarter",
  "summary": "Fifteen million NOAH for one thousand member grants of fifteen thousand, the first quarter of step 2's member budget.",
  "metadata": "",
  "deposit": "1000000000000000000000anoah",
  "messages": [
    {
      "@type": "/ark.disbursement.v1.MsgUpdateParams",
      "authority": "GOV",
      "params": {
        "member_amount": "15000000000000000000000",
        "member_schedule": {
          "first_length": "1209600", "first_weight": "13",
          "period_length": "2419200", "period_weight": "9", "periods": 13
        },
        "compensation_cap": "1000000000000000000000000",
        "compensation_window": "31449600",
        "compensation_period": "1209600",
        "ownership_cap": "15000000000000000000000000",
        "ownership_schedule": {
          "first_length": "31449600", "first_weight": "26",
          "period_length": "1209600", "period_weight": "1", "periods": 78
        }
      }
    },
    {
      "@type": "/ark.disbursement.v1.MsgOpenTranche",
      "authority": "GOV",
      "amount": "15000000000000000000000000"
    }
  ]
}
```

`MsgUpdateParams` replaces the whole params object, so copy the current values from `arkd query disbursement params`
and change only `member_amount`; the compensation and ownership terms stay unless one of them is the proposal. Step 1
opens at the launch artefact's amount, so its proposals need only the tranche; each later step's first tranche carries
both messages and its later ones the tranche alone, and the plan's schedule runs 20,000, 15,000, 10,000, 7,500, 5,000.
Open each step's member budget in quarters: 7.5M at step 1, 375 members, then 15M, 22.5M, 22.5M, and 49.8M, so
twenty tranche votes cover the distribution. The open tranche is all a stolen registrar key can commit, and the two-week
hold is the time to suspend it; a step that fills fast may split a quarter in two.

The committee appoints the registrar with
`arkd tx disbursement committee-registrar --expected-term TERM --registrar REGISTRAR --activation-time ACTIVATION --expiry-time EXPIRY --from COMMITTEE`;
`query disbursement registrar-mandate` shows the appointment, its term, and whether it is active. The registrar then
signs
`arkd tx disbursement register-members --expected-term REGISTRAR_TERM --addresses ADDRESS --addresses ADDRESS --reference REF --from REGISTRAR`,
where the reference names the batch's admission record and carries no personal data; the member roll does not pass
through a proposal. Registration reserves the whole grant and pays nothing; the block sends the tenth when the two-week
hold ends. Look up the permanent grant ID with `arkd query disbursement member ADDRESS`. The chain pays each later
period at the end of the block its boundary falls in, to the recorded payee; nothing is signed to receive it, and
`grant ID` shows `due_time`, the next look.

**A faked member** can be paused with `arkd tx disbursement committee-suspend --expected-term TERM --addresses ADDRESS --from COMMITTEE`.
Governance then cancels by permanent grant ID:

```json
{
  "@type": "/ark.disbursement.v1.MsgCancelGrants",
  "authority": "GOV",
  "grant_ids": ["1", "2"]
}
```

Cancellation retains earned unpaid principal at the effective suspension time, or at execution time if there is no
effective suspension. It returns unearned member funds to the open member tranche for the next member. It attempts no member payment;
retained debt remains payable even after cancellation. `MsgCommitteeReinstate` from the committee or `MsgReinstateMembers`
from governance resumes a suspended grant and permits catch-up; neither can reopen a cancelled grant. A compromised
committee can reinstate members before cancellation, so follow the disbursement plan's prompt cancellation process.

**Registrar recovery** is one committee transaction: `committee-registrar` naming the new key, or an empty one to
disable, advances the registrar term, so nothing prepared under the old key lands afterwards. The committee then
suspends the fakes that `arkd query disbursement grants --registrar-term OLD_REGISTRAR_TERM` lists, before their hold
ends, and governance cancels them by ID as below; cancelled inside the hold, a fake returns its whole grant to the
tranche.

**Committee recovery** takes two proposals. The first carries `MsgSetGrantsMandate` appointing the new committee,
which clears the registrar the old key appointed in the same write, `MsgCancelGrants` for
the old term's awards, a hundred IDs a message, and `MsgReinstateMembers` for any members the old
key wrongly suspended, a hundred addresses a message. `arkd query disbursement grants --committee-term OLD_TERM` lists the
awards and `--registrar-term OLD_REGISTRAR_TERM` the registrations, each `OLD_TERM` being the term its query showed before the vote; legitimate ones can stay off the list. The
appointment advances the term, so nothing prepared under the old key lands afterwards. Once it executes, list the old
term again and cancel, in a second, expedited proposal, any award the key made while the first vote was open: nothing
pays within a period of an award, at least two weeks, and the two votes take about three days at launch. Neither proposal
reverses payments or touches the new term, and suspensions stand until reinstated. Governance cancels known sybils among the
listed registrations in the first proposal. An empty committee disables the mandate without changing existing grants, and a lapsed window does
the same until the next appointment; disabling or replacing the committee clears its registrar, and the new committee
names one again before registration resumes.

**A contributor award** is a `MsgGrantOwnership` or `MsgCompensate` drawing on the contributor pool, which has no
tranche; the step's contributor budget is the plan's guideline, not a limit; an award past it states the overrun in the
summary. The award carries no schedule: ownership takes the shape in params, a one-year cliff then biweekly for three
years, and `start_time` places the clock, no earlier than execution. The proposal summary states the work, its period,
and why this amount, against the founder's 15M grant and the grants already made; `reference` identifies the approved
work or agreement on the permanent grant itself. The pool must cover the whole amount before it starts.

```json
{
  "title": "Contributor ownership award",
  "summary": "<the work, its period, and why this amount>",
  "metadata": "",
  "deposit": "1000000000000000000000anoah",
  "messages": [
    {
      "@type": "/ark.disbursement.v1.MsgGrantOwnership",
      "authority": "GOV",
      "beneficiary": "BENEFICIARY",
      "amount": "1000000000000000000000000",
      "start_time": "START_UNIX_SECONDS",
      "reference": "<approved contributor agreement>"
    }
  ]
}
```

The clock starts at `start_time`, for ownership no earlier than execution. No payment occurs until a period has
elapsed, and ownership still checks the cap. Founding status is read from genesis seat records; an award has no `seat_holder`
input. The beneficiary initially controls the contributor payee, and later awards preserve the existing controller.
Use `arkd tx disbursement set-controller --beneficiary ADDRESS --controller NEW_CONTROLLER --from CURRENT_CONTROLLER` to
rotate it. Governance can also submit `MsgSetController`, with `sender: GOV`, for recovery.

**Compensation** is `MsgCompensate`, with the funded denomination and `periods`: that many fortnights in equal shares,
one for a bounty, together at most ten years. Its clock may start up to a quarter back, so a stream aligns to the day a
hire started and pays the elapsed fortnights at the first look. Its denomination is NOAH or an active asset in the Asset
registry; `max_offer` bounds the NOAH converted to fund a stablecoin award. Stablecoin compensation is fully funded in
that denomination, untaxed, and outside NOAH ownership caps. It does not promise a conversion rate. A denomination's
later suspension prevents new awards without erasing already funded entitlement. One record's compensation is at most
`compensation_cap` in the reference unit a `compensation_window`, by proposal or committee alike, valued at the fresh
oracle rates when awarded, a stream counting at its yearly rate while it runs and cancelled awards for a window after
them, so an award fails while the oracle has no rate for its denomination or the reference unit.

**Committee awards** pay pay streams and bounties without a proposal; ownership is a proposal only. Within its
appointment the committee signs `MsgCommitteeCompensate` with the beneficiary, amount, `start_time`, `periods`, and a work
reference; nothing pays before one period after the award, whatever the start time, and a stablecoin award draws on custody first and converts
NOAH from the contributor pool for the rest, within the award's `max_offer`, like a governance award. The
compensation allowance caps the term's total at reference value, cancelled awards included.
`query disbursement grants-mandate` shows what the live term has used and what remains, and `query disbursement grants
--committee-term TERM` lists its awards.

**Stablecoin funding** happens in the award. A stablecoin award draws uncommitted stablecoin in the contributor
account first and converts NOAH from the contributor pool for the rest, through Market like any trader's swap,
inside the message that creates the grant. `max_offer` is the most NOAH the award may spend; the module sizes the
smallest offer that funds it, and an award whose cap buys less than it needs fails whole. Size the cap from
`arkd query market buy AMOUNTausd anoah`, Market's reverse quote for the whole pay; the award converts less if custody
covers part. A proposal executes after its vote, so its cap is the worst price governance accepts then.

```json
{
  "@type": "/ark.disbursement.v1.MsgCompensate",
  "authority": "GOV",
  "beneficiary": "BENEFICIARY",
  "amount": {"denom": "ausd", "amount": "200000000000000000000000"},
  "start_time": "START_UNIX_SECONDS",
  "periods": 52,
  "reference": "<approved work>",
  "max_offer": "260000000000000000000000"
}
```

Market settles the conversion at EndBlock like any trader's, and the grant journal's `converted` entry under the grant records
the output and the NOAH offer it cost; Market's own event carries the fee. Stablecoin a cancelled award leaves in custody funds the next award before
anything converts.

**Cancellation and destination recovery.** `MsgCancelGrants` applies to any grant kind. Contributor cancellation
returns unearned NOAH to the contributor pool, leaves unearned stablecoins in custody for the next award, and
retains accrued debt; an ownership cancel also returns the unearned part to the record's room under the cap.
A cancellation sends nothing itself, so a blocked destination cannot veto it; the block pays the retained debt
afterwards and defers a destination that refuses, which `EventPaymentDeferred` reports. A contributor controller, or the original
registered member key for a member grant, signs
`arkd tx disbursement set-payee --grant-id ID --payee ADDRESS --from CONTROLLER_OR_MEMBER`. The beneficiary identity and all
historical payments remain unchanged. The committee has no destination-recovery authority.

**Audit after execution.** Use `arkd query disbursement` with `grant ID`, `beneficiary ADDRESS`, `balance DENOM`, and
`grant-journal --grant-id ID`. The grant journal holds each grant's creation, conversion, payments and their payees, cancellation,
suspensions, and payee changes; `beneficiary-journal ADDRESS` lists a beneficiary's controller changes; governance actions
are recorded in their proposals. All list queries use key pagination; maximum page size is 100. Read all pages when reconciling.
`MsgReturnUnallocated` returns from the contributor account to the community pool without touching any grant's
reserved principal: NOAH from its pool, any other denomination from custody outside grants. The member
account has no exit.

---

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
   [depth rule](ECONOMIC_COMMITTEE_RUNBOOK.md#conversion-depth) with the live price and the liquid NOAH outstanding,
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

Stablecoin compensation in Disbursement becomes available at step 3 (§5).
