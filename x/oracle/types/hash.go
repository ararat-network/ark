package types

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// TruncatedHashSize is the number of bytes used for the vote hash,
// matching the CometBFT address length (RIPEMD-160 / 20 bytes).
const TruncatedHashSize = 20

// VoteHash is hash value to hide vote exchange rates which is formatted as hex string
// in SHA256("{salt}:{exchange rate}{denom},...,{exchange rate}{denom}:{voter}")
type VoteHash []byte

// GetVoteHash computes hash value of ExchangeRateVote to avoid redundant DecCoins stringify
// operation, use string argument
func GetVoteHash(salt, exchangeRatesStr string, voter sdk.ValAddress) VoteHash {
	sourceStr := fmt.Sprintf("%s:%s:%s", salt, exchangeRatesStr, voter.String())
	hash := sha256.Sum256([]byte(sourceStr))
	return hash[:TruncatedHashSize]
}

// VoteHashFromHexString convert hex string to VoteHash
func VoteHashFromHexString(s string) (VoteHash, error) {
	h, err := hex.DecodeString(s)
	if err != nil {
		return nil, err
	}

	return h, nil
}

// String implements fmt.Stringer interface
func (h VoteHash) String() string {
	return hex.EncodeToString(h)
}
