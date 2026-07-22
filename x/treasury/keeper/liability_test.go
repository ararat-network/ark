package keeper_test

import (
	"go.uber.org/mock/gomock"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	oracletypes "ark/x/oracle/types"
)

func (s *KeeperTestSuite) TestLiabilitySnapshotReusesScanAndTracksSupplyChanges() {
	tobinTaxes := []oracletypes.TobinTax{
		{Denom: chain.MicroUSDDenom},
		{Denom: chain.MicroKRWDenom},
	}
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(tobinTaxes, nil).Times(2)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.MicroUSDDenom).
		Return(sdk.NewInt64Coin(chain.MicroUSDDenom, 100)).Times(1)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.MicroKRWDenom).
		Return(sdk.NewInt64Coin(chain.MicroKRWDenom, 100)).Times(1)
	s.bankKeeper.EXPECT().GetBalance(
		gomock.Any(),
		gomock.Any(),
		chain.MicroNoahDenom,
	).Return(sdk.NewInt64Coin(chain.MicroNoahDenom, 0)).Times(2)

	rates := oracletypes.RateSnapshot{
		chain.MicroNoahDenom: math.LegacyOneDec(),
		chain.MicroUSDDenom:  math.LegacyOneDec(),
		chain.MicroKRWDenom:  math.LegacyOneDec(),
	}
	first, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.MicroUSDDenom, 10),
		math.NewInt(10),
		rates,
	)
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyNewDec(200), first.AggregateLiabilityNoah)

	s.Require().NoError(s.keeper.RecordSupplyChange(
		s.ctx,
		sdk.NewInt64Coin(chain.MicroUSDDenom, 20),
		sdk.NewInt64Coin(chain.MicroKRWDenom, 10),
		rates,
	))

	second, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.MicroUSDDenom, 10),
		math.NewInt(10),
		rates,
	)
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyNewDec(190), second.AggregateLiabilityNoah)
}

func (s *KeeperTestSuite) TestLiabilitySnapshotDoesNotCacheIncompleteValuation() {
	tobinTaxes := []oracletypes.TobinTax{
		{Denom: chain.MicroUSDDenom},
		{Denom: chain.MicroKRWDenom},
	}
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(tobinTaxes, nil).Times(2)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.MicroUSDDenom).
		Return(sdk.NewInt64Coin(chain.MicroUSDDenom, 100)).Times(2)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.MicroKRWDenom).
		Return(sdk.NewInt64Coin(chain.MicroKRWDenom, 100)).Times(2)
	s.oracleKeeper.EXPECT().GetRateSnapshot(gomock.Any(), chain.MicroKRWDenom).
		Return(nil, oracletypes.ErrStaleExchangeRate).Times(2)

	rates := oracletypes.RateSnapshot{
		chain.MicroNoahDenom: math.LegacyOneDec(),
		chain.MicroUSDDenom:  math.LegacyOneDec(),
	}
	for range 2 {
		draw, err := s.keeper.DrawRedemptionBuffer(
			s.ctx,
			sdk.NewInt64Coin(chain.MicroUSDDenom, 10),
			math.NewInt(10),
			rates,
		)
		s.Require().NoError(err)
		s.Require().False(draw.ValuationComplete)
		s.Require().True(draw.BufferPaid.IsZero())
	}

	s.Require().NoError(s.keeper.RecordSupplyChange(
		s.ctx,
		sdk.NewInt64Coin(chain.MicroUSDDenom, 10),
		sdk.NewInt64Coin(chain.MicroNoahDenom, 10),
		rates,
	))

	transientStore := s.transientStoreService.OpenTransientStore(s.ctx)
	iterator, err := transientStore.Iterator(nil, nil)
	s.Require().NoError(err)
	s.Require().False(iterator.Valid())
	s.Require().NoError(iterator.Close())
}

func (s *KeeperTestSuite) TestLiabilitySnapshotResetsAtBlockCommit() {
	tobinTaxes := []oracletypes.TobinTax{{Denom: chain.MicroUSDDenom}}
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(tobinTaxes, nil).Times(2)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.MicroUSDDenom).
		Return(sdk.NewInt64Coin(chain.MicroUSDDenom, 100)).Times(2)
	s.bankKeeper.EXPECT().GetBalance(
		gomock.Any(),
		gomock.Any(),
		chain.MicroNoahDenom,
	).Return(sdk.NewInt64Coin(chain.MicroNoahDenom, 0)).Times(2)

	rates := oracletypes.RateSnapshot{
		chain.MicroNoahDenom: math.LegacyOneDec(),
		chain.MicroUSDDenom:  math.LegacyOneDec(),
	}
	for block := int64(1); block <= 2; block++ {
		draw, err := s.keeper.DrawRedemptionBuffer(
			s.ctx,
			sdk.NewInt64Coin(chain.MicroUSDDenom, 10),
			math.NewInt(10),
			rates,
		)
		s.Require().NoError(err)
		s.Require().Equal(math.LegacyNewDec(100), draw.AggregateLiabilityNoah)

		if block == 1 {
			s.commitMultiStore.Commit()
			s.setBlockHeight(block + 1)
		}
	}
}

func (s *KeeperTestSuite) TestRecordSupplyChangeWithoutSnapshotIsNoOp() {
	s.Require().NoError(s.keeper.RecordSupplyChange(
		s.ctx,
		sdk.NewInt64Coin(chain.MicroUSDDenom, 10),
		sdk.NewInt64Coin(chain.MicroKRWDenom, 9),
		oracletypes.RateSnapshot{
			chain.MicroUSDDenom: math.LegacyOneDec(),
			chain.MicroKRWDenom: math.LegacyOneDec(),
		},
	))

	transientStore := s.transientStoreService.OpenTransientStore(s.ctx)
	iterator, err := transientStore.Iterator(nil, nil)
	s.Require().NoError(err)
	s.Require().False(iterator.Valid())
	s.Require().NoError(iterator.Close())
}
