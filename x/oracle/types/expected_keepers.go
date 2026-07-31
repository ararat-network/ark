package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// StakingKeeper is expected keeper for staking module
type StakingKeeper interface {
	Validator(ctx context.Context, address sdk.ValAddress) (stakingtypes.ValidatorI, error)
	Jail(context.Context, sdk.ConsAddress) error
	ValidatorByConsAddr(context.Context, sdk.ConsAddress) (stakingtypes.ValidatorI, error)
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

// MarketReferenceDenomKeeper re-denominates Market state held in reference units,
// the base pool and its delta, when the protocol reference moves. Oracle owns
// the replacement choice and the conversion rates — read once and fresh on both
// sides — so executors never invent freshness policy of their own. The call
// runs in the same transaction as the reference change and its error fails the
// whole action.
type MarketReferenceDenomKeeper interface {
	RebaseBasePool(ctx context.Context, from string, to string, rates RateSet) error
}

// TreasuryReferenceDenomKeeper re-expresses Treasury state held in reference
// units, the reference tax cap amount, when the protocol reference moves. It
// receives the same handed rates as Market's executor and runs in the same
// transaction as the reference change; its error fails the whole action.
type TreasuryReferenceDenomKeeper interface {
	RebaseTaxCap(ctx context.Context, from string, to string, rates RateSet) error
}
