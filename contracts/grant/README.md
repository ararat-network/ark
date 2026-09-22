# Grant contract

`ark-grant` is the on-chain half of the [distribution plan](../../docs/governance/DISTRIBUTION_PLAN.md): it
issues member grants at registration and holds contributor grants in escrow, releasing them by the plan's cap
and bloc rule against bonded stake. Governance funds it one tranche at a time by `MsgCommunityPoolSpend` and
instructs it through `MsgSudoContract`; it has no admin, because wasmd lets governance sudo and migrate any
contract, and it stores nothing the plan does not name.

## Code map

| Entry point | Responsibility |
| --- | --- |
| [src/contract.rs](src/contract.rs) | Entry points, registration, releases, replies, queries. |
| [src/rules.rs](src/rules.rs) | The plan's arithmetic: schedule split, cap, bloc allowance, pro rata, unit rounding. Pure, tested alone. |
| [src/proto.rs](src/proto.rs) | The SDK messages it dispatches and the pool query it reads. |
| [src/msg.rs](src/msg.rs), [src/state.rs](src/state.rs) | Interface and storage. |
| [tests/flows.rs](tests/flows.rs) | Flows on cw-multi-test with a chain stub. |

## Surface

**Sudo, governance only.** `set_registrar`, `set_issuance_limit`, `set_member_grant`, `add_grant`,
`set_controller`, `cancel_grant`, `return_unallocated`.

**Execute.** `register_members` by the registrar, at most 100 addresses; `set_release_address` and
`set_controller` by a grant's controller; `release` by anyone.

**Query.** `config`, `grant`, `person`, `releasable`, `totals`, `member`, `issuance`.

## Behaviour

- **Members.** Each registered address gets the configured grant as a periodic vesting account on the configured
  schedule, starting at the block, plus a basic fee allowance from the contract so the account can delegate.
  Each creation is a submessage with a reply on error: an address that already holds an account fails only its
  own creation, is recorded as rejected, and the rest of the batch lands. A rejected or issued address is never
  registered again; the member supplies a fresh one. The registrar may register at most `max_members` in any
  `window_seconds`, attempted registrations counted whatever their outcome, so a stolen key costs a window's
  issuance rather than a tranche before governance replaces it.
- **Contributors.** `add_grant` escrows the whole amount from the unallocated balance and pays what the rules
  allow at once to the grantee, who becomes the grant's controller. Later tranches go to an address the
  controller registers, and anyone may trigger them. Each tranche is a fresh vesting account on the grant's own
  schedule.
- **Rules.** A person's `own` is their seat if they hold one and what this contract has released to them, all
  assumed bonded: subtracting more than is bonded only lowers the cap. Their total may reach a fifth of
  `bonded − own`, at most the ceiling. Seat holders
  together may reach a third of bonded stake less the founding stake, shared pro rata by remaining escrow. A
  grant that fits under its allowance pays whole; a larger one releases the allowance floored to whole units. A
  grant too small to give every period of its schedule a coin is refused where it is set, since the chain rejects
  a period without one.
- **Accounting.** Escrowed is the sum of every grant's remaining amount; unallocated is the bank balance less
  that. Registration and `add_grant` both draw from unallocated. `cancel_grant` sends a grant's remaining
  amount back to the community pool; `return_unallocated` sends idle balance back.
- **Trust.** Governance holds every unbounded path. The registrar can only name who receives the next fixed
  member grant, bounded by the unallocated balance and the issuance window. A controller can only point their own
  next tranche.

## Development

```sh
make contracts
go test ./app -run TestGrantContract
```

The application tests in [app/grant_contract_test.go](../../app/grant_contract_test.go) embed the built wasm and
run it on the real app: real vesting accounts, a dusted address skipped inside a batch, the staking pool query,
sudo from the gov authority, and a cancel returning to the pool. Change the source and run
`make contracts-optimize` before them, since they run the checked-in artefact, the optimizer's build.
