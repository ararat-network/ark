package types

import (
	"context"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"

	oracletypes "noah/x/oracle/types"
)

// AccountKeeper expected account keeper
type AccountKeeper interface {
	GetModuleAddress(name string) sdk.AccAddress
	GetModuleAccount(ctx context.Context, moduleName string) sdk.ModuleAccountI
}

// BankKeeper expected bank keeper
type BankKeeper interface {
	MintCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
	BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
	GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	GetSupply(ctx context.Context, denom string) sdk.Coin
}

// MarketKeeper expected market keeper
type MarketKeeper interface {
	ComputeOracleRate(ctx context.Context, offerCoin sdk.DecCoin, askDenom string) (sdk.DecCoin, error)
}

// StakingKeeper expected keeper for staking module
type StakingKeeper interface {
	TotalBondedTokens(context.Context) math.Int // total bonded tokens within the validator set
}

// DistributionKeeper expected keeper for distribution module
type DistributionKeeper interface {
	GetFeePool(ctx context.Context) (feePool distrtypes.FeePool)
	SetFeePool(ctx context.Context, feePool distrtypes.FeePool)
}

// OracleKeeper defines expected oracle keeper
type OracleKeeper interface {
	Whitelist(ctx context.Context) (res oracletypes.DenomList)

	// only used for test purpose
	SetArkExchangeRate(ctx context.Context, denom string, exchangeRate math.LegacyDec)
	SetWhitelist(ctx context.Context, whitelist oracletypes.DenomList)
}
