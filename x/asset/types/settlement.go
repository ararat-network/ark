package types

import (
	"fmt"

	"cosmossdk.io/collections"

	chain "github.com/ararat-network/ark/pkg/chain"
)

// Validate checks a settlement plan's denomination, redemption rate, and
// heights.
func (p SettlementPlan) Validate() error {
	if err := chain.ValidatePricedDenom(p.Denom); err != nil {
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
	if p.OpenedHeight <= 0 {
		return fmt.Errorf("settlement opened height must be positive: %d", p.OpenedHeight)
	}
	if p.OpenedHeight >= p.ActivationHeight {
		return fmt.Errorf(
			"settlement opened height %d must precede activation height %d",
			p.OpenedHeight,
			p.ActivationHeight,
		)
	}
	// The window is mandatory, so every plan carries a guarantee holders can
	// state: redemption opens at the activation height and cannot be
	// derecognized before this one.
	if p.EarliestClosingHeight <= p.ActivationHeight {
		return fmt.Errorf(
			"settlement earliest closing height %d must be after activation height %d",
			p.EarliestClosingHeight,
			p.ActivationHeight,
		)
	}
	return nil
}

// IsActive reports whether the plan's activation delay has elapsed at the given
// height. It is the single question that separates an announced plan from a
// binding one, asked by the redemption gate and the cancellation window alike.
func (p SettlementPlan) IsActive(blockHeight int64) bool {
	return blockHeight >= p.ActivationHeight
}

// Validate checks an immutable resolution snapshot.
func (r ResolutionRecord) Validate() error {
	if err := chain.ValidatePricedDenom(r.Denom); err != nil {
		return fmt.Errorf("resolution record %w", err)
	}
	if r.Kind != ResolutionKind_RESOLUTION_KIND_WRITE_OFF &&
		r.Kind != ResolutionKind_RESOLUTION_KIND_RETIREMENT_RESIDUAL {
		return fmt.Errorf("resolution kind must be specified and known: %d", r.Kind)
	}
	if r.Version == 0 {
		return fmt.Errorf("resolution version must be positive")
	}
	if r.ResolutionHeight <= 0 {
		return fmt.Errorf("resolution height must be positive: %d", r.ResolutionHeight)
	}
	if err := r.OutstandingSupply.Validate(); err != nil {
		return fmt.Errorf("resolution outstanding supply is invalid: %w", err)
	}
	if !r.OutstandingSupply.IsPositive() {
		return fmt.Errorf("resolution outstanding supply must be positive: %s", r.OutstandingSupply)
	}
	if r.OutstandingSupply.Denom != r.Denom {
		return fmt.Errorf(
			"resolution outstanding supply denom %s must match asset denom %s",
			r.OutstandingSupply.Denom,
			r.Denom,
		)
	}
	if r.SettlementPlan == nil {
		return nil
	}
	if r.Kind != ResolutionKind_RESOLUTION_KIND_WRITE_OFF {
		return fmt.Errorf(
			"only a write-off resolution may carry final settlement terms: %s",
			r.Kind,
		)
	}
	if err := r.SettlementPlan.Validate(); err != nil {
		return fmt.Errorf("resolution settlement plan is invalid: %w", err)
	}
	if r.SettlementPlan.Denom != r.Denom {
		return fmt.Errorf(
			"resolution settlement plan denom %s must match asset denom %s",
			r.SettlementPlan.Denom,
			r.Denom,
		)
	}

	return nil
}

// Key returns the canonical ResolutionRecords collection key.
func (r ResolutionRecord) Key() collections.Pair[string, uint64] {
	return collections.Join(r.Denom, r.Version)
}
