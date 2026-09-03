package types

import (
	"errors"
	"fmt"

	"cosmossdk.io/math"

	"github.com/ararat-network/ark/pkg/mandate"
)

// EconomicMandateLabel names the shared appointment envelope in
// economic-policy errors.
const EconomicMandateLabel = "economic mandate"

// DefaultEconomicMandate returns the canonical disabled committee
// mandate. The term may increase later while the mandate remains disabled.
func DefaultEconomicMandate() EconomicMandate {
	return NewDisabledEconomicMandate(0)
}

// NewDisabledEconomicMandate returns a canonical disabled mandate at the
// supplied term.
func NewDisabledEconomicMandate(term uint64) EconomicMandate {
	policy := DefaultEconomicPolicy()
	return EconomicMandate{
		Envelope:      mandate.Disabled(term),
		MinimumPolicy: policy,
		MaximumPolicy: policy,
	}
}

// Validate validates either a disabled mandate or one complete bounded
// committee appointment.
func (mandate EconomicMandate) Validate() error {
	if err := mandate.MinimumPolicy.Validate(); err != nil {
		return fmt.Errorf("invalid economic-policy minimum: %w", err)
	}
	if err := mandate.MaximumPolicy.Validate(); err != nil {
		return fmt.Errorf("invalid economic-policy maximum: %w", err)
	}

	if err := mandate.Envelope.Validate(); err != nil {
		return fmt.Errorf("%s: %w", EconomicMandateLabel, err)
	}

	if mandate.IsDisabled() {
		if !mandate.MinimumPolicy.Equal(mandate.MaximumPolicy) || !mandate.MinimumPolicy.IsZero() {
			return errors.New("disabled economic mandate must use identical zero bounds")
		}
		return nil
	}

	if err := mandate.ValidatePolicy(mandate.MinimumPolicy); err != nil {
		return fmt.Errorf("invalid economic-policy bounds: %w", err)
	}
	return nil
}

// ValidatePolicy checks a committee policy against every mandate bound.
//
// Two tables, split only by the type each bound compares. Every lever is
// clamped identically — inside [minimum, maximum], inclusive at both ends — so
// the rule lives once and each lever is a row naming itself; Dec and Int need
// separate tables because only their comparison differs.
//
// A lever missing from these tables is a lever the committee may set freely,
// which is why adding one is a row rather than a block: the omission would be
// invisible in a wall of near-identical conditionals.
func (mandate EconomicMandate) ValidatePolicy(policy EconomicPolicy) error {
	minimum, maximum := mandate.MinimumPolicy, mandate.MaximumPolicy

	for _, bound := range []struct {
		name    string
		value   math.LegacyDec
		minimum math.LegacyDec
		maximum math.LegacyDec
	}{
		{
			"transfer tax rate",
			policy.TransferTaxRate,
			minimum.TransferTaxRate,
			maximum.TransferTaxRate,
		},
		{
			"redemption Buffer target ratio",
			policy.RedemptionBufferTargetRatio,
			minimum.RedemptionBufferTargetRatio,
			maximum.RedemptionBufferTargetRatio,
		},
		{
			"strategic Reserve target ratio",
			policy.StrategicReserveTargetRatio,
			minimum.StrategicReserveTargetRatio,
			maximum.StrategicReserveTargetRatio,
		},
		{
			"insurance target ratio",
			policy.InsuranceTargetRatio,
			minimum.InsuranceTargetRatio,
			maximum.InsuranceTargetRatio,
		},
		{
			"exposure liability ratio weight",
			policy.LiabilityRatioWeight,
			minimum.LiabilityRatioWeight,
			maximum.LiabilityRatioWeight,
		},
		{
			"exposure volatility weight",
			policy.VolatilityWeight,
			minimum.VolatilityWeight,
			maximum.VolatilityWeight,
		},
		{
			"exposure flow weight",
			policy.FlowWeight,
			minimum.FlowWeight,
			maximum.FlowWeight,
		},
	} {
		if bound.value.LT(bound.minimum) || bound.value.GT(bound.maximum) {
			return fmt.Errorf(
				"%s %s is outside mandate range [%s, %s]",
				bound.name,
				bound.value,
				bound.minimum,
				bound.maximum,
			)
		}
	}

	for _, bound := range []struct {
		name    string
		value   math.Int
		minimum math.Int
		maximum math.Int
	}{
		{
			"validator block reward target",
			policy.ValidatorBlockRewardTarget,
			minimum.ValidatorBlockRewardTarget,
			maximum.ValidatorBlockRewardTarget,
		},
		{
			"oracle block reward target",
			policy.OracleBlockRewardTarget,
			minimum.OracleBlockRewardTarget,
			maximum.OracleBlockRewardTarget,
		},
	} {
		if bound.value.LT(bound.minimum) || bound.value.GT(bound.maximum) {
			return fmt.Errorf(
				"%s %s is outside mandate range [%s, %s]",
				bound.name,
				bound.value,
				bound.minimum,
				bound.maximum,
			)
		}
	}

	return nil
}
