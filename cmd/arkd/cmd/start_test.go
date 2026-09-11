package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	cmtcfg "github.com/cometbft/cometbft/config"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/ararat-network/ark/app/mempool"
)

func TestPrepareStart(t *testing.T) {
	t.Run("rejects invalid app config", func(t *testing.T) {
		svrCtx := server.NewDefaultContext()
		svrCtx.Viper.Set(server.FlagMinGasPrices, maximumCoinAmount.String()+"anoah")

		_, _, err := prepareStart(svrCtx)
		require.ErrorIs(t, err, errortypes.ErrAppConfig)
		require.ErrorContains(t, err, "too large to multiply by the maximum gas limit")
	})

	t.Run("defaults without an endpoint", func(t *testing.T) {
		svrCtx := server.NewDefaultContext()
		svrCtx.Viper.Set(server.FlagMinGasPrices, "0anoah")

		cfg, endpoint, err := prepareStart(svrCtx)
		require.NoError(t, err)
		require.Nil(t, endpoint)
		require.Equal(t, defaultAppConfig().PriceFeed, cfg.PriceFeed)
		require.Equal(t, defaultAppConfig().Prometheus, cfg.Prometheus)
	})

	t.Run("accepts a list larger than the pool", func(t *testing.T) {
		svrCtx := server.NewDefaultContext()
		svrCtx.Config.Mempool.Size = mempool.DefaultMaxTx + 1
		svrCtx.Viper.Set(server.FlagMinGasPrices, "0anoah")

		_, _, err := prepareStart(svrCtx)
		require.NoError(t, err)
	})

	t.Run("rejects a pull reader in otel.yaml", func(t *testing.T) {
		svrCtx := server.NewDefaultContext()
		svrCtx.Config.SetRoot(t.TempDir())
		svrCtx.Viper.Set(server.FlagMinGasPrices, "0anoah")
		writeOtelFile(t, svrCtx.Config.RootDir, otelPullReader)

		_, _, err := prepareStart(svrCtx)
		require.ErrorContains(t, err, "pull metric reader")
	})

	t.Run("the SDK's config-file variable", func(t *testing.T) {
		tests := []struct {
			name       string
			prometheus bool
			doc        string
			refused    bool
		}{
			{name: "refused with the endpoint on", prometheus: true, refused: true},
			{name: "accepted with the endpoint off"},
			// The SDK builds OpenTelemetry from the variable's file and never
			// reads config/otel.yaml, so nothing in it can refuse the start.
			{name: "config/otel.yaml is not judged", doc: otelPullReader},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				svrCtx := server.NewDefaultContext()
				svrCtx.Config.SetRoot(t.TempDir())
				svrCtx.Viper.Set(server.FlagMinGasPrices, "0anoah")
				svrCtx.Viper.Set("prometheus.enabled", tt.prometheus)
				svrCtx.Viper.Set("chain-id", "ark-test")
				if tt.doc != "" {
					writeOtelFile(t, svrCtx.Config.RootDir, tt.doc)
				}
				// Read by the SDK at package load, long before this test; only
				// prepareStart sees it now.
				t.Setenv(otelConfigFileEnv, filepath.Join(t.TempDir(), "otel.yaml"))

				_, _, err := prepareStart(svrCtx)
				if tt.refused {
					require.ErrorContains(t, err, otelConfigFileEnv)
					return
				}
				require.NoError(t, err)
			})
		}
	})

	t.Run("endpoint hosts the baseapp instrument unless otel.yaml names it", func(t *testing.T) {
		tests := []struct {
			name  string
			doc   string
			hosts bool
		}{
			{name: "empty otel.yaml", hosts: true},
			{name: "otel.yaml names it", doc: otelBaseappInstrument},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				svrCtx := server.NewDefaultContext()
				svrCtx.Config.SetRoot(t.TempDir())
				svrCtx.Viper.Set(server.FlagMinGasPrices, "0anoah")
				svrCtx.Viper.Set("prometheus.enabled", true)
				svrCtx.Viper.Set("chain-id", "ark-test")
				writeOtelFile(t, svrCtx.Config.RootDir, tt.doc)

				_, endpoint, err := prepareStart(svrCtx)
				require.NoError(t, err)
				require.NotNil(t, endpoint)
				require.Equal(t, tt.hosts, endpoint.hostBaseapp)
			})
		}
	})
}

func TestStartRequiresMirroredFloodMempool(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*server.Context)
		want   string
	}{
		{"app mode", func(c *server.Context) { c.Config.Mempool.Type = "app" }, "mempool.type flood"},
		{"nop mode", func(c *server.Context) { c.Config.Mempool.Type = "nop" }, "mempool.type flood"},
		{"recheck disabled", func(c *server.Context) { c.Config.Mempool.Recheck = false }, "recheck = true"},
		{"negative count", func(c *server.Context) { c.Viper.Set(mempool.MaxTxsKey, -1) }, "max-txs must be between"},
		{"excess count", func(c *server.Context) { c.Viper.Set(mempool.MaxTxsKey, mempool.MaxTxLimit+1) }, "max-txs must be between"},
		{"zero pool bytes", func(c *server.Context) { c.Config.Mempool.MaxTxsBytes = 0 }, "max_txs_bytes must be positive"},
		{"negative pool bytes", func(c *server.Context) { c.Config.Mempool.MaxTxsBytes = -1 }, "max_txs_bytes must be positive"},
		{"zero transaction bytes", func(c *server.Context) { c.Config.Mempool.MaxTxBytes = 0 }, "max_tx_bytes must be positive"},
		{"negative transaction bytes", func(c *server.Context) { c.Config.Mempool.MaxTxBytes = -1 }, "max_tx_bytes must be positive"},
		{"list below the default pool", func(c *server.Context) {
			// Checked after app.toml validation, so the fixture must pass that.
			c.Viper.Set(server.FlagMinGasPrices, "0anoah")
			c.Config.Mempool.Size = mempool.DefaultMaxTx - 1
		}, "size = 4999 is below app.toml [mempool] max-txs = 5000"},
		{"list below a configured pool", func(c *server.Context) {
			c.Viper.Set(server.FlagMinGasPrices, "0anoah")
			c.Viper.Set(mempool.MaxTxsKey, 40)
			c.Config.Mempool.Size = 39
		}, "size = 39 is below app.toml [mempool] max-txs = 40"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := server.NewDefaultContext()
			tc.mutate(c)
			_, _, err := prepareStart(c)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestMempoolByteConfigAlignment(t *testing.T) {
	for _, tc := range []struct {
		name      string
		existing  bool
		env       bool
		poolBytes int64
		txBytes   int
	}{
		{"new home", false, false, 64 << 20, 1 << 20},
		{"existing upstream budget", true, false, 1 << 30, 2 << 20},
		{"environment override", true, true, 128 << 20, 3 << 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if tc.existing {
				cfg := initCometBFTConfig()
				cfg.Mempool.MaxTxsBytes = 1 << 30
				cfg.Mempool.MaxTxBytes = 2 << 20
				require.NoError(t, os.MkdirAll(filepath.Join(home, "config"), 0o755))
				cmtcfg.WriteConfigFile(filepath.Join(home, "config", "config.toml"), cfg)
			}
			// A misplaced app.toml key must never override the already resolved
			// CometBFT value supplied to the application.
			writeHomeConfig(t, home, "app", "minimum-gas-prices = \"0anoah\"\n[mempool]\nmax-txs = 5000\nmax_tx_bytes = 7\nmax_txs_bytes = 8\n")
			cmd := &cobra.Command{Use: "start"}
			cmd.Flags().String(flags.FlagHome, home, "")
			if tc.env {
				executable, err := os.Executable()
				require.NoError(t, err)
				prefix := strings.NewReplacer(".", "_", "-", "_").Replace(strings.ToUpper(filepath.Base(executable)))
				t.Setenv(prefix+"_MEMPOOL_MAX_TX_BYTES", "3145728")
				t.Setenv(prefix+"_MEMPOOL_MAX_TXS_BYTES", "134217728")
			}
			tmpl, cfg := initAppConfig()
			svrCtx, err := server.InterceptConfigsAndCreateContext(cmd, tmpl, cfg, initCometBFTConfig())
			require.NoError(t, err)
			require.Equal(t, tc.txBytes, svrCtx.Config.Mempool.MaxTxBytes)
			require.Equal(t, tc.poolBytes, svrCtx.Config.Mempool.MaxTxsBytes)
			appCfg, endpoint, err := prepareStart(svrCtx)
			require.NoError(t, err)
			require.Nil(t, endpoint)
			require.Equal(t, mempool.Config{MaxTxs: 5000, MaxTxsBytes: tc.poolBytes, MaxTxBytes: tc.txBytes}, appCfg.Mempool)
			require.Equal(t, tc.txBytes, svrCtx.Viper.GetInt(mempool.MaxTransactionBytesKey))
			require.Equal(t, tc.poolBytes, svrCtx.Viper.GetInt64(mempool.MaxPoolBytesKey))
			if !tc.existing {
				written := readFile(t, filepath.Join(home, "config", "config.toml"))
				require.Contains(t, written, "max_txs_bytes = 67108864")
				require.Contains(t, written, "max_tx_bytes = 1048576")
			}
		})
	}
}

// The default app.toml disables the price-feed client; a validator running
// on it abstains from every oracle vote, which start says once rather than
// the vote handler saying it every block.
func TestPrepareStartWarnsWhenPriceFeedIsDisabled(t *testing.T) {
	var logs bytes.Buffer
	svrCtx := server.NewContext(viper.New(), cmtcfg.DefaultConfig(), log.NewLogger(&logs, log.ColorOption(false)))
	svrCtx.Viper.Set(server.FlagMinGasPrices, "0anoah")

	cfg, _, err := prepareStart(svrCtx)
	require.NoError(t, err)
	require.False(t, cfg.PriceFeed.Enabled)
	require.Contains(t, logs.String(), "[pricefeed] is disabled")

	logs.Reset()
	svrCtx.Viper.Set("pricefeed.enabled", true)
	svrCtx.Viper.Set("pricefeed.sidecar_addresses", []string{"localhost:1"})
	cfg, _, err = prepareStart(svrCtx)
	require.NoError(t, err)
	require.True(t, cfg.PriceFeed.Enabled)
	require.NotContains(t, logs.String(), "[pricefeed] is disabled")
}
