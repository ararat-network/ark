# Post-Launch Operations

- Status: recorded 2026-09-06. Every step here is a governance vote or a relayer action on a chain that has launched;
  none is code.
- Companions: `docs/GENESIS.md` holds every setting fixed at genesis and the launch review.
  `docs/TREASURY_REDESIGN_PLAN.md` §20 and §21 are the design record behind the capital operations in §5; `Dnn`
  references are rows of its register.

Governance signs every message below unless a row says otherwise. Type URLs are given so a proposal can be drafted
from this page; each is the `Msg` under the package named.

## 1. What is post-launch, and why

One thing is post-launch by nature: an IBC channel is a handshake with a live counterparty, so channels and everything
that rides on them cannot exist in a fresh genesis. Rate limits are keyed by channel or client ID, so they follow the
channels, and the plan makes limits a precondition for the transfer flags (D46), so the flags follow the limits.

Everything else was a choice, and it was made the same way for every surface: a genesis value is permanent and a
parameter vote is reversible, so a surface is opened by vote and closed by nothing. The launch artefact therefore ships
the hub shut behind one switch, the empty allowed-client list, and the contract runtime open (D45 as amended, §16.3 of
the plan). What that leaves for after launch is the table of contents of this document.

Not here, because it is genesis: the economic levers, the three committee appointments, the Wasm permissions and query
accept list, and the first external asset if it is decided before launch (`docs/GENESIS.md` §12).

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
  and pays execution tax on a contract's terms (D48). Covered end to end in `app/gmp_relay_test.go`.
- **Contract channels.** A contract with IBC entry points may open its own channels on either stack. Callback delivery
  into contracts on both chains, and the failure and gas-cap semantics at each callback stage, are covered in
  `app/ibc_callbacks_test.go`.

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
| Appoint or replace a committee | `ark.treasury.v1.MsgSetEconomicMandate`, `ark.claims.v1.MsgSetClaimsMandate`, `ark.market.v1.MsgSetConversionMandate`, `ark.reserve.v1.MsgSetReserveMandate`, `ark.asset.v1.MsgSetEmergencyMandate`, `ark.security.v1.MsgSetSecurityMandate` | governance | The committee's shape is observed from the live account at appointment. The account must exist with its own NOAH for gas, never funded from a custody account. The first appointment is the trigger for Stage 1 of the committee-realisation record. |
| Commit Reserve NOAH to the Buffer | `ark.reserve.v1.MsgFundBuffer` with the exact amount and minimum remaining balance; `MsgCommitteeFundBuffer` inside the mandate floor | governance; committee | One-way; changes no supply, quote, or pool state. There is no global floor, cap, or trigger: each proposal states its own (D27). |
| Move the tax | `ark.treasury.v1.MsgUpdateParams` with `transfer_tax_rate`, `reference_tax_cap` | governance | The rate stays at or below the spread floor (D81). The cap is denominated in the reference unit; zero means uncapped. |
| Switch the exposure multiplier on | `ark.treasury.v1.MsgUpdatePolicy`; `MsgCommitteeUpdatePolicy` inside the mandate bounds | governance; committee | Only after the observability calibration window has produced thresholds (`docs/superpowers/specs/2026-08-10-exposure-observability.md`). `m` is floored at one, so zero weights are exactly the unscaled sizing (D72). |
| Onboard the first external asset | `ark.oracle.v1.MsgAddFeed`, then `ark.reserve.v1.MsgSetRecognitionPolicy` (haircut, cap ratio, staleness window), then `ark.reserve.v1.MsgSetReserveMandate` naming the destination | governance | Price-feed sidecars must serve the symbol before the feed can be Active, which listing requires. Custody is attested: the asset sits at the destination and the committee attests the quantity (D59). The Reserve account never holds an external token (D70, amended). |
| Deploy, attest, impair, close | `ark.reserve.v1.MsgCommitteeDeploy`, `MsgCommitteeRecordUpdate`, `MsgCommitteeAttributeReturn`, `MsgCommitteeMarkImpaired`, `MsgCommitteeClosePosition`, and the governance corrections | committee; governance | The one-committee mandate (D56, D57); governance corrects records, clears impairment, and owns the recognition policy. |
| Burn Reserve holdings | `MsgCommitteeBurnPaper`, `MsgCommitteeBurnSurplus`; `ark.reserve.v1.MsgBurnReserveAssets` | committee; governance | Split burn authority (D61). The surplus bound is Treasury's required capital, which needs a complete valuation. |

## 6. Deferred, each on a stated trigger

- **Per-fund exposure model.** Retired as Phase 6. Under NOAH-only custody every fund's exposure has liability as its
  only base, so the launch ratios are the model. Revisit only if a fund carries material risk that does not scale with
  liability.
- **Recognised-capital block cache.** Deferred behind a cadence benchmark beside the existing liability one, at
  realistic position counts.
- **What sits behind a committee address.** Stages 1 to 3 of
  `docs/superpowers/specs/2026-08-11-committee-realisation-design.md`, each on its trigger; operational, not chain work,
  until Stage 3.

## 7. Will not be built

Recorded so nobody re-proposes them:

- On-chain custody of an external token in the Reserve account; the send restriction's refusal is permanent (D70).
- A typed Reserve deployment adapter; attestation with bounded references is the permanent evidence model (D59).
- A residual-mint limiter (P2), a Treasury Reserve-transfer history query, a wildcard ICA host allowlist, and IBC Hooks
  short of a concrete requirement for Osmosis-compatible memo semantics.
