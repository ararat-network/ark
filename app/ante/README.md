# Transaction ante and post handling

[ante.go](ante.go) assembles the complete signed-transaction ante chain and the transfer-tax post handler.
The SDK runtime's predefined ante handler is skipped in app configuration. Decorator order determines error precedence,
gas charged, and when state writes occur, so edits must account for the surrounding SDK chain.

## Code map

| Files | Responsibility |
| --- | --- |
| [fee.go](fee.go) | Signed fee requirements, transfer-tax estimate, fee deduction, and priority. |
| [transfer_tax.go](transfer_tax.go) | Transfer tax committed with successful message execution in the post handler. |
| [gas_tally.go](gas_tally.go) | Per-block gas accounting consumed by Treasury's fee controller. |
| [privilege.go](privilege.go) | Authenticated lane classification after signature verification. |
| [gov.go](gov.go), [gov_vote.go](gov_vote.go) | Governance eligibility/validation and the vote fee floor. |
| [authz.go](authz.go), [multisend.go](multisend.go) | Wrapped-message and multisend policy. |

Ante validates signed message/fee policy before fee deduction. Signature checks precede privilege classification, so its
keeper reads see authenticated signers and post-fee balances. An ante failure discards ante writes; after successful ante,
the gas fee can stand even if messages fail. Transfer tax belongs to the successful message/post branch. The separate
[mempool admission wrapper](../mempool/admission.go) checks storage capacity while ante writes are still disposable.

The [execution-policy router](../execution_policy_router.go) also covers messages created during execution. Changes to
signed-message policy must consider that boundary rather than assuming every message arrives in a signed top-level batch.

## Transfer-tax commitment and rollback

Ante computes the transfer tax once, checks its signed declaration, and carries the amount through context.
It deducts gas and the NOAH tip, drawing only that feegrant allowance. The post handler charges tax after successful
messages in finalisation and simulation, against the balances and allowance those messages left. Missing context is
an error rather than an untaxed transfer; zero tax is still explicitly carried. Admission, recheck, and proposal
verification do not rehearse tax collection.

| Outcome | Gas and sequence | Messages and tax | Feegrant usage |
| --- | --- | --- | --- |
| Ante refuses | Reverted | Not executed | None |
| Message fails | Committed after successful ante | Reverted; no tax | Gas only |
| Post cannot collect tax | Committed after successful ante | Reverted; no tax | Gas only |
| Success | Committed | Committed together | Gas and tax |

A transaction may acquire its tax denomination during execution. A pre-execution affordability check would reject
that valid case and duplicate balance/allowance reads without reserving anything. Gas remains payable for a transaction
that ultimately cannot fund tax. Exhausting or revoking a grant before the post draw makes that draw fail with the
message branch rolled back; allowance usage records only charges that stand.

Charging tax in ante and refunding in post was rejected because failed messages discard post writes. Charging signed
messages in the router loses the outer fee granter. Recomputing in post duplicates metered work and weakens the
identity between declaration and charge. This is deferred collection, with no escrow or refund.
Execution-generated messages use the [policy router](../README.md#ibc-and-wasm-integration), whose tax and transfer
share the dispatch cache. Later IBC timeouts retain the tax under the [economic transfer contract](../../docs/design/ECONOMIC_DESIGN.md#111-the-execution-tax-contract).

Fee-less simulation charges `SimulatedFeeTransferGas` (38,000) for the gas-fee transfer it cannot execute. This covers
the two-denomination stable-base-fee plus NOAH-tip path conservatively; a simulation with a payable fee meters its
actual transfer. The tax post handler still runs during simulation so its transfer cost is included.

## Development

From the root, run `go test ./app/ante/...`. For fees, execution-generated messages, or lane integration, also run the
relevant tests under `./app`. Keep local rollback and ordering reasoning beside the decorator that enforces it.

[Client fee construction](../../docs/clients/CLIENT_FEES.md), [economic policy](../../docs/design/ECONOMIC_DESIGN.md), and
[mempool policy](../mempool/README.md#admission-service-and-sdk-behaviour) own the externally meaningful rules; this README maps their implementation.
