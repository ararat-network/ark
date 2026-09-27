# Disbursement

`x/disbursement` holds fully funded member grants, contributor ownership grants, and compensation. Every award retains its
original beneficiary, amount, denomination, schedule, and public reference. Bank owns the coins in the `disbursement` module
account; the module reserves outstanding principal and records each operation in a permanent journal. It never mints,
quotes or settles a conversion, delegates escrow, or pays from a block hook. Anyone can submit a release when payment is
available.

The [disbursement plan](../../docs/governance/DISBURSEMENT_PLAN.md) sets the launch allocation and review process;
[governance operations](../../docs/governance/GOVERNANCE_OPERATIONS.md#7-grant-tranches) describes proposals and recovery.
This is a clean prelaunch replacement of the grant contract. There is no contract-state migration.

## Funding and authority

Both distribution pools are funded at genesis and sit in `disbursement` custody from block 1: the member pool and the
contributor pool, with nothing open. A pool records its uncommitted NOAH (`unallocated`) and the part governance has
opened to new grants (`open`). Each step of the plan is a governance `MsgOpenTranche` naming a pool and an amount;
opening adds to the tranche and moves no coins, and only genesis grows a pool. Member registration draws on the open
member tranche, so the committee registers members without putting the roll through a proposal. Ownership awards and
NOAH compensation draw on the open contributor tranche: a contributor award is one proposal carrying `MsgCreateGrant`,
preceded by `MsgOpenTranche` when the step's tranche lacks room.

The grants committee holds the module's one [committee mandate](../../pkg/mandate/README.md): it registers and
suspends members and awards bounded compensation. Governance appoints an exact account for a half-open height window
with `MsgSetGrantsMandate`; every replacement advances the term, and each committee message carries
`expected_term`, so transactions prepared under a replaced key never land. Appointment records the shape the chain
could prove about the account, which is how the plan's multisig requirement becomes a fact on chain. Committee messages
ride the committee lane. A lapsed window stops the committee without moving the term or any suspension.

The module account has no mint/burn permission and accepts deposits, which land outside both pools. Each grant must be
backed at creation: NOAH by its pool's open tranche, any other denomination by unallocated custody in that denomination.
`MsgReturnUnallocated` is governance-only and returns through `FundCommunityPool`, preserving Distribution's ledger. Its
`pool` names the source: unspecified returns custody outside reservations, pools, and orders, the contributor pool returns
its NOAH with the unopened part first, and the member pool has no exit. Cancellation returns unearned NOAH to its pool's
open tranche, the step it was charged to, and leaves other denominations unallocated in custody for the next award.

| Action | Authority |
| --- | --- |
| Set operational params, appoint or replace the committee, open tranches, create contributor awards, authorise and cancel conversion orders, cancel grants, return unallocated or contributor-pool funds, void a replaced term's suspensions | Governance |
| Register and suspend members, award compensation within the term's allowance | Committee, under its exact term and active window |
| Reinstate members | Committee or governance |
| Cancel every unfinished award of one committee term | Governance |
| Release one or more grants | Any sender; always pays the stored payee |
| Execute an authorised conversion | Any sender except governance |
| Change member payee | Original registered member key |
| Change contributor payee | Beneficiary's current controller |
| Rotate contributor controller | Current controller or governance |

The committee cannot change a payee or cancel a grant. Changing a contributor controller cannot change control of a
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
  in the award's denomination; a conversion order can supply stablecoin custody beforehand, but an award never converts
  and carries no exchange-rate guarantee.

These payments are **untaxed protocol disbursements**, including stablecoins. A caller still pays normal transaction
gas. Paid coins are ordinary spendable coins; unpaid escrow cannot stake or vote. Compensation payments do not count
as ownership awards even if the recipient subsequently stakes them.

Suspension blocks member payments while their original schedule continues. Reinstatement permits catch-up. Each
suspension records the committee term it was made under. Governance voids every suspension of a replaced term with one
write; the live term is never voided, so to void a sitting committee's suspensions governance re-appoints it, which
advances the term, and voids the previous term in the same proposal. A suspension under the new term stands even in the
same block. Replacing the committee alone does not void suspensions, and neither does expiry.

Cancellation freezes entitlement at the cancellation time, or at the effective suspension time for a suspended member.
It retains accrued unpaid principal, frees only unearned principal, and **never sends to the payee**. A blocked recipient
therefore cannot veto cancellation. Later releases pay retained principal to the current payee; ownership limits still
apply. A cancelled grant has no future accrual, cannot be reinstated, and cannot be registered again as a member.

## Committee compensation

The appointment sets the committee's compensation power: `compensation_allowance`, a per-denomination cap on the
term's awards, and `min_first_period`, the shortest first period an award may have. Every allowance denomination must be
on the compensation allowlist and, for a stablecoin, active, or the appointment is refused; a nonzero allowance needs a
positive period, and a disabled mandate carries neither. `MsgCommitteeCompensate` creates compensation only, funded like
a governance award: NOAH from the open contributor tranche, which the allowance caps but never opens, and stablecoins
from custody. It refuses a founding seat holder, whose pay stays a governance vote, and a first period below the
minimum, so a stolen key's awards pay nothing before governance can act.

A grant records its awarding term in `mandate_term`. A term's usage is the sum of its awards, cancelled ones included, so
a cancel never restores allowance and a replacement starts at zero. A term makes at most 100 awards. Governance's
`MsgCancelTermGrants` cancels every unfinished award of one term in one write, including any a key made while the vote
that replaced it was open, so recovery is one proposal: appoint a new committee, void the old term's suspensions, and
cancel the old term's awards.

## Stablecoin conversion

Contributor NOAH can fund stablecoin compensation. Governance's `MsgAuthoriseConversion` moves open contributor NOAH
into the conversion order for one denomination; each authorisation adds NOAH and restates the order's maximum spread.
`MsgCancelConversion` returns what remains to the open contributor tranche. Any sender may execute an order with
`MsgConvert`, up to its remaining NOAH: the module trades its own custody through Market's ordinary swap, so Market
quotes, escrows, mints, and settles it at EndBlock like any trader's conversion, and the stablecoin lands in custody for
the next compensation award.

An execution fails, rolling its swap back, when the realised spread, truncation dust included, exceeds the order's cap;
a cap must therefore sit above Market's spread floor. The cap also paces a large order, since a fill that would slip
past it waits for the pool to recover. The denomination must be allowlisted and active; delisting it stalls the order
until governance cancels it. A governance sender is refused, because proposals execute after Market's EndBlocker has
settled the block.

## Ownership and founding seats

A beneficiary's cumulative ownership payments follow their original identity across grants, controllers, and payees.
Genesis seat assembly records each founding validator's 5M NOAH seat on the beneficiary record at its operator account.
No transaction sets a seat. Founding stake is the sum of actual assembled seats; it is never supplied by
an award or inferred from the current validator set. Founding seats themselves remain native vesting accounts.

The genesis ownership policy is one fifth and a 60M NOAH ceiling. There is no operational parameter update for
it. In base units, a release computes:

```text
own = founding seat principal (if any) + beneficiary's cumulative ownership payments
limit = min(floor(max(bonded - own, 0) × numerator / denominator), ceiling)
individual allowance = max(limit - own, 0)
founder room = max(floor(bonded × 3 / 10) - all founding seat principal - all founder ownership payments, 0)
founder allowance = floor(founder room × this grant's remaining / all founder grants' remaining)
payment = min(accrued unpaid, individual allowance, founder allowance if a founder)
```

`bonded` is the current staking bonded-pool balance. Ownership is conservatively counted as bonded whether the payee
stakes it or not. The seat counts toward both the concentration limit and the 60M ceiling. Founder room holds the seat
holders together below the 33.3% that blocks a proposal: the tally passes only when yes exceeds 66.7%, so a bloc at a
third would still block. Cancelled grants' retained accrued ownership remains in the founder denominator. Payments
and cancellation maintain founder aggregates, so a release never scans historical grants. Each release uses current
state; repeated or differently ordered releases may produce different pro-rata shares as remaining balances change,
but cannot spend more than the live room. There is no clawback when bonded stake later falls.

## State, journal, and bounds

For every grant, `amount = paid + remaining + cancelled_amount`. Per-denomination reservations equal all remaining
principal, including cancelled accrued debt. For NOAH, `Bank balance = reserved + member pool + contributor pool +
conversion orders + unallocated`; other denominations have no pools or orders. `unallocated` must be non-negative.
Paid counters distinguish members, ownership, and compensation. Bank sends, counters, indexes, and journal writes
share the SDK transaction cache; one failure rolls the whole message batch back.

Grant records and member indexes are permanent; the grants mandate and voided terms export verbatim, an expired
appointment included. The append-only journal records creation (including the original
terms and reference), each payment and destination, cancellation and retained debt, suspensions, reinstatements,
payee changes, controller changes, parameter changes, committee appointments, term voids, term cancellations,
unallocated returns, tranche openings, and conversion authorisations, cancellations, and executions. Every journal entry also
emits a typed `EventOperation`. Incoming funding is visible through Bank and Distribution events. Grant ID zero
identifies administrative journal entries; list them with the unfiltered journal query. Records are public; references
should identify approved work or an agreement without publishing private personnel information.

| Input or operation | Bound |
| --- | --- |
| Registration, release, suspension, reinstatement, cancellation batch | 100 entries; no duplicates |
| One grant, member amount, outstanding reservation per denomination | 128-bit positive principal; zero allowed for balances |
| Pool, tranche, conversion order | 128-bit; a tranche never exceeds its pool; an order's cap strictly between zero and one |
| Schedule | 1–1,200 periods, each at most 100 years, positive weights with a checked uint64 sum |
| Member issuance | 1–10,000 members per rolling window; window 1 second–100 years |
| Compensation allowlist | Up to 64 sorted, unique native denominations |
| Voided term | Below the current committee term; each term at most once |
| Committee awards | At most 100 per term, within the term's allowance; first period at least the appointment's minimum |
| Contributor public reference | Nonempty, at most 2,048 bytes |
| Query page | At most 100 records, key pagination only; no offsets or count-total scans |

The issuance log retains registrations in the configured window and prunes expired entries on registration. Changing
the window does not reconstruct registrations already pruned under an earlier policy. Lowering the member limit can
stop registration until enough entries expire. The library default is 1,000 per week; the reviewed launch and testnet
artifacts use the disbursement plan's **250 per week**, with the grants mandate disabled until appointed.

Genesis validation checks identities, schedules, conservation, aggregate totals, contiguous IDs, journal accounting, the
grants mandate, voided terms, pools, conversion orders, and each term's committee awards.
Import also checks Bank backing, pools and orders included, and timestamps against genesis time, then rebuilds secondary
indexes and founder aggregates. Continuation exports retain absolute times, IDs, member identities, and the journal.

## APIs and source map

The authoritative API is [proto/ark/disbursement/v1](../../proto/ark/disbursement/v1). AutoCLI exposes `arkd tx disbursement` and
`arkd query disbursement`; gRPC and REST expose the same query surface. `params` includes the immutable ownership policy and
founding stake; `grants-mandate` shows the appointment, its term, whether it is active, and the live term's awards; `releasable` separates accrued unpaid principal from currently payable funds and shows live cap inputs.
`balance` shows Bank backing, reservation, paid totals, both pools, NOAH held in conversion orders, and unallocated
funds; `conversion-orders` lists open orders. `grants --beneficiary`, `grants --mandate-term`, and
`journal --grant-id` use indexes rather than scanning unrelated history. `member`, `beneficiary`, `issuance`, and
`totals` complete the public accounting surface. Queries carry `module_query_safe`; they are not automatically added
to the app's separately reviewed Wasm query accept list.

| Source | Responsibility |
| --- | --- |
| [types/rules.go](types/rules.go), [types/params.go](types/params.go), [types/grants_mandate.go](types/grants_mandate.go) | Schedule arithmetic, bounds, operational and ownership policy, grants mandate and committee surface |
| [types/genesis.go](types/genesis.go) | Record and import validation |
| [keeper/keeper.go](keeper/keeper.go) | Collections, custody checks, pools, journal writer |
| [keeper/mandate.go](keeper/mandate.go) | Committee appointment, observation, authorisation, and term usage |
| [keeper/payments.go](keeper/payments.go) | Creation, entitlement, caps, payment, cancellation |
| [keeper/msg_server.go](keeper/msg_server.go) | Authorities, rolling issuance, member recovery, destinations, tranches, conversion orders |
| [keeper/grpc_query.go](keeper/grpc_query.go), [keeper/genesis.go](keeper/genesis.go) | Indexed reads and continuation export/import |
| [module](module) | Dependency injection, services, AutoCLI, simulation |
| [app/disbursement_test.go](../../app/disbursement_test.go) | Real-app custody, authority, accounting, failure, and continuation tests |
| [tests/integration/disbursement_conversion_test.go](../../tests/integration/disbursement_conversion_test.go) | Conversion through real Market settlement |

```sh
go test ./x/disbursement/... ./app/genesis ./app -run 'TestNativeDisbursement|TestAddValidatorSeats|TestParamsValidation|TestScheduleSplit|TestAccrualBoundaries|TestRandomisedScheduleConservation|TestGenesisValidation'
go test ./app ./app/genesis ./x/disbursement/...
go test ./tests/integration -run TestDisbursementConvertsThroughMarket
```

The Docker delegator E2E suite additionally exercises opening a member tranche, registration into fresh and existing
accounts, delegation of paid coins, and committee replacement through the public CLI.
