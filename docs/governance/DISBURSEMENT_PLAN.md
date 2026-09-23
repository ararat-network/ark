# NOAH Genesis & Disbursement Plan

- Status: draft 2026-09-23. Member grants and contributor grants are held by the native
  [Disbursement module](../../x/disbursement/README.md) and paid on one schedule each. Contributor ownership payments also check the
  cap and founding-seat bloc rule at every release;
  delayed entitlement accumulates without starting another vesting clock. Founding seats keep their native vesting
  accounts. This is a proposal, not launch behaviour.
  Every genesis figure below is the decided value in [Launch genesis](GENESIS.md); the plan adds what genesis leaves to
  governance: the split, the member grant, the contributor bands, the payment schedules, the distribution steps, the
  per-person caps, and the gates. Nothing here changes the artefact.

**Goal:** hand the chain to its users. The ten founding validators hold every vote at launch and lose control as the
community pool is distributed and staked; they are not the permanent operators of the chain. At no point may one party,
including a founder, hold ⅓ of bonded stake: ⅓ of bonded consensus power halts the chain, and ⅓ of the non-abstaining
vote blocks any proposal at the 66.7% threshold. Working limit: no single wallet above ⅙ of the vote, so blocking a
proposal takes at least three people colluding.

---

## 1. Where the supply sits at genesis

Initial supply: **1,000M NOAH** ([genesis §3](GENESIS.md#3-accounts-supply-and-validator-seats)). Ten seats are assumed
throughout; genesis fixes the per-seat figures, not the count, and every pool figure below moves by 5.3M a seat.

| Account           | Amount          | Type                                                                                                | Can do                                                                                                                                                                                          |
| ----------------- | --------------- | --------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Protocol funds    | 165M            | Four module accounts: subsidy pool 100M, strategic Reserve 50M, Redemption Buffer 10M, Insurance 5M | Whatever each fund's own rules allow: settlement draws the subsidy, redemptions draw the Buffer, governance commits or burns the Reserve, recorded claims draw Insurance. Cannot stake or vote. |
| 10 founding seats | 53M (5.3M each) | Continuous vesting accounts                                                                         | Stake, vote, pay fees from the 300k float. The 5M grant vests from year four to year ten: it delegates, votes, and can be slashed, and is not transferable until it vests.                      |
| Community pool    | 782M            | Distribution module account                                                                         | Nothing on its own. Tokens leave only by governance proposal.                                                                                                                                   |

Nobody holds the 782M. It is protocol-owned from block 1. That is what prevents the founder from being the "one person."

The pool has two uses under this plan, both by proposal, and nothing is drawn from it for validators after launch (D84):

| Use          | Amount      | Mechanism                                      |
| ------------ | ----------- | ---------------------------------------------- |
| Members      | 469.2M, 60% | 10,000 NOAH grants paid out over a year, in five steps (§4) |
| Contributors | 312.8M, 40% | Scheduled ownership grants in five steps (§4)                           |

The protocol funds are four seeded bank balances with their own exits (D9, D26), not one line and not an earmark in the
community pool.

---

## 2. Validators

**The founding ten.** Vetted at the launch review; equal seats of **5M vesting over ten years + 300k liquid** each.

- The grant is a continuous vesting account: nothing vests for four years from genesis, then it vests evenly to the
  tenth year, about 69,000 NOAH a month. It is deferred pay for running the chain through its first decade. Unvested
  coins can be delegated, vote, and be slashed, never transferred: a double-sign takes 250,000 NOAH of it, downtime
  500. A founder who stops validating keeps the schedule and its vote through whichever validator they bond to, like
  any grantee.
- The 300k float covers fees and operations. The floats are the only liquid NOAH at launch, and the only liquid NOAH a
  founder holds until the seat begins to vest in year four.
- Each validator self-delegates the full 5M in their gentx, at a commission of at least the 5% floor.
- Founding validators receive **no grants** for validator work; other work is granted under the bloc rule in §4.

**Entry is open.** Nothing gates validator creation: anyone holding NOAH can bond it and register, and the set caps at
100 validators. No proposal grants a seat after launch (D84). A contributor or member can bond payments they have
received as a validator's own stake. Unpaid grants stay in the module and cannot supply that stake. Power
past genesis is own stake plus public delegations, and the public decides delegations. Equality of the founding seats is
a launch fact, not an enforced property.

**Income.** A seat earns its share of the per-seat reward targets, 0.025 NOAH a block split 70/30 between the validator
and oracle lanes, plus commission on public delegations. Both lanes are split by delegation inside each validator, so a
seat's income is undiluted until the public delegates to it. The targets are the founding ten's shares and do not move
with the set, so a validator that joins splits the existing pot by power and lives on commission until governance raises
the targets (§6).

---

## 3. Consensus safety without a ballast

An earlier draft placed 150M of pool NOAH under a multisig, delegated equally to the founders, to keep insider consensus
weight high while the public's stake was small. It is removed. Its job is done by three things that exist:

- **Delegators cannot halt the chain; validators can.** Halting takes ⅓ of bonded consensus power on validators that
  stop signing. A grantee can only delegate to a validator, or register one and bond to it. The founders hold all
  consensus power until someone else bonds enough to matter, and the cap bounds how fast a contributor can (§4). Member
  stake lands where each member sends it: the app's picker defaults away from the founding ten and shows how close a
  validator is to a third, but nothing bounds a member's choice.
- **The cap keeps any two colluders under ⅓.** The cap is a fifth of the bonded stake the person does not hold (§4), so
  one grantee bonds to at most a sixth of the vote, and two at cap to a third less rounding, even if nobody else stakes:
  each person's own stake is outside their own base, so two people granting in turns converge on a third and never cross
  it. Three could, which is the same coalition size as four of the ten founders, who can halt the chain from block one.
- **A halt is recoverable.** A continuation export with `--jail-allowed-addrs` relaunches with only the listed operators
  ([node operations](../operations/NODE_OPERATIONS.md#upgrades-and-relaunch)). Identified contributors who halt the
  chain gain nothing by it.

What the ballast would have bought beyond this, protection against one rogue founder backed by a whale, is not worth
150M of custody, seven standing rules, and most of every seat's income for years.

---

## 4. Distribution

The community pool's 782M goes to two groups, in five steps, as grants the module holds and pays out by a schedule.

| Group        | Share | NOAH   | Who                                                                   | Grant                                |
| ------------ | ----- | ------ | --------------------------------------------------------------------- | ------------------------------------ |
| Members      | 60%   | 469.2M | People who join the cause: many, holding a small equal amount         | 10,000 NOAH each, fixed              |
| Contributors | 40%   | 312.8M | People who have done real work for the system: few, paid for the work | Sized per grant, under the cap below |

Members are the handover. Founders and contributors concentrate the vote; members are the group that dilutes it, and at
60% their pool is larger than the founders' stake and the contributor pool together. A member grant is fully paid a
year after registration, so past that year members hold and stake by choice, and a member who sells passes their share
of the public to whoever buys: the founders are diluted either way, by the market rather than the roll. Contributors are
paid for work, so their grants are sized to it, and the cap is written for them.

**The member grant is 10,000 NOAH**, the same for every member whenever they join. A tenth is paid on registration
and the rest at 750 a month over the following year. At that size the pool reaches about 46,900 members, and step 1's
tranche seats 3,000 of them (§7).

**Instrument.** Every contributor grant has one schedule from approval: **four years, 25% after twelve months,
then monthly**. The module holds the coins and sends accrued payments directly to the payee's account, subject to
ownership limits. Coins arrive spendable and stakeable; unpaid coins never stake, vote, or earn staking rewards.
A cap can postpone a payment beyond its scheduled date, even beyond year four. Waiting never starts a new clock:
when room opens, accrued unpaid entitlement can be claimed immediately, up to that room.

A member grant has **one year, a tenth on registration, then monthly**. Each month's coins arrive spendable, and
what has not accrued stays cancellable. Pay streams use the same clock mechanism without ownership limits.
Founding seats are separate: their native vesting accounts can stake before their coins become transferable (§2).

```text
start_time:    the block the module opens the grant in; the clock never restarts

contributor    period 1       length 31536000 s (365 d)    amount 25% of the grant
               periods 2–37   length 2628000 s (30.4 d)    amount 1/48 of the grant each; the last takes the remainder
member         period 1       length 1 s                   amount a tenth of the grant, sent at registration
               periods 2–13   length 2628000 s (30.4 d)    amount 3/40 of the grant each; the last takes the remainder
```

**Mechanism.** Every grant is created by the native [Disbursement module](../../x/disbursement/README.md), which governance funds
one tranche at a time by `MsgCommunityPoolSpend` to its module account. Typed grant messages govern awards and
recovery, while the pool still leaves only by its spend message (D87). A member grant is opened when the registrar, a
key governance sets, registers the member's address: the module sends the tenth at once, so the member's own account
pays its gas from the first block, and holds the rest on the schedule above from that block, paying each month to the
address once it has elapsed. Anyone may trigger the payment and it is cumulative, so a claim after six months pays six
months: the app makes it in the member's own claim-and-stake transaction, and anyone, the platform included, may make
it for a member who has not, which only moves the month into that member's own account. An address that already holds
coins is paid like any other. A contributor
proposal spends the grant itself; it reserves no tranche gas and creates no vesting account. The payee may already
hold an account, and the same address receives later payments. Anyone can trigger the first payment, including the
platform, so an unfunded recipient need not pay its transaction fee; subsequent transactions can use paid coins.
No proposal carries a member address. The module issues at most
250 members in any seven days, counted from each registration's block, a limit governance sets, so a stolen registrar
key pays out at most a week's tenths before an expedited vote replaces it, and the registrar is a multisig for the same
reason, appointed under a term-limited mandate that records the shape of the account it names. A contributor grant is one `MsgCreateGrant` in a proposal, which escrows the whole amount and starts its schedule
(below). A member record keeps the schedule it registered under permanently; records, indexes, and the append-only journal
grow with registrations and subsequent operations. A tranche proposal passes like any other: half the
bonded stake voting, two thirds of the non-abstaining vote agreeing. At launch the ten founders are the only voters, so
proposals pass; later the public votes. The mechanism never changes, only who is voting. Every grant is on-chain, and
the module's per-denomination totals and permanent journal expose reservations, payments, destinations, and remaining funds.

**A faked member** keeps paid and already accrued entitlement and loses what has not accrued. The member process (§9)
admits members and finds sybils, mostly after the fact; the module gives the finding somewhere to land. The registrar
suspends payments, which moves nothing and which the registrar or a vote can undo. Governance cancels with a batched
`MsgCancelGrants`, retaining the months earned by the effective suspension for later release and keeping the unearned
rest in the tranche for the next member, never through the pool.
It sends nothing to the member, so a blocked destination cannot prevent cancellation. The original registered key can
change the member payee without changing identity or becoming eligible to register again. Suspend sits with the
registrar because it can only delay; cancel sits with governance because a cancel followed by a registration would let one key redirect a grant.
A stolen registrar key can therefore pay out a week's tenths, pause member payments, and lift every suspension, and no
more. The proposal that replaces it advances the term, so nothing prepared under the old key lands afterwards, and
voids that term's suspensions in one write, so the new registrar suspends afresh only the genuine cases. A lifted suspension it cannot undo: the next release pays the lifted member the months since,
so a cancel follows a suspension within the month, and what a lifted suspension can pay stays under a month a member.
What a faked member costs is the tenth plus the months it took to find them, and the year is the time there is to do it.

**Members through the app.** A member never sees a key, a transaction, or a fee. The interface is the member app the
[tooling direction](../direction/TOOLING_DIRECTION.md#6-make-a-first-party-ark-wallet-the-reference-experience) puts
on top of a wallet: signup, the grant, the monthly claim, and the picker, asking the wallet for every signature and
never holding a key. It runs inside the first-party wallet, so a member sees one app, and connects to any other wallet
through the wallet kit. The chain is the ledger, and the platform behind the app holds no member coins: a custodial
ledger holding the member pool would make its operator the one party the goal forbids, with 47% of supply in one
account. Four pieces make the non-custodial version work, all on modules the chain wires today:

- **Address at signup.** The wallet generates the member's key on their own device, with backup and recovery, and the
  app submits the address into the member process with a signature from that key, so no grant lands on a key lost
  between generation and backup. The registrar's registration sends the tenth there, and the member sees a balance, a
  schedule, and the next payment.
- **Gas from the tenth.** The tenth is sent on registration, so the member's own account pays its gas from the first
  block, and the module grants members no allowance. An unfunded contributor can have anyone trigger the first
  payment, then pay subsequent transaction fees from received coins; the module reserves no gas.
- **Actions on the device.** The wallet signs delegations, votes, reward claims, and the monthly claim on the member's
  device at the app's request, the claim a transaction that releases the month and delegates it in one, and nothing
  else signs for a member: the platform holds no authorisation over any member account, so a member who does nothing
  stakes nothing, and the app's defaults are the only steer.
- **Selling** needs nothing new: paid coins are spendable, and the app sends them out, to an exchange or anywhere.

What remains of the platform's influence is the app's defaults. Its validator picker weights validators outside the
founding ten once they exist and shows how close each is to a third of bonded stake, so member stake tends away from
making any validator a halt or handing the founding validators the vote the handover is taking from them, but every
delegation is the member's own choice and signature. The app casts no vote: a member who does not vote inherits their
validator's vote by chain rule (§5), and the app puts every open proposal in front of the member, whose own vote
overrides the validator's. The platform can neither move a coin nor cast a vote.

**Contributors by review round.** Contributor grants are ownership-scale, not pay-scale. The design already prices a
skilled year at about 131,000 NOAH, the per-seat income, and at that rate thirty contributors over four years would
spend about 16M of a 312.8M pool. Each grant is therefore a decision about who owns the chain, and every case is argued
in the same units:

| Band            | What it is                                                  | Range       | Share of supply |
| --------------- | ----------------------------------------------------------- | ----------- | --------------- |
| Founding        | Built a subsystem, years of work, before there was anything | 10M to 60M  | 1% to 6%        |
| Core, sustained | A year or more of senior work                               | 2M to 10M   | 0.2% to 1%      |
| Scoped          | A deliverable: a module, an audit, a client, a campaign     | 0.2M to 2M  | up to 0.2%      |
| Bounty          | A bug, a fix, a document                                    | 10k to 200k |                 |

Two multipliers place a grant inside its band: timing, since work done before launch or in the first year carried more
risk and earns two to three times the same work later; and irreplaceability, whether the person could have been hired
for it. Hours and seniority are inside the band, not multipliers on it.

Each step's contributor tranche is a review round: what was done since the last round, placed in a band with its
multipliers, and the reasoning written into the proposal's summary field so it sits on chain beside the grant. The
summary holds about ten thousand characters, so a round with several grants keeps each case short or files one proposal
a grant. While the founders hold the block (§5) they decide; after it, governance does, and the written reasoning is
what lets a public vote judge work it did not witness. A founding-band grant may be funded whole once a step has room
for it, with payments
limited by both its original clock and the live ownership rules. At the genesis stake the cap admits at most 10M to
one contributor without a seat, but a new grant pays nothing until its first period elapses. Ownership grants do not
provide immediate expense money. The first round sets the precedents every later grant is measured against in public,
so its awards need a reasoned spread across contributors.

**Hires.** The review rounds look back; a team brought in to build the tooling needs a stake in what it builds,
and pay it can spend. Until NOAH has a market outside the chain there is nothing to spend it on: spendable NOAH
converts only into the currencies, which have no off-ramp yet either, so a monthly stream would be pay with
nowhere to go. **Before that market exists a hire gets one grant, in the core band on the four-year schedule,
with the timing multiplier as the pay for the risk.** Once it exists a hire gets two grants in the same
proposal, of different kinds. A **pay stream**: twenty-four monthly periods of a twenty-fourth each, sized at
the design's skilled-year rate of about 131,000 NOAH a year, which the Disbursement module holds and pays to the hire
month by month as each elapses. Each month's pay is spendable on arrival, and the unpaid balance stays in the
module's escrow, where governance can cancel it if the hire leaves or the work stops: a cancel holds the
months that have elapsed for the hire, who releases them as before, and returns the rest to the pool, so no
payee can fail it. The unpaid balance does not stake, which at pay scale moves no gate. An **ownership grant**
in the core band on the four-year schedule, with its timing multiplier. A team of six for two years is about
1.6M in pay and, at six core-band grants, 12M to 60M in ownership; the streams start at once and the ownership
accrues from approval and pays as the cap and bloc rule admit it. The stream pays for the hired work: a hire who
also holds a seat draws seat income for the seat and the stream for the work, never both for the same hours.
Before streams open, a hire who holds a seat has nothing until the bloc rule admits their ownership, so the team
and the seats are kept separate (§9).

Stablecoin compensation can use the same module once governance approves an active denomination and funds the award
in that denomination. These are untaxed protocol disbursements, with no conversion promise and no contribution to the
NOAH ownership counters. The public record retains the work reference, original terms, actual payments, destination
changes, and cancellations. Genesis fixes founding identities; an award cannot reclassify its beneficiary.

**Founding validators.** A seat holder receives no grant for validator work; the seat, which vests to them over
ten years, and its income are that pay. Other work, before launch or after, is granted like anyone's, in its
band, with the grantee abstaining from the vote. What bounds it is the bloc: every ownership payment to one of
the ten can add to the founders' combined stake. The rule: **an ownership payment to a founding seat holder must
fit the bloc allowance before it leaves escrow.** A grant can be approved and accrue while that allowance is
zero. Pay streams are exempt by policy because they are pay-scale, not ownership awards; their unpaid balance
cannot stake, although recipients may stake payments they receive. A seat holder also stays within the cap, seat
and grants together, so the bloc rule binds while public stake is under about 180M and the cap binds above it:
seat and grant together reach the goal's sixth at the top of the founder's grant, 65M of 390M, and fall from
there. The founder's own founding-band grant, the whole chain before launch at the top of the band, takes no
exception: it is placed in its band in round one and escrows whole once a tranche has room for it, and the two
rules give the following ceilings once enough of the grant has accrued:

| Public bonded stake | Total bonded stake, what the escrow reads | Founder's grant, cumulative | Binding rule |
| ------------------- | ----------------------------------------- | --------------------------- | ------------ |
| up to 100M          | up to 150M                                | nothing, escrowed           | bloc         |
| 110M                | 165M                                      | 5M                          | bloc         |
| 120M                | 180M                                      | 10M                         | bloc         |
| 150M                | 225M                                      | 25M                         | bloc         |
| 200M                | 294M                                      | 44M                         | cap          |
| 280M and above      | 390M and above                            | 60M                         | cap          |

The ownership allowance reaches the ceiling at about a 40% staking rate, after the handover rather than during
it. Actual payments also require the original schedule to have accrued that amount. The grant is escrowed whole
in the first step whose contributor tranche has room for it, step 3 at the earliest, which starts its clock
then. The bloc rule still releases none of it before public stake reaches 100M; calendar time alone does not
admit a payment. Grants to the other nine queue behind it under the same rules: with the founder at 60M the bloc
is 110M, and nine further grants of 5M need 310M of public bonded stake. Hires are kept off the seats, so their
ownership pay checks the individual cap and clock and the bloc stays at 50M; a hire who held a seat would wait
on that.

**One schedule, with capped payments.** The whole ownership grant leaves the pool in one proposal, in a step whose
contributor budget has room for it. The module escrows it and records its start time, schedule, and payee. Nothing
pays at approval. Anyone may trigger a payment to that payee, including an existing account, and only the controller
can change the payee. The module reads total bonded stake `T` at each payment:

- a person's seat and all ownership payments already received are `own`, assumed bonded even when they are not.
  Their cumulative ownership may reach `(T − own) / 5`; ownership grants alone also have a 60M ceiling, excluding
  the seat. The next payment fits within the remaining room under both limits;
- seat holders together may receive ownership payments up to `T/3 − 50M`, less what they have already received.
  Remaining room is shared pro rata by their remaining ownership escrow, including accrued pay retained after a
  cancellation. Pay streams and member grants stay outside this calculation.

The payment is the smaller of **accrued unpaid entitlement** and **the allowance under those rules**. Fractions floor
at the base unit; there is no million-NOAH payment unit. A partial payment does not mark a whole period paid: the
unpaid part stays accrued, and later periods continue to accrue on the same clock. Each send arrives spendable; the
module creates no native vesting account and grants no fee allowance. Unpaid escrow cannot stake or vote.

For example, a 30M ownership grant has accrued 7.5M after one year and 15M after two. With 50M of other stake and prior
payments bonded, its cap admits at most 10M in total: it can pay the first 7.5M, then 2.5M more, leaving 5M accrued
and waiting at year two. At year four all 30M has accrued, but any unpaid portion still waits for ownership room.
When that room opens it pays from the original entitlement, with no further vesting delay. Catch-up can pay several
periods together. Neither the four-year end nor the cap promises a final payment date.

Governance can cancel a grant: its unaccrued balance returns to the community pool, while accrued unpaid entitlement
stays escrowed for the payee, under the same ownership limits. Cancellation freezes the clock and never sends coins
to the payee, so a blocked destination cannot prevent cancellation; the controller can repair that destination.
Already paid coins remain the recipient's. The module has no delegation or vote handler.

| Step | Cumulative distributed | Members, cumulative | Members, count | Contributors, cumulative | Gate: public bonded stake before the step opens |
| ---- | ---------------------- | ------------------- | -------------- | ------------------------ | ----------------------------------------------- |
| 1    | 50M                    | 30M                 | 3,000          | 20M                      | none                                            |
| 2    | 150M                   | 90M                 | 9,000          | 60M                      | 15M                                             |
| 3    | 300M                   | 180M                | 18,000         | 120M                     | 45M                                             |
| 4    | 450M                   | 270M                | 27,000         | 180M                     | 90M                                             |
| 5    | 782M                   | 469.2M              | ~46,900        | 312.8M                   | 135M                                            |

Each step carries both groups at 60/40: member tranches as rolling proposals, contributor grants as the work is
done. Admitting members is slower than paying for work, so a step's member tranche never holds the next step:
whatever is unpaid when the contributor tranche and the gate are met rolls into the next step's member tranche,
and the 60/40 holds over the whole distribution rather than inside each step. The member pool waits for members;
it is never reallocated to contributors. A step's contributor budget is committed when its grants have been
funded, including amounts still escrowed. **Distributed**, for the tranches and the 60/40, is what has left the
pool: a payment and an escrow deposit alike, charged whole to the step in which the proposal executes, so a
grant the open tranche cannot hold waits for the next step, escrow deposits included, and a cancelled unaccrued
balance returns to the pool and to the tranche it was charged to. For the gates it is what can be staked: actual
grant payments, since escrowed coins can neither stake nor vote; the gates in the table assume nothing is in
escrow, and with coins escrowed and unreleased the gate is 30% of what has been paid or released, which is
lower. Both member and contributor escrow are ordinary cases: a gate reads actual payments, not the size of the
approved grants. The table is an upper budget illustration, not a calendar forecast or a fixed gate independent
of unpaid escrow. **Public bonded stake** is everything bonded except the founders' combined stake, read as
`arkd query staking pool` less 50M, and less what the module has released to seat holders (§4). Each gate is
30% of actual grant payments so far, so a step opens only once enough paid coins are being staked, not merely
held. Step 5 is whatever the pool holds after steps 1 to 4: 782M with ten founding seats, and more if governance
returns idle subsidy through `MsgReturnSubsidy` (D83).

**Cap per contributor** = the most any single person may hold from the pool in total, seat and grants together. It is
measured, not scheduled: **a fifth of the bonded stake the person does not hold, total bonded stake less their own, at
each payment, rounded down to the base unit. Cumulative ownership grants are capped separately at
60M, excluding the seat.** Their own includes the seat and ownership grants they have received, bonded or not;
counting unbonded coins only tightens the measure. A seat holder's total ceiling is therefore 65M, all of which still
counts against the fifth. The base excludes the person's own stake because a share of a total their own bonding
raises would let two people grant each other up to half the chain (§7).

For a contributor without a seat:

| Bonded stake the person does not hold | Cap |
| ------------------------------------- | --- |
| 50M (genesis)                         | 10M |
| 100M                                  | 20M |
| 200M                                  | 40M |
| 300M and above                        | 60M |

A seat holder reaches the full 60M grant ceiling when others hold 325M: the fifth then admits their 5M seat and
60M in grants together, 65M of 390M once bonded. The bloc rule must also admit the release.

Example: against the genesis stake, once their schedules have accrued enough, one grantee can receive 10M and a
second another 10M, but one person cannot receive 20M. Someone who received 10M can receive more once the stake
others hold has grown to admit it; their own bonding never counts. The module enforces the cap on its
ownership payments; it is not a chain-wide limit on balances acquired elsewhere, and a fall in bonded stake does
not claw back earlier payments. A member grant never approaches the cap; the member risk is the opposite one,
one person holding many wallets, and the member process in §9 guards it at admission, the stream after.

**Grant rules:**

- One wallet per person. Contributors are identified; members are admitted by the member process (§9).
- Member addresses come from the app at signup. The platform holds no member coins and no authorisation over a member
  account.
- Every grant follows its group's schedule above, held and paid by the module to an ordinary account; contributor
  ownership payments additionally pass the cap and bloc rule.
- Cumulative contributor cap checked against the bonded stake the person does not hold before every ownership payment.
- Every contributor grant is placed in a band, with its multipliers and reasoning in the proposal summary.
- No grants to founding validators for validator work; any other grant to a seat holder waits for the bloc rule, with
  the grantee abstaining.

**Gate before moving to the next step — all three must hold:**

1. The previous step's contributor budget has been funded into grants; its unallocated member remainder rolls forward.
2. Public bonded stake is at or above the step's gate.
3. No contributor is over the cap.

---

## 5. Handover

Only bonded stake votes. The founders' 50M is self-delegated at genesis and unvested for four years, so it stays bonded
unless a founder undelegates; a grantee's weight is what they have delegated. Three lines follow from the 66.7%
threshold, all in public bonded stake:

| Public bonded stake | Founders' own stake alone can | Crossed when                                                |
| ------------------- | ----------------------------- | ----------------------------------------------------------- |
| below 25M           | pass any proposal             | Public bonded stake has not reached 25M |
| 25M to 100M         | block, not pass               | Recipients have bonded at least 25M |
| above 100M          | neither                       | Recipients have bonded more than 100M |

The lines are crossed by recipients bonding payments they have received. Unpaid member and contributor grants
cannot help cross them: ownership grants pay nothing during their one-year cliff, then only what both the clock and
cap admit. Paid coins may be spent or sold as well as staked. The plan therefore assumes no early delegation of
unvested contributor grants and gives no calendar date for handover. The gates measure actual payments and live
public bonded stake; grant approvals and elapsed time alone move neither the stake nor the handover lines.

The lines measure the founders' own stake. A vote a founding validator casts carries every delegation to it that does
not vote itself, so with member stake delegated to the founding ten and left silent, the lines are crossed on paper
while the founders' validators still cast the member vote. Effective control passes when grantees vote or delegate
elsewhere, which is why the app's picker defaults away from the founding ten and puts every proposal in front of the
member (§4). After the block ends, quorum needs half of all bonded stake to vote and the founders cannot supply it from
their own stake; silent delegations vote with their validator, so quorum is validator turnout, carried by whichever
validators grantees bond to.

**End state** (founders' bonded stake up to 50M; members and contributors hold 782M):

| Grantees' staking rate | Grantees bonded | Founders' share of bonded |
| ---------------------- | --------------- | ------------------------- |
| 20%                    | ~156M           | ~24%                      |
| 30%                    | ~235M           | ~18%                      |
| 50%                    | ~391M           | ~11%                      |

This table describes the end state after payments, not the first years, when much of each grant remains in escrow.
The ten founding validators own 5.3% of supply. Members own 46.9% and contributors 31.3%, and
together they control the community pool, the validator set, and the protocol funds' policy through governance. Founding
validators will likely remain among the largest validators through public delegations, but that power is borrowed, and
any delegator can move it.

---

## 6. If the public does not stake

Nothing pays people to stake. There is no inflation (D1); the reward pot is the fixed floors plus tax and gas
revenue: ten seats' 1.3M NOAH a year is 2.6% on the founders' 50M before commission and falls under 1% once
bonded stake passes about 130M. Scheduled payments do not delegate themselves: the recipient chooses whether to
stake spendable coins. Unpaid escrow earns no staking rewards and cannot supply public stake. The gates in §4
slow distribution when that lags, which is the safe direction, and two levers exist:

1. **Raise the reward targets for a bounded period.** `MsgUpdatePolicy` sets both per-block targets; the economic
   committee's corridor reaches twice the seat-scaled launch value and governance can go further within the domain cap
   ([genesis §4](GENESIS.md#4-treasury), [§13](GENESIS.md#13-committee-appointments)). A higher validator target is a
   higher delegator yield, paid from the subsidy pool at a stated cost to its runway; lower it again once public bonded
   stake has passed the block line. The same lever funds validators who join after launch.
2. **Founders undelegate.** Unvested coins need not be bonded, and unbonded coins do not vote, so a founder can shrink
   their own weight at any time. This is a promise, not a rule, and the plan does not depend on it.

---

## 7. Why these numbers

- To reach ⅓ of bonded stake, an attacker must bond **half** of what everyone else has bonded. Against the founders' 50M
  that is 25M, and the cap keeps any two grantees under it.
- Cap per contributor is a fifth of what everyone else has bonded, measured instead of assumed, so one grantee holds at
  most ⅙ of the vote, the working limit in the goal, and two at cap hold ⅓ less rounding. It is measured against the
  stake the person does not hold because a quarter of the total, the earlier draft, counted the grantee's own bonded
  grants in the total: two people granting in turns crossed ⅓ on their second grant and converged on half the chain with
  nobody else staking. The 60M ownership grant ceiling is a fifth of 300M held by others for a contributor without a
  seat, ⅙ of the 360M bonded once it is staked. A seat holder's 65M including the seat needs 325M held by others,
  ⅙ of 390M once bonded: the end state at about a 40% staking rate.
- 60/40 because members are the handover: at 469.2M their pool outweighs the founders' 50M and the contributors' 312.8M
  together, so once grants are delegated the group that dilutes the vote holds most of it.
- 10,000 NOAH a member because a tenth on the day and the rest within a year is real to a person joining, and because
  it sizes step 1 at three thousand members, a year of vouching at a few hundred a month. Smaller reads as an airdrop
  and larger pays for fraud; the member process guards against one person holding many wallets at admission, and the
  stream lets a member found within the year lose what is unearned.
- The contributor bands are ownership-scale because the pool is: the genesis stake admits up to 10M per contributor once it has accrued, and
  pay-scale grants would leave most of 312.8M unspent.
- Four years with a one-year cliff for contributors to spread ownership payments over the contribution horizon; ownership accrues on that clock
  while the cap can delay actual payment. One year for
  members, a tenth on registration, because 10,000 NOAH over four is thin and a tenth on the day is real to a person
  joining. A member grant is streamed rather than vested because a vesting account has no clawback, so a faked member
  found after registration would still collect the whole grant, and the stream is what makes the find worth anything.
  For members the schedule also sets how long a faked member can be found before the full grant is paid.
  Handover depends on both groups' actual payments and their choices to stake, measured by the gates (§5). Ten years behind a four-year
  cliff for the seat
  because it is pay for a decade of running the chain, and the cliff keeps the floats the only liquid NOAH a founder
  holds through the launch years.
- The bloc rule bounds ownership payments to seat holders so their additional stake does not reverse the public
  handover. Approval and accrual alone add no stake; the allowance is checked before every payment.
- The gates are 30% of actual grant payments so far. They require measurable public stake before another step
  opens, but unpaid escrow makes their numerical thresholds lower than the fully paid budget table.
- The founders lose their own blocking stake once public bonded stake passes 100M. With unpaid escrow, the
  numerical step gates in the table must be recomputed from actual payments; a step number alone does not prove
  handover. That progression is visible on-chain, but its pace depends on payment and staking.

---

## 8. Standing rules (checklist)

- [ ] Every grant is created by the disbursement module from tranches governance spends to it, on its group's schedule: a
      contributor's with capped scheduled payments, a member's to its recorded payee, controlled by the original member key.
- [ ] The registrar is a multisig appointed under a term-limited mandate, and the module's issuance window stays at 250
      members in any seven days unless governance changes it.
- [ ] The registrar suspends or resumes a member's pay and nothing more; only governance cancels a member, retaining
      earned debt at the effective suspension cutoff and keeping unearned funds in the tranche, within the month of the suspension,
      since a resumed member is paid the months since.
- [ ] Members and contributors at 60/40 over the whole distribution; a step's unpaid member tranche rolls forward.
- [ ] Cumulative contributor cap is checked against the bonded stake the person does not hold before every ownership payment.
- [ ] Each contributor grant names its band, its multipliers, and its reasoning in the proposal summary.
- [ ] One wallet per person: contributors identified, members admitted by a verified identity and a vouch; the
      platform keeps attestations, never documents.
- [ ] The member platform holds no coins and no authorisation over any member account; the app's picker defaults away
      from the founding ten, and the app shows the member every open proposal.
- [ ] No grants to seat holders for validator work; other grants to them wait for the bloc rule, grantee abstaining.
- [ ] Pay streams open once NOAH has a market outside the chain, pay for hired work, and never for the same hours as
      seat income; the module holds them and pays month by month, and only governance cancels what has not elapsed.
- [ ] An ownership grant goes to escrow whole, charged to its funding step; one clock accrues its payments and the
      cap and bloc rule bound each send. Cancellation returns unaccrued pay and retains accrued pay under the caps.
- [ ] Before each step: contributor budget funded, public bonded stake at the live payment-based gate, no contributor
      over cap.
- [ ] Reward targets move only by a stated, bounded policy change.

---

## 9. Open items

- The member process admits by identity and vouch together. A verified identity is the gate, since only it enforces one
  wallet per person: a third-party verifier checks liveness and a document and returns a uniqueness attestation, and
  the platform keeps the attestation and a dedup token, never the document. A vouch from an existing member, founders
  vouching the first cohort, is the second signal and the lever: the vouch graph is what sybil detection reads, and a
  voucher whose vouchees are cancelled can be suspended by the same call. The process is the guard at admission, and
  what it finds within the year the stream takes back (§4). Still open: the verifier, the data it obliges the platform
  to protect, which joins the counsel question below, and who runs the process while the founders hold the block, and
  so who holds the registrar key. The registrar is a multisig, and the module's issuance window bounds a stolen key
  to at most a week's tenths, its term's suspensions voided by the vote that replaces it.
- The member app: a layer on a wallet, with signup, the identity check, and the vouch in front of it and the picker
  defaults §4 sets, hosted in the first-party wallet and open to any wallet through the kit. It and the wallet are the
  tooling dependency for the first member tranche, and none of it is built.
- What counts as NOAH having a market outside the chain, the trigger that opens pay streams (§4): a listing, an
  off-ramp for the currencies, or a depth figure governance reads.
- The band ranges are a first rubric. The first review round fixes the precedents, so the ranges are worth a second look
  against the actual first cases before that round.
- The native Disbursement module requires audit and a registrar appointment before the first tranche. Governance may
  return unaccrued contributor escrow on cancellation; accrued ownership pay remains under the caps. Founding
  identities come from genesis seat assembly, and compensation denominations require an explicit allowlist (D87).
- The gate ratio: 30% of distributed is the draft's staking assumption turned into a threshold; a lower ratio opens
  steps sooner and hands over later.
- When, if at all, to pull the reward-target lever for organic validators or for a stalled handover, and the runway cost
  governance accepts for it.
- Vetting criteria for the founding ten.
- What a member is, what they give in return, and what the identity attestations oblige the platform to protect are
  questions for counsel before the first tranche, not ones this plan answers.

Resolved by the launch genesis: the protocol funds are the four seeded accounts (§1); the seat grant is 5M,
vesting over ten years behind a four-year cliff (§2, D84). Decided in this draft: the 60/40 split, the 10,000
NOAH member grant with its rollover and its one-year schedule with a tenth on registration, streamed by the
module with the registrar's suspend and the governance cancel, the four-year, one-year-cliff schedule for
every contributor grant, the hire package with pay streams held and paid monthly by the disbursement module, deferred
until NOAH has a market outside the chain, and hires kept off the seats until then, the bloc rule without
exception, the cap as a fifth of the stake the person does not hold, one clock per ownership grant, live payment
caps, and a governance cancel that preserves accrued pay, with the grant's charge to the step it leaves the pool
in, the registrar's issuance window (§4), and admission by a verified identity and a vouch together, the member
app a layer on a wallet (§9). Removed: the ballast, its multisig, its withdrawal schedule, and the question of
moving it into a module (§3); the seat-admission path (D84); the question of locking member grants for good,
answered by the shorter schedule; and the platform's authorisations over member accounts, so a member's stake
moves only by their own signature.

**Assumptions:** the Cosmos SDK in `go.mod` (gov v1, multi-message proposals, the bank sends and community-pool
funding path, and typed Disbursement module governance messages), 21-day unbonding, and the
genesis governance params: 50% quorum, 66.7% threshold, 75% expedited, 33.4% veto, two-day voting ([genesis
§10](GENESIS.md#10-cosmos-sdk-modules)).
