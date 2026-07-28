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
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.SDRBaseDenom, math.ZeroInt()))
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return([]oracletypes.TobinTax{
		{Denom: chain.SDRBaseDenom},
	}, nil)

	s.Require().NoError(s.keeper.BeginBlocker(s.ctx))
	funding, err := s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(types.DefaultRewardFundingWindow-1, funding.BlocksRemaining)
	s.Require().True(funding.ValuationComplete)
}

func (s *KeeperTestSuite) TestBeginBlockerReusesTobinTaxesForStableFeeValuation() {
	s.setBlockHeight(2)
	configured := []oracletypes.TobinTax{{Denom: chain.SDRBaseDenom}}
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.SDRBaseDenom, math.ZeroInt()))
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(configured, nil)
	s.expectValidatorFees(sdk.NewCoins(sdk.NewInt64Coin(chain.SDRBaseDenom, 5)))
	s.oracleKeeper.EXPECT().GetRateSet(gomock.Any(), chain.SDRBaseDenom).Return(
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.SDRBaseDenom:  math.LegacyOneDec(),
		},
		nil,
	)

	s.Require().NoError(s.keeper.BeginBlocker(s.ctx))
	funding, err := s.keeper.RewardFunding.Get(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(math.NewInt(5), funding.ValidatorFeeValue)
}

func (s *KeeperTestSuite) TestBeginBlockerRefreshesMismatchedCaps() {
	s.setBlockHeight(1)
	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.OneInt()
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	configured := []oracletypes.TobinTax{
		{Denom: chain.SDRBaseDenom},
		{Denom: chain.USDBaseDenom},
	}
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(configured, nil)
	s.oracleKeeper.EXPECT().GetRateSet(
		gomock.Any(),
		chain.SDRBaseDenom,
		chain.USDBaseDenom,
	).Return(oracletypes.RateSet{
		chain.SDRBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom: math.LegacyOneDec(),
	}, nil)

	s.Require().NoError(s.keeper.BeginBlocker(s.ctx))
	for _, denom := range []string{chain.SDRBaseDenom, chain.USDBaseDenom} {
		cap, err := s.keeper.TaxCaps.Get(s.ctx, denom)
		s.Require().NoError(err)
		s.Require().Equal(math.OneInt(), cap)
	}
}

func (s *KeeperTestSuite) TestBeginBlockerReplacesStaleTaxCapDenoms() {
	tests := []struct {
		name   string
		stored []string
	}{
		{name: "unexpected stored denom", stored: []string{chain.SDRBaseDenom, chain.USDBaseDenom}},
		{name: "equal count replacement", stored: []string{chain.USDBaseDenom}},
	}

	for _, test := range tests {
		s.Run(test.name, func() {
			s.setBlockHeight(1)
			s.Require().NoError(s.keeper.TaxCaps.Clear(s.ctx, nil))
			for _, denom := range test.stored {
				s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, denom, math.ZeroInt()))
			}
			s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return([]oracletypes.TobinTax{
				{Denom: chain.SDRBaseDenom},
			}, nil)

			s.Require().NoError(s.keeper.BeginBlocker(s.ctx))
			cap, err := s.keeper.TaxCaps.Get(s.ctx, chain.SDRBaseDenom)
			s.Require().NoError(err)
			s.Require().True(cap.IsZero())
			_, err = s.keeper.TaxCaps.Get(s.ctx, chain.USDBaseDenom)
			s.Require().Error(err)
		})
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
	s.Require().NoError(s.keeper.TaxCaps.Set(s.ctx, chain.SDRBaseDenom, math.NewInt(1_000_000)))
	configured := []oracletypes.TobinTax{
		{Denom: chain.SDRBaseDenom},
		{Denom: chain.USDBaseDenom},
	}
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(configured, nil).Times(3)
	s.oracleKeeper.EXPECT().GetRateSet(
		gomock.Any(),
		chain.SDRBaseDenom,
		chain.USDBaseDenom,
	).Return(nil, oracletypes.ErrStaleExchangeRate)

	s.Require().NoError(s.keeper.BeginBlocker(s.ctx))
	s.requireTypedEvent(&types.EventTaxCapsUpdateSkipped{
		Reason: types.EventSkipReason_EVENT_SKIP_REASON_STALE_EXCHANGE_RATE,
	})

	tax, err := s.keeper.ComputeTax(s.ctx, []sdk.Msg{&banktypes.MsgSend{
		Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.SDRBaseDenom, 100)),
	}})
	s.Require().NoError(err)
	s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin(chain.SDRBaseDenom, 10)), tax)

	_, err = s.keeper.ComputeTax(s.ctx, []sdk.Msg{&banktypes.MsgSend{
		Amount: sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 100)),
	}})
	s.Require().ErrorIs(err, types.ErrTaxCapUnavailable)
}

func (s *KeeperTestSuite) TestBeginBlockerSkipsUnrepresentableTaxCapConversion() {
	s.setBlockHeight(1)
	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.NewInt(1_000_000)
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	configured := []oracletypes.TobinTax{
		{Denom: chain.SDRBaseDenom},
		{Denom: chain.USDBaseDenom},
	}
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(configured, nil)
	s.oracleKeeper.EXPECT().GetRateSet(
		gomock.Any(),
		chain.SDRBaseDenom,
		chain.USDBaseDenom,
	).Return(nil, oracletypes.ErrConversionOutOfRange)

	s.Require().NoError(s.keeper.BeginBlocker(s.ctx))
	s.requireTypedEvent(&types.EventTaxCapsUpdateSkipped{
		Reason: types.EventSkipReason_EVENT_SKIP_REASON_CONVERSION_OUT_OF_RANGE,
	})
	_, err := s.keeper.TaxCaps.Get(s.ctx, chain.USDBaseDenom)
	s.Require().Error(err)
}

func (s *KeeperTestSuite) TestBeginBlockerSkipsTaxCapConversionThatTruncatesToZero() {
	s.setBlockHeight(1)
	params := types.DefaultParams()
	params.ReferenceTaxCap.Amount = math.OneInt()
	s.Require().NoError(s.keeper.Params.Set(s.ctx, params))
	configured := []oracletypes.TobinTax{
		{Denom: chain.SDRBaseDenom},
		{Denom: chain.USDBaseDenom},
	}
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(configured, nil)
	s.oracleKeeper.EXPECT().GetRateSet(
		gomock.Any(),
		chain.SDRBaseDenom,
		chain.USDBaseDenom,
	).Return(oracletypes.RateSet{
		chain.SDRBaseDenom: math.LegacyNewDec(2),
		chain.USDBaseDenom: math.LegacyOneDec(),
	}, nil)

	s.Require().NoError(s.keeper.BeginBlocker(s.ctx))
	s.requireTypedEvent(&types.EventTaxCapsUpdateSkipped{
		Reason: types.EventSkipReason_EVENT_SKIP_REASON_CONVERSION_OUT_OF_RANGE,
	})
	_, err := s.keeper.TaxCaps.Get(s.ctx, chain.USDBaseDenom)
	s.Require().Error(err)
}
