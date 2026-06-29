# ABCI Oracle Processing

The oracle package owns the fixed oracle protocol used after proposal data reaches preblock. It decodes injected vote
extension data, aggregates validator reports, writes exchange rates, and updates validator accounting.

## Vote Extraction

`GetOracleVotes` reads the encoded extended commit info from the first injected proposal transaction, decodes each
validator vote extension, and returns one `Vote` per validator entry. Empty vote extensions are preserved as empty oracle
reports so their voting power still participates in quorum denominator logic.

## Aggregation

`VoteAggregator.AggregateOracleVotes` groups reported rates by denom, filters unsupported or failed-quorum denoms, chooses
a reference denom from the supported denoms with the largest passing voting power, and computes weighted-median exchange
rates.

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

## Types

`oracle/types` contains aggregation values and oracle-specific error wrappers used by the vote aggregator and price
applier. Keep these types close to the aggregation code that consumes them.
