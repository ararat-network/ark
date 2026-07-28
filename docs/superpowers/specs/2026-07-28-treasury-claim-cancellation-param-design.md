# Treasury Claim Cancellation Period As A Module Param

Date: 2026-07-28
Status: approved design

## Problem

The Claims cancellation period lives on the committee appointment.
`ClaimsMandate` stores `cancellation_period_blocks`
(`proto/ark/treasury/v1/treasury.proto:165`), and the shared submission core
`submitClaim` (`x/treasury/keeper/claims.go:143`) derives every claim's
executable height from it. Because the period is mandate state, governance
submissions are forced through committee machinery they have no other reason to
touch:

1. `MsgSubmitClaim` must carry `expected_term` to pin the mandate whose period
   it uses, while its sibling governance messages (`MsgUpdateMonetaryPolicy`,
   `MsgCancelClaim`) dropped `expected_term` in the role-split redesign.
2. The core enforces `IsActive` and the executable-height-versus-expiry cap for
   both origins, so governance cannot submit a claim while the Claims mandate
   is disabled or outside its window — exactly the emergency situations in
   which a governance claim is most plausible. A disabled mandate is
   structurally unusable anyway: its zero period yields
   `ExecutableHeight == SubmittedHeight`, which `Claim.Validate`
   (`x/treasury/types/claims.go:177`) rejects.

The mandate's only functional contribution to a governance claim is the number
`cancellation_period_blocks`. The term pin exists solely to keep that number
from being swapped mid-vote.

## Non-goals

- Per-origin cancellation periods. The period stays one shared value; the only
  canceller of a governance claim is governance itself, which needs a voting
  period to land a cancellation, so the shared default is sized to a week. A
  split can be added later without ceremony.
- Changing cancellation or execution semantics. `ExecuteClaim` and the
  `cancelClaim` core already never read the mandate; the claim's own
  `ExecutableHeight` remains the sole boundary.
- Committee-path behavior changes. Identity, `IsActive`, `RequireTerm`, the
  expiry cap, and allowance metering are unchanged for committee submissions.

## Approach

Move the period to Treasury `Params`
(`claim_cancellation_period_blocks`), which is governance-owned via the
existing `MsgUpdateParams`. Both origins read it at submission time. The
mandate shrinks to a pure appointment: `Envelope` (term, committee, window)
plus `CommitteeClaimLimit` — who may act, when, and how much.

Consequences:

- `MsgSubmitClaim` drops `expected_term`; the governance path never loads the
  mandate. Governance can submit while the mandate is disabled or lapsed.
- `Claim.mandate_term` becomes committee-only provenance: committee claims
  record the authorizing term (positive), governance claims record zero.
  `Claim.Validate` enforces this per origin. Genesis validation is unaffected:
  the future-term bound passes trivially for zero, and the allowance
  reconciliation already filters on committee origin.
- `ClaimsMandate.Validate` loses its three period checks (disabled-must-be-
  zero, positive, and period-within-active-span).

## Span guard

The deleted static check `period <= expiry - activation` is what made an
appointment usable: the committee runtime cap rejects any claim whose
`executableHeight = height + period` exceeds `ExpiryHeight`, so a period longer
than the span means no committee submission can ever succeed. The check is
reinstated at the one place both values meet under governance control:
`SetClaimsMandate` rejects appointing a committee whose active span is shorter
than the current param. The subtraction is underflow-safe because the envelope
already enforces `activation < expiry`.

The reverse direction — `MsgUpdateParams` raising the period above a live
mandate's span — is deliberately unguarded. Guarding it would make params
validation read mandate state (an inverted coupling); the runtime expiry cap
backstops it, and governance recovers by lowering the param or re-appointing.

## Wire and API changes

`proto/ark/treasury/v1/treasury.proto`:

- `Params`: new `uint64 claim_cancellation_period_blocks = 3`.
- `ClaimsMandate`: remove `cancellation_period_blocks = 2`; renumber
  `committee_claim_limit` 3 → 2 (compact pre-launch renumbering, matching the
  file's existing style).
- `Claim.mandate_term`: comment now states zero for governance claims.

`proto/ark/treasury/v1/tx.proto`:

- `MsgSetClaimsMandate`: remove `cancellation_period_blocks = 5`; renumber
  `committee_claim_limit` 6 → 5.
- `MsgSubmitClaim`: remove `expected_term = 2`; renumber the remaining fields
  2–5; doc comment states the message does not depend on the Claims mandate.

## Defaults and validation

- `DefaultClaimCancellationPeriodBlocks = chain.BlocksPerWeek`, matching
  `DefaultRewardFundingWindow`. Sized so the governance veto window on a claim
  can fit a governance voting cycle; the launch value remains a pending
  parameter decision (plan item P4).
- `Params.Validate` requires the period to be positive. A zero period would
  make every claim invalid (`ExecutableHeight` must exceed
  `SubmittedHeight`).
- Executable-height overflow stays caught origin-independently by
  `Claim.Validate` ("must follow its submission height").

## Keeper shape

`CommitteeSubmitClaim` owns committee authorization — identity, `IsActive`,
and `RequireTerm`, mirroring `CommitteeCancelClaim` — and passes the mandate
facts the core consumes (`MandateTerm`, `MandateExpiryHeight`,
`CommitteeClaimLimit`) as plain data on `claimSubmission`; governance leaves
them zero. `submitClaim(ctx, sub claimSubmission)` reads `Params` for the
executable height and never reads the Claims mandate: only the expiry cap and
allowance metering stay committee-conditional in the core, because the
executable height is computed there and the allowance check must stay atomic
with its write. The governance handler is authority check plus core call.

## Plan-doc amendments

`docs/TREASURY_REDESIGN_PLAN.md`: D30 and D38 move the shared cancellation
period from the mandate to Params and record that governance submissions are
mandate-independent; the Claims prose and P4 follow.

## Testing

- Types: params mutate-case for the new field; origin-aware `mandate_term`
  cases in both directions; mandate validation drops period cases.
- Keeper: the governance-versus-disabled-sentinel submission flips from
  rejection to success; governance gains post-expiry and cross-expiry
  submission coverage; committee window/term/cap rejections are unchanged; the
  executable-height wrap test re-targets the governance path (the span guard
  makes the committee wrap unreachable) and the guard gains its own rejection
  case.
- Fixture ripple: keeper genesis/query suites and the app-level treasury
  suites drop the period from mandate literals and set it via params.
