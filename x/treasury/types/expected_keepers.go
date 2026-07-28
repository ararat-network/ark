package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	oracletypes "ark/x/oracle/types"
)

// AccountKeeper defines the auth functionality required by Treasury.
type AccountKeeper interface {
	GetModuleAddress(name string) sdk.AccAddress
	GetModuleAccount(ctx context.Context, moduleName string) sdk.ModuleAccountI
}

// BankKeeper defines the custody, supply, and transfer functionality required
// by Treasury. Treasury deliberately has no mint or burn dependency.
type BankKeeper interface {
	BlockedAddr(addr sdk.AccAddress) bool
	GetSupply(ctx context.Context, denom string) sdk.Coin
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
}

// OracleKeeper defines the immutable pricing and native-stable registry
// functionality required by Treasury.
type OracleKeeper interface {
	GetRateSet(ctx context.Context, denoms ...string) (oracletypes.RateSet, error)
	GetTobinTaxes(ctx context.Context) ([]oracletypes.TobinTax, error)
}
