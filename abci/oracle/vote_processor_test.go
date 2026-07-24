package oracle_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"ark/abci/codec"
	"ark/abci/oracle"
	abcitestutil "ark/abci/testutil"
	arkabci "ark/abci/types"
	vetypes "ark/abci/voteextension/types"
	oracletypes "ark/x/oracle/types"
)

func TestProcessVoteExtensions(t *testing.T) {
	voteExtensionCodec := codec.NewVoteExtensionCodec()
	val1 := sdk.ConsAddress("validator1")
	val2 := sdk.ConsAddress("validator2")
	val3 := sdk.ConsAddress("validator3")
	voteTargetsErr := errors.New("vote targets unavailable")

	testCases := []struct {
		name            string
		req             *cometabci.RequestFinalizeBlock
		setup           func(*testing.T, *abcitestutil.MockOracleKeeper) [][]byte
		expectErr       bool
		expectedPrices  map[string]math.LegacyDec
		expectedErrorIs error
		expectedCause   error
	}{
		{
			name: "vote target lookup preserves keeper category and cause",
			req: &cometabci.RequestFinalizeBlock{
				Height: 3,
			},
			expectErr:       true,
			expectedErrorIs: arkabci.ErrOracleKeeper,
			expectedCause:   voteTargetsErr,
			setup: func(_ *testing.T, keeper *abcitestutil.MockOracleKeeper) [][]byte {
				keeper.EXPECT().GetVoteTargets(gomock.Any(), int64(2)).Return(oracletypes.VoteTargetSet{}, voteTargetsErr)
				return nil
			},
		},
		{
			name: "missing injected commit info preserves missing commit classification",
			req: &cometabci.RequestFinalizeBlock{
				Height: 3,
				Txs:    nil,
			},
			expectErr:       true,
			expectedErrorIs: arkabci.ErrMissingCommitInfo,
			setup: func(_ *testing.T, keeper *abcitestutil.MockOracleKeeper) [][]byte {
				keeper.EXPECT().GetVoteTargets(gomock.Any(), int64(2)).Return(oracletypes.VoteTargetSet{
					Version: oracletypes.InitialVoteTargetVersion,
					Denoms:  []string{"uusd"},
				}, nil)
				return nil
			},
		},
		{
			name: "valid quorum writes exchange rate and score weights",
			req: &cometabci.RequestFinalizeBlock{
				Height:            3,
				DecidedLastCommit: cometabci.CommitInfo{Votes: make([]cometabci.VoteInfo, 2)},
			},
			setup: func(t *testing.T, keeper *abcitestutil.MockOracleKeeper) [][]byte {
				voteTargets := oracletypes.VoteTargetSet{
					Version: oracletypes.InitialVoteTargetVersion,
					Denoms:  []string{"uusd"},
				}
				params := oracletypes.DefaultParams()
				params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
				voteExtension := abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
				})
				ve1Bz := abcitestutil.MustEncodeVoteExtension(t, voteExtension)
				ve2Bz := abcitestutil.MustEncodeVoteExtension(t, voteExtension)
				commitBz := abcitestutil.MustEncodeExtendedCommit(t, cometabci.ExtendedCommitInfo{
					Votes: []cometabci.ExtendedVoteInfo{
						abcitestutil.NewExtendedVoteInfo(val1, 1, ve1Bz),
						abcitestutil.NewExtendedVoteInfo(val2, 1, ve2Bz),
					},
				})
				keeper.EXPECT().GetParams(gomock.Any()).Return(params, nil)
				keeper.EXPECT().GetVoteTargets(gomock.Any(), int64(2)).Return(voteTargets, nil)
				keeper.EXPECT().
					SetExchangeRateWithEvent(gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, exchangeRate oracletypes.ExchangeRate) error {
						require.Equal(t, "uusd", exchangeRate.Denom)
						require.True(t, math.LegacyNewDec(100).Equal(exchangeRate.Rate))
						return nil
					})
				keeper.EXPECT().RecordVoteAccounting(gomock.Any(), val1, math.NewInt(1), false).Return(nil)
				keeper.EXPECT().RecordVoteAccounting(gomock.Any(), val2, math.NewInt(1), false).Return(nil)
				return [][]byte{commitBz}
			},
			expectedPrices: map[string]math.LegacyDec{
				"uusd": math.LegacyNewDec(100),
			},
		},
		{
			name: "failed quorum target remains accountable for missed votes",
			req: &cometabci.RequestFinalizeBlock{
				Height:            3,
				DecidedLastCommit: cometabci.CommitInfo{Votes: make([]cometabci.VoteInfo, 2)},
			},
			setup: func(t *testing.T, keeper *abcitestutil.MockOracleKeeper) [][]byte {
				voteTargets := oracletypes.VoteTargetSet{
					Version: oracletypes.InitialVoteTargetVersion,
					Denoms:  []string{"ukrw", "uusd"},
				}
				params := oracletypes.DefaultParams()
				params.VoteThreshold = math.LegacyNewDecWithPrec(75, 2)
				ve1Bz := abcitestutil.MustEncodeVoteExtension(t, abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
					"ukrw": math.LegacyNewDec(1000),
				}))
				ve2Bz := abcitestutil.MustEncodeVoteExtension(t, abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
				}))
				commitBz := abcitestutil.MustEncodeExtendedCommit(t, cometabci.ExtendedCommitInfo{
					Votes: []cometabci.ExtendedVoteInfo{
						abcitestutil.NewExtendedVoteInfo(val1, 10, ve1Bz),
						abcitestutil.NewExtendedVoteInfo(val2, 10, ve2Bz),
					},
				})
				keeper.EXPECT().GetParams(gomock.Any()).Return(params, nil)
				keeper.EXPECT().GetVoteTargets(gomock.Any(), int64(2)).Return(voteTargets, nil)
				keeper.EXPECT().
					SetExchangeRateWithEvent(gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, exchangeRate oracletypes.ExchangeRate) error {
						require.Equal(t, "uusd", exchangeRate.Denom)
						require.True(t, math.LegacyNewDec(100).Equal(exchangeRate.Rate))
						return nil
					})
				keeper.EXPECT().RecordVoteAccounting(gomock.Any(), val1, math.NewInt(10), false).Return(nil)
				keeper.EXPECT().RecordVoteAccounting(gomock.Any(), val2, math.NewInt(10), true).Return(nil)
				return [][]byte{commitBz}
			},
			expectedPrices: map[string]math.LegacyDec{
				"uusd": math.LegacyNewDec(100),
			},
		},
		{
			name: "invalid payload does not contribute target unavailability power",
			req: &cometabci.RequestFinalizeBlock{
				Height:            3,
				DecidedLastCommit: cometabci.CommitInfo{Votes: make([]cometabci.VoteInfo, 3)},
			},
			setup: func(t *testing.T, keeper *abcitestutil.MockOracleKeeper) [][]byte {
				voteTargets := oracletypes.VoteTargetSet{
					Version: oracletypes.InitialVoteTargetVersion,
					Denoms:  []string{"uusd"},
				}
				params := oracletypes.DefaultParams()
				params.VoteThreshold = math.LegacyNewDecWithPrec(67, 2)
				unavailableBz := abcitestutil.MustEncodeVoteExtension(t, vetypes.OracleVoteExtension{
					TargetVersion: voteTargets.Version,
				})
				positiveBz := abcitestutil.MustEncodeVoteExtension(t, abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
				}))
				commitBz := abcitestutil.MustEncodeExtendedCommit(t, cometabci.ExtendedCommitInfo{
					Votes: []cometabci.ExtendedVoteInfo{
						abcitestutil.NewCommitExtendedVoteInfo(val1, 40, []byte("not-zlib")),
						abcitestutil.NewCommitExtendedVoteInfo(val2, 30, unavailableBz),
						abcitestutil.NewCommitExtendedVoteInfo(val3, 30, positiveBz),
					},
				})
				keeper.EXPECT().GetParams(gomock.Any()).Return(params, nil)
				keeper.EXPECT().GetVoteTargets(gomock.Any(), int64(2)).Return(voteTargets, nil)
				keeper.EXPECT().RecordVoteAccounting(gomock.Any(), val1, math.ZeroInt(), true).Return(nil)
				keeper.EXPECT().RecordVoteAccounting(gomock.Any(), val2, math.ZeroInt(), true).Return(nil)
				keeper.EXPECT().RecordVoteAccounting(gomock.Any(), val3, math.ZeroInt(), false).Return(nil)
				return [][]byte{commitBz}
			},
			expectedPrices: map[string]math.LegacyDec{},
		},
		{
			name: "invalid payload is classified as a missed report",
			req: &cometabci.RequestFinalizeBlock{
				Height:            3,
				DecidedLastCommit: cometabci.CommitInfo{Votes: make([]cometabci.VoteInfo, 2)},
			},
			setup: func(t *testing.T, keeper *abcitestutil.MockOracleKeeper) [][]byte {
				voteTargets := oracletypes.VoteTargetSet{
					Version: oracletypes.InitialVoteTargetVersion,
					Denoms:  []string{"uusd"},
				}
				params := oracletypes.DefaultParams()
				params.VoteThreshold = math.LegacyNewDecWithPrec(50, 2)
				validBz := abcitestutil.MustEncodeVoteExtension(t, abcitestutil.NewOracleVoteExtension(t, map[string]math.LegacyDec{
					"uusd": math.LegacyNewDec(100),
				}))
				commitBz := abcitestutil.MustEncodeExtendedCommit(t, cometabci.ExtendedCommitInfo{
					Votes: []cometabci.ExtendedVoteInfo{
						abcitestutil.NewCommitExtendedVoteInfo(val1, 67, validBz),
						abcitestutil.NewCommitExtendedVoteInfo(val2, 33, []byte("not-zlib")),
					},
				})
				keeper.EXPECT().GetParams(gomock.Any()).Return(params, nil)
				keeper.EXPECT().GetVoteTargets(gomock.Any(), int64(2)).Return(voteTargets, nil)
				keeper.EXPECT().
					SetExchangeRateWithEvent(gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, exchangeRate oracletypes.ExchangeRate) error {
						require.Equal(t, "uusd", exchangeRate.Denom)
						require.True(t, math.LegacyNewDec(100).Equal(exchangeRate.Rate))
						return nil
					})
				keeper.EXPECT().RecordVoteAccounting(gomock.Any(), val1, math.NewInt(67), false).Return(nil)
				keeper.EXPECT().RecordVoteAccounting(gomock.Any(), val2, math.ZeroInt(), true).Return(nil)
				return [][]byte{commitBz}
			},
			expectedPrices: map[string]math.LegacyDec{
				"uusd": math.LegacyNewDec(100),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			keeper := abcitestutil.NewMockOracleKeeper(ctrl)
			if tc.setup != nil {
				tc.req.Txs = tc.setup(t, keeper)
			}
			prices, err := oracle.ProcessVoteExtensions(
				abcitestutil.NewSDKContext(3, 0),
				keeper,
				voteExtensionCodec,
				tc.req,
			)
			if tc.expectErr {
				require.Error(t, err)
				if tc.expectedErrorIs != nil {
					require.ErrorIs(t, err, tc.expectedErrorIs)
				}
				if tc.expectedCause != nil {
					require.ErrorIs(t, err, tc.expectedCause)
				}
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.expectedPrices, prices)
		})
	}
}
