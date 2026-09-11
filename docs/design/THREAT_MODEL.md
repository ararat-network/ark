# Ark Threat Model

- Status: **Living document; a change that crosses a boundary below updates it**

For protocol contributors and security reviewers. This page names the components, the boundaries between them, what crosses each boundary, what each side
is allowed to assume, and the controls that hold the assumption. It is the checklist a security review
works down. It is not a claim of completeness: the last section lists what it leaves out.

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
| Contracts | on chain, uploaded by anyone | nothing; untrusted code bounded by the policy router and the accept list |
| Committee accounts | off chain, usually multisigs | one bounded, expiring power per module, appointed by governance |
| Governance | on chain | every unbounded power, including appointing committees |
| Release pipeline | CI and a maintainer's host | the binaries validators run |

The chain's safety assumption is CometBFT's: fewer than one third of voting power is faulty. Every control
below is about keeping an honest validator honest under pressure, not about surviving a faulty
supermajority.

## 2. Boundaries

### 2.1 Validator to consensus: vote extensions, proposals, the preblock

**What crosses.** Each validator's oracle vote rides its vote extension; the proposer injects the
aggregated votes into the block; the preblock applies them. Every byte here comes from another validator,
so it is untrusted until verified.

**Assumptions.** A vote extension is bounded before it is decoded, decodes canonically or not at all,
names only feeds the registry knows, and carries rates the chain can represent. A proposer cannot smuggle a
vote no validator signed or exceed what the mempool would have admitted.

**Controls.** `abci/codec` bounds the aggregate wire and decoded sizes and the vote count; `pkg/encoding`
rejects non-canonical compact decimals; per-rate bytes are capped at `MaxEncodedVoteRateBytes`; rates must
be positive and at most `MaxExchangeRate`; the feed set is capped at `MaxFeeds`. Aggregation is a
stake-weighted median inside a reward band, and attendance is accounted, so one bad vote is outvoted and
recorded. Fee validity is enforced in CheckTx and FinalizeBlock alike, so a proposer cannot include what
every mempool would refuse.

**Review questions.** Is every field bounded before allocation? Does VerifyVoteExtension check everything
ExtendVote produces? What does a malicious proposer control that ProcessProposal does not re-check?

### 2.2 Node to sidecar: the price RPC

**What crosses.** The node polls `ark.pricefeed.v1.PriceFeed/Prices` and caches the snapshot; the vote
extension reads the cache, never the network.

**Assumptions.** The sidecar is the validator's own process, so its prices are trusted as that
validator's view. What is not trusted is the path between them: anything on the network between node
and sidecar can read or replace prices unless the link is authenticated.

**Controls.** TLS with an optional client certificate on the sidecar's listener and the node's client:
the files through `pkg/tlsconfig`, the gRPC clients through `pkg/grpcconn`, and TLS termination shared
by the sidecar's HTTP and gRPC handlers. Material is read and locally validated at start. The default
local mode refuses remote endpoints. TLS and remote plaintext each require an
explicit mode; plaintext with TLS fields is refused. TLS 1.3 verifies peer identity normally, with
system roots when no client CA file is supplied and a replacement trust bundle otherwise.
Owners reload identity contents every minute outside the handshake path. Certificate files must contain
complete certificate PEM blocks separated only by whitespace; incomplete PEM blocks are rejected.
Invalid replacements retain the current certificate and report failure; expiry and recovery are
observable. Trust bundles are fixed
per loaded transport, and changing their contents at the same path requires restart. Rotation applies
to new handshakes only; existing sessions are not revoked.
Snapshots are bounded to twice `MaxFeeds` entries and to the compact encoding's byte limit per price,
refused past `price_ttl` and past a five-second clock skew. The sidecar's build version is logged and exported and
never gated on; the service contract is described in [Node–sidecar compatibility](../operations/PRICEFEED_OPERATIONS.md#nodesidecar-compatibility).

**Review questions.** Is the sidecar on another host, and is TLS mode selected under `[pricefeed.tls]`? Does the node hold
a client certificate if the sidecar requires one? Does anything read the network on the vote path?

### 2.3 Sidecar to node: the feed registry query

**What crosses.** The sidecar reads the active feed set and scheduled transitions from the node's gRPC
port, so it knows what to price.

**Assumptions.** The node's gRPC port terminates no TLS itself. Off one host, a TLS terminator sits in
front of it and the sidecar trusts that terminator's CA.

**Controls.** The sidecar's `client.tls` block dials the terminator with a CA and optional client
certificate. Local mode refuses remote nodes. Address or TLS config changes load replacement material
before committing; failed updates retain the old connections. Timing-only changes retain material.
Identity contents rotate through the same owner-managed loop as the price client. The
response is bounded by `MaxFeeds`; a failed refresh keeps the last snapshot rather than emptying it.

### 2.4 Sidecar to providers: the internet

**What crosses.** Outbound HTTPS and WebSocket requests to exchanges and rate services; API keys go out,
prices come back.

**Assumptions.** Every provider is untrusted. One can be wrong, slow, compromised, or hostile, and the
sidecar must neither crash nor vote on a single bad source unchecked. The chain-level defence is the
median across validators, which only holds while validators draw on different providers.

**Controls.** Response bodies are size-capped; WebSocket reads use the library's default limit; API keys
travel in headers, never URLs, and the config file is written owner-only. Endpoints require HTTPS/WSS
and reject URL user information and fragments. Both provider clients refuse all redirects before a
second request. An API handler must retain its configured HTTPS origin before credentials are attached.
Certificate verification is never disabled anywhere in the repository. Provider diversity is an operational duty, not a code property:
a fleet on the same default providers has one point of failure.

**Review questions.** Does a new provider parse untrusted bytes with a bound? Does any error path log a
request that carries a key? How many validators would a compromise of the default providers move?

### 2.5 Process endpoints: metrics, admin, pprof

**What crosses.** On the sidecar, the admin reload RPC, the Prometheus scrape, and pprof. On the node, the
`[prometheus]` scrape: an Ark listener beside CometBFT's and the SDK's own.

**Assumptions.** Admin and pprof expose process control and internals and stay on the host; metrics may
be scraped from elsewhere. A scrape is a read. The process parses nothing from it beyond the HTTP request
line, and what it returns is operational state: timings, counts, the prices this node wrote, the sidecar
addresses it polls, its chain ID and moniker. None of it is secret; all of it helps someone timing an attack
on one validator, which is why the default is loopback.

**Controls.** The admin listener and pprof are refused off a loopback IP. Both metrics listeners are off by
default, default to loopback when on, and are configurable and documented as such. Only `arkd start` opens
the node's, and a listener that cannot bind stops the node rather than leaving it running unobserved. The
node's registry is its own, so the endpoint carries exactly what `docs/operations/PROCESS_MONITORING.md` §2 lists and
nothing CometBFT or the legacy bridge registers elsewhere. ABCI query paths are also a metrics input, even when
routing rejects the request. The go-metrics bridge maps them to fixed instruments and registered-route or fixed
category labels, collapsing other paths to `unknown`. Separate legacy namespaces by instrument kind and a digest
of each original key prevent collisions with runtime/Ark collectors and Prometheus-generated suffixes. A hard
1,024-instrument cache limit bounds future dynamic legacy call sites; unknown query paths never allocate entries.
Regression tests require scrapes to remain valid after colliding names and many distinct unknown paths.

### 2.6 Users to the chain: transactions

**What crosses.** Signed transactions through CheckTx into the mempool, then into blocks.

**Assumptions.** A transaction pays for what it consumes, cannot buy priority it is not entitled to, and
passes basic, authentication, and upfront fee checks before execution. Tax affordability depends on
message results and is enforced during execution, like transfer affordability.

**Controls.** The ante chain in `app/ante`: the fee decorator prices the transfer tax and holds the fee to
it; MultiSend fan-out is capped with a quadratic surcharge; authz nesting is bounded by the decoder's depth.
Gas fees and tips must be funded upfront. Tax collection and its feegrant allowance draw run only after
successful messages in finalisation and simulation. Admission, recheck, and proposal verification do not
rehearse collection: a payer may acquire tax funds during execution. If collection fails, message and tax
writes revert together while gas fees and the sequence increment remain. Such transactions may occupy
mempool and block resources; the admission bounds and gas fees provide the existing limits, and clients
can simulate to discover execution failures. The SDK proposal path skips tax collection through the same
execution-mode guard, so proposers and validators agree on acceptance.
The app-owned mempool shares one message-type/vouch registry for separate committee and governance
lanes. Bounded read-only vouches run after signature verification; votes also require an active proposal.
Committee candidates must pass mandate authorisation or admission fails. Votes on active, unexpired
proposals, submissions and deposits meeting x/gov's deposit ratios from spendable balance, and
cancellations by the proposer earn governance priority; a deposit followed by a vote remains a normal batch.
Expected ineligibility uses normal service. Mixed/authz batches remain normal without
priority vouches. The independent vote
stake floor is still enforced recursively in ante and at the execution policy router.

CometBFT runs its standard flood mempool. `app/mempool.Pool` wraps the SDK priority/nonce
index with independent 5% committee and governance count/byte reservations; normal admissions
cannot use that reserved storage. The SDK executes CheckTx, recheck, ante and post handlers.
A final ante capacity check runs after authentication but before the SDK retains ante writes,
so reservation refusals cannot consume pending fees, account sequences or unordered nonces.
Lane eligibility is carried in SDK context. Surviving priority and allocations stay fixed at
insertion; current authorisation remains enforced by transaction validation and execution.
There are no per-sender quotas, repeated-vote limits, or proactive eligibility scans.

Startup requires `type = "flood"`, `recheck = true`, CometBFT `size` at least the app count,
and shared positive wire/aggregate byte limits (defaults 1 MiB and 64 MiB). The flood list
checks transaction size before decoding. Local capacity never changes ProcessProposal validity.
SDK removals during optimistic execution or proposal filtering are deferred because freeing
application slots while CometBFT retains the same bytes would let normal traffic exhaust
transport headroom. Actual block entries are released in the SDK post-commit hook while
CometBFT holds its mempool lock; CometBFT's subsequent recheck calls the SDK directly, and
failed ante rechecks remove entries normally. There is no application revalidation loop.
Other SDK behavior, including its post-handler failure semantics and first-signer ordering,
is retained. Snapshot metadata and index operations are protected without holding a pool
lock during validation.

The SDK default proposal handler owns sequencing and transaction verification. An SDK comparator
orders lane then full int64 fee priority, and a small TxSelector caps committee and governance service
independently at 5% of gas and available protobuf bytes. Lane exhaustion leaves the remaining
entries out of the proposal index without stopping other lanes, so a saturated lane cannot make the
proposer verify the whole pool. Normal transactions may use unused capacity. A transaction larger than its entire
lane allowance competes in normal fee order, using a disposable SDK index for the current proposal;
pending priority and storage allocations do not change. Mixed batches also use normal service. Thus
these caps bound preferential service, not total committee/vote traffic. ProcessProposal remains the
SDK default beneath the oracle wrapper and does not enforce node-local lane shares. Gossip, duplicate
caches, retries, peer tracking and RPC validation responses remain CometBFT/SDK behavior.
The execution ante chain still runs privilege checks; changing the vouch set changes execution gas
and must be coordinated on an existing chain.

Reservations isolate classes, not individual members: an eligible sender or qualified Sybil
traffic can fill a privileged reservation. Pending transactions do not pay on-chain fees until
execution, and shared network bandwidth is not reserved. Treasury's consensus fee applies;
local minimum-gas-prices is not a filter. A node on an older binary can still refuse propagation.
Private carriers and isolated sentries remain an optional pre-inclusion confidentiality path with their
own operator/access-control trust boundary, not a prerequisite for public governance admission.
Messages a contract, an interchain account, or a GMP-derived account dispatches never see the ante; the
policy router in `app/execution_policy_router.go` charges their transfer tax at dispatch. What a contract
may read is the accept list in `app/wasm_query.go`: a query path a contract can reach is an input to
consensus, so the list admits only paths annotated `module_query_safe` and freezes each listed response
shape for the life of the chain. The contract runtime is open from launch: upload and instantiation are
permissionless, so a contract is an untrusted input bounded by exactly those two seams, and by the IBC
client allowlist, empty at launch, which leaves the channels a contract could open unopenable until
governance admits a client type.

**Review questions.** Does a new privileged message join a privilege, with its module's own check as the
vouch, and is that vouch a bounded number of reads rather than the handler? Does a new policy walk authz?
Does an eligibility refusal only downgrade, while true consensus policy remains enforced at execution? Does a
newly listed query path carry the annotation, and is its response shape one you will never change?

### 2.7 Governance and committees

**What crosses.** Governance appoints a committee to one module's mandate for a height window; the
committee acts within that window under a term; expiry is lazy.

**Assumptions.** A committee is an ordinary account, so its multisig threshold lives at the account layer,
and the chain records the shape it observed at appointment. A contract may hold a deliberative mandate; the
chain records that it is one and nothing about its code or membership, which its admin, its membership
contract, or governance can change without a term advance. Governance holds every unbounded path, including
Wasmd's authority policy over every contract, and can replace or disable any committee. A committee's
transactions may need to reach a block during an incident, which is what the priority lane and the
emergency submission runbook are for.

**Launch supply.** Most NOAH starts in the community pool and every validator seat holds the same locked grant, so
governance is one seat one vote and the only control over the pool is its own tally: half the seats voting, two
thirds agreeing. Until the pool is distributed this is a proof-of-authority trust model, and admitting a seat is
itself a governance act ([genesis](../governance/GENESIS.md#3-accounts-supply-and-validator-seats)).

**Controls.** `pkg/mandate` fixes the envelope: exact signer, exact term, half-open window, term retained
on disablement so stale transactions never revive. Each module keeps its own mandate and payload bounds.
Committee actions dispatch through the module's own message router, so every target re-validates. A
contract acts by dispatching the committee message from a proposer-signed `MsgExecuteContract`: Wasmd
checks the message names the contract, the policy router taxes it, the module re-validates term and
window, and the transaction rides the normal lane.

**Review questions.** Does a new committee power have a bound the mandate states? Is its message in the
lane and the gate? Does the runbook still describe how it reaches a block under congestion?

### 2.8 Releases and supply chain

**What crosses.** Source becomes the binaries validators run.

**Controls.** `go mod verify` before every release; the wasmvm static archives are pinned by version and
digest in the image Dockerfile, and both the image build and the chain release's before hook check the
archives against them; CI actions are pinned, the mise action by commit; dependabot, nightly govulncheck,
and CodeQL run; goreleaser is pinned in `mise.toml`. The sidecar releases from its own tag line so a chain
release never carries an unreviewed sidecar change, or the reverse.

**Review questions.** Are the remaining actions pinned by commit rather than major tag? Does a release
run from a clean tree at a tag on `HEAD`?

## 3. Out of scope here

- Validator key management, sentry topology, and network-layer denial of service: CometBFT's domain and
  the operator's.
- Committee multisig custody and signer operational security.
- IBC and CosmWasm internals beyond the ante and policy-router seams that route their messages.
- Economic attacks on the peg, the reserve, or the tax, which the module specs cover.

## 4. Keeping this current

A change that adds a listener, a message type, an inbound parser, a credential, or a release step crosses
one of the boundaries above. Add it to the boundary's controls, or add the boundary, in the same change.
