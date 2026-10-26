# Ark Threat Model

- Status: **Living document; a change that crosses a boundary below updates it**

For protocol contributors and security reviewers. This page names the components, the boundaries between them, what
crosses each boundary, what each side may assume, the controls that hold the assumption, and the limits those controls
leave. It is the checklist a security review works down: each control is stated here once, and the README or guide it
links to explains the mechanism. It is not a claim of completeness: §3 lists what it leaves out.

## Contents

- [1. Components and what each is trusted with](#1-components-and-what-each-is-trusted-with)
- [2. Boundaries](#2-boundaries)
- [3. Out of scope here](#3-out-of-scope-here)
- [4. Keeping this current](#4-keeping-this-current)

## 1. Components and what each is trusted with

| Component | Runs where | Trusted with |
|---|---|---|
| `arkd` validator | validator host | the consensus key; the node's vote in every oracle round |
| `pricefeed` sidecar | beside or away from the node | the prices that vote carries; outbound credentials to price providers |
| Price providers | the internet | nothing; every one is an untrusted input |
| Contracts | on chain, uploaded by anyone | nothing; untrusted code bounded by the policy router and its query surface |
| Committee accounts | off chain, usually multisigs | one bounded, expiring mandate per module, appointed by governance |
| Registrar key | the registrar signer, a service of its own | registering members within the open tranche; appointed and rotated by the grants committee without a vote |
| Member platform | off chain, behind the Ark app | admission records, the vouch graph, and the uniqueness store: the verifier's attestations and dedup tokens beside members' addresses |
| Identity vault | off chain, a service of its own in the member backend, once insurance coverage or AML needs it | personal data and the person ↔ address links; every other service sees an opaque person ID |
| Governance | on chain | every unbounded power, including appointing committees |
| Release pipeline | GitHub Actions; a maintainer reviews and publishes | the binaries and images validators run |

The chain's safety assumption is CometBFT's: fewer than one third of voting power is faulty. Every control below keeps
an honest validator honest under pressure; none survives a faulty supermajority. Until distributed NOAH is bonded the
founding validators hold every vote, a proof-of-authority trust model on proof-of-stake machinery, which public stake
erodes as the grants of the [disbursement plan](../governance/DISBURSEMENT_PLAN.md#5-handover) are paid and bonded.

## 2. Boundaries

### 2.1 Validator to consensus: vote extensions, proposals, the preblock

**What crosses.** Each validator's oracle vote rides its vote extension; the proposer injects the aggregated votes into
the block; the preblock applies them. Every byte comes from another validator, so it is untrusted until verified.

**Assumptions.** A vote extension is bounded before it is decoded, decodes canonically or not at all, names only feeds
the registry knows, and carries rates the chain can represent. A proposer cannot smuggle a vote no validator signed.

**Controls.**

- `abci/codec` bounds the aggregate wire and decoded sizes and the vote count; `pkg/encoding` refuses non-canonical
  compact decimals; each rate is capped at `MaxEncodedVoteRateBytes` and must be positive and at most
  `MaxExchangeRate`; a report names only the targets of its vote height's feed epoch, a set capped at `MaxFeeds`
  ([encoding boundary](../../abci/oracle/README.md#encoding-boundary)).
- ProcessProposal authenticates the injected commit against the last commit's signatures and quorum
  ([proposal handlers](../../abci/README.md#proposal-handlers)).
- Aggregation is a stake-weighted median inside a reward band, and attendance is accounted, so one bad vote is
  outvoted and recorded ([oracle processing](../../abci/oracle/README.md)).
- Fee validity is checked in CheckTx and FinalizeBlock alike, and a block's fee and tax prices are fixed when the block
  before it commits, so a fee the proposal check accepts still clears at execution.
- A proposer whose clock finds the proposal's time older than `max_exchange_rate_age` proposes commit-only, so the
  first block after a stall or halt does not trade on pre-outage prices.

**Limits.**

- ProcessProposal re-checks neither the stale-price rule, which would make acceptance depend on validator clocks, nor
  node-local admission (§2.6). A validator proposing the first block after an outage can still trade it, on chain and
  attributably.
- A transaction can fail its ante at execution when an earlier one in the block spent what its fee needed, as in the
  SDK; the earlier transaction paid its own fee.

**Review questions.** Is every field bounded before allocation? Does VerifyVoteExtension check everything ExtendVote
produces? What does a malicious proposer control that ProcessProposal does not re-check?

### 2.2 Node to sidecar: the price RPC

**What crosses.** The node polls `ark.pricefeed.v1.PriceFeed/Prices` and caches the snapshot; the vote extension reads
the cache, never the network.

**Assumptions.** The sidecar is the validator's own process, so its prices are trusted as that validator's view. The
path between them is not: anything on the network between node and sidecar can read or replace prices unless the link
is authenticated.

**Controls.**

- Local mode, the default, refuses remote endpoints. TLS and remote plaintext each need an explicit mode, and plaintext
  with TLS files is refused.
- TLS 1.3 with normal peer verification: files load through `pkg/tlsconfig` and connections through `pkg/grpcconn`;
  identities reload every minute, and an invalid replacement keeps the current one
  ([transport and rotation](../operations/PRICEFEED_OPERATIONS.md#tls)).
- A snapshot is bounded to twice `MaxFeeds` entries and the compact encoding's per-price limit, and refused past
  `price_ttl` or more than five seconds ahead of the node's clock
  ([price client](../../pricefeed/client/README.md#lifecycle-and-snapshots)).
- The sidecar's build version is logged, never exported or gated on
  ([compatibility](../operations/PRICEFEED_OPERATIONS.md#10-releases-and-compatibility)).

**Limits.** Without `--tls-client-ca-file` the sidecar accepts any client. Trust bundles are fixed per loaded
transport, so changing one at the same path needs a restart, and rotation reaches new handshakes only, revoking no
session.

**Review questions.** Is the sidecar on another host, and is TLS mode selected under `[pricefeed.tls]`? Does the node
hold a client certificate if the sidecar requires one? Does anything read the network on the vote path?

### 2.3 Sidecar to node: the feed registry query

**What crosses.** The sidecar reads the active feed set and scheduled transitions from the node's gRPC port, so it
knows what to price.

**Assumptions.** The node's gRPC port terminates no TLS itself. Off one host, a TLS terminator sits in front of it and
the sidecar trusts that terminator's CA.

**Controls.**

- `[client.tls]` dials the terminator with a CA and optional client certificate, rotating identities as the price link
  does; local mode refuses remote nodes.
- An address or TLS change loads replacement material before committing, and a failed update keeps the old
  connections.
- The response is bounded by `MaxFeeds`, and a failed refresh keeps the last feed set rather than emptying it
  ([chain feed client](../../pricefeed/sidecar/chainstate/README.md)).

**Review questions.** Off one host, does the sidecar reach the node only through a terminator whose CA it trusts? Can
a change let a feed response grow past `MaxFeeds`, or a failed refresh empty the set?

### 2.4 Sidecar to providers: the internet

**What crosses.** Outbound HTTPS and WebSocket requests to exchanges and rate services; API keys go out, prices come
back.

**Assumptions.** Every provider is untrusted: one can be wrong, slow, compromised, or hostile, and the sidecar must
neither crash nor vote on a single bad source unchecked. The chain-level defence is the median across validators,
which holds only while validators draw on different providers.

**Controls.**

- Response bodies are capped at `MaxResponseBodyBytes`, 1 MiB, and WebSocket frames at `MaxMessageBytes`, 32 KiB;
  compressed (Huobi) and protobuf-framed (MEXC) streams decode under their own bounds
  ([API](../../pricefeed/sidecar/providers/api/README.md), [WebSocket](../../pricefeed/sidecar/providers/websocket/README.md)).
- A price is read only as plain decimal text of at most `MaxPriceTextBytes`, 128 bytes, so a venue that changes
  format, such as to hex-encoded integers, goes unresolved instead of being read as a different number.
- Endpoints must be HTTPS or WSS without URL user information or fragments. Both clients refuse every redirect, and an
  API handler keeps its configured origin before credentials attach
  ([transport](../operations/PRICEFEED_OPERATIONS.md#tls)).
- API keys travel in headers, never URLs, and the config file is written owner-only. KuCoin's connect token, a
  short-lived public value from a fixed HTTPS origin, rides the dial query because the protocol reads it there, and
  dial errors redact it.
- Certificate verification is disabled nowhere in the repository.

**Limits.**

- Provider diversity is an operational duty, not a code property: a fleet on the same default providers has one point
  of failure.
- The NOAH leg of every route is a bootstrap price until a provider reports a live NOAH sample, so the first venue to
  list NOAH re-marks every rate. NOAH venues enter the configs together and only once a venue has depth, and conversion
  is closed until then, so no currency exists to re-mark ([opening
  conversion](../governance/GOVERNANCE_RUNBOOK.md#opening-conversion)).

**Review questions.** Does a new provider parse untrusted bytes with a bound? Does any error path log a request that
carries a key? How many validators would a compromise of the default providers move?

### 2.5 Process endpoints: metrics, admin, pprof

**What crosses.** On the sidecar, the admin reload RPC, the Prometheus scrape, and pprof. On the node, the
`[prometheus]` scrape, an Ark listener beside CometBFT's and the SDK's own.

**Assumptions.** Admin and pprof expose process control and internals and stay on the host; metrics may be scraped
from elsewhere. A scrape is a read: the process parses nothing beyond the HTTP request line and returns operational
state, such as timings, counts, the prices this node wrote, the sidecar addresses it polls, and its chain ID and
moniker. None of it is secret; all of it helps someone timing an attack on one validator.

**Controls.**

- The admin listener and pprof are refused off a loopback IP.
- Both metrics listeners are off by default and default to loopback when on. Only `arkd start` opens the node's, and a
  listener that cannot bind stops the node rather than leaving it running unobserved.
- The node's registry is its own, so its scrape carries exactly what
  [process monitoring §5](../operations/PROCESS_MONITORING.md#5-prometheus) lists and nothing CometBFT or the legacy
  bridge registers.
- The legacy go-metrics bridge maps ABCI query paths, routed or not, to fixed route or category labels and collapses
  the rest to `unknown`; other legacy instruments are capped at 1,024, so no request grows the series set
  ([legacy bridge](../operations/PROCESS_MONITORING.md#7-the-legacy-telemetry-bridge)).

**Review questions.** Does a new endpoint default to loopback, and refuse to start off it if it controls the process?
Can a request choose a label value on a new instrument?

### 2.6 Users to the chain: transactions

**What crosses.** Signed transactions through CheckTx into the mempool, then into blocks, and the messages contracts
dispatch during execution.

**Assumptions.** A transaction pays for what it consumes, cannot buy priority it is not entitled to, and passes basic,
authentication, and upfront fee checks before execution. Tax affordability depends on message results and is enforced
during execution, like transfer affordability.

**Controls.**

- The ante chain in `app/ante` prices the transfer tax and holds the fee to it. Gas fees and tips are funded upfront;
  tax is collected after successful messages, and if it cannot be the messages revert with it while gas and the
  sequence stand ([ante and post](../../app/ante/README.md#transfer-tax-commitment-and-rollback)).
- MultiSend is capped at 500 outputs with a quadratic surcharge; authz nesting is bounded by the decoder's depth; the
  vote stake floor is enforced recursively in ante and at the execution policy router.
- A consensus-params update, signed, authz-wrapped, or in a proposal at submission, that leaves less than
  `ante.MinBlockDataBytes` (8 MiB) of `block.max_bytes − evidence.max_bytes` is refused, so no vote can price the
  injected extended commit out of every proposal.
- No Market message carries a recipient, so a payment in another denomination is a buy and a Bank send, taxed on the
  send ([role separation](../economics/ECONOMIC_DESIGN.md#72-role-separation)).
- The committee and governance lanes each reserve 5% of pending count and bytes and 5% of a proposal's gas and bytes.
  Eligibility is a bounded read-only vouch after signature verification, and a privileged entry past its lane's room
  spills to normal fee order ([mempool](../../app/mempool/README.md#admission-service-and-sdk-behaviour)).
- Messages a contract dispatches never see the ante: the policy router in `app/execution_policy_router.go` taxes them
  at dispatch. A contract's queries go through the accept list in `app/wasm_query.go`, which admits only paths
  annotated `module_query_safe` and freezes each listed response shape for the life of the chain, as Wasmd's
  typed Bank, Staking, Distribution, and Wasm queriers freeze theirs
  ([Wasm integration](../../app/README.md#wasm-integration)).
- Upload and instantiation are permissionless, so a contract is an untrusted input bounded by the router and that query
  surface. Its IBC messages and queries are refused and no contract port is routed, so there is no channel a contract
  could open.

**Limits.**

- Reservations isolate classes, not members: one eligible sender or qualified Sybil traffic can fill a class's
  reservation. The proposal caps bound preferential service, not total committee or vote traffic, and network
  bandwidth is not reserved.
- Lane shares, `max_tx_bytes`, and the stale-vote refusal are node-local: ProcessProposal does not enforce them, and a
  node on an older binary can still refuse propagation.
- Pending transactions pay nothing until execution. A transaction whose tax fails at execution still occupies mempool
  and block space and pays only gas; admission bounds and gas fees are the limit, and clients simulate to find such
  failures.
- A node cannot shed spam by raising its local `minimum-gas-prices`: every node admits at Treasury's consensus fee.

**Review questions.** Does a new privileged message join a privilege, with its module's own check as the vouch, and is
that vouch a bounded number of reads rather than the handler? Does a new policy walk authz? Does an eligibility refusal
only downgrade, while true consensus policy remains enforced at execution? Does a newly listed query path carry the
annotation, and is its response shape one you will never change?

### 2.7 Governance and committees

**What crosses.** Governance appoints a committee to one module's mandate for a window of block time, and the
committee acts inside that window under a term. Windows a vote must fit are block time, as the vote is, so validators
running a faster block cannot shorten them.

**Assumptions.** A committee is an ordinary account, so its multisig threshold lives at the account layer, and the
chain records the shape it observed at appointment. A contract may hold a deliberative mandate: the chain records that
it is one and nothing about its code or membership, which its admin, its membership contract, or governance can change
without a term advance. Governance holds every unbounded power, including Wasmd's authority over every contract, and
can replace or disable any committee.

**Controls.**

- `pkg/mandate` fixes the envelope: exact signer, exact term, half-open window, and the term retained on disablement,
  so stale transactions never revive ([mandate conventions](../../pkg/mandate/README.md)). Each module bounds its own
  payload, and every committee power is a message of its own
  ([role separation](../economics/ECONOMIC_DESIGN.md#72-role-separation)).
- Committee actions dispatch through the module's own message router, so every target re-validates. A contract acts by
  dispatching the committee message from a `MsgExecuteContract`: Wasmd checks the message names the contract, the
  policy router taxes it, the module re-validates term and window, and it rides the normal lane.
- Committee messages ride the committee lane, so an incident's transactions reach a block under congestion
  ([mempool policy](../../app/mempool/README.md#admission-service-and-sdk-behaviour)).
- Disbursement's journal, Claims' records, and the Reserve's ledger are permanent, so what a key did stays on the
  record for governance to undo.

**A stolen committee key.** The worst each appointment's key can do before governance replaces it; a replacement
advances the term, so transactions prepared under the old key expire. The linked README owns each bound.

| Appointment | Worst a stolen key can do | What answers it |
|---|---|---|
| [Treasury](../../x/treasury/README.md) economic policy | Set the reward targets, fund target ratios, and exposure weights anywhere inside the corridor | Replacement; governance resets the policy |
| [Market](../../x/market/README.md#conversion-mandate) conversion | Set depth, recovery period, and spread floor inside the corridor, and the Tobin tax up to the mandate cap | Replacement; governance resets the policy |
| [Asset](../../x/asset/README.md#9-the-emergency-mandate) emergency | Suspend each healthy asset once a term; a suspension moves no value | Governance recovers the asset |
| [Reserve](../../x/reserve/README.md#mandate-custody-and-journal) | Deploy up to the term's allowance, only to governance's approved destinations and above the NOAH floor; burn NOAH within the surplus bound, and the Ark paper it holds, which earns no capital credit; attest inflated holdings, each adding at most its entry's `max_credit_per_noah` × the NOAH balance and moving no other asset's credit or any coin, each restatement citing its evidence | Replacement; governance corrects the attestation |
| [Claims](../../x/claims/README.md#stablecoin-funding-and-the-committee-limit) | Submit claims within the term's limit and cancel committee claims; a stablecoin claim's buy, which no cancel undoes, is charged against the limit | Nothing pays before the cancellation period, a week at launch; governance cancels |
| [Security](../../x/security/README.md) | Schedule an upgrade at a height it picks, halting the chain there; there is no lead time | A replacement leaves the plan standing: governance cancels it, or the new committee does, if either lands before the height; otherwise validators restart past it with `--unsafe-skip-upgrades` |
| [Grants](../../x/disbursement/README.md#committee-awards) | Award compensation within the term's allowance, a stablecoin award's buy charged against it; appoint its own registrar; suspend members, or reinstate them, which pays their arrears | Nothing pays before one period, at least two weeks. One proposal replaces the committee, clearing its registrar, cancels its term's awards, and reinstates members it suspended; a second, expedited, cancels what it made during the first vote |
| [Registrar](../../x/disbursement/README.md#funding-and-authority) | Register members up to the open tranche; the runbook opens each step a quarter at a time | Nothing pays before the two-week hold; the committee reappoints and suspends, and governance cancels from the term's list |

**Limits.**

- The registrar hold protects only if someone compares the chain's registrations with the platform's admission records
  inside it ([grants](../governance/COMMITTEE_RUNBOOK.md#8-grants)).
- Member admission runs off chain: a third-party identity verifier checks the person and returns a uniqueness
  attestation and a dedup token, which the member platform keeps beside the admission record; nothing it keeps names a
  person. The chain never sees the verifier. The uniqueness store, which the verifier can map back to people, ranks with
  the committee and registrar keys, and so does the identity vault once it holds personal data and the person ↔ address
  link ([member backend](../direction/TOOLING_DIRECTION.md#7-run-the-member-backend-without-custody)). Public
  references, registration batches and claim references included, carry no personal data.
- A Reserve holding not re-attested within its entry's window earns nothing, which catches a custodian that
  fails while the committee stops attesting; a key that keeps re-attesting falsely is answered only by the evidence
  its references cite.
- The ownership cap binds beneficiary records, not people; tying a record to a person is the proposal's case and the
  admission process's.
- The compensation cap and allowance and the Claims limit value an act at the fresh oracle rates, so a committee act is
  refused while no rate is live, and a pushed rate changes only how much of the bound one act takes.
- Disbursement custody holds the distribution from block 1, so a defect in its accounting or tranche checks is exposed
  from launch.
- The module cannot stop governance labelling ownership-scale pay as compensation. The founder's 10M NOAH stream is
  the one deliberate case, counted as the founder's stake; it and the founder's 15M grant are genesis governance
  awards, approved by every founding validator's gentx and cancellable like any other.
- A bought third of bonded stake blocks every proposal, stops each member step and committee re-appointment, and on
  the buyer's own validators halts the chain. It costs about half of what everyone else has bonded, 34M against the
  founding stake at ten seats. A continuation export with `--jail-allowed-addrs` relaunches without the buyer
  ([node operations](../operations/NODE_OPERATIONS.md#8-relaunch)).

**Review questions.** Does a new committee power have a bound the mandate states, and a row in the table above? Is its
message in the lane and the gate? Does the runbook still describe how it reaches a block under congestion? Does a
change give a bare stake majority a faster or quieter path to an irreversible act, or a blocking minority a new vote to
stop?

### 2.8 Releases and supply chain

**What crosses.** Source becomes the binaries and images validators run.

**Assumptions.** Everything a release pulls in is checked against a pin or a checksum. The publishing jobs and every
tool in them are trusted, since they hold package-write, contents-write, OIDC, and attestation permissions, and an
administrator account is trusted with `main`.

**Controls.**

- `go mod verify` runs before every release. The wasmvm static archives are pinned by version and digest in the image
  Dockerfile, and both the image build and the standalone builder check them. Every external CI action is pinned by
  full commit SHA, which the repository requires; goreleaser is pinned in `mise.toml`, and Syft in the workflows.
  Dependabot, nightly govulncheck, and CodeQL run.
- Source packaging exports a fixed committed tree, refusing tracked changes and a moving HEAD, and rebuilds it in fresh
  containers with networking disabled before publication. OS sources come from aports recipes at the package-recorded
  commits with verified checksums; an APKBUILD is shell code and runs only in a separate unprivileged stage with no
  secrets or host mounts ([third-party notices](../../THIRD_PARTY_NOTICES.md)).
- The standalone build adapter forwards GoReleaser's arguments without shell evaluation, admits only the exported
  source and the build request, and verifies architecture, static linkage, revision, binary hash, and runtime-source
  identity before returning a binary.
- Images upload by digest with an SPDX SBOM and OIDC-signed provenance, verified against repository, workflow,
  revision, and ref before any tag moves. Standalone releases build in Actions from a tag on a reviewed `main`
  ancestor, sign every asset, verify the downloaded draft, and never overwrite a release or publish a draft themselves
  ([release runbook](../releases/RELEASE_RUNBOOK.md), [release verification](../operations/RELEASE_VERIFICATION.md)).
- Published releases are immutable: GitHub locks their assets and tag. The sidecar's standalone binaries release from
  their own `pricefeed/v*` tag line, so a chain binary release never carries an unreviewed sidecar change, or the
  reverse.
- Every chain tag pushes the proto module to the Buf Schema Registry from a job holding only `BUF_TOKEN`, a repository
  secret scoped to `module.push` on `buf.build/ararat-network/ark` with an expiry. The module is public, so nothing
  reads with the token, and the push labels the tag's own commit. The registry copy mirrors the tagged tree and is
  never a source: a consumer verifies against the tag.

**Limits.**

- The distribution image builds `pricefeed` from the chain tag's tree, so it carries whatever sidecar code sits at that
  tag, including commits after the last `pricefeed/v*` tag.
- Signed claims establish origin and integrity, not the absence of vulnerabilities, licence compatibility, upstream
  provenance, or reproducibility. Commit and checksum matching do not prove upstream code benign, and the recipe
  stage's network access is a supply-chain boundary. The scanner does not prove it found every statically linked
  native dependency.
- A failed run can leave untagged image data or attestations in the registry, and promotion across several tags is not
  atomic.
- Consumers verify what they download even after draft review, since a maintainer with access can otherwise change
  assets and tags.
- A leaked `BUF_TOKEN` can push a label to the proto module until it expires or is revoked. It cannot move the git
  tag, so a consumer that pins the tag sees the mismatch; a consumer that trusts the registry label alone does not.

**Repository settings.** These live outside Git and are verified separately before publication.

- `main` requires pull requests, an approval from someone other than the last pusher with stale approvals dismissed,
  passing CI, no new high-severity code scanning alerts, and resolved conversations. Administrators may bypass these,
  including by pushing directly. Nobody may force-push or delete `main`.
- Only administrators create release tags, and nobody moves or deletes them.
- Required checks run on every pull request, documentation-only changes included, and each names a job the workflows
  still run: a removed or renamed job leaves its check pending and blocks every pull request short of an administrator
  bypass.
- Maintainer accounts use 2FA. Actions require full commit-SHA pins, default tokens stay read-only, Actions cannot
  approve pull requests, and every external fork workflow needs approval, given only after its code is reviewed.

**Review questions.** Does every required status check match a job the workflows still run? Does a release run from a
clean tree at a tag on `HEAD`? Does a new release step pull anything without a pin or checksum? Does the Buf push run
only on tags, with a token scoped to the one module?

### 2.9 Clients to the node: queries

**What crosses.** gRPC, REST, and CometBFT RPC queries from anyone who can reach a node's listeners. The queries a
contract makes during execution are §2.6's.

**Assumptions.** A query is free to its caller and writes nothing, so what it can do to a node is cost. What it returns
is public chain state. Which listeners face the internet is the operator's choice.

**Controls.**

- Every Ark list query takes its page through `pkg/pagination.Bounded`: key pagination only, at most `MaxLimit`, 100
  entries, with no offset and no total count, since the records behind the queries mostly only grow
  ([pagination](../../pkg/pagination/pagination.go)).
- A smart query runs its contract under wasmd's `[wasm] query_gas_limit`, 3,000,000 by default.

**Limits.** The SDK's own paginator sets no ceiling, so the standard modules' list queries accept offsets and total
counts, and the SDK's `query-gas-limit` defaults to 0, unlimited. A node serving public queries sets it and fronts its
listeners with a proxy; rate limiting is the operator's (§3).

**Review questions.** Does a new list query page through `pagination.Bounded`? Does a new query read more than a
bounded number of entries without a page?

## 3. Out of scope here

- Validator key management, sentry topology, and network-layer denial of service: CometBFT's domain and
  the operator's.
- Committee multisig custody and signer operational security.
- CosmWasm internals beyond the ante and policy-router seams that route its messages.
- Clients and wallets that build and sign transactions, such as the Ledger app, the Ark app, and its signer interface
  ([tooling direction](../direction/TOOLING_DIRECTION.md)).
- Economic attacks on the peg, the reserve, or the tax, which the module specs cover.

## 4. Keeping this current

A change that adds a listener, a message type, an inbound parser, a credential, or a release step crosses one of the
boundaries above. Add it to the boundary's controls, or add the boundary, in the same change. State the control here
and explain its mechanism in the README that owns it.
