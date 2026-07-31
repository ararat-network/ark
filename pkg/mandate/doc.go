// Package mandate holds the shared appointment envelope every
// governance-appointed committee mandate embeds, and the derivation and
// authorization semantics that go with it.
//
// A mandate is one bounded, expiring delegation of a power governance already
// holds. Governance appoints an exact account for a half-open height window;
// the committee acts faster than a voting period allows; the appointment
// expires on its own. Four mandates exist today — Treasury's monetary-policy
// and Claims mandates, Market's conversion mandate, and Asset's emergency
// mandate — and the sections below are the convention a fifth follows.
//
// # What this package owns
//
// Appointment shape and staleness only: term monotonicity ([Envelope.NextTerm],
// [Next]), the two canonical envelope shapes, the half-open active window
// ([Envelope.IsActive]), and the signer-then-term-then-window authorization
// ordering ([Envelope.Authorise]). Nothing here reads module state, so a
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
//	env, err := mandate.Next(current.Envelope, msg.Committee, msg.ActivationHeight, msg.ExpiryHeight)
//	if err != nil {
//		return nil, err
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
// governance is better placed to hold. Committee membership is likewise not
// this package's problem — a committee is an ordinary account, so a multisig
// composes at the account layer with no mandate-level support.
//
// # Replacing
//
// Replacement advances the term, which is what makes term-scoped usage
// term-scoped: reset it in the same handler that stores the new appointment, so
// the appointment and the usage keyed to it can never skew. Treasury's Claims
// mandate resets its allowance; Asset's emergency mandate clears its recorded
// per-term suspensions. A mandate with no usage to reset resets nothing.
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
// Expiry is lazy. There is no EndBlocker sweep and no stored active flag — a
// mandate stops authorizing because [Envelope.IsActive] says so at the height
// the action lands. Genesis therefore imports mandates verbatim, expired ones
// included, since an export taken after expiry must round-trip.
//
// # Observing
//
// Appointment, replacement, and disabling each emit Event<Name>MandateSet
// carrying exactly the envelope fields — term, committee, activation height,
// expiry height — with the empty committee identifying a disabling. These
// events are the chain-wide record of who holds delegated power at any height,
// so a mandate that does not emit one is invisible to anything indexing it.
// Genesis import emits nothing; it is a state load, not an appointment.
package mandate
