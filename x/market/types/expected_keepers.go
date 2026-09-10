package types

import (
	"context"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	assettypes "github.com/ararat-network/ark/x/asset/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// AccountKeeper is expected keeper for auth module
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

// BankKeeper defines expected supply keeper
type BankKeeper interface {
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error

	BurnCoins(ctx context.Context, name string, amt sdk.Coins) error
	MintCoins(ctx context.Context, name string, amt sdk.Coins) error
}

// OracleKeeper defines expected oracle keeper.
type OracleKeeper interface {
	GetRateSet(ctx context.Context, denoms ...string) (oracletypes.RateSet, error)
	GetReferenceDenom(ctx context.Context) (string, error)
}

// AssetKeeper defines the lifecycle authority Market derives eligibility from.
type AssetKeeper interface {
	GetAsset(ctx context.Context, denom string) (assettypes.Asset, error)
	ActiveSettlementPlan(ctx context.Context, denom string) (assettypes.SettlementPlan, bool, error)
	OraclePricedDenoms(ctx context.Context) ([]string, error)
}

// TreasuryKeeper defines the allocation and liability accounting required by
// Market settlement.
type TreasuryKeeper interface {
	// SettleConversions places one block's recorded conversion flow and returns
	// the NOAH Market must burn to finish it: principal that overflowed every
	// funded target, plus the Buffer's coverage of output already minted.
	//
	// It is called once, from Market's EndBlocker, after every conversion in the
	// block has minted, burned, and paid its trader. Market never learns how
	// Treasury split the principal between the funds, so settlement cannot
	// branch on economic policy; Treasury never learns which conversions
	// produced the totals, so allocation cannot favour one.
	SettleConversions(ctx context.Context, totals ConversionTotals) (math.Int, error)
}
