# Ark First-Party Tooling Direction

For product and integration contributors. This document records a proposed direction for Ark's user and developer
tooling. It is intended to guide product and architecture decisions without prescribing a complete implementation plan,
delivery schedule, or package layout.

The current direction is to make Ark easy to use through a coherent, first-party, Cosmos-native toolchain rather than
adopting EVM compatibility primarily to inherit MetaMask, Solidity, or EVM explorer support. EVM compatibility can be
reconsidered if Ark later has a concrete need to host EVM applications or reach EVM-native developers, but it should not
be the default answer to a tooling problem.

Ark already has useful foundations for this direction: Cosmos SDK protobuf and gRPC APIs, gRPC-gateway support, direct
and textual signing modes, the `ark` Bech32 prefix, and BIP-44 coin type `330`. The main challenge is to turn those
protocol capabilities into a dependable product surface.

The [distribution plan](../governance/DISTRIBUTION_PLAN.md) turned part of this direction into a dependency with a date:
its first member tranche needs the member app, the platform behind it, and the tooling around the grant contract, and
nothing else here. Sections 4, 6, and 7 record what that requires; the rest is unchanged in intent and later in order.

## Contents

- [Design precedent](#design-precedent)
- [Recommended direction for Ark](#recommended-direction-for-ark)
- [Ownership boundaries](#ownership-boundaries)
- [Important considerations](#important-considerations)
- [Directional sequencing](#directional-sequencing)
- [Decision checkpoints](#decision-checkpoints)
- [Non-goals](#non-goals)

## Design precedent

Terra's useful precedent is separation of responsibility: the chain owns state and validation, a client SDK exposes
those interfaces, wallets own signing and user workflows, indexers derive history, explorers make it inspectable, and
shared network metadata connects the products. Ark should use that division of responsibility without inheriting an
obsolete package layout or fee assumption.

## Recommended direction for Ark

Ark should aim for the same form of vertical integration, adapted to the current Cosmos SDK rather than reproducing
Terra's older implementation choices.

### 1. Keep `arkd` as the protocol authority

The chain should remain the only source of truth for economic and consensus-sensitive behavior. Market conversion,
treasury policy, oracle rates, taxes, fees, staking, governance, IBC state, and any future contract integrations must be
determined by committed chain state.

Client tooling may request quotes, simulate transactions, format results, and explain outcomes. It should not
independently reimplement policy calculations and present them as authoritative. If a user-facing workflow needs a value
that is difficult to obtain safely, the preferred solution is a clear query or simulation surface on the chain, not
copied business logic in every client.

### 2. Establish a canonical network registry

All first-party tooling should consume a shared, versioned description of Ark networks. At minimum, that registry should
be capable of describing:

- chain IDs and environment names;
- Bech32 prefixes and coin type;
- native and supported fee denominations;
- display denominations, exponents, symbols, and icons;
- RPC, gRPC, REST, WebSocket, indexer, and explorer endpoints;
- genesis identity or another way to prevent endpoint/network confusion;
- IBC channels, denomination traces, and recognised asset metadata;
- enabled product features and compatibility ranges.

This information should not be copied into each wallet and dapp. A canonical registry reduces configuration drift and
makes testnet, mainnet, and local development behave consistently.

### 3. Build an Ark-native client SDK

An Ark SDK should be the primary integration surface for web applications and, where practical, other TypeScript
consumers. It should build on generated protobuf types and modern Cosmos transaction signing rather than depend on
legacy LCD assumptions.

Its useful responsibilities include:

- typed queries and messages for both standard Cosmos modules and Ark modules;
- transaction construction, simulation, fee selection, signing requests, and broadcast;
- address and denomination formatting;
- well-defined handling of direct, textual, and any intentionally supported fallback sign modes;
- decoding of transaction messages, events, errors, and module-specific results;
- network-registry consumption;
- a small set of higher-level workflow helpers where those helpers do not duplicate chain policy.

The SDK should expose protocol concepts accurately. Convenience abstractions are valuable, but hiding important behavior
such as slippage, tax, oracle freshness, IBC routing, or transaction finality would make the tooling easier to misuse.

### 4. Ship contract tooling for the contracts the chain runs

The contract runtime ships open: upload and instantiation are permissionless from launch
([genesis §11](../governance/GENESIS.md#11-ibc-interchain-accounts-and-cosmwasm)), and the first first-party contract
exists, the grant contract under [contracts/](../../contracts/README.md), with the toolchain it needed: a Cargo
workspace on the Rust the chain's wasmvm accepts, `make contracts` for lint, tests, build, validation, and the embedded
artefact the application tests drive, CI that fails when the artefact drifts from source, and the optimizer build whose
checksum a store proposal cites.

What remains is the surface around it: published JSON schemas for the contract's messages and queries, so the SDK,
wallet, and explorer decode them from one source; explorer decoding of contract events and sudo and execute payloads;
wallet presentation of the few contract messages a person signs, a contributor naming a release address and a member
never; and a localnet flow that stores, instantiates, funds, and drives the contract the way the runbook does. Contract
support still affects security, governance, state growth, and decoding, which is why the first contract is one the
chain's own process needs, and why a general contract product remains a separate decision.

### 5. Offer a repeatable local Ark environment

A LocalArk-style environment should make it easy to start a known network, fund deterministic development accounts,
submit transactions, exercise Ark-specific modules, and reset or reproduce state. It should use the same registry and
SDK conventions as public environments.

The goal is not only convenience. A consistent local environment shortens integration feedback loops and gives wallet,
indexer, explorer, contract, and application developers a common fixture for testing protocol upgrades.

### 6. Make a first-party Ark wallet the reference experience

A first-party wallet gives Ark control over the workflows that define the chain, and its first user is fixed by the
distribution plan: a member who holds a 10,000 NOAH vesting grant, has never seen a key, and must be able to stake and
vote it. That user decides the order of work. The wallet is phone-first, and before anything else it must cover:

- signup with the member process in front of it, generating the key on the device with backup and recovery, and
  submitting the address for registration;
- the vesting account as the member sees it: locked and spendable balance, the schedule, and the member's status read
  from the grant contract;
- one-tap delegation, voting, and reward claims, with the plan's fee pool set as fee granter on every transaction, so
  the member never holds gas;
- the optional authorisations the plan bounds: delegation only, with the platform's spread, a vote only by choice,
  revocation one tap away, and every open proposal shown to the member;
- sending vested coins out, to an exchange or anywhere.

The operations the earlier draft listed remain the reference experience's later scope: simulation and comprehensible
signing review, fee and tax presentation, validator selection, Ark market operations with explicit pricing and slippage,
IBC transfers and denomination provenance, a contributor's few interactions with the grant contract such as naming a
release address, and account recovery, hardware-wallet use, and multisignature flows where supported.

The first-party wallet should define the quality bar for message decoding and user safety, while the wallet kit and SDK
make it possible for other wallets to reach the same level of integration.

### 7. Run the member platform without custody

The platform behind the member app is a component of its own, and the plan bounds it with rules rather than norms. It
owns the member process, vouching or verified identity and the registry of who is a member; the registrar signature, a
multisig, that registers batches of up to a hundred addresses with the grant contract inside the contract's issuance
window; the fee pool's funding requests to governance; execution under members' authorisations, delegation only, spread
across validators at no more than a tenth of the member stake it directs on any one and weighted outside the founding
ten, with no vote cast by default; and the release pokes an escrowed grant needs, since anyone may trigger a release and
someone must.

It holds no member coins, it is not a vote, and it keeps no identity data beyond what vouching needs. Its two key sets,
the registrar's and the authorisation executor's, are security components on a par with the wallet's key custody: the
first can misdirect a window's member grants, the second can move tens of thousands of delegations. Both are revocable,
the first by an expedited governance vote and the second by each member, and both act only through transactions on the
member's own account, visibly.

### 8. Build an explorer around Ark semantics

A useful Ark explorer should decode what transactions mean, not merely show raw protobuf JSON. It should understand
Ark's market, oracle, and treasury events in addition to standard Cosmos messages. Users should be able to follow the
relationship among a transaction, its messages, emitted events, balance changes, fees, taxes, and final result.

The distribution is the explorer's first job. It should decode the grant contract's events and its sudo and execute
payloads, render periodic vesting accounts with their schedules, show fee allowances and authorisations, and present the
plan's numbers with the height they were read at: public bonded stake, the cap a grant was checked against, each step's
gate, and the handover lines. A thin page on node queries meets round one; the full explorer follows the indexer.

The explorer is also an operational tool. It should make block production, validator status, governance, oracle
activity, IBC paths, upgrades, and relevant network health visible without pretending that every off-chain observation
is consensus state.

Shared decoding libraries between the SDK, wallet, indexer, and explorer will help prevent each product from assigning
different meanings to the same transaction.

### 9. Treat indexing as a separate derived-data system

A node API is suitable for current state and transaction submission, but it is not automatically an ergonomic historical
database. Ark will likely need an indexer for account history, transaction search, validator activity, governance
history, contract activity, and product analytics.

None of that is needed for the plan's first tranche. What the plan promises anyone can check is served by the grant
contract's own queries, its totals, a grant's releasable amount, a person's figures, a member's status, and the issuance
window, together with the staking pool and the governance proposals, all read from a node at a height. The indexer comes
after.

The indexer should be replayable from canonical chain data and should record the block height from which every result is
derived. It must tolerate restarts, delayed blocks, transaction failures, software upgrades, and schema evolution.
Indexed results should never silently override a conflicting chain query.

Keeping this boundary honest makes it possible to scale and redesign search infrastructure without placing consensus or
fund safety at risk.

### 10. Provide a wallet integration kit

Dapps should integrate with a stable Ark wallet interface rather than couple themselves to one browser extension or
transport mechanism. A wallet kit can provide account discovery, connection state, network switching, transaction
requests, event subscriptions, and consistent errors.

The interface should be transport-neutral enough to support first-party web and extension wallets, hardware wallets,
mobile handoff, and selected compatible Cosmos wallets over time. Application code should not need to understand
private-key storage or the internal mechanics of each wallet.

Permissions should be explicit. Connecting an account, reading public addresses, requesting a signature, changing
networks, and accessing any sensitive metadata are different capabilities and should not be collapsed into an indefinite
blanket session.

## Ownership boundaries

The following boundaries should remain clear as the stack grows:

| Component         | Owns                                                                                                                                            | Must not become                                              |
| ----------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------ |
| `arkd`            | Canonical state, validation, policy, execution, and protocol queries                                                                            | A product-specific UI backend                                |
| Network registry  | Shared public configuration and asset metadata                                                                                                  | A second source of chain state                               |
| Ark SDK           | Encoding, querying, transaction workflows, decoding, and formatting                                                                             | An independent implementation of economic or market policy   |
| Wallet kit        | Dapp-to-wallet session and request protocol                                                                                                     | A key store or an Ark-specific policy engine                 |
| Ark wallet        | Key custody on the member's device, signing consent, and reference user workflows                                                               | The authority for balances, quotes, or transaction success   |
| Member platform   | Vouching, the registrar signature, fee-pool funding requests, execution under members' authorisations within the plan's ceilings, release pokes | A custodian, a vote, or an identity database beyond vouching |
| Grant contract    | Custody of each tranche, member issuance, and the escrow released by the plan's rules                                                           | A policy engine beyond those rules, or a treasury            |
| Indexer           | Replayable historical and searchable derived data                                                                                               | Consensus state or an irreplaceable ledger                   |
| Explorer          | Human-readable verification and network visibility                                                                                              | The only way to inspect chain activity                       |
| Local environment | Reproducible development and integration fixtures                                                                                               | A simulation with materially different protocol behavior     |

## Important considerations

### Security and key custody

The wallet will be the highest-risk component in the user-facing stack. Key generation, encryption, recovery, session
isolation, dependency updates, hardware-wallet support, signing-device displays, and release distribution need explicit
security ownership. A polished interface cannot compensate for an ambiguous custody or update model.

Two further key sets rank with the wallet's: the registrar signature that issues member grants, and the executor that
acts under members' authorisations. Section 7 bounds both.

Transaction review should be based on decoded, structured messages. Raw JSON or opaque bytes are not an acceptable
default for common Ark operations. Unknown messages and unverified contract payloads should be presented as such rather
than receiving a reassuring generic label.

### Fees, taxes, quotes, and simulation

Ark-specific economics make simulation and transaction explanation especially important. A client should clearly
separate:

- the amount the user intends to spend;
- network fees and their denomination;
- any protocol tax or module-level charge;
- quoted and minimum received amounts;
- price age or oracle conditions when relevant;
- simulation estimates from values guaranteed by transaction validation.

Values derived from mutable chain state should be accompanied by the relevant height or expiration assumption. Clients
must handle the possibility that state changes between simulation and inclusion.

Use [client fee construction](../clients/CLIENT_FEES.md) as the transaction contract. Products must show the declared
maximum, explain the gas/tax split, choose explicit headroom, and handle a refusal after state changes. Do not turn
arkd's current gas adjustment or denomination preference into a protocol requirement; those choices are documented with
the [CLI implementation](../../cmd/arkd/README.md#cli-fee-completion).

### Message and event compatibility

Protobuf schemas, type URLs, events, and error semantics form a public integration contract even when they are not
marketed as one. Tooling should be considered when these surfaces change. Stable decoders and compatibility tests are
more valuable than multiple clients each guessing how to interpret loosely structured events.

Textual signing can improve legibility, but it needs accurate coin metadata and type rendering. Direct signing should
remain a first-class path. Any legacy signing support should be a deliberate compatibility decision rather than the
foundation of new tooling.

### IBC and asset identity

IBC assets require more than a ticker and icon. Tooling should preserve source chain, base denomination, channel path,
trace, and the distinction between canonical and community-recognised representations. A friendly alias must not make
two different voucher denominations appear interchangeable.

Network-registry and indexer data can improve presentation, but the underlying on-chain denomination remains the
identity used for balances and transactions.

### Infrastructure reliability

First-party tooling depends on public RPC, gRPC, REST, WebSocket, indexer, and asset-metadata services. These need
redundancy, rate limits, observability, version reporting, and documented failure behavior. The wallet should be able to
distinguish node unavailability, indexer lag, transaction rejection, and an unconfirmed broadcast.

Where possible, consumers should be able to select or operate alternative endpoints. Sovereign tooling should not mean
that one hosted API becomes an unavoidable central point of failure.

### Versioning and upgrades

Chain, SDK, wallet, indexer, and explorer releases will not always move in lockstep. Compatibility ranges and feature
detection should be explicit. Tooling should degrade safely when it encounters an unknown module version or message
type, and upgrades should be testable against captured or replayed chain data before activation.

### Ecosystem participation

First-party products should establish defaults without making the ecosystem permissioned. Public schemas, generated
clients, message decoders, network metadata, wallet interfaces, and local fixtures should be reusable by third-party
wallets, explorers, exchanges, and dapps.

The strongest identity comes from a recognisable Ark experience combined with open integration boundaries, not from
requiring every user to depend on a single frontend.

## Directional sequencing

This is a dependency order, not an implementation schedule:

1. **Stabilise the integration foundation.** Treat protocol schemas, network metadata, signing modes, transaction
   decoding, and public endpoints as a coherent surface. The local environment is part of it: it exists, and
   everything in the next step is proven on it.
2. **Build the distribution's dependencies, in this order.** The grant contract's schemas and its localnet flow
   first, since they are what the rest is tested against; then the member process and the registrar multisig, since
   the platform is built around them; then the platform's registrar flow and the fee pool; then the app, the last
   thing a member touches and the first thing they see. The contract's audit sits before the first tranche, not
   before the build. Transfers, simulation, market operations, and IBC follow.
3. **Add the information layer.** A thin distribution view on node queries with round one; replayable indexing and the
   full explorer once the meanings of messages, events, assets, and account activity are stable enough to share.
4. **Make third-party development routine.** Provide the wallet kit, documented interfaces, fixtures, and
   compatibility guidance needed to build without private knowledge.
5. **Expand execution environments deliberately.** CosmWasm is on, and its first contract shipped with its toolchain; a
   general contract product, or an EVM-facing one, is added only when application demand and operational costs justify
   it.

These areas can overlap in practice, but later layers should not compensate for unresolved protocol or metadata
contracts underneath them.

## Decision checkpoints

The direction is working when:

- a new application can query Ark and request safe transactions through supported public interfaces without copying code
  from the node;
- a member with no technical background can receive, stake, and vote a grant from the app without a key, a CLI, or a
  fee;
- every grant can be checked against the plan's rules from public interfaces;
- the same transaction is decoded consistently by the wallet, explorer, and SDK;
- a user can understand the assets, fees, taxes, quotes, permissions, and likely outcome before signing;
- historical views can be rebuilt from chain data and expose their synchronization height;
- a developer can reproduce core workflows locally using the same conventions as a public network;
- third parties can replace a first-party endpoint or interface without losing access to the protocol;
- Ark's identity is apparent in its workflows and economics rather than being reduced to cosmetic branding around
  generic tools.

## Non-goals

This direction does not:

- define a complete implementation plan, staffing model, timeline, or repository structure;
- require every component to be built before Ark can launch;
- require Ark to reproduce Terra's historical code or service architecture;
- make the wallet, indexer, or explorer authoritative for chain state;
- give any platform component custody of member coins;
- justify duplicating Ark's economic calculations in client libraries;
- rule out compatibility with existing Cosmos wallets and infrastructure;
- permanently rule out EVM support if a future application strategy needs it.

The near-term architectural commitment is narrower: make Ark's native protocol easy to integrate with, easy to
understand, and safe to use through a coherent set of first-party tools.
