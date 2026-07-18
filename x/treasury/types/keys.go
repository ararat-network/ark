package types

import "cosmossdk.io/collections"

const (
	ModuleName = "treasury"
	StoreKey   = ModuleName

	SubsidyPoolName           = "treasury_subsidy_pool"
	RedemptionBufferName      = "treasury_redemption_buffer"
	StrategicReserveName      = "treasury_strategic_reserve"
	InsuranceName             = "treasury_insurance"
	StabilityTaxCollectorName = "stability_tax_collector"
)

var (
	ParamsKey              = collections.NewPrefix(0)
	TaxCapsKey             = collections.NewPrefix(1)
	ClaimsMandateKey       = collections.NewPrefix(2)
	InsuranceReservedKey   = collections.NewPrefix(3)
	ClaimsKey              = collections.NewPrefix(4)
	RewardFundingKey       = collections.NewPrefix(5)
	MonetaryMandateKey     = collections.NewPrefix(6)
	MonetaryPolicyKey      = collections.NewPrefix(7)
	ClaimsAllowanceUsedKey = collections.NewPrefix(8)
	NextClaimIDKey         = collections.NewPrefix(9)
)

// FundAccountNames returns all launch Treasury custody module accounts.
func FundAccountNames() []string {
	return []string{SubsidyPoolName, RedemptionBufferName, StrategicReserveName, InsuranceName}
}
