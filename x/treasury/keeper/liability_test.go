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

func (s *KeeperTestSuite) TestLiabilityIncompleteValuationMarksBlockUnavailable() {
	tobinTaxes := []oracletypes.TobinTax{
		{Denom: chain.USDBaseDenom},
		{Denom: chain.KRWBaseDenom},
	}
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(tobinTaxes, nil).Times(2)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(1)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 100)).Times(1)
	s.oracleKeeper.EXPECT().GetRateSet(gomock.Any(), chain.KRWBaseDenom).
		Return(nil, oracletypes.ErrStaleExchangeRate).Times(1)

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

	// Key 0x01 is the liability snapshot, key 0x02 the unavailability marker
	// (mirrors the unexported keys in liability.go).
	transientStore := s.transientStoreService.OpenTransientStore(s.ctx)
	snapshot, err := transientStore.Get([]byte{0x01})
	s.Require().NoError(err)
	s.Require().Nil(snapshot)
	marker, err := transientStore.Get([]byte{0x02})
	s.Require().NoError(err)
	s.Require().NotNil(marker)
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

func (s *KeeperTestSuite) TestPrimeLiabilitySnapshotStoresCompleteValuation() {
	tobinTaxes := []oracletypes.TobinTax{
		{Denom: chain.USDBaseDenom},
		{Denom: chain.KRWBaseDenom},
	}
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(tobinTaxes, nil).Times(2)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(1)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.KRWBaseDenom).
		Return(sdk.NewInt64Coin(chain.KRWBaseDenom, 100)).Times(1)
	s.oracleKeeper.EXPECT().GetRateSet(gomock.Any(), chain.USDBaseDenom, chain.KRWBaseDenom).
		Return(oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyOneDec(),
			chain.KRWBaseDenom:  math.LegacyOneDec(),
		}, nil).Times(1)
	s.bankKeeper.EXPECT().GetBalance(gomock.Any(), gomock.Any(), chain.NoahBaseDenom).
		Return(sdk.NewInt64Coin(chain.NoahBaseDenom, 0)).Times(1)

	s.Require().NoError(s.keeper.PrimeLiabilitySnapshot(s.ctx))

	// GetSupply/GetRateSet expectations are exhausted by priming: the draw
	// below must reuse the primed snapshot without rescanning.
	draw, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		math.NewInt(10),
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyOneDec(),
		},
	)
	s.Require().NoError(err)
	s.Require().True(draw.ValuationComplete)
	s.Require().Equal(math.LegacyNewDec(200), draw.AggregateLiabilityNoah)
}

func (s *KeeperTestSuite) TestPrimeLiabilitySnapshotMarksUnavailableValuation() {
	tobinTaxes := []oracletypes.TobinTax{{Denom: chain.USDBaseDenom}}
	s.oracleKeeper.EXPECT().GetTobinTaxes(gomock.Any()).Return(tobinTaxes, nil).Times(2)
	s.bankKeeper.EXPECT().GetSupply(gomock.Any(), chain.USDBaseDenom).
		Return(sdk.NewInt64Coin(chain.USDBaseDenom, 100)).Times(1)
	s.oracleKeeper.EXPECT().GetRateSet(gomock.Any(), chain.USDBaseDenom).
		Return(nil, oracletypes.ErrStaleExchangeRate).Times(1)

	s.Require().NoError(s.keeper.PrimeLiabilitySnapshot(s.ctx))

	// The rest of the block reuses the marker without rescanning.
	draw, err := s.keeper.DrawRedemptionBuffer(
		s.ctx,
		sdk.NewInt64Coin(chain.USDBaseDenom, 10),
		math.NewInt(10),
		oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
			chain.USDBaseDenom:  math.LegacyOneDec(),
		},
	)
	s.Require().NoError(err)
	s.Require().False(draw.ValuationComplete)
	s.Require().True(draw.BufferPaid.IsZero())
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
