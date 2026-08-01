package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/pkg/chain"
)

const (
	DefaultReferenceTaxCapDenom          = chain.SDRBaseDenom
	DefaultRewardFundingWindow           = chain.BlocksPerWeek
	DefaultClaimCancellationPeriodBlocks = chain.BlocksPerWeek
	DefaultTaxCapRefreshPeriodBlocks     = chain.BlocksPerWeek
)

// DefaultParams returns the safe launch defaults for Treasury.
func DefaultParams() Params {
	return Params{
		ReferenceTaxCap:               sdk.NewCoin(DefaultReferenceTaxCapDenom, math.ZeroInt()),
		RewardFundingWindow:           DefaultRewardFundingWindow,
		ClaimCancellationPeriodBlocks: DefaultClaimCancellationPeriodBlocks,
		TaxCapRefreshPeriodBlocks:     DefaultTaxCapRefreshPeriodBlocks,
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
	if p.ClaimCancellationPeriodBlocks == 0 {
		return errors.New("treasury parameter ClaimCancellationPeriodBlocks must be positive")
	}
	if p.TaxCapRefreshPeriodBlocks == 0 {
		return errors.New("treasury parameter TaxCapRefreshPeriodBlocks must be positive")
	}
	return nil
}
