package claims

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
	"ark/x/claims/types"
)

func TestClaimsSendRestrictionAdmitsNoah(t *testing.T) {
	insurance := authtypes.NewModuleAddress(types.InsuranceName)
	amount := sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1))

	got, err := ClaimsSendRestriction(context.Background(), sdk.AccAddress{1}, insurance, amount)
	require.NoError(t, err)
	require.Equal(t, insurance, got)
}

func TestClaimsSendRestrictionIgnoresOtherRecipients(t *testing.T) {
	unrelated := sdk.AccAddress{2}
	// Invalid coins still pass, because the restriction only guards Insurance.
	invalid := sdk.Coins{{Denom: chain.NoahBaseDenom, Amount: math.NewInt(-1)}}

	got, err := ClaimsSendRestriction(context.Background(), sdk.AccAddress{1}, unrelated, invalid)
	require.NoError(t, err)
	require.Equal(t, unrelated, got)
}

func TestClaimsSendRestrictionRejectsInvalidDeposits(t *testing.T) {
	insurance := authtypes.NewModuleAddress(types.InsuranceName)
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

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ClaimsSendRestriction(context.Background(), sdk.AccAddress{1}, insurance, tc.amount)
			require.ErrorIs(t, err, errortypes.ErrInvalidCoins)
			require.Nil(t, got)
		})
	}
}

// TestClaimsSendRestrictionHasNoCollectorExemption pins the difference from
// Treasury's restriction: the stability-tax collector may route non-NOAH
// residue into strategic Reserve, but nothing may route it into Insurance.
func TestClaimsSendRestrictionHasNoCollectorExemption(t *testing.T) {
	collector := authtypes.NewModuleAddress("stability_tax_collector")
	insurance := authtypes.NewModuleAddress(types.InsuranceName)

	got, err := ClaimsSendRestriction(
		context.Background(),
		collector,
		insurance,
		sdk.NewCoins(sdk.NewInt64Coin("asdr", 1)),
	)
	require.ErrorIs(t, err, errortypes.ErrInvalidCoins)
	require.Nil(t, got)
}

// TestClaimsSendRestrictionUsesRewrittenRecipient proves the restriction reads
// the recipient a prior restriction in the chain produced, not the original.
func TestClaimsSendRestrictionUsesRewrittenRecipient(t *testing.T) {
	insurance := authtypes.NewModuleAddress(types.InsuranceName)
	rewriteToInsurance := func(
		_ context.Context,
		_ sdk.AccAddress,
		_ sdk.AccAddress,
		_ sdk.Coins,
	) (sdk.AccAddress, error) {
		return insurance, nil
	}
	restriction := banktypes.ComposeSendRestrictions(rewriteToInsurance, ClaimsSendRestriction)

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
	require.Equal(t, insurance, got)
}
