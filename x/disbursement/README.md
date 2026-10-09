# Disbursement

`x/disbursement` holds fully funded member grants, contributor ownership grants, and compensation. Every award retains its
original beneficiary, amount, denomination, schedule, and public reference. Bank owns the coins in two module accounts,
`disbursement_members` and `disbursement_contributors`; the module reserves outstanding principal in each and records
each operation in a permanent journal. It never mints or settles a conversion or delegates escrow; Market quotes and
settles the conversion a stablecoin award asks for. The block pays each grant as its schedule falls due; no
transaction releases a payment.

The [disbursement plan](../../docs/governance/DISBURSEMENT_PLAN.md) sets the launch allocation and review process;
[governance operations](../../docs/governance/GOVERNANCE_OPERATIONS.md#5-grant-tranches) describes proposals and recovery.
This is a clean prelaunch replacement of the grant contract. There is no contract-state migration.

## Funding and authority

Both disbursement pools are funded at genesis, each in its own module account from block 1, with nothing open. The
member account, `disbursement_members`, holds the member pool and every member grant's escrow, NOAH only. The
contributor account, `disbursement_contributors`, holds the contributor pool, ownership and compensation escrow in
every denomination, and deposits. An account's pool is its NOAH outside grants; the module also records the
part of the member pool governance has opened to registration, `member_tranche`. Each member step of the plan is a governance `MsgOpenTranche`
naming an amount; opening adds to the tranche and moves no coins. A NOAH deposit into either account joins its
pool; into the member account it stays unopened until a tranche opens it. Member registration is the registrar's, a key the grants committee
appoints under its mandate, and draws on the open member tranche, so the roll never passes through a proposal; that tranche
is the only bound on registration, so governance sizes what a registrar may commit by how much it opens. Each registration batch carries a reference to
the platform's admission record, stored on every member grant and in the journal; it holds no personal data.
Ownership awards and NOAH compensation draw on the contributor pool, which has no tranche (D117): a contributor award
is one proposal carrying `MsgGrantOwnership` or `MsgCompensate`, or one committee message within the term's allowance.

The grants committee holds the module's [committee mandate](../../pkg/mandate/README.md): it appoints the registrar,
suspends members, and awards bounded compensation. Governance appoints an exact account for a half-open height window
with `MsgSetGrantsMandate`; every replacement advances the term, and each committee message carries
`expected_term`, so transactions prepared under a replaced key never land. Appointment records the shape the chain
could prove about the account, which is how the plan's multisig requirement becomes a fact on chain. Committee messages
ride the committee lane. A lapsed window stops the committee without moving the term or any suspension.

The registrar holds the module's second appointment, a bare envelope under the committee's, set by the committee
under its term with `MsgCommitteeRegistrar` for a window inside its own; the committee may name itself. Every
appointment advances the registrar term, which `MsgRegisterMembers` carries; the open tranche is the registrar's only
bound. The registrar is the committee's delegate: a committee appointment, replacement or disabling, clears it, so no
key registers under a committee that cannot act, and governance reaches it only through the committee. Its batches
ride the normal lane: registration is routine traffic, not an incident path, and only the appointment is a committee
message (D120).

Neither account may mint or burn. The member account accepts NOAH deposits, which join its pool unopened; Bank refuses
anything else into it on every path, and nothing but member payments leaves it, so it pays members or nobody. The contributor account
accepts NOAH and registered-asset deposits, and Bank refuses anything else at the send: stablecoins wait uncommitted
for the next award, NOAH joins the pool. Each grant must be backed at creation: a member's by the open member tranche, a contributor's by
uncommitted contributor custody in its denomination and, for a stablecoin shortfall, a conversion of NOAH from the
contributor pool inside the message that creates it, offering at most the award's `max_offer`. Coins never move
between the accounts; a draw and a cancellation only move principal between the pool and the reservation.
`MsgReturnUnallocated` is governance-only and returns from the contributor account through `FundCommunityPool`,
preserving Distribution's ledger: any denomination from custody outside grants. The member account has no exit.
Cancellation reopens a member's unearned NOAH in the tranche, the step it was charged to, returns a contributor's to
the pool, and leaves other denominations uncommitted in custody for the next award.

| Action | Authority |
| --- | --- |
| Set operational params, appoint or replace the committee, open tranches, create contributor awards, cancel grants, return uncommitted contributor custody | Governance |
| Appoint, replace, or disable the registrar | Committee, under its exact term and active window, for a window inside its own |
| Register members within the open tranche | Registrar, under its exact term and active window |
| Suspend members, award compensation within the term's allowance | Committee, under its exact term and active window |
| Reinstate members | Committee or governance |
| Pay what a grant owes | The block, at its end; always to the stored payee |
| Change member payee | Original registered member key |
| Change contributor payee | Beneficiary's current controller |
| Rotate contributor controller | Current controller or governance |

Neither the committee nor the registrar can change a payee or cancel a grant. Changing a contributor controller cannot change control of a
member grant at the same address. Registration is permanent: cancellation, completion, controller rotation, or a new
payee never makes the original member address eligible for another member grant. The off-chain admission process must
still detect people presenting different addresses. Governance can recover contributor control; there is no substitute
signer for a lost original member key.

## Schedules and cancellation

Each kind has one shape, held in params and copied onto the grant at creation, so a later parameter change never
rewrites a live grant (D116). A shape is five integers: a first period's length and weight, each further period's length and weight, and how many
further periods follow, at most 1,200, each length within a century and each weight under 2^32. The first share is
`amount × first_weight / total_weight`, each further share `amount × period_weight / total_weight`, both floored, the
last period takes the remainder, and every period must receive a positive base-unit amount; a shape with no further
periods pays everything in its first. Accrual is one division, so its cost does not grow with the schedule. The grantor sets `start_time`, up to a century ahead: an ownership clock no earlier than
the award, a compensation clock at most a quarter back (`MaxCompensationBackdate`, three plan months), by proposal or
committee alike (D122). The clock runs from it and
never restarts. The grant also records `created_time`, the block it was awarded in, and **nothing accrues
before one period after it**, wherever the clock starts: a backdated clock pays its elapsed periods at that first look,
a clock already run out is due then, and a cancel before it returns everything. The compensation window measures from
the same time. Period boundaries are inclusive.

- **Members** take `member_schedule`, a first period then equal periods like ownership's, and receive the first share
  immediately in the registration transaction, regardless of its length; their clock is the registration. The remaining shares accrue at cumulative period boundaries. Default terms are
  the plan's step 1 amount, 20,000 NOAH, a tenth immediately, then twelve monthly periods, as the launch artefact opens;
  each step's tranche proposal lowers it.
- **Ownership** takes `ownership_schedule`, a first period paid whole at its end then equal periods: at launch a
  one-year first period weighted as the 26 fortnights it spans, then 78 fortnights, so it pays a quarter. It pays nothing
  immediately. The ownership cap bounds the award, not its payments, so each payment is the whole accrued unpaid
  principal (D121).
- **Compensation** pays `periods` equal periods of `compensation_period`, fortnights at launch, one for a bounty and
  together at most ten years, outside the ownership cap. Its denomination is
  NOAH or an active native Asset registry entry.
  One record's compensation is at most `compensation_cap` in the reference unit a `compensation_window`, valued at the
  fresh oracle rates when awarded. In the window after it is made an award charges what it pays there: its whole value
  if it runs within a window plus its backdate, otherwise its value over its duration across that span; after that a
  stream charges its value over its duration a window at a time while it runs, rounded up throughout. The sum over the
  record's awards still running or made inside the trailing window, cancelled ones included for that window, must fit
  (D119, D122); a clock that starts after the award counts from the award, and an award the oracle cannot value is
  refused. Suspending an asset later does not erase existing obligations. Funding and payment remain
  in the award's denomination; a stablecoin award converts NOAH for what custody lacks when it is created and carries
  no exchange-rate guarantee afterwards.

These payments are **untaxed protocol disbursements**, including stablecoins. A caller still pays normal transaction
gas. Paid coins are ordinary spendable coins; unpaid escrow cannot stake or vote. Compensation payments do not count
as ownership awards even if the recipient subsequently stakes them.

Suspension blocks member payments while their original schedule continues, and it stands until the committee or
governance reinstates it, across a committee replacement or a mandate expiry alike. Reinstatement permits catch-up:
the block that lifts it pays the arrears. A suspension records when it was made, which a later cancellation uses as
its cutoff, so governance cancels a known sybil rather than leaving them suspended.

Cancellation freezes entitlement at the cancellation time, or at the effective suspension time for a suspended member.
It retains accrued unpaid principal, frees only unearned principal, and **sends nothing itself**: the block pays the
retained debt to the current payee afterwards, and a payee that refuses defers that payment without touching the
cancellation. Cancelling an ownership grant drops its unearned principal from the record's committed ownership; what it
earned stays committed. A cancelled grant has no future accrual, cannot be reinstated, and cannot
be registered again as a member.

## Payment

Every grant that owes principal and is not suspended carries `due_time`, the block time at or after which the module
next looks at it, and the due queue indexes those times. At the end of each block the module takes the queued grants
whose time has come, oldest first and at most 1,000, and pays each what its schedule has accrued; the rest stay due
for the next block. One payment catches up every boundary that has passed, so a grant holds one
queue entry however long the chain was down. After a look the grant waits for its next boundary. Registration queues a member for
the boundary after the tenth, a contributor award for its first boundary, cancellation queues retained debt and
reinstatement queues arrears for the block they happen in, suspension leaves the queue, and a payee change brings
a later look forward to its own block, never before the grant could first be paid, so a corrected destination is paid
at once rather than at the retry.

A failed payment inside the block is a halt, so the inputs that could fail one are refused at the write: a payee
Bank blocks is refused at creation and at a payee change, as is either disbursement account, which Bank leaves
unblocked for deposits and whose payment would only return to custody, and amounts and schedules carry their bounds. The inputs no
write can vet are a Bank send restriction that refuses the payee later, such as a stablecoin paid into a NOAH-only
fund account, and a payee the blocked list names by the time an export relaunches, which import accepts. That send is
deferred instead: nothing is written, `EventPaymentDeferred` names the
grant, payee, amount, and the retry time, the node log records why, and the grant waits a day or its next boundary. Every other failure in the
hook is an accounting invariant and fails the block, as in Claims. Disbursement runs after Market in the block, and a
stablecoin award's first boundary is at least a second after its creation, so its custody has settled before it can
fall due. Payments in the block are journaled with the paying account as the actor, and a member's first payment,
which registration makes, with the registrar; a deferral moves nothing and is not journaled.

## Committee awards

The appointment sets the committee's award power: `compensation_allowance`, the most the term may award at reference
value; a disabled mandate carries none. Every committee award is compensation and ownership is governance's alone (D113):
import refuses ownership that carries a term and a member that carries none. `MsgCommitteeCompensate` creates a compensation
award, funded like a governance award: NOAH from the contributor pool within the allowance, and stablecoins from
uncommitted contributor custody first, converting NOAH for the rest within the award's `max_offer`. An award in NOAH or any active asset charges the reference value written on the grant, and a seat holder
is awarded like anyone. Nothing pays before one period after an award, wherever its clock starts, and
`compensation_period` is floored at a week, so a stolen key's awards pay nothing before governance can act.

A grant records the term of the appointment that created it in `mandate_term`: a member the registrar's, an award the
committee's. The live committee term's usage is one figure the module keeps, the charges against the allowance: an
award grows it, a cancel restores nothing, and an appointment zeroes it. Genesis carries it, and import refuses a
figure the term's awards do not add up to. The grants query by committee term lists a term's awards and by
registrar term its registrations, and governance cancels either by ID. A compromised registrar is one committee
transaction: a reappointment advances its term, and governance cancels the fakes from the old term's list. Committee
recovery is two proposals: the first appoints a new committee, which clears the registrar the old one appointed,
cancels the old term's awards and any registrations found fake, and reinstates any members the old key wrongly
suspended; the second, expedited, cancels any award the key made while the first vote was open. Nothing pays within a
period of an award, at least a week, and the two votes take about three days at launch.

## Stablecoin funding

A stablecoin award funds itself when it is created. It draws uncommitted stablecoin already in the contributor account
first, then converts NOAH from the contributor pool for the rest through Market's ordinary swap, inside the
same message, so Market quotes, escrows, mints, and settles it at EndBlock like any trader's conversion. The award
carries `max_offer`, the most NOAH it may spend; the module quotes that cap, refuses an award it cannot fund whole, and
otherwise shrinks the offer by the ratio of need to output until the smallest sufficient offer is found, which the
swap then executes with the shortfall as its floor. The surplus of a few units stays uncommitted for the next award,
as does the unearned stablecoin of a cancelled award, so cancelled pay funds later pay before anything converts.

The denomination must be an active asset. A NOAH award carries no `max_offer`. Market's `buy` query sizes the cap
for a prospective award by the same reverse quote; quoting the whole pay is always enough, since custody only lowers
what converts. Governance awards convert inside their proposal, which executes before Market settles the block.

## Ownership and founding seats

A beneficiary's committed ownership follows its original identity across grants, controllers, and payees.
Genesis seat assembly records each founding validator's seat on the beneficiary record at its operator account: the 5M
seat grant, or for the founder half the other seats' sum (D112). The seat is a record nothing reads, so genesis
validation checks only that it is a non-negative amount. No transaction sets a seat; one
is never supplied by an award or inferred from the current validator set. Founding seats themselves remain native
vesting accounts.

`ownership_cap` in `Params`, 15M NOAH at launch, is the most one record's ownership grants commit from the pools over
every grant (D115), checked when each is awarded (D121). In base units:

```text
committed = sum over the record's ownership grants of (paid + remaining)
ownership_remaining = max(ownership_cap - committed, 0)
an ownership award is refused when its amount exceeds ownership_remaining
```

The module reads neither bonded stake nor the seat. The cap binds the beneficiary record, not the person: the chain
cannot tell that two records are one holder, so for a second address the committed figure undercounts and
`ownership_remaining` overcounts. What it bounds is what any one public case can be paid, so no proposal pays a named identity past the cap;
tying a record to a person is the proposal's reasoning and the admission process, and the contributor pool bounds
the total granted across every record. A seat holder is bound by the same cap as anyone, the seat outside it
(D106, D115). Governance moves the cap by `MsgUpdateParams`; a move changes what later awards admit and leaves existing
grants on their terms, so a lower cap refuses new ownership to a record past it and governance cancels a grant it
means to stop.

## State, journal, and bounds

A live grant holds its whole principal, `amount = paid + remaining`; a cancelled one holds exactly its unpaid earned
debt, and what it no longer holds is what its cancellation entry freed. Each module account keeps one book: its reserved
principal by denomination, which equals the remaining principal of the grants it holds, cancelled accrued debt
included; the member tranche is one figure beside the books. In each account and denomination,
`Bank balance = reserved + uncommitted`; `uncommitted` is the pool for NOAH and custody open to the next award
otherwise, and the member tranche never exceeds the member pool. Payments are recorded on each grant and in the
journal; the module keeps no programme-wide payout total. Bank sends, books, indexes, and journal writes
share the SDK transaction cache; one failure rolls the whole message batch back.

Grant records and member indexes are permanent; the grants mandate exports verbatim, an expired
appointment included. The due queue is an index of each grant's `due_time`, rebuilt at import. Two append-only ledgers reconcile to the records they describe. The grant journal records each
grant's creation, the conversion that funded a stablecoin award and the NOAH it cost, each payment and the payee that
received it, cancellation, suspensions and reinstatements with their actor, and payee changes; an entry's
shape is fixed by its action and checked at the write and on import; its payments and cancellations must sum to the
grant's, its payee changes must lead to the grant's payee, a conversion is the entry after its award's creation, and
the last suspension and the cancellation fall at the times the grant records. The beneficiary journal records each controller
change, and a record's entries must lead from its address to its current controller. In both journals heights and times
never fall as entry IDs rise. Governance actions, parameter changes, appointments, tranches, returns, and term
cancellations, are recorded in their proposals. Every grant entry emits a flat typed `EventGrantEntryRecorded`, with the
action by name, and every beneficiary entry an `EventBeneficiaryEntryRecorded`; a tranche opening emits
`EventTrancheOpened` and a return `EventUnallocatedReturned`. Incoming
funding is visible through Bank and Distribution events. Records are public; references should identify approved
work or an agreement without publishing private personnel information.

| Input or operation | Bound |
| --- | --- |
| Registration, suspension, reinstatement, cancellation batch | 100 entries; no duplicates |
| Payments in one block | 1,000, oldest due first; the rest stay due |
| Wait after a refused send | One day, or the next boundary if sooner |
| One grant, member amount, outstanding reservation per denomination | 128-bit positive principal; zero allowed for balances |
| Tranche, `max_offer` | 128-bit; the tranche never exceeds the member pool, an offer never the contributor pool |
| Schedule | a first period and at most 1,200 further, each within a century, weights positive and at most 2^32 |
| Committee awards | Compensation only, within the term's reference-unit allowance; nothing paid before one period after the award |
| Registration | Within the open member tranche, `member_amount` a head; a cancel reopens the unearned part |
| Grant clock | Up to a century ahead; ownership no earlier than the award, compensation at most a quarter back |
| Compensation per record | `compensation_cap` in the reference unit a `compensation_window`: an award charging its arrears with its first window, a stream its rate a window while it runs, cancelled awards counting for a window |
| Grant reference, a member's its batch's | Nonempty, at most 2,048 bytes (`chain.MaxReferenceBytes`, shared with Reserve and Claims) |
| Query page | At most 100 records, key pagination only; no offsets or count-total scans |

Registration is bounded by the open member tranche alone: a stolen registrar key, or a stolen committee key through a
registrar of its own, commits at most what governance has opened before the committee rotates the registrar in one
transaction, so governance opens a step in slices sized to the exposure it accepts. The launch and testnet artifacts
leave both appointments disabled until appointed.

Genesis validation checks identities, schedules, each kind's clock and compensation's shape, conservation, due times, account books, contiguous IDs, journal accounting, controller chains, both
appointments, the registrar inside the committee's window, tranches, each committee term's awards, and each member's registrar term; every list arrives in storage-key order, which is also the duplicate check.
Import creates both module accounts before writing state, so no deposit can leave a base account at either address. It also checks that neither the committee nor the registrar is
the authority, each account's Bank backing, tranches and orders included, that the member account holds NOAH only and the contributor account NOAH or
registered assets, as the send restriction admits, that every grant pays NOAH or a registered asset, and heights and timestamps against the genesis block,
then rebuilds secondary indexes and the due queue. Continuation exports retain absolute times, IDs, member identities, and the journal.

## APIs and source map

The authoritative API is [proto/ark/disbursement/v1](../../proto/ark/disbursement/v1). AutoCLI exposes `arkd tx disbursement` and
`arkd query disbursement`; gRPC and REST expose the same query surface. `params` includes the ownership cap;
`grants-mandate` shows the committee appointment, its term, whether it is active, and the live term's compensation used and remaining; `registrar-mandate` shows the registrar's appointment, its term, and whether it is active; `members` lists member grants in address order, a page of at most 100; `beneficiary` shows a record's control, committed ownership, recorded seat, and room under the cap; `grant` carries `paid`, `remaining`, and `due_time`, the next look.
`balance` shows each account's Bank balance, reservation, and uncommitted custody for NOAH and every registered
asset it holds, and the member tranche; `balance DENOM` shows one denomination, held or not.
`grants --beneficiary`, `grants --committee-term`, `grants --registrar-term`,
`grant-journal --grant-id`, and `beneficiary-journal ADDRESS` use indexes rather than scanning unrelated history. `member` and `beneficiary`
complete the public accounting surface. Queries carry `module_query_safe`; of them, `Grant`, `Grants`,
`Member`, and `Beneficiary` are on the app's separately reviewed Wasm query accept list.

| Source | Responsibility |
| --- | --- |
| [types/schedule.go](types/schedule.go), [types/grant.go](types/grant.go), [types/compensation.go](types/compensation.go) | Schedule shapes and shares, grant validity, accrual and the next look, the compensation shape and cap charge |
| [types/params.go](types/params.go), [types/limits.go](types/limits.go) | Params with both caps; request, work, and amount bounds |
| [types/mandate.go](types/mandate.go), [types/committee.go](types/committee.go) | The grants mandate and registrar appointments, the committee's lane surface |
| [types/books.go](types/books.go), [types/beneficiary.go](types/beneficiary.go), [types/journal.go](types/journal.go) | Account books, beneficiary records, journal entry shapes |
| [types/genesis.go](types/genesis.go) | Import validation across records, books, and journals |
| [keeper/keeper.go](keeper/keeper.go) | Collections, construction, sequences, record reads |
| [keeper/books.go](keeper/books.go) | Account books: reservations, uncommitted custody, tranche draws |
| [keeper/journal.go](keeper/journal.go) | The grant and beneficiary journal writers |
| [keeper/grants.go](keeper/grants.go) | Creation, schedules, the ownership cap, cancellation, the grant write path, the due queue and indexes |
| [keeper/members.go](keeper/members.go) | Member suspension and reinstatement |
| [keeper/compensation.go](keeper/compensation.go) | Compensation denominations, reference valuation, the record's cap |
| [keeper/payments.go](keeper/payments.go) | Entitlement, payment, and the next look |
| [keeper/abci.go](keeper/abci.go) | EndBlock payment of due grants, the per-block bound, deferral of a refused send |
| [keeper/funding.go](keeper/funding.go) | Stablecoin award funding through Market and the conversion entry |
| [keeper/mandate.go](keeper/mandate.go) | Committee and registrar appointment, observation, authorisation, and term usage |
| [keeper/send_restriction.go](keeper/send_restriction.go) | Custody admission on every Bank path, and the registry check import shares |
| [keeper/msg_server.go](keeper/msg_server.go) | Authorisation and request parsing for every message |
| [keeper/grpc_query.go](keeper/grpc_query.go), [keeper/genesis.go](keeper/genesis.go) | Indexed reads and continuation export/import |
| [module](module) | Dependency injection, services, AutoCLI, simulation registration |
| [simulation](simulation) | Simulation genesis on run-owned appointments and a seeded tranche; governance, committee, registrar, and controller factories |
| [testutil](testutil) | Generated mocks of the expected keepers |
| [app/disbursement_test.go](../../app/disbursement_test.go) | Real-app custody, authority, accounting, failure, and continuation tests |
| [tests/integration/disbursement_funding_test.go](../../tests/integration/disbursement_funding_test.go) | Stablecoin awards funded through real Market settlement |

```sh
go test ./x/disbursement/... ./app/genesis ./app -run 'TestNativeDisbursement|TestAddValidatorSeats|TestParamsValidation|TestScheduleShares|TestAccrualBoundaries|TestRetryAt|TestGrantDueTime|TestRandomisedShareConservation|TestGenesis|TestDefaultGenesisState|TestSortBeneficiaries'
go test ./app ./app/genesis ./x/disbursement/...
go test ./tests/integration -run TestDisbursementFundsAwardsThroughMarket
```

The Docker delegator E2E suite additionally exercises opening a member tranche, appointing the registrar through the
committee, registration into fresh and existing accounts, delegation of paid coins, and committee replacement with
registrar rotation through the public CLI.

Errors are registered with their gRPC codes, so a query returns the keeper's error and the code follows it: a
missing grant, member grant, or beneficiary is `NotFound`, on the transaction path as on the query path, and a send
Bank refuses at the payee is `FailedPrecondition`, the one error the EndBlocker branches on. Every other
failure in the module is a plain error, since nothing branches on it. Handlers classify only the request itself.
On a transaction path, wrap with `errorsmod.Wrapf` when the cause may carry a code: the SDK finds a transaction's
ABCI code only through its own wrapper, so a standard-library wrap keeps the text but reports internal.
