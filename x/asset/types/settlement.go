package types

import (
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	"ark/pkg/decimal"
)

// Validate checks a settlement plan's denomination, redemption rate, heights,
// and lifecycle version.
func (p SettlementPlan) Validate() error {
	if err := validateAssetDenom(p.Denom); err != nil {
		return fmt.Errorf("settlement plan %w", err)
	}
	if p.RedemptionRate.IsNil() {
		return fmt.Errorf("settlement redemption rate must not be nil")
	}
	if !p.RedemptionRate.IsInValidRange() {
		return fmt.Errorf("settlement redemption rate is out of range")
	}
	if !p.RedemptionRate.IsPositive() {
		return fmt.Errorf(
			"settlement redemption rate must be positive: %s",
			p.RedemptionRate,
		)
	}
	if p.ActivationHeight <= 0 {
		return fmt.Errorf("settlement activation height must be positive: %d", p.ActivationHeight)
	}
	if p.EarliestClosingHeight < 0 {
		return fmt.Errorf(
			"settlement earliest closing height must not be negative: %d",
			p.EarliestClosingHeight,
		)
	}
	if p.EarliestClosingHeight != 0 &&
		p.EarliestClosingHeight <= p.ActivationHeight {
		return fmt.Errorf(
			"settlement earliest closing height %d must be after activation height %d",
			p.EarliestClosingHeight,
			p.ActivationHeight,
		)
	}
	if p.Version == 0 {
		return fmt.Errorf("settlement version must be positive")
	}

	return nil
}

// QuoteRedemption calculates the rate-based NOAH output for an asset input,
// rounding down to whole anoah.
func (p SettlementPlan) QuoteRedemption(offer sdk.Coin) (sdk.Coin, error) {
	if err := p.Validate(); err != nil {
		return sdk.Coin{}, err
	}
	if err := offer.Validate(); err != nil {
		return sdk.Coin{}, fmt.Errorf("settlement offer is invalid: %w", err)
	}
	if !offer.IsPositive() {
		return sdk.Coin{}, fmt.Errorf("settlement offer must be positive: %s", offer)
	}
	if offer.Denom != p.Denom {
		return sdk.Coin{}, fmt.Errorf(
			"settlement offer denom %s must match asset denom %s",
			offer.Denom,
			p.Denom,
		)
	}

	outputDec, err := decimal.Mul(
		math.LegacyNewDecFromInt(offer.Amount),
		p.RedemptionRate,
	)
	if err != nil {
		return sdk.Coin{}, fmt.Errorf("multiplying settlement redemption rate: %w", err)
	}
	output := outputDec.TruncateInt()
	if !output.IsPositive() {
		return sdk.Coin{}, fmt.Errorf("settlement output rounds to zero")
	}

	return sdk.NewCoin(chain.NoahBaseDenom, output), nil
}

// Validate checks an immutable write-off snapshot.
func (r WriteOffRecord) Validate() error {
	if err := validateAssetDenom(r.Denom); err != nil {
		return fmt.Errorf("write-off record %w", err)
	}
	if r.Version == 0 {
		return fmt.Errorf("write-off version must be positive")
	}
	if r.WriteOffHeight <= 0 {
		return fmt.Errorf("write-off height must be positive: %d", r.WriteOffHeight)
	}
	if err := r.OutstandingSupply.Validate(); err != nil {
		return fmt.Errorf("write-off outstanding supply is invalid: %w", err)
	}
	if !r.OutstandingSupply.IsPositive() {
		return fmt.Errorf("write-off outstanding supply must be positive: %s", r.OutstandingSupply)
	}
	if r.OutstandingSupply.Denom != r.Denom {
		return fmt.Errorf(
			"write-off outstanding supply denom %s must match asset denom %s",
			r.OutstandingSupply.Denom,
			r.Denom,
		)
	}
	if r.SettlementPlan == nil {
		return nil
	}
	if err := r.SettlementPlan.Validate(); err != nil {
		return fmt.Errorf("write-off settlement plan is invalid: %w", err)
	}
	if r.SettlementPlan.Denom != r.Denom {
		return fmt.Errorf(
			"write-off settlement plan denom %s must match asset denom %s",
			r.SettlementPlan.Denom,
			r.Denom,
		)
	}
	if r.SettlementPlan.Version >= r.Version {
		return fmt.Errorf(
			"write-off settlement version %d must precede write-off version %d",
			r.SettlementPlan.Version,
			r.Version,
		)
	}
	if _, err := r.SettlementPlan.QuoteRedemption(r.OutstandingSupply); err != nil {
		return fmt.Errorf("valuing written-off settlement supply: %w", err)
	}

	return nil
}

// NoahLiability derives the written-off NOAH entitlement when the asset had a
// settlement plan. The boolean is false when the write-off was unpriced.
func (r WriteOffRecord) NoahLiability() (sdk.Coin, bool, error) {
	if err := r.Validate(); err != nil {
		return sdk.Coin{}, false, err
	}
	if r.SettlementPlan == nil {
		return sdk.Coin{}, false, nil
	}

	liability, err := r.SettlementPlan.QuoteRedemption(r.OutstandingSupply)
	if err != nil {
		return sdk.Coin{}, false, err
	}

	return liability, true, nil
}
