package types

import (
	"context"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	assettypes "ark/x/asset/types"
	oracletypes "ark/x/oracle/types"
)

// ClaimsKeeper reports what x/claims' committee-operated fund is worth toward
// the capital requirement Treasury computes for it, per the redesign plan's
// §7.2 split: Treasury owns the requirement (target ratio times covered
// exposure) and each operating module owns its own recognised capital, so
// Treasury never reads a claim record and never pays one. The signature is
// written twice rather than shared through one embedded interface because
// depinject resolves ModuleInputs fields by type, and two fields of one
// interface type would bind to the same keeper. The named types plus
// depinject.BindInterface in app wiring are what disambiguate.
type ClaimsKeeper interface {
	RecognisedCapital(ctx context.Context) (math.Int, error)
}

// ReserveKeeper reports what x/reserve's committee-operated fund is worth
// toward its capital requirement. See ClaimsKeeper for the accounting contract
// both funds answer to.
//
// The interface only ever reports a number. Treasury credits the fund through
// Bank exactly as it credits the other two, and tells it nothing: principal
// that lands under an incomplete valuation is ordinary custody the moment it
// arrives, so the fund has nothing to record that its balance does not already
// say. What is worth knowing — that a credit was never sized against targets —
// belongs to the act, and EventLiabilityIncomplete carries it.
type ReserveKeeper interface {
	RecognisedCapital(ctx context.Context) (math.Int, error)
}

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
// and the protocol reference its tax cap is denominated in. Membership and
// valuation questions belong to the asset registry instead. Cap derivation is
// deliberately feed-based — the reference unit need not be a registered
// member — and takes the available read, so a stale feed defers a cap rather
// than failing a block or a governance act.
type OracleKeeper interface {
	GetAvailableRateSet(ctx context.Context, denoms ...string) (oracletypes.RateSet, error)
	GetReferenceDenom(ctx context.Context) (string, error)
}

// AssetKeeper defines the lifecycle authority Treasury derives its liability
// partition and cap membership from; Treasury stores rate policy and fund
// state only, never a membership set. The tax base is deliberately not read
// from here: it is the cap set, which outlives membership. Pricing verdicts
// are asked of the registry rather than assembled here — Treasury owns only
// what to do about the answer.
// HasAsset is membership rather than lifecycle status, and it is read at
// genesis alone: it answers what the protocol has ever issued, which is the
// question a seeded cap and a seeded collector balance must both survive.
type AssetKeeper interface {
	Pricings(ctx context.Context, denoms ...string) (assettypes.AssetPricings, error)
	PricedAssets(ctx context.Context) ([]string, assettypes.AssetPricings, error)
	OraclePricedDenoms(ctx context.Context) ([]string, error)
	HasAsset(ctx context.Context, denom string) (bool, error)
}
