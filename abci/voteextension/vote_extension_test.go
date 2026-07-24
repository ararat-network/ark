package voteextension_test

import (
	"context"
	"errors"
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
	transporttypes "ark/oracle/types"
	oracletypes "ark/x/oracle/types"
)

func TestExtendVoteHandler(t *testing.T) {
	voteExtensionCodec := codec.NewVoteExtensionCodec()
	validRate := abcitestutil.MustEncodeRate(t, math.LegacyNewDec(100))
	validPrices := map[string][]byte{"uusd": validRate}
	targets := oracletypes.VoteTargetSet{
		Version: oracletypes.InitialVoteTargetVersion,
		Denoms:  []string{"uusd"},
	}
	validVoteExt := vetypes.OracleVoteExtension{
		Rates:         validPrices,
		TargetVersion: targets.Version,
	}
	partialVoteExt := vetypes.OracleVoteExtension{
		Rates:         map[string][]byte{},
		TargetVersion: targets.Version,
	}
	encodedVoteExt := abcitestutil.MustEncodeVoteExtension(t, validVoteExt)
	encodedPartialVoteExt := abcitestutil.MustEncodeVoteExtension(t, partialVoteExt)
	panicCause := errors.New("boom")

	testCases := []struct {
		name              string
		req               *cometabci.RequestExtendVote
		setup             func(*abcitestutil.MockOracleClient)
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
			name:              "vote target error returns empty vote extension",
			req:               &cometabci.RequestExtendVote{Height: 10},
			targetErr:         errors.New("vote targets unavailable"),
			expectedExtension: []byte{},
			expectResp:        true,
		},
		{
			name: "oracle client error returns empty vote extension",
			req:  &cometabci.RequestExtendVote{Height: 10},
			setup: func(oracleClient *abcitestutil.MockOracleClient) {
				oracleClient.EXPECT().
					Prices(gomock.Any(), &transporttypes.OraclePricesRequest{}).
					Return(nil, errors.New("oracle unavailable"))
			},
			expectedExtension: []byte{},
			expectResp:        true,
		},
		{
			name: "nil oracle response returns empty vote extension",
			req:  &cometabci.RequestExtendVote{Height: 10},
			setup: func(oracleClient *abcitestutil.MockOracleClient) {
				oracleClient.EXPECT().
					Prices(gomock.Any(), &transporttypes.OraclePricesRequest{}).
					Return(nil, nil)
			},
			expectedExtension: []byte{},
			expectResp:        true,
		},
		{
			name: "invalid oracle prices return empty vote extension",
			req:  &cometabci.RequestExtendVote{Height: 10},
			setup: func(oracleClient *abcitestutil.MockOracleClient) {
				oracleClient.EXPECT().
					Prices(gomock.Any(), &transporttypes.OraclePricesRequest{}).
					Return(&transporttypes.OraclePricesResponse{
						Prices: map[string][]byte{"uusd": []byte("invalid")},
					}, nil)
			},
			expectedExtension: []byte{},
			expectResp:        true,
		},
		{
			name: "missing target price remains missing",
			req:  &cometabci.RequestExtendVote{Height: 10},
			setup: func(oracleClient *abcitestutil.MockOracleClient) {
				oracleClient.EXPECT().
					Prices(gomock.Any(), &transporttypes.OraclePricesRequest{}).
					Return(&transporttypes.OraclePricesResponse{Prices: map[string][]byte{}}, nil)
			},
			expectedExtension: encodedPartialVoteExt,
			expectResp:        true,
		},
		{
			name: "valid prices are encoded into vote extension",
			req:  &cometabci.RequestExtendVote{Height: 10},
			setup: func(oracleClient *abcitestutil.MockOracleClient) {
				oracleClient.EXPECT().
					Prices(gomock.Any(), &transporttypes.OraclePricesRequest{}).
					Return(&transporttypes.OraclePricesResponse{Prices: validPrices}, nil)
			},
			expectedExtension: encodedVoteExt,
			expectResp:        true,
		},
		{
			name: "panic returns empty vote extension and error",
			req:  &cometabci.RequestExtendVote{Height: 10},
			setup: func(oracleClient *abcitestutil.MockOracleClient) {
				oracleClient.EXPECT().
					Prices(gomock.Any(), &transporttypes.OraclePricesRequest{}).
					DoAndReturn(func(context.Context, *transporttypes.OraclePricesRequest, ...grpc.CallOption) (*transporttypes.OraclePricesResponse, error) {
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
			oracleClient := abcitestutil.NewMockOracleClient(ctrl)
			oracleKeeper := abcitestutil.NewMockOracleKeeper(ctrl)
			if tc.req != nil {
				oracleKeeper.EXPECT().
					GetVoteTargets(gomock.Any(), tc.req.Height).
					Return(targets, tc.targetErr)
			}
			if tc.setup != nil {
				tc.setup(oracleClient)
			}

			handler := voteextension.NewHandler(
				log.NewNopLogger(),
				oracleClient,
				oracleKeeper,
				voteExtensionCodec,
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

func TestVerifyVoteExtensionHandler(t *testing.T) {
	voteExtensionCodec := codec.NewVoteExtensionCodec()
	validRate := abcitestutil.MustEncodeRate(t, math.LegacyNewDec(100))
	targets := oracletypes.VoteTargetSet{
		Version: oracletypes.InitialVoteTargetVersion,
		Denoms:  []string{"uusd"},
	}
	validVoteExt := vetypes.OracleVoteExtension{
		Rates:         map[string][]byte{"uusd": validRate},
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
			name: "vote target error rejects vote extension",
			req: &cometabci.RequestVerifyVoteExtension{
				Height:        10,
				VoteExtension: abcitestutil.MustEncodeVoteExtension(t, validVoteExt),
			},
			targetErr:      errors.New("vote targets unavailable"),
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
				GetVoteTargets(gomock.Any(), int64(10)).
				Return(targets, tc.targetErr).
				AnyTimes()
			handler := voteextension.NewHandler(
				log.NewNopLogger(),
				nil,
				oracleKeeper,
				voteExtensionCodec,
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
