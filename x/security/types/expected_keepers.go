package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
)

// Both interfaces here are read-only: every write the committee causes travels
// back out through the message router.

// AccountKeeper defines the auth functionality required by Security, which is
// only the committee account an appointment observes.
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

// UpgradeKeeper reads the single pending upgrade plan, which decides whether a
// committee schedule replaces its own plan or displaces a governance one.
type UpgradeKeeper interface {
	GetUpgradePlan(ctx context.Context) (upgradetypes.Plan, error)
}
