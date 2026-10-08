# Governance operations

For governance proposal authors and committee appointers. This guide covers contract permissions, appointments, and
economic operations throughout the network's life.
[Genesis](GENESIS.md) owns launch values; [economic design](../design/ECONOMIC_DESIGN.md) owns financial rules;
[economic decisions](../design/ECONOMIC_DECISIONS.md) owns the D/P history.

Governance signs each message unless its row names another signer. Before submission, query current parameters and
authority, preserve fields a whole-object update retains, and check the stated dependencies. After execution, query
the resulting state and verify the intended route or appointment before proceeding to a dependent step. If a step
fails, re-read state and correct the proposal; do not enable a dependent service to bypass an unmet prerequisite.

## 1. Launch defaults and ongoing changes

Initial permissions are launch choices that later governance can change. The launch artefact ships the contract
runtime open (`docs/design/ECONOMIC_DESIGN.md` §11.3). The following procedures apply when those services or
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
| Appoint or replace a committee | `ark.treasury.v1.MsgSetEconomicMandate`, `ark.claims.v1.MsgSetClaimsMandate`, `ark.market.v1.MsgSetConversionMandate`, `ark.reserve.v1.MsgSetReserveMandate`, `ark.asset.v1.MsgSetEmergencyMandate`, `ark.security.v1.MsgSetSecurityMandate` | governance | The committee's shape is observed from the live account at appointment. The account must exist with its own NOAH for gas, never funded from a custody account. See §4 for account backing and appointment review. |
| Commit Reserve NOAH to the Buffer | `ark.reserve.v1.MsgFundBuffer` with the exact amount and minimum remaining balance; `MsgCommitteeFundBuffer` inside the mandate floor | governance; committee | One-way; changes no supply, quote, or pool state. There is no global floor, cap, or trigger: each proposal states its own (D27). |
| Return idle subsidy to the community pool | `ark.treasury.v1.MsgReturnSubsidy` with the exact amount and minimum remaining balance | governance | One-way from the subsidy pool into the community pool; changes no supply or target. The minimum is the proposal's stale-state guard, not a floor, and a return that would breach it fails at execution (D83). The community pool tops the subsidy pool up by an ordinary spend to its address. |
| Move the tax | `ark.treasury.v1.MsgUpdateParams` with `transfer_tax_rate`, `reference_tax_cap` | governance | The rate stays at or below the spread floor (D81). The cap is denominated in the reference unit; zero means uncapped. |
| Switch the exposure multiplier on | `ark.treasury.v1.MsgUpdatePolicy`; `MsgCommitteeUpdatePolicy` inside the mandate bounds | governance; committee | Only after the observability calibration window has produced thresholds (`docs/operations/PROTOCOL_MONITORING.md` §2.1). `m` is floored at one, so zero weights are exactly the unscaled sizing (D72). |
| Onboard the first external asset | `ark.oracle.v1.MsgAddFeed`, then `ark.reserve.v1.MsgSetRecognitionPolicy` (haircut, cap ratio, staleness window), then `ark.reserve.v1.MsgSetReserveMandate` naming the destination | governance | Price-feed sidecars must serve the symbol before the feed can be Active, which listing requires. Custody is attested: the asset sits at the destination and the committee attests the quantity (D59). The Reserve account never holds an external token (D70, amended). |
| Deploy, attest, impair, close | `ark.reserve.v1.MsgCommitteeDeploy`, `MsgCommitteeRecordUpdate`, `MsgCommitteeAttributeReturn`, `MsgCommitteeMarkImpaired`, `MsgCommitteeClosePosition`, and the governance corrections | committee; governance | The one-committee mandate (D56, D57); governance corrects records, clears impairment, and owns the recognition policy. |
| Burn Reserve holdings | `MsgCommitteeBurnPaper`, `MsgCommitteeBurnSurplus`; `ark.reserve.v1.MsgBurnReserveAssets` | committee; governance | Split burn authority (D61). The surplus bound is Treasury's required capital, which needs a complete valuation. |

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
2. Draft the domain's `MsgSet*Mandate` with activation/expiry and its policy bounds, allowance, floor, or destinations.
3. After execution, verify the emitted appointment and query state, including term and shape. Construct actions for
   that exact term and send only while the appointment is active.
4. At replacement, review outstanding claims and positions as well as term usage. Pending commitments outlive terms;
   replacing the charter does not erase their custody or history.

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

Other engineering follow-ups are tracked in [future changes](../direction/FUTURE_CHANGES.md#4-capital-and-protocol-follow-ups).

## 5. Grant tranches

The [disbursement plan](DISBURSEMENT_PLAN.md) uses the native [Disbursement module](../../x/disbursement/README.md) (D87,
D88), which holds the member and contributor pools from genesis. Inspect `arkd query disbursement params` and
`arkd query disbursement balance anoah`, which shows both pools and their open tranches, before proposing. `GOV` below
is the governance authority. No contract upload, instantiation, or sudo is involved. Amounts are denomination base
units and timestamps are block time.

**A member tranche** is a `MsgOpenTranche` on the member pool; it moves no coins. Appoint the grants committee
with `/ark.disbursement.v1.MsgSetGrantsMandate`, naming the exact committee account, a half-open height window, and
its compensation power: a per-denomination allowance for the term and the minimum first period of an award, one month
in the example; an appointment without compensation leaves both empty. The appointment is its own message, so
operational params never travel with it; every appointment advances the term the committee's transactions must carry,
shown by `query disbursement grants-mandate`. Launch artifacts set 250 members per rolling seven days and leave the
mandate disabled; ownership policy and founding identities are not operational params.

```json
{
  "@type": "/ark.disbursement.v1.MsgSetGrantsMandate",
  "authority": "GOV",
  "committee": "COMMITTEE",
  "activation_height": "ACTIVATION",
  "expiry_height": "EXPIRY",
  "compensation_allowance": [
    {"denom": "anoah", "amount": "500000000000000000000000"},
    {"denom": "ausd", "amount": "250000000000000000000000"}
  ],
  "min_first_period": "2628000"
}
```

```json
{
  "title": "Member tranche, step 1",
  "summary": "Thirty million NOAH for three thousand member grants.",
  "metadata": "",
  "deposit": "1000000000000000000000anoah",
  "messages": [{
    "@type": "/ark.disbursement.v1.MsgOpenTranche",
    "authority": "GOV",
    "pool": "POOL_MEMBERS",
    "amount": "30000000000000000000000000"
  }]
}
```

The committee signs `arkd tx disbursement committee-register --expected-term TERM --addresses ADDRESS --addresses ADDRESS --from COMMITTEE`;
the member roll does not pass through a proposal. Registration sends the tenth immediately and reserves the rest. Look up the permanent
grant ID with `arkd query disbursement member ADDRESS`. Anyone can call
`arkd tx disbursement release --grant-ids ID,ID --from CALLER`; payment always goes to each recorded payee.

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

**Committee recovery** uses one proposal with three messages: `MsgSetGrantsMandate` appointing the new committee,
`/ark.disbursement.v1.MsgVoidSuspensions` with `authority: GOV` and `term: OLD_TERM`, the term the mandate query showed
before the vote, and `/ark.disbursement.v1.MsgCancelTermGrants` with the same term. The appointment advances the term,
so nothing prepared under the old key lands afterwards; the void invalidates every suspension made under the old term in
one write, and the term cancel cancels every unfinished award of the old term, including any the key made while the
vote was open. Neither reverses payments nor touches the new term. The live term can never be voided: to void a sitting
committee's suspensions, re-appoint the same account and void the previous term in the same proposal. The new committee
reviews genuine suspension cases, and governance re-awards any legitimate compensation the term cancel caught. An empty
committee disables the mandate without changing existing grants, and a lapsed window does the same until the next
appointment.

**A contributor award** is a `MsgCreateGrant` drawing on the open contributor tranche. When the step's tranche lacks
room, open it in the same proposal, so both execute or neither does. The example below is a complete one-year cliff
schedule; a disbursement-plan ownership award uses its full 37-period schedule: first
`{"length":"31536000","parts":"12"}`, then 36 entries of `{"length":"2628000","parts":"1"}`.
The proposal summary argues the band and multipliers; `reference` identifies the approved work or agreement on the
permanent grant itself. The open tranche must cover the complete schedule before it starts.

```json
{
  "title": "Contributor ownership award",
  "summary": "<approved work, band, and rationale>",
  "metadata": "",
  "deposit": "1000000000000000000000anoah",
  "messages": [
    {
      "@type": "/ark.disbursement.v1.MsgOpenTranche",
      "authority": "GOV",
      "pool": "POOL_CONTRIBUTORS",
      "amount": "1000000000000000000000000"
    },
    {
      "@type": "/ark.disbursement.v1.MsgCreateGrant",
      "authority": "GOV",
      "kind": "GRANT_KIND_OWNERSHIP",
      "beneficiary": "BENEFICIARY",
      "amount": {"denom": "anoah", "amount": "1000000000000000000000000"},
      "schedule": [{"length": "31536000", "parts": "1"}],
      "reference": "<approved contributor agreement>"
    }
  ]
}
```

The clock starts at proposal execution. No payment occurs until a period elapses, and ownership still checks the live
individual and founder-bloc limits. Founding status is read from genesis seat records; an award has no `seat_holder`
input. The beneficiary initially controls the contributor payee, and later awards preserve the existing controller.
Use `arkd tx disbursement set-controller --beneficiary ADDRESS --controller NEW_CONTROLLER --from CURRENT_CONTROLLER` to
rotate it. Governance can also submit `MsgSetController`, with `sender: GOV`, for recovery.

**Compensation** uses `GRANT_KIND_COMPENSATION` with the funded denomination and agreed schedule, such as twenty-four
monthly periods. Governance first adds an active native stablecoin to `params.compensation_denoms` when needed.
Stablecoin compensation is fully funded in that denomination, untaxed, and outside NOAH ownership caps. It does not
promise a conversion rate. A denomination's later suspension or removal from the allowlist prevents new awards without
erasing already funded entitlement.

**Committee compensation** pays pay streams and bounties without a proposal. Within its appointment the committee signs
`MsgCommitteeCompensate` with the beneficiary, amount, schedule, and a work reference; the first period must be at
least the appointment's minimum, and the award draws on the open contributor tranche for NOAH or on stablecoin
custody, like a governance award. The term's allowance caps the total, cancelled awards included, and at most 100 awards
fit in a term. A founding seat holder's pay stays a governance proposal, with the grantee abstaining.
`query disbursement grants-mandate` shows what the live term has used, and `query disbursement grants --mandate-term
TERM` lists its awards.

**Stablecoin funding** converts contributor NOAH. The proposal authorises an order from the open contributor tranche
with the worst spread it accepts; the cap must sit above Market's 2% floor, and each authorisation of the same
denomination adds NOAH and restates the cap:

```json
{
  "@type": "/ark.disbursement.v1.MsgAuthoriseConversion",
  "authority": "GOV",
  "denom": "ausd",
  "amount": "200000000000000000000000",
  "max_spread": "0.025000000000000000"
}
```

Anyone then executes it in ordinary transactions, never inside a proposal:
`arkd tx disbursement convert --denom ausd --amount AMOUNT --from CALLER`. Market settles each fill like any trader's,
and the stablecoin lands in disbursement custody, where a later `MsgCreateGrant` draws on it. A fill whose realised
spread exceeds the cap fails, so a large order converts as the pool recovers; size fills to Market's depth. Follow
progress with `arkd query disbursement conversion-orders`. `MsgCancelConversion` returns the rest to the open tranche.

**Cancellation and destination recovery.** `MsgCancelGrants` applies to any grant kind. Contributor cancellation
returns unearned NOAH to the open contributor tranche, leaves unearned stablecoins in custody for the next award, and
retains accrued debt, with ownership limits still applied.
No cancellation pays the recipient, so a blocked destination cannot veto it. A contributor controller, or the original
registered member key for a member grant, signs
`arkd tx disbursement set-payee --grant-id ID --payee ADDRESS --from CONTROLLER_OR_MEMBER`. The beneficiary identity and all
historical payments remain unchanged. The committee has no destination-recovery authority.

**Audit after execution.** Use `arkd query disbursement` with `grant ID`, `releasable ID`, `balance DENOM`, `issuance`,
`conversion-orders`, and `journal --grant-id ID`. The journal includes original terms, approvals, destinations, payments, cancellations,
and recovery actions. All list queries use key pagination; maximum page size is 100. Read all pages when reconciling.
`MsgReturnUnallocated` returns to the community pool without touching any grant's reserved principal: with `pool`
unspecified it returns custody outside the pools and orders, with `POOL_CONTRIBUTORS` it returns contributor NOAH,
unopened first, and the member pool has no exit.

---

## 6. Opening conversion

Conversion is closed at launch: the artefact registers the eleven price feeds and no asset, so Market has no `ACTIVE`
ask to mint (D95). It opens in this order, once a venue trades NOAH with real depth.

1. **Sidecars read the venue.** Operators add NOAH markets to their provider configs in one coordinated window, not as
   each notices a listing. A live sample replaces the bootstrap NOAH price on every sidecar that carries the venue, and
   the `ark.pricefeed.bootstrap.price.uses` counter falling to zero shows the switch. From that block the NOAH gas
   factor tracks the live rate and fees in NOAH move to their XDR-targeted level; clients that build fees from the gas
   price query follow on their own ([client fees](../clients/CLIENT_FEES.md)).
2. **Confirm the depth.** The artefact sets `conversion_policy.base_pool` to 100,000 XDR. Run the
   [depth rule](ECONOMIC_COMMITTEE_RUNBOOK.md#conversion-depth) with the live price and the liquid NOAH outstanding
   (D104), and change the depth before opening, by `MsgUpdatePolicy`, only if the rule gives a reason: depth is the
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
