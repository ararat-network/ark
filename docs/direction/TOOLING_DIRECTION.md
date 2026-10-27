# Ark First-Party Tooling Direction

For product and integration contributors. This document records the direction for Ark's user and developer tooling:
the repositories it is made of, what each serves, and the order they are built in. It guides product and architecture
decisions without prescribing a delivery schedule or package layout.

The tooling is a coherent, first-party, Cosmos-native toolchain on what the chain already provides: protobuf and gRPC
APIs, gRPC-gateway support, direct and textual signing modes, the `ark` Bech32 prefix, and BIP-44 coin type `330`. The
division of responsibility is Terra's: the chain owns state and validation, a client SDK exposes those interfaces,
wallets own signing and user workflows, indexers derive history, explorers make it inspectable, and shared network
metadata connects the products. Terra's package layout and fee assumptions are not inherited. The work is turning those
protocol capabilities into a dependable product surface.

The [disbursement plan](../governance/DISBURSEMENT_PLAN.md) turned part of this direction into a dependency with a date:
its first member tranche needs the Ark app, the backend behind it, and the tooling around the Disbursement module, and
nothing else here. The [build order](#build-order) sequences that first, and each repository's section says what the
tranche needs from it.

## Contents

- [The stack](#the-stack)
- [Build order](#build-order)
- [Chain and shared contracts](#chain-and-shared-contracts)
- [Client](#client)
- [Backend](#backend)
- [Information layer](#information-layer)
- [Rules across repositories](#rules-across-repositories)
- [Decision checkpoints](#decision-checkpoints)
- [Non-goals](#non-goals)

## The stack

Each component is its own repository under `ararat-network`, so each has its own access list, release cadence, and CI.
Two live inside another repository: the TypeScript decoder ships in `ark-sdk`, since the contract it shares with Go
consumers is the schemas and fixtures in `ark`, and the local environment stays in `ark`, since it builds `arkd` from
source and runs the testnet artefact. The rows follow the sections below: by layer, and by build order within a layer.

| Repository         | Serves                                                                                                           | Must not become                                                               | Visibility | First tranche |
| ------------------ | ---------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- | ---------- | ------------- |
| `ark`              | The chain: state, validation, policy, and protocol queries; the protos, decoding fixtures, local environment, and monitoring templates | A product-specific UI backend                                                 | Public     | Yes           |
| `ark-registry`     | Shared network configuration and asset metadata                                                                  | A second source of chain state                                                | Public     | Yes           |
| `ark-sdk`          | Encoding, querying, transaction workflows, decoding, and formatting; the signup protocol                         | An implementation of economic or market policy                                | Public     | Yes           |
| `ark-signer`       | Key custody on the device, signing consent, transaction review, multisig; later the dapp-to-wallet protocol      | An authority for balances, quotes, or transaction success                     | Public     | Yes           |
| `ledger-ark`       | Signing on a Ledger device behind the signer interface                                                           | A second signer interface                                                     | Public     | Yes           |
| `ark-app`          | Signup, the grant view, staking and voting each payment, and what the app later adds                             | A key store outside the signer interface                                      | Public     | Yes           |
| `ark-membership`   | Admission by vouch or identity, the vouch graph and uniqueness store, the registrar key in a KMS, funding requests, suspend | A custodian, a delegator, a vote, or a holder of anything that names a person | Private    | Yes           |
| `ark-indexer`      | Replayable historical and searchable derived data                                                                | Consensus state or an irreplaceable ledger                                    | Public     | Later         |

## Build order

This is a dependency order, not a schedule. Steps 1 and 2 put the app with member registration in front of the first
member tranche, and nothing after them is needed for it. Later layers should not compensate for unresolved protocol or
metadata contracts underneath them.

### First member tranche

1. **Build two tracks in parallel.** The backend: [`ark-membership`](#ark-membership), its registrar signer first, the
   one part that writes to the chain, then admissions and the vouch graph, the uniqueness store following with the
   identity check. The client: `ark-sdk` with the queries, transactions, ADR-036 signature, and decoding the app needs,
   then [`ark-signer`](#ark-signer) with the device keystore and the adapter for [`ledger-ark`](#ledger-ark), then the
   signup flow in [`ark-app`](#ark-app). The tracks meet when the app's signup registers a member through the membership
   service, from a device key and from a Ledger.
2. **Finish what a member needs before the first payment.** The grant view, staking, and voting in the app, since the
   first tenth pays two weeks after registration; the app's distribution screen, readable before signup; and a testnet
   rehearsal of the whole flow with the daily comparison running.

### After the first tranche

3. **Add the identity check before the plan's step 2.** Before conversion opens: counsel on face matching, the verifier
   pilot, the membership service's verifier adapter and uniqueness store, and the return of verified-only vouching,
   which step-1 members pass to keep vouching ([admission](../governance/DISBURSEMENT_PLAN.md#admission)).
4. **Widen the app.** Transfers, simulation, market operations, and the contributor interactions in the app's later
   scope; external-wallet signers behind the signer interface; and multisignature signing, so a committee signs its
   acts from the app and a shared account signs the same way, with the relay that carries partial signatures.
5. **Add the information layer.** Replayable indexing and the app's explorer pages on it, once the meanings of
   messages, events, assets, and account activity are stable enough to share.
6. **Make third-party development routine.** Extend the signer interface into the wallet kit's dapp protocol, and
   provide the documented interfaces and compatibility guidance needed to build without private knowledge.
7. **Expand execution environments deliberately.** A general contract product or an EVM-facing one is added when
   application demand and operational costs justify it.

## Chain and shared contracts

### `ark`

The chain is the only source of truth for economic and consensus-sensitive behaviour. Market conversion, treasury
policy, oracle rates, taxes, fees, staking, governance, and any future contract integrations are determined by
committed chain state. Client tooling may request quotes, simulate transactions, format results, and explain outcomes;
it does not reimplement policy calculations and present them as authoritative. If a user-facing workflow needs a value
that is difficult to obtain safely, the answer is a clear query or simulation surface on the chain, not copied business
logic in every client.

Four more things the tooling consumes live here, since each is the chain's half of a contract:

- **The Disbursement module.** The [module](../../x/disbursement/README.md) is the distribution and compensation
  ledger: custody of each tranche, member issuance, and the escrow paid out by the plan's rules, through typed protobuf
  messages, gRPC/REST queries, AutoCLI, and a permanent journal. SDK and app integrations share those
  schemas and decode typed events. Contributor interfaces need the original agreement, current payee, paid and
  remaining principal, the record's room under the ownership cap, and cancellation history; stablecoin compensation
  uses the same API. The e2e tier opens member tranches through governance, registers members, and sees their tenths
  queued behind the two-week hold; `go test ./tests/integration -run TestNativeDisbursement` covers the block paying
  them, deferral, cancellation, and committee recovery through the real app's keepers. The module is not a policy
  engine beyond the plan's rules, and not a treasury. Native grants need no Rust workspace, optimizer artifact, or
  contract deployment; CosmWasm upload and instantiation remain permissionless from launch
  ([genesis §12](../governance/GENESIS.md#12-cosmwasm)), and general contract tooling is a separate product decision.
- **The decoding fixtures.** Real transactions through the application's own encoder and real blocks, in
  [tests/fixtures](../../tests/fixtures/README.md): the contract every decoder, in TypeScript or Go, is tested against,
  so that the app and the SDK give each transaction [one meaning](#one-meaning-per-transaction).
- **The local environment.** The [Docker localnet](../../contrib/localnet/README.md): one or four validators with
  their sidecars on the testnet artefact, from generated development keys, with its own registry entry, reset on every
  start. It is a fixture for wallet, indexer, contract, and application developers and for testing protocol
  upgrades, never a simulation with materially different protocol behaviour.
- **The monitoring templates.** [contrib/observability](../../contrib/observability/README.md): the scrape and routing
  templates, the rules for every alert [process](../operations/PROCESS_MONITORING.md) and
  [protocol monitoring](../operations/PROTOCOL_MONITORING.md) specify, and the dashboards, taken from the tag of the
  `arkd` they watch. The protocol rules read series the indexer exports, and the first tranche's Disbursement alerts
  the membership service's.

The chain is not a product-specific UI backend.

### `ark-registry`

All first-party tooling consumes one shared, versioned description of Ark networks: chain IDs and environment names;
Bech32 prefixes and coin type; native and supported fee denominations; display denominations, exponents, symbols, and
icons; RPC, gRPC, REST, WebSocket, indexer, and explorer endpoints; genesis identity or another way to prevent
endpoint/network confusion; recognised asset metadata; and enabled product features and compatibility ranges. None of
it is copied into each wallet and dapp, so testnet, mainnet, and local development behave consistently.

The chain-derived half, denominations, prefix, coin type, versions, and genesis identity, is generated by
[contrib/registry](../../contrib/registry/README.md) in the community chain-registry's schema, so it cannot drift from
the chain; the registry keeps endpoints and explorers by hand and checks the rest against a pinned `ark` tag. It is not
a second source of chain state.

### `ark-sdk`

The TypeScript SDK is the primary integration surface for web applications and, where practical, other TypeScript
consumers. It builds on generated protobuf types and modern Cosmos transaction signing, not legacy LCD assumptions, and
covers:

- typed queries and messages for both standard Cosmos modules and Ark modules;
- transaction construction, simulation, fee selection, signing requests, and broadcast;
- address and denomination formatting;
- well-defined handling of direct, textual, and any intentionally supported fallback sign modes;
- decoding of transaction messages, events, errors, and module-specific results, tested against the fixtures in `ark`;
- network-registry consumption;
- a small set of higher-level workflow helpers where those helpers do not duplicate chain policy.

The SDK exposes protocol concepts accurately. Convenience abstractions are valuable, but hiding behaviour such as
slippage, tax, oracle freshness, or transaction finality would make the tooling easier to misuse. It is not an
independent implementation of economic or market policy.

One contract is published here beside the code. The
[signup protocol](https://github.com/ararat-network/ark-sdk/blob/main/docs/SIGNUP_PROTOCOL.md), address, proof of
possession, vouch, and identity attestation to the backend, is specified with its test vectors so that a member signing
with an external wallet can complete it: proof of possession is an ADR-036 off-chain signature over a challenge the
backend issues, which common Cosmos wallets already produce, and a vouch is the voucher's signature over a document
naming the admission; neither signs a transaction or adds a legacy sign mode to transaction signing. The first tranche
needs the queries, transactions, ADR-036 signature, and decoding the app needs.

## Client

### `ark-signer`

The signer interface is the audited, slow-moving part of the app, and it owns nothing about membership: key generation
on the device with backup and recovery, signing consent, transaction review built from decoded messages, fee selection,
broadcast, and reading the chain from any node. Every feature asks it for a signature and never sees a key. The device
keystore answers by default; a Ledger device, through the adapter for `ledger-ark`, or a wallet a crypto-native member
already carries stands in behind the same interface. The first tranche needs the device keystore and the Ledger adapter;
external-wallet adapters follow.

A multisignature account signs through the same interface. The signer builds the transaction for a K-of-N account,
each member signs it with their own adapter, keystore or Ledger, and partial signatures pass between members until the
threshold is met and one of them assembles and broadcasts. Three constraints are fixed elsewhere. A committee is a
legacy amino threshold multisig of at most ten hardware-backed keys, registered on chain before its appointment
([genesis](../governance/GENESIS.md#committee-appointments)), and the SDK refuses direct signing for a multisig, so its
members sign in amino JSON or textual mode: the deliberate compatibility decision the
[signing rule](#one-meaning-per-transaction) asks for. Collection stays private, because a committee's target must not
be public before broadcast ([committee runbook](../governance/COMMITTEE_RUNBOOK.md#submitting-a-transaction)), so
partial signatures move through a relay that holds unsigned bytes and signatures and never a key; which repository
holds the relay is open. A multisig's gas is simulated against its recorded key shape, never a single-key dry run. The
same flow serves any shared account a member holds, not only a committee.

It is the first form of the wallet kit. When third-party dapps arrive, the same interface extends into a dapp-to-wallet
protocol, so dapps integrate with a stable Ark wallet interface rather than couple themselves to one browser extension
or transport mechanism: account discovery, connection state, network switching, transaction requests, event
subscriptions, and consistent errors. The interface stays transport-neutral enough to support first-party web and
extension wallets, hardware wallets, mobile handoff, and selected compatible Cosmos wallets over time, and application
code never needs to understand private-key storage or the internal mechanics of each wallet. Permissions are explicit:
connecting an account, reading public addresses, requesting a signature, changing networks, and accessing any sensitive
metadata are different capabilities, never one indefinite blanket session.

It is not the authority for balances, quotes, or transaction success, and not an Ark-specific policy engine.

### `ledger-ark`

The Ledger app signs Ark transactions on a Ledger device, with the transaction shown on the device's display. It is
what the signer interface's Ledger adapter talks to, so a member who already carries a device registers and signs with
it in place of the device keystore. The first tranche needs it for the signup flow and for staking and voting the grant
from a Ledger. Until Ledger lists it in Ledger Live, members install it by sideloading a release
([install guide](https://github.com/ararat-network/ledger-ark/blob/main/docs/SIDELOAD.md)): the Nano X cannot, and each
release fits one firmware version, so the listing follows the first tranche rather than gating it. It is not a second
signer interface.

### `ark-app`

A first-party app gives Ark control over the workflows that define the chain. It is one app: it holds the wallet and
everything a member does, and grows past both. Its first user is fixed by the disbursement plan: a member who holds a
grant the module pays out over a year, has never seen a key, and must be able to stake and vote it. Custody sits behind
the signer interface; the features own everything else a member does:

- signup with the member process in front of it, the vouch or the identity check, with the signals the vouch path leans
  on, one Apple or Google account, device attestation, and a phone number, submitting the address with a proof of
  possession;
- the grant as the member sees it, what has been paid and what waits, the schedule and the next payment, and the
  member's status read from the Disbursement module;
- the distribution as anyone may check it, readable before signup: the open tranche, the member count against the
  step, and the figures the step review publishes, each at the height it was read;
- the delegation of each payment that arrived, and the validator picker that shows how close each validator is to a
  third and defaults away from the founding set;
- every open proposal put in front of the member;
- sending paid coins out.

The app pays gas from the tenth the block sends two weeks after registration, so the member never has to fund it, and
speaks the member's language ([languages](#languages)). The first tranche needs signup, then the grant view, the
distribution screen, staking, and voting before the first tenth pays.

Its later scope covers simulation and comprehensible signing review, fee and tax presentation, Ark market operations
with explicit pricing and slippage, a contributor's few interactions with the Disbursement module such as changing its
destination or rotating its controller, account recovery, external hardware wallets behind the signer interface, and
multisignature: a committee member reviews and signs the committee's acts from the app, with the same decoded review
as any other transaction, and a shared account a member holds signs the same way.

Its explorer pages, on the indexer and readable without an account, are where anyone inspects the chain: each
transaction decoded, its messages, events, balance changes, fees, taxes, and result, with Ark's market, oracle, and
treasury events understood beside the standard Cosmos messages; the distribution, each grant's original clock, accrued
unpaid entitlement, next due payment and payee, and the plan's numbers at the height they were read, public bonded
stake, the cap a grant was checked against, each step's headcount and fake rate, and the handover lines; and the
network, block production, validator status, governance, oracle activity, upgrades, and health, without pretending
that every off-chain observation is consensus state.

The app defines the quality bar for message decoding and user safety, while the signer interface and SDK make it
possible for other wallets to reach the same level of integration. It is not a key store outside the signer interface,
not a second source of chain state, and not the only way to inspect chain activity.

## Backend

The backend behind the app is the membership service with its registrar signer, and the plan bounds it with rules rather
than norms. On a public chain a leaked link between a person and an address exposes that person's whole history, past
and future, so nothing the backend holds names a person ([identity data](#identity-data)). Services that manage
identities for claims coverage are intended later and not designed here.

The backend holds no member coins and no authorisation over any member account, and it is not a vote. The committee's
signature and the registrar key are security components on a par with the app's key custody: through its registrar the
committee can commit the open tranche, which pays nothing for the two-week hold, pause every member's pay, and commit
its term's allowance into awards that pay nothing for a period, and an expedited governance vote revokes it, reinstates
the members it wrongly suspended, and cancels its awards and registrations. The membership service's uniqueness store
ranks with them. Where members delegate is their own choice, steered only by the app's defaults. Registration callers
pay normal transaction gas; payments arrive without one. Disbursement custody reserves award principal and provides no
fee allowance; members pay their own gas from the first payment, the tenth the block sends once the two-week hold ends.

The backend repository is private, since published monitoring rules are a guide to evading them. Its app-facing API is
defined beside its code; the app's builders read the repository and commit the generated client, so no public build
needs a credential for a private repository.

### `ark-membership`

The membership service runs the member process the [plan](../governance/DISBURSEMENT_PLAN.md#admission) sets: admission
by a vouch, and by a verified identity once the check is live before the plan's step 2; the vouch graph with each
member's standing, limits, and liability, which it reads for sybils and keeps private; a daily comparison of the chain's
registrations against its admission records, inside the two-week hold before anything pays, exported with the tranche
it fills as the series the [Disbursement alerts](../operations/PROTOCOL_MONITORING.md#7-disbursement) read;
grant-tranche funding requests to governance; and the committee's suspend, which stops a sybil's pay and moves nothing,
for governance to cancel. A flag suspends the vouchee and the voucher, a finding cancels both, and cancelling a member
suspends their unverified vouchees until they verify or are vouched for again.

The registrar key is a KMS key on the service's own server: the service signs registration batches of up to a hundred
addresses with it, the committee appoints and replaces it, and the open member tranche is its only bound. The key never
leaves the KMS, so a breach of the service can sign only while it lasts and only within the tranche, and the committee
ends it by replacing the registrar. Two controls hold that bound: a key policy under which only the service's cloud role
can sign, and the KMS audit log shipped where the service cannot alter it, so a committee member can compare the
signatures against admissions inside the hold. The committee may name itself registrar and register the first cohort
before the service exists.

Registration needs uniqueness, one grant per person, not identity: the identity verifier, a third party that checks
liveness and a document, returns a uniqueness attestation and the earlier enrolments whose face matches this one. The
service derives the dedup token from that: a match above its threshold takes the earlier enrolment's token, so the same
person always resolves to one token however many times they verify. It keeps the attestation, the token, and the
verifier's reference beside the admission record; nothing it keeps names a person. A vouched-only member has no
attestation and no token: their uniqueness rests on the voucher's liability, and, once a verifier exists, the check is
what they pass to vouch themselves. It is not a custodian, a delegator, a vote, a holder of any key but the registrar's,
or a holder of anything that names a person.

## Information layer

### `ark-indexer`

A node API serves current state and transaction submission; it is not an ergonomic historical database. Ark will need an
indexer for account history, transaction search, validator activity, governance history, contract activity, and product
analytics. It is also the collector
[protocol monitoring](../operations/PROTOCOL_MONITORING.md#27-collection-requirements) requires: it exports the
typed-event series and hosts the query poller that the rules in `ark` read.

None of that is needed for the first tranche. What the plan promises anyone can check is served by the Disbursement
module's own queries, its totals, a grant's paid and remaining principal, a person's figures and live limit, a member's
status, each account's balance and reservation, and the open member tranche, together with the staking pool and the
governance proposals, all read from a node at a height. The indexer comes after.

The indexer is replayable from canonical chain data and records the block height from which every result is derived.
It tolerates restarts, delayed blocks, transaction failures, software upgrades, and schema evolution, and an indexed
result never silently overrides a conflicting chain query. That boundary is what lets search infrastructure scale and
be redesigned without placing consensus or fund safety at risk. It is not consensus state or an irreplaceable ledger.

## Rules across repositories

### Key custody

Key custody is the highest-risk part of the user-facing stack. Key generation, encryption, recovery, session isolation,
dependency updates, hardware-wallet support, signing-device displays, and release distribution need explicit security
ownership; a polished interface cannot compensate for an ambiguous custody or update model. Two further keys rank with
the app's, the committee signature that issues member grants and compensation and the registrar key, and the
[backend](#backend) bounds them.

Transaction review is based on decoded, structured messages. Raw JSON or opaque bytes are not an acceptable default for
common Ark operations, and unknown messages and unverified contract payloads are presented as such rather than given a
reassuring generic label.

### Identity data

Nothing the backend holds names a person: the uniqueness store links a dedup token, which only the verifier can map to a
person, to an address, the vouch graph links addresses to each other, and whether either is personal data in law is
counsel's question. Nothing chain-visible carries either: registration batches, claim references, and grant records name
records kept off chain, never people. A service that held the link between people and addresses would be the one place a
breach ties known people to public addresses permanently, so none holds it until a use needs it, and then one alone.

### Fees, taxes, quotes, and simulation

Ark's economics make simulation and transaction explanation especially important. A client separates the amount the
user intends to spend; network fees and their denomination; any protocol tax or module-level charge; quoted and minimum
received amounts; price age or oracle conditions when relevant; and simulation estimates from values guaranteed by
transaction validation. Values derived from mutable chain state carry the relevant height or expiration assumption, and
clients handle state changing between simulation and inclusion.

[Client fee construction](../clients/CLIENT_FEES.md) is the transaction contract: products show the declared maximum,
explain the gas/tax split, choose explicit headroom, and handle a refusal after state changes. arkd's current gas
adjustment and denomination preference are not protocol requirements; they are documented with the
[CLI implementation](../../cmd/arkd/README.md#cli-fee-completion).

### One meaning per transaction

Protobuf schemas, type URLs, events, and error semantics are a public integration contract even when they are not
marketed as one, so tooling is considered when these surfaces change. The SDK, app, and indexer give each
transaction one meaning: what they share is the proto schemas and typed events, with the
[decoding fixtures](../../tests/fixtures/README.md) kept in `ark` that every decoder, in TypeScript or Go, must read the
same way. Stable decoders and compatibility tests are worth more than multiple clients each guessing how to interpret
loosely structured events.

Textual signing can improve legibility, but it needs accurate coin metadata and type rendering. Direct signing remains a
first-class path, and any legacy signing support is a deliberate compatibility decision rather than the foundation of
new tooling.

### Languages

The app speaks the languages of the starting assets: for each of the ten currencies
[registered after launch](../governance/GOVERNANCE_RUNBOOK.md#opening-conversion), the official languages of its
issuing area. English, French, Spanish, Chinese, Japanese, Korean, Malay, and Tamil follow from AUD, CAD, CNY, GBP, JPY,
KRW, MXN, SGD, and USD; which of the euro area's official languages count is open, as is whether the first tranche
ships every language or the ones its members need. Every surface a member reads is in scope: the app, the signer's
consent and transaction review, and the display names the registry carries; the Ledger app's display
stays English. A translation renders the same decoded fields as every other language, never a different reading of
the transaction, so the one-meaning rule holds across languages; strings live outside code from the first release, so
a language is a resource rather than a build; and amounts, dates, and currencies format by the member's locale.

### Infrastructure reliability

First-party tooling depends on public RPC, gRPC, REST, WebSocket, indexer, and asset-metadata services, which need
redundancy, rate limits, observability, version reporting, and documented failure behaviour. The app distinguishes node
unavailability, indexer lag, transaction rejection, and an unconfirmed broadcast. Consumers can select or operate
alternative endpoints wherever possible: sovereign tooling does not mean one hosted API becomes an unavoidable central
point of failure.

### Versioning and upgrades

Chain, SDK, app, and indexer releases will not always move in lockstep. Compatibility ranges and feature
detection are explicit, tooling degrades safely on an unknown module version or message type, and upgrades are testable
against captured or replayed chain data before activation.

### Ecosystem participation

First-party products establish defaults without making the ecosystem permissioned. Public schemas, generated clients,
message decoders, network metadata, wallet interfaces, and local fixtures are reusable by third-party wallets,
explorers, exchanges, and dapps. The strongest identity comes from a recognisable Ark experience combined with open
integration boundaries, not from requiring every user to depend on a single frontend.

## Decision checkpoints

The direction is working when:

- a new application can query Ark and request safe transactions through supported public interfaces without copying code
  from the node;
- a member with no technical background can receive, stake, and vote a grant from the app without a key, a CLI, or a
  fee;
- a member can use the app, and read what they sign, in their own language;
- a committee signs its acts from the app without a CLI, and nothing it signs is public before broadcast;
- every grant can be checked against the plan's rules from public interfaces;
- nothing the backend holds or the chain shows names a person;
- the same transaction is decoded consistently by the app and the SDK;
- a user can understand the assets, fees, taxes, quotes, permissions, and likely outcome before signing;
- historical views can be rebuilt from chain data and expose their synchronization height;
- a developer can reproduce core workflows locally using the same conventions as a public network;
- third parties can replace a first-party endpoint or interface without losing access to the protocol;
- Ark's identity is apparent in its workflows and economics rather than being reduced to cosmetic branding around
  generic tools.

## Non-goals

This direction does not:

- define a complete implementation plan, staffing model, or timeline;
- require every component to be built before Ark can launch;
- require Ark to reproduce Terra's historical code or service architecture;
- adopt EVM compatibility to inherit MetaMask, Solidity, or EVM explorer support; it is reconsidered when Ark has a
  concrete need to host EVM applications or reach EVM-native developers, and is not the default answer to a tooling
  problem;
- make the app or indexer authoritative for chain state;
- give any backend component custody of member coins or a way to freeze them;
- put personal data on chain or in any public reference;
- justify duplicating Ark's economic calculations in client libraries;
- rule out compatibility with existing Cosmos wallets and infrastructure.

The near-term architectural commitment is narrower: make Ark's native protocol easy to integrate with, easy to
understand, and safe to use through a coherent set of first-party tools.
