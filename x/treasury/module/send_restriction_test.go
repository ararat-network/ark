package treasury

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"ark/pkg/chain"
	"ark/x/treasury/types"
)

func TestTreasurySendRestriction(t *testing.T) {
	fundAddresses := []sdk.AccAddress{
		authtypes.NewModuleAddress(types.SubsidyPoolName),
		authtypes.NewModuleAddress(types.RedemptionBufferName),
		authtypes.NewModuleAddress(types.StrategicReserveName),
		authtypes.NewModuleAddress(types.InsuranceName),
	}
	anoah := sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1))

	for _, fundAddress := range fundAddresses {
		fundAddress := fundAddress
		t.Run(fundAddress.String(), func(t *testing.T) {
			got, err := TreasurySendRestriction(context.Background(), sdk.AccAddress{1}, fundAddress, anoah)
			require.NoError(t, err)
			require.Equal(t, fundAddress, got)
		})
	}

	unrelated := sdk.AccAddress{2}
	invalid := sdk.Coins{{Denom: chain.NoahBaseDenom, Amount: math.NewInt(-1)}}
	got, err := TreasurySendRestriction(context.Background(), sdk.AccAddress{1}, unrelated, invalid)
	require.NoError(t, err)
	require.Equal(t, unrelated, got)
}

// TestTreasurySendRestrictionExemptsTaxCollectorToReserve pins the one exempt
// pair: settlement routing of derecognized stability tax from the collector
// into Reserve custody carries non-NOAH coins — possibly several denominations
// in one send — and must pass. The exemption is the pair, not the sender: the
// collector still cannot reach any other fund with non-NOAH coins, and no
// other sender inherits the Reserve exemption.
func TestTreasurySendRestrictionExemptsTaxCollectorToReserve(t *testing.T) {
	collector := authtypes.NewModuleAddress(types.StabilityTaxCollectorName)
	reserve := authtypes.NewModuleAddress(types.StrategicReserveName)

	for _, amount := range []sdk.Coins{
		sdk.NewCoins(sdk.NewInt64Coin("asdr", 1)),
		sdk.NewCoins(sdk.NewInt64Coin("asdr", 1), sdk.NewInt64Coin("ausd", 2)),
	} {
		got, err := TreasurySendRestriction(context.Background(), collector, reserve, amount)
		require.NoError(t, err)
		require.Equal(t, reserve, got)
	}

	for _, otherFund := range []string{
		types.SubsidyPoolName,
		types.RedemptionBufferName,
		types.InsuranceName,
	} {
		got, err := TreasurySendRestriction(
			context.Background(),
			collector,
			authtypes.NewModuleAddress(otherFund),
			sdk.NewCoins(sdk.NewInt64Coin("asdr", 1)),
		)
		require.ErrorIs(t, err, errortypes.ErrInvalidCoins)
		require.Nil(t, got)
	}

	got, err := TreasurySendRestriction(
		context.Background(),
		sdk.AccAddress{1},
		reserve,
		sdk.NewCoins(sdk.NewInt64Coin("asdr", 1)),
	)
	require.ErrorIs(t, err, errortypes.ErrInvalidCoins)
	require.Nil(t, got)
}

func TestTreasurySendRestrictionRejectsInvalidFundDeposits(t *testing.T) {
	fundNames := []string{
		types.SubsidyPoolName,
		types.RedemptionBufferName,
		types.StrategicReserveName,
		types.InsuranceName,
	}
	tests := []struct {
		name   string
		amount sdk.Coins
	}{
		{name: "empty", amount: sdk.Coins{}},
		{name: "unset amount", amount: sdk.Coins{{Denom: chain.NoahBaseDenom}}},
		{name: "zero", amount: sdk.Coins{sdk.NewInt64Coin(chain.NoahBaseDenom, 0)}},
		{name: "negative", amount: sdk.Coins{{Denom: chain.NoahBaseDenom, Amount: math.NewInt(-1)}}},
		{name: "non noah", amount: sdk.NewCoins(sdk.NewInt64Coin("asdr", 1))},
		{name: "mixed", amount: sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1), sdk.NewInt64Coin("asdr", 1))},
	}

	for _, fundName := range fundNames {
		fundAddress := authtypes.NewModuleAddress(fundName)
		t.Run(fundName, func(t *testing.T) {
			for _, tc := range tests {
				t.Run(tc.name, func(t *testing.T) {
					got, err := TreasurySendRestriction(context.Background(), sdk.AccAddress{1}, fundAddress, tc.amount)
					require.ErrorIs(t, err, errortypes.ErrInvalidCoins)
					require.Nil(t, got)
				})
			}
		})
	}
}

func TestTreasurySendRestrictionUsesRewrittenRecipient(t *testing.T) {
	fundAddress := authtypes.NewModuleAddress(types.RedemptionBufferName)
	rewriteToFund := func(
		_ context.Context,
		_ sdk.AccAddress,
		_ sdk.AccAddress,
		_ sdk.Coins,
	) (sdk.AccAddress, error) {
		return fundAddress, nil
	}
	restriction := banktypes.ComposeSendRestrictions(rewriteToFund, TreasurySendRestriction)

	got, err := restriction(
		context.Background(),
		sdk.AccAddress{1},
		sdk.AccAddress{2},
		sdk.NewCoins(sdk.NewInt64Coin("asdr", 1)),
	)
	require.ErrorIs(t, err, errortypes.ErrInvalidCoins)
	require.Nil(t, got)

	got, err = restriction(
		context.Background(),
		sdk.AccAddress{1},
		sdk.AccAddress{2},
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1)),
	)
	require.NoError(t, err)
	require.Equal(t, fundAddress, got)
}
