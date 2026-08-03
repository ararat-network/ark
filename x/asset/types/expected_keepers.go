package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	oracletypes "ark/x/oracle/types"
)

// BankKeeper defines the supply and metadata functionality required by Asset.
type BankKeeper interface {
	GetSupply(ctx context.Context, denom string) sdk.Coin
	SetDenomMetaData(ctx context.Context, metadata banktypes.Metadata)
	GetDenomMetaData(ctx context.Context, denom string) (banktypes.Metadata, bool)
}

// OracleKeeper defines the immutable pricing functionality required by Asset.
// x/asset reads feed state but never writes it: membership is oracle-owned
// governance. All calls are keyed by denomination, because a feed is keyed by
// the denomination it prices.
type OracleKeeper interface {
	GetAvailableRateSet(ctx context.Context, denoms ...string) (oracletypes.RateSet, error)
	// GetLastKnownRateSet ignores the freshness window, so it answers only what
	// a denomination was last worth — never what it may be transacted at. It
	// backs the LastRate disclosure on an unavailable-feed verdict, which
	// consumers totalling outstanding supply may read and consumers quoting or
	// paying may not.
	GetLastKnownRateSet(ctx context.Context, denoms ...string) (oracletypes.RateSet, error)
	FeedPhase(ctx context.Context, denom string) (oracletypes.FeedPhase, error)
}

// RegistryCacheInvalidator drops state a consumer derived from the asset
// registry and scoped to a single block.
//
// Almost no consumer needs this. Every lifecycle transition is a governance
// message, and x/gov executes those in the EndBlocker — after every transaction
// and after the only EndBlockers that follow it — so a block-scoped fold is
// safe by ordering alone and is rebuilt before anything reads it again.
//
// The emergency committee is the single exception: it acts in an ordinary
// transaction, so its transitions land while the block is still being read,
// leaving a consumer's fold describing a registry that no longer exists.
//
// Of its two powers only suspension invalidates, and the asymmetry is a fact
// about the fold rather than about the timing. Consumers partition supply by
// lifecycle status, and an issuance halt moves an asset from ACTIVE to
// ISSUANCE_HALTED — both inside oracle-priced membership — so a fold taken
// before it is still true after it. Suspension moves supply out of that
// membership, so it is not. A third committee power, or a halt that ever came
// to change the partition, would have to be re-examined against exactly this.
//
// The committee-signed message set is pinned by
// TestAutoCLIOptionsCoverAssetServices, which asserts the complete message set
// and names every command that reaches consensus outside x/gov's EndBlocker, so
// no new one ships unnoticed.
//
// Consumers depend on x/asset, so the reverse edge is injected at wiring rather
// than imported, the same shape as x/oracle's feed referent guards.
type RegistryCacheInvalidator interface {
	InvalidateRegistryCache(ctx context.Context) error
}
