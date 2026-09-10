package types

import (
	"fmt"

	"github.com/ararat-network/ark/pkg/chain"
)

// DefaultSettlementActivationDelayBlocks allows a day of blocks for governance correction before
// settlement binds. Governance must size this window against its voting period.
const DefaultSettlementActivationDelayBlocks = chain.BlocksPerDay

// MaxSettlementActivationDelayBlocks caps the correction window at a chain year. Height addition
// still requires overflow checking near the int64 limit.
const MaxSettlementActivationDelayBlocks = chain.BlocksPerYear

// DefaultParams returns the launch asset module parameters.
func DefaultParams() Params {
	return Params{
		SettlementActivationDelayBlocks: DefaultSettlementActivationDelayBlocks,
	}
}

// Validate requires a positive, bounded settlement delay so every plan has a correction window
// before activation.
func (p Params) Validate() error {
	if p.SettlementActivationDelayBlocks == 0 ||
		p.SettlementActivationDelayBlocks > MaxSettlementActivationDelayBlocks {
		return fmt.Errorf(
			"asset parameter SettlementActivationDelayBlocks must be between one and %d, is %d",
			MaxSettlementActivationDelayBlocks,
			p.SettlementActivationDelayBlocks,
		)
	}

	return nil
}
