package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/pkg/chain"
)

const (
	DefaultReferenceTaxCapDenom = chain.SDRBaseDenom
	DefaultRewardFundingWindow  = chain.BlocksPerWeek
)

// DefaultParams returns the safe launch defaults for Treasury.
func DefaultParams() Params {
	return Params{
		ReferenceTaxCap:     sdk.NewCoin(DefaultReferenceTaxCapDenom, math.ZeroInt()),
		RewardFundingWindow: DefaultRewardFundingWindow,
	}
}

// Validate performs context-free validation of Treasury parameters.
// Oracle membership of ReferenceTaxCap.Denom is validated by the keeper.
func (p Params) Validate() error {
	if err := p.ReferenceTaxCap.Validate(); err != nil {
		return fmt.Errorf("treasury parameter ReferenceTaxCap is invalid: %w", err)
	}
	if err := chain.ValidateNativeBaseDenom(p.ReferenceTaxCap.Denom); err != nil {
		return fmt.Errorf("treasury parameter ReferenceTaxCap denom is invalid: %w", err)
	}
	if p.RewardFundingWindow == 0 {
		return errors.New("treasury parameter RewardFundingWindow must be positive")
	}
	return nil
}
