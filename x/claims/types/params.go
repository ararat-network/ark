package types

import (
	"errors"

	"ark/pkg/chain"
)

const DefaultClaimCancellationPeriodBlocks = chain.BlocksPerWeek

// DefaultParams returns the safe launch defaults for Claims.
func DefaultParams() Params {
	return Params{
		ClaimCancellationPeriodBlocks: DefaultClaimCancellationPeriodBlocks,
	}
}

// Validate performs context-free validation of Claims parameters.
func (p Params) Validate() error {
	if p.ClaimCancellationPeriodBlocks == 0 {
		return errors.New("claims parameter ClaimCancellationPeriodBlocks must be positive")
	}
	return nil
}
