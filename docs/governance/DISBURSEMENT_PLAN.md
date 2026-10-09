# NOAH Genesis & Disbursement Plan

- Status: draft 2026-09-23. Member grants and contributor grants are held by the native
  [Disbursement module](../../x/disbursement/README.md) and paid on one schedule each. Contributor ownership payments also check the
  cap at every payment;
  delayed entitlement accumulates without starting another vesting clock. Founding seats keep their native vesting
  accounts. This is a proposal, not launch behaviour.
  Every genesis figure below is the decided value in [Launch genesis](GENESIS.md); the plan adds what genesis leaves to
  governance: the split, the member grant, the contributor bands, the payment schedules, the distribution steps, the
  per-person caps, and the gates. Nothing here changes the artefact.

**Goal:** hand the chain to its users. The founding validators hold every vote at launch and lose control as the
disbursement pools are distributed and staked; they are not the permanent operators of the chain. At no point may one party
hold ⅓ of bonded stake: ⅓ of bonded consensus power halts the chain, and ⅓ of the non-abstaining vote blocks any
proposal at the 66.7% threshold. The one exception is the founder's seat at genesis, a third of the founding stake by
design, which public stake dilutes from the first delegation and the founder's grant extends on the schedule in §5
(D112, D115). Working limit: no record is paid more than 15M of ownership from the pools, under a fifth of the vote at
the founding stake and a tenth once bonded stake passes 150M (D115).

---

## 1. Where the supply sits at genesis

Initial supply: **1,000M NOAH** ([genesis §3](GENESIS.md#3-accounts-supply-and-validator-seats)). Every figure is per
seat, with ten seats as the worked example: genesis fixes each seat at 5M whatever count the launch review admits, seven
to thirteen, and the founder's seat at half the other seats' sum (D111, D112), so the count sets the pools' cost rather
than the seat. Every seat's grant and float, the founder's larger seat included, come out of the community pool; the
founder's 15M grant for the work before launch comes out of the contributor pool like any founding-band grant (§4).

| Account            | Amount          | Type                                                                                                | Can do                                                                                                                                                                                          |
| ------------------ | --------------- | --------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Protocol funds     | 165M            | Four module accounts: subsidy pool 100M, strategic Reserve 50M, Redemption Buffer 10M, Insurance 5M | Whatever each fund's own rules allow: settlement draws the subsidy, redemptions draw the Buffer, governance commits or burns the Reserve, recorded claims draw Insurance. Cannot stake or vote. |
| Disbursement pools | 732M            | Two Disbursement module accounts: members 439.2M, contributors 292.8M                               | The member pool pays nothing until governance opens a tranche, then members or nobody; the contributor pool pays what governance awards and can return to the community pool.                                                        |
| 10 founding seats  | 68M: nine of 5.05M, the founder's 22.55M | Continuous vesting accounts                                                                | Stake, vote, pay fees from the 50k float. The grant vests from year four to year eight: it delegates, votes, and can be slashed, and is not transferable until it vests.                       |
| Community pool     | 35M             | Distribution module account                                                                         | Nothing on its own. Tokens leave only by governance proposal.                                                                                                                                   |

Nobody holds the pools' 732M. It is protocol-owned from block 1, and the split is a module rule rather than a promise: the
member pool has no exit, so no proposal can redirect it, only a chain upgrade. That is what prevents the founder from
being the "one person." The community pool keeps 35M at ten for what neither pool covers (D88, D115).

The pools have one use each, and nothing is drawn from them for validators after launch (D84):

| Use          | Amount      | Mechanism                                      |
| ------------ | ----------- | ---------------------------------------------- |
| Members      | 439.2M, 60% | Member grants paid out over a year, sized per step from 20,000 down to 5,000 NOAH, in five steps (§4) |
| Contributors | 292.8M, 40% | Scheduled ownership grants in five steps, the founder's 15M among the first, pay streams, and stablecoin conversion for them (§4) |

The protocol funds are four seeded bank balances with their own exits (D9, D26), not one line and not an earmark in the
community pool.

---

## 2. Validators

**The founding set.** Vetted at the launch review; equal seats of **5M vesting over eight years + 50k liquid**
each, seven to thirteen of them, 45M of other seats at ten, and the founder's seat at **half the other seats' sum**, a
third of the founding stake at any count, 22.5M at ten, on the same account and schedule and from the same pool (D111,
D112, D115).

- The grant is a continuous vesting account: nothing vests for four years from genesis, then it vests evenly to the
  eighth year, about 104,000 NOAH a month for a seat and 469,000 for the founder's at ten (D114). It is a founding
  allocation with long vesting rather than pay for service: a founder who leaves keeps it (below). Unvested
  coins can be delegated, vote, and be slashed, never transferred: a double-sign takes 250,000 NOAH of it, downtime
  450. A founder who stops validating keeps the schedule and its vote through whichever validator they bond to, like
  any grantee.
- The 50k float covers the week before seat income starts: gas, proposal deposits, and a committee account's funding
  (D96). Beside that income it is the only liquid NOAH a founder holds until the seat begins to vest in year four.
- Each validator self-delegates one seat in their gentx, the founder included, at a commission of at least the 5%
  floor. In the first blocks the founder delegates the rest of the seat across the other validators, so no node
  holds a third of consensus power and halting still takes four nodes. A delegator's vote overrides the validator's,
  so one vote from the founder's account carries the whole seat wherever it sits, and a founder who does not vote
  lets each validator vote the stake it carries. Genesis cannot hold the spread, since validators exist only once the
  gentxs execute, so it is the launch procedure's last step ([genesis §12](GENESIS.md#12-assembly-sequence)).
- Founding validators receive **no grants** for validator work; other work is granted like anyone's, under the cap in §4.

**Entry is open.** Nothing gates validator creation: anyone holding NOAH can bond it and register, and the set caps at
100 validators. No proposal grants a seat after launch (D84). A contributor or member can bond payments they have
received as a validator's own stake. Unpaid grants stay in the module and cannot supply that stake. Power
past genesis is own stake plus public delegations, and the public decides delegations. Equality of the founding seats is
a launch fact, not an enforced property.

**Income.** A seat earns its share of the per-seat reward targets, 0.025 NOAH a block split 70/30 between the validator
and oracle lanes, plus commission on public delegations. Both lanes are split by delegation inside each validator, so a
seat's income is undiluted until the public delegates to it. The targets are the founding seats' shares and do not move
with the set, so a validator that joins splits the existing pot by power and lives on commission until governance raises
the targets (§6).

---

## 3. Consensus safety without a ballast

An earlier draft placed 150M of pool NOAH under a multisig, delegated equally to the founders, to keep insider consensus
weight high while the public's stake was small. It is removed. Its job is done by three things that exist:

- **Delegators cannot halt the chain; validators can.** Halting takes ⅓ of bonded consensus power on validators that
  stop signing. A grantee can only delegate to a validator, or register one and bond to it. The founders hold all
  consensus power until someone else bonds enough to matter, and the cap bounds how fast a contributor can (§4). Member
  stake lands where each member sends it: the app's picker defaults away from the founding set and shows how close a
  validator is to a third, but nothing bounds a member's choice.
- **The cap keeps any two grantees under ⅓.** The cap is 15M of ownership a record (§4), so at the founding stake a
  grantee holds under a fifth of the vote and two at cap under a third even if nobody else stakes; three could, and
  the shares fall as the public bonds. The founder's seat is a third of the vote and not of consensus power, spread
  across the other nodes (§2).
  The cap binds grant identities; that one person stands behind one is the proposal's claim, not the chain's (§4).
- **A halt is recoverable.** A continuation export with `--jail-allowed-addrs` relaunches with only the listed operators
  ([node operations](../operations/NODE_OPERATIONS.md#upgrades-and-relaunch)). Identified contributors who halt the
  chain gain nothing by it.

What the ballast would have bought beyond this, protection against one rogue founder backed by a whale, is not worth
150M of custody, seven standing rules, and most of every seat's income for years.

---

## 4. Distribution

The disbursement pools' 732M goes to two groups, in five steps, as grants the module holds and pays out by a schedule.

| Group        | Share | NOAH   | Who                                                                   | Grant                                |
| ------------ | ----- | ------ | --------------------------------------------------------------------- | ------------------------------------ |
| Members      | 60%   | 439.2M | People who join the cause: many, each holding a small amount          | Per step, 20,000 falling to 5,000    |
| Contributors | 40%   | 292.8M | People who have done real work for the system: few, paid for the work | Sized per grant, under the cap below |

Members are the handover. Seats and contributors concentrate the vote; members are the group that dilutes it, and at
60% their pool is larger than every concentrated holding together, the seats and the contributor pool. Membership obliges nothing: a
member grant is fully paid a year after registration, members hold, stake, vote, or sell by choice, and a member who
sells passes their share of the public to whoever buys, so the founders are diluted either way, by the market rather
than the roll. The chain needs both choices. Those who stake carry security and the vote; those who sell are the float,
since in the first year member payments are nearly all the liquid NOAH there is, beside the founders' floats and about
1.3M of seat income, and the market outside the chain that opens pay streams (§9) forms from them. The pace governance
opens a member tranche at is therefore also the pace of the float (D102). Contributors are paid for work, so their
grants are sized to it, and the cap is written for them.

**The member grant is set per step**, 20,000 NOAH at launch and falling to 5,000 by step 5 (§7, D91). The proposal
that opens a step's member tranche sets the amount beside it, and a member keeps the amount and schedule they registered
under. A tenth is paid on registration and the rest in twelve monthly payments over the following year. At the planned
amounts the pool reaches about 60,300 members, and step 1's tranche seats 1,500 of them (§7).

**Instrument.** Each kind has one shape, a module parameter governance owns (D116). An ownership grant runs **four
years: a one-year cliff paying a quarter, then biweekly for three years**, 79 payments. A compensation award pays
**biweekly for the number of fortnights it sets**, in equal shares and at most ten years; a bounty is **one
period**. The grantor sets the clock's start: an ownership clock no earlier than the award, since the cliff is the
point and the band's timing multiplier credits the past; a compensation clock up to a quarter back, by proposal or
committee alike, so a stream aligns to the day a hire started, as vesting commonly does (D122); either up to a
century ahead. Whatever the clock,
**nothing pays before one period after the award**: a backdated award accrues its elapsed periods and pays them at
that first look, and a cancel before it returns everything, which is the window governance has against a stolen
committee key (D116). The module holds the coins and sends
accrued payments directly to the payee's account; the ownership cap is checked when a grant is awarded, so no payment
is held back (D121). Coins arrive spendable and stakeable; unpaid coins never stake, vote, or earn staking rewards.

A member grant has **one year, a tenth on registration, then monthly**, its clock the registration itself. Each
month's coins arrive spendable, and what has not accrued stays cancellable. Pay streams use the same clock mechanism
outside the ownership cap. Founding seats are separate: their native vesting accounts can stake before their coins
become transferable (§2).

```text
start_time:    set by the grantor: no earlier than the award for ownership, up to a quarter back for compensation;
               nothing pays before one period after the award

ownership      period 1       length 31536000 s (365 d)    amount a quarter of the grant
               periods 2–79   length 1209600 s (14 d)      amount 1/104 of the grant each; the last takes the remainder
compensation   periods 1–n    length 1209600 s (14 d)      equal shares over n fortnights, at most ten years; a bounty is one period
member         period 1       length 1 s                   amount a tenth of the grant, sent at registration
               periods 2–13   length 2628000 s (30.4 d)    amount 3/40 of the grant each; the last takes the remainder
```

**Mechanism.** Every grant is created by the native [Disbursement module](../../x/disbursement/README.md), which holds both
pools from genesis; governance opens the member pool one tranche at a time with `MsgOpenTranche`, which moves no
coins, and awards from the contributor pool by proposal, with no tranche in between (D117). Typed grant messages
govern awards and recovery, and the member pool has no exit (D87, D88). A member grant is opened when the registrar,
a key the grants committee sets under its own mandate, registers the member's address: the module sends the tenth at once, so the member's own account
pays its gas from the first block, and holds the rest on the schedule above from that block. The chain pays each
month into the member's account at the end of the block its boundary falls in; no one signs for it, and a member who
does nothing is still paid in full (D105). An address that already holds
coins is paid like any other. A contributor
proposal draws the grant from the contributor pool; it reserves no gas and creates no vesting account. The payee may already
hold an account, and the same address receives later payments. Receiving costs nothing, so an unfunded recipient
pays no transaction fee; subsequent transactions can use paid coins.
No proposal carries a member address. Registration is the registrar's, a single hot key the grants committee appoints
for a height window and rotates without a vote (D120); the open member tranche is the only bound on registration, so a
stolen registrar key pays out at most the tenths of what governance has opened before the committee rotates it in one
transaction, and governance opens a step in slices sized to the exposure it accepts.
The committee is a multisig appointed under a term-limited mandate that records the shape of the account it names; the
registrar's appointment records its shape too, a single key. A contributor grant is one `MsgGrantOwnership` or `MsgCompensate` in a proposal, which escrows the whole amount and starts its schedule
(below). A member record keeps the amount and schedule it registered under permanently, so a later step's lower amount
never touches an earlier member; records, indexes, and the append-only journal
grow with registrations and subsequent operations. A tranche proposal passes like any other: half the
bonded stake voting, two thirds of the non-abstaining vote agreeing. At launch the seats are the only voters, the founder's a
third of them (§5); later the public votes. The mechanism never changes, only who is voting. Every grant is on-chain, and
the module's account books and permanent journal expose reservations, payments, destinations, and remaining funds.

**A faked member** keeps paid and already accrued entitlement and loses what has not accrued. The member process (§9)
admits members and finds sybils, mostly after the fact; the module gives the finding somewhere to land. The committee
suspends payments, which moves nothing and which the committee or a vote can undo. Governance cancels with a batched
`MsgCancelGrants`, retaining the months earned by the effective suspension, which the block then pays, and keeping the unearned
rest in the tranche for the next member, never through the pool.
The cancellation itself sends nothing, so a destination that refuses cannot prevent it; the block pays the retained
months afterwards and defers a refusal. The original registered key can
change the member payee without changing identity or becoming eligible to register again. Suspend sits with the
committee because it can only delay; cancel sits with governance because a cancel followed by a registration would let one key redirect a grant.
A stolen committee key can therefore appoint a registrar of its own and pay out the open tranche's tenths through it, pause member payments, lift every suspension, and commit
up to its term's compensation allowance into awards that pay nothing for a period, a fortnight, and no more. The proposal that
replaces it advances the term, so nothing prepared under the old key lands afterwards, clears the registrar it
appointed, cancels that term's awards by
ID, and reinstates the members it wrongly suspended, a hundred a message; a second, expedited vote cancels any award the
key made while the first was open, inside the period before anything pays. A lifted suspension it cannot undo: the block that lifts it pays the member the months since,
so a cancel follows a suspension within the month, and what a lifted suspension can pay stays under a month a member.
What a faked member costs is the tenth plus the months it took to find them, and the year is the time there is to do it;
every registration records its registrar term, so the search starts from the old term's list, and a stolen registrar
key alone is stopped by the committee's rotation without a vote.

**Members through the app.** A member never sees a key, a transaction, or a fee. The interface is the member app the
[tooling direction](../direction/TOOLING_DIRECTION.md#6-make-a-first-party-ark-wallet-the-reference-experience) puts
on top of a wallet: signup, the grant, the monthly claim, and the picker, asking the wallet for every signature and
never holding a key. It runs inside the first-party wallet, so a member sees one app, and connects to any other wallet
through the wallet kit. The chain is the ledger, and the platform behind the app holds no member coins: a custodial
ledger holding the member pool would make its operator the one party the goal forbids, with 44% of supply in one
account. Four pieces make the non-custodial version work, all on modules the chain wires today:

- **Address at signup.** The wallet generates the member's key on their own device, with backup and recovery, and the
  app submits the address into the member process with a signature from that key, so no grant lands on a key lost
  between generation and backup. The committee's registration sends the tenth there, and the member sees a balance, a
  schedule, and the next payment.
- **Gas from the tenth.** The tenth is sent on registration, so the member's own account pays its gas from the first
  block, and the module grants members no allowance. Later payments arrive without a transaction, so an unfunded
  contributor pays nothing to receive and pays later fees from received coins; the module reserves no gas.
- **Actions on the device.** The wallet signs delegations, votes, and reward claims on the member's device at the
  app's request; the month arrives on its own, and the app's monthly transaction is the delegation of it. Nothing
  else signs for a member: the platform holds no authorisation over any member account, so a member who does nothing
  stakes nothing, and the app's defaults are the only steer.
- **Selling** needs nothing new: paid coins are spendable, and the app sends them out, to an exchange or anywhere.

What remains of the platform's influence is the app's defaults. Its validator picker weights validators outside the
founding set once they exist and shows how close each is to a third of bonded stake, so member stake tends away from
making any validator a halt or handing the founding validators the vote the handover is taking from them, but every
delegation is the member's own choice and signature. The app casts no vote: a member who does not vote inherits their
validator's vote by chain rule (§5), and the app puts every open proposal in front of the member, whose own vote
overrides the validator's. The platform can neither move a coin nor cast a vote.

**Contributors by review round.** Contributor grants are ownership-scale, not pay-scale. The design already prices a
skilled year at about 131,000 NOAH, the per-seat income, and at that rate thirty contributors over four years would
spend about 16M of a 292.8M pool. Each grant is therefore a decision about who owns the chain, and every case is argued
in the same units:

| Band            | What it is                                                  | Range       | Share of supply |
| --------------- | ----------------------------------------------------------- | ----------- | --------------- |
| Founding        | Built a subsystem, years of work, before there was anything | 10M to 15M  | 1% to 1.5%      |
| Core, sustained | A year or more of senior work                               | 2M to 10M   | 0.2% to 1%      |
| Scoped          | A deliverable: a module, an audit, a client, a campaign     | 0.2M to 2M  | up to 0.2%      |

**Bounties** are pay, not ownership: a bug, a fix, or a document, about 10,000 to 200,000 NOAH or the same in a
stablecoin. The grants committee pays them as one period, and pay streams biweekly, as compensation within its
term's compensation allowance, nothing paying within a period of the award and outside the ownership cap, since they
carry no band. Governance can pay any of
them by proposal too, and ownership it awards alone: the committee's menu is compensation only (D113). Either way one record's compensation is at most 1M in the reference unit a year: an award within a
year counts whole when awarded, a longer stream at its yearly rate for as long as it runs, and cancelled awards count
for the year after them; a backdated award also counts its arrears in its first year, since its first payment pays
them (D109, D119, D122).

Two multipliers place a grant inside its band: timing, since work done before launch or in the first year carried more
risk and earns two to three times the same work later; and irreplaceability, whether the person could have been hired
for it. Hours and seniority are inside the band, not multipliers on it.

Each step's contributor budget is a review round: what was done since the last round, placed in a band with its
multipliers, and the reasoning written into the proposal's summary field so it sits on chain beside the grant. The
summary holds about ten thousand characters, so a round with several grants keeps each case short or files one proposal
a grant. While the founding set is the electorate (§5) it decides; as the public bonds, governance does, and the written reasoning is
what lets a public vote judge work it did not witness. A founding-band grant may be funded whole once a step has room
for it, with payments
limited by its original clock and the 15M cap, which a grant at the band's top reaches exactly, but a new grant pays
nothing until its first period elapses. Ownership grants do not
provide immediate expense money. The first round sets the precedents every later grant is measured against in public,
so its awards need a reasoned spread across contributors.

**Hires.** The review rounds look back; a team brought in to build the tooling needs a stake in what it builds,
and pay it can spend. Until NOAH has a market outside the chain there is nothing to spend it on: spendable NOAH
converts only into the currencies, which have no off-ramp yet either, so a monthly stream would be pay with
nowhere to go. **Before that market exists a hire gets one grant, in the core band on the four-year schedule,
with the timing multiplier as the pay for the risk.** Once it exists a hire gets two grants in the same
proposal, of different kinds. A **pay stream**: biweekly for the fortnights the award sets, 52 for a two-year hire, sized at
the design's skilled-year rate of about 131,000 NOAH a year, which the Disbursement module holds and pays to the hire
fortnight by fortnight as each elapses. Each payment is spendable on arrival, and the unpaid balance stays in the
module's escrow, where governance can cancel it if the hire leaves or the work stops: a cancel holds the
fortnights that have elapsed for the hire, which the block pays as before, and returns the rest to the pool, so no
payee can fail it. The unpaid balance does not stake, which at pay scale moves no gate. An **ownership grant**
in the core band on the four-year schedule, with its timing multiplier. A team of six for two years is about
1.6M in pay and, at six core-band grants, 12M to 60M in ownership; the streams start at once and the ownership
accrues from approval and pays on its schedule. The stream pays for the hired work: a hire who
also holds a seat draws seat income for the seat and the stream for the work, never both for the same hours.
Before streams open, a hire's ownership is awarded under the cap and pays on its schedule like anyone's, seat or not
(D115).

Stablecoin compensation can use the same module for any active denomination, so it waits for conversion to open (D95). The funding comes from contributor NOAH:
the award itself converts what custody lacks through Market when it is created, offering at most the cap the award
carries, so the proposal or committee message sets the pay and the worst price together. These are untaxed protocol disbursements, with no conversion promise and no contribution to the
NOAH ownership counters. The public record retains the work reference, original terms, actual payments, destination
changes, and cancellations. Genesis fixes founding identities; an award cannot reclassify its beneficiary.

**Founding validators.** A seat holder receives no grant for validator work; the seat, which vests to them over
eight years, and its income are that pay. Other work, before launch or after, is granted like anyone's, in its
band, ownership by proposal with the grantee abstaining and compensation by the committee within its allowance, and bound like anyone's
by the 15M cap on what the record is paid; the seat is outside it, a record nothing reads, and nothing distinguishes a
seat holder's grant (D106, D115). No
rule binds the ten to each other. They are independent validators who may vote in different directions, and a cap
that paid one founder less because another was paid would treat them as the bloc the handover exists to end. Pay
streams are exempt by policy because they are pay-scale, not ownership awards; their unpaid balance cannot stake,
although recipients may stake payments they receive.

The founder's work before launch is paid by a founding-band grant of **15M**, the band's top and the cap exactly, so
the founder sits at the cap's edge and no record leaves the pools heavier (D115). It is proposed in the first review
round like any founding-band grant, the founder abstaining, escrows from the contributor pool, and accrues and pays
on the contributor schedule. The seat is separate: consensus weight at a third of the founding
stake, from the community pool like every seat, vesting on the seat schedule (§2, D112). Grants to the other seat
holders are bound the same way, each by its own record under the cap, and none delays another.

**One schedule, with a capped total.** The whole ownership grant is escrowed in one proposal, from the contributor
pool. The module escrows it and records its start time, schedule, and payee. Nothing
pays at approval. The block pays that payee, an existing account or not, as each period falls due, and only the
controller can change the payee. The cap is one parameter, `ownership_cap`, 15M at launch (D115), checked when a grant
is awarded (D121):

- a record's committed ownership, what its ownership grants have paid or still owe over every grant, may reach the
  cap and no more. Pay streams, member grants, and the seat stay outside this calculation.

An award larger than **the cap less the record's committed ownership** is refused, so every ownership grant pays in
full on its schedule. A cancel drops what the grant had not earned from the committed figure. Fractions floor at the
base unit; there is no million-NOAH payment unit. Each send arrives spendable; the module creates no native vesting
account and grants no fee allowance. Unpaid escrow cannot stake or vote.

For example, a record awarded a 10M grant can be awarded at most 5M more. If the first is cancelled after its cliff,
what it earned stays committed and the rest returns to the record's room. A cap vote changes what later awards admit;
grants already made keep their terms. Catch-up can pay several periods together.

Governance can cancel a grant: its unaccrued balance returns to the contributor pool, while accrued unpaid entitlement
stays escrowed for the payee; an ownership cancel also returns the unaccrued balance to the record's room under the cap. Cancellation freezes the clock and never sends coins
to the payee, so a blocked destination cannot prevent cancellation; the controller can repair that destination.
Already paid coins remain the recipient's. The module has no delegation or vote handler.

| Step | Cumulative distributed | Members, cumulative | Member amount | Members, count | Contributors, cumulative | Gate: public bonded stake before the step opens |
| ---- | ---------------------- | ------------------- | ------------- | -------------- | ------------------------ | ----------------------------------------------- |
| 1    | 50M                    | 30M                 | 20,000        | 1,500          | 20M                      | none                                            |
| 2    | 150M                   | 90M                 | 15,000        | 5,500          | 60M                      | 7.5M                                            |
| 3    | 300M                   | 180M                | 10,000        | 14,500         | 120M                     | 22.5M                                           |
| 4    | 450M                   | 270M                | 7,500         | 26,500         | 180M                     | 45M                                             |
| 5    | 732M                   | 439.2M              | 5,000         | ~60,300        | 292.8M                   | 67.5M                                           |

Each step carries both groups at 60/40: member tranches as rolling proposals, contributor grants as the work is
done. Neither budget holds the next step: whatever is unallocated in either when the gate is met rolls into the next
step, members at that step's amount, contributors at the bands, and the 60/40 holds over the whole
distribution rather than inside each step (D101). The member pool waits for members and is never reallocated to
contributors, and a contributor budget is never a reason to award: a step's grants are sized to the work, and what
is not awarded rolls forward. **Distributed**, for the steps and the 60/40, is what has been drawn from a pool: a
payment and an escrow alike, charged whole when the registration or the proposal executes, a member's to the open
tranche and a contributor's to the pool, which has no tranche (D117). The chain records the member tranche alone, so
a grant the step's contributor budget cannot hold waits for the next step by the plan, not by a refusal, and a
cancelled unaccrued balance returns where it was charged. The gates are fixed minimums of
public bonded stake, the table's figures, 15% of each step's full distribution (D99, D101): escrow cannot lower them,
so a step opens only once the public has actually bonded that much, whatever has or has not been paid. The table
is a budget illustration, not a calendar forecast. **Public bonded stake** is everything bonded except the genesis seats, read as
`arkd query staking pool` less the seats and the founder's, 67.5M at ten, one and a half times the other seats' sum (D112). Each gate is three
quarters of the 20% planning rate applied to the step's distribution, so at that rate a step opens once its coins are
mostly paid and staked, and a 30% year opens it sooner. Step 5 is whatever the pools hold after steps 1 to 4: 732M in all. Idle subsidy returned through
`MsgReturnSubsidy` lands in the community pool, not the disbursement pools (D83, D88).

**Cap per contributor** = the most one contributor is paid in ownership from the pools over every grant, where a
contributor is the beneficiary record a proposal names: **15M NOAH, the founding band's top**, a module parameter
governance sets (D115). It reads the record's payments alone, not bonded stake and not the seat. A fraction of the vote
assumed the recipient staked everything it was paid, the one thing the module cannot measure, and against a public
that sells most of what it is paid it held contributors to a tenth of a small vote while the bands promised figures it
would not pay for years. The figure says what the plan means instead, that nobody is paid more than the founder's
grant, and what it means for the vote is stated rather than enforced:

| Bonded stake                                               | One record at the cap | Two | Three        |
| ---------------------------------------------------------- | --------------------- | --- | ------------ |
| 67.5M, the founding stake at ten, plus the records' own    | 18%                   | 31% | 40%, blocks  |
| 150M                                                       | 10%                   | 20% | 30%          |
| 210M, the planning-rate end                                | 7%                    | 14% | 21%          |

The module enforces the cap on its ownership payments to a beneficiary record. It cannot tell that two records are one
person, so it bounds each public case; the proposal's reasoning and the identity requirement (§9) tie a case to a
person, and the contributor pool bounds the total across every record. It is not a chain-wide limit on
balances acquired elsewhere, and a lower cap claws nothing back. A member grant never approaches the cap; the member
risk is the opposite one, one person holding many wallets, and the member process in §9 guards it at admission, the
stream after.

**Grant rules:**

- One wallet per person. Contributors are identified; members are admitted by the member process (§9).
- Member addresses come from the app at signup. The platform holds no member coins and no authorisation over a member
  account.
- Every grant follows its group's schedule above, held and paid by the module to an ordinary account; contributor
  ownership awards additionally pass the cap.
- The 15M cap is checked against the record's committed ownership when every ownership grant is awarded.
- Every contributor grant is placed in a band, with its multipliers and reasoning in the proposal summary.
- No grants to founding validators for validator work; any other grant to a seat holder passes the cap like anyone's,
  by proposal with the grantee abstaining or by the committee within its allowance.

**Gate before moving to the next step:** public bonded stake is at or above the step's figure in the table. Nothing
else gates a step (D101): unallocated member and contributor remainders roll forward, and a record at the cap waits
on the cap, not on everyone's distribution.

---

## 5. Handover

Only bonded stake votes. The seats are self-delegated at genesis and unvested for four years, so they stay bonded
unless a founder undelegates; a grantee's weight is what they have delegated. Once the founder has spread the seat
(§2) the founder holds a third of the founding stake and the other seats two thirds exactly, so under the strict 66.7%
threshold the launch shape is the same at any count: the founder blocks alone, the other seats together cannot pass
over an active no, the founder passes nothing alone since a third is under the 50% quorum, passing takes the founder
plus half the other seats, and blocking takes the founder or half the others. Public bonded stake erodes it on a
per-seat schedule that does not depend on the count (D112):

| Coalition                                  | Blocks until public bonded stake reaches                    |
| ------------------------------------------ | ----------------------------------------------------------- |
| The founder alone, or half the other seats | about 0.3% of the founder's seat, roughly 68k NOAH at ten   |
| The founder plus one seat                  | 15M, three seats' worth                                     |
| The founder plus m seats                   | 15M times m                                                 |

So the seat alone loses its solo veto with the first few members' delegations, and each ally seat buys the founder
15M of public stake before the coalition loses the block: at the step-5 gate the founder plus five seats still block,
which at ten seats is a majority of the launch set rather than a founder power. The founder's 15M grant adds to the
seat as it is paid (§4), and the seat plus grant block alone while public bonded stake is under twice what the grant
has paid: 7.5M after the grant's first year, 15M after its second, 22.5M after its third, and 30M once it is paid in
full, with one seat beside them 15M more in each row (D115). At the planning pace public stake runs behind those
lines until about year five, so the founder blocks alone through roughly the first half of the distribution. The
founder is the largest wallet until bonded stake passes ten times the seat and grant together, 375M at ten, past the
distribution at the planning rate. Every other seat is under a tenth from the first public delegation at eight seats
or more, and at seven once the public has bonded 5M. Ownership payments to seat holders are bound record by record,
each under the 15M cap; whether the seats together stay under a
third is what the public votes them, since the chain does not assume they act as one (§4, D106). Unpaid member and
contributor grants cannot help: ownership grants pay nothing during their one-year cliff, then only what both the
clock and cap admit. Paid coins may be spent or sold as well as staked. The plan therefore assumes no early
delegation of unvested contributor grants and gives no calendar date for handover. The gates measure live public
bonded stake; grant approvals and elapsed time alone move neither the stake nor the schedule.

The table measures the seats' own stake. A vote a validator casts carries every delegation to it that does not vote
itself, so with member stake delegated to the founding validators and left silent, the schedule runs on paper while
those validators still cast the member vote. Effective control passes when grantees vote or delegate elsewhere, which
is why the app's picker defaults away from the founding set and puts every proposal in front of the member (§4).
Quorum needs half of all bonded stake to vote, which the seats cannot supply once the public has bonded more than
they hold; silent delegations vote with their validator, so quorum is validator turnout, carried by whichever
validators grantees bond to.

**End state** (the seats' bonded stake 67.5M at ten plus the founder's 15M grant; members and other contributors hold
717M):

| Grantees' staking rate | Grantees bonded | Founding set's share of bonded | Founder's share |
| ---------------------- | --------------- | ------------------------------ | --------------- |
| 20%                    | ~143M           | ~37%                           | ~17%            |
| 30%                    | ~215M           | ~28%                           | ~13%            |
| 50%                    | ~359M           | ~19%                           | ~8.5%           |

This table describes the end state after payments, not the first years, when much of each grant remains in escrow.
The founder is about a sixth of the vote at the planning rate and under a tenth only at the top of the table. The
founding set owns 8.25% of supply at ten: 4.5% in nine seats, 2.25% in the founder's, and 1.5% in the founder's
grant. Members own 43.9% and other contributors 27.8%, and together they control the community pool, the validator
set, and the protocol funds' policy through governance. Founding
validators will likely remain among the largest validators through public delegations, but that power is borrowed, and
any delegator can move it.

---

## 6. If the public does not stake

Nothing pays people to stake. There is no inflation (D1); the reward pot is the fixed floors plus tax and gas
revenue: ten seats' 1.3M NOAH a year is 1.9% on the seats' 67.5M before commission and falls under 1% once
bonded stake passes about 130M. Scheduled payments do not delegate themselves: the recipient chooses whether to
stake spendable coins. Unpaid escrow earns no staking rewards and cannot supply public stake. The gates in §4
slow distribution when that lags, which is the safe direction, and two levers exist:

1. **Raise the reward targets for a bounded period.** `MsgUpdatePolicy` sets both per-block targets; the economic
   committee's corridor reaches twice the seat-scaled launch value and governance can go further within the domain cap
   ([genesis §4](GENESIS.md#4-treasury), [§13](GENESIS.md#13-committee-appointments)). A higher validator target is a
   higher delegator yield, paid from the subsidy pool at a stated cost to its runway; lower it again once public bonded
   stake has passed what the seats hold. The same lever funds validators who join after launch.
2. **Founders undelegate.** Unvested coins need not be bonded, and unbonded coins do not vote, so a founder can shrink
   their own weight at any time. This is a promise, not a rule, and the plan does not depend on it.

---

## 7. Why these numbers

- To reach ⅓ of bonded stake, an attacker must bond **half** of what everyone else has bonded. Against the seats' 67.5M
  at ten that is 33.75M, and the cap keeps any two grantees under it.
- Compensation to one record is capped at 1M in the reference unit a year, a stream counting at its yearly rate for
  as long as it runs: seven streams at the design's 131,000 NOAH rate fit side by side, or one stream and four maximum
  bounties, while no single address can take the committee's whole compensation allowance; the reference unit holds
  the figure steady against NOAH (D109, D119).
- The cap is 15M a record, the founding band's top and the founder's grant, because a fraction of the vote assumed
  the recipient staked everything it was paid, the one thing the module cannot measure, and against a public that
  sells most of what it is paid it held contributors to a tenth of a small vote while the bands promised figures it
  would not pay for years. One figure says what the plan means, nobody paid more than the founder, and what it means
  for the vote is stated in §4 rather than enforced: a record at the cap is under a fifth of the vote at the founding
  stake, a tenth at 150M of bonded stake, and falling (D115).
- 60/40 because members are the handover: at 439.2M their pool outweighs the seats' 67.5M and the contributors' 292.8M
  together, so once grants are delegated the group that dilutes the vote holds the largest share of it: about 42% of
  bonded stake at equal 20% staking, since founders bond everything and members a fifth, and the majority only with
  the other contributors (D101).
- A member amount set per step, 20,000 NOAH at launch falling to 5,000, because the value of a fixed NOAH amount, and
  with it the payoff for a faked member, would float with the price: a falling schedule keeps both flatter if the chain
  succeeds, pays the first cohort for carrying the most risk and doing the vouching, and needs no oracle, where a value
  anchor would need a NOAH market the distribution itself is meant to create. Each step's proposal sets the figure
  against the price it sees, and a member keeps the amount they joined under (D91). A tenth on the day and the rest
  within a year is real to a person joining; at 20,000 step 1 seats 1,500 members, a year of vouching at over a hundred
  a month. Membership obliges nothing, so the grant is ownership rather than pay, and the joiner and the faker bound it,
  not the work: the floor is the cost of joining, an identity check, a voucher, and a wallet, under which it reads as
  an airdrop; the ceiling is the price of a bought identity, since a fake does nothing a real seller does not, so each
  step's vote holds the tenth, the part no finding recovers, under that price; the pool over the grant is the roll.
  Nothing is priced at launch and the first cohort is founder-vouched, so the launch figure risks least, and the later
  figures are illustrations each step's vote applies the rule to (D102). The member process guards against one person
  holding many wallets at admission, and the stream lets a member found within the year lose what is unearned.
- The contributor bands are ceilings and judging guidelines, sized to the work and never to the pool: the cap
  admits 15M a record, and what a step does not award rolls forward (D101, D115).
- The founder's seat at half the other seats' sum, a third of the founding stake, because the founder needs weight
  at the start that does not depend on the other seats: with every seat independent, a grant voted years later by
  strangers is a promise, and one seat among the set is no weight in the launch electorate. The third is the veto,
  chosen on purpose; it dissolves on the schedule in §5, and the founder is the largest wallet until bonded stake
  passes ten times the seat and grant together. The founding band tops at 15M, the founder's grant for the work
  before launch and the cap; the seat is consensus weight sized by the threshold, not pay (D112, D115).
- Seven to thirteen seats because the founding stake must clear the 40M a buyer of the first year's liquid NOAH could
  take a third against, which the founder's third does at seven, and the founder's seat reaches 3% of supply at
  thirteen. Outside that range launching is a decision, not something assembly absorbs by resizing seats, which would
  reverse D111 for D111's reason and make the dissolution schedule move with the review (D112).
- Four years with a one-year cliff for contributors to spread ownership payments over the contribution horizon; ownership accrues on that clock
  while the cap can delay actual payment. One year for
  members, a tenth on registration, because a member grant over four is thin and a tenth on the day is real to a person
  joining. A member grant is streamed rather than vested because a vesting account has no clawback, so a faked member
  found after registration would still collect the whole grant, and the stream is what makes the find worth anything.
  For members the schedule also sets how long a faked member can be found before the full grant is paid.
  Handover depends on both groups' actual payments and their choices to stake, measured by the gates (§5).
- A four-year cliff and a four-year tail for the seat, fully vested at year eight (D114). Vesting gates transfer
  alone, since unvested seats delegate and vote from block one, so the schedule sets when founding stake can be sold.
  The cliff covers the launch window, when the founding set is the electorate and the founder's coalitions still
  block, and keeps the floats the only liquid NOAH a founder holds while the first member cohorts are the whole
  float. The tail paces the exit: a one-year tail is a lump unlock above the member flow of that year, two or three
  leave founders half or more of what is sold, and four keeps them a minority while finishing about when public
  bonded stake passes the seats, the step-5 gate, so the founding stake is fully for sale only once the public
  outweighs it. The decade framing went with D101, which made the seat an allocation a leaver keeps rather than pay
  for ten years of service.
- No group rule binds the seat holders together (D106). The seat holders are independent validators who may vote in
  different directions, and a cap that paid one founder less because another was paid would treat them as the bloc
  the handover exists to end; each seat holder's record is bound by the same cap as any contributor, the seat outside
  it. Approval and accrual alone add no stake; the cap is checked before every payment.
- The plan is sized at a 20% staking rate of paid coins, the rate a live chain paying what Ark pays falls back to in
  a bad year; 30% is the active rate the original Terra ran at on fee-funded rewards, and 15%, Terra Classic today, is the
  scenario every genesis figure must also survive, since Ark's rewards do not make 20% a floor (D101). Genesis figures
  hold at 20% and improve on their own at 30%; figures set by vote are set so the plan works
  at 20% (D99).
- The gates are fixed minimums of public bonded stake, 15% of each step's full distribution, three quarters of the
  planning rate: a step opens only once the public has bonded that much, escrow cannot lower the bar, a weak year
  slows the steps rather than stalling them, and a 30% year opens them sooner (D99, D101).
- The founder's coalitions lose the block on the §5 schedule, 15M of public bonded stake an ally seat. The gates are fixed figures
  of the same stake, so a step number does say how far the handover has come; its pace depends on payment and
  staking.

---

## 8. Standing rules (checklist)

- [ ] Every grant is created by the disbursement module from the pool it holds from genesis, a member's from the
      tranche governance opens and a contributor's by the proposal, on its group's schedule: a contributor's with
      capped scheduled payments, a member's to its recorded payee, controlled by the original member key.
- [ ] The grants committee is a multisig appointed under a term-limited mandate; it appoints the registrar, a single key
      it rotates without a vote, and governance opens member tranches in slices sized to the exposure it accepts, since
      the open tranche is the only bound on registration.
- [ ] The committee awards compensation only, within its term's reference-unit allowance, with nothing paying within a
      period of the award; ownership is governance's alone, and the vote that replaces a committee cancels its term's
      awards by ID, with a second, expedited vote for any made while it was open.
- [ ] Compensation to one record is at most 1M in the reference unit a year, on either path, a stream counting at its
      yearly rate while it runs, a backdated award its arrears in its first year, and cancelled awards counting for the
      year after them.
- [ ] The committee suspends or resumes a member's pay; only governance cancels a member, retaining
      earned debt at the effective suspension cutoff and keeping unearned funds in the tranche, within the month of the suspension,
      since a resumed member is paid the months since.
- [ ] Members and contributors at 60/40 over the whole distribution; a step's unpaid member tranche rolls forward.
- [ ] Cumulative contributor cap is checked against the bonded stake the person does not hold before every ownership payment.
- [ ] Each contributor grant names its band, its multipliers, and its reasoning in the proposal summary.
- [ ] One wallet per person: contributors identified, members admitted by a verified identity and a vouch; the
      platform keeps attestations, never documents.
- [ ] The member platform holds no coins and no authorisation over any member account; the app's picker defaults away
      from the founding set, and the app shows the member every open proposal.
- [ ] No grants to seat holders for validator work; other grants to them pass the cap like anyone's, by proposal with
      the grantee abstaining or by the committee within its allowance.
- [ ] The founder's seat is half the other seats' sum at genesis, from the community pool like every seat, and the
      founder spreads it across the other seats in the first blocks; launch assembly seats seven to thirteen; the
      founder's 15M grant is a step-1 founding-band award with the founder abstaining.
- [ ] Pay streams open once NOAH has a market outside the chain, pay for hired work, and never for the same hours as
      seat income; the module holds them and pays month by month, and only governance cancels what has not elapsed.
- [ ] An ownership grant goes to escrow whole, charged to its funding step; one clock accrues its payments and the
      cap bounds each send. Cancellation returns unaccrued pay and retains accrued pay under the cap.
- [ ] Before each step: public bonded stake at or above the step's gate (D101).
- [ ] Reward targets move only by a stated, bounded policy change.

---

## 9. Open items

- The member process admits by identity and vouch together. A verified identity is the gate, since only it enforces one
  wallet per person: a third-party verifier checks liveness and a document and returns a uniqueness attestation, and
  the platform keeps the attestation and a dedup token, never the document. A vouch from an existing member, founders
  vouching the first cohort, is the second signal and the lever: the vouch graph is what sybil detection reads, and a
  voucher whose vouchees are cancelled can be suspended by the same call. The process is the guard at admission, and
  what it finds within the year the stream takes back (§4). Still open: the verifier, the data it obliges the platform
  to protect, which joins the counsel question below, and who runs the process while the founding set is the electorate, and
  so who holds the committee key and runs the registrar. The committee is a multisig and the registrar a single key it
  appoints: the open tranche bounds a stolen registrar to the tenths of what governance has opened before
  the committee rotates it, the committee's compensation allowance and the period before anything pays bound its awards, and the vote that replaces a committee cancels
  its term's awards, clears its registrar, and reinstates the members it wrongly suspended.
- The member app: a layer on a wallet, with signup, the identity check, and the vouch in front of it and the picker
  defaults §4 sets, hosted in the first-party wallet and open to any wallet through the kit. It and the wallet are the
  tooling dependency for the first member tranche, and none of it is built.
- What counts as NOAH having a market outside the chain, the trigger that opens pay streams (§4): a listing, an
  off-ramp for the currencies, or a depth figure governance reads.
- The band ranges are a first rubric. The first review round fixes the precedents, so the ranges are worth a second look
  against the actual first cases before that round.
- The native Disbursement module holds both pools from block 1, so its custody accounting and tranche checks require
  audit before launch; the rest of the module, and a committee appointment, before the first tranche. Governance may
  return unaccrued contributor escrow on cancellation; accrued ownership pay remains under the caps. Founding
  identities come from genesis seat assembly, and a compensation denomination is NOAH or an active registry asset (D87).
- The gate ratio: 15% of distributed, three quarters of the 20% planning rate (D99); a lower ratio opens steps sooner
  and hands over later, a higher one stalls a weak year.
- When, if at all, to pull the reward-target lever for organic validators or for a stalled handover, and the runway cost
  governance accepts for it.
- Vetting criteria for the founding set.
- Membership obliges nothing (§4, D102), so what remains for counsel before the first tranche is how a grant with
  nothing owed is characterised, and what the identity attestations oblige the platform to protect; the plan answers
  neither.

Resolved by the launch genesis: the protocol funds are the four seeded accounts (§1); the seat grant is 5M a seat and the
founder's half the other seats' sum, 67.5M at ten, vesting over eight years behind a four-year cliff (§2, D84, D111, D112, D114). Decided in this draft: the 60/40 split, the member
grant set per step from 20,000 down to 5,000 NOAH with its rollover and its one-year schedule with a tenth on
registration, streamed by the module with the committee's suspend and the governance cancel, the four-year,
one-year-cliff schedule for
every contributor grant, the hire package with pay streams held and paid biweekly by the disbursement module, deferred
until NOAH has a market outside the chain, and hires kept off the seats until then, no group rule on the seat
holders, the founder's seat at a third of the founding stake and the founder's 15M grant as a step-1 founding-band award with no proposal line, the cap as 15M of ownership a record, a governance parameter the seat sits outside (D115),
one clock per ownership grant, live payment
caps, and a governance cancel that preserves accrued pay, with the grant's charge to the tranche it draws on, the open tranche as the only bound on registration, bounties and pay streams as committee compensation within a term allowance (§4),
membership that obliges nothing, its sellers the first year's float (§4), and admission by a verified identity and a
vouch together, the member app a layer on a wallet (§9). Removed: the ballast, its multisig, its withdrawal schedule, and the question of
moving it into a module (§3); the seat-admission path (D84); the question of locking member grants for good,
answered by the shorter schedule; and the platform's authorisations over member accounts, so a member's stake
moves only by their own signature.

**Assumptions:** the Cosmos SDK in `go.mod` (gov v1, multi-message proposals, the bank sends and community-pool
funding path, and typed Disbursement module governance messages), 21-day unbonding, and the
genesis governance params: 50% quorum, 66.7% threshold, 75% expedited, 33.4% veto, two-day voting ([genesis
§10](GENESIS.md#10-cosmos-sdk-modules)).
