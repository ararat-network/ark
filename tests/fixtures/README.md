# Decoding fixtures

Real transactions, signed through the application's own encoder and run through real blocks, written with their sign
documents, bytes, results, and events. They are the contract every decoder of Ark transactions is tested against,
and the bytes every producer must reproduce: the app, the SDK, the explorer, and the compliance service read one
transaction one way because each passes these files. The [tooling direction](../../docs/direction/TOOLING_DIRECTION.md#one-meaning-per-transaction)
is why they exist.

## Run

```sh
go test ./tests/fixtures
```

verifies [testdata/decoding](testdata/decoding/) against the chain as built and that every message type the
application registers appears in some fixture, so an encoding change or a new message fails CI until the files are
regenerated or the message is pinned:

```sh
go test ./tests/fixtures -update-fixtures
```

Regeneration is a deliberate step in a chain release, reviewed like any other change to a public contract. A consumer
copies the directory in at a pinned `ark` tag.

## Coverage

[coverage_test.go](coverage_test.go) lists every message type the application registers and requires each to appear
in a fixture, at the top level of a transaction or inside a proposal, unless it is in `knownGaps` with its reason. The
list only shrinks. Three messages are gaps: submitting evidence needs two real conflicting votes, and the two
Wasm IBC messages are emitted by contracts over channels the chain does not have.

## The chain

[harness_test.go](harness_test.go) boots the application with nothing random: keys from fixed secrets, chain ID
`ark-fixtures-1`, genesis at a fixed instant, one second a block unless a step jumps the clock. Its genesis appoints
one key as every module's committee, appoints a registrar, opens a member tranche of three grants, funds the Reserve,
Insurance, and contributor custodies, and taxes at the launch rate and cap. The validator reports a fixed rate for
every feed, so the chain holds prices from height two.

[keys.json](testdata/decoding/keys.json) lists the signers and the validator, whose operator signs with the same
scalar as its consensus key. They are test keys; never fund them on a public network. Signatures are deterministic,
so a producer signing with the same key must reproduce the signature bytes, not merely a valid signature.

## The sets

[fixtures_test.go](fixtures_test.go) runs the sets in this order on one chain; later fixtures see earlier state.
Each file's `description` says what it pins.

| Set | Fixtures | What it pins |
| --- | --- | --- |
| First tranche | 11 | What the app and the registrar send before the first payment, plus an ante refusal and an execution failure |
| Committee | 26 | Every message a committee or contributor key signs, each under its module's mandate, with the reserve position's whole lifecycle |
| Ark proposals | 8 | One proposal per Ark module carrying every authority message the module has |
| Cosmos | 28 | The Cosmos SDK messages a key signs: authz, bank, feegrant, distribution, gov v1 and v1beta1, staking, slashing, vesting |
| Wasm | 9 | Storing the counter contract, both instantiations, execute, migrate, admin changes, and the upload config |
| Cosmos proposals | 3 | Every Cosmos SDK and Wasm authority message, split so no proposal exceeds the block's transaction size |
| Payment block | 1 | The block that pays each member's tenth two weeks on: no transaction, only EndBlock events |

A fixture whose name ends in `_refused` records a refusal on purpose, the message pinned together with the error a
decoder must show. Authority messages are pinned as proposal content, which is the only shape they have on chain;
the proposals are not voted on, so their execution events are not recorded.

## The files

Each transaction file holds:

| Member | Content |
| --- | --- |
| `chain_id`, `height`, `block_time` | The block the transaction was included in; `block_time` in Unix seconds |
| `signer` | Key name, address, compressed public key, account number, sequence |
| `fee` | The fee contract applied at that height: the tax the messages owe, the chosen denomination's gas price with its derived height, the gas leg, the padding, and what was declared |
| `sign_mode_direct` | The direct sign document, and the body and auth-info bytes it is built from |
| `sign_mode_amino` | The amino JSON sign bytes a Ledger signs over the same transaction |
| `signature` | 64 bytes, `r || s`, low-S |
| `tx_bytes`, `tx_hash` | The raw transaction and its SHA-256, as a node returns them |
| `tx` | The transaction decoded by the application's codec, as proto JSON: the decoding contract |
| `result` | Code, codespace, log, gas wanted and used |
| `events` | The transaction's events as the node emits them, each typed event also decoded to proto JSON under `typed` |
| `block_events` | Where a fixture asks for them: the block's own events, Ark's typed events and the bank movements beside them |

Integers that may exceed 2^53 are decimal strings, as in the chain's JSON. Bytes are standard base64. The store-code
fixture carries the counter contract gzipped, as the CLI sends it, from [testdata/counter.wasm.gz](testdata/counter.wasm.gz).

## For a decoder

- A block event carries a `mode` attribute whose value is a bare word, not JSON. The SDK's own typed-event parser
  fails on it. Drop `mode` before decoding a block event; the files decode it that way.
- An execution failure from a handler is code 1 in codespace `undefined` with the refusal in `log`. Only a registered
  module error carries its own code, and the ante's refusals carry the SDK's.
- `gas_used` moves with store layout between chain versions. Compare it only against the same tag.
- Fee padding is the CLI's policy, recorded so the declaration can be reproduced, not a rule of the chain. The
  [fee contract](../../docs/clients/CLIENT_FEES.md) is the rule.
