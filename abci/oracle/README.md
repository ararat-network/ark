# ABCI Oracle Processing

The oracle package owns the fixed oracle protocol used after proposal data reaches preblock. It decodes injected vote
extension data, aggregates validator reports, writes exchange rates, and updates validator accounting.

## Vote Extraction

`GetOracleVotes` reads the encoded extended commit info from the first injected proposal transaction, decodes each
validator vote extension, and returns one `Vote` per validator entry. Empty vote extensions are preserved as empty oracle
reports so their voting power still participates in quorum denominator logic.

## Aggregation

`VoteAggregator` groups reported rates by denom, filters unsupported or failed-quorum denoms, chooses the reference denom
that can price the most passing denoms through validator overlap, and computes weighted-median exchange rates. Ties in
reference selection are broken by total overlap power, raw voting power, then denom order.

Non-reference denoms must have enough overlap voting power with the selected reference denom before they can be priced
through cross rates.

Non-positive submitted rates are treated as abstain/outage signals. They do not add quorum power or earn reward weight,
but they still count as submitted reports for miss-accounting purposes.

## Price Application

`PriceApplier.ApplyPricesFromVoteExtensions` coordinates the preblock oracle write:

1. Decode proposal vote data with `GetOracleVotes`.
2. Load oracle params and vote targets from the keeper.
3. Aggregate rates and validator scores.
4. Write exchange rates with events.
5. Add validator score weight and increment miss counts when required.

The preblock package calls the price applier and handles the ABCI lifecycle around it.

## Encoding Boundary

`oracle/encoding` is the oracle-specific wrapper around shared primitive encoding. It enforces the maximum encoded rate
size before delegating LegacyDec decoding to `pkg/encoding`.

## Internal Values

Aggregation values live in this package because they are working state for the fixed oracle protocol. Error wrappers
remain exported from this package where callers need to classify failures returned by the price applier.
