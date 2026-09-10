package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// AccountKeeper defines the auth functionality required by Asset, which is
// only the committee account an emergency appointment observes.
type AccountKeeper interface {
	// GetAccount resolves the committee account whose shape an appointment
	// records. It returns nil for an address holding no account.
	GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI
}

// WasmKeeper answers whether the contract store holds code at an address,
// the one fact about a committee an appointment cannot read off the account.
type WasmKeeper interface {
	HasContractInfo(ctx context.Context, address sdk.AccAddress) bool
}

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
