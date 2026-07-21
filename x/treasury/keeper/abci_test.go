package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	chain "ark/pkg/chain"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/types"
)

func (s *KeeperTestSuite) TestBeginBlockerSkipsRewardFundingAtGenesisHeight() {
	s.setBlockHeight(1)
	s.expectTaxCapsMatch()

	s.Require().NoError(s.keeper.BeginBlocker(s.ctx))
	s.requireDefaultRewardFunding()
}

func (s *KeeperTestSuite) TestBeginBlockerAccruesRewardFundingWhenCapsMatch() {
	s.setBlockHeight(2)
	s.expectValidatorFees(sdk.NewCoins())
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.MicroSDRDenom, math.ZeroInt()))
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(oracletypes.TobinTaxes{
		{Denom: chain.MicroSDRDenom},
	}, nil)

	s.Require().NoError(s.keeper.BeginBlocker(s.ctx))
	funding, err := s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(types.DefaultRewardFundingWindow-1, funding.BlocksRemaining)
	s.Require().True(funding.ValuationComplete)
}

func (s *KeeperTestSuite) TestBeginBlockerRefreshesMismatchedCaps() {
	s.setBlockHeight(1)
	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.OneInt()
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	configured := oracletypes.TobinTaxes{
		{Denom: chain.MicroSDRDenom},
		{Denom: chain.MicroUSDDenom},
	}
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(configured, nil).Times(2)
	s.oracleKeeper.EXPECT().GetRateSnapshot(
		gomock.Any(),
		chain.MicroSDRDenom,
		chain.MicroUSDDenom,
	).Return(oracletypes.RateSnapshot{
		chain.MicroSDRDenom: math.LegacyOneDec(),
		chain.MicroUSDDenom: math.LegacyOneDec(),
	}, nil)

	s.Require().NoError(s.keeper.BeginBlocker(s.ctx))
	for _, denom := range []string{chain.MicroSDRDenom, chain.MicroUSDDenom} {
		cap, err := s.keeper.TaxCaps.Get(s.ctx, denom)
		s.Require().NoError(err)
		s.Require().Equal(math.OneInt(), cap)
	}
}

func (s *KeeperTestSuite) TestBeginBlockerSkipsUnavailableTaxCapRates() {
	s.setBlockHeight(1)
	policy := types.DefaultMonetaryPolicy()
	policy.StabilityTaxRate = math.LegacyMustNewDecFromStr("0.1")
	s.Require().NoError(s.keeper.MonetaryPolicy.Set(s.ctx, policy))
	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.NewInt(1_000_000)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.MicroSDRDenom, math.NewInt(1_000_000)))
	configured := oracletypes.TobinTaxes{
		{Denom: chain.MicroSDRDenom},
		{Denom: chain.MicroUSDDenom},
	}
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(configured, nil).Times(4)
	s.oracleKeeper.EXPECT().GetRateSnapshot(
		gomock.Any(),
		chain.MicroSDRDenom,
		chain.MicroUSDDenom,
	).Return(nil, oracletypes.ErrStaleExchangeRate)

	s.Require().NoError(s.keeper.BeginBlocker(s.ctx))
	s.requireTypedEvent(&types.EventTaxCapsUpdateSkipped{
		Reason: types.EventSkipReason_EVENT_SKIP_REASON_STALE_EXCHANGE_RATE,
	})

	tax, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{&banktypes.MsgSend{
		Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.MicroSDRDenom, 100)),
	}})
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.MicroSDRDenom, 10)), tax)

	_, err = s.keeper.ComputeTax(s.ctx, []sdk.Msg{&banktypes.MsgSend{
		Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.MicroUSDDenom, 100)),
	}})
	s.Require().ErrorIs(err, types.ErrTaxCapUnavailable)
}

func (s *KeeperTestSuite) TestBeginBlockerSkipsUnrepresentableTaxCapConversion() {
	s.setBlockHeight(1)
	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.NewInt(1_000_000)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	configured := oracletypes.TobinTaxes{
		{Denom: chain.MicroSDRDenom},
		{Denom: chain.MicroUSDDenom},
	}
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(configured, nil).Times(2)
	s.oracleKeeper.EXPECT().GetRateSnapshot(
		gomock.Any(),
		chain.MicroSDRDenom,
		chain.MicroUSDDenom,
	).Return(nil, oracletypes.ErrConversionOutOfRange)

	s.Require().NoError(s.keeper.BeginBlocker(s.ctx))
	s.requireTypedEvent(&types.EventTaxCapsUpdateSkipped{
		Reason: types.EventSkipReason_EVENT_SKIP_REASON_CONVERSION_OUT_OF_RANGE,
	})
	_, err := s.keeper.TaxCaps.Get(s.ctx, chain.MicroUSDDenom)
	s.Require().Error(err)
}

func (s *KeeperTestSuite) TestBeginBlockerSkipsTaxCapConversionThatTruncatesToZero() {
	s.setBlockHeight(1)
	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.OneInt()
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	configured := oracletypes.TobinTaxes{
		{Denom: chain.MicroSDRDenom},
		{Denom: chain.MicroUSDDenom},
	}
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(configured, nil).Times(2)
	s.oracleKeeper.EXPECT().GetRateSnapshot(
		gomock.Any(),
		chain.MicroSDRDenom,
		chain.MicroUSDDenom,
	).Return(oracletypes.RateSnapshot{
		chain.MicroSDRDenom: math.LegacyNewDec(2),
		chain.MicroUSDDenom: math.LegacyOneDec(),
	}, nil)

	s.Require().NoError(s.keeper.BeginBlocker(s.ctx))
	s.requireTypedEvent(&types.EventTaxCapsUpdateSkipped{
		Reason: types.EventSkipReason_EVENT_SKIP_REASON_CONVERSION_OUT_OF_RANGE,
	})
	_, err := s.keeper.TaxCaps.Get(s.ctx, chain.MicroUSDDenom)
	s.Require().Error(err)
}
