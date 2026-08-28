// Code generation directive for the mocks used by the keeper tests:
//
//	mockgen -source=x/reserve/types/expected_keepers.go -package=testutil -destination=x/reserve/testutil/expected_keepers_mocks.go
package types

import (
	"context"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// AccountKeeper defines the auth functionality required by Reserve.
type AccountKeeper interface {
	// GetAccount resolves the committee account whose shape an appointment
	// records. It returns nil for an address holding no account.
	GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI
	GetModuleAddress(name string) sdk.AccAddress
	GetModuleAccount(ctx context.Context, moduleName string) sdk.ModuleAccountI
}

// BankKeeper defines the custody and transfer functionality required by
// Reserve.
type BankKeeper interface {
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	BurnCoins(ctx context.Context, name string, amt sdk.Coins) error
}

// AssetKeeper defines the registry membership read Reserve needs. Membership
// is the whole question — lifecycle status is not, because a written-off or
// retired asset is still Ark-issued.
type AssetKeeper interface {
	HasAsset(ctx context.Context, denom string) (bool, error)
}

// TreasuryCapitalReader reports the figures Treasury derives from its own
// targets, which bound every committee act that moves capital. It is wired
// after construction rather than injected, because Treasury already injects
// this module and depinject cannot close that loop. All three methods refuse
// under an incomplete aggregate valuation, because a target nobody can size
// is not a bound.
type TreasuryCapitalReader interface {
	RequiredReserveCapital(ctx context.Context) (math.Int, error)
	// RedemptionBufferShortfall reports how far the Buffer falls below its
	// target, and zero once it does not.
	RedemptionBufferShortfall(ctx context.Context) (math.Int, error)
	// InsuranceShortfall reports how far Insurance falls below its target,
	// measured against its recognised capital rather than its raw balance,
	// because an approved pending claim already encumbers the fund.
	InsuranceShortfall(ctx context.Context) (math.Int, error)
}

// OracleKeeper defines the pricing functionality required by Reserve. Proven
// coin movements are priced at conversion grade through the all-or-nothing
// read, because a movement derives exactly one value per denomination it names
// and has no unvalued outcome to fall back to; recognition credit is priced
// through the windowed read, one request per eligibility entry carrying the
// entry's own denomination and tolerance. FeedPhase is asked at the policy
// write alone.
type OracleKeeper interface {
	GetRateSet(ctx context.Context, denoms ...string) (oracletypes.RateSet, error)
	GetRateSetWithin(ctx context.Context, requests []oracletypes.RateRequest) (oracletypes.RateSet, error)
	FeedPhase(ctx context.Context, denom string) (oracletypes.FeedPhase, error)
}
