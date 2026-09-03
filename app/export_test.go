package app

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	cmttypes "github.com/cometbft/cometbft/types"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/testutil/mock"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// exportFixture is a committed chain with validatorCount equal-power
// validators, ready to export.
type exportFixture struct {
	app        *ArkApp
	validators []*cmttypes.Validator
}

func newExportFixture(t *testing.T, validatorCount int) exportFixture {
	t.Helper()

	validators := make([]*cmttypes.Validator, validatorCount)
	for i := range validators {
		pubKey, err := mock.NewPV().GetPubKey()
		require.NoError(t, err)
		validators[i] = cmttypes.NewValidator(pubKey, 1)
	}
	funderKey := secp256k1.GenPrivKey()
	funder := authtypes.NewBaseAccount(funderKey.PubKey().Address().Bytes(), funderKey.PubKey(), 0, 0)
	balance := banktypes.Balance{
		Address: funder.GetAddress().String(),
		Coins:   sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, sdk.DefaultPowerReduction.MulRaw(1_000))),
	}
	arkApp := SetupWithGenesisValSet(t, cmttypes.NewValidatorSet(validators), []authtypes.GenesisAccount{funder}, balance)
	_, err := arkApp.Commit()
	require.NoError(t, err)

	return exportFixture{app: arkApp, validators: validators}
}

// operator returns the operator address the genesis builder derives for a
// validator: its consensus address bytes under the operator prefix.
func (f exportFixture) operator(i int) string {
	return sdk.ValAddress(f.validators[i].Address).String()
}

// TestExportRefusesZeroHeight pins that the zero-height flag is refused rather
// than half-honoured: the prep it would need re-expresses every stored height,
// and a partial one imports cleanly while reopening closed windows.
func TestExportRefusesZeroHeight(t *testing.T) {
	fixture := newExportFixture(t, 1)

	_, err := fixture.app.ExportAppStateAndValidators(true, nil, nil)
	require.ErrorContains(t, err, "zero-height export is not supported")
}

// TestExportContinuesAtNextHeight pins the relaunch contract every
// height-anchored record relies on: the exported genesis starts one past the
// last committed height, so absolute heights carry over unchanged.
func TestExportContinuesAtNextHeight(t *testing.T) {
	fixture := newExportFixture(t, 1)

	exported, err := fixture.app.ExportAppStateAndValidators(false, nil, nil)
	require.NoError(t, err)
	require.Equal(t, fixture.app.LastBlockHeight()+1, exported.Height)
	require.Len(t, exported.Validators, 1)
}

// TestExportTrimsValidatorsToAllowList pins the relaunch tool for a set that
// lost more than a third of its power: every validator off the allow list is
// jailed, and the consensus validators and the staking genesis agree on who
// remains, which is what InitChain demands of a genesis.
func TestExportTrimsValidatorsToAllowList(t *testing.T) {
	fixture := newExportFixture(t, 3)
	kept := fixture.operator(0)

	exported, err := fixture.app.ExportAppStateAndValidators(false, []string{kept}, nil)
	require.NoError(t, err)

	require.Len(t, exported.Validators, 1)
	require.Equal(t, fixture.validators[0].Address, exported.Validators[0].Address)

	var appState map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(exported.AppState, &appState))
	var stakingGenesis stakingtypes.GenesisState
	fixture.app.AppCodec().MustUnmarshalJSON(appState[stakingtypes.ModuleName], &stakingGenesis)

	require.Len(t, stakingGenesis.Validators, 3)
	for _, validator := range stakingGenesis.Validators {
		require.Equal(t, validator.OperatorAddress != kept, validator.Jailed, validator.OperatorAddress)
	}
	require.Len(t, stakingGenesis.LastValidatorPowers, 1)
	require.Equal(t, kept, stakingGenesis.LastValidatorPowers[0].Address)
}

// TestExportRefusesUnknownAllowedValidator pins that an allow-list entry
// naming no validator fails the export: a typo there would otherwise jail the
// validator it meant to keep.
func TestExportRefusesUnknownAllowedValidator(t *testing.T) {
	fixture := newExportFixture(t, 2)
	unknown := sdk.ValAddress(secp256k1.GenPrivKey().PubKey().Address()).String()

	tests := []struct {
		name    string
		allowed []string
		want    string
	}{
		{name: "malformed address", allowed: []string{"not-an-address"}, want: "parsing allowed validator"},
		{name: "no such validator", allowed: []string{fixture.operator(0), unknown}, want: "allowed validator " + unknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := fixture.app.ExportAppStateAndValidators(false, tc.allowed, nil)
			require.ErrorContains(t, err, tc.want)
		})
	}
}
