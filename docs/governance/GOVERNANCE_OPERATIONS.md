# Governance operations

For governance proposal authors, committee appointers, and relayers coordinating network changes. This guide covers
opening interchain services, contract permissions, appointments, and economic operations throughout the network's life.
[Genesis](GENESIS.md) owns launch values; [economic design](../design/ECONOMIC_DESIGN.md) owns financial rules;
[economic decisions](../design/ECONOMIC_DECISIONS.md) owns the D/P history.

Governance signs each message unless its row names another signer. Before submission, query current parameters and
authority, preserve fields a whole-object update retains, and check the stated dependencies. After execution, query
the resulting state and verify the intended route or appointment before proceeding to a dependent step. If a step
fails, re-read state and correct the proposal; do not enable a dependent service to bypass an unmet prerequisite.

## 1. Launch defaults and ongoing changes

One thing is post-launch by nature: an IBC channel is a handshake with a live counterparty, so channels and everything
that rides on them cannot exist in a fresh genesis. Rate limits are keyed by channel or client ID, so they follow the
channels, and the plan makes limits a precondition for the transfer flags (D46), so the flags follow the limits.

Other initial permissions are launch choices that later governance can change. The launch artefact ships
the hub shut behind one switch, the empty allowed-client list, and the contract runtime open (D45 as amended,
`docs/design/ECONOMIC_DESIGN.md` §11.3). The following procedures apply when those services or appointments are needed.

Initial economic values and permissions are fixed in [genesis](GENESIS.md); committee appointments follow its
[first-cycle sequence](GENESIS.md#13-committee-appointments). Validator entry after launch is open and needs no
proposal ([genesis §3](GENESIS.md#3-accounts-supply-and-validator-seats)). The same roles and permissions may be
changed later through the procedures below; the contract query accept list remains a code change rather than a
governance parameter.

## 2. Opening the hub

The order is fixed by dependencies. Step 1 is the switch; steps 2 to 4 cannot be reordered; step 5 is independent of
3 and 4.

| # | Step | Message | Signer | Needs | Effect |
| --- | --- | --- | --- | --- | --- |
| 1 | Admit the client type | `ibc.core.client.v1.MsgUpdateParams` (rpc `UpdateClientParams`), `allowed_clients: ["07-tendermint"]` | governance | the decision to open | Clients, connections, channels, and v2 counterparties can be created. GMP and contract channels become reachable at the same moment (§3). ICS-20 and ICA stay behind their own flags. |
| 2 | Create clients, connections, channels | `MsgCreateClient`, `MsgConnectionOpen*`, `MsgChannelOpen*` in `ibc.core.*.v1`; for v2, `ibc.core.client.v2.MsgRegisterCounterparty`, signed by the client's creator | relayers | step 1 | Routes exist. Handshakes succeed while transfer is disabled: the flags gate packets, not channels. |
| 3 | Set rate limits | `ibc.applications.rate_limiting.v1.MsgAddRateLimit` with `denom`, `channel_or_client_id`, `max_percent_send`, `max_percent_recv`, `duration_hours`; `MsgUpdateRateLimit`, `MsgRemoveRateLimit`, `MsgResetRateLimit` | governance | step 2, for the IDs | One limit per denomination per route: `anoah` and each registered stablecoin outbound, and each inbound `ibc/…` denomination as it appears. Percentages are of the route's value over the window. Every route that will carry value is limited before step 4 (D46). |
| 4 | Enable ICS-20 | `ibc.applications.transfer.v1.MsgUpdateParams` with `send_enabled`, `receive_enabled` | governance | step 3 | Transfers flow, taxed at the ante for signed sends and at the policy router for everything else. The two flags may move separately. |
| 5 | Enable ICA | host: `ibc.applications.interchain_accounts.host.v1.MsgUpdateParams` with `host_enabled` and `allow_messages` as explicit type URLs, never `*`; controller: `ibc.applications.interchain_accounts.controller.v1.MsgUpdateParams` with `controller_enabled` | governance | step 1 | Each allowed URL is remote execution on Ark; the interchain account pays execution tax through the policy router like a contract. The controller lets Ark accounts act on other chains. |
| 6 | Admit 08-Wasm | `ibc.core.client.v1.MsgUpdateParams` adding `08-wasm`, then `ibc.lightclients.wasm.v1.MsgStoreCode` | governance | a non-CometBFT counterparty | Not planned. The module is installed dormant with no checksums (D50). |

## 3. What opens with the client type

Two surfaces have no flag of their own and are held shut only by step 1:

- **GMP.** A remote chain executes SDK messages here through an account the chain derives for it. The module has no
  params; its only message is the outbound `SendCall`. The derived account is an ordinary account with no standing
  authority, it can do only what its own balance allows, and every message it executes runs through the policy router
  and pays execution tax on a contract's terms (D48).
- **Contract channels.** A contract with IBC entry points may open its own channels on either stack. Callback delivery
  follows the callback and gas rules described in [application integration](../../app/README.md#ibc-and-wasm-integration).

Neither needs a further vote once the client type is admitted, which is why step 1 is the decision to open the hub and
not merely a preliminary.

## 4. The contract runtime

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

## 5. Ark governance operations

| Operation | Message | Signer | Notes |
| --- | --- | --- | --- |
| Appoint or replace a committee | `ark.treasury.v1.MsgSetEconomicMandate`, `ark.claims.v1.MsgSetClaimsMandate`, `ark.market.v1.MsgSetConversionMandate`, `ark.reserve.v1.MsgSetReserveMandate`, `ark.asset.v1.MsgSetEmergencyMandate`, `ark.security.v1.MsgSetSecurityMandate` | governance | The committee's shape is observed from the live account at appointment. The account must exist with its own NOAH for gas, never funded from a custody account. See §6 for account backing and appointment review. |
| Commit Reserve NOAH to the Buffer | `ark.reserve.v1.MsgFundBuffer` with the exact amount and minimum remaining balance; `MsgCommitteeFundBuffer` inside the mandate floor | governance; committee | One-way; changes no supply, quote, or pool state. There is no global floor, cap, or trigger: each proposal states its own (D27). |
| Return idle subsidy to the community pool | `ark.treasury.v1.MsgReturnSubsidy` with the exact amount and minimum remaining balance | governance | One-way from the subsidy pool into the community pool; changes no supply or target. The minimum is the proposal's stale-state guard, not a floor, and a return that would breach it fails at execution (D83). The community pool tops the subsidy pool up by an ordinary spend to its address. |
| Move the tax | `ark.treasury.v1.MsgUpdateParams` with `transfer_tax_rate`, `reference_tax_cap` | governance | The rate stays at or below the spread floor (D81). The cap is denominated in the reference unit; zero means uncapped. |
| Switch the exposure multiplier on | `ark.treasury.v1.MsgUpdatePolicy`; `MsgCommitteeUpdatePolicy` inside the mandate bounds | governance; committee | Only after the observability calibration window has produced thresholds (`docs/operations/PROTOCOL_MONITORING.md` §2.1). `m` is floored at one, so zero weights are exactly the unscaled sizing (D72). |
| Onboard the first external asset | `ark.oracle.v1.MsgAddFeed`, then `ark.reserve.v1.MsgSetRecognitionPolicy` (haircut, cap ratio, staleness window), then `ark.reserve.v1.MsgSetReserveMandate` naming the destination | governance | Price-feed sidecars must serve the symbol before the feed can be Active, which listing requires. Custody is attested: the asset sits at the destination and the committee attests the quantity (D59). The Reserve account never holds an external token (D70, amended). |
| Deploy, attest, impair, close | `ark.reserve.v1.MsgCommitteeDeploy`, `MsgCommitteeRecordUpdate`, `MsgCommitteeAttributeReturn`, `MsgCommitteeMarkImpaired`, `MsgCommitteeClosePosition`, and the governance corrections | committee; governance | The one-committee mandate (D56, D57); governance corrects records, clears impairment, and owns the recognition policy. |
| Burn Reserve holdings | `MsgCommitteeBurnPaper`, `MsgCommitteeBurnSurplus`; `ark.reserve.v1.MsgBurnReserveAssets` | committee; governance | Split burn authority (D61). The surplus bound is Treasury's required capital, which needs a complete valuation. |

## 6. Committee appointments

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

## 7. Grant tranches

The [distribution plan](DISTRIBUTION_PLAN.md) runs through the grant contract, `ark-grant` under
[contracts/grant](../../contracts/grant/README.md) (D86). The runtime is open, so anyone stores and instantiates it
with no admin; governance then funds it one tranche at a time and instructs it by sudo. Before the first tranche,
check that the code's checksum is the optimizer build cited in the review (`make contracts-optimize`) and that the
instantiate message carried the plan's figures: `arkd query wasm code-info <code-id>` and
`arkd query wasm contract-state smart <contract> '{"config":{}}'`.

**A member tranche** is one message, the pool spend to the contract. The registrar, set at instantiation or by the
sudo below, then issues grants as members are admitted; nothing about a member appears in a proposal. The contract
issues at most the window's members, 250 in any seven days at launch, each registration counting for seven days from its
block; a compromised registrar is replaced by an expedited `set_registrar`, and the window bounds what the old key can
issue meanwhile.

```json
{
  "title": "Member tranche, step 1",
  "summary": "Thirty million NOAH to the grant contract for three thousand member grants.",
  "metadata": "",
  "deposit": "1000000000000000000000anoah",
  "messages": [
    {
      "@type": "/cosmos.distribution.v1beta1.MsgCommunityPoolSpend",
      "authority": "GOV",
      "recipient": "CONTRACT",
      "amount": [{"denom": "anoah", "amount": "30000000000000000000000000"}]
    }
  ]
}
```

**A contributor grant** is the spend followed by the contract's add-grant call, in one proposal that reverts whole if
either fails; the reasoning for the band goes in the summary.

```json
{
  "title": "Founding-band grant: <name>",
  "summary": "<band, multipliers, and reasoning>",
  "metadata": "",
  "deposit": "1000000000000000000000anoah",
  "messages": [
    {
      "@type": "/cosmos.distribution.v1beta1.MsgCommunityPoolSpend",
      "authority": "GOV",
      "recipient": "CONTRACT",
      "amount": [{"denom": "anoah", "amount": "30000000000000000000000000"}]
    },
    {
      "@type": "/cosmwasm.wasm.v1.MsgSudoContract",
      "authority": "GOV",
      "contract": "CONTRACT",
      "msg": {
        "add_grant": {
          "grantee": "GRANTEE",
          "amount": "30000000000000000000000000",
          "schedule": [{"length": 31536000, "parts": 12}, {"length": 2628000, "parts": 1}, "... 36 monthly entries"],
          "seat_holder": false
        }
      }
    }
  ]
}
```

The other sudo calls take the same shape with `set_registrar`, `set_issuance_limit`, `set_member_grant`,
`set_controller`, `cancel_grant`, or `return_unallocated` in `msg`. After execution, read the contract back:
`{"totals":{}}` for what the tranche holds, `{"grant":{"id":N}}` for a grant, `{"releasable":{"id":N}}` for what
the rules allow now. Anyone may then send `{"release":{"id":N}}` once the grantee has registered a fresh address.

**A grant to a seat holder** is the same proposal with `seat_holder` true, the grantee abstaining. The bloc rule
releases nothing to a seat holder until public bonded stake passes the block line
([plan §4](DISTRIBUTION_PLAN.md#4-distribution)), so the grant escrows whole and anyone releases it, tranche by
tranche, as bonded stake grows.
