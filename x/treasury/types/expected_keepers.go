package types

import (
	"context"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	oracletypes "ark/x/oracle/types"
)

// AccountKeeper expected account keeper
type AccountKeeper interface {
	GetModuleAddress(name string) sdk.AccAddress
	GetModuleAccount(ctx context.Context, moduleName string) sdk.ModuleAccountI
}

// BankKeeper expected bank keeper
type BankKeeper interface {
	MintCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
	GetSupply(ctx context.Context, denom string) sdk.Coin
}

// StakingKeeper expected keeper for staking module
type StakingKeeper interface {
	TotalValidatorPower(context.Context) (math.Int, error) // total bonded tokens within the validator set
}

// ProtocolpoolKeeper expected keeper for distribution module
type ProtocolpoolKeeper interface {
	FundCommunityPool(ctx context.Context, amount sdk.Coins, sender sdk.AccAddress) error
}

// OracleKeeper defines expected oracle keeper
type OracleKeeper interface {
	GetRateSnapshot(ctx context.Context, denoms ...string) (oracletypes.RateSnapshot, error)
	GetTobinTaxes(ctx context.Context) (res oracletypes.TobinTaxes, err error)
}
