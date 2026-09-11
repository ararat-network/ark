package voteextension_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"

	cmtabci "github.com/cometbft/cometbft/abci/types"

	"cosmossdk.io/log/v2"
	"cosmossdk.io/math"

	"github.com/ararat-network/ark/abci/codec"
	abcitestutil "github.com/ararat-network/ark/abci/testutil"
	"github.com/ararat-network/ark/abci/voteextension"
	vetypes "github.com/ararat-network/ark/abci/voteextension/types"
	"github.com/ararat-network/ark/pricefeed/api"
	pricefeedclient "github.com/ararat-network/ark/pricefeed/client"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
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
		req               *cmtabci.RequestExtendVote
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
			req:               &cmtabci.RequestExtendVote{Height: 10},
			targetErr:         errors.New("feeds unavailable"),
			expectedExtension: []byte{},
			expectResp:        true,
		},
		{
			name: "oracle client error returns empty vote extension",
			req:  &cmtabci.RequestExtendVote{Height: 10},
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
			req:  &cmtabci.RequestExtendVote{Height: 10},
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
			req:  &cmtabci.RequestExtendVote{Height: 10},
			setup: func(oracleClient *abcitestutil.MockPriceFeedClient) {
				oracleClient.EXPECT().
					Prices(gomock.Any(), &api.PricesRequest{}).
					Return(&api.PricesResponse{
						Prices: map[string][]byte{"ausd": {0x00, 0x01}},
					}, nil)
			},
			expectedExtension: encodedUnavailableVoteExt,
			expectResp:        true,
		},
		{
			name: "fresh sparse response encodes target unavailability",
			req:  &cmtabci.RequestExtendVote{Height: 10},
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
			req:  &cmtabci.RequestExtendVote{Height: 10},
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
			req:  &cmtabci.RequestExtendVote{Height: 10},
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
				"ajpy": {0x00, 0x01},
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
			name: "empty rate bytes are dropped without aborting the report",
			prices: map[string][]byte{
				"ajpy": {},
				"ausd": validRate,
			},
			expectedRates: map[string][]byte{"ausd": validRate},
		},
		{
			name: "dropping every rate still submits an empty report",
			prices: map[string][]byte{
				"ajpy": {0x00, 0x01},
			},
			expectedRates: map[string][]byte{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			oracleClient := abcitestutil.NewMockPriceFeedClient(ctrl)
			oracleKeeper := abcitestutil.NewMockOracleKeeper(ctrl)
			req := &cmtabci.RequestExtendVote{Height: 10}
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
		req            *cmtabci.RequestVerifyVoteExtension
		targetErr      error
		expectedStatus cmtabci.ResponseVerifyVoteExtension_VerifyStatus
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
			req: &cmtabci.RequestVerifyVoteExtension{
				Height:        10,
				VoteExtension: nil,
			},
			expectedStatus: cmtabci.ResponseVerifyVoteExtension_ACCEPT,
			expectResp:     true,
		},
		{
			name: "decode error rejects vote extension",
			req: &cmtabci.RequestVerifyVoteExtension{
				Height:        10,
				VoteExtension: []byte("not-zlib"),
			},
			expectedStatus: cmtabci.ResponseVerifyVoteExtension_REJECT,
			expectResp:     true,
			expectErr:      true,
		},
		{
			name: "decoded invalid prices reject vote extension",
			req: &cmtabci.RequestVerifyVoteExtension{
				Height:        10,
				VoteExtension: abcitestutil.MustEncodeVoteExtension(t, invalidVoteExt),
			},
			expectedStatus: cmtabci.ResponseVerifyVoteExtension_REJECT,
			expectResp:     true,
			expectErr:      true,
		},
		{
			name: "feed lookup error rejects vote extension",
			req: &cmtabci.RequestVerifyVoteExtension{
				Height:        10,
				VoteExtension: abcitestutil.MustEncodeVoteExtension(t, validVoteExt),
			},
			targetErr:      errors.New("feeds unavailable"),
			expectedStatus: cmtabci.ResponseVerifyVoteExtension_REJECT,
			expectResp:     true,
			expectErr:      true,
		},
		{
			name: "wrong target version rejects vote extension",
			req: &cmtabci.RequestVerifyVoteExtension{
				Height:        10,
				VoteExtension: abcitestutil.MustEncodeVoteExtension(t, wrongVersionVoteExt),
			},
			expectedStatus: cmtabci.ResponseVerifyVoteExtension_REJECT,
			expectResp:     true,
			expectErr:      true,
		},
		{
			name: "unversioned report is rejected",
			req: &cmtabci.RequestVerifyVoteExtension{
				Height:        10,
				VoteExtension: abcitestutil.MustEncodeVoteExtension(t, unversionedVoteExt),
			},
			expectedStatus: cmtabci.ResponseVerifyVoteExtension_REJECT,
			expectResp:     true,
			expectErr:      true,
		},
		{
			name: "decoded valid prices accept vote extension",
			req: &cmtabci.RequestVerifyVoteExtension{
				Height:        10,
				VoteExtension: abcitestutil.MustEncodeVoteExtension(t, validVoteExt),
			},
			expectedStatus: cmtabci.ResponseVerifyVoteExtension_ACCEPT,
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

// The global binds package-level instruments to the first provider installed
// in the process, so this package gets one provider-installing test.
func TestExtendVoteHandlerRecordsCoverage(t *testing.T) {
	registry := prometheus.NewRegistry()
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registry))
	require.NoError(t, err)
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
	})
	otel.SetMeterProvider(provider)

	t.Run("verify panic propagates and counts as panic", func(t *testing.T) {
		keeper := abcitestutil.NewMockOracleKeeper(gomock.NewController(t))
		cause := errors.New("verify panic")
		keeper.EXPECT().GetFeeds(gomock.Any(), int64(10)).DoAndReturn(func(context.Context, int64) (oracletypes.FeedSet, error) { panic(cause) })
		vote := abcitestutil.MustEncodeVoteExtension(t, vetypes.OracleVoteExtension{Rates: map[string][]byte{"ausd": abcitestutil.MustEncodeRate(t, math.LegacyNewDec(1))}, TargetVersion: oracletypes.InitialFeedVersion})
		handler := voteextension.NewHandler(log.NewNopLogger(), nil, keeper, time.Second).VerifyVoteExtensionHandler()
		require.PanicsWithValue(t, cause, func() {
			_, _ = handler(newVoteExtensionContext(10, 2), &cmtabci.RequestVerifyVoteExtension{Height: 10, VoteExtension: vote})
		})
		families, err := registry.Gather()
		require.NoError(t, err)
		var count float64
		for _, f := range families {
			if f.GetName() == "ark_abci_requests_total" {
				for _, m := range f.Metric {
					if labelValue(m.Label, "method") == "verify_vote_extension" {
						require.Equal(t, "Panic", labelValue(m.Label, "status"))
						count += m.GetCounter().GetValue()
					}
				}
			}
		}
		require.Equal(t, 1.0, count)
	})

	targets := oracletypes.FeedSet{
		Version: oracletypes.InitialFeedVersion,
		Denoms:  []string{"aeur", "ajpy", "ausd"},
	}
	validRate := abcitestutil.MustEncodeRate(t, math.LegacyNewDec(100))
	req := &cmtabci.RequestExtendVote{Height: 10}

	extend := func(t *testing.T, prices map[string][]byte, clientErr error) *cmtabci.ResponseExtendVote {
		t.Helper()
		ctrl := gomock.NewController(t)
		oracleClient := abcitestutil.NewMockPriceFeedClient(ctrl)
		oracleKeeper := abcitestutil.NewMockOracleKeeper(ctrl)
		oracleKeeper.EXPECT().GetFeeds(gomock.Any(), req.Height).Return(targets, nil)
		if clientErr != nil {
			oracleClient.EXPECT().Prices(gomock.Any(), &api.PricesRequest{}).Return(nil, clientErr)
		} else {
			oracleClient.EXPECT().Prices(gomock.Any(), &api.PricesRequest{}).Return(&api.PricesResponse{Prices: prices}, nil)
		}
		handler := voteextension.NewHandler(log.NewNopLogger(), oracleClient, oracleKeeper, time.Second).ExtendVoteHandler()
		resp, err := handler(newVoteExtensionContext(10, 2), req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		return resp
	}

	// One target priced, one undecodable, one the sidecar did not price.
	resp := extend(t, map[string][]byte{"ausd": validRate, "ajpy": {0x00, 0x01}}, nil)
	require.NotEmpty(t, resp.VoteExtension)
	require.Equal(t, map[string]float64{"priced": 1, "omitted": 1, "dropped": 1}, voteTargets(t, registry))

	// A sidecar failure is an empty extension: nothing priced, every target
	// omitted, and the gauge says so rather than holding the last report.
	resp = extend(t, nil, errors.New("sidecar down"))
	require.Empty(t, resp.VoteExtension)
	require.Equal(t, map[string]float64{"priced": 0, "omitted": 3, "dropped": 0}, voteTargets(t, registry))

	families, err := registry.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != "ark_oracle_vote_dropped_targets_total" {
			continue
		}
		require.Len(t, family.Metric, 1)
		require.Equal(t, float64(1), family.Metric[0].GetCounter().GetValue())
		require.Equal(t, "ajpy", labelValue(family.Metric[0].GetLabel(), "denom"))
		return
	}
	t.Fatal("dropped-targets counter not exported")
}

// voteTargets reads the per-status values of this node's vote coverage gauge.
func voteTargets(t *testing.T, gatherer prometheus.Gatherer) map[string]float64 {
	t.Helper()
	families, err := gatherer.Gather()
	require.NoError(t, err)
	got := map[string]float64{}
	for _, family := range families {
		if family.GetName() != "ark_oracle_vote_targets" {
			continue
		}
		for _, metric := range family.Metric {
			got[labelValue(metric.GetLabel(), "status")] = metric.GetGauge().GetValue()
		}
	}
	return got
}

func labelValue(labels []*dto.LabelPair, name string) string {
	for _, label := range labels {
		if label.GetName() == name {
			return label.GetValue()
		}
	}
	return ""
}

// A client disabled in app.toml is the operator's choice: the vote is an
// empty extension, the handler returns no error, and the log is one line
// at Info rather than a failure every block.
func TestExtendVoteHandlerDisabledClientAbstainsQuietly(t *testing.T) {
	ctrl := gomock.NewController(t)
	oracleClient := abcitestutil.NewMockPriceFeedClient(ctrl)
	oracleKeeper := abcitestutil.NewMockOracleKeeper(ctrl)
	req := &cmtabci.RequestExtendVote{Height: 10}
	oracleKeeper.EXPECT().GetFeeds(gomock.Any(), req.Height).Return(oracletypes.FeedSet{Version: oracletypes.InitialFeedVersion, Denoms: []string{"ausd"}}, nil)
	oracleClient.EXPECT().Prices(gomock.Any(), &api.PricesRequest{}).Return(nil, pricefeedclient.ErrDisabled)

	var logs bytes.Buffer
	logger := log.NewLogger(&logs, log.ColorOption(false))
	handler := voteextension.NewHandler(logger, oracleClient, oracleKeeper, time.Second).ExtendVoteHandler()

	resp, err := handler(newVoteExtensionContext(10, 2), req)
	require.NoError(t, err)
	require.Empty(t, resp.VoteExtension)
	require.Contains(t, logs.String(), "INF")
	require.Contains(t, logs.String(), "price-feed client disabled")
	require.NotContains(t, logs.String(), "ERR")
}
