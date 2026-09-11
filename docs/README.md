# Ark documentation

Choose a reading path below. These guides own cross-subsystem explanations and reader workflows; subsystem READMEs
own local behaviour, rationale, and development. The [placement rule](#documentation-placement-rule) explains how to
maintain that boundary.

## Understand the chain

- [Manifesto](papers/MANIFESTO.pdf): "The Revolution Begins Within", the founding statement on intelligence, love, nature,
  technology, and the future we choose.
- [Whitepaper](papers/WHITEPAPER.md): "Stability and Capital", why the design is what it is. A Markdown draft;
  the typeset PDF replaces it at this path when finished.
- [Economic design](design/ECONOMIC_DESIGN.md): current ownership, custody, conversion, capital, fee, and reward contracts.
- [Threat model](design/THREAT_MODEL.md): trust boundaries, assumptions, controls, and security review questions.

## Launch a network

- [Genesis](governance/GENESIS.md): curated launch settings, unresolved values and appointments, and final validation.

## Operate services

- [Node operations](operations/NODE_OPERATIONS.md): build, testnet setup, network joining, configuration, upgrades, and relaunch.
- [Release verification](operations/RELEASE_VERIFICATION.md): verify container origin, revision, digest and signed dependency inventory before use.
- [Pricefeed operations](operations/PRICEFEED_OPERATIONS.md): two-way node/sidecar setup, TLS, reload, failover, and independent releases.

## Monitor the network

- [Process monitoring](operations/PROCESS_MONITORING.md): node and sidecar scrape/export configuration, process metrics, and alerts.
- [Protocol monitoring](operations/PROTOCOL_MONITORING.md): chain-state queries/events, exposure and capital signals, and alert calibration.

## Govern and respond

- [Governance operations](governance/GOVERNANCE_OPERATIONS.md): proposals, dependent relayer actions, service permissions, and appointments.
- [Economic committee runbook](governance/ECONOMIC_COMMITTEE_RUNBOOK.md): bounded policy changes, fund actions, and responses to economic alerts.
- [Emergency submission runbook](governance/EMERGENCY_SUBMISSION_RUNBOOK.md): signing, private carriers, submission, inclusion checks, and rehearsal.

## Integrate clients

- [Client fee construction](clients/CLIENT_FEES.md): external transaction builders, signed fee declarations, simulation, and failure handling.
- [Node–sidecar compatibility](operations/PRICEFEED_OPERATIONS.md#nodesidecar-compatibility): the independently released transport contract.

## Understand future direction

- [Future changes](direction/FUTURE_CHANGES.md): deferred engineering, prerequisites, and revisit triggers; not shipped behaviour.
- [Tooling direction](direction/TOOLING_DIRECTION.md): proposed product boundaries and sequencing for clients, wallets, indexing, and explorers.

## Trace decisions

- [Economic decisions](design/ECONOMIC_DECISIONS.md): stable D/P identifiers, recorded rationale, amendment trails, and superseded choices.
  Use economic design for the current contract and genesis for pending launch values.

## Find subsystem documentation

- [Application](../app/README.md), [ante/post handling](../app/ante/README.md), and [mempool](../app/mempool/README.md).
- [Asset](../x/asset/README.md), [Oracle](../x/oracle/README.md), [Market](../x/market/README.md),
  [Treasury](../x/treasury/README.md), [Claims](../x/claims/README.md), [Reserve](../x/reserve/README.md),
  and [Security](../x/security/README.md).
- [ABCI](../abci/README.md) and [oracle aggregation](../abci/oracle/README.md).
- [Pricefeed](../pricefeed/README.md), [provider development](../pricefeed/sidecar/providers/README.md), and [shared packages](../pkg/README.md).
- [Testing](../tests/README.md), [proto generation](../proto/README.md), and [localnet/rehearsal tooling](../contrib/README.md).
- [Repository guidelines](../AGENTS.md).

## Documentation placement rule

A directory README owns its subsystem's purpose, behaviour, design rationale, interfaces, and development guidance.
`docs/` owns cross-subsystem explanations and end-to-end reader workflows. Each subject has one authoritative home;
other documents summarise and link to it. Choose the home by ownership and audience, not length.

| Location | Content |
| --- | --- |
| Root `README.md` | Project purpose, a short build/run quickstart, repository map, and documentation links. |
| Subsystem `README.md` | Purpose, ownership, behaviour and invariants, design rationale, state and interfaces, entry points, and how to test or extend the subsystem. |
| `docs/` topic documents | Cross-module protocol and economic design, trust boundaries, operator runbooks, client workflows, launch policy, and future plans. A workflow may concern one subsystem; its internal design still belongs with its owner. |
| Code comments and Go documentation | Function contracts, local invariants, arithmetic reasoning, and implementation details needed while editing or calling the code. |

### Layout

Each directory under `docs/` is a reader path; a new topic document goes with the audience that acts on it.

| Directory | Audience and content |
| --- | --- |
| `papers/` | Everyone. The manifesto and the whitepaper. |
| `design/` | Protocol contributors and reviewers. The economic contract, its decision record, the threat model. |
| `operations/` | Node and sidecar operators. Running, connecting, and monitoring services. |
| `governance/` | Governance, committees, and relayers. Launch settings, proposals, and runbooks. |
| `clients/` | External transaction builders. Fee construction and submission. |
| `direction/` | Contributors. Deferred changes and proposed tooling; none of it is shipped behaviour. |

A subsystem README can explain design in depth and use a contents list when useful. "Design" is not a reason for a
separate topic file: mempool reservations belong with the mempool, and asset lifecycle belongs with Asset. Provider
construction belongs beside provider code; deploying the node and sidecar together belongs in an operator guide.

### Maintenance rules

1. Create READMEs at meaningful subsystem boundaries. A child README must add useful information beyond its parent.
   Do not require a README in every `keeper/`, `types/`, `metrics/`, generated-code, fixture, or test-helper directory.
   Existing useful Go package documentation can fulfil the local documentation need without a duplicate README.
2. Avoid duplicated specifications. Brief summaries are welcome, but formulas, parameter tables, compatibility rules,
   and procedures each have one maintained home. Link to that home where another audience needs the subject.
3. Keep current behaviour, proposals, and superseded history distinguishable. Deferred code and dependency changes
   belong in [FUTURE_CHANGES.md](direction/FUTURE_CHANGES.md); post-launch governance and relayer operations belong in
   [GOVERNANCE_OPERATIONS.md](governance/GOVERNANCE_OPERATIONS.md). Label unimplemented proposals and historical decisions explicitly.
4. Make documentation discoverable. The root README links here; this index links topic documents and subsystem entry
   points. Parent READMEs link their useful child READMEs, and local READMEs link the relevant topic documents.
5. Update the authoritative document with the behaviour it describes. Before removing or moving a document, preserve
   its useful content in the chosen home and update inbound links and section references, including contributor guidance.
   Verify commands and examples against the current checkout rather than copying stale instructions into a new home.

The contributor check is: **If this behaviour changes, is there an obvious single document I must update?** If the
same detailed explanation must change in several places, tighten the ownership boundary.

A subsystem README normally covers purpose and boundaries, behaviour and rationale, state/interfaces, code entry
points and flow, focused development/testing instructions, and related documents. Omit sections that add no useful information; this is a guide, not mandatory boilerplate.

## Document conventions

Start with purpose, audience, and scope. Long references need a contents list. A procedure should state prerequisites,
ordered actions, result verification, and failure handling where relevant; a planning document should distinguish
shipped foundations from proposed work and name its trigger. Do not add empty template sections.

Monitoring owns alert conditions and diagnostics; runbooks own decisions and actions. Preserve alert IDs when moving
sections. Decision IDs stay stable, including when a decision is amended or superseded. Prefer links to named sections
or decision anchors over bare section numbers in another document.

Resolved fixes should describe the current state in reference documents; preserve useful history in the decision
record. Completed migration catalogues are not maintained navigation. Keep this index and each subject's authoritative
home up to date instead of creating another directory inventory.
