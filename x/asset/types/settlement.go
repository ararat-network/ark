package types

import (
	"fmt"

	"cosmossdk.io/collections"

	"github.com/ararat-network/ark/pkg/chain"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// Validate checks a settlement plan's denomination, redemption rate, opening, and
// times.
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
	// Held to the store bound like every rate a quantity is multiplied by:
	// the plan sits beside oracle rates in the registry's rate sets, and a
	// 2^128-capped supply times a rate past MaxExchangeRate leaves the Dec
	// domain in the folds that value it.
	if p.RedemptionRate.GT(oracletypes.MaxExchangeRate) {
		return fmt.Errorf(
			"settlement redemption rate %s exceeds %s",
			p.RedemptionRate,
			oracletypes.MaxExchangeRate,
		)
	}
	if p.OpenedHeight <= 0 {
		return fmt.Errorf("settlement opened height must be positive: %d", p.OpenedHeight)
	}
	if p.OpenedTime == 0 {
		return fmt.Errorf("settlement opened time must be positive")
	}
	if p.OpenedTime >= p.ActivationTime {
		return fmt.Errorf(
			"settlement opened time %d must precede activation time %d",
			p.OpenedTime,
			p.ActivationTime,
		)
	}
	// The window is mandatory, so every plan carries a guarantee holders can
	// state: redemption opens at the activation time and cannot be
	// derecognized before this one.
	if p.EarliestClosingTime <= p.ActivationTime {
		return fmt.Errorf(
			"settlement earliest closing time %d must be after activation time %d",
			p.EarliestClosingTime,
			p.ActivationTime,
		)
	}
	// A deadline past the header's int64 is one no block reaches.
	if p.EarliestClosingTime > chain.MaxBlockSeconds {
		return fmt.Errorf("settlement earliest closing time %d exceeds the header's int64", p.EarliestClosingTime)
	}
	return nil
}

// IsActive reports whether the plan's activation delay has elapsed at block time now, in Unix
// seconds. It is the single question that separates an announced plan from a binding one, asked by
// the redemption gate and the cancellation window alike.
func (p SettlementPlan) IsActive(now uint64) bool {
	return now >= p.ActivationTime
}

// MaxSettlementWindow caps a plan's redemption window at five years: a domain bound with no
// policy meaning, so activation plus the window stays far inside the header's int64.
const MaxSettlementWindow = uint64(5 * 365 * 86_400)

// ValidateSettlementWindow requires a positive, bounded window: a plan with none would be one
// WriteOffAsset could end the block after it activates.
func ValidateSettlementWindow(window uint64) error {
	if window == 0 || window > MaxSettlementWindow {
		return fmt.Errorf(
			"settlement window must be between one and %d seconds, is %d",
			MaxSettlementWindow,
			window,
		)
	}
	return nil
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
