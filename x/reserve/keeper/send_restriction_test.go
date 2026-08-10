package keeper_test

import (
	"context"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"ark/pkg/chain"
	"ark/x/reserve/types"
	treasurytypes "ark/x/treasury/types"
)

func (s *KeeperTestSuite) TestSendRestrictionAdmitsNoah() {
	s.SetupTest()
	reserve := authtypes.NewModuleAddress(types.StrategicReserveName)
	amount := sdk.NewCoins(sdk.NewInt64Coin(chain.NoahBaseDenom, 1))

	got, err := s.keeper.SendRestriction(s.ctx, sdk.AccAddress{1}, reserve, amount)
	s.Require().NoError(err)
	s.Require().Equal(reserve, got)
}

func (s *KeeperTestSuite) TestSendRestrictionIgnoresOtherRecipients() {
	s.SetupTest()
	unrelated := sdk.AccAddress{2}
	invalid := sdk.Coins{{Denom: chain.NoahBaseDenom, Amount: math.NewInt(-1)}}

	got, err := s.keeper.SendRestriction(s.ctx, sdk.AccAddress{1}, unrelated, invalid)
	s.Require().NoError(err)
	s.Require().Equal(unrelated, got)
}

func (s *KeeperTestSuite) TestSendRestrictionRejectsInvalidDeposits() {
	reserve := authtypes.NewModuleAddress(types.StrategicReserveName)
	tests := []struct {
		name   string
		amount sdk.Coins
	}{
		{name: "empty", amount: sdk.Coins{}},
		{name: "unset amount", amount: sdk.Coins{{Denom: chain.NoahBaseDenom}}},
		{name: "zero", amount: sdk.Coins{sdk.NewInt64Coin(chain.NoahBaseDenom, 0)}},
		{name: "negative", amount: sdk.Coins{{Denom: chain.NoahBaseDenom, Amount: math.NewInt(-1)}}},
		{name: "unlisted", amount: sdk.NewCoins(sdk.NewInt64Coin(chain.SDRBaseDenom, 1))},
		{name: "mixed noah and unlisted", amount: sdk.NewCoins(
			sdk.NewInt64Coin(chain.NoahBaseDenom, 1),
			sdk.NewInt64Coin(chain.SDRBaseDenom, 1),
		)},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			s.SetupTest()
			got, err := s.keeper.SendRestriction(s.ctx, sdk.AccAddress{1}, reserve, tc.amount)
			s.Require().ErrorIs(err, errortypes.ErrInvalidCoins)
			s.Require().Nil(got)
		})
	}
}

// TestSendRestrictionRefusesExternalSymbols pins that external custody has no
// on-chain door: an external symbol names attested off-chain holdings, and no
// mint path can produce a bank coin under one. Listing changes nothing —
// admission never reads the policy.
func (s *KeeperTestSuite) TestSendRestrictionRefusesExternalSymbols() {
	s.SetupTest()
	reserve := authtypes.NewModuleAddress(types.StrategicReserveName)
	deposit := sdk.NewCoins(sdk.NewInt64Coin(sdrExternal, 5))

	_, err := s.keeper.SendRestriction(s.ctx, sdk.AccAddress{1}, reserve, deposit)
	s.Require().ErrorIs(err, errortypes.ErrInvalidCoins)

	s.setPolicy(eligibility(sdrExternal, "0.5", "0.1"))
	_, err = s.keeper.SendRestriction(s.ctx, sdk.AccAddress{1}, reserve, deposit)
	s.Require().ErrorIs(err, errortypes.ErrInvalidCoins)

	// An external symbol beside an admissible coin fails the whole set.
	mixed := sdk.NewCoins(
		sdk.NewInt64Coin(chain.NoahBaseDenom, 1),
		sdk.NewInt64Coin(sdrExternal, 5),
	)
	_, err = s.keeper.SendRestriction(s.ctx, sdk.AccAddress{1}, reserve, mixed)
	s.Require().ErrorIs(err, errortypes.ErrInvalidCoins)
}

// TestSendRestrictionAdmitsRegistryMembers pins admission by membership.
// Ark-issued paper is protocol liability wherever it sits and can never earn
// recognition credit, so the Reserve — the one account with a committee able to
// retire it — takes it from anyone, listed or not.
func (s *KeeperTestSuite) TestSendRestrictionAdmitsRegistryMembers() {
	s.SetupTest()
	reserve := authtypes.NewModuleAddress(types.StrategicReserveName)
	s.registerAsset(chain.SDRBaseDenom)
	s.registerAsset(chain.USDBaseDenom)

	for _, amount := range []sdk.Coins{
		sdk.NewCoins(sdk.NewInt64Coin(chain.SDRBaseDenom, 1)),
		sdk.NewCoins(sdk.NewInt64Coin(chain.SDRBaseDenom, 1), sdk.NewInt64Coin(chain.USDBaseDenom, 2)),
	} {
		got, err := s.keeper.SendRestriction(s.ctx, sdk.AccAddress{1}, reserve, amount)
		s.Require().NoError(err)
		s.Require().Equal(reserve, got)
	}
}

// TestSendRestrictionSubsumesTaxCollectorExemption pins that settlement
// routing of derecognized stability tax passes through the membership test: a
// written-off or retired asset is still a registry member.
func (s *KeeperTestSuite) TestSendRestrictionSubsumesTaxCollectorExemption() {
	s.SetupTest()
	collector := authtypes.NewModuleAddress(treasurytypes.StabilityTaxCollectorName)
	reserve := authtypes.NewModuleAddress(types.StrategicReserveName)
	s.registerAsset(chain.SDRBaseDenom)

	got, err := s.keeper.SendRestriction(
		s.ctx,
		collector,
		reserve,
		sdk.NewCoins(sdk.NewInt64Coin(chain.SDRBaseDenom, 1)),
	)
	s.Require().NoError(err)
	s.Require().Equal(reserve, got)

	// The collector has no bypass for a coin that is neither NOAH, an external
	// symbol, nor a member.
	_, err = s.keeper.SendRestriction(
		s.ctx,
		collector,
		reserve,
		sdk.NewCoins(sdk.NewInt64Coin("afuture", 1)),
	)
	s.Require().ErrorIs(err, errortypes.ErrInvalidCoins)
}

// TestSendRestrictionRefusesUnregisteredBareDenoms pins the one denomination
// class that is refused from every sender: a bare priced name no asset carries,
// which is neither protocol paper nor an honest claim and is precisely the name
// a registration could take tomorrow.
func (s *KeeperTestSuite) TestSendRestrictionRefusesUnregisteredBareDenoms() {
	s.SetupTest()
	reserve := authtypes.NewModuleAddress(types.StrategicReserveName)

	got, err := s.keeper.SendRestriction(
		s.ctx,
		sdk.AccAddress{1},
		reserve,
		sdk.NewCoins(sdk.NewInt64Coin(chain.SDRBaseDenom, 1)),
	)
	s.Require().ErrorIs(err, errortypes.ErrInvalidCoins)
	s.Require().Nil(got)
}

func (s *KeeperTestSuite) TestSendRestrictionUsesRewrittenRecipient() {
	s.SetupTest()
	reserve := authtypes.NewModuleAddress(types.StrategicReserveName)
	rewriteToReserve := func(
		_ context.Context,
		_ sdk.AccAddress,
		_ sdk.AccAddress,
		_ sdk.Coins,
	) (sdk.AccAddress, error) {
		return reserve, nil
	}
	restriction := banktypes.ComposeSendRestrictions(rewriteToReserve, s.keeper.SendRestriction)

	got, err := restriction(
		s.ctx,
		sdk.AccAddress{1},
		sdk.AccAddress{2},
		sdk.NewCoins(sdk.NewInt64Coin(chain.SDRBaseDenom, 1)),
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
	s.Require().Equal(reserve, got)
}
