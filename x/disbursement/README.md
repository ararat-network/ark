# Disbursement

`x/disbursement` holds fully funded member grants, contributor ownership grants, and compensation. Every award retains its
original beneficiary, amount, denomination, schedule, and public reference. Bank owns the coins in the `disbursement` module
account; the module reserves outstanding principal and records each operation in a permanent journal. It never mints,
converts currencies, delegates escrow, or pays from a block hook. Anyone can submit a release when payment is available.

The [disbursement plan](../../docs/governance/DISBURSEMENT_PLAN.md) sets the launch allocation and review process;
[governance operations](../../docs/governance/GOVERNANCE_OPERATIONS.md#7-grant-tranches) describes proposals and recovery.
This is a clean prelaunch replacement of the grant contract. There is no contract-state migration.

## Funding and authority

Governance funds custody with Distribution's `MsgCommunityPoolSpend`. For a contributor award, include that spend and
`MsgCreateGrant` in the same proposal: both execute or neither does. A member tranche funds unallocated custody; the
appointed registrar subsequently registers members without putting the member roll through a proposal.

The module account has no mint/burn permission and accepts deposits. Each grant must be backed in its own denomination
at creation. `MsgReturnUnallocated` is governance-only and returns at most Bank balance minus reservations through
`FundCommunityPool`, preserving Distribution's ledger. Contributor cancellation uses the same return path; unearned
member principal stays unallocated in disbursement custody for another member.

| Action | Authority |
| --- | --- |
| Set operational params, create contributor awards, cancel, return unallocated funds, void a registrar's suspensions | Governance |
| Register and suspend members | Current registrar |
| Reinstate members | Current registrar or governance |
| Release one or more grants | Any sender; always pays the stored payee |
| Change member payee | Original registered member key |
| Change contributor payee | Beneficiary's current controller |
| Rotate contributor controller | Current controller or governance |

The registrar cannot change a payee or cancel a grant. Changing a contributor controller cannot change control of a
member grant at the same address. Registration is permanent: cancellation, completion, controller rotation, or a new
payee never makes the original member address eligible for another member grant. The off-chain admission process must
still detect people presenting different addresses. Governance can recover contributor control; there is no substitute
signer for a lost original member key.

## Schedules and cancellation

Periods contain positive lengths in seconds and positive integer weights. Each share is
`floor(amount × period.parts / total.parts)`; the final period receives the remainder. Every period must receive a
positive base-unit amount. The block timestamp at creation starts the only clock. Period boundaries are inclusive.

- **Members** receive their first share immediately in the registration transaction, regardless of its length. The
  remaining shares accrue at cumulative period boundaries. Terms are copied from params at registration and never
  rewritten by later parameter changes. Default terms are 10,000 NOAH, a tenth immediately, then twelve monthly periods.
- **Ownership** accrues from approval and pays nothing immediately. Each release pays the smaller of accrued unpaid
  principal and the live ownership allowance. A cap can delay payment indefinitely after the schedule ends; it never
  resets the clock or discards a partially paid period.
- **Compensation** uses the same schedule without ownership limits. Governance must approve its denomination; NOAH is
  admitted initially. A new stablecoin award requires an active native Asset registry entry. Removing a denomination
  from the allowlist or changing its lifecycle status does not erase existing obligations. Funding and payment remain
  in the award's denomination, with no oracle conversion or exchange-rate guarantee.

These payments are **untaxed protocol disbursements**, including stablecoins. A caller still pays normal transaction
gas. Paid coins are ordinary spendable coins; unpaid escrow cannot stake or vote. Compensation payments do not count
as ownership awards even if the recipient subsequently stakes them.

Suspension blocks member payments while their original schedule continues. Reinstatement permits catch-up. Each
suspension records its registrar and that registrar's epoch. Governance voids all that registrar's earlier suspensions
with one epoch increment; a new suspension in the same block remains effective. Rotating the registrar alone does not
void suspensions: include both messages in an incident proposal.

Cancellation freezes entitlement at the cancellation time, or at the effective suspension time for a suspended member.
It retains accrued unpaid principal, frees only unearned principal, and **never sends to the payee**. A blocked recipient
therefore cannot veto cancellation. Later releases pay retained principal to the current payee; ownership limits still
apply. A cancelled grant has no future accrual, cannot be reinstated, and cannot be registered again as a member.

## Ownership and founding seats

A beneficiary's cumulative ownership payments follow their original identity across grants, controllers, and payees.
Genesis seat assembly records each founding validator's operator address, matching account address, and 5M NOAH seat.
These records have no transaction setter. Founding stake is the sum of actual assembled seats; it is never supplied by
an award or inferred from the current validator set. Founding seats themselves remain native vesting accounts.

The genesis ownership policy is one fifth and a 60M NOAH award ceiling. There is no operational parameter update for
it. In base units, a release computes:

```text
own = founding seat principal (if any) + beneficiary's cumulative ownership payments
limit = min(floor(max(bonded - own, 0) × numerator / denominator), ceiling + seat)
individual allowance = max(limit - own, 0)
founder room = max(floor(bonded / 3) - all founding seat principal - all founder ownership payments, 0)
founder allowance = floor(founder room × this grant's remaining / all founder grants' remaining)
payment = min(accrued unpaid, individual allowance, founder allowance if a founder)
```

`bonded` is the current staking bonded-pool balance. Ownership is conservatively counted as bonded whether the payee
stakes it or not. The seat counts toward concentration but does not consume the 60M award ceiling: a founder's total
ceiling is 65M. Cancelled grants' retained accrued ownership remains in the founder denominator. Payments and
cancellation maintain founder aggregates, so a release never scans historical grants. Each release uses current
state; repeated or differently ordered releases may produce different pro-rata shares as remaining balances change,
but cannot spend more than the live room. There is no clawback when bonded stake later falls.

## State, journal, and bounds

For every grant, `amount = paid + remaining + cancelled_amount`. Per-denomination reservations equal all remaining
principal, including cancelled accrued debt. `Bank balance >= reserved` is required; the difference is unallocated.
Paid counters distinguish members, ownership, and compensation. Bank sends, counters, indexes, and journal writes
share the SDK transaction cache; one failure rolls the whole message batch back.

Grant records and member indexes are permanent. The append-only journal records creation (including the original
terms and reference), each payment and destination, cancellation and retained debt, suspensions, reinstatements,
payee changes, controller changes, parameter changes, epoch voids, and unallocated returns. Every journal entry also
emits a typed `EventOperation`. Incoming funding is visible through Bank and Distribution events. Grant ID zero
identifies administrative journal entries; list them with the unfiltered journal query. Records are public; references
should identify approved work or an agreement without publishing private personnel information.

| Input or operation | Bound |
| --- | --- |
| Registration, release, suspension, reinstatement, cancellation batch | 100 entries; no duplicates |
| One grant, member amount, outstanding reservation per denomination | 128-bit positive principal; zero allowed for balances |
| Schedule | 1–1,200 periods, each at most 100 years, positive weights with a checked uint64 sum |
| Member issuance | 1–10,000 members per rolling window; window 1 second–100 years |
| Compensation allowlist | Up to 64 sorted, unique native denominations |
| Contributor public reference | Nonempty, at most 2,048 bytes |
| Query page | At most 100 records, key pagination only; no offsets or count-total scans |

The issuance log retains registrations in the configured window and prunes expired entries on registration. Changing
the window does not reconstruct registrations already pruned under an earlier policy. Lowering the member limit can
stop registration until enough entries expire. The library default is 1,000 per week; the reviewed launch and testnet
artifacts use the disbursement plan's **250 per week**, with the registrar disabled until appointed.

Genesis validation checks identities, schedules, conservation, aggregate totals, contiguous IDs and journal accounting.
Import also checks Bank backing and timestamps against genesis time, then rebuilds secondary indexes and founder
aggregates. Continuation exports retain absolute times, IDs, member identities, and the journal.

## APIs and source map

The authoritative API is [proto/ark/disbursement/v1](../../proto/ark/disbursement/v1). AutoCLI exposes `arkd tx disbursement` and
`arkd query disbursement`; gRPC and REST expose the same query surface. `params` includes the immutable ownership policy and
founding stake; `releasable` separates accrued unpaid principal from currently payable funds and shows live cap inputs.
`balance` shows Bank backing, reservation, paid totals, and unallocated funds. `grants --beneficiary` and
`journal --grant-id` use indexes rather than scanning unrelated history. `member`, `beneficiary`, `issuance`, and
`totals` complete the public accounting surface. Queries carry `module_query_safe`; they are not automatically added
to the app's separately reviewed Wasm query accept list.

| Source | Responsibility |
| --- | --- |
| [types/rules.go](types/rules.go), [types/params.go](types/params.go) | Schedule arithmetic, bounds, operational and ownership policy |
| [types/genesis.go](types/genesis.go) | Record and import validation |
| [keeper/keeper.go](keeper/keeper.go) | Collections, custody checks, journal writer |
| [keeper/payments.go](keeper/payments.go) | Creation, entitlement, caps, payment, cancellation |
| [keeper/msg_server.go](keeper/msg_server.go) | Authorities, rolling issuance, member recovery, destinations |
| [keeper/grpc_query.go](keeper/grpc_query.go), [keeper/genesis.go](keeper/genesis.go) | Indexed reads and continuation export/import |
| [module](module) | Dependency injection, services, AutoCLI, simulation |
| [app/disbursement_test.go](../../app/disbursement_test.go) | Real-app custody, authority, accounting, failure, and continuation tests |

```sh
go test ./x/disbursement/... ./app/genesis ./app -run 'TestNativeDisbursement|TestAddValidatorSeats|TestParamsValidation|TestScheduleSplit|TestAccrualBoundaries|TestRandomisedScheduleConservation|TestGenesisValidation'
go test ./app ./app/genesis ./x/disbursement/...
```

The Docker delegator E2E suite additionally exercises governance funding, registration into fresh and existing
accounts, delegation of paid coins, and registrar replacement through the public CLI.
