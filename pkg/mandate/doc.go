// Package mandate holds the shared appointment envelope every
// governance-appointed committee mandate embeds, and the derivation and
// authorization semantics that go with it.
//
// A mandate is one bounded, expiring delegation of a power governance already
// holds. Governance appoints an exact account for a half-open height window;
// the committee acts faster than a voting period allows; the appointment
// expires on its own. Six mandates exist today — Treasury's economic-policy
// mandate, Claims's, Market's conversion mandate, Asset's emergency mandate,
// Reserve's, and Security's — and the sections below are the convention a
// seventh follows.
//
// # What this package owns
//
// Appointment shape and staleness only: term monotonicity ([Envelope.NextTerm],
// [Next]), the two canonical envelope shapes, the half-open active window
// ([Envelope.IsActive]), the signer-then-term-then-window authorization
// ordering ([Envelope.Authorise]), and the committee observation
// ([Envelope.Observe], [Shape]). Nothing here reads module state — the
// observation classifies an account the module resolved and passed in — so a
// mandate's powers, bounds, and usage metering are invisible to it by
// construction.
//
// # What the module owns
//
// Everything else, and deliberately: the payload fields wrapping the envelope
// (policy corridors, claim limits, Tobin caps), validation of the assembled
// appointment, appointment-time checks against live state, term-scoped usage,
// and the committee powers themselves. [Envelope.Validate] and
// [Envelope.Authorise] both exist to be called from those module-owned checks,
// not to replace them.
//
// # Appointing
//
// One governance-signed message per mandate, MsgSet<Name>Mandate, replacing the
// complete appointment — never a partial edit. An empty committee disables;
// a disabled mandate retains its term so transactions prepared under an earlier
// appointment cannot become valid again if the same account is later
// re-appointed. Derive the successor envelope with [Next], assemble the module
// mandate around it, then validate the whole:
//
//	env, committee, err := mandate.Next(current.Envelope, msg.Committee, msg.ActivationHeight, msg.ExpiryHeight)
//	if err != nil {
//		return nil, err
//	}
//	if !env.IsDisabled() {
//		env.Observe(k.accountKeeper.GetAccount(ctx, committee))
//	}
//	updated := types.NewDisabled<Name>Mandate(env.Term)
//	updated.Envelope = env
//	if msg.Committee != "" {
//		// copy payload fields, then validate and run appointment-time checks
//	}
//
// Building from the disabled constructor is what keeps a disablement's payload
// zeroed, so a stored disabled mandate is always the canonical one.
//
// Appointment-time checks belong here rather than at action time whenever they
// can be decided at appointment: a corridor denominated in a unit the live pool
// no longer uses, or an expiry already behind the chain, is a stillborn
// delegation, and rejecting it fails a proposal loudly instead of leaving a
// committee to discover at the worst moment that it has no usable power.
//
// The committee must differ from the module authority — governance holds the
// unbounded path already, so appointing itself would read as a live delegation
// that delegates nothing. Distinctness *across* modules is deliberately not
// chain-enforced: it is a governance-process concern, and enforcing it would
// put a cross-module keeper read on the appointment path for an invariant
// governance is better placed to hold.
//
// A committee is an ordinary account, so a threshold multisig composes at the
// account layer and nothing here gates on one. What appointment does record is
// [Envelope.Observe]: what the chain could prove about the account, classified
// by [Shape] from the account the module resolved. An address is opaque, so
// without this nothing on chain answers "is this committee still 3-of-5" — the
// address commits to that K-of-N, but only the appointment is placed to read it
// and write it down. The shape authorises nothing, and a zeroed one is a
// committee whose backing could not be established rather than one refused.
//
// # Account backing and dispatch
//
// Authorisation checks the message's committee signer field, never its key
// shape. Direct transactions authenticate that field in ante; authz dispatch
// derives the granter from the message signer and checks its grant; Wasmd
// requires the dispatched signer to be the calling contract. Each path still
// reaches the same module-owned term and window checks. Membership, weights,
// and tallies therefore need no native committee module: the mandate is the
// charter, while account backing and staff delegation have separate lifecycles.
//
// Shape records what was provable at appointment, not a continuously observed
// organisation chart. A simple-key multisig address commits to its threshold
// and members. A contract address does not commit to an immutable signing rule:
// migration or membership changes may alter it. A keyless shape makes no claim
// about contract membership, and must not be extended to record a mutable
// threshold as though it were fixed for the term.
//
// # Replacing
//
// Replacement advances the term, which is what makes term-scoped usage
// term-scoped: reset it in the same handler that stores the new appointment, so
// the appointment and the usage keyed to it can never skew. The Claims and
// Reserve mandates each reset an allowance; Asset's emergency mandate clears
// its recorded per-term suspensions. A mandate with no usage to reset resets
// nothing.
//
// # Acting
//
// Roles with disjoint powers or different execution semantics get separate
// messages; one message serves several roles only when they are the same action
// under the same rules. The practical effect is that the complete committee
// surface is enumerable from the proto service alone. Every committee message
// carries expected_term and authorises through [Envelope.Authorise] before any
// module-specific bound is consulted; governance messages carry no term,
// because no governance authorization depends on a mandate.
//
// The priority mempool lane carries committee messages ahead of other traffic,
// when every message qualifies. [Vouch] calls the module's AuthoriseCommittee,
// the same appointment check its handler runs first. An inactive or mismatched
// appointment rejects admission, as does a store error; submit only once the
// appointment is active. Ante evaluates this after signature verification.
// Mixed and authz transactions use the normal lane without priority vouches.
// Handlers still enforce authorisation at the height the transaction lands.
//
// Expiry is lazy. There is no EndBlocker sweep and no stored active flag — a
// mandate stops authorizing because [Envelope.IsActive] says so at the height
// the action lands. Genesis therefore imports mandates verbatim, expired ones
// included, since an export taken after expiry must round-trip.
//
// # Observing
//
// Appointment, replacement, and disabling each emit Event<Name>MandateSet
// carrying exactly the envelope fields — term, committee, activation height,
// expiry height, committee shape — with the empty committee identifying a
// disabling. These events are the chain-wide record of who holds delegated
// power at any height, and of what backed them, so a mandate that does not emit
// one is invisible to anything indexing it.
// Genesis import emits nothing; it is a state load, not an appointment.
package mandate
