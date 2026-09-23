# NOAH Genesis & Distribution Plan

- Status: draft 2026-09-21, revised 2026-09-22 without the ballast, on the decided 5M seat, with open validator entry
  (D84), with the 60/40 member and contributor split, the 10,000 NOAH member grant, and four-year vesting, and after
  review with the cap measured against stake the person does not hold, escrow deposits charged to the step they leave
  the pool in, a governance cancel for unreleased escrow, and every grant, the founder's included, through the grant
  contract that issues members and holds the escrow, written and tested (`contracts/grant`, D86); then with the member
  grant a tenth on registration and the rest monthly over a year, pay streams deferred until NOAH has a market outside
  the chain, and the seat vesting over ten years behind a four-year cliff; then without any platform authorisation over
  member accounts, so a member's stake moves only by their own signature; then, 2026-09-23, with the member grant held
  and paid by the contract month by month rather than vested, so a member found to be a sybil loses what is unpaid, and
  with admission by a verified identity and a vouch together, the member app a layer on a wallet. A proposal, not launch
  behaviour.
  Every genesis figure below is the decided value in [Launch genesis](GENESIS.md); the plan adds what genesis leaves to
  governance: the split, the member grant, the contributor bands, the vesting schedules, the distribution steps, the
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
| Contributors | 312.8M, 40% | Vesting grants in five steps (§4)                           |

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
100 validators. No proposal grants a seat after launch (D84). A vesting grant can be bonded as a validator's own stake,
since unvested coins delegate like any other, so a contributor can register with what they were granted, and a member
with what they have been paid. Power
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

The community pool's 782M goes to two groups, in five steps, as grants the contract holds and pays out by a schedule.

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

**Instrument.** A contributor grant is a periodic vesting account, **four years, 25% at a twelve-month cliff, then
monthly**. A member grant is a stream the contract holds and pays to the member's address, **one year, a tenth on
registration, then monthly**: each month's coins arrive spendable, and what has not been paid stays in the contract,
where governance can cancel it (A faked member, below). A pay stream is the same instrument (Hires, below).
Unvested coins can be delegated and vote but cannot be transferred, and staking rewards on them arrive spendable, so
the only use of an unvested grant is to stake it (§6). A contributor schedule's length sets how much of the grant is
staked by default rather than by choice; a member schedule's sets how long a faked member has to go unfound; the gates
carry the handover (§5).

```text
start_time:    the block the contract creates the account or opens the stream in; the clock counts from it

contributor    period 1       length 31536000 s (365 d)    amount 25% of the grant
               periods 2–37   length 2628000 s (30.4 d)    amount 1/48 of the grant each; the last takes the remainder
member         period 1       length 1 s                   amount a tenth of the grant, sent at registration
               periods 2–13   length 2628000 s (30.4 d)    amount 3/40 of the grant each; the last takes the remainder
```

**Mechanism.** Every grant is created by the grant contract, `ark-grant` under
[contracts/grant](../../contracts/grant/README.md), which governance funds one tranche at a time by a
`MsgCommunityPoolSpend` to its address and instructs by `MsgSudoContract`; it has no admin, since a vote can sudo or
migrate any contract, and the pool still leaves only by that spend (D86). A member grant is opened when the registrar, a
key governance sets, registers the member's address: the contract sends the tenth at once, so the member's own account
pays its gas from the first block, and holds the rest on the schedule above from that block, paying each month to the
address once it has elapsed. Anyone may trigger the payment and it is cumulative, so a claim after six months pays six
months: the app makes it in the member's own claim-and-stake transaction, and anyone, the platform included, may make
it for a member who has not, which only moves the month into that member's own account. An address that already holds
coins is paid like any other. A contributor
proposal spends the grant plus one allowance per whole unit of it, the most tranches the cap can split it into; the
contract holds that as the grant's gas, grants each tranche account a fee allowance from it, since nothing in a tranche
is spendable until its cliff, and returns what is unused with a cancel or after the last tranche. It counts every
allowance it has promised and issues nothing from those coins, and since it cannot see one being spent, unallocated
reads low by what tranche accounts have spent on gas. No proposal carries a member address. The contract issues at most
250 members in any seven days, counted from each registration's block, a limit governance sets, so a stolen registrar
key pays out at most a week's tenths before an expedited vote replaces it, and the registrar is a multisig for the same
reason. A contributor grant is one `add_grant` in a proposal, which escrows the whole amount and pays what the rules
allow at once (below). A member record keeps the schedule it registered under, about 600 bytes, so the full member roll
is on the order of 30 MB of contract state before tree overhead. A tranche proposal passes like any other: half the
bonded stake voting, two thirds of the non-abstaining vote agreeing. At launch the ten founders are the only voters, so
proposals pass; later the public votes. The mechanism never changes, only who is voting. Every grant is on-chain, and
the contract's totals show what each tranche has issued, escrowed, paid, and still holds.

**A faked member** keeps what has been paid and loses the rest. The member process (§9) admits members and is where a
sybil is found, mostly after the fact, and the contract gives the finding somewhere to land. The registrar suspends the
member's pay, which moves nothing and which the registrar or a vote can undo; governance cancels it in a batched sudo,
which pays the months that had elapsed by the suspension, since anyone could have claimed them before it, and keeps
the rest in the tranche for the next member, never through the pool. Suspend sits with the registrar because it can
only delay; cancel sits with governance because a cancel followed by a registration would let one key redirect a grant.
A stolen registrar key can therefore pay out a week's tenths, pause every stream, and lift every suspension, and no
more. The proposal that replaces it voids that key's suspensions in one write, so the new registrar suspends afresh
only the genuine cases. A lifted suspension it cannot undo: the next release pays the lifted member the months since,
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
  block, and the contract grants members no allowance; no platform account funds gas. A contributor tranche, whose
  first period is a year, is the account that cannot pay, and its allowance comes from the gas its own proposal
  brings, as the mechanism above describes.
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
what lets a public vote judge work it did not witness. A founding-band grant lands across rounds rather than at once:
the cap allows 10M at genesis and more as the stake others hold and the work both grow, and the cumulative cap is
checked at each round. No round pays liquid NOAH for expenses, which vesting forecloses. The first round sets the
precedents every later grant is measured against in public, so it is decided with that in mind, and not spent on the
people nearest the founders because step 1's 20M reaches two people at the genesis cap.

**Hires.** The review rounds look back; a team brought in to build the tooling needs a stake in what it builds, and pay
it can spend. Until NOAH has a market outside the chain there is nothing to spend it on: spendable NOAH converts only
into the currencies, which have no off-ramp yet either, so a monthly stream would be pay with nowhere to go. **Before
that market exists a hire gets one grant, in the core band on the four-year schedule, with the timing multiplier as the
pay for the risk.** Once it exists a hire gets two grants in the same proposal, of different kinds. A **pay stream**:
twenty-four monthly periods of a twenty-fourth each, sized at the design's skilled-year rate of about 131,000 NOAH a
year, which the grant contract holds and pays to the hire month by month as each elapses. Each month's pay is spendable
on arrival, and the unpaid balance stays in the contract's escrow, where governance can cancel it if the hire leaves or
the work stops: a cancel holds the months that have elapsed for the hire, who releases them as before, and returns the
rest to the pool, so no payee can fail it. The unpaid balance does not stake, which at pay scale moves no gate. An
**ownership grant** in the core band on the four-year schedule, with its timing multiplier. A team of six for two years
is about 1.6M in pay and, at six core-band grants, 12M to 60M in ownership; the streams start at once and the ownership
lands as the step tranches admit it. The stream pays for the hired work: a hire who also holds a seat draws seat income
for the seat and the stream for the work, never both for the same hours. Before streams open, a hire who holds a seat
has nothing until the bloc rule admits their ownership, so the team and the seats are kept separate (§9).

**Founding validators.** A seat holder receives no grant for validator work; the seat, which vests to them over ten
years, and its income are that pay. Other work, before launch or after, is granted like anyone's, in its band, with the
grantee abstaining from the vote. What
bounds it is the bloc: every grant to one of the ten adds to the founders' combined stake, and the block line in §5 is
twice that stake, so an early grant would move the handover out of the public's reach. The rule: **a grant to anyone
holding a founding seat is made only when public bonded stake is at least twice the founders' combined stake after the
grant.** Pay streams are exempt: the contract holds them, so nothing in a stream is bonded, and the pay is far under
any cap. A seat holder also stays within the cap, seat and grants together, so the bloc rule binds while public stake
is under about 180M and the cap binds above it: seat and grant together reach the goal's sixth at the top of the
founder's grant, 65M of 390M, and fall from there. The founder's own founding-band grant, the whole chain before launch
at the top of the band, takes no exception: it is placed in its band in round one and escrows whole once a tranche has
room for it, and the two rules give:

| Public bonded stake | Total bonded stake, what the escrow reads | Founder's grant, cumulative | Binding rule |
| ------------------- | ----------------------------------------- | --------------------------- | ------------ |
| up to 100M          | up to 150M                                | nothing, escrowed           | bloc         |
| 110M                | 165M                                      | 5M                          | bloc         |
| 120M                | 180M                                      | 10M                         | bloc         |
| 150M                | 225M                                      | 25M                         | bloc         |
| 200M                | 294M                                      | 44M                         | cap          |
| 280M and above      | 390M and above                            | 60M                         | cap          |

It reaches the ceiling at about a 40% staking rate, after the handover rather than during it. The grant is escrowed
whole in the first step whose contributor tranche has room for it, step 3 at the earliest, which loses nothing: the
bloc rule releases none of it before public stake reaches 100M, past step 4's gate. Grants to the other nine queue
behind it under the same rules: with the founder at 60M the bloc is 110M, and nine further grants of 5M need 310M of
public bonded stake. Hires are kept off the seats, so their ownership is granted at once and the bloc stays at 50M; a
hire who held a seat would wait on that.

**Escrow for tranched grants.** A grant larger than its first tranche, a founding-band grant or any grant to a seat
holder, is not a series of promises. The whole amount leaves the pool in one proposal, in a step whose contributor
tranche has room for it, and sits in the grant contract's escrow, the same contract that issues member grants, until the
rules release it. The proposal spends the grant and its gas, one allowance per whole unit, to the contract and, as the
gov account, executes its add-grant call naming the grantee, the amount, whether they hold a seat, and, for a grantee
whose address already holds an account, the fresh address of the first tranche. Anyone can call release. The escrow
reads total bonded stake `T` from the staking pool query and releases, in whole millions, whatever the rules allow:

- for every grantee, cumulative paid and released, the 5M seat included for a seat holder, is `own`, and stays within
  `(T − own) / 5`, a fifth of the stake the person does not hold; cumulative ownership grants alone stay within
  60M, so a seat holder's total ceiling is 65M including the seat;
- for seat holders together, cumulative paid and released stays within `T/3 − 50M`, a third of total bonded stake less
  the founders' 50M, which is the bloc rule restated against the number the contract can read, shared pro rata by what
  remains in escrow.

Each release is a fresh periodic vesting account on the four-year schedule, created by the escrow at the next unused
address the grantee has registered with it, so a released tranche stakes and votes like any other grant and cannot be
sold. Escrowed coins are never bonded and never vote: they sit outside the founders' bloc and outside public bonded
stake alike, so an escrowed grant moves no gate and no handover line until it is released. Governance adds grants and,
by the same vote, may cancel what is unreleased, which returns to the community pool: the grant was for work reviewed in
public, and the vote that made it can unmake what has not yet released, for fraud found later or a key lost, while
released coins are the grantee's. The escrow has no admin and no vote handler. A grantee's registered key can replace
its unreleased addresses and nothing else. A 30M founding-band grant to a contributor without a seat, made in step 2
where the tranche has room for it while the stake others hold is still 50M, runs: 10M paid at once under the cap, 20M
escrowed, released in whole millions as that stake grows, 10M of it by the time it reaches 100M and the last 10M at
150M. The staking pool query it reads is on the contract accept list (D85).

| Step | Cumulative distributed | Members, cumulative | Members, count | Contributors, cumulative | Gate: public bonded stake before the step opens |
| ---- | ---------------------- | ------------------- | -------------- | ------------------------ | ----------------------------------------------- |
| 1    | 50M                    | 30M                 | 3,000          | 20M                      | none                                            |
| 2    | 150M                   | 90M                 | 9,000          | 60M                      | 15M                                             |
| 3    | 300M                   | 180M                | 18,000         | 120M                     | 45M                                             |
| 4    | 450M                   | 270M                | 27,000         | 180M                     | 90M                                             |
| 5    | 782M                   | 469.2M              | ~46,900        | 312.8M                   | 135M                                            |

Each step carries both groups at 60/40: member tranches as rolling proposals, contributor grants as the work is done.
Admitting members is slower than paying for work, so a step's member tranche never holds the next step: whatever is
unpaid when the contributor tranche and the gate are met rolls into the next step's member tranche, and the 60/40 holds
over the whole distribution rather than inside each step. The member pool waits for members; it is never reallocated to
contributors. A step is paid out when its contributor tranche is. **Distributed**, for the tranches and the 60/40, is
what has left the pool: a paid grant and an escrow deposit alike, charged whole to the step in which the proposal
executes, so a grant the open tranche cannot hold waits for the next step, escrow deposits included, and a cancelled
escrow balance returns to the pool and to the tranche it was charged to. For the gates it is what can be staked: paid
grants and escrow releases, since escrowed coins can neither stake nor vote; the gates in the table assume nothing is in
escrow, and with coins escrowed and unreleased the gate is 30% of what has been paid or released, which is lower. Member
escrow is the ordinary case, since nine tenths of each grant waits in the contract and pays out over the member's first
year, so a gate reads what members have been paid, not what they were granted.
**Public bonded stake** is everything bonded except the founders' combined stake, read as `arkd query staking pool` less
50M, and less what the contract has released to seat holders (§4). Each gate is 30% of what has been distributed so far,
so a step opens only once the previous one is being staked, not merely held. Step 5 is whatever the pool holds after
steps 1 to 4: 782M with ten founding seats, and more if governance returns idle subsidy through `MsgReturnSubsidy`
(D83).

**Cap per contributor** = the most any single person may hold from the pool in total, seat and grants together. It is
measured, not scheduled: **a fifth of the bonded stake the person does not hold, total bonded stake less their own, at
the time of the grant, rounded down to the nearest million. Cumulative ownership grants are capped separately at
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

Example: at genesis the first grantee can receive 10M and a second the tranche's remaining 10M, but not 20M to one
person. Someone who received 10M can receive more once the stake others hold has grown to admit it; their own bonding
never counts. The cap is a rule for the grant process; the chain cannot enforce it, but the number it depends on is
on-chain and anyone can check a grant against it. A member grant never approaches the cap; the member risk is the
opposite one, one person holding many wallets, and the member process in §9 guards it at admission, the stream after.

**Grant rules:**

- One wallet per person. Contributors are identified; members are admitted by the member process (§9).
- Member addresses come from the app at signup. The platform holds no member coins and no authorisation over a member
  account.
- Every grant follows its group's schedule above, created by the grant contract: a contributor's as a vesting account
  at a fresh address, a member's as a stream to the address the member registered.
- Cumulative contributor cap checked against the bonded stake the person does not hold before every grant.
- Every contributor grant is placed in a band, with its multipliers and reasoning in the proposal summary.
- No grants to founding validators for validator work; any other grant to a seat holder waits for the bloc rule, with
  the grantee abstaining.

**Gate before moving to the next step — all three must hold:**

1. The previous step's contributor tranche is fully paid out; its unpaid member remainder rolls forward.
2. Public bonded stake is at or above the step's gate.
3. No contributor is over the cap.

---

## 5. Handover

Only bonded stake votes. The founders' 50M is self-delegated at genesis and unvested for four years, so it stays bonded
unless a founder undelegates; a grantee's weight is what they have delegated. Three lines follow from the 66.7%
threshold, all in public bonded stake:

| Public bonded stake | Founders' own stake alone can | Crossed when                                                |
| ------------------- | ----------------------------- | ----------------------------------------------------------- |
| below 25M           | pass any proposal             | 50% of step 1 is bonded; step 3's gate of 45M guarantees it |
| 25M to 100M         | block, not pass               | steps 2 to 4 bond, under their gates                        |
| above 100M          | neither                       | step 4 fills; step 5's gate of 135M guarantees it           |

The lines are crossed by grantees bonding what they receive, and by nothing else: no seat is granted after launch, so
the founders can neither shortcut their own handover nor delay it, and grants to the ten are timed by the bloc rule in
§4, so none of them moves these lines back across the public. Vesting pushes the same way: a contributor grant is wholly
unvested for a year and mostly for four, and an unvested coin can only be staked, so the gates should be crossed by
delegation rather than waited for; a member grant pays out over its first year, and what is unpaid neither stakes nor
votes, so member stake arrives as members are paid and choose to bond it. The schedules do not hold the earliest
grants bonded until the lines are crossed, since at the pace §7 assumes step 4 is some years off; the gates do, because
a step opens only on bonded stake and the lines are read live.

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

With contributor grants unvested for a year and mostly for four, the expectation is a rate above this table in the first
years; the table shows the founders' share even if it is not. The
ten founding validators own 5.3% of supply. Members own 46.9% and contributors 31.3%, and
together they control the community pool, the validator set, and the protocol funds' policy through governance. Founding
validators will likely remain among the largest validators through public delegations, but that power is borrowed, and
any delegator can move it.

---

## 6. If the public does not stake

Nothing pays people to stake. There is no inflation (D1); the reward pot is the fixed floors plus tax and gas revenue:
ten seats' 1.3M NOAH a year is 2.6% on the founders' 50M before commission and falls under 1% once bonded stake passes
about 130M. Vesting removes the reason not to stake, since an unvested grant has no other use, but it does not delegate
by itself: the grantee sends the delegation. The gates in §4 slow distribution when that lags, which is the safe
direction, and two levers exist:

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
  stream lets a member found within the year lose what is unpaid.
- The contributor bands are ownership-scale because the pool is: the cap rule already assumes 10M grants at genesis, and
  pay-scale grants would leave most of 312.8M unspent.
- Four years with a one-year cliff for contributors because it is the standard schedule for compensation. One year for
  members, a tenth on registration, because 10,000 NOAH over four is thin and a tenth on the day is real to a person
  joining. A member grant is streamed rather than vested because a vesting account has no clawback, so a faked member
  found after registration would still collect the whole grant, and the stream is what makes the find worth anything.
  The NOAH paid each month is the registration rate times the grant at any length, so the length moves neither the
  sell flow nor the handover, which rides on the contributors' pool and the gates (§5); it sets how long a faked member
  has to go unfound, so a longer tail is the lever if the member process proves porous. Ten years behind a four-year
  cliff for the seat
  because it is pay for a decade of running the chain, and the cliff keeps the floats the only liquid NOAH a founder
  holds through the launch years.
- The bloc rule because the block line is twice the founders' combined stake: a grant to a seat holder made before the
  public holds twice the bloc moves the line out past the public, which is the ten-million seat by another door.
- The gates are 30% of everything distributed so far, so the handover lines in §5 are crossed by steps 2 to 4 without
  assuming a staking rate beyond what the gate itself demands.
- The founders keep the block through step 3 and lose it during step 4, once public bonded stake passes 100M, and before
  step 5 can open. That is the relinquishment curve, and it is all visible on-chain.

---

## 8. Standing rules (checklist)

- [ ] Every grant is created by the grant contract from tranches governance spends to it, on its group's schedule: a
      contributor's as a vesting account at a fresh address, a member's as a stream to the registered address.
- [ ] The registrar is a multisig, and the contract's issuance window stays at 250 members in any seven days unless
      governance changes it.
- [ ] The registrar suspends or resumes a member's pay and nothing more; only governance cancels a member, paying
      what had elapsed by the suspension and keeping the rest in the tranche, within the month of the suspension,
      since a resumed member is paid the months since.
- [ ] Members and contributors at 60/40 over the whole distribution; a step's unpaid member tranche rolls forward.
- [ ] Cumulative contributor cap is checked against the bonded stake the person does not hold before every grant.
- [ ] Each contributor grant names its band, its multipliers, and its reasoning in the proposal summary.
- [ ] One wallet per person: contributors identified, members admitted by a verified identity and a vouch; the
      platform keeps attestations, never documents.
- [ ] The member platform holds no coins and no authorisation over any member account; the app's picker defaults away
      from the founding ten, and the app shows the member every open proposal.
- [ ] No grants to seat holders for validator work; other grants to them wait for the bloc rule, grantee abstaining.
- [ ] Pay streams open once NOAH has a market outside the chain, pay for hired work, and never for the same hours as
      seat income; the contract holds them and pays month by month, and only governance cancels what has not elapsed.
- [ ] A tranched grant goes to the escrow whole, charged to the step it leaves the pool in; the escrow releases by the
      cap and the bloc rule from bonded stake, and only governance cancels what is unreleased, back to the pool.
- [ ] Before each step: contributor tranche paid out, public bonded stake at the gate, no contributor over cap.
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
  so who holds the registrar key. The registrar is a multisig, and the contract's issuance window bounds a stolen key
  to at most a week's tenths, its suspensions voided by the vote that replaces it.
- The member app: a layer on a wallet, with signup, the identity check, and the vouch in front of it and the picker
  defaults §4 sets, hosted in the first-party wallet and open to any wallet through the kit. It and the wallet are the
  tooling dependency for the first member tranche, and none of it is built.
- What counts as NOAH having a market outside the chain, the trigger that opens pay streams (§4): a listing, an
  off-ramp for the currencies, or a depth figure governance reads.
- The band ranges are a first rubric. The first review round fixes the precedents, so the ranges are worth a second look
  against the actual first cases before that round.
- The grant contract is written and tested; its audit, the optimizer build whose checksum the store proposal cites, and
  the registrar's holder remain before the first tranche. Its escrow has the governance cancel as its only exit besides
  release, and the query it reads is on the contract accept list (D85).
- The gate ratio: 30% of distributed is the draft's staking assumption turned into a threshold; a lower ratio opens
  steps sooner and hands over later.
- When, if at all, to pull the reward-target lever for organic validators or for a stalled handover, and the runway cost
  governance accepts for it.
- Vetting criteria for the founding ten.
- What a member is, what they give in return, and what the identity attestations oblige the platform to protect are
  questions for counsel before the first tranche, not ones this plan answers.

Resolved by the launch genesis: the protocol funds are the four seeded accounts (§1); the seat grant is 5M, vesting
over ten years behind a four-year cliff (§2, D84). Decided in this draft: the 60/40 split, the 10,000 NOAH member grant
with its rollover and its one-year schedule with a tenth on registration, streamed by the contract with the registrar's
suspend and the governance cancel, the four-year, one-year-cliff schedule for
every contributor grant, the hire package with pay streams held and paid monthly by the grant contract, deferred until
NOAH has a market outside the chain, and hires kept off the seats until then, the bloc rule without exception, the cap
as a fifth of the stake the person does not hold, the escrow for tranched grants with its governance cancel and its
charge to the step it leaves the pool in, the registrar's issuance window (§4), and admission by a verified identity and
a vouch together, the member app a layer on a wallet (§9). Removed: the ballast, its multisig, its withdrawal schedule,
and the question of moving it into a module (§3); the seat-admission path (D84); the question of locking member grants
for good, answered by the shorter schedule; and the platform's authorisations over member accounts, so a member's stake
moves only by their own signature.

**Assumptions:** the Cosmos SDK in `go.mod` (gov v1, multi-message proposals, the `x/vesting` account messages the
contract dispatches, wasmd's governance policy over contracts), 21-day unbonding, and the genesis governance params: 50%
quorum, 66.7% threshold, 75% expedited, 33.4% veto, two-day voting ([genesis §10](GENESIS.md#10-cosmos-sdk-modules)).
