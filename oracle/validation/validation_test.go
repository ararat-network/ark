package validation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"cosmossdk.io/log/v2"

	"noah/oracle/transport/types"
)

func TestRunReturnsLivenessForAvailablePrices(t *testing.T) {
	now := time.Now().UTC()
	client := &fakePriceClient{
		responses: []priceResponse{
			{resp: pricesResponse(now, "uusd", "ukrw")},
			{resp: pricesResponse(now, "uusd", "ukrw")},
		},
	}
	validator, err := NewValidator(log.NewNopLogger(), client, Config{
		ValidationPeriod:             2 * time.Millisecond,
		NumChecks:                    2,
		RequiredPriceLivenessPercent: 100,
		ExpectedDenoms:               []string{"uusd", "ukrw"},
		MaxResponseAge:               time.Hour,
	})
	require.NoError(t, err)

	results, err := validator.Run(context.Background())

	require.NoError(t, err)
	require.Equal(t, LivenessResults{
		"uusd": 100,
		"ukrw": 100,
	}, results)
	require.Equal(t, 2, client.calls)
}

func TestRunFailsWhenDenomBelowLivenessThreshold(t *testing.T) {
	now := time.Now().UTC()
	client := &fakePriceClient{
		responses: []priceResponse{
			{resp: pricesResponse(now, "uusd", "ukrw")},
			{resp: pricesResponse(now, "uusd")},
		},
	}
	validator, err := NewValidator(log.NewNopLogger(), client, Config{
		ValidationPeriod:             2 * time.Millisecond,
		NumChecks:                    2,
		RequiredPriceLivenessPercent: 75,
		ExpectedDenoms:               []string{"uusd", "ukrw"},
		MaxResponseAge:               time.Hour,
	})
	require.NoError(t, err)

	results, err := validator.Run(context.Background())

	require.ErrorContains(t, err, "invalid denoms below liveness threshold")
	require.Equal(t, LivenessResults{
		"uusd": 100,
		"ukrw": 50,
	}, results)
}

func TestRunCountsStaleResponsesAsMissing(t *testing.T) {
	client := &fakePriceClient{
		responses: []priceResponse{
			{resp: pricesResponse(time.Now().Add(-time.Hour), "uusd")},
			{resp: pricesResponse(time.Now().UTC(), "uusd")},
		},
	}
	validator, err := NewValidator(log.NewNopLogger(), client, Config{
		ValidationPeriod:             2 * time.Millisecond,
		NumChecks:                    2,
		RequiredPriceLivenessPercent: 75,
		ExpectedDenoms:               []string{"uusd"},
		MaxResponseAge:               time.Minute,
	})
	require.NoError(t, err)

	results, err := validator.Run(context.Background())

	require.ErrorContains(t, err, "invalid denoms below liveness threshold")
	require.Equal(t, LivenessResults{"uusd": 50}, results)
}

func TestRunExitsWhenContextCancelledDuringBurnIn(t *testing.T) {
	client := &fakePriceClient{}
	validator, err := NewValidator(log.NewNopLogger(), client, Config{
		BurnInPeriod:                 time.Hour,
		ValidationPeriod:             time.Millisecond,
		NumChecks:                    1,
		RequiredPriceLivenessPercent: 100,
		ExpectedDenoms:               []string{"uusd"},
		MaxResponseAge:               time.Minute,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	results, err := validator.Run(ctx)

	require.Nil(t, results)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, client.calls)
}

func TestConfigValidateRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
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
			name: "liveness percent",
			mutate: func(cfg *Config) {
				cfg.RequiredPriceLivenessPercent = 101
			},
			wantErr: "required price liveness percent must be between 0 and 100",
		},
		{
			name: "expected denoms",
			mutate: func(cfg *Config) {
				cfg.ExpectedDenoms = nil
			},
			wantErr: "expected denoms cannot be empty",
		},
		{
			name: "max response age",
			mutate: func(cfg *Config) {
				cfg.MaxResponseAge = 0
			},
			wantErr: "max response age must be greater than zero",
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
		ValidationPeriod:             time.Millisecond,
		NumChecks:                    1,
		RequiredPriceLivenessPercent: 100,
		ExpectedDenoms:               []string{"uusd"},
		MaxResponseAge:               time.Minute,
	}
}

func pricesResponse(timestamp time.Time, denoms ...string) *types.OraclePricesResponse {
	prices := make(map[string][]byte, len(denoms))
	for _, denom := range denoms {
		prices[denom] = []byte("1.23")
	}

	return &types.OraclePricesResponse{
		Prices:    prices,
		Timestamp: timestamp,
	}
}

type priceResponse struct {
	resp *types.OraclePricesResponse
	err  error
}

type fakePriceClient struct {
	responses []priceResponse
	calls     int
}

func (f *fakePriceClient) Prices(
	context.Context,
	*types.OraclePricesRequest,
	...grpc.CallOption,
) (*types.OraclePricesResponse, error) {
	if len(f.responses) == 0 {
		f.calls++
		return nil, errors.New("no response configured")
	}

	index := f.calls
	if index >= len(f.responses) {
		index = len(f.responses) - 1
	}
	f.calls++

	response := f.responses[index]
	return response.resp, response.err
}
