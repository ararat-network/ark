/*
Package params holds Ark's compile-time chain identity: the bech32 prefixes,
the address verifier, and the BIP-44 derivation constants. app/config.go reads
it once to set and seal the global SDK config.
*/
package params
