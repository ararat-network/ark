# ABCI oracle processing

This package implements extraction, aggregation, scoring, and price application for the fixed oracle protocol.
This README owns quorum, participation, reference selection, median arithmetic, and reward weights. [The ABCI guide](../README.md) owns hook ordering; [x/oracle](../../x/oracle/README.md) owns persistent state.


[Vote extraction](#vote-extraction) · [Participation](#participation-and-functioning-blocks) · [Aggregation](#report-aggregation) · [Implementation](#ballot-representation) · [Price application](#price-application) · [Verification](#verification)

## Vote extraction

`GetOracleVotes` reads the encoded extended commit info from the first injected proposal transaction, validates and
decodes each validator vote extension into domain rates, and returns one `Vote` per validator entry. Empty vote
extensions become empty reports; undecodable and semantically invalid payloads become invalid reports. Telemetry keeps empty and invalid reports distinguishable; their treatment is specified below. The authenticated extended commit itself is never
rewritten. A decoded, target-version-matched extension is marked as a valid report even when it contains only a subset of
the canonical targets.

`ValidateVoteExtension` parses the transport rates, enforces payload bounds, and checks the submitted target version and
denoms against the canonical target state required for its vote height. It returns decoded `VoteRate` values keyed by
canonical target index, so consensus aggregation does not repeat rate decoding or denom lookup. `ParseVoteExtension`
exposes the state-independent parsing step for historical inspection, where the matching target epoch is unavailable.

## Participation and functioning blocks

There is no per-block fault accounting. A validator *participates* in a block when its valid report prices enough
distinct targets with positive rates to reach the participation floor:

```text
required rates = max(1, ceil(participation_threshold * canonical targets))
```

Omissions, empty reports, and invalid reports never count toward the floor. Non-participation is not itself
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

Attendance counters and jailing settle in [x/oracle](../../x/oracle/README.md#11-attendance-settlement).

## Report aggregation

### Report validity

An authenticated extended commit is not rewritten to remove missing or malformed oracle reports. Empty reports and
invalid decoded payloads still leave the validator in total commit power. A valid report matches the vote height's exact
target version and may contain only a subset of canonical targets. Invalid or missing reports earn no report participation
or reward weight; attendance eligibility still depends on the functioning-block gate.

[Vote-extension handlers](../README.md#vote-extension-handlers) define the reporting and verification failure policy.

### Raw quorum

Total commit power includes every validator entry, including empty, absent, and invalid oracle reports. Positive reports
contribute their validator's power to the corresponding raw ballot. Raw quorum is:

```text
threshold power = ceil(VoteThreshold * total commit power)
```

The ceiling prevents fractional power requirements from rounding down. Only positive rates contribute ballot power or score weight. The compact wire decoder accepts positive values;
omission is the wire-level abstention, while defensive aggregation also excludes non-positive values. A target that does not reach raw quorum is simply left unpriced; no
separate unavailability signal exists, so an omitted target and an abstained target are equivalent.

### Reference Selection

Only targets whose positive-report power meets raw quorum are considered passing. Each passing target is a possible
reference. Reference selection ranks candidates by:

1. Number of passing targets whose raw voter overlap with the reference meets quorum.
2. Sum of qualifying overlap power across those targets.
3. Raw positive-report power for the reference target.
4. Canonical target index, which is lexical denom order.

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

## Ballot representation

Canonical targets arrive in lexical order, with each report already decoded into target-indexed rates. Source ballots
hold positive votes in validator order and a dense validator-indexed view for overlap lookup. Count-then-fill construction
sizes the positive-vote slices exactly. When ballots share the same voter/power sequence, the shared-support path avoids
repeated pairwise scans. The winning reference's cross ballots and medians are retained for scoring.

## Implementation optimisations

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

## Price application

`ProcessVoteExtensions` coordinates the preblock oracle writes and returns only an error:

1. Load the feed epoch for the previous height.
2. Decode and validate proposal vote data with `GetOracleVotes` against that epoch.
3. Load oracle params and aggregate rates and validator scores.
4. Write exchange rates with events in lexical denom order.
5. Record reward weight, block eligibility, and participation for every commit validator in commit-validator order.

The preblock package calls this function before feed promotion. A target omitted by the tally receives no new rate;
consumers apply the keeper's freshness policy to its stored rate. Reward funding and payout are specified in
[economic design](../../docs/ECONOMIC_DESIGN.md#9-validator-and-oracle-funding).

## Encoding boundary

`oracle_votes.go` enforces vote-extension rate cardinality, target membership, and the vote-rate size bound before
decoding each `LegacyDec` value through `pkg/encoding`. Vote rates are prices, so `MaxEncodedVoteRateBytes` sits far
below the state-level encoding bound, and the codec's wire and decoded limits derive from it: the codec admits exactly
the payloads this validation could accept, so wire padding — oversized rates, duplicate map keys, stored-block zlib —
buys an attacker nothing.

Aggregate wire/decompression bounds belong to [abci/codec](../codec/codec.go); per-value codecs belong to
[pkg/encoding](../../pkg/encoding/legacy_dec.go). These are separate from feed cardinality and `MaxExchangeRate`.

## Internal values

Aggregation values live in this package because they are working state for the fixed oracle protocol:

- `aggregation.go` builds ballots, applies quorum, and computes prices and validator scores.
- `ballot.go` owns source/cross ballot representation, overlap lookup, and weighted median selection.
- `reference.go` owns overlap scoring, reference selection, and evaluation.
- `oracle_votes.go` owns proposal extraction and the decoded vote representation.
- `vote_processor.go` coordinates keeper reads, aggregation, state writes, and accounting.

The shared `abci/types` oracle keeper error lets preblock and vote-extension boundaries classify state-access and
mutation failures consistently.

## Verification

From the repository root, run `go test ./abci/oracle/...`. Extraction, aggregation, accounting calls, and deterministic
ordering have focused tests. Run `go test ./abci/oracle -run '^$' -bench . -benchmem` when changing algorithm cost;
the benchmarks distinguish sparse/full targets and identical/different validator support. Cross-module accounting changes
also need the relevant `./x/oracle/...` and `./app` tests.
