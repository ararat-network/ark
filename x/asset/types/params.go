package types

import (
	"fmt"

	chain "ark/pkg/chain"
)

// DefaultSettlementActivationDelayBlocks gives governance a day to notice a
// mistaken settlement plan and pass CancelSettlement before it activates. It is
// the only correction window a plan has — from activation the terms are fixed
// and the announced closing height binds — so the value is a judgment about how
// long a correcting proposal takes to land, which is why it is a parameter
// rather than a constant: x/gov's voting period can move without x/asset
// knowing.
const DefaultSettlementActivationDelayBlocks = chain.BlocksPerDay

// MaxSettlementActivationDelayBlocks bounds the correction window at a chain
// year. The ceiling is not a policy opinion about the right delay so much as
// the point where the field stops being one: a delay measured in years is a
// refusal to let anyone open a settlement, which belongs in a rejected proposal
// rather than in a parameter that still reads as a working delay. It also keeps
// the delay comfortably inside the int64 block height it is added to, so the
// keeper's arithmetic cannot overflow for any accepted value.
const MaxSettlementActivationDelayBlocks = chain.BlocksPerYear

// DefaultParams returns the launch asset module parameters.
func DefaultParams() Params {
	return Params{
		SettlementActivationDelayBlocks: DefaultSettlementActivationDelayBlocks,
	}
}

// Validate performs context-free validation of asset parameters.
//
// A zero delay is refused rather than read as "activate immediately". The
// delay is the only window in which a mistaken plan can be withdrawn — from
// activation onward the plan binds holders and cannot be closed early — so a
// zero would quietly remove the correction the message is designed around,
// which is not something a parameter update should be able to express.
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
