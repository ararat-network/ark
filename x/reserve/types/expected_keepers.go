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

// WasmKeeper answers whether the contract store holds code at an address,
// the one fact about a committee an appointment cannot read off the account.
type WasmKeeper interface {
	HasContractInfo(ctx context.Context, address sdk.AccAddress) bool
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

// TreasuryCapitalReader supplies the Reserve requirement and destination fund gaps.
// Post-construction wiring avoids the Treasury/Reserve injection cycle. Incomplete valuation blocks
// the disposal requirement; fund shortfalls use available liability.
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

// OracleKeeper provides strict current rates for proven movements and per-entry freshness windows
// for recognition. Policy writes require active underlying feeds.
type OracleKeeper interface {
	GetRateSet(ctx context.Context, denoms ...string) (oracletypes.RateSet, error)
	GetRateSetWithin(ctx context.Context, requests []oracletypes.RateRequest) (oracletypes.RateSet, error)
	FeedPhase(ctx context.Context, denom string) (oracletypes.FeedPhase, error)
}
