package voteextension_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"ark/abci/codec"
	abcitestutil "ark/abci/testutil"
	"ark/abci/voteextension"
	vetypes "ark/abci/voteextension/types"
	"ark/pricefeed/api"
	oracletypes "ark/x/oracle/types"
)

func TestExtendVoteHandler(t *testing.T) {
	validRate := abcitestutil.MustEncodeRate(t, math.LegacyNewDec(100))
	validPrices := map[string][]byte{"ausd": validRate}
	targets := oracletypes.FeedSet{
		Version: oracletypes.InitialFeedVersion,
		Denoms:  []string{"ausd"},
	}
	validVoteExt := vetypes.OracleVoteExtension{
		Rates:         validPrices,
		TargetVersion: targets.Version,
	}
	unavailableVoteExt := vetypes.OracleVoteExtension{
		Rates:         map[string][]byte{},
		TargetVersion: targets.Version,
	}
	encodedVoteExt := abcitestutil.MustEncodeVoteExtension(t, validVoteExt)
	encodedUnavailableVoteExt := abcitestutil.MustEncodeVoteExtension(t, unavailableVoteExt)
	panicCause := errors.New("boom")

	testCases := []struct {
		name              string
		req               *cometabci.RequestExtendVote
		setup             func(*abcitestutil.MockPriceFeedClient)
		targetErr         error
		expectedExtension []byte
		expectResp        bool
		expectErr         bool
		expectedErrIs     error
	}{
		{
			name:      "nil request returns error",
			req:       nil,
			expectErr: true,
		},
		{
			name:              "feed lookup error returns empty vote extension",
			req:               &cometabci.RequestExtendVote{Height: 10},
			targetErr:         errors.New("feeds unavailable"),
			expectedExtension: []byte{},
			expectResp:        true,
		},
		{
			name: "oracle client error returns empty vote extension",
			req:  &cometabci.RequestExtendVote{Height: 10},
			setup: func(oracleClient *abcitestutil.MockPriceFeedClient) {
				oracleClient.EXPECT().
					Prices(gomock.Any(), &api.PricesRequest{}).
					Return(nil, errors.New("oracle unavailable"))
			},
			expectedExtension: []byte{},
			expectResp:        true,
		},
		{
			name: "nil oracle response returns empty vote extension",
			req:  &cometabci.RequestExtendVote{Height: 10},
			setup: func(oracleClient *abcitestutil.MockPriceFeedClient) {
				oracleClient.EXPECT().
					Prices(gomock.Any(), &api.PricesRequest{}).
					Return(nil, nil)
			},
			expectedExtension: []byte{},
			expectResp:        true,
		},
		{
			name: "undecodable rate degrades to an empty report",
			req:  &cometabci.RequestExtendVote{Height: 10},
			setup: func(oracleClient *abcitestutil.MockPriceFeedClient) {
				oracleClient.EXPECT().
					Prices(gomock.Any(), &api.PricesRequest{}).
					Return(&api.PricesResponse{
						Prices: map[string][]byte{"ausd": []byte("invalid")},
					}, nil)
			},
			expectedExtension: encodedUnavailableVoteExt,
			expectResp:        true,
		},
		{
			name: "fresh sparse response encodes target unavailability",
			req:  &cometabci.RequestExtendVote{Height: 10},
			setup: func(oracleClient *abcitestutil.MockPriceFeedClient) {
				oracleClient.EXPECT().
					Prices(gomock.Any(), &api.PricesRequest{}).
					Return(&api.PricesResponse{Prices: map[string][]byte{}}, nil)
			},
			expectedExtension: encodedUnavailableVoteExt,
			expectResp:        true,
		},
		{
			name: "valid prices are encoded into vote extension",
			req:  &cometabci.RequestExtendVote{Height: 10},
			setup: func(oracleClient *abcitestutil.MockPriceFeedClient) {
				oracleClient.EXPECT().
					Prices(gomock.Any(), &api.PricesRequest{}).
					Return(&api.PricesResponse{Prices: validPrices}, nil)
			},
			expectedExtension: encodedVoteExt,
			expectResp:        true,
		},
		{
			name: "panic returns empty vote extension and error",
			req:  &cometabci.RequestExtendVote{Height: 10},
			setup: func(oracleClient *abcitestutil.MockPriceFeedClient) {
				oracleClient.EXPECT().
					Prices(gomock.Any(), &api.PricesRequest{}).
					DoAndReturn(func(context.Context, *api.PricesRequest, ...grpc.CallOption) (*api.PricesResponse, error) {
						panic(panicCause)
					})
			},
			expectedExtension: []byte{},
			expectResp:        true,
			expectErr:         true,
			expectedErrIs:     panicCause,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			oracleClient := abcitestutil.NewMockPriceFeedClient(ctrl)
			oracleKeeper := abcitestutil.NewMockOracleKeeper(ctrl)
			if tc.req != nil {
				oracleKeeper.EXPECT().
					GetFeeds(gomock.Any(), tc.req.Height).
					Return(targets, tc.targetErr)
			}
			if tc.setup != nil {
				tc.setup(oracleClient)
			}

			handler := voteextension.NewHandler(
				log.NewNopLogger(),
				oracleClient,
				oracleKeeper,
				time.Second,
			).ExtendVoteHandler()

			resp, err := handler(newVoteExtensionContext(10, 2), tc.req)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tc.expectedErrIs != nil {
				require.ErrorIs(t, err, tc.expectedErrIs)
			}
			if tc.expectResp {
				require.NotNil(t, resp)
				require.Equal(t, tc.expectedExtension, resp.VoteExtension)
			} else {
				require.Nil(t, resp)
			}
		})
	}
}

func TestExtendVoteHandlerDropsUndecodableRates(t *testing.T) {
	validRate := abcitestutil.MustEncodeRate(t, math.LegacyNewDec(100))
	zeroRate := abcitestutil.MustEncodeRate(t, math.LegacyZeroDec())
	negativeRate := abcitestutil.MustEncodeRate(t, math.LegacyNewDec(-1))
	// One byte past the vote-rate size bound: decodable as a LegacyDec but
	// oversized for a vote extension.
	oversizedRate := abcitestutil.MustEncodeRate(
		t,
		math.LegacyMustNewDecFromStr("1"+strings.Repeat("0", 22)),
	)
	require.Len(t, oversizedRate, oracletypes.MaxEncodedVoteRateBytes+1)
	targets := oracletypes.FeedSet{
		Version: oracletypes.InitialFeedVersion,
		Denoms:  []string{"ajpy", "ausd"},
	}

	testCases := []struct {
		name          string
		prices        map[string][]byte
		expectedRates map[string][]byte
	}{
		{
			name: "undecodable rate is dropped without aborting the report",
			prices: map[string][]byte{
				"ajpy": []byte("invalid"),
				"ausd": validRate,
			},
			expectedRates: map[string][]byte{"ausd": validRate},
		},
		{
			name: "oversized rate is dropped without aborting the report",
			prices: map[string][]byte{
				"ajpy": oversizedRate,
				"ausd": validRate,
			},
			expectedRates: map[string][]byte{"ausd": validRate},
		},
		{
			name: "zero rate is carried as an explicit abstention",
			prices: map[string][]byte{
				"ajpy": zeroRate,
				"ausd": validRate,
			},
			expectedRates: map[string][]byte{"ajpy": zeroRate, "ausd": validRate},
		},
		{
			name: "negative rate is carried as an explicit abstention",
			prices: map[string][]byte{
				"ajpy": negativeRate,
				"ausd": validRate,
			},
			expectedRates: map[string][]byte{"ajpy": negativeRate, "ausd": validRate},
		},
		{
			name: "dropping every rate still submits an empty report",
			prices: map[string][]byte{
				"ajpy": []byte("invalid"),
			},
			expectedRates: map[string][]byte{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			oracleClient := abcitestutil.NewMockPriceFeedClient(ctrl)
			oracleKeeper := abcitestutil.NewMockOracleKeeper(ctrl)
			req := &cometabci.RequestExtendVote{Height: 10}
			oracleKeeper.EXPECT().
				GetFeeds(gomock.Any(), req.Height).
				Return(targets, nil)
			oracleClient.EXPECT().
				Prices(gomock.Any(), &api.PricesRequest{}).
				Return(&api.PricesResponse{Prices: tc.prices}, nil)

			handler := voteextension.NewHandler(
				log.NewNopLogger(),
				oracleClient,
				oracleKeeper,
				time.Second,
			).ExtendVoteHandler()

			resp, err := handler(newVoteExtensionContext(10, 2), req)
			require.NoError(t, err)
			require.NotNil(t, resp)

			// Multi-entry rate maps do not encode with a canonical byte order,
			// so assertions compare the decoded report.
			decoded, err := codec.DecodeVoteExtension(resp.VoteExtension)
			require.NoError(t, err)
			require.Equal(t, targets.Version, decoded.TargetVersion)
			if len(tc.expectedRates) == 0 {
				require.Empty(t, decoded.Rates)
				return
			}
			require.Equal(t, tc.expectedRates, decoded.Rates)
		})
	}
}

func TestVerifyVoteExtensionHandler(t *testing.T) {
	validRate := abcitestutil.MustEncodeRate(t, math.LegacyNewDec(100))
	targets := oracletypes.FeedSet{
		Version: oracletypes.InitialFeedVersion,
		Denoms:  []string{"ausd"},
	}
	validVoteExt := vetypes.OracleVoteExtension{
		Rates:         map[string][]byte{"ausd": validRate},
		TargetVersion: targets.Version,
	}
	invalidVoteExt := vetypes.OracleVoteExtension{
		Rates:         map[string][]byte{"bad denom": validRate},
		TargetVersion: targets.Version,
	}
	wrongVersionVoteExt := validVoteExt
	wrongVersionVoteExt.TargetVersion++
	unversionedVoteExt := validVoteExt
	unversionedVoteExt.TargetVersion = 0

	testCases := []struct {
		name           string
		req            *cometabci.RequestVerifyVoteExtension
		targetErr      error
		expectedStatus cometabci.ResponseVerifyVoteExtension_VerifyStatus
		expectResp     bool
		expectErr      bool
	}{
		{
			name:      "nil request returns error",
			req:       nil,
			expectErr: true,
		},
		{
			name: "empty vote extension is accepted",
			req: &cometabci.RequestVerifyVoteExtension{
				Height:        10,
				VoteExtension: nil,
			},
			expectedStatus: cometabci.ResponseVerifyVoteExtension_ACCEPT,
			expectResp:     true,
		},
		{
			name: "decode error rejects vote extension",
			req: &cometabci.RequestVerifyVoteExtension{
				Height:        10,
				VoteExtension: []byte("not-zlib"),
			},
			expectedStatus: cometabci.ResponseVerifyVoteExtension_REJECT,
			expectResp:     true,
			expectErr:      true,
		},
		{
			name: "decoded invalid prices reject vote extension",
			req: &cometabci.RequestVerifyVoteExtension{
				Height:        10,
				VoteExtension: abcitestutil.MustEncodeVoteExtension(t, invalidVoteExt),
			},
			expectedStatus: cometabci.ResponseVerifyVoteExtension_REJECT,
			expectResp:     true,
			expectErr:      true,
		},
		{
			name: "feed lookup error rejects vote extension",
			req: &cometabci.RequestVerifyVoteExtension{
				Height:        10,
				VoteExtension: abcitestutil.MustEncodeVoteExtension(t, validVoteExt),
			},
			targetErr:      errors.New("feeds unavailable"),
			expectedStatus: cometabci.ResponseVerifyVoteExtension_REJECT,
			expectResp:     true,
			expectErr:      true,
		},
		{
			name: "wrong target version rejects vote extension",
			req: &cometabci.RequestVerifyVoteExtension{
				Height:        10,
				VoteExtension: abcitestutil.MustEncodeVoteExtension(t, wrongVersionVoteExt),
			},
			expectedStatus: cometabci.ResponseVerifyVoteExtension_REJECT,
			expectResp:     true,
			expectErr:      true,
		},
		{
			name: "unversioned report is rejected",
			req: &cometabci.RequestVerifyVoteExtension{
				Height:        10,
				VoteExtension: abcitestutil.MustEncodeVoteExtension(t, unversionedVoteExt),
			},
			expectedStatus: cometabci.ResponseVerifyVoteExtension_REJECT,
			expectResp:     true,
			expectErr:      true,
		},
		{
			name: "decoded valid prices accept vote extension",
			req: &cometabci.RequestVerifyVoteExtension{
				Height:        10,
				VoteExtension: abcitestutil.MustEncodeVoteExtension(t, validVoteExt),
			},
			expectedStatus: cometabci.ResponseVerifyVoteExtension_ACCEPT,
			expectResp:     true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			oracleKeeper := abcitestutil.NewMockOracleKeeper(ctrl)
			oracleKeeper.EXPECT().
				GetFeeds(gomock.Any(), int64(10)).
				Return(targets, tc.targetErr).
				AnyTimes()
			handler := voteextension.NewHandler(
				log.NewNopLogger(),
				nil,
				oracleKeeper,
				time.Second,
			).VerifyVoteExtensionHandler()

			resp, err := handler(newVoteExtensionContext(10, 2), tc.req)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tc.expectResp {
				require.NotNil(t, resp)
				require.Equal(t, tc.expectedStatus, resp.Status)
			} else {
				require.Nil(t, resp)
			}
		})
	}
}
