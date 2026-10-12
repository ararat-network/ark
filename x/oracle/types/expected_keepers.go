// SPDX-License-Identifier: Apache-2.0
// Originates from Ark's Terra Classic port of x/oracle/types/expected_keeper.go.
// Modified for Ark: keeper interfaces for the modern module boundaries.
// See NOTICE and THIRD_PARTY_NOTICES.md for upstream attribution.

package types

import (
	"context"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// StakingKeeper is expected keeper for staking module
type StakingKeeper interface {
	Validator(ctx context.Context, address sdk.ValAddress) (stakingtypes.ValidatorI, error)
	Jail(context.Context, sdk.ConsAddress) error
	ValidatorByConsAddr(context.Context, sdk.ConsAddress) (stakingtypes.ValidatorI, error)
}

// SlashingKeeper holds an attendance jail until a time, which MsgUnjail enforces.
type SlashingKeeper interface {
	JailUntil(context.Context, sdk.ConsAddress, time.Time) error
}

// DistributionKeeper is expected keeper for distribution module
type DistributionKeeper interface {
	AllocateTokensToValidator(ctx context.Context, val stakingtypes.ValidatorI, tokens sdk.DecCoins) error
}

// AccountKeeper is expected keeper for auth module
type AccountKeeper interface {
	GetModuleAddress(name string) sdk.AccAddress
	GetModuleAccount(ctx context.Context, moduleName string) sdk.ModuleAccountI
}

// BankKeeper defines the expected interface needed to retrieve account balances.
type BankKeeper interface {
	GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
}

// ReferenceRebaser re-expresses a consumer's reference-valued state when the protocol reference
// moves. Every consumer receives the one rate pair Oracle priced, applies no freshness policy of
// its own, and runs in the reference-change transaction; its error fails the whole action.
type ReferenceRebaser interface {
	RebaseReference(ctx context.Context, from string, to string, rates RateSet) error
}
