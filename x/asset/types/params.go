package types

import (
	"fmt"
)

// DefaultSettlementActivationDelay allows a week, in seconds, for governance correction before
// settlement binds, the Claims cancellation period: a full deposit period and vote fit with margin.
// It is block time, as the voting period is, so block pace cannot shorten it.
const DefaultSettlementActivationDelay = uint64(7 * 86_400)

// MaxSettlementActivationDelay caps the correction window at a year, so opening time plus delay
// stays far inside the header's int64.
const MaxSettlementActivationDelay = uint64(365 * 86_400)

// DefaultParams returns the launch asset module parameters.
func DefaultParams() Params {
	return Params{
		SettlementActivationDelay: DefaultSettlementActivationDelay,
	}
}

// Validate requires a positive, bounded settlement delay so every plan has a correction window
// before activation.
func (p Params) Validate() error {
	if p.SettlementActivationDelay == 0 ||
		p.SettlementActivationDelay > MaxSettlementActivationDelay {
		return fmt.Errorf(
			"asset parameter SettlementActivationDelay must be between one and %d, is %d",
			MaxSettlementActivationDelay,
			p.SettlementActivationDelay,
		)
	}

	return nil
}
