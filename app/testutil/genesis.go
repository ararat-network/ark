package testutil

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	cmtsecp256k1 "github.com/cometbft/cometbft/crypto/secp256k1"
	cmttypes "github.com/cometbft/cometbft/types"

	"github.com/cosmos/cosmos-sdk/codec"
	sdksecp256k1 "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// Validators is a genesis validator set together with the consensus keys behind
// it, so a fixture can address or sign as a validator after genesis.
type Validators struct {
	Set  *cmttypes.ValidatorSet
	Keys []cmtsecp256k1.PrivKey
}

// NewValidators returns count equal-power validators.
func NewValidators(tb testing.TB, count int) Validators {
	tb.Helper()
	require.Positive(tb, count)

	keys := make([]cmtsecp256k1.PrivKey, count)
	validators := make([]*cmttypes.Validator, count)
	for i := range keys {
		keys[i] = cmtsecp256k1.GenPrivKey()
		validators[i] = cmttypes.NewValidator(keys[i].PubKey(), 1)
	}

	return Validators{Set: cmttypes.NewValidatorSet(validators), Keys: keys}
}

// Validator returns validator i as the consensus engine sees it.
func (v Validators) Validator(i int) *cmttypes.Validator { return v.Set.Validators[i] }

// ConsAddress returns validator i's consensus address.
func (v Validators) ConsAddress(i int) sdk.ConsAddress {
	return sdk.ConsAddress(v.Keys[i].PubKey().Address())
}

// ConsAddresses returns every validator's consensus address, in set order.
func (v Validators) ConsAddresses() []sdk.ConsAddress {
	addrs := make([]sdk.ConsAddress, len(v.Keys))
	for i := range v.Keys {
		addrs[i] = v.ConsAddress(i)
	}
	return addrs
}

// Count returns the number of validators in the set.
func (v Validators) Count() int { return len(v.Keys) }

// Funder is a genesis account, its signing key, and its opening balance.
type Funder struct {
	Account *authtypes.BaseAccount
	Key     *sdksecp256k1.PrivKey
	Balance banktypes.Balance
}

// NewFunder returns a genesis account holding coins.
func NewFunder(tb testing.TB, coins sdk.Coins) Funder {
	tb.Helper()

	key := sdksecp256k1.GenPrivKey()
	account := authtypes.NewBaseAccount(key.PubKey().Address().Bytes(), key.PubKey(), 0, 0)

	return Funder{
		Account: account,
		Key:     key,
		Balance: banktypes.Balance{Address: account.GetAddress().String(), Coins: coins},
	}
}

// Address returns the funder's account address.
func (f Funder) Address() sdk.AccAddress { return f.Account.GetAddress() }

// Accounts wraps the funder for GenesisStateWithValSet, which takes a slice.
func (f Funder) Accounts() []authtypes.GenesisAccount {
	return []authtypes.GenesisAccount{f.Account}
}

// CorrectBondedPool rewrites Bank's bonded-pool balance to cover every
// validator's bond. GenesisStateWithValSet counts every validator's bond in
// total supply but seeds the pool with a single bond, so a multi-validator
// genesis fails Bank's supply check without this.
func CorrectBondedPool(tb testing.TB, cdc codec.Codec, genesisState map[string]json.RawMessage, validatorCount int) {
	tb.Helper()

	var bankGenesis banktypes.GenesisState
	cdc.MustUnmarshalJSON(genesisState[banktypes.ModuleName], &bankGenesis)

	bondedPool := authtypes.NewModuleAddress(stakingtypes.BondedPoolName).String()
	for i := range bankGenesis.Balances {
		if bankGenesis.Balances[i].Address != bondedPool {
			continue
		}
		bankGenesis.Balances[i].Coins = sdk.NewCoins(sdk.NewCoin(
			sdk.DefaultBondDenom,
			sdk.DefaultPowerReduction.MulRaw(int64(validatorCount)),
		))
		break
	}

	genesisState[banktypes.ModuleName] = cdc.MustMarshalJSON(&bankGenesis)
}

// SeedSigningInfos gives every listed validator slashing signing info starting
// at startHeight. Blocks carry the previous commit, so a validator needs
// signing info before slashing sees it.
func SeedSigningInfos(
	tb testing.TB,
	cdc codec.Codec,
	genesisState map[string]json.RawMessage,
	consAddrs []sdk.ConsAddress,
	startHeight int64,
) {
	tb.Helper()

	var slashingGenesis slashingtypes.GenesisState
	cdc.MustUnmarshalJSON(genesisState[slashingtypes.ModuleName], &slashingGenesis)

	slashingGenesis.SigningInfos = make([]slashingtypes.SigningInfo, len(consAddrs))
	for i, consAddr := range consAddrs {
		slashingGenesis.SigningInfos[i] = slashingtypes.SigningInfo{
			Address: consAddr.String(),
			ValidatorSigningInfo: slashingtypes.NewValidatorSigningInfo(
				consAddr,
				startHeight,
				0,
				time.Unix(0, 0).UTC(),
				false,
				0,
			),
		}
	}

	genesisState[slashingtypes.ModuleName] = cdc.MustMarshalJSON(&slashingGenesis)
}
