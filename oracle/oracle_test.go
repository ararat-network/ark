package oracle

import (
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"noah/oracle/providers"
	"noah/oracle/providers/base"
	"noah/oracle/types"
)

func TestNewOracleRejectsInvalidInputs(t *testing.T) {
	validCfg := testOracleConfig([]string{"uusd"}, map[string]providers.Config{})

	testCases := []struct {
		name    string
		cfg     Config
		agg     PriceAggregator
		opts    []Option
		wantErr string
	}{
		{
			name: "invalid config",
			cfg: func() Config {
				cfg := validCfg
				cfg.UpdateInterval = 0
				return cfg
			}(),
			agg:     newRecordingPriceAggregator(),
			wantErr: "oracle update interval must be greater than 0",
		},
		{
			name:    "nil logger",
			cfg:     validCfg,
			agg:     newRecordingPriceAggregator(),
			opts:    []Option{WithLogger(nil)},
			wantErr: "logger is nil",
		},
		{
			name:    "nil aggregator",
			cfg:     validCfg,
			agg:     nil,
			wantErr: "aggregator is required",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewOracle(tc.cfg, tc.agg, tc.opts...)

			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestNewOracleBuildsConfiguredProviders(t *testing.T) {
	markets := testMarkets()
	providerCfg := testBinanceAPIProviderConfig(markets)
	cfg := testOracleConfig([]string{"uusd", "ukrw"}, map[string]providers.Config{
		providerCfg.Name: providerCfg,
	})

	oracle, err := NewOracle(cfg, newRecordingPriceAggregator())

	require.NoError(t, err)
	require.Contains(t, oracle.providers, providerCfg.Name)
	require.Equal(t, providerCfg.Name, oracle.providers[providerCfg.Name].Name())
}

func TestGetProvidersReturnsMapSnapshot(t *testing.T) {
	ctrl := gomock.NewController(t)
	provider := newMockProvider(t, ctrl, "unknown", testMarkets(), []string{"uusd", "ukrw"})
	oracle := &Oracle{
		providers: map[string]*base.Provider{
			"unknown": provider.provider,
		},
	}

	providers := oracle.GetProviders()
	delete(providers, "unknown")

	require.Contains(t, oracle.providers, "unknown")
}

func TestGetLastSyncTime(t *testing.T) {
	lastSync := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	oracle := &Oracle{
		lastPriceSync: lastSync,
	}

	require.Equal(t, lastSync, oracle.GetLastSyncTime())
}

func TestGetPricesDelegatesToAggregator(t *testing.T) {
	prices := types.Prices{
		"uusd": big.NewFloat(1.23),
	}
	aggregator := newRecordingPriceAggregator()
	aggregator.prices = prices
	oracle := &Oracle{
		aggregator: aggregator,
	}

	require.Equal(t, prices, oracle.GetPrices())
}
