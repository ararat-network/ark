package app_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/ararat-network/ark/app"
)

// TestInitArkAppForTestnet pins the fork: three validators become one bonded
// at the operator with a seat's grant, every index agrees, and the next block
// carries no validator update because the app already agrees with the set
// the consensus rewrite installs.
func TestInitArkAppForTestnet(t *testing.T) {
	fixture := newExportFixture(t, 3)
	arkApp := fixture.app
	key := cmted25519.GenPrivKey()
	operator := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())

	power, err := app.InitArkAppForTestnet(arkApp, key.PubKey(), operator, "")
	require.NoError(t, err)
	require.Equal(t, sdk.TokensToConsensusPower(app.TestnetValidatorTokens, sdk.DefaultPowerReduction), power)

	ctx := sdk.NewContext(arkApp.CommitMultiStore(), cmtproto.Header{Height: arkApp.LastBlockHeight()}, false, arkApp.Logger())
	validators, err := arkApp.StakingKeeper.GetAllValidators(ctx)
	require.NoError(t, err)
	require.Len(t, validators, 1)
	require.Equal(t, sdk.ValAddress(operator).String(), validators[0].GetOperator())
	require.True(t, validators[0].IsBonded())
	require.Equal(t, app.TestnetValidatorTokens, validators[0].Tokens)
	last, err := arkApp.StakingKeeper.GetLastValidators(ctx)
	require.NoError(t, err)
	require.Len(t, last, 1)
	total, err := arkApp.StakingKeeper.GetLastTotalPower(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(power), total)
	consAddr := sdk.ConsAddress(key.PubKey().Address())
	byConsAddr, err := arkApp.StakingKeeper.GetValidatorByConsAddr(ctx, consAddr)
	require.NoError(t, err)
	require.Equal(t, validators[0].GetOperator(), byConsAddr.GetOperator())
	_, err = arkApp.SlashingKeeper.GetValidatorSigningInfo(ctx, consAddr)
	require.NoError(t, err)
	bondedPool := arkApp.AccountKeeper.GetModuleAddress(stakingtypes.BondedPoolName)
	require.Equal(t, app.TestnetValidatorTokens, arkApp.BankKeeper.GetBalance(ctx, bondedPool, sdk.DefaultBondDenom).Amount)
	require.Equal(t, app.TestnetValidatorFloat, arkApp.BankKeeper.GetBalance(ctx, operator, sdk.DefaultBondDenom).Amount)

	resp, err := arkApp.FinalizeBlock(&cmtabci.RequestFinalizeBlock{Height: arkApp.LastBlockHeight() + 1})
	require.NoError(t, err)
	require.Empty(t, resp.ValidatorUpdates)
	_, err = arkApp.Commit()
	require.NoError(t, err)
}

// An upgrade to trigger is scheduled for the first block of the testnet.
func TestInitArkAppForTestnetSchedulesTheUpgrade(t *testing.T) {
	fixture := newExportFixture(t, 1)
	arkApp := fixture.app
	operator := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())

	_, err := app.InitArkAppForTestnet(arkApp, cmted25519.GenPrivKey().PubKey(), operator, "v9")
	require.NoError(t, err)

	ctx := sdk.NewContext(arkApp.CommitMultiStore(), cmtproto.Header{Height: arkApp.LastBlockHeight()}, false, arkApp.Logger())
	plan, err := arkApp.UpgradeKeeper.GetUpgradePlan(ctx)
	require.NoError(t, err)
	require.Equal(t, "v9", plan.Name)
	require.Equal(t, arkApp.LastBlockHeight()+1, plan.Height)
}
