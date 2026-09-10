// Package params holds Ark's compile-time chain identity: the bech32 prefixes,
// the address verifier, and the BIP-44 derivation constants. app/config.go reads
// it once to set and seal the global SDK config.
package params

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// The validator, consensus, and public-key prefixes derive from the account
// prefix the way the SDK derives its own, which is also how the depinject
// runtime fills them in when the staking config leaves them unset.
const (
	// Bech32PrefixAccAddr defines the Bech32 prefix of an account's address
	Bech32PrefixAccAddr = "ark"
	// Bech32PrefixAccPub defines the Bech32 prefix of an account's public key
	Bech32PrefixAccPub = Bech32PrefixAccAddr + sdk.PrefixPublic
	// Bech32PrefixValAddr defines the Bech32 prefix of a validator's operator address
	Bech32PrefixValAddr = Bech32PrefixAccAddr + sdk.PrefixValidator + sdk.PrefixOperator
	// Bech32PrefixValPub defines the Bech32 prefix of a validator's operator public key
	Bech32PrefixValPub = Bech32PrefixValAddr + sdk.PrefixPublic
	// Bech32PrefixConsAddr defines the Bech32 prefix of a consensus node address
	Bech32PrefixConsAddr = Bech32PrefixAccAddr + sdk.PrefixValidator + sdk.PrefixConsensus
	// Bech32PrefixConsPub defines the Bech32 prefix of a consensus node public key
	Bech32PrefixConsPub = Bech32PrefixConsAddr + sdk.PrefixPublic
)

// AddressVerifier ark address verifier.
//
// Twenty bytes is every key-derived and module address on the chain. Thirty-two
// is what CosmWasm derives a contract address to, both the classic and the
// predictable form, so refusing that length rejects every message naming a
// contract and makes the runtime unusable rather than merely restricted.
var AddressVerifier = func(bz []byte) error {
	switch n := len(bz); n {
	case 20, 32:
		return nil
	default:
		return fmt.Errorf("incorrect address length %d", n)
	}
}
