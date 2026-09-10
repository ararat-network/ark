// Code generation directive for the mocks used by the keeper tests:
//
//	mockgen -source=x/claims/types/expected_keepers.go -package=testutil -destination=x/claims/testutil/expected_keepers_mocks.go
package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AccountKeeper defines the auth functionality required by Claims.
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

// BankKeeper defines the custody and transfer functionality required by
// Claims. Claims pays out of the Insurance account and never credits it: the
// expansion waterfall that funds Insurance lives in Treasury, so no
// module-to-module send is needed here. Claims deliberately has no mint or
// burn dependency.
type BankKeeper interface {
	BlockedAddr(addr sdk.AccAddress) bool
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
}
