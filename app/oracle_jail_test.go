package app_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"

	apptestutil "github.com/ararat-network/ark/app/testutil"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// An attendance jail holds for the Oracle's JailDuration: the wired slashing keeper records the
// release time MsgUnjail checks, so a jailed validator cannot rejoin in the next block.
func TestOracleJailHoldsForJailDuration(t *testing.T) {
	arkApp := apptestutil.Setup(t, false)
	ctx := arkApp.NewContextLegacy(false, cmtproto.Header{
		ChainID: apptestutil.ChainID,
		Height:  arkApp.LastBlockHeight() + 1,
		Time:    apptestutil.GenesisTime,
	})

	validators, err := arkApp.StakingKeeper.GetAllValidators(ctx)
	require.NoError(t, err)
	require.Len(t, validators, 1)
	valAddr, err := sdk.ValAddressFromBech32(validators[0].OperatorAddress)
	require.NoError(t, err)
	consAddr, err := validators[0].GetConsAddr()
	require.NoError(t, err)

	// Signing info exists for any validator that has signed a block; Setup's chain has signed none.
	require.NoError(t, arkApp.SlashingKeeper.SetValidatorSigningInfo(ctx, consAddr,
		slashingtypes.NewValidatorSigningInfo(consAddr, 1, 0, time.Unix(0, 0), false, 0)))
	require.NoError(t, arkApp.OracleKeeper.Attendance.Set(ctx, valAddr, oracletypes.Attendance{EligibleBlocks: 20}))
	require.NoError(t, arkApp.OracleKeeper.SettleAttendance(ctx, 20))

	validator, err := arkApp.StakingKeeper.GetValidator(ctx, valAddr)
	require.NoError(t, err)
	require.True(t, validator.IsJailed())

	info, err := arkApp.SlashingKeeper.GetValidatorSigningInfo(ctx, consAddr)
	require.NoError(t, err)
	require.Equal(t, ctx.BlockTime().Add(time.Duration(oracletypes.DefaultJailDuration)*time.Second), info.JailedUntil)
}
