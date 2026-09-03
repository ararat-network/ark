// Package app assembles the Ark chain: the module set, the wiring between
// keepers, and every surface a transaction or packet crosses on its way into
// module code.
//
// The package layers as follows, each file in exactly one layer:
//
//   - Chain identity: params/, config.go. Process-global constants, set and
//     sealed before anything else runs.
//   - Module set and policy: app_config.go. The declarative module list,
//     account permissions, and lifecycle orderings depinject consumes.
//   - Assembly: app.go. ArkApp and the construction sequence, including the
//     hand-wired cross-module hooks depinject cannot express.
//   - Transaction admission: the ante/ subpackage, mempool.go. Lane ordering,
//     the decorator chain, fees, and transfer tax routing.
//   - Consensus hooks: oracle.go. Vote-extension, proposal, and preblock
//     handler installation, and the oracle client lifecycle.
//   - Execution surfaces: ibc.go, gmp.go, wasm.go, wasm_query.go,
//     execution_policy_router.go. The routed ways in from outside; contract,
//     GMP, and ICA dispatch share the execution policy router.
//   - State lifecycle: genesis.go, export.go.
//   - Harness: test_helpers.go. Production code, not test-only: the testnet
//     command builds on NewTestNetworkFixture.
//
// The client/ subpackage is the submission-side mirror of ante/: it prices
// the transfer tax into a transaction's fee before broadcast.
//
// The test files outnumber the code because app/ doubles as the chain's
// cross-module integration suite: properties that only exist once real
// keepers meet — settlement ordering, lifecycle seams, committee bounds —
// are pinned here, next to the wiring that creates them.
package app
