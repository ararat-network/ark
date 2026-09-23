package types

import (
	"context"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	assettypes "github.com/ararat-network/ark/x/asset/types"
)

// AccountKeeper resolves the custody address and the committee account an appointment observes.
type AccountKeeper interface {
	GetModuleAddress(string) sdk.AccAddress
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
	SendCoinsFromModuleToAccount(context.Context, string, sdk.AccAddress, sdk.Coins) error
}

// DistributionKeeper returns idle principal through community-pool accounting.
type DistributionKeeper interface {
	FundCommunityPool(context.Context, sdk.Coins, sdk.AccAddress) error
}

// StakingKeeper supplies the live bonded denominator for ownership payments.
type StakingKeeper interface {
	TotalValidatorPower(context.Context) (math.Int, error)
}

// AssetReader is the public collection used to validate new compensation assets.
type AssetReader = collections.Map[string, assettypes.Asset]
