package types

import (
	"fmt"

	"ark/pkg/chain"
)

const (
	DefaultClaimCancellationPeriodBlocks = chain.BlocksPerWeek

	// MaxClaimCancellationPeriodBlocks bounds the veto window at a year.
	//
	// The window is what stands between a submitted claim and a payable one, so
	// a period no chain reaches does not make cancellation more generous — it
	// makes every claim permanently unexecutable and freezes Insurance while
	// the mandate still reads as configured. That is a governance deadlock with
	// no error to find it by. A year is fifty-odd times the week-long default,
	// well past any review a claim could honestly need.
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
			uint64(MaxClaimCancellationPeriodBlocks),
			p.ClaimCancellationPeriodBlocks,
		)
	}
	return nil
}
