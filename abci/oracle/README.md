# ABCI Oracle Processing

The oracle package owns the fixed oracle protocol used after proposal data reaches preblock. It decodes injected vote
extension data, aggregates validator reports, writes exchange rates, and updates validator accounting.

## Vote Extraction

`GetOracleVotes` reads the encoded extended commit info from the first injected proposal transaction, validates and
decodes each validator vote extension into domain rates, and returns one `Vote` per validator entry. Empty vote
extensions become empty reports; undecodable and semantically invalid payloads become invalid reports. Consensus grades
both exactly like an absent report — the validator still counts toward total commit power and still accrues attendance
eligibility on a functioning block — while telemetry keeps empty and invalid reports distinguishable. The authenticated extended commit itself is never
rewritten. A decoded, target-version-matched extension is marked as a valid report even when it contains only a subset of
the canonical targets.

`ValidateVoteExtension` parses the transport rates, enforces payload bounds, and checks the submitted target version and
denoms against the canonical target state required for its vote height. It returns decoded `VoteRate` values keyed by
canonical target index, so consensus aggregation does not repeat rate decoding or denom lookup. `ParseVoteExtension`
exposes the state-independent parsing step for historical inspection, where the matching target epoch is unavailable.

## Aggregation

### Ballot Construction And Raw Quorum

Aggregation receives lexically ordered canonical targets and already decoded, target-indexed rates. Each source ballot
stores its positive reports in validator order and also keeps a dense validator-indexed rate view for constant-time
overlap lookup. A nil dense rate means that the validator did not submit a positive report for that target.

Ballots are assembled in two passes. The first pass counts positive reports per target. The second fills an exactly
sized vote slice for each target ballot. This avoids geometric slice growth without reserving a full validator-sized
vote slice for every configured target.

Total commit power includes every validator entry, including empty, absent, and invalid oracle reports. Positive reports
contribute their validator's power to the corresponding raw ballot. Raw quorum is:

```text
threshold power = ceil(VoteThreshold * total commit power)
```

The ceiling prevents fractional power requirements from rounding down. A non-positive submitted rate is an abstention: it
adds no ballot power and earns no score weight. A target that does not reach raw quorum is simply left unpriced; no
separate unavailability signal exists, so an omitted target and an abstained target are equivalent.

### Participation And Attendance

There is no per-block fault accounting. A validator *participates* in a block when its valid report prices enough
distinct targets with positive rates to reach the participation floor:

```text
required rates = max(1, ceil(participation_threshold * canonical targets))
```

Abstentions, omissions, empty reports, and invalid reports never count toward the floor. Non-participation is not itself
penalised for the block, and reward scoring stays independent of it: a below-floor report still earns band-gated rewards
for the targets it does price.

`participation_threshold` is capped at 50% and defaults to 20%; zero restores the single-positive-rate floor. The cap is
the boundary between a deadman switch and a coverage mandate: coverage misses correlate through shared providers, so a
floor above half the target set would let a split-fleet provider gap grade the affected half absent while the healthy
half keeps blocks functioning. Coverage pressure belongs to band-gated rewards; the floor exists so that a hardcoded
token rate does not count as running an oracle.

A block is *functioning* when participating power reaches `functioning_block_threshold` of total commit power:

```text
functioning block = total commit power > 0 and
                    participating power >= ceil(functioning_block_threshold * total commit power)
```

Every commit validator of a functioning block accrues one eligible attendance unit, and participants also accrue one
attended unit. Grading only functioning blocks is what makes correlated outages judge no one: when most of the fleet
cannot price anything, the block grades nobody rather than penalising everybody. Below-floor reports contribute no
participating power, so the same protection covers coverage collapses: a fleet-wide provider gap that drags most power
under the floor turns grading off instead of grading everyone absent. The explicit positive-power guard keeps
a zero-power commit from trivially satisfying a zero threshold product.

The threshold is a governance parameter floored at 50% and defaulted to it. The floor is the load-bearing half: a dark
coalition holding more than the remaining share switches grading off entirely, so flooring at a majority forces that
coalition to be a majority itself, and no block is ever graded that a majority could not participate in. Governance may only
raise the threshold, trading deadman sensitivity for forgiveness of a partially degraded fleet — useful when a chronic
minority outage would otherwise grade every block against the same operators. It is deliberately not
`vote_threshold`: reusing the price-quorum parameter would let a coalition of `1 - vote_threshold` disable attendance
accounting by going dark, and would silently retune jailing whenever price quorum is tuned for price safety.

Attendance drives only the oracle module's periodic settlement, which jails — never slashes — validators whose attended
share of eligible blocks falls below `min_attendance_per_window`. Accuracy and coverage incentives come entirely from
band-gated rewards, not from this counter.

Every record is judged at settlement, however few eligible blocks it holds. `min_attendance_per_window` is the whole
grace: a ratio is scale-free, so a validator present for part of a window is held to the same share of the blocks it was
actually present for, and an eligible-block floor on top would silently soften the ratio governance set. Correlated
outages need no such floor, because non-functioning blocks never reach the record in the first place — which means a
sparse record is evidence the validator was absent while the fleet worked, not evidence that grading is unsafe.

Newly activated targets get no special grading: attendance is unconditional per window, by decision. Rollout slack comes
from layers that already exist — the sidecar prices scheduled targets throughout their pending window, pricing the
surviving targets keeps a validator above the participation floor unless one activation batch more than doubles the
target set (the worst case at the 50% threshold cap; the 20% default tolerates a 5x expansion), a majority unable to
participate grades nobody, and the windowed ratio leaves a lagging
operator most of a window to ship provider support. The deliberately accepted residual is a validator pricing nothing
for the better part of a window while a majority prices: it is jailed at settlement, which also restores quorum by
shrinking total power toward the capable share.

### Reference Selection

Only targets whose positive-report power meets raw quorum are considered passing. Each passing target is a possible
reference. Reference selection ranks candidates by:

1. Number of passing targets whose raw voter overlap with the reference meets quorum.
2. Sum of qualifying overlap power across those targets.
3. Raw positive-report power for the reference target.
4. Canonical target index, which is lexical denom order.

Selection scores raw overlap for every candidate. When all passing ballots contain the same validator and power sequence,
a shared-support fast path proves every pair's overlap without scanning each pair. Otherwise, pairwise overlap is
computed from the dense validator-indexed rates.

The strongest overlap score selects one reference, which is evaluated once. Its priced tallies are retained for scoring
rather than building its cross ballots again.

### Cross Rates And Weighted Medians

For a non-reference target, only validators with a positive report in both ballots contribute a cross rate:

```text
reference report / target report
```

Each cross rate is materialised immediately as a `LegacyDec`, so division follows the type's fixed 18-decimal
precision. Division is checked before calling panic-based `LegacyDec` arithmetic. A quotient that is unrepresentable or
rounds to zero is excluded from the cross ballot together with its voting power.

Both the raw target ballot and its overlap with the candidate reference must meet the same ceiling quorum. The lower
weighted median is the first rate whose cumulative sorted power reaches `ceil(ballot power / 2)`. Ballots are sorted by
rate and then validator index for deterministic ties.

The reference price is the lower weighted median of its source reports. Every other price is derived from the lower
weighted median of the overlapping validators' cross rates:

```text
target price = reference median / median(reference vote / target vote)
```

The final division uses the same checked `LegacyDec` semantics. A target is omitted when its final price is
unrepresentable, non-positive, or above `MaxExchangeRate` — the magnitude a direct report is held to by its
encoding. The reference median is itself a report, so only the quotient can leave that bound, and a price the
store's consumers cannot multiply a capped quantity by is omitted like one they cannot represent.

### Accuracy Scoring

Only priced tallies are accuracy-scored. A validator earns one reward target when its tally rate is inside the inclusive
symmetric band:

```text
absolute(tally ratio - median ratio) <= median ratio * RewardBand / 2
```

The implementation materialises the lower and upper `LegacyDec` endpoints with checked arithmetic and compares each rate
with them. A positive out-of-band report simply earns no reward target; it still counts toward the participation floor.
Targets that fail raw quorum, usable overlap quorum, final price conversion, or reward-band construction are not
accuracy-scored, and reporting them still counts toward the participation floor.

Each validator accumulates a count of rewarded targets. After all tallies are scored, its reward weight is computed once:

```text
reward weight = validator voting power * rewarded target count
```

This is equivalent to adding the same proposal-validated voting power once per rewarded target, but avoids repeated
arbitrary-precision additions.

### Implementation Optimisations

The implementation combines the protocol arithmetic above with optimisations that reduce repeated work and temporary
allocation while preserving those rules:

- Vote extraction decodes each rate once and replaces string-keyed aggregation lookups with canonical target indexes.
- Each target ballot owns a dense validator-indexed rate view for overlap lookup.
- A count-then-fill pass creates an exactly sized positive-vote slice for each target ballot.
- Shared voter support bypasses general pairwise overlap scans.
- Raw overlap scores select one reference before cross-rate evaluation.
- Winning cross ballots and medians flow directly into scoring instead of being rebuilt.
- Rewarded targets are counted before multiplying by validator power once.

The shared-support optimisation is data-shape dependent. Benchmarks cover identical and differing validator support and
sparse and fully reported target sets.

### Complexity

Let `V` be validator entries, `T` configured targets, `P` passing targets, and `R` positive reports:

- Ballot construction is `O(V + T*V + R)` because validator accounting is initialised, dense rate views are zeroed, and
  positive reports are counted and filled.
- General raw-overlap scoring is `O(P^2*V)`; identical voter support uses `O(P*V + P^2)` work.
- The selected reference is evaluated once in `O(P*V log V)` because each tally is sorted for its weighted median.
- Working memory is `O(T*V + P + R)` for dense rates, reference scores, and selected-reference vote storage.

The configured target limit bounds these costs. The benchmark suite includes the maximum-target cases so changes to the
algorithm or capacity are measured together.

## Price Application

`ProcessVoteExtensions` coordinates the preblock oracle writes and returns only an error:

1. Load the feed epoch for the previous height.
2. Decode and validate proposal vote data with `GetOracleVotes` against that epoch.
3. Load oracle params and aggregate rates and validator scores.
4. Write exchange rates with events in lexical denom order.
5. Record reward weight, block eligibility, and participation for every commit validator in commit-validator order.

The preblock package calls this function with its oracle keeper and handles the ABCI lifecycle around it.

## Encoding Boundary

`oracle_votes.go` enforces vote-extension rate cardinality, target membership, and the vote-rate size bound before
decoding each `LegacyDec` value through `pkg/encoding`. Vote rates are prices, so `MaxEncodedVoteRateBytes` sits far
below the state-level encoding bound, and the codec's wire and decoded limits derive from it: the codec admits exactly
the payloads this validation could accept, so wire padding — oversized rates, duplicate map keys, stored-block zlib —
buys an attacker nothing.

## Internal Values

Aggregation values live in this package because they are working state for the fixed oracle protocol:

- `aggregation.go` builds ballots, applies quorum, and computes prices and validator scores.
- `ballot.go` owns source/cross ballot representation, overlap lookup, and weighted median selection.
- `reference.go` owns overlap scoring, reference selection, and evaluation.
- `oracle_votes.go` owns proposal extraction and the decoded vote representation.
- `vote_processor.go` coordinates keeper reads, aggregation, state writes, and accounting.

The shared `abci/types` oracle keeper error lets preblock and vote-extension boundaries classify state-access and
mutation failures consistently.
