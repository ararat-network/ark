package ve_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"

	cometabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	abcitestutil "ark/abci/testutil"
	"ark/abci/ve"
	vetypes "ark/abci/ve/types"
	transporttypes "ark/oracle/types"
)

func TestExtendVoteHandler(t *testing.T) {
	validRate := abcitestutil.MustEncodeRate(t, math.LegacyNewDec(100))
	validPrices := map[string][]byte{"uusd": validRate}
	validVoteExt := vetypes.OracleVoteExtension{Rates: validPrices}
	encodedVoteExt := []byte("encoded-vote-extension")

	testCases := []struct {
		name              string
		req               *cometabci.RequestExtendVote
		setup             func(*abcitestutil.MockOracleClient, *abcitestutil.MockVoteExtensionCodec)
		expectedExtension []byte
		expectResp        bool
		expectErr         bool
	}{
		{
			name:      "nil request is swallowed as non-panic error",
			req:       nil,
			expectErr: false,
		},
		{
			name: "oracle client error returns empty vote extension",
			req:  &cometabci.RequestExtendVote{Height: 10},
			setup: func(oracleClient *abcitestutil.MockOracleClient, _ *abcitestutil.MockVoteExtensionCodec) {
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
			setup: func(oracleClient *abcitestutil.MockOracleClient, _ *abcitestutil.MockVoteExtensionCodec) {
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
			setup: func(oracleClient *abcitestutil.MockOracleClient, _ *abcitestutil.MockVoteExtensionCodec) {
				oracleClient.EXPECT().
					Prices(gomock.Any(), &transporttypes.OraclePricesRequest{}).
					Return(&transporttypes.OraclePricesResponse{
						Prices: map[string][]byte{"bad denom": validRate},
					}, nil)
			},
			expectedExtension: []byte{},
			expectResp:        true,
		},
		{
			name: "codec encode error returns empty vote extension",
			req:  &cometabci.RequestExtendVote{Height: 10},
			setup: func(oracleClient *abcitestutil.MockOracleClient, voteExtensionCodec *abcitestutil.MockVoteExtensionCodec) {
				oracleClient.EXPECT().
					Prices(gomock.Any(), &transporttypes.OraclePricesRequest{}).
					Return(&transporttypes.OraclePricesResponse{Prices: validPrices}, nil)
				voteExtensionCodec.EXPECT().
					Encode(validVoteExt).
					Return(nil, errors.New("encode failed"))
			},
			expectedExtension: []byte{},
			expectResp:        true,
		},
		{
			name: "valid prices are encoded into vote extension",
			req:  &cometabci.RequestExtendVote{Height: 10},
			setup: func(oracleClient *abcitestutil.MockOracleClient, voteExtensionCodec *abcitestutil.MockVoteExtensionCodec) {
				oracleClient.EXPECT().
					Prices(gomock.Any(), &transporttypes.OraclePricesRequest{}).
					Return(&transporttypes.OraclePricesResponse{Prices: validPrices}, nil)
				voteExtensionCodec.EXPECT().
					Encode(validVoteExt).
					Return(encodedVoteExt, nil)
			},
			expectedExtension: encodedVoteExt,
			expectResp:        true,
		},
		{
			name: "panic returns empty vote extension and error",
			req:  &cometabci.RequestExtendVote{Height: 10},
			setup: func(oracleClient *abcitestutil.MockOracleClient, _ *abcitestutil.MockVoteExtensionCodec) {
				oracleClient.EXPECT().
					Prices(gomock.Any(), &transporttypes.OraclePricesRequest{}).
					DoAndReturn(func(sdk.Context, *transporttypes.OraclePricesRequest, ...grpc.CallOption) (*transporttypes.OraclePricesResponse, error) {
						panic("boom")
					})
			},
			expectedExtension: []byte{},
			expectResp:        true,
			expectErr:         true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			oracleClient := abcitestutil.NewMockOracleClient(ctrl)
			voteExtensionCodec := abcitestutil.NewMockVoteExtensionCodec(ctrl)
			if tc.setup != nil {
				tc.setup(oracleClient, voteExtensionCodec)
			}

			handler := ve.NewHandler(
				log.NewNopLogger(),
				oracleClient,
				time.Second,
				voteExtensionCodec,
			).ExtendVoteHandler()

			resp, err := handler(newVoteExtensionContext(10, 2), tc.req)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
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
	validRate := abcitestutil.MustEncodeRate(t, math.LegacyNewDec(100))
	validVoteExt := vetypes.OracleVoteExtension{Rates: map[string][]byte{
		"uusd": validRate,
	}}
	invalidVoteExt := vetypes.OracleVoteExtension{Rates: map[string][]byte{
		"bad denom": validRate,
	}}

	testCases := []struct {
		name           string
		req            *cometabci.RequestVerifyVoteExtension
		setup          func(*abcitestutil.MockVoteExtensionCodec)
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
				VoteExtension: []byte("encoded-vote-extension"),
			},
			setup: func(voteExtensionCodec *abcitestutil.MockVoteExtensionCodec) {
				voteExtensionCodec.EXPECT().
					Decode([]byte("encoded-vote-extension")).
					Return(vetypes.OracleVoteExtension{}, errors.New("decode failed"))
			},
			expectedStatus: cometabci.ResponseVerifyVoteExtension_REJECT,
			expectResp:     true,
			expectErr:      true,
		},
		{
			name: "decoded invalid prices reject vote extension",
			req: &cometabci.RequestVerifyVoteExtension{
				Height:        10,
				VoteExtension: []byte("encoded-vote-extension"),
			},
			setup: func(voteExtensionCodec *abcitestutil.MockVoteExtensionCodec) {
				voteExtensionCodec.EXPECT().
					Decode([]byte("encoded-vote-extension")).
					Return(invalidVoteExt, nil)
			},
			expectedStatus: cometabci.ResponseVerifyVoteExtension_REJECT,
			expectResp:     true,
			expectErr:      true,
		},
		{
			name: "decoded valid prices accept vote extension",
			req: &cometabci.RequestVerifyVoteExtension{
				Height:        10,
				VoteExtension: []byte("encoded-vote-extension"),
			},
			setup: func(voteExtensionCodec *abcitestutil.MockVoteExtensionCodec) {
				voteExtensionCodec.EXPECT().
					Decode([]byte("encoded-vote-extension")).
					Return(validVoteExt, nil)
			},
			expectedStatus: cometabci.ResponseVerifyVoteExtension_ACCEPT,
			expectResp:     true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			voteExtensionCodec := abcitestutil.NewMockVoteExtensionCodec(ctrl)
			if tc.setup != nil {
				tc.setup(voteExtensionCodec)
			}

			handler := ve.NewHandler(
				log.NewNopLogger(),
				nil,
				time.Second,
				voteExtensionCodec,
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
