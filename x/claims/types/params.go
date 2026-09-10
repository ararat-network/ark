package types

import (
	"fmt"

	"github.com/ararat-network/ark/pkg/chain"
)

const (
	DefaultClaimCancellationPeriodBlocks = chain.BlocksPerWeek

	// MaxClaimCancellationPeriodBlocks caps the veto window at a chain year, well above the
	// week-long default, so valid policy cannot defer claims indefinitely while reserving
	// Insurance.
	MaxClaimCancellationPeriodBlocks = chain.BlocksPerYear
)

// DefaultParams returns the safe launch defaults for Claims.
func DefaultParams() Params {
	return Params{
		ClaimCancellationPeriodBlocks: DefaultClaimCancellationPeriodBlocks,
	}
}

// Validate performs context-free validation of Claims parameters.
func (p Params) Validate() error {
	if p.ClaimCancellationPeriodBlocks == 0 ||
		p.ClaimCancellationPeriodBlocks > MaxClaimCancellationPeriodBlocks {
		return fmt.Errorf(
			"claims parameter ClaimCancellationPeriodBlocks must be between one and %d, is %d",
			MaxClaimCancellationPeriodBlocks,
			p.ClaimCancellationPeriodBlocks,
		)
	}
	return nil
}
