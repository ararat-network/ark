package oracle_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"noah/abci/oracle"
	abcitestutil "noah/abci/testutil"
	oracletypes "noah/x/oracle/types"
)

func TestApplyPricesFromVoteExtensions(t *testing.T) {
	commitBz := []byte("commit")
	val1 := sdk.ConsAddress("validator1")
	val2 := sdk.ConsAddress("validator2")

	testCases := []struct {
		name              string
		req               *cometabci.RequestFinalizeBlock
		setup             func(*testing.T, *abcitestutil.MockOracleKeeper, *abcitestutil.MockVoteExtensionCodec, *abcitestutil.MockExtendedCommitCodec)
		expectErr         bool
		expectedPrices    map[string]math.LegacyDec
		expectedTargets   map[string]math.LegacyDec
		expectedErrorType any
	}{
		{
			name: "missing injected commit info returns oracle keeper error",
			req: &cometabci.RequestFinalizeBlock{
				Height: 3,
				Txs:    nil,
			},
			expectErr:         true,
			expectedErrorType: oracle.OracleKeeperError{},
		},
		{
			name: "valid quorum writes exchange rate and score weights",
			req: &cometabci.RequestFinalizeBlock{
				Height: 3,
				Txs:    [][]byte{commitBz},
			},
			setup: func(t *testing.T, keeper *abcitestutil.MockOracleKeeper, veCodec *abcitestutil.MockVoteExtensionCodec, extCommitCodec *abcitestutil.MockExtendedCommitCodec) {
				voteTargets := map[string]math.LegacyDec{
					"uusd": math.LegacyZeroDec(),
				}
				params := oracletypes.DefaultParams()
				params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
				ve1Bz := []byte("ve1")
				ve2Bz := []byte("ve2")

				extCommitCodec.EXPECT().Decode(commitBz).Return(cometabci.ExtendedCommitInfo{
					Votes: []cometabci.ExtendedVoteInfo{
						abcitestutil.NewExtendedVoteInfo(val1, 1, ve1Bz),
						abcitestutil.NewExtendedVoteInfo(val2, 1, ve2Bz),
					},
				}, nil)
				veCodec.EXPECT().Decode(ve1Bz).Return(abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
				}), nil)
				veCodec.EXPECT().Decode(ve2Bz).Return(abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
				}), nil)
				keeper.EXPECT().GetParams(gomock.Any()).Return(params, nil)
				keeper.EXPECT().GetVoteTargets(gomock.Any()).Return(voteTargets, nil)
				keeper.EXPECT().
					SetExchangeRateWithEvent(gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, exchangeRate oracletypes.ExchangeRate) error {
						require.Equal(t, "uusd", exchangeRate.Denom)
						require.True(t, math.LegacyNewDec(100).Equal(exchangeRate.Rate))
						return nil
					})
				keeper.EXPECT().AddScoreWeight(gomock.Any(), val1, uint64(1)).Return(nil)
				keeper.EXPECT().AddScoreWeight(gomock.Any(), val2, uint64(1)).Return(nil)
			},
			expectedPrices: map[string]math.LegacyDec{
				"uusd": math.LegacyNewDec(100),
			},
			expectedTargets: map[string]math.LegacyDec{
				"uusd": math.LegacyZeroDec(),
			},
		},
		{
			name: "failed quorum target remains accountable for missed votes",
			req: &cometabci.RequestFinalizeBlock{
				Height: 3,
				Txs:    [][]byte{commitBz},
			},
			setup: func(t *testing.T, keeper *abcitestutil.MockOracleKeeper, veCodec *abcitestutil.MockVoteExtensionCodec, extCommitCodec *abcitestutil.MockExtendedCommitCodec) {
				voteTargets := map[string]math.LegacyDec{
					"uusd": math.LegacyZeroDec(),
					"ukrw": math.LegacyZeroDec(),
				}
				params := oracletypes.DefaultParams()
				params.VoteThreshold = math.LegacyNewDecWithPrec(75, 2)
				ve1Bz := []byte("ve1")
				ve2Bz := []byte("ve2")

				extCommitCodec.EXPECT().Decode(commitBz).Return(cometabci.ExtendedCommitInfo{
					Votes: []cometabci.ExtendedVoteInfo{
						abcitestutil.NewExtendedVoteInfo(val1, 10, ve1Bz),
						abcitestutil.NewExtendedVoteInfo(val2, 10, ve2Bz),
					},
				}, nil)
				veCodec.EXPECT().Decode(ve1Bz).Return(abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
					"ukrw": math.LegacyNewDec(1000),
				}), nil)
				veCodec.EXPECT().Decode(ve2Bz).Return(abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
				}), nil)
				keeper.EXPECT().GetParams(gomock.Any()).Return(params, nil)
				keeper.EXPECT().GetVoteTargets(gomock.Any()).Return(voteTargets, nil)
				keeper.EXPECT().
					SetExchangeRateWithEvent(gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, exchangeRate oracletypes.ExchangeRate) error {
						require.Equal(t, "uusd", exchangeRate.Denom)
						require.True(t, math.LegacyNewDec(100).Equal(exchangeRate.Rate))
						return nil
					})
				keeper.EXPECT().AddScoreWeight(gomock.Any(), val1, uint64(10)).Return(nil)
				keeper.EXPECT().AddScoreWeight(gomock.Any(), val2, uint64(10)).Return(nil)
				keeper.EXPECT().IncrementMissCount(gomock.Any(), val2).Return(nil)
			},
			expectedPrices: map[string]math.LegacyDec{
				"uusd": math.LegacyNewDec(100),
			},
			expectedTargets: map[string]math.LegacyDec{
				"uusd": math.LegacyZeroDec(),
				"ukrw": math.LegacyZeroDec(),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			keeper := abcitestutil.NewMockOracleKeeper(ctrl)
			veCodec := abcitestutil.NewMockVoteExtensionCodec(ctrl)
			extCommitCodec := abcitestutil.NewMockExtendedCommitCodec(ctrl)
			if tc.setup != nil {
				tc.setup(t, keeper, veCodec, extCommitCodec)
			}
			priceApplier := oracle.NewPriceApplier(
				oracle.NewVoteAggregator(log.NewTestLogger(t)),
				keeper,
				veCodec,
				extCommitCodec,
				log.NewTestLogger(t),
			)

			prices, voteTargets, err := priceApplier.ApplyPricesFromVoteExtensions(abcitestutil.NewSDKContext(3, 0), tc.req)
			if tc.expectErr {
				require.Error(t, err)
				if tc.expectedErrorType != nil {
					require.IsType(t, tc.expectedErrorType, err)
				}
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.expectedPrices, prices)
			require.Equal(t, tc.expectedTargets, voteTargets)
		})
	}
}
