package types

import (
	"context"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	assettypes "ark/x/asset/types"
	oracletypes "ark/x/oracle/types"
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
	// RouteExpansion returns the NOAH Market must burn to settle the escrow it
	// still holds: the quote spread plus any principal that overflowed every
	// funded target. Market never learns how Treasury split the rest between
	// the funds, so settlement cannot branch on monetary policy.
	RouteExpansion(ctx context.Context, grossOffer sdk.Coin, stableOutput sdk.Coin, quoteRates oracletypes.RateSet) (sdk.Coin, error)
	// DrawRedemptionBuffer returns the Buffer's payment toward noahOutput.
	// Market mints the remainder and never learns how Treasury sized it: the
	// liability figures behind the share stay execution-local, and settlement
	// must not branch on them.
	DrawRedemptionBuffer(ctx context.Context, redeemedStable sdk.Coin, noahOutput math.Int, quoteRates oracletypes.RateSet) (math.Int, error)
	RecordSupplyChange(ctx context.Context, burned sdk.Coin, minted sdk.Coin, quoteRates oracletypes.RateSet) error
}
