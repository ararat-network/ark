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
	// DefaultClaimCancellationPeriodBlocks sizes the claim veto window to a
	// governance voting cycle: governance is the only canceller of its own
	// claims, and a cancellation proposal needs a voting period to land.
	DefaultClaimCancellationPeriodBlocks = chain.BlocksPerWeek
)

// DefaultParams returns the safe launch defaults for Treasury.
func DefaultParams() Params {
	return Params{
		ReferenceTaxCap:               sdk.NewCoin(DefaultReferenceTaxCapDenom, math.ZeroInt()),
		RewardFundingWindow:           DefaultRewardFundingWindow,
		ClaimCancellationPeriodBlocks: DefaultClaimCancellationPeriodBlocks,
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
	if p.ClaimCancellationPeriodBlocks == 0 {
		return errors.New("treasury parameter ClaimCancellationPeriodBlocks must be positive")
	}
	return nil
}
