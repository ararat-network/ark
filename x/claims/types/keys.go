package types

import "cosmossdk.io/collections"

const (
	ModuleName = "claims"
	StoreKey   = ModuleName

	// InsuranceName is the custody account claims are paid from. Its derived
	// module address follows this string, so it is not interchangeable with the
	// Treasury-owned fund accounts.
	InsuranceName = "claims_insurance"
)

var (
	ParamsKey              = collections.NewPrefix(0)
	ClaimsMandateKey       = collections.NewPrefix(1)
	ClaimsAllowanceUsedKey = collections.NewPrefix(2)
	InsuranceReservedKey   = collections.NewPrefix(3)
	NextClaimIDKey         = collections.NewPrefix(4)
	ClaimsKey              = collections.NewPrefix(5)
	// DueClaimsKey indexes pending claims by the height they become payable,
	// which is the order EndBlock settles them in. It is derived state: genesis
	// rebuilds it from the pending claims rather than importing it.
	DueClaimsKey = collections.NewPrefix(6)
)
