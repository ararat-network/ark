package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/pkg/chain"
)

const (
	DefaultReferenceTaxCapDenom      = chain.SDRBaseDenom
	DefaultRewardFundingWindow       = chain.BlocksPerWeek
	DefaultTaxCapRefreshPeriodBlocks = chain.BlocksPerWeek
)

// DefaultParams returns the safe launch defaults for Treasury.
//
// The reference tax cap launches at one base unit — the tightest finite
// ceiling — rather than zero, which is the explicit uncapped sentinel: an
// unconfigured chain should clamp the stability tax to dust, not leave it
// unbounded, and governance opts into either a real ceiling or none.
func DefaultParams() Params {
	return Params{
		ReferenceTaxCap:           sdk.NewCoin(DefaultReferenceTaxCapDenom, math.OneInt()),
		RewardFundingWindow:       DefaultRewardFundingWindow,
		TaxCapRefreshPeriodBlocks: DefaultTaxCapRefreshPeriodBlocks,
	}
}

// Validate performs context-free validation of Treasury parameters.
// ReferenceTaxCap.Denom's identity with the protocol reference is validated
// by the keeper.
func (p Params) Validate() error {
	if err := p.ReferenceTaxCap.Validate(); err != nil {
		return fmt.Errorf("treasury parameter ReferenceTaxCap is invalid: %w", err)
	}
	if err := chain.ValidatePricedDenom(p.ReferenceTaxCap.Denom); err != nil {
		return fmt.Errorf("treasury parameter ReferenceTaxCap denom is invalid: %w", err)
	}
	if p.RewardFundingWindow == 0 {
		return errors.New("treasury parameter RewardFundingWindow must be positive")
	}
	if p.TaxCapRefreshPeriodBlocks == 0 {
		return errors.New("treasury parameter TaxCapRefreshPeriodBlocks must be positive")
	}
	return nil
}
