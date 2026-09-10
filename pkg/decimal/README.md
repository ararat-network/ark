# Checked decimal arithmetic

Package decimal provides checked arithmetic for Cosmos SDK LegacyDec values.

Every operation returns exactly what the equivalent cosmossdk.io/math method
returns, and reports an error wherever that method would panic. That
equivalence is a consensus rule rather than an implementation detail: these
results feed state transitions, so any edit that changes an observable result
— reordering a rounding step, widening a fast-path bound, simplifying
roundByPrecision — makes new binaries disagree with deployed ones on the same
block. The differential tests in this package are the specification of that
equivalence; treat anything they catch as needing a coordinated upgrade.

Reach for a checked operation when an operand's magnitude comes from outside
the caller's invariants — a validator's vote, a user's offer amount, a
governance parameter, a total accumulated across modules — or when the
operation itself amplifies magnitude, such as squaring a pool or dividing by a
denominator that can approach zero. Those inputs can make a stock LegacyDec method panic. Checked operations let
message and vote handlers reject invalid input through their normal error paths.
Inside BeginBlock or EndBlock, either a panic or a returned error still fails the
block; liveness depends on bounding inputs before they reach those folds.

Plain LegacyDec methods stay correct where a nearby guard or a type invariant
already bounds both operands and the result: a ratio the caller has proved
lies in [0, 1), or a subtraction a comparison has just made safe. Wrapping
those adds error paths that can never fire and buries the arithmetic in
plumbing, so this boundary is left to judgement rather than enforced by a lint
rule. An error here reports an operand the chain cannot represent, so handle
it as a domain outcome — reject the message, skip the vote — rather than as an
internal fault.
