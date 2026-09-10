# Governance operations

For governance proposal authors, committee appointers, and relayers coordinating network changes. This guide covers
opening interchain services, contract permissions, appointments, and economic operations throughout the network's life.
[Genesis](GENESIS.md) owns launch values; [economic design](ECONOMIC_DESIGN.md) owns financial rules;
[economic decisions](ECONOMIC_DECISIONS.md) owns the D/P history.

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
`docs/ECONOMIC_DESIGN.md` §11.3). The following procedures apply when those services or appointments are needed.

Initial economic values, committee appointments, and permissions belong in [genesis](GENESIS.md#12-open-decisions)
when chosen before launch. The same roles and permissions may be changed later through the procedures below; the
contract query accept list remains a code change rather than a governance parameter.

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
  follows the callback and gas rules described in [application integration](../app/README.md#ibc-and-wasm-integration).

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
| Move the tax | `ark.treasury.v1.MsgUpdateParams` with `transfer_tax_rate`, `reference_tax_cap` | governance | The rate stays at or below the spread floor (D81). The cap is denominated in the reference unit; zero means uncapped. |
| Switch the exposure multiplier on | `ark.treasury.v1.MsgUpdatePolicy`; `MsgCommitteeUpdatePolicy` inside the mandate bounds | governance; committee | Only after the observability calibration window has produced thresholds (`docs/PROTOCOL_MONITORING.md` §2.1). `m` is floored at one, so zero weights are exactly the unscaled sizing (D72). |
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

1. Prepare the root account and its independent gas funding. Inspect what the current shape query can establish.
2. Draft the domain's `MsgSet*Mandate` with activation/expiry and its policy bounds, allowance, floor, or destinations.
3. After execution, verify the emitted appointment and query state, including term and shape. Construct actions for
   that exact term and send only while the appointment is active.
4. At replacement, review outstanding claims and positions as well as term usage. Pending commitments outlive terms;
   replacing the charter does not erase their custody or history.

For Security and Asset emergency mandates, use threshold signatures collected off-chain. A public voting contract
would disclose the target during deliberation; ordinary public transaction propagation can still expose a signed act,
so the [emergency submission runbook](EMERGENCY_SUBMISSION_RUNBOOK.md) remains relevant.

Routine staff authz and deliberative cw3/cw4 roots remain deferred behind the tests and observation preconditions in
[future changes](FUTURE_CHANGES.md#5-committee-account-evolution). Their risks are not solved by Wasmd being installed.
Other engineering follow-ups are tracked in [future changes](FUTURE_CHANGES.md#4-capital-and-protocol-follow-ups).
