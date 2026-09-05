// Package asset is the registry and lifecycle owner of every governance-managed
// Bank asset other than NOAH.
//
// Registering an asset is the act that creates a liability: each is priced by
// the feed its own denomination keys and convertible through Market, the
// chain's only native mint path. The module owns the lifecycle statuses, the
// settlement plans that fix the rate at which supply leaves the loop, and the
// emergency-suspension committee mandate. Treasury derives its tax base,
// liability partition, and cap membership from the registry; Market reads it
// for conversion eligibility; x/oracle consults it before removing a feed.
package asset
