# ABCI Oracle Processing

The oracle package owns the fixed oracle protocol used after proposal data reaches preblock. It decodes injected vote
extension data, aggregates validator reports, writes exchange rates, and updates validator accounting.

## Vote Extraction

`GetOracleVotes` reads the encoded extended commit info from the first injected proposal transaction, decodes each
validator vote extension, and returns one `Vote` per validator entry. Empty, undecodable, and semantically invalid vote
extensions become empty oracle reports so their validator remains accountable and their voting power still participates
in quorum denominator logic. The authenticated extended commit itself is never rewritten.

## Aggregation

Aggregation sorts the configured targets, decodes each validator report once, and groups positive supported rates into
validator-indexed ballots. Total commit power includes validators with empty or absent reports. Raw quorum uses
`ceil(VoteThreshold * total power)`, so fractional power requirements never round down.

The reference is the passing denom that can actually price the most other passing denoms. For each reference direction,
overlap counts only validators whose positive `reference/target` quotient is inside the conservative safe magnitude
range, and a target counts as priceable only when the final median conversion is also safe. Ties are broken by summed
qualifying overlap power, raw positive-report power, then lexical denom order.

The reference price is its lower weighted median. Every other price is computed from the lower weighted median of the
overlapping validators' individual `reference/target` ratios:

```text
target price = reference median / median(reference vote / target vote)
```

Both raw and cross ballots must meet the same ceiling quorum. A derived ratio near either `LegacyDec` numeric boundary is
deliberately excluded, even if technically representable, instead of carrying exact edge-rounding complexity into the
oracle protocol. The conservative range check makes the final division safe without panic recovery.

Non-positive submitted rates are treated as abstain/outage signals. They do not add quorum power or earn reward weight,
but they still count as submitted reports for miss-accounting purposes.

For each priced ballot, validators earn their voting power when their tally rate is within the inclusive fixed band
`median * RewardBand / 2`. Positive out-of-band reports mark one miss for the block. Denoms that fail raw quorum, usable
overlap quorum, or final price conversion remain participation-accountable but are not accuracy-scored.

## Price Application

`PriceApplier.ApplyPricesFromVoteExtensions` coordinates the preblock oracle write and returns one `AggregationResult`
containing prices, vote targets, and validator reports:

1. Decode proposal vote data with `GetOracleVotes`.
2. Load oracle params and vote targets from the keeper.
3. Aggregate rates and validator scores.
4. Write exchange rates with events in lexical denom order.
5. Add score weights in commit-validator order and increment at most one miss per validator for the block.

The preblock package calls the price applier and handles the ABCI lifecycle around it.

## Encoding Boundary

`oracle/encoding` is the oracle-specific wrapper around shared primitive encoding. It enforces the maximum encoded rate
size before delegating LegacyDec decoding to `pkg/encoding`.

## Internal Values

Aggregation values live in this package because they are working state for the fixed oracle protocol. Error wrappers
remain exported from this package where callers need to classify failures returned by the price applier.
