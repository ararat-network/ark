# Emergency Submission Runbook

How the emergency committee submits `MsgEmergencySuspendAsset` (and any other
crisis-path committee transaction) without exposing it to the public mempool
before it executes.

**Why this exists.** A suspension observable in the public mempool can be
front-run: anyone watching gossip has seconds to exit positions against
pre-suspension state. Honest proposers prefer eligible committee and governance
transactions, subject to sender nonce dependencies. This runbook keeps an
emergency transaction private until a carrier proposes it. Use a committee
account without pending normal transactions to avoid a nonce dependency.

## Contents

- [Preconditions](#preconditions)
- [Carrier roster](#carrier-roster)
- [Variant A — dark carriers (standing configuration)](#variant-a--dark-carriers-standing-configuration)
- [Variant B — proposer timing (no configuration)](#variant-b--proposer-timing-no-configuration)
- [Congestion hardening](#congestion-hardening)
- [Submission procedure](#submission-procedure)
- [Committee upgrades](#committee-upgrades)
- [Rehearsal](#rehearsal)

## Preconditions

Use an active mandate and current term, funded signing account, healthy private carriers, and a rehearsed multisig
workflow. Operators must have the [upgrade binary policy](../operations/NODE_OPERATIONS.md#upgrade-binary-policy) in place before
an incident. The submission procedure below verifies CheckTx and inclusion separately and defines carrier fallback.

## Carrier roster

Maintain a standing roster of **carrier validators**:

- At least two independent operators; target a combined **10–20% of bonded
  stake** (expected inclusion wait is `1/p` blocks for combined stake fraction
  `p`: 10% ≈ 10 blocks, 20% ≈ 5, 33% ≈ 3).
- Each carrier exposes its **validator node's** RPC to the committee over a
  private path (WireGuard/VPN). Never over the public internet.
- Price submissions from Treasury's current gas-price query. Local
  `minimum-gas-prices` does not control Ark admission.
- Carriers and the signing/submission infrastructure see emergency transactions
  before the chain does. Trust and audit these operators; a leak is not
  automatically attributable to one carrier. Review the roster when it changes.

## Variant A — dark carriers (standing configuration)

Configure the **carrier and its dedicated sentries** in `config.toml`:

```toml
[mempool]
broadcast = false

[p2p]
pex = false
```

The carrier dials only those sentries through an authenticated private network.
Allowlist their P2P endpoints at the firewall, disable inbound public P2P, and
expose carrier RPC only to the committee's authenticated private submission
path. Keep the carrier out of public peer lists. Sentries should mark its node
ID in `private_peer_ids` as defence in depth; this setting alone is not access
control. Use at least two independent sentries in production.

Dedicated sentries receive public transactions but **do not relay them to the
carrier**. They still carry blocks, proposals and consensus votes. The carrier
accepts private RPC submissions and does not gossip them back out, so they
first become public in its proposed block. An ordinary public sentry with
`broadcast = true` would reopen inbound transaction flooding, even if the
carrier's own broadcast were disabled.

These dedicated sentries must not serve public transaction submission: they
would accept transactions without forwarding them. Keep ordinary public RPC
nodes and gossiping sentries available separately. Inclusion waits for a
carrier's proposer slot, and its trusted RPC clients can still fill its pool.

## Variant B — proposer timing (no configuration)

If no dark carrier is reachable: the proposer schedule is deterministic, so
watch for a slot where a friendly validator is the **next** proposer and
submit through normal RPC at that moment. Public exposure shrinks to under one
block time. This is the degraded path — use it when Variant A is unavailable,
not instead of maintaining it.

## Congestion hardening

Public nodes and validators run CometBFT's flood mempool (`config.toml [mempool] type = "flood"`,
the default) as a mirror of Ark's pool. Ark authenticates and classifies transactions received
through either RPC or peer gossip before assigning reserved storage; matched limits and deferred storage release leave transport headroom
for eligible committee actions and governance messages when normal storage is saturated. Block
selection caps each privileged lane at 5% of gas and available
transaction bytes. Transactions above the remaining allowance wait. An individual action larger than
the entire allowance competes in ordinary fee order, so a large legitimate action is not permanently
excluded. This caps preferential service; mixed batches also compete as normal.

The start command refuses `type = "app"` or `"nop"`, `recheck = false`, and a `size` below
`app.toml [mempool] max-txs`. For node homes written by an app-mode binary, before restart:

```sh
arkd config set config mempool.type flood
```

Normal public nodes must keep transaction broadcast on. The private carrier/sentry configuration
above still disables broadcast deliberately for confidentiality.

`app.toml [mempool] max-txs` defaults to 5000; zero now selects that bounded default. Its total
count and the configured encoded-byte budget are partitioned 5% committee, 5% governance and 90% normal (with rounding assigned to normal).
The byte settings live in `config.toml`, not `app.toml`:

```toml
[mempool]
max_tx_bytes = 1048576     # 1 MiB per signed transaction, including all messages
max_txs_bytes = 67108864   # 64 MiB total encoded pending transactions
```

Both must be positive. Ark and CometBFT use the same resolved transaction limit
and storage budget. Decoded transactions and indexes
consume additional RAM. Changing these settings requires a node restart.
Existing files retain their values: a previously ignored `max_txs_bytes = 1073741824`
now permits a 1 GiB backlog. To retain the previous effective budget before restart:

```sh
arkd config set config mempool.max_txs_bytes 67108864
```

Count, storage bytes and block gas/bytes have separate shares in
`app/mempool/limits.go`; preferential block service also starts at 5% for each privileged class.
Gossip uses CometBFT's flood reactor without lane allocation. There are no per-sender or repeat-vote
quotas, so eligible traffic can fill its own class's reservation. These initial shares require calibration against the intended voting
population and emergency bundle. A qualifying message never grants unlimited admission or bandwidth.

Use Ark's `ark_mempool_transactions{lane="normal|governance|committee"}` and
`ark_mempool_bytes` metrics for occupancy by lane; CometBFT's unconfirmed-transaction RPCs report the
mirrored list. A reserved-partition refusal answers the `lanes` capacity code; total count exhaustion uses the SDK's capacity error. CometBFT's default cache forgets rejected transactions:
resubmit the same signed transaction while it remains valid; an asynchronous broadcast response is not
proof of admission.

Ark still enforces Treasury's consensus base fee. Local `minimum-gas-prices` does not affect admission.
Query `arkd query treasury gas-price anoah` and price declared gas with headroom. Public admission and
gossip remove the need to wait for a private carrier's proposer slot, but do not guarantee inclusion
against arbitrary Sybil traffic, unavailable network links, or censoring proposers.

## Submission procedure

1. **Prepare and sign.** For a multisig committee, run the signing ceremony
   offline first (`arkd tx sign` per member, `arkd tx multi-sign` to
   assemble). Keep decision-to-broadcast latency short: the window opens when
   intent becomes observable anywhere, including off-chain.
2. **Preflight against the carrier** (private path, leaks nothing):

   ```bash
   arkd tx simulate <unsigned-tx.json> --from <committee-key> \
     --gas auto --node tcp://<carrier-vpn-ip>:26657
   ```

   A rejected emergency transaction during a crisis is the failure mode this
   step exists to prevent. Verify the account sequence, fees, and the current
   mandate term before the real send. For a multisig account, use `tx simulate`
   with the local committee public key: `--dry-run` uses a single-key
   placeholder and is not a reliable multisig preflight. If gas or fees need
   changing, rebuild the unsigned transaction and repeat the signatures.
3. **Broadcast to the carrier:**

   ```bash
   arkd tx broadcast <signed-tx.json> \
     --broadcast-mode sync --node tcp://<carrier-vpn-ip>:26657
   ```

   Require `code: 0` in the CheckTx response. Any other code: fix and resend —
   the transaction is not queued.
4. **Confirm inclusion.** Poll `arkd query tx <hash>` and watch for the
   `EventEmergencySuspended` event. Expected wait is the roster's `1/p`
   blocks.
5. **Escalate on delay.** If not included within twice the expected wait,
   submit to the next carrier (same transaction bytes — duplicates are
   harmless; the second inclusion attempt fails on sequence). If no carrier is
   healthy, fall back to Variant B, accepting the exposure.

## Committee upgrades

The security committee can schedule its upgrade through the same signing and submission process. Every operator
must already follow the [standing upgrade binary policy](../operations/NODE_OPERATIONS.md#upgrade-binary-policy): automatic binary
downloads stay disabled, and operators independently build or verify the executable. Scheduling a halt does not
approve the binary named by the plan.


## Rehearsal

Drill quarterly on testnet, unannounced to the operators on rotation:

- [ ] Signing ceremony completes within the target time budget.
- [ ] Dry-run and broadcast succeed against a dark carrier over the VPN path.
- [ ] While pending, the transaction is absent from public nodes'
      `/unconfirmed_txs`.
- [ ] Inclusion lands within `2/p` blocks, transaction is at the top of the
      block (after the injected oracle commit), and `EventEmergencySuspended`
      is emitted.
- [ ] Escalation to a second carrier works.
- [ ] A transaction below Treasury's consensus fee is rejected at CheckTx.
- [ ] Carrier peers are only dedicated sentries; public nodes cannot directly
      reach its P2P or RPC endpoint, and sentries cannot relay pending traffic.
- [ ] Public mempool saturation leaves private carrier admission available.
      Exercise qualified spam separately; eligibility alone does not bound it.
- [ ] Roster stake fractions re-measured; latency table in this document
      still honest.
