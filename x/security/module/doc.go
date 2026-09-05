// Package security gives the chain a security committee with a bounded fast
// path over the standard modules' emergency surface: upgrade scheduling and
// cancellation, IBC client recovery, and door-closing halts.
//
// The charter is code and connectivity repair, never funds. The module owns a
// mandate and a committee plan and nothing else: no account, params, hooks, or
// blockers. Each power dispatches a self-constructed upstream message through
// the message router with the effective authority injected, so the committee
// can only do what governance could, sooner.
package security
