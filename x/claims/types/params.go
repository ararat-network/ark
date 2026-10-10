package types

import (
	"fmt"
)

const (
	// DefaultClaimCancellationPeriod is a week, in seconds of block time, so the veto window fits a
	// governance deposit period and vote, which run in block time too.
	DefaultClaimCancellationPeriod = uint64(7 * 86_400)

	// MaxClaimCancellationPeriod caps the veto window at a year, well above the week-long default,
	// so valid policy cannot defer claims indefinitely while reserving Insurance.
	MaxClaimCancellationPeriod = uint64(365 * 86_400)
)

// DefaultParams returns the safe launch defaults for Claims.
func DefaultParams() Params {
	return Params{
		ClaimCancellationPeriod: DefaultClaimCancellationPeriod,
	}
}

// Validate performs context-free validation of Claims parameters.
func (p Params) Validate() error {
	if p.ClaimCancellationPeriod == 0 ||
		p.ClaimCancellationPeriod > MaxClaimCancellationPeriod {
		return fmt.Errorf(
			"claims parameter ClaimCancellationPeriod must be between one and %d, is %d",
			MaxClaimCancellationPeriod,
			p.ClaimCancellationPeriod,
		)
	}
	return nil
}
