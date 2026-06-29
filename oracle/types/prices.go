package types

import (
	"fmt"
	"math/big"
)

type Prices map[string]*big.Float

// ParsePrice parses a decimal string into an oracle price.
func ParsePrice(s string) (*big.Float, error) {
	price, ok := new(big.Float).SetString(s)
	if !ok {
		return nil, fmt.Errorf("failed to parse oracle price %q", s)
	}

	return price, nil
}
