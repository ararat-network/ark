// Code generation directive for the mocks used by the keeper tests:
//
//	mockgen -source=x/claims/types/expected_keepers.go -package=testutil -destination=x/claims/testutil/expected_keepers_mocks.go
package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	assettypes "github.com/ararat-network/ark/x/asset/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// AccountKeeper defines the auth functionality required by Claims.
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

// BankKeeper exposes Insurance custody reads and payouts only. Treasury owns funding transfers;
// Claims has no mint or burn dependency.
type BankKeeper interface {
	BlockedAddr(addr sdk.AccAddress) bool
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
}

// MarketKeeper buys what custody lacks for a stablecoin claim, with Insurance as the trader.
type MarketKeeper interface {
	Buy(ctx context.Context, trader sdk.AccAddress, ask sdk.Coin, offerDenom string, maximumOffer sdk.Coin) (sdk.Coin, sdk.Coin, sdk.DecCoin, error)
}

// OracleKeeper values committee claims in the reference unit.
type OracleKeeper interface {
	GetRateSet(ctx context.Context, denoms ...string) (oracletypes.RateSet, error)
	GetReferenceDenom(ctx context.Context) (string, error)
}

// AssetKeeper admits a claim's denomination and Insurance deposits.
type AssetKeeper interface {
	GetAsset(ctx context.Context, denom string) (assettypes.Asset, error)
	HasAsset(ctx context.Context, denom string) (bool, error)
}
