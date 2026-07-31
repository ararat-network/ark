package validation_test

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"

	"cosmossdk.io/log/v2"
	sdkmath "cosmossdk.io/math"

	"ark/oracle/types"
	. "ark/oracle/validation"
	validationtestutil "ark/oracle/validation/testutil"
	"ark/pkg/encoding"
	oracletypes "ark/x/oracle/types"
)

func TestNewValidatorDerivesComponentLogger(t *testing.T) {
	logs := new(bytes.Buffer)
	ctrl := gomock.NewController(t)
	client := validationtestutil.NewMockPriceClient(ctrl)
	feedClient := validationtestutil.NewMockFeedClient(ctrl)
	gomock.InOrder(
		expectFeeds(feedClient, []string{"ausd"}, nil),
		client.EXPECT().
			Prices(gomock.Any(), gomock.Any(), waitForReady()).
			Return(nil, context.DeadlineExceeded),
	)
	validator, err := NewValidator(log.NewLogger(logs, log.ColorOption(false)), client, feedClient, validConfig())
	require.NoError(t, err)

	_, _ = validator.Run(context.Background())
	output := logs.String()

	require.Contains(t, output, "component=validation")
	require.NotContains(t, output, "process=oracle_validation")
}

func TestNewValidatorDefaultsNilLogger(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := validationtestutil.NewMockPriceClient(ctrl)
	feedClient := validationtestutil.NewMockFeedClient(ctrl)

	validator, err := NewValidator(nil, client, feedClient, validConfig())

	require.NoError(t, err)
	require.NotNil(t, validator)
}

func TestNewValidatorRejectsNilDependencies(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := validationtestutil.NewMockPriceClient(ctrl)
	feedClient := validationtestutil.NewMockFeedClient(ctrl)

	_, err := NewValidator(log.NewNopLogger(), nil, feedClient, validConfig())
	require.EqualError(t, err, "price client cannot be nil")

	_, err = NewValidator(log.NewNopLogger(), client, nil, validConfig())
	require.EqualError(t, err, "feed client cannot be nil")
}

func TestRunReturnsLivenessForAvailablePrices(t *testing.T) {
	now := time.Now().UTC()
	ctrl := gomock.NewController(t)
	client := validationtestutil.NewMockPriceClient(ctrl)
	feedClient := validationtestutil.NewMockFeedClient(ctrl)
	gomock.InOrder(
		expectFeeds(feedClient, []string{"akrw", "ausd"}, nil),
		client.EXPECT().
			Prices(gomock.Any(), gomock.Any(), waitForReady()).
			Return(pricesResponse(t, now, "ausd", "akrw"), nil),
		client.EXPECT().
			Prices(gomock.Any(), gomock.Any(), waitForReady()).
			Return(pricesResponse(t, now, "ausd", "akrw"), nil),
	)
	cfg := validConfig()
	cfg.ValidationPeriod = 20 * time.Millisecond
	cfg.NumChecks = 2
	validator, err := NewValidator(log.NewNopLogger(), client, feedClient, cfg)
	require.NoError(t, err)

	results, err := validator.Run(context.Background())

	require.NoError(t, err)
	require.Equal(t, LivenessResults{
		"ausd": 100,
		"akrw": 100,
	}, results)
}

func TestRunUsesActiveFeedsInsteadOfResponseKeys(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := validationtestutil.NewMockPriceClient(ctrl)
	feedClient := validationtestutil.NewMockFeedClient(ctrl)
	gomock.InOrder(
		expectFeeds(feedClient, []string{"ausd", "akrw"}, nil),
		client.EXPECT().
			Prices(gomock.Any(), gomock.Any(), waitForReady()).
			Return(pricesResponse(t, time.Now().UTC(), "ausd", "aatom"), nil),
	)
	validator, err := NewValidator(log.NewNopLogger(), client, feedClient, validConfig())
	require.NoError(t, err)

	results, err := validator.Run(context.Background())

	require.ErrorContains(t, err, "invalid feeds below liveness threshold: [akrw]")
	require.Equal(t, LivenessResults{
		"ausd": 100,
		"akrw": 0,
	}, results)
	require.NotContains(t, results, "aatom")
}

func TestRunTracksRotatedActiveDenoms(t *testing.T) {
	now := time.Now().UTC()
	ctrl := gomock.NewController(t)
	client := validationtestutil.NewMockPriceClient(ctrl)
	feedClient := validationtestutil.NewMockFeedClient(ctrl)
	gomock.InOrder(
		expectFeeds(feedClient, []string{"ausd"}, nil),
		client.EXPECT().
			Prices(gomock.Any(), gomock.Any(), waitForReady()).
			Return(pricesResponse(t, now, "ausd"), nil),
		expectFeeds(feedClient, []string{"akrw"}, nil),
		client.EXPECT().
			Prices(gomock.Any(), gomock.Any(), waitForReady()).
			Return(pricesResponse(t, now, "akrw"), nil),
	)
	cfg := validConfig()
	cfg.ValidationPeriod = 20 * time.Millisecond
	cfg.NumChecks = 2
	cfg.FeedRefreshInterval = 15 * time.Millisecond
	validator, err := NewValidator(log.NewNopLogger(), client, feedClient, cfg)
	require.NoError(t, err)

	results, err := validator.Run(context.Background())

	require.NoError(t, err)
	require.Equal(t, LivenessResults{
		"ausd": 100,
		"akrw": 100,
	}, results)
}

func TestRunRequiresInitialActiveFeeds(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := validationtestutil.NewMockPriceClient(ctrl)
	feedClient := validationtestutil.NewMockFeedClient(ctrl)
	expectFeeds(feedClient, nil, errors.New("feeds unavailable"))
	validator, err := NewValidator(log.NewNopLogger(), client, feedClient, validConfig())
	require.NoError(t, err)

	results, err := validator.Run(context.Background())

	require.Nil(t, results)
	require.ErrorContains(t, err, "querying chain feeds: feeds unavailable")
}

func TestRunRejectsNilFeedResponse(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := validationtestutil.NewMockPriceClient(ctrl)
	feedClient := validationtestutil.NewMockFeedClient(ctrl)
	feedClient.EXPECT().
		Feeds(gomock.Any(), gomock.Any(), waitForReady()).
		Return(nil, nil)
	validator, err := NewValidator(log.NewNopLogger(), client, feedClient, validConfig())
	require.NoError(t, err)

	results, err := validator.Run(context.Background())

	require.Nil(t, results)
	require.ErrorContains(t, err, "chain feed response is nil")
}

func TestRunReportsNoActiveFeeds(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := validationtestutil.NewMockPriceClient(ctrl)
	feedClient := validationtestutil.NewMockFeedClient(ctrl)
	expectFeeds(feedClient, []string{}, nil)
	validator, err := NewValidator(log.NewNopLogger(), client, feedClient, validConfig())
	require.NoError(t, err)

	results, err := validator.Run(context.Background())

	require.Nil(t, results)
	require.ErrorIs(t, err, ErrNoActiveFeeds)
}

func TestRunRejectsInvalidInitialActiveFeeds(t *testing.T) {
	tests := []struct {
		name    string
		feeds   []string
		wantErr string
	}{
		{
			name:    "empty denom",
			feeds:   []string{" "},
			wantErr: `invalid active feed " "`,
		},
		{
			name:    "duplicate denom",
			feeds:   []string{"ausd", "ausd"},
			wantErr: `duplicate active feed "ausd"`,
		},
		{
			name:    "invalid SDK denom",
			feeds:   []string{"u?"},
			wantErr: `invalid active feed "u?"`,
		},
		{
			name:    "noncanonical sidecar denom",
			feeds:   []string{"aUSD"},
			wantErr: `invalid active feed "aUSD"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			client := validationtestutil.NewMockPriceClient(ctrl)
			feedClient := validationtestutil.NewMockFeedClient(ctrl)
			expectFeeds(feedClient, tt.feeds, nil)
			validator, err := NewValidator(log.NewNopLogger(), client, feedClient, validConfig())
			require.NoError(t, err)

			results, err := validator.Run(context.Background())

			require.Nil(t, results)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestRunRejectsTooManyActiveFeeds(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := validationtestutil.NewMockPriceClient(ctrl)
	feedClient := validationtestutil.NewMockFeedClient(ctrl)
	expectFeeds(feedClient, make([]string, oracletypes.MaxFeeds+1), nil)
	validator, err := NewValidator(log.NewNopLogger(), client, feedClient, validConfig())
	require.NoError(t, err)

	results, err := validator.Run(context.Background())

	require.Nil(t, results)
	require.ErrorContains(t, err, "active feed count")
	require.ErrorContains(t, err, "exceeds maximum")
}

func TestRunPreservesActiveDenomsWhenRefreshFails(t *testing.T) {
	now := time.Now().UTC()
	ctrl := gomock.NewController(t)
	client := validationtestutil.NewMockPriceClient(ctrl)
	feedClient := validationtestutil.NewMockFeedClient(ctrl)
	gomock.InOrder(
		expectFeeds(feedClient, []string{"ausd"}, nil),
		client.EXPECT().
			Prices(gomock.Any(), gomock.Any(), waitForReady()).
			Return(pricesResponse(t, now, "ausd"), nil),
		expectFeeds(feedClient, nil, errors.New("feeds unavailable")),
		client.EXPECT().
			Prices(gomock.Any(), gomock.Any(), waitForReady()).
			Return(pricesResponse(t, now, "ausd"), nil),
	)
	cfg := validConfig()
	cfg.ValidationPeriod = 20 * time.Millisecond
	cfg.NumChecks = 2
	cfg.FeedRefreshInterval = 15 * time.Millisecond
	validator, err := NewValidator(log.NewNopLogger(), client, feedClient, cfg)
	require.NoError(t, err)

	results, err := validator.Run(context.Background())

	require.NoError(t, err)
	require.Equal(t, LivenessResults{"ausd": 100}, results)
}

func TestRunFailsWhenDenomBelowLivenessThreshold(t *testing.T) {
	now := time.Now().UTC()
	ctrl := gomock.NewController(t)
	client := validationtestutil.NewMockPriceClient(ctrl)
	feedClient := validationtestutil.NewMockFeedClient(ctrl)
	gomock.InOrder(
		expectFeeds(feedClient, []string{"ausd", "akrw"}, nil),
		client.EXPECT().
			Prices(gomock.Any(), gomock.Any(), waitForReady()).
			Return(pricesResponse(t, now, "ausd", "akrw"), nil),
		client.EXPECT().
			Prices(gomock.Any(), gomock.Any(), waitForReady()).
			Return(pricesResponseWithValues(t, now, map[string]sdkmath.LegacyDec{
				"ausd": positivePrice(),
				"akrw": sdkmath.LegacyZeroDec(),
			}), nil),
	)
	cfg := validConfig()
	cfg.ValidationPeriod = 20 * time.Millisecond
	cfg.NumChecks = 2
	cfg.RequiredPriceLivenessPercent = 75
	validator, err := NewValidator(log.NewNopLogger(), client, feedClient, cfg)
	require.NoError(t, err)

	results, err := validator.Run(context.Background())

	require.ErrorContains(t, err, "invalid feeds below liveness threshold")
	require.Equal(t, LivenessResults{
		"ausd": 100,
		"akrw": 50,
	}, results)
}

func TestRunCountsInvalidPricesAsMissing(t *testing.T) {
	tests := []struct {
		name     string
		rawPrice []byte
	}{
		{
			name:     "zero",
			rawPrice: encodedPrice(t, sdkmath.LegacyZeroDec()),
		},
		{
			name:     "negative",
			rawPrice: encodedPrice(t, sdkmath.LegacyNewDec(-1)),
		},
		{
			name:     "malformed",
			rawPrice: []byte("not-a-decimal"),
		},
		{
			name:     "oversized",
			rawPrice: make([]byte, encoding.MaxEncodedLegacyDecBytes+1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			client := validationtestutil.NewMockPriceClient(ctrl)
			feedClient := validationtestutil.NewMockFeedClient(ctrl)
			gomock.InOrder(
				expectFeeds(feedClient, []string{"ausd"}, nil),
				client.EXPECT().
					Prices(gomock.Any(), gomock.Any(), waitForReady()).
					Return(&types.OraclePricesResponse{
						Prices:    map[string][]byte{"ausd": tt.rawPrice},
						Timestamp: time.Now().UTC(),
					}, nil),
			)
			validator, err := NewValidator(log.NewNopLogger(), client, feedClient, validConfig())
			require.NoError(t, err)

			results, err := validator.Run(context.Background())

			require.ErrorContains(t, err, "invalid feeds below liveness threshold")
			require.Equal(t, LivenessResults{"ausd": 0}, results)
		})
	}
}

func TestRunCountsStaleResponsesAsMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := validationtestutil.NewMockPriceClient(ctrl)
	feedClient := validationtestutil.NewMockFeedClient(ctrl)
	gomock.InOrder(
		expectFeeds(feedClient, []string{"ausd"}, nil),
		client.EXPECT().
			Prices(gomock.Any(), gomock.Any(), waitForReady()).
			Return(pricesResponse(t, time.Now().Add(-time.Hour), "ausd"), nil),
		client.EXPECT().
			Prices(gomock.Any(), gomock.Any(), waitForReady()).
			Return(pricesResponse(t, time.Now().UTC(), "ausd"), nil),
	)
	cfg := validConfig()
	cfg.ValidationPeriod = 20 * time.Millisecond
	cfg.NumChecks = 2
	cfg.RequiredPriceLivenessPercent = 75
	validator, err := NewValidator(log.NewNopLogger(), client, feedClient, cfg)
	require.NoError(t, err)

	results, err := validator.Run(context.Background())

	require.ErrorContains(t, err, "invalid feeds below liveness threshold")
	require.Equal(t, LivenessResults{"ausd": 50}, results)
}

func TestRunCountsFutureResponsesAsMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := validationtestutil.NewMockPriceClient(ctrl)
	feedClient := validationtestutil.NewMockFeedClient(ctrl)
	gomock.InOrder(
		expectFeeds(feedClient, []string{"ausd"}, nil),
		client.EXPECT().
			Prices(gomock.Any(), gomock.Any(), waitForReady()).
			Return(pricesResponse(t, time.Now().Add(time.Hour), "ausd"), nil),
	)
	validator, err := NewValidator(log.NewNopLogger(), client, feedClient, validConfig())
	require.NoError(t, err)

	results, err := validator.Run(context.Background())

	require.ErrorContains(t, err, "invalid feeds below liveness threshold")
	require.Equal(t, LivenessResults{"ausd": 0}, results)
}

func TestRunBoundsPriceRequests(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := validationtestutil.NewMockPriceClient(ctrl)
	feedClient := validationtestutil.NewMockFeedClient(ctrl)
	gomock.InOrder(
		expectFeeds(feedClient, []string{"ausd"}, nil),
		client.EXPECT().
			Prices(gomock.Any(), gomock.Any(), waitForReady()).
			DoAndReturn(func(
				ctx context.Context,
				_ *types.OraclePricesRequest,
				_ ...grpc.CallOption,
			) (*types.OraclePricesResponse, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			}),
	)
	cfg := validConfig()
	cfg.RequestTimeout = time.Millisecond
	validator, err := NewValidator(log.NewNopLogger(), client, feedClient, cfg)
	require.NoError(t, err)

	results, err := validator.Run(context.Background())

	require.ErrorContains(t, err, "invalid feeds below liveness threshold")
	require.Equal(t, LivenessResults{"ausd": 0}, results)
}

func TestRunBoundsInitialDenomRequest(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := validationtestutil.NewMockPriceClient(ctrl)
	feedClient := validationtestutil.NewMockFeedClient(ctrl)
	feedClient.EXPECT().
		Feeds(gomock.Any(), gomock.Any(), waitForReady()).
		DoAndReturn(func(
			ctx context.Context,
			_ *oracletypes.QueryFeedsRequest,
			_ ...grpc.CallOption,
		) (*oracletypes.QueryFeedsResponse, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		})
	cfg := validConfig()
	cfg.RequestTimeout = time.Millisecond
	validator, err := NewValidator(log.NewNopLogger(), client, feedClient, cfg)
	require.NoError(t, err)

	results, err := validator.Run(context.Background())

	require.Nil(t, results)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRunExitsWhenContextCancelledDuringBurnIn(t *testing.T) {
	ctrl := gomock.NewController(t)
	client := validationtestutil.NewMockPriceClient(ctrl)
	feedClient := validationtestutil.NewMockFeedClient(ctrl)
	cfg := validConfig()
	cfg.BurnInPeriod = time.Hour
	validator, err := NewValidator(log.NewNopLogger(), client, feedClient, cfg)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	results, err := validator.Run(ctx)

	require.Nil(t, results)
	require.ErrorIs(t, err, context.Canceled)
}

func TestConfigValidateRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name: "burn in period",
			mutate: func(cfg *Config) {
				cfg.BurnInPeriod = -1
			},
			wantErr: "burn in period cannot be negative",
		},
		{
			name: "validation period",
			mutate: func(cfg *Config) {
				cfg.ValidationPeriod = 0
			},
			wantErr: "validation period must be greater than zero",
		},
		{
			name: "num checks",
			mutate: func(cfg *Config) {
				cfg.NumChecks = 0
			},
			wantErr: "num checks must be greater than zero",
		},
		{
			name: "validation period shorter than checks",
			mutate: func(cfg *Config) {
				cfg.ValidationPeriod = time.Nanosecond
				cfg.NumChecks = 2
			},
			wantErr: "validation period must be long enough for num checks",
		},
		{
			name: "liveness percent above range",
			mutate: func(cfg *Config) {
				cfg.RequiredPriceLivenessPercent = 101
			},
			wantErr: "required price liveness percent must be between 0 and 100",
		},
		{
			name: "liveness percent NaN",
			mutate: func(cfg *Config) {
				cfg.RequiredPriceLivenessPercent = math.NaN()
			},
			wantErr: "required price liveness percent must be between 0 and 100",
		},
		{
			name: "max response age",
			mutate: func(cfg *Config) {
				cfg.MaxResponseAge = 0
			},
			wantErr: "max response age must be greater than zero",
		},
		{
			name: "max future skew",
			mutate: func(cfg *Config) {
				cfg.MaxFutureSkew = -1
			},
			wantErr: "max future skew cannot be negative",
		},
		{
			name: "request timeout",
			mutate: func(cfg *Config) {
				cfg.RequestTimeout = 0
			},
			wantErr: "request timeout must be greater than zero",
		},
		{
			name: "feed refresh interval",
			mutate: func(cfg *Config) {
				cfg.FeedRefreshInterval = 0
			},
			wantErr: "feed refresh interval must be greater than zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(&cfg)

			err := cfg.Validate()

			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func validConfig() Config {
	return Config{
		ValidationPeriod:             10 * time.Millisecond,
		NumChecks:                    1,
		RequiredPriceLivenessPercent: 100,
		MaxResponseAge:               time.Minute,
		MaxFutureSkew:                time.Second,
		RequestTimeout:               100 * time.Millisecond,
		FeedRefreshInterval:          time.Second,
	}
}

func pricesResponse(t *testing.T, timestamp time.Time, feeds ...string) *types.OraclePricesResponse {
	t.Helper()

	prices := make(map[string]sdkmath.LegacyDec, len(feeds))
	for _, denom := range feeds {
		prices[denom] = positivePrice()
	}
	return pricesResponseWithValues(t, timestamp, prices)
}

func pricesResponseWithValues(
	t *testing.T,
	timestamp time.Time,
	prices map[string]sdkmath.LegacyDec,
) *types.OraclePricesResponse {
	t.Helper()

	encodedPrices := make(map[string][]byte, len(prices))
	for denom, price := range prices {
		encodedPrices[denom] = encodedPrice(t, price)
	}
	return &types.OraclePricesResponse{
		Prices:    encodedPrices,
		Timestamp: timestamp,
	}
}

func encodedPrice(t *testing.T, price sdkmath.LegacyDec) []byte {
	t.Helper()

	rawPrice, err := encoding.EncodeLegacyDec(price)
	require.NoError(t, err)
	return rawPrice
}

func positivePrice() sdkmath.LegacyDec {
	return sdkmath.LegacyMustNewDecFromStr("1.23")
}

type waitForReadyMatcher struct{}

func (waitForReadyMatcher) Matches(value any) bool {
	option, ok := value.(grpc.FailFastCallOption)
	return ok && !option.FailFast
}

func (waitForReadyMatcher) String() string {
	return "grpc.WaitForReady(true)"
}

func waitForReady() gomock.Matcher {
	return waitForReadyMatcher{}
}

func expectFeeds(
	client *validationtestutil.MockFeedClient,
	feeds []string,
	err error,
) *gomock.Call {
	return client.EXPECT().
		Feeds(gomock.Any(), gomock.Any(), waitForReady()).
		Return(&oracletypes.QueryFeedsResponse{
			Feeds: oracletypes.Feeds{
				Denoms:  append([]string(nil), feeds...),
				Version: oracletypes.InitialFeedVersion,
			},
		}, err)
}
