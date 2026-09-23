# Grant contract

`ark-grant` is the on-chain half of the [distribution plan](../../docs/governance/DISTRIBUTION_PLAN.md): it
holds every grant in escrow and pays each out by its rule, member grants by the clock from registration,
ownership grants by the plan's cap and bloc rule against bonded stake, and pay streams by the clock. Governance
funds it one tranche at a time by `MsgCommunityPoolSpend` and instructs it through `MsgSudoContract`; it has no
admin, because wasmd lets governance sudo and migrate any contract, and it stores nothing the plan does not name.

## Code map

| Entry point | Responsibility |
| --- | --- |
| [src/contract.rs](src/contract.rs) | Entry points, registration, releases, replies, queries. |
| [src/rules.rs](src/rules.rs) | The plan's arithmetic: schedule split, cap, bloc allowance, pro rata, unit rounding, the stream clock. Pure, tested alone. |
| [src/proto.rs](src/proto.rs) | The SDK messages it dispatches and the pool query it reads. |
| [src/msg.rs](src/msg.rs), [src/state.rs](src/state.rs) | Interface and storage. |
| [tests/flows.rs](tests/flows.rs) | Flows on cw-multi-test with a chain stub. |

## Surface

**Sudo, governance only.** `set_registrar`, `set_issuance_limit`, `set_member_grant`, `set_fee_allowance`,
`add_grant`, `add_stream`, `set_controller`, `cancel_grant`, `cancel_members`, `reinstate_members`,
`void_suspensions`, `return_unallocated`.

**Execute.** `register_members`, `suspend_members`, and `reinstate_members` by the registrar, at most 100
addresses each; `release_members` by anyone, the same bound; `set_release_address` and `set_controller` by a
grant's controller; `release` by anyone.

**Query.** `config`, `grant`, `person`, `releasable`, `totals`, `member`, `issuance`.

## Behaviour

- **Members.** Registration opens the configured grant for each address on the configured schedule, which the
  member keeps whatever governance sets later. The first period is sent at once, whatever its length, so the
  member's own account can pay the gas for every claim after it; the rest stays in escrow and goes to the address
  by bank send as whole periods elapse, by anyone and cumulatively, so waiting loses nothing. The send lands in a
  fresh account or one that already holds coins alike; a member is never a vesting account. An issued address is
  never registered again. The registrar may register at most `max_members` in any `window_seconds`, counted from
  each registration's block, so a stolen key costs a window's first periods rather than a tranche before
  governance replaces it.
  The registrar may suspend a member, which stops their pay and moves nothing; reinstated, by the registrar or by
  sudo, the next release pays everything since. Governance cancels a member by sudo: what had elapsed by the
  suspension, or by the vote if there was none, is paid, and the rest leaves escrow into the unallocated balance,
  the tranche's again. A suspension records who made it, and `void_suspensions` lifts everything a registrar
  suspended up to that height in one write, read wherever a suspension is, so the proposal that replaces a stolen
  key undoes what it did and a key set again later suspends afresh.
- **Ownership grants.** `add_grant` escrows the whole amount from the free balance and pays what the rules allow at
  once, to the address the proposal names, else to the grantee while the contract has not paid them: an address holding
  an account cannot take a vesting account, so a repeat grant waits for an address. The grantee becomes the grant's
  controller. Later tranches go to an address the controller registers, and anyone may trigger them. Each tranche is a
  fresh vesting account on the grant's own schedule, with a fee allowance from the grant's gas reserve, since nothing in
  a tranche is spendable until its cliff. The proposal spends the grant plus one allowance per whole unit of it, the
  most tranches the cap can split it into; the contract holds that as the grant's gas reserve, promises one allowance
  to each tranche from it, and returns what is unused with a cancel or after the last tranche.
- **Streams.** `add_stream` escrows a pay stream the contract pays down itself: nothing at once, then each period of
  its schedule to the payee by bank send once it has elapsed since the block it was added in, whole periods only, so
  the pay is spendable on arrival and the unpaid rest stays in escrow. Anyone may trigger a payment; the controller may
  point the payee anywhere, an existing account included. A stream checks neither the cap nor the bloc rule and counts
  in neither `own` nor the seat pool: it is pay, not ownership, and never stakes. A cancel holds what has elapsed for
  the payee, who releases it as before, and returns the rest, so no payee can fail it.
- **Rules.** A person's `own` is their seat if they hold one and what this contract has released to them, all assumed
  bonded: subtracting more than is bonded only lowers the cap. Their total may reach a fifth of `bonded − own`; the
  ceiling bounds cumulative ownership grants alone, so the total ceiling is `ceiling + seat`. At the plan's figures, a
  seat holder may receive 60M in grants alongside their 5M seat, with all 65M counted against the percentage limit. Seat
  holders together may reach a third of bonded stake less the founding stake, shared pro rata by remaining escrow. A
  grant that fits under its allowance pays whole; a larger one releases the allowance floored to whole units. A grant
  too small to give every period of its schedule a coin is refused where it is set, an ownership grant by its smallest
  tranche, since the chain rejects a period without one.
- **Accounting.** Escrowed is the sum of every grant's remaining amount, fees reserved the sum of their gas
  reserves, and fees promised every allowance granted. Free is the bank balance less escrow and reserves;
  unallocated is free less promises. `add_grant` draws grant plus reserve from free, since a grant arrives with
  its own spend and must never wait on the member tranche. Registration draws a grant a member from unallocated,
  and `return_unallocated` is bounded by it. The contract cannot see an allowance being drawn, so a promise counts
  for good and unallocated reads low by the gas tranche accounts have spent, and free reads high by the gas promised
  and not yet drawn, which is why a proposal spends a grant and its gas whole. A changed allowance does not resize
  gas already reserved: a tranche takes the smaller of the allowance and its reserve spread over the tranches the
  grant can still take, so a raise neither reaches the grant nor starves its last tranches.
  `add_stream` draws its amount from free the same way, and stream pay counts in contributors paid; member pay,
  the first periods included, counts in members paid.
  `cancel_grant` sends a grant's remaining amount and unused gas back to the community pool, a stream's less what
  has elapsed, which stays escrowed for the payee; `cancel_members` leaves the rest in the balance, where it is
  unallocated; `return_unallocated` sends idle balance back.
- **Trust.** Governance holds every unbounded path. The registrar can only name who receives the next fixed
  member grant, bounded by the unallocated balance and the issuance window, and stop a member's pay, which it or
  a vote can resume, the next release then paying the months since; it moves no coin. A controller can only point
  their own next tranche or stream pay.

## Development

```sh
make contracts
go test ./app -run TestGrantContract
```

The application tests in [app/grant_contract_test.go](../../app/grant_contract_test.go) embed the built wasm and
run it on the real app: real vesting accounts for tranches, member pay by send into fresh and dusted accounts, the
staking pool query, sudo from the gov authority, a suspension, and cancels returning to the pool or to the
tranche. Change the source and run `make contracts-optimize` before them, since they run the checked-in
artefact, the optimizer's build.
