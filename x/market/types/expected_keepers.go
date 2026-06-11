package types

import (
	"context"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AccountKeeper is expected keeper for auth module
type AccountKeeper interface {
	GetModuleAddress(name string) sdk.AccAddress
	GetModuleAccount(ctx context.Context, moduleName string) sdk.ModuleAccountI
}

// BankKeeper defines expected supply keeper
type BankKeeper interface {
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error

	BurnCoins(ctx context.Context, name string, amt sdk.Coins) error
	MintCoins(ctx context.Context, name string, amt sdk.Coins) error
}

// OracleKeeper defines expected oracle keeper
type OracleKeeper interface {
	GetExchangeRate(ctx context.Context, denom string) (price math.LegacyDec, err error)
	GetTobinTax(ctx context.Context, denom string) (tobinTax math.LegacyDec, err error)

	// only used for simulation
	GetActives(ctx context.Context) ([]string, error)
}
