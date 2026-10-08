package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	assettypes "github.com/ararat-network/ark/x/asset/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// AccountKeeper resolves the custody address, creates the custody accounts at genesis, and reads the
// committee account an appointment observes.
type AccountKeeper interface {
	GetModuleAddress(string) sdk.AccAddress
	GetModuleAccount(ctx context.Context, moduleName string) sdk.ModuleAccountI
	GetAccount(context.Context, sdk.AccAddress) sdk.AccountI
}

// WasmKeeper answers whether the contract store holds code at the committee address, the one fact
// an appointment cannot read off the account.
type WasmKeeper interface {
	HasContractInfo(context.Context, sdk.AccAddress) bool
}

// BankKeeper only exposes custody reads and funded payments.
type BankKeeper interface {
	BlockedAddr(sdk.AccAddress) bool
	GetBalance(context.Context, sdk.AccAddress, string) sdk.Coin
	GetAllBalances(context.Context, sdk.AccAddress) sdk.Coins
	SendCoinsFromModuleToAccount(context.Context, string, sdk.AccAddress, sdk.Coins) error
}

// DistributionKeeper returns idle principal through community-pool accounting.
type DistributionKeeper interface {
	FundCommunityPool(context.Context, sdk.Coins, sdk.AccAddress) error
}

// MarketKeeper executes the buy that funds a stablecoin award.
type MarketKeeper interface {
	Buy(ctx context.Context, trader sdk.AccAddress, ask sdk.Coin, offerDenom string, maximumOffer sdk.Coin) (sdk.Coin, sdk.Coin, sdk.DecCoin, error)
}

// OracleKeeper values a compensation award in the reference unit at the fresh rates Market quotes with.
type OracleKeeper interface {
	GetRateSet(ctx context.Context, denoms ...string) (oracletypes.RateSet, error)
	GetReferenceDenom(ctx context.Context) (string, error)
}

// AssetKeeper validates compensation denominations, admits registered deposits, and lists the registry
// the balance query walks.
type AssetKeeper interface {
	GetAsset(ctx context.Context, denom string) (assettypes.Asset, error)
	HasAsset(ctx context.Context, denom string) (bool, error)
	ListAssets(ctx context.Context) ([]assettypes.Asset, error)
}
