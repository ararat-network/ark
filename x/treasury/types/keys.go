package types

import "cosmossdk.io/collections"

const (
	ModuleName = "treasury"
	StoreKey   = ModuleName

	SubsidyPoolName           = "treasury_subsidy_pool"
	RedemptionBufferName      = "treasury_redemption_buffer"
	StabilityTaxCollectorName = "stability_tax_collector"
)

var (
	ParamsKey                 = collections.NewPrefix(0)
	TaxCapsKey                = collections.NewPrefix(1)
	RewardFundingKey          = collections.NewPrefix(2)
	MonetaryMandateKey        = collections.NewPrefix(3)
	MonetaryPolicyKey         = collections.NewPrefix(4)
	TaxCapRefreshPendingKey   = collections.NewPrefix(5)
	ExposureStateKey          = collections.NewPrefix(6)
	ExposureRefreshPendingKey = collections.NewPrefix(7)
)

// FundAccountNames returns the custody accounts Treasury itself operates.
// Insurance and the strategic Reserve are deliberately absent: each belongs to
// the module whose committee operates it. Treasury still credits both during
// the expansion waterfall, which needs only their names.
func FundAccountNames() []string {
	return []string{SubsidyPoolName, RedemptionBufferName}
}
