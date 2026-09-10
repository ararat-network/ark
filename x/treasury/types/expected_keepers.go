package types

import (
	"context"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	assettypes "github.com/ararat-network/ark/x/asset/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// ClaimsKeeper reports Insurance capital while Treasury owns its requirement. Separate named
// interfaces and app depinject bindings distinguish Claims from Reserve despite identical method
// sets; Treasury neither reads nor pays claims.
type ClaimsKeeper interface {
	RecognisedCapital(ctx context.Context) (math.Int, error)
}

// ReserveKeeper reports recognised Reserve capital only. Treasury credits custody through Bank;
// degraded allocation is disclosed by EventLiabilityIncomplete rather than a second Reserve-side
// state record.
type ReserveKeeper interface {
	RecognisedCapital(ctx context.Context) (math.Int, error)
}

// AccountKeeper defines the auth functionality required by Treasury.
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

// BankKeeper defines the custody, supply, and transfer functionality required
// by Treasury. Treasury deliberately has no mint or burn dependency.
type BankKeeper interface {
	GetSupply(ctx context.Context, denom string) sdk.Coin
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
}

// OracleKeeper supplies rates and the protocol reference. Available-rate reads let factor refresh
// defer unavailable crosses; Asset owns membership and valuation verdicts.
type OracleKeeper interface {
	GetAvailableRateSet(ctx context.Context, denoms ...string) (oracletypes.RateSet, error)
	GetReferenceDenom(ctx context.Context) (string, error)
}

// AssetKeeper supplies lifecycle, pricing verdicts, and permanent membership. Treasury retains
// factors across pricing-status changes; genesis uses HasAsset to validate factors and collector
// custody against issued denominations.
type AssetKeeper interface {
	Pricings(ctx context.Context, denoms ...string) (assettypes.AssetPricings, error)
	PricedAssets(ctx context.Context) ([]string, assettypes.AssetPricings, error)
	OraclePricedDenoms(ctx context.Context) ([]string, error)
	HasAsset(ctx context.Context, denom string) (bool, error)
}
