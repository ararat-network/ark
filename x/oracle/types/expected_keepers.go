package types

import (
	"context"

	"cosmossdk.io/core/address"
	"cosmossdk.io/core/store"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// StakingKeeper is expected keeper for staking module
type StakingKeeper interface {
	Validator(ctx context.Context, address sdk.ValAddress) stakingtypes.ValidatorI // get validator by operator address; nil when validator not found
	TotalBondedTokens(context.Context) math.Int                                    // total bonded tokens within the validator set
	Slash(context.Context, sdk.ConsAddress, int64, int64, math.LegacyDec)          // slash the validator and delegators of the validator, specifying offence height, offence power, and slash fraction
	Jail(context.Context, sdk.ConsAddress)                                         // jail a validator
	ValidatorsPowerStoreIterator(ctx context.Context) store.Iterator               // an iterator for the current validator power store
	MaxValidators(context.Context) uint32                                          // MaxValidators returns the maximum amount of bonded validators
	PowerReduction(ctx context.Context) (res math.Int)
	ValidatorAddressCodec() address.Codec

	// only used for simulation
	GetAllValidators(ctx context.Context) ([]stakingtypes.Validator, error)
}

// DistributionKeeper is expected keeper for distribution module
type DistributionKeeper interface {
	AllocateTokensToValidator(ctx context.Context, val stakingtypes.ValidatorI, tokens sdk.DecCoins)

	// only used for simulation
	GetValidatorOutstandingRewardsCoins(ctx context.Context, val sdk.ValAddress) sdk.DecCoins
}

// AccountKeeper is expected keeper for auth module
type AccountKeeper interface {
	GetModuleAddress(name string) sdk.AccAddress
	GetModuleAccount(ctx context.Context, moduleName string) sdk.ModuleAccountI
	GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI // only used for simulation
}

// BankKeeper defines the expected interface needed to retrieve account balances.
type BankKeeper interface {
	GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
	GetDenomMetaData(ctx context.Context, denom string) (banktypes.Metadata, bool)
	SetDenomMetaData(ctx context.Context, denomMetaData banktypes.Metadata)

	// only used for simulation
	SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins
}
