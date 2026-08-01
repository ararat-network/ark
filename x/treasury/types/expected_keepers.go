package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	assettypes "ark/x/asset/types"
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

// OracleKeeper defines the pricing functionality required by Treasury: rates,
// and the protocol reference its tax cap is denominated in. Membership
// questions belong to the asset registry, and so does valuation — every path
// that prices registry supply asks the registry for verdicts instead.
//
// What remains is cap derivation, which is deliberately feed-based rather than
// verdict-based: the reference unit need not be a registered member, and a cap
// set is one coherent artifact restating the same parity per denom. It needs
// every requested rate or none, and it reports which rate failed and why on
// its skip event, so it takes the all-or-nothing read.
type OracleKeeper interface {
	GetRateSet(ctx context.Context, denoms ...string) (oracletypes.RateSet, error)
	GetReferenceDenom(ctx context.Context) (string, error)
}

// AssetKeeper defines the lifecycle authority Treasury derives its liability
// partition and cap membership from. Treasury stores rate policy and fund
// state only — never a membership set. The tax base is deliberately not read
// from here: it is the cap set, which outlives membership.
//
// Pricing verdicts are asked of the registry rather than assembled here.
// Whether a denomination is worth anything, and on whose authority, is
// lifecycle state; Treasury owns only what to do about the answer — defer,
// disclose, zero, or route.
type AssetKeeper interface {
	Pricings(ctx context.Context, overlay oracletypes.RateSet, denoms ...string) (assettypes.DenomPricings, error)
	ListAssets(ctx context.Context) ([]assettypes.Asset, error)
	PricedLiveDenoms(ctx context.Context) ([]string, error)
}
