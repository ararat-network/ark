package keeper_test

import (
	"context"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/x/treasury/types"
)

func (s *KeeperTestSuite) TestSendRestrictionAdmitsNoah() {
	s.SetupTest()
	anoah := sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1))

	for _, fundName := range types.FundAccountNames() {
		fundAddress := authtypes.NewModuleAddress(fundName)
		s.Run(fundName, func() {
			got, err := s.keeper.SendRestriction(s.ctx, sdk.AccAddress{1}, fundAddress, anoah)
			s.Require().NoError(err)
			s.Require().Equal(fundAddress, got)
		})
	}
}

func (s *KeeperTestSuite) TestSendRestrictionIgnoresOtherRecipients() {
	s.SetupTest()
	unrelated := sdk.AccAddress{2}
	invalid := sdk.Coins{{Denom: chain.NoahBaseDenom, Amount: math.NewInt(-1)}}

	got, err := s.keeper.SendRestriction(s.ctx, sdk.AccAddress{1}, unrelated, invalid)
	s.Require().NoError(err)
	s.Require().Equal(unrelated, got)
}

func (s *KeeperTestSuite) TestSendRestrictionRejectsInvalidFundDeposits() {
	s.SetupTest()
	tests := []struct {
		name   string
		amount sdk.Coins
	}{
		{name: "empty", amount: sdk.Coins{}},
		{name: "unset amount", amount: sdk.Coins{{Denom: chain.NoahBaseDenom}}},
		{name: "zero", amount: sdk.Coins{sdk.NewInt64Coin(chain.NoahBaseDenom, 0)}},
		{name: "negative", amount: sdk.Coins{{Denom: chain.NoahBaseDenom, Amount: math.NewInt(-1)}}},
		{name: "non noah", amount: sdk.NewCoins(sdk.NewInt64Coin("axdr", 1))},
		{name: "mixed", amount: sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1), sdk.NewInt64Coin("axdr", 1))},
	}

	for _, fundName := range types.FundAccountNames() {
		fundAddress := authtypes.NewModuleAddress(fundName)
		s.Run(fundName, func() {
			for _, tc := range tests {
				s.Run(tc.name, func() {
					got, err := s.keeper.SendRestriction(s.ctx, sdk.AccAddress{1}, fundAddress, tc.amount)
					s.Require().ErrorIs(err, errortypes.ErrInvalidCoins)
					s.Require().Nil(got)
				})
			}
		})
	}
}

// TestSendRestrictionGuardsEveryDeclaredFund pins the guarded set to
// FundAccountNames(), the same list the constructor asserts registration
// against and the app leaves unblocked. A fund added there and nowhere else
// stays guarded; there is no second list to keep in step.
func (s *KeeperTestSuite) TestSendRestrictionGuardsEveryDeclaredFund() {
	s.SetupTest()
	nonNoah := sdk.NewCoins(sdk.NewInt64Coin("axdr", 1))

	for _, fundName := range types.FundAccountNames() {
		fundAddress := authtypes.NewModuleAddress(fundName)
		s.Run(fundName, func() {
			got, err := s.keeper.SendRestriction(s.ctx, sdk.AccAddress{1}, fundAddress, nonNoah)
			s.Require().ErrorIs(err, errortypes.ErrInvalidCoins)
			s.Require().Nil(got)
		})
	}
}

// TestSendRestrictionUsesRewrittenRecipient proves the restriction reads the
// recipient a prior restriction in the chain produced, not the original.
func (s *KeeperTestSuite) TestSendRestrictionUsesRewrittenRecipient() {
	s.SetupTest()
	fundAddress := authtypes.NewModuleAddress(types.RedemptionBufferName)
	rewriteToFund := func(
		_ context.Context,
		_ sdk.AccAddress,
		_ sdk.AccAddress,
		_ sdk.Coins,
	) (sdk.AccAddress, error) {
		return fundAddress, nil
	}
	restriction := banktypes.ComposeSendRestrictions(rewriteToFund, s.keeper.SendRestriction)

	got, err := restriction(
		s.ctx,
		sdk.AccAddress{1},
		sdk.AccAddress{2},
		sdk.NewCoins(sdk.NewInt64Coin("axdr", 1)),
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
	s.Require().Equal(fundAddress, got)
}
