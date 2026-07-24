# ABCI Oracle Processing

The oracle package owns the fixed oracle protocol used after proposal data reaches preblock. It decodes injected vote
extension data, aggregates validator reports, writes exchange rates, and updates validator accounting.

## Vote Extraction

`GetOracleVotes` reads the encoded extended commit info from the first injected proposal transaction, validates and
decodes each validator vote extension into domain rates, and returns one `Vote` per validator entry. Empty, undecodable,
and semantically invalid vote extensions become empty oracle reports so their validator remains accountable and their
voting power still participates in quorum denominator logic. The authenticated extended commit itself is never
rewritten. A decoded, target-version-matched extension is marked as a valid report even when it contains only a subset of
the canonical targets. This distinguishes a fresh sparse report from an absent or invalid extension.

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

The ceiling prevents fractional power requirements from rounding down. Non-positive submitted rates are unusable: they
do not add ballot power or earn score weight, and they mark the validator missed for the block. They still count as
submitted values, so they cannot masquerade as target-unavailability signals.

### Target Unavailability

Omission from a valid, target-version-matched report is an unavailability vote for that target. Empty, undecodable, and
semantically invalid extensions contribute no unavailability power. For each target:

```text
unavailability power = valid report power - power that submitted any value
```

When positive-report power does not meet raw quorum but unavailability power does, the target is unavailable for that
block. No exchange rate is written or refreshed. A validator's valid omission avoids a miss only for a target that
reaches unavailability quorum; below quorum, the omission remains a miss. Positive reports remain participation-valid
when a target is unavailable but receive no target reward. Non-positive submissions and invalid whole reports remain
misses.

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
unrepresentable or non-positive.

### Accuracy Scoring

Only priced tallies are accuracy-scored. A validator earns one reward target when its tally rate is inside the inclusive
symmetric band:

```text
absolute(tally ratio - median ratio) <= median ratio * RewardBand / 2
```

The implementation materialises the lower and upper `LegacyDec` endpoints with checked arithmetic and compares each rate
with them. Positive out-of-band reports mark one miss for the block. Targets that fail raw quorum, usable overlap quorum,
final price conversion, or reward-band construction remain participation-accountable but are not accuracy-scored.

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

`ProcessVoteExtensions` coordinates the preblock oracle write and returns the consensus prices applied to state:

1. Load the vote-target epoch for the previous height.
2. Decode and validate proposal vote data with `GetOracleVotes` against that epoch.
3. Load oracle params and aggregate rates and validator scores.
4. Write exchange rates with events in lexical denom order.
5. Record reward weights in commit-validator order and increment at most one miss per validator for the block.

The preblock package calls this function with its oracle keeper and handles the ABCI lifecycle around it.

## Encoding Boundary

`oracle_votes.go` enforces vote-extension rate cardinality and target membership before decoding each bounded
`LegacyDec` value through `pkg/encoding`.

## Internal Values

Aggregation values live in this package because they are working state for the fixed oracle protocol:

- `aggregation.go` builds ballots, applies quorum, and computes prices and validator scores.
- `ballot.go` owns source/cross ballot representation, overlap lookup, and weighted median selection.
- `reference.go` owns overlap scoring, reference selection, and evaluation.
- `oracle_votes.go` owns proposal extraction and the decoded vote representation.
- `vote_processor.go` coordinates keeper reads, aggregation, state writes, and accounting.

The shared `abci/types` oracle keeper error lets preblock and vote-extension boundaries classify state-access and
mutation failures consistently.
