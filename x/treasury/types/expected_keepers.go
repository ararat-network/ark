package types

import (
	"context"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	assettypes "ark/x/asset/types"
	oracletypes "ark/x/oracle/types"
)

// ClaimsKeeper reports what x/claims' committee-operated fund is worth toward
// the capital requirement Treasury computes for it.
//
// The split follows the redesign plan's §7.2 accounting contract:
//
//	required_capital   = target_ratio * covered_risk_exposure
//	recognised_capital = liquid_unencumbered_anoah + risk_adjusted_external
//	capital_gap        = max(required_capital - recognised_capital, 0)
//
// Treasury owns the left side, because it needs consolidated liability and the
// governed target ratio. The operating module owns the right side, because it
// needs the fund's own encumbrance, and later its eligibility entries,
// haircuts, and concentration caps. Assets held by a fund satisfy its
// requirement and never define it (§20.1), which is what keeps the two
// separable. Treasury never reads a claim record and never pays one.
//
// The return widens to a total/liquid pair when a fund's first non-NOAH asset
// is approved and §7.2's liquid_gap becomes real. Until then every recognised
// balance is immediately usable and the two are equal.
//
// ReserveKeeper states the same contract for x/reserve. The signature is
// written twice rather than shared through one embedded interface because
// depinject resolves ModuleInputs fields by type, and two fields of a single
// interface type would bind to the same keeper. Two named types are what make
// them separately addressable; app wiring then binds each to its keeper with
// depinject.BindInterface. The names alone are not sufficient — Go interfaces
// are structural, so both keepers satisfy both — which is why the bindings are
// what actually disambiguate.
type ClaimsKeeper interface {
	RecognisedCapital(ctx context.Context) (math.Int, error)
}

// ReserveKeeper reports what x/reserve's committee-operated fund is worth
// toward its capital requirement. See ClaimsKeeper for the accounting contract
// both funds answer to.
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
// and the protocol reference its tax cap is denominated in. Membership
// questions belong to the asset registry, and so does valuation — every path
// that prices registry supply asks the registry for verdicts instead.
//
// What remains is cap derivation, which is deliberately feed-based rather than
// verdict-based: the reference unit need not be a registered member, and a cap
// set is one coherent artefact restating the same parity per denom. It needs
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
	Pricings(ctx context.Context, overlay oracletypes.RateSet, denoms ...string) (assettypes.AssetPricings, error)
	PricedAssets(ctx context.Context, overlay oracletypes.RateSet) ([]string, assettypes.AssetPricings, error)
	OraclePricedDenoms(ctx context.Context) ([]string, error)
}
