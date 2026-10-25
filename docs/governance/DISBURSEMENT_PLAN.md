# NOAH Genesis & Disbursement Plan

- Status: the allocation, the founder's awards, and the grant rules are launch behaviour, with figures from
  [Launch genesis](GENESIS.md) and rules from the [Disbursement module](../../x/disbursement/README.md). The steps
  and proposal rules in §4 are governance policy the chain does not enforce.

**Goal:** hand the chain to its users. The founding validators hold every vote at launch and lose it as the
disbursement pools are paid and staked. No party should hold ⅓ of bonded stake, which halts the chain and blocks any
proposal at the 66.7% threshold. The founder's seat is the one exception: it is a third of the founding stake, and
public stake dilutes it from the first delegation (§5).

---

## 1. Supply at genesis

Initial supply is **1,000M NOAH**. Figures assume ten seats. A launch admits seven to thirteen seats, each fixed at 5M.

| Account            | NOAH                                                                | Exit                                                                                      |
| ------------------ | ------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| Protocol funds     | 115M: subsidy 50M, Reserve 50M, Redemption Buffer 10M, Insurance 5M | Each module account is spent only under its module's rules                                |
| Disbursement pools | 782M: members 469.2M, contributors 312.8M                           | The member pool has none. The contributor pool returns to the community pool only by `MsgReturnUnallocated` |
| Founding seats     | 68M: nine of 5.05M, the founder's 22.55M                            | Vesting accounts, plus a 50k liquid float each (§2)                                       |
| Community pool     | 35M                                                                 | Governance proposal                                                                       |

The seats come from the community pool. The founder's two awards, 25M in all, are reserved in the contributor pool at
genesis (§4).

## 2. Validators

- **Seats.** Each founding seat is 5M NOAH vesting from year four to year eight, plus a 50k liquid float. The founder's
  seat is half the other seats' sum, which makes it a third of the founding stake at any count (22.5M at ten).
  Unvested coins can be delegated, vote, and be slashed, but cannot be transferred.
- **Spread.** Each validator self-delegates one seat in its gentx. In the first blocks the founder delegates the rest
  of the founder's seat across the other validators, so no node holds a third of consensus power
  ([genesis §14](GENESIS.md#assembly-sequence)).
- **No validator grants.** Seat holders receive no grant for validator work. Other work is granted as anyone's would
  be (§4). After launch, anyone can bond NOAH and register as a validator, up to 100 validators with a 5% minimum
  commission. Nothing comes from the pools.
- **Income.** Each seat adds 0.025 NOAH a block to the reward targets, about 131,400 NOAH a year, split 70/30 between
  the validator and oracle lanes. Both lanes pay by bonded stake. At ten seats and 5% commission, the founder's seat
  earns about 421,000 a year and each other seat about 99,000. Only `MsgUpdatePolicy` moves the targets: the economic
  committee within half to twice the launch values, governance beyond that. If public bonded stake runs under the
  fifth of paid grants the reward figures assume (§5), raising them for a bounded period is the lever, at a cost to
  the subsidy pool.

## 3. Grants

The [Disbursement module](../../x/disbursement/README.md) holds both pools from block 1. It escrows every grant in full
when the grant is made, and pays it at EndBlock as the schedule falls due. Unpaid escrow cannot stake or vote. Each
kind's schedule is a governance parameter, copied onto the grant when it is made.

| Kind         | Pool         | Made by                                                     | Launch schedule                                                                | Cap                                                    |
| ------------ | ------------ | ----------------------------------------------------------- | ------------------------------------------------------------------------------ | ------------------------------------------------------ |
| Member       | Members      | The registrar, within the open tranche                      | `member_amount` (20,000 NOAH): a tenth after a two-week hold, then 13 payments four weeks apart | None                                           |
| Ownership    | Contributors | A governance proposal                                       | A quarter after a 364-day cliff, then 78 fortnightly payments                  | `ownership_cap`: 15M NOAH a record                     |
| Compensation | Contributors | A governance proposal, or the committee within its allowance | Fortnightly for the number of periods the award sets, at most 260; a bounty is one period | `compensation_cap`: 1M reference units a record a year |

- **Clocks.** An ownership clock can't start before its award. A compensation clock can start up to 13 weeks before
  it. Nothing pays until one period after the award, and every first period is at least two weeks.
- **Ownership cap.** A record's committed ownership, meaning what all its ownership grants have paid and still owe,
  cannot exceed the cap. The cap is checked at award, so every grant pays in full. The seat doesn't count toward it.
  The cap binds a beneficiary record, not a person.
- **Compensation cap.** Awards are valued in the reference unit when made. A stream counts at its yearly rate while it
  runs, a backdated award counts its arrears in its first year, and a cancelled award still counts for a year. A
  stablecoin award converts contributor NOAH through Market when it is made, offering at most its `max_offer`, so
  stablecoin awards wait until conversion opens.
- **Authority.** Governance sets params, opens member tranches with `MsgOpenTranche` (this moves no coins), makes
  contributor awards, cancels grants, and appoints the grants committee, a multisig, for a term. The committee
  appoints the registrar, a single key, for a window inside the committee's own term. The committee can also suspend
  and reinstate members and award compensation within its term's allowance. It cannot cancel a grant or change a
  payee. Any committee appointment clears the registrar.
- **Fake members.** A suspension stops a member's pay and moves nothing. A governance cancel keeps what accrued up to
  the suspension for the payee and returns the rest to the tranche. A fake found within the two-week hold costs
  nothing. The [committee runbook](COMMITTEE_RUNBOOK.md#8-grants) covers recovering from a stolen key.

## 4. Distribution

### Founder's awards

At genesis, seat assembly writes two governance awards to the founder from the contributor pool, both clocked from
genesis. Every founding validator approves them by signing a gentx against the published file.

- Grant 1: **15M NOAH of ownership** for the work before launch, on the ownership schedule. This fills the ownership
  cap.
- Grant 2: **10M NOAH of compensation** for the work after launch, paid over 260 fortnights and valued at the
  bootstrap price of one XDR a NOAH. This fills the founder's compensation cap for its ten years.

Both grants pay, can be cancelled, and count against the caps like any other grant.

### Steps

| Step | Distributed | Members | Member amount | Member count | Contributors |
| ---- | ----------: | ------: | ------------: | -----------: | -----------: |
| 1    | 50M         | 30M     | 20,000        | 1,500        | 20M          |
| 2    | 150M        | 90M     | 15,000        | 5,500        | 60M          |
| 3    | 300M        | 180M    | 10,000        | 14,500       | 120M         |
| 4    | 450M        | 270M    | 7,500         | 26,500       | 180M         |
| 5    | 782M        | 469.2M  | 5,000         | ~66,300      | 312.8M       |

All figures except the member amount are cumulative.

- **A step is a headcount.** Step n runs from its predecessor's member count to its own, at its amount, so the step
  number says how many members the chain has. Nothing else conditions a step: no date, no stake figure, and no
  contributor budget spent. A step opens with a proposal that pairs `MsgOpenTranche` with the step's `member_amount`,
  proposed as the open tranche nears exhaustion; anything still open is consumed at the new amount. A member keeps the
  amount and schedule they registered under.
- **Pace is admissions.** The registrar registers as fast as verified admissions clear, and opening moves no coins, so
  an open tranche nobody fills costs nothing and a step that fills in a month opens the next in a month. The plan sets
  no maximum pace.
- **The amount is the vote's.** Each step's first proposal sets the amount by one rule: the whole above the cost of
  joining, an identity check, a voucher, and a wallet, under which it reads as an airdrop; and the tenth, the part no
  finding after the hold recovers, under the price of a bought identity at the oracle's NOAH rate. Step 1 runs at the
  artefact's 20,000 with nothing priced and a founder-vouched cohort. Step 2 waits for the oracle to read a live NOAH
  price, the reading that also opens conversion, since its amount cannot be checked without one. The amounts in the
  table are the plan's expectation; each vote applies the rule.
- **The fake rate is the step's check.** Each step's first proposal states the previous step's registrations,
  suspensions, and cancellations from the journal, and what admissions changed if the rate warrants it. A step whose
  predecessor's fakes have no answer waits for the admissions fix. This is the one condition that holds a step.
- **The tranche is the exposure.** The open tranche is all a stolen registrar key commits, and a fake found within the
  two-week hold costs nothing, so a tranche is sized to the loss governance accepts should a stolen key go unnoticed
  through a hold. The launch default opens each step's member budget in quarters, 7.5M at step 1, so twenty votes cover
  the distribution; a quarter grows with the step, from 375 grants to nearly 10,000, and governance that wants the
  exposure flat splits the later tranches at the cost of more votes.
- **Contributors.** The chain records only the member tranche. The contributor figures are a guideline: a round goes
  over if its proposal says why, and need not be spent before the next step opens. The founder's 25M is charged to
  step 1's 20M contributor budget, the overrun the guideline allows. Anything either pool doesn't allocate rolls into
  the next step. The 60/40 split holds over the whole distribution.

### Contributor proposals

- Each grant is one `MsgGrantOwnership` or `MsgCompensate`. The summary states the work, the period it covers, and why
  the amount is right compared with the founder's grant and the grants already made. The grantee abstains.
- **Hires** get a pay stream and an ownership grant in the same proposal. The stream is compensation sized near the
  per-seat 131,400 NOAH a year. It never pays for hours that seat income already pays.
- **Bounties** are one-period compensation, from the committee or by proposal.

## 5. Handover

Once the founder's seat is spread, the founder holds a third of the founding stake and the other seats hold two
thirds. At the 66.7% threshold and 50% quorum ([genesis §11](GENESIS.md#11-cosmos-sdk-modules)), the founder can
block a proposal alone but cannot pass one alone. Public bonded stake erodes that block:

| Coalition                                  | Blocks until public bonded stake reaches              |
| ------------------------------------------ | ----------------------------------------------------- |
| The founder alone, or half the other seats | About 0.3% of the founder's seat (68k NOAH at ten)    |
| The founder plus m other seats             | 15M × m                                               |

As the founder's grant and stream pay out, they extend the founder's block: the founder can block alone while public
bonded stake is under twice what they have paid. They also lift the founder towards quorum: once the founder's bonded
awards exceed the founder's seat plus public bonded stake, which with all 25M paid and bonded takes public bonded stake
under 2.5M, the founder alone meets quorum and passes any proposal no other stake votes against; with the other seats
voting no, the founder's 51.35% stays under the 66.7% threshold. "Cannot pass one alone" holds at launch, not for the
whole payout. A delegation that doesn't vote follows its validator's vote, so control passes only as grantees vote or
delegate away from the founding set.

**Public bonded stake** is the handover's measure and never a step's condition. It is the bonded total from
`arkd query staking pool` minus what the founding accounts have delegated: their seats, and whatever of their income and
the founder's paid awards they have bonded, which `arkd query staking delegations <address>` gives per account.
Subtracting what the awards have paid would count unbonded NOAH against the public and could go negative. Each step's
proposal reports it beside the headcount. The genesis reward figures are sized for a fifth of paid grants bonded, the
rate a chain paying what Ark pays falls back to in a bad year; when public bonded stake runs under that, the reward
targets are the margin (§2), not the distribution.

## 6. Open items

- The member process: a third-party identity check that returns a uniqueness attestation, plus a vouch from an
  existing member. Still open: which verifier, who runs it, and who holds the committee and registrar keys
  ([tooling direction §7](../direction/TOOLING_DIRECTION.md#7-run-the-member-backend-without-custody)).
- Who vouches for the first members: a vouch comes from an existing member, and the first cohort has none.
- The Ark app, which holds the wallet and the member features
  ([tooling direction §6](../direction/TOOLING_DIRECTION.md#6-make-one-first-party-ark-app-the-reference-experience)).
  It is needed for the first member tranche and is not built.
- An audit of the Disbursement module before launch, since it holds the pools from block 1.
- Vetting criteria for the founding set.
- Counsel on how a member grant is characterised; on what the uniqueness attestations and dedup tokens oblige the
  platform to protect; and, before the identity vault is built, on what holding identity data obliges the backend to
  protect, retain, and report.
