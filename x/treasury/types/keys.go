package types

import "cosmossdk.io/collections"

const (
	ModuleName = "treasury"
	StoreKey   = ModuleName

	SubsidyPoolName          = "treasury_subsidy_pool"
	RedemptionBufferName     = "treasury_redemption_buffer"
	TransferTaxCollectorName = "transfer_tax_collector"
)

var (
	ParamsKey                 = collections.NewPrefix(0)
	ConversionFactorsKey      = collections.NewPrefix(1)
	RewardFundingKey          = collections.NewPrefix(2)
	EconomicMandateKey        = collections.NewPrefix(3)
	EconomicPolicyKey         = collections.NewPrefix(4)
	ExposureStateKey          = collections.NewPrefix(6)
	ExposureRefreshPendingKey = collections.NewPrefix(7)
	BaseGasPriceKey           = collections.NewPrefix(8)
)

// FundAccountNames returns the custody accounts Treasury itself operates.
// Insurance and the strategic Reserve are deliberately absent: each belongs to
// the module whose committee operates it. Treasury still credits both during
// the expansion waterfall, which needs only their names.
func FundAccountNames() []string {
	return []string{SubsidyPoolName, RedemptionBufferName}
}
