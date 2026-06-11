package types

import (
	"context"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// StakingKeeper is expected keeper for staking module
type StakingKeeper interface {
	Validator(ctx context.Context, address sdk.ValAddress) (stakingtypes.ValidatorI, error) // get validator by operator address; nil when validator not found
	Slash(context.Context, sdk.ConsAddress, int64, int64, math.LegacyDec) (math.Int, error) // slash the validator and delegators of the validator, specifying offence height, offence power, and slash fraction
	Jail(context.Context, sdk.ConsAddress) error                                            // jail a validator
	PowerReduction(ctx context.Context) (res math.Int)
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
	GetDenomMetaData(ctx context.Context, denom string) (banktypes.Metadata, bool)
	SetDenomMetaData(ctx context.Context, denomMetaData banktypes.Metadata)
}
