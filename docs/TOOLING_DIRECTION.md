# Ark First-Party Tooling Direction

## Status and purpose

This document records a general direction for Ark's user and developer tooling. It is intended to guide product and architecture decisions without prescribing a complete implementation plan, delivery schedule, or package layout.

The current direction is to make Ark easy to use through a coherent, first-party, Cosmos-native toolchain rather than adopting EVM compatibility primarily to inherit MetaMask, Solidity, or EVM explorer support. EVM compatibility can be reconsidered if Ark later has a concrete need to host EVM applications or reach EVM-native developers, but it should not be the default answer to a tooling problem.

Ark already has useful foundations for this direction: Cosmos SDK protobuf and gRPC APIs, gRPC-gateway support, direct and textual signing modes, the `ark` Bech32 prefix, and BIP-44 coin type `330`. The main challenge is to turn those protocol capabilities into a dependable product surface.

## The model established by Terra

Terra did not achieve a cohesive user experience by making the chain itself imitate Ethereum. It assembled an integrated stack around a sovereign Cosmos SDK chain and made the pieces feel like one product.

The exact components and boundaries changed over Terra's lifetime, but the model broadly included:

| Layer | Terra-era example | Responsibility |
| --- | --- | --- |
| Protocol | Terra Core | Consensus, accounts, transactions, staking, governance, market and treasury logic, and contract execution |
| Node API | LCD, RPC, and later gRPC-style APIs | Canonical access to chain queries, simulation, transaction broadcast, and block data |
| Client SDK | Terra.js | Typed messages, queries, transaction construction, signing support, and common chain abstractions |
| Dapp connection | Wallet Provider | A consistent way for applications to discover wallets, request accounts, and submit transactions |
| First-party wallet | Terra Station | Key management and complete user workflows for transfers, staking, governance, swaps, and contracts |
| Indexed data | FCD and related services | Searchable transaction history, account activity, analytics, and other derived views that are inefficient to obtain from a node directly |
| Explorer | Finder | Human-readable blocks, transactions, accounts, validators, and contracts |
| Local development | LocalTerra | A repeatable local network and developer environment |
| Contract ecosystem | CosmWasm tooling and integrations | Contract deployment, querying, execution, schema-driven development, and wallet presentation |

The important lesson is not the names of those products. It is the division of responsibility:

- The chain remained the authority for balances, fees, policy, and state transitions.
- The SDK translated protocol APIs into application-friendly operations.
- The wallet combined secure signing with complete user journeys.
- The indexer made historical and cross-entity data convenient, but did not become authoritative state.
- The explorer made transactions legible enough for users and operators to verify independently.
- Shared network metadata kept wallets, dapps, explorers, and infrastructure aligned.

This gave Terra a strong identity without requiring every application team to solve basic chain integration independently.

## Recommended direction for Ark

Ark should aim for the same form of vertical integration, adapted to the current Cosmos SDK rather than reproducing Terra's older implementation choices.

### 1. Keep `arkd` as the protocol authority

The chain should remain the only source of truth for economic and consensus-sensitive behavior. Market conversion, treasury policy, oracle rates, taxes, fees, staking, governance, IBC state, and any future contract integrations must be determined by committed chain state.

Client tooling may request quotes, simulate transactions, format results, and explain outcomes. It should not independently reimplement policy calculations and present them as authoritative. If a user-facing workflow needs a value that is difficult to obtain safely, the preferred solution is a clear query or simulation surface on the chain, not copied business logic in every client.

### 2. Establish a canonical network registry

All first-party tooling should consume a shared, versioned description of Ark networks. At minimum, that registry should be capable of describing:

- chain IDs and environment names;
- Bech32 prefixes and coin type;
- native and supported fee denominations;
- display denominations, exponents, symbols, and icons;
- RPC, gRPC, REST, WebSocket, indexer, and explorer endpoints;
- genesis identity or another way to prevent endpoint/network confusion;
- IBC channels, denomination traces, and recognised asset metadata;
- enabled product features and compatibility ranges.

This information should not be copied into each wallet and dapp. A canonical registry reduces configuration drift and makes testnet, mainnet, and local development behave consistently.

### 3. Build an Ark-native client SDK

An Ark SDK should be the primary integration surface for web applications and, where practical, other TypeScript consumers. It should build on generated protobuf types and modern Cosmos transaction signing rather than depend on legacy LCD assumptions.

Its useful responsibilities include:

- typed queries and messages for both standard Cosmos modules and Ark modules;
- transaction construction, simulation, fee selection, signing requests, and broadcast;
- address and denomination formatting;
- well-defined handling of direct, textual, and any intentionally supported fallback sign modes;
- decoding of transaction messages, events, errors, and module-specific results;
- network-registry consumption;
- a small set of higher-level workflow helpers where those helpers do not duplicate chain policy.

The SDK should expose protocol concepts accurately. Convenience abstractions are valuable, but hiding important behavior such as slippage, tax, oracle freshness, IBC routing, or transaction finality would make the tooling easier to misuse.

### 4. Provide a wallet integration kit

Dapps should integrate with a stable Ark wallet interface rather than couple themselves to one browser extension or transport mechanism. A wallet kit can provide account discovery, connection state, network switching, transaction requests, event subscriptions, and consistent errors.

The interface should be transport-neutral enough to support first-party web and extension wallets, hardware wallets, mobile handoff, and selected compatible Cosmos wallets over time. Application code should not need to understand private-key storage or the internal mechanics of each wallet.

Permissions should be explicit. Connecting an account, reading public addresses, requesting a signature, changing networks, and accessing any sensitive metadata are different capabilities and should not be collapsed into an indefinite blanket session.

### 5. Make a first-party Ark wallet the reference experience

A first-party wallet gives Ark control over the workflows that define the chain. It should be more than an account balance and a send button. The reference experience should eventually cover the operations users most need to understand and trust, such as:

- receiving and transferring assets;
- transaction simulation and comprehensible signing review;
- fee and tax presentation;
- staking, rewards, and validator selection;
- governance participation;
- Ark market operations with explicit pricing and slippage;
- IBC transfers and denomination provenance;
- contract interactions if and when general contract execution is enabled;
- account recovery, hardware-wallet use, and multisignature flows where supported.

The first-party wallet should define the quality bar for message decoding and user safety, while the wallet kit and SDK make it possible for other wallets to reach the same level of integration.

### 6. Treat indexing as a separate derived-data system

A node API is suitable for current state and transaction submission, but it is not automatically an ergonomic historical database. Ark will likely need an indexer for account history, transaction search, validator activity, governance history, contract activity, and product analytics.

The indexer should be replayable from canonical chain data and should record the block height from which every result is derived. It must tolerate restarts, delayed blocks, transaction failures, software upgrades, and schema evolution. Indexed results should never silently override a conflicting chain query.

Keeping this boundary honest makes it possible to scale and redesign search infrastructure without placing consensus or fund safety at risk.

### 7. Build an explorer around Ark semantics

A useful Ark explorer should decode what transactions mean, not merely show raw protobuf JSON. It should understand Ark's market, oracle, and treasury events in addition to standard Cosmos messages. Users should be able to follow the relationship among a transaction, its messages, emitted events, balance changes, fees, taxes, and final result.

The explorer is also an operational tool. It should make block production, validator status, governance, oracle activity, IBC paths, upgrades, and relevant network health visible without pretending that every off-chain observation is consensus state.

Shared decoding libraries between the SDK, wallet, indexer, and explorer will help prevent each product from assigning different meanings to the same transaction.

### 8. Offer a repeatable local Ark environment

A LocalArk-style environment should make it easy to start a known network, fund deterministic development accounts, submit transactions, exercise Ark-specific modules, and reset or reproduce state. It should use the same registry and SDK conventions as public environments.

The goal is not only convenience. A consistent local environment shortens integration feedback loops and gives wallet, indexer, explorer, contract, and application developers a common fixture for testing protocol upgrades.

### 9. Add contract tooling deliberately

Ark contains a Wasm-facing exported integration surface, but that should not be mistaken for a complete decision to expose general contract execution in the running application. Contract support affects security, governance, state growth, transaction decoding, indexer schemas, wallets, developer tooling, and the messages that contracts may send into Ark modules.

If CosmWasm becomes part of the public product, Ark should provide an intentional end-to-end experience: supported runtime and permissions, Ark-specific query and message bindings, schemas, local testing, deployment tooling, explorer decoding, and safe wallet presentation. Enabling a runtime without those surrounding capabilities would reproduce the integration gap this strategy is meant to close.

## Ownership boundaries

The following boundaries should remain clear as the stack grows:

| Component | Owns | Must not become |
| --- | --- | --- |
| `arkd` | Canonical state, validation, policy, execution, and protocol queries | A product-specific UI backend |
| Network registry | Shared public configuration and asset metadata | A second source of chain state |
| Ark SDK | Encoding, querying, transaction workflows, decoding, and formatting | An independent implementation of monetary or market policy |
| Wallet kit | Dapp-to-wallet session and request protocol | A key store or an Ark-specific policy engine |
| Ark wallet | Key custody, signing consent, and reference user workflows | The authority for balances, quotes, or transaction success |
| Indexer | Replayable historical and searchable derived data | Consensus state or an irreplaceable ledger |
| Explorer | Human-readable verification and network visibility | The only way to inspect chain activity |
| Local environment | Reproducible development and integration fixtures | A simulation with materially different protocol behavior |

## Important considerations

### Security and key custody

The wallet will be the highest-risk component in the user-facing stack. Key generation, encryption, recovery, session isolation, dependency updates, hardware-wallet support, signing-device displays, and release distribution need explicit security ownership. A polished interface cannot compensate for an ambiguous custody or update model.

Transaction review should be based on decoded, structured messages. Raw JSON or opaque bytes are not an acceptable default for common Ark operations. Unknown messages and unverified contract payloads should be presented as such rather than receiving a reassuring generic label.

### Fees, taxes, quotes, and simulation

Ark-specific economics make simulation and transaction explanation especially important. A client should clearly separate:

- the amount the user intends to spend;
- network fees and their denomination;
- any protocol tax or module-level charge;
- quoted and minimum received amounts;
- price age or oracle conditions when relevant;
- simulation estimates from values guaranteed by transaction validation.

Values derived from mutable chain state should be accompanied by the relevant height or expiration assumption. Clients must handle the possibility that state changes between simulation and inclusion.

### Message and event compatibility

Protobuf schemas, type URLs, events, and error semantics form a public integration contract even when they are not marketed as one. Tooling should be considered when these surfaces change. Stable decoders and compatibility tests are more valuable than multiple clients each guessing how to interpret loosely structured events.

Textual signing can improve legibility, but it needs accurate coin metadata and type rendering. Direct signing should remain a first-class path. Any legacy signing support should be a deliberate compatibility decision rather than the foundation of new tooling.

### IBC and asset identity

IBC assets require more than a ticker and icon. Tooling should preserve source chain, base denomination, channel path, trace, and the distinction between canonical and community-recognised representations. A friendly alias must not make two different voucher denominations appear interchangeable.

Network-registry and indexer data can improve presentation, but the underlying on-chain denomination remains the identity used for balances and transactions.

### Infrastructure reliability

First-party tooling depends on public RPC, gRPC, REST, WebSocket, indexer, and asset-metadata services. These need redundancy, rate limits, observability, version reporting, and documented failure behavior. The wallet should be able to distinguish node unavailability, indexer lag, transaction rejection, and an unconfirmed broadcast.

Where possible, consumers should be able to select or operate alternative endpoints. Sovereign tooling should not mean that one hosted API becomes an unavoidable central point of failure.

### Versioning and upgrades

Chain, SDK, wallet, indexer, and explorer releases will not always move in lockstep. Compatibility ranges and feature detection should be explicit. Tooling should degrade safely when it encounters an unknown module version or message type, and upgrades should be testable against captured or replayed chain data before activation.

### Ecosystem participation

First-party products should establish defaults without making the ecosystem permissioned. Public schemas, generated clients, message decoders, network metadata, wallet interfaces, and local fixtures should be reusable by third-party wallets, explorers, exchanges, and dapps.

The strongest identity comes from a recognisable Ark experience combined with open integration boundaries, not from requiring every user to depend on a single frontend.

## Directional sequencing

This is a dependency order, not an implementation schedule:

1. **Stabilise the integration foundation.** Treat protocol schemas, network metadata, signing modes, transaction decoding, and public endpoints as a coherent surface.
2. **Prove core user journeys.** Use a small Ark SDK and reference wallet experience to validate transfers, simulation, fees, staking, governance, market operations, and IBC where enabled.
3. **Add the information layer.** Introduce replayable indexing and an explorer once the meanings of messages, events, assets, and account activity are stable enough to share.
4. **Make third-party development routine.** Provide the wallet kit, documented interfaces, local environment, fixtures, and compatibility guidance needed to build without private knowledge.
5. **Expand optional execution environments deliberately.** Add complete CosmWasm or EVM-facing product surfaces only when their application demand and operational costs justify them.

These areas can overlap in practice, but later layers should not compensate for unresolved protocol or metadata contracts underneath them.

## Decision checkpoints

The direction is working when:

- a new application can query Ark and request safe transactions through supported public interfaces without copying code from the node;
- the same transaction is decoded consistently by the wallet, explorer, and SDK;
- a user can understand the assets, fees, taxes, quotes, permissions, and likely outcome before signing;
- historical views can be rebuilt from chain data and expose their synchronization height;
- a developer can reproduce core workflows locally using the same conventions as a public network;
- third parties can replace a first-party endpoint or interface without losing access to the protocol;
- Ark's identity is apparent in its workflows and economics rather than being reduced to cosmetic branding around generic tools.

## Non-goals

This direction does not:

- define a complete implementation plan, staffing model, timeline, or repository structure;
- require every component to be built before Ark can launch;
- require Ark to reproduce Terra's historical code or service architecture;
- make the wallet, indexer, or explorer authoritative for chain state;
- justify duplicating Ark's economic calculations in client libraries;
- rule out compatibility with existing Cosmos wallets and infrastructure;
- permanently rule out EVM support if a future application strategy needs it.

The near-term architectural commitment is narrower: make Ark's native protocol easy to integrate with, easy to understand, and safe to use through a coherent set of first-party tools.
