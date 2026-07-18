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
	unoah := sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 1))

	for _, fundAddress := range fundAddresses {
		fundAddress := fundAddress
		t.Run(fundAddress.String(), func(t *testing.T) {
			got, err := TreasurySendRestriction(context.Background(), sdk.AccAddress{1}, fundAddress, unoah)
			require.NoError(t, err)
			require.Equal(t, fundAddress, got)
		})
	}

	unrelated := sdk.AccAddress{2}
	invalid := sdk.Coins{{Denom: chain.MicroNoahDenom, Amount: math.NewInt(-1)}}
	got, err := TreasurySendRestriction(context.Background(), sdk.AccAddress{1}, unrelated, invalid)
	require.NoError(t, err)
	require.Equal(t, unrelated, got)
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
		{name: "unset amount", amount: sdk.Coins{{Denom: chain.MicroNoahDenom}}},
		{name: "zero", amount: sdk.Coins{sdk.NewInt64Coin(chain.MicroNoahDenom, 0)}},
		{name: "negative", amount: sdk.Coins{{Denom: chain.MicroNoahDenom, Amount: math.NewInt(-1)}}},
		{name: "non noah", amount: sdk.NewCoins(sdk.NewInt64Coin("usdr", 1))},
		{name: "mixed", amount: sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 1), sdk.NewInt64Coin("usdr", 1))},
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
		sdk.NewCoins(sdk.NewInt64Coin("usdr", 1)),
	)
	require.ErrorIs(t, err, errortypes.ErrInvalidCoins)
	require.Nil(t, got)

	got, err = restriction(
		context.Background(),
		sdk.AccAddress{1},
		sdk.AccAddress{2},
		sdk.NewCoins(sdk.NewInt64Coin(chain.MicroNoahDenom, 1)),
	)
	require.NoError(t, err)
	require.Equal(t, fundAddress, got)
}
