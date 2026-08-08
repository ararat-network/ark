package keeper_test

import (
	"context"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"ark/pkg/chain"
	"ark/x/claims/types"
)

func (s *KeeperTestSuite) TestSendRestrictionAdmitsNoah() {
	s.SetupTest()
	insurance := authtypes.NewModuleAddress(types.InsuranceName)
	amount := sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1))

	got, err := s.keeper.SendRestriction(s.ctx, sdk.AccAddress{1}, insurance, amount)
	s.Require().NoError(err)
	s.Require().Equal(insurance, got)
}

func (s *KeeperTestSuite) TestSendRestrictionIgnoresOtherRecipients() {
	s.SetupTest()
	unrelated := sdk.AccAddress{2}
	// Invalid coins still pass, because the restriction only guards Insurance.
	invalid := sdk.Coins{{Denom: chain.NoahBaseDenom, Amount: math.NewInt(-1)}}

	got, err := s.keeper.SendRestriction(s.ctx, sdk.AccAddress{1}, unrelated, invalid)
	s.Require().NoError(err)
	s.Require().Equal(unrelated, got)
}

func (s *KeeperTestSuite) TestSendRestrictionRejectsInvalidDeposits() {
	s.SetupTest()
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
		s.Run(tc.name, func() {
			got, err := s.keeper.SendRestriction(s.ctx, sdk.AccAddress{1}, insurance, tc.amount)
			s.Require().ErrorIs(err, errortypes.ErrInvalidCoins)
			s.Require().Nil(got)
		})
	}
}

// TestSendRestrictionHasNoCollectorExemption pins the difference from
// Treasury's restriction: the stability-tax collector may route non-NOAH
// residue into strategic Reserve, but nothing may route it into Insurance.
func (s *KeeperTestSuite) TestSendRestrictionHasNoCollectorExemption() {
	s.SetupTest()
	collector := authtypes.NewModuleAddress("stability_tax_collector")
	insurance := authtypes.NewModuleAddress(types.InsuranceName)

	got, err := s.keeper.SendRestriction(
		s.ctx,
		collector,
		insurance,
		sdk.NewCoins(sdk.NewInt64Coin("asdr", 1)),
	)
	s.Require().ErrorIs(err, errortypes.ErrInvalidCoins)
	s.Require().Nil(got)
}

// TestSendRestrictionUsesRewrittenRecipient proves the restriction reads the
// recipient a prior restriction in the chain produced, not the original.
func (s *KeeperTestSuite) TestSendRestrictionUsesRewrittenRecipient() {
	s.SetupTest()
	insurance := authtypes.NewModuleAddress(types.InsuranceName)
	rewriteToInsurance := func(
		_ context.Context,
		_ sdk.AccAddress,
		_ sdk.AccAddress,
		_ sdk.Coins,
	) (sdk.AccAddress, error) {
		return insurance, nil
	}
	restriction := banktypes.ComposeSendRestrictions(rewriteToInsurance, s.keeper.SendRestriction)

	got, err := restriction(
		s.ctx,
		sdk.AccAddress{1},
		sdk.AccAddress{2},
		sdk.NewCoins(sdk.NewInt64Coin("asdr", 1)),
	)
	s.Require().ErrorIs(err, errortypes.ErrInvalidCoins)
	s.Require().Nil(got)

	got, err = restriction(
		s.ctx,
		sdk.AccAddress{1},
		sdk.AccAddress{2},
		sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1)),
	)
	s.Require().NoError(err)
	s.Require().Equal(insurance, got)
}
