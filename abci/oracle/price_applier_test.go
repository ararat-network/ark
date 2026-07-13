package oracle_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/abci/oracle"
	abcitestutil "ark/abci/testutil"
	vetypes "ark/abci/ve/types"
	oracletypes "ark/x/oracle/types"
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
		expectedTargets   []string
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
				voteTargets := []string{"uusd"}
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
				keeper.EXPECT().RecordVoteAccounting(gomock.Any(), val1, math.NewInt(1), false).Return(nil)
				keeper.EXPECT().RecordVoteAccounting(gomock.Any(), val2, math.NewInt(1), false).Return(nil)
			},
			expectedPrices: map[string]math.LegacyDec{
				"uusd": math.LegacyNewDec(100),
			},
			expectedTargets: []string{"uusd"},
		},
		{
			name: "failed quorum target remains accountable for missed votes",
			req: &cometabci.RequestFinalizeBlock{
				Height: 3,
				Txs:    [][]byte{commitBz},
			},
			setup: func(t *testing.T, keeper *abcitestutil.MockOracleKeeper, veCodec *abcitestutil.MockVoteExtensionCodec, extCommitCodec *abcitestutil.MockExtendedCommitCodec) {
				voteTargets := []string{"ukrw", "uusd"}
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
				keeper.EXPECT().RecordVoteAccounting(gomock.Any(), val1, math.NewInt(10), false).Return(nil)
				keeper.EXPECT().RecordVoteAccounting(gomock.Any(), val2, math.NewInt(10), true).Return(nil)
			},
			expectedPrices: map[string]math.LegacyDec{
				"uusd": math.LegacyNewDec(100),
			},
			expectedTargets: []string{"ukrw", "uusd"},
		},
		{
			name: "invalid payload is classified as a missed report",
			req: &cometabci.RequestFinalizeBlock{
				Height: 3,
				Txs:    [][]byte{commitBz},
			},
			setup: func(t *testing.T, keeper *abcitestutil.MockOracleKeeper, veCodec *abcitestutil.MockVoteExtensionCodec, extCommitCodec *abcitestutil.MockExtendedCommitCodec) {
				voteTargets := []string{"uusd"}
				params := oracletypes.DefaultParams()
				params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
				validBz := []byte("valid")
				invalidBz := []byte("invalid")

				extCommitCodec.EXPECT().Decode(commitBz).Return(cometabci.ExtendedCommitInfo{
					Votes: []cometabci.ExtendedVoteInfo{
						abcitestutil.NewCommitExtendedVoteInfo(val1, 67, validBz),
						abcitestutil.NewCommitExtendedVoteInfo(val2, 33, invalidBz),
					},
				}, nil)
				veCodec.EXPECT().Decode(validBz).Return(abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
				}), nil)
				veCodec.EXPECT().Decode(invalidBz).Return(vetypes.OracleVoteExtension{}, errors.New("decode failed"))
				keeper.EXPECT().GetParams(gomock.Any()).Return(params, nil)
				keeper.EXPECT().GetVoteTargets(gomock.Any()).Return(voteTargets, nil)
				keeper.EXPECT().
					SetExchangeRateWithEvent(gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, exchangeRate oracletypes.ExchangeRate) error {
						require.Equal(t, "uusd", exchangeRate.Denom)
						require.True(t, math.LegacyNewDec(100).Equal(exchangeRate.Rate))
						return nil
					})
				keeper.EXPECT().RecordVoteAccounting(gomock.Any(), val1, math.NewInt(67), false).Return(nil)
				keeper.EXPECT().RecordVoteAccounting(gomock.Any(), val2, math.ZeroInt(), true).Return(nil)
			},
			expectedPrices: map[string]math.LegacyDec{
				"uusd": math.LegacyNewDec(100),
			},
			expectedTargets: []string{"uusd"},
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
				keeper,
				veCodec,
				extCommitCodec,
				log.NewTestLogger(t),
			)

			result, err := priceApplier.ApplyPricesFromVoteExtensions(abcitestutil.NewSDKContext(3, 0), tc.req)
			if tc.expectErr {
				require.Error(t, err)
				if tc.expectedErrorType != nil {
					require.IsType(t, tc.expectedErrorType, err)
				}
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.expectedPrices, result.Prices)
			require.Equal(t, tc.expectedTargets, result.VoteTargets)
		})
	}
}
