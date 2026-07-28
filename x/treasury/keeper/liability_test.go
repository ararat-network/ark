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
		{Denom: chain.USDBaseDenom},
		{Denom: chain.KRWBaseDenom},
	}
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(tobinTaxes, nil).Times(2)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(1)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 100)).Times(1)
	s.bankKeeper.EXPECT().GetBalance(
		gomock.Any(),
		gomock.Any(),
		chain.NoahBaseDenom,
	).Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0)).Times(2)

	rates := oracletypes.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom:  math.LegacyOneDec(),
		chain.KRWBaseDenom:  math.LegacyOneDec(),
	}
	first, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		math.NewInt(10),
		rates,
	)
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyNewDec(200), first.AggregateLiabilityNoah)

	s.Require().NoError(s.keeper.RecordSupplyChange(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 20),
		sdk.NewInt64Coin(chain.KRWBaseDenom, 10),
		rates,
	))

	second, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		math.NewInt(10),
		rates,
	)
	s.Require().NoError(err)
	s.Require().Equal(math.LegacyNewDec(190), second.AggregateLiabilityNoah)
}

func (s *KeeperTestSuite) TestLiabilitySnapshotDoesNotCacheIncompleteValuation() {
	tobinTaxes := []oracletypes.TobinTax{
		{Denom: chain.USDBaseDenom},
		{Denom: chain.KRWBaseDenom},
	}
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(tobinTaxes, nil).Times(2)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(2)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 100)).Times(2)
	s.oracleKeeper.EXPECT().GetRateSet(gomock.Any(), chain.KRWBaseDenom).
		Return(nil, oracletypes.ErrStaleExchangeRate).Times(2)

	rates := oracletypes.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom:  math.LegacyOneDec(),
	}
	for range 2 {
		draw, err := s.keeper.DrawRedemptionBuffer(
			s.ctx,
			sdk.NewInt64Coin(chain.USDBaseDenom, 10),
			math.NewInt(10),
			rates,
		)
		s.Require().NoError(err)
		s.Require().False(draw.ValuationComplete)
		s.Require().True(draw.BufferPaid.IsZero())
	}

	s.Require().NoError(s.keeper.RecordSupplyChange(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		sdk.NewInt64Coin(chain.NoahBaseDenom, 10),
		rates,
	))

	transientStore := s.transientStoreService.OpenTransientStore(s.ctx)
	iterator, err := transientStore.Iterator(nil, nil)
	s.Require().NoError(err)
	s.Require().False(iterator.Valid())
	s.Require().NoError(iterator.Close())
}

func (s *KeeperTestSuite) TestLiabilitySnapshotResetsAtBlockCommit() {
	tobinTaxes := []oracletypes.TobinTax{{Denom: chain.USDBaseDenom}}
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(tobinTaxes, nil).Times(2)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(2)
	s.bankKeeper.EXPECT().GetBalance(
		gomock.Any(),
		gomock.Any(),
		chain.NoahBaseDenom,
	).Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0)).Times(2)

	rates := oracletypes.RateSet{
		chain.NoahBaseDenom: math.LegacyOneDec(),
		chain.USDBaseDenom:  math.LegacyOneDec(),
	}
	for block := int64(1); block <= 2; block++ {
		draw, err := s.keeper.DrawRedemptionBuffer(
			s.ctx,
			sdk.NewInt64Coin(chain.USDBaseDenom, 10),
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
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		sdk.NewInt64Coin(chain.KRWBaseDenom, 9),
		oracletypes.RateSet{
			chain.USDBaseDenom: math.LegacyOneDec(),
			chain.KRWBaseDenom: math.LegacyOneDec(),
		},
	))

	transientStore := s.transientStoreService.OpenTransientStore(s.ctx)
	iterator, err := transientStore.Iterator(nil, nil)
	s.Require().NoError(err)
	s.Require().False(iterator.Valid())
	s.Require().NoError(iterator.Close())
}
