package types

import (
	"context"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	oracletypes "ark/x/oracle/types"
	treasurytypes "ark/x/treasury/types"
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
	GetRateSnapshot(ctx context.Context, denoms ...string) (oracletypes.RateSnapshot, error)
	GetTobinTax(ctx context.Context, denom string) (tobinTax math.LegacyDec, err error)
	GetTobinTaxes(ctx context.Context) ([]oracletypes.TobinTax, error)

	// only used for simulation
	GetExchangeRates(ctx context.Context) (sdk.DecCoins, error)
}

// TreasuryKeeper defines the allocation and liability accounting required by
// Market settlement.
type TreasuryKeeper interface {
	RouteExpansion(ctx context.Context, grossOffer sdk.Coin, stableOutput sdk.Coin, quoteRates oracletypes.RateSnapshot) (treasurytypes.ExpansionAllocation, error)
	DrawRedemptionBuffer(ctx context.Context, redeemedStable sdk.Coin, noahOutput math.Int, quoteRates oracletypes.RateSnapshot) (treasurytypes.BufferDraw, error)
	RecordSupplyChange(ctx context.Context, burned sdk.Coin, minted sdk.Coin, quoteRates oracletypes.RateSnapshot) error
}
