package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/pkg/chain"
)

const (
	DefaultReferenceTaxCapDenom = chain.MicroSDRDenom
	DefaultRewardFundingWindow  = chain.BlocksPerWeek
)

// DefaultParams returns the safe launch defaults for Treasury.
func DefaultParams() Params {
	return Params{
		ReferenceTaxCap:     sdk.NewCoin(DefaultReferenceTaxCapDenom, math.OneInt()),
		RewardFundingWindow: DefaultRewardFundingWindow,
	}
}

// Validate performs context-free validation of Treasury parameters.
// Oracle membership of ReferenceTaxCap.Denom is validated by the keeper.
func (p Params) Validate() error {
	if err := p.ReferenceTaxCap.Validate(); err != nil {
		return fmt.Errorf("treasury parameter ReferenceTaxCap is invalid: %w", err)
	}
	if err := chain.ValidateMicroDenom(p.ReferenceTaxCap.Denom); err != nil {
		return fmt.Errorf("treasury parameter ReferenceTaxCap denom is invalid: %w", err)
	}
	if !p.ReferenceTaxCap.IsPositive() {
		return errors.New("treasury parameter ReferenceTaxCap must be positive")
	}
	if p.RewardFundingWindow == 0 {
		return errors.New("treasury parameter RewardFundingWindow must be positive")
	}
	return nil
}
