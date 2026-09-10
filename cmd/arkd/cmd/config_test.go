package cmd

import (
	stdmath "math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/ararat-network/ark/app/mempool"
	pricefeedclient "github.com/ararat-network/ark/pricefeed/client"
)

func TestValidateMinGasPrices(t *testing.T) {
	maximumSafeIntegerPrice := math.LegacyNewDecFromInt(
		maximumCoinAmount.TruncateInt().Quo(math.NewIntFromUint64(stdmath.MaxUint64)),
	)

	tests := []struct {
		name        string
		value       string
		errorSubstr string
	}{
		{
			name:  "zero default",
			value: "0anoah",
		},
		{
			name:  "ordinary prices",
			value: "0.01anoah,0.1ausd",
		},
		{
			name:  "maximum safe integer price",
			value: maximumSafeIntegerPrice.String() + "anoah",
		},
		{
			name:        "malformed price",
			value:       "not-a-price",
			errorSubstr: "invalid minimum gas prices",
		},
		{
			name:        "price overflows maximum gas",
			value:       maximumCoinAmount.String() + "anoah",
			errorSubstr: "too large to multiply by the maximum gas limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMinGasPrices(tt.value)
			if tt.errorSubstr == "" {
				require.NoError(t, err)
				return
			}

			require.ErrorIs(t, err, errortypes.ErrAppConfig)
			require.ErrorContains(t, err, tt.errorSubstr)
		})
	}
}

// requireDefaultAppConfig is equality with defaultAppConfig, allowing for
// GetConfig parsing the template's "{}" block-range map into an empty map
// where DefaultConfig leaves nil: the SDK's own quirk, and the same value.
func requireDefaultAppConfig(t *testing.T, cfg arkAppConfig) {
	t.Helper()
	require.Empty(t, cfg.GRPC.HistoricalGRPCAddressBlockRange)
	cfg.GRPC.HistoricalGRPCAddressBlockRange = nil
	require.Equal(t, defaultAppConfig(), cfg)
}

// Generated app.toml must decode to its source defaults through both the start decoder and app key
// readers, including identical TOML types.
func TestAppConfigTemplateRoundTrip(t *testing.T) {
	v := viperFromTOML(t, renderAppTOML(t, appConfigTemplate))

	// Booleans render bare and durations quoted: what anything decoding
	// app.toml by declared type sees.
	require.IsType(t, false, v.Get("pricefeed.enabled"))
	require.IsType(t, false, v.Get("prometheus.enabled"))
	require.IsType(t, "", v.Get("pricefeed.client_timeout"))
	require.IsType(t, "", v.Get("pricefeed.price_ttl"))
	require.IsType(t, "", v.Get("pricefeed.interval"))
	require.IsType(t, "", v.Get("pricefeed.tls.ca_file"))

	decoded, err := readAppConfig(v)
	require.NoError(t, err)
	requireDefaultAppConfig(t, decoded)

	for _, enabled := range []bool{false, true} {
		v.Set("pricefeed.enabled", enabled)
		decoded, err := readAppConfig(v)
		require.NoError(t, err)
		require.Equal(t, enabled, decoded.PriceFeed.Enabled)

		// The app reads [pricefeed] on its own; it must see what start saw.
		priceFeed, err := pricefeedclient.ReadConfigFromAppOpts(v)
		require.NoError(t, err)
		require.Equal(t, decoded.PriceFeed, priceFeed)
	}

	// An environment override of the address list is one string that both
	// decoders split; they must split it the same way.
	v.SetEnvPrefix("arkd")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()
	t.Setenv("ARKD_PRICEFEED_TLS_MODE", "plaintext")
	t.Setenv("ARKD_PRICEFEED_SIDECAR_ADDRESSES", "10.0.0.3:8080,10.0.0.2:8080")
	decoded, err = readAppConfig(v)
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.3:8080", "10.0.0.2:8080"}, decoded.PriceFeed.SidecarAddresses)
	priceFeed, err := pricefeedclient.ReadConfigFromAppOpts(v)
	require.NoError(t, err)
	require.Equal(t, decoded.PriceFeed, priceFeed)

	// The TLS table nests one level down; an override of a key inside it
	// reaches both decoders.
	t.Setenv("ARKD_PRICEFEED_TLS_MODE", "tls")
	t.Setenv("ARKD_PRICEFEED_TLS_CA_FILE", "ca.pem")
	decoded, err = readAppConfig(v)
	require.NoError(t, err)
	require.Equal(t, "ca.pem", decoded.PriceFeed.TLS.CAFile)
	priceFeed, err = pricefeedclient.ReadConfigFromAppOpts(v)
	require.NoError(t, err)
	require.Equal(t, decoded.PriceFeed, priceFeed)
}

// The template swaps the SDK's advice that -1 disables the mempool for
// Ark's refusal by exact text; a reworded SDK comment would silently bring
// the advice back.
func TestAppConfigTemplateRefusesNoopMempoolAdvice(t *testing.T) {
	require.NotContains(t, appConfigTemplate, "no-op mempool")
	require.Contains(t, appConfigTemplate, "Negative max-txs values are rejected")
}

func TestReadAppConfig(t *testing.T) {
	defaults := defaultAppConfig()

	tests := []struct {
		name        string
		doc         string // TOML; empty renders the defaults
		env         map[string]string
		mutate      func(*viper.Viper)
		check       func(*testing.T, arkAppConfig)
		errorSubstr string
	}{
		{
			name: "absent ark sections default",
			doc:  "minimum-gas-prices = \"0anoah\"\n",
			check: func(t *testing.T, cfg arkAppConfig) {
				t.Helper()
				require.Equal(t, defaults.PriceFeed, cfg.PriceFeed)
				require.Equal(t, defaults.Prometheus, cfg.Prometheus)
			},
		},
		{
			name: "env overrides nested keys",
			env: map[string]string{
				"ARKD_PRICEFEED_ENABLED":  "true",
				"ARKD_PROMETHEUS_ADDRESS": "0.0.0.0:9465",
			},
			check: func(t *testing.T, cfg arkAppConfig) {
				t.Helper()
				require.True(t, cfg.PriceFeed.Enabled)
				require.Equal(t, "0.0.0.0:9465", cfg.Prometheus.Address)
			},
		},
		{
			name: "pricefeed address list keeps its order",
			doc: "minimum-gas-prices = \"0anoah\"\n[pricefeed]\n" +
				"sidecar_addresses = [\"10.0.0.3:8080\", \"10.0.0.2:8080\"]\n[pricefeed.tls]\nmode = \"plaintext\"\n",
			check: func(t *testing.T, cfg arkAppConfig) {
				t.Helper()
				require.Equal(t, []string{"10.0.0.3:8080", "10.0.0.2:8080"}, cfg.PriceFeed.SidecarAddresses)
			},
		},
		{
			name: "env overrides the address list",
			env:  map[string]string{"ARKD_PRICEFEED_TLS_MODE": "plaintext", "ARKD_PRICEFEED_SIDECAR_ADDRESSES": "10.0.0.3:8080,10.0.0.2:8080"},
			check: func(t *testing.T, cfg arkAppConfig) {
				t.Helper()
				require.Equal(t, []string{"10.0.0.3:8080", "10.0.0.2:8080"}, cfg.PriceFeed.SidecarAddresses)
			},
		},
		{
			name:        "remote pricefeed requires explicit transport",
			mutate:      func(v *viper.Viper) { v.Set("pricefeed.sidecar_addresses", []string{"10.0.0.2:8080"}) },
			errorSubstr: "requires mode tls or explicit mode plaintext",
		},
		{
			name: "pricefeed tls table",
			doc: "minimum-gas-prices = \"0anoah\"\n[pricefeed]\n[pricefeed.tls]\n" +
				"mode = \"tls\"\nca_file = \"ca.pem\"\nserver_name = \"sidecar\"\n",
			check: func(t *testing.T, cfg arkAppConfig) {
				t.Helper()
				require.Equal(t, "ca.pem", cfg.PriceFeed.TLS.CAFile)
				require.Equal(t, "sidecar", cfg.PriceFeed.TLS.ServerName)
			},
		},
		{
			name: "pricefeed tls cert without key",
			mutate: func(v *viper.Viper) {
				v.Set("pricefeed.tls.ca_file", "ca.pem")
				v.Set("pricefeed.tls.cert_file", "node.pem")
			},
			errorSubstr: "[pricefeed]: tls cert file and key file must be set together",
		},
		{
			name: "pricefeed duplicate address",
			mutate: func(v *viper.Viper) {
				v.Set("pricefeed.sidecar_addresses", []string{"10.0.0.2:8080", "10.0.0.2:8080"})
			},
			errorSubstr: "[pricefeed]: sidecar_addresses[1] repeats",
		},
		{
			name:        "malformed pricefeed duration",
			mutate:      func(v *viper.Viper) { v.Set("pricefeed.interval", "soon") },
			errorSubstr: "pricefeed.interval",
		},
		{
			name:        "pricefeed interval not below price_ttl",
			mutate:      func(v *viper.Viper) { v.Set("pricefeed.interval", "30s") },
			errorSubstr: "[pricefeed]: interval must be strictly less",
		},
		{
			name:        "prometheus address without port",
			mutate:      func(v *viper.Viper) { v.Set("prometheus.address", "localhost") },
			errorSubstr: "[prometheus]: address must be host:port",
		},
		{
			name: "minimum gas price overflows the gas limit",
			mutate: func(v *viper.Viper) {
				v.Set(server.FlagMinGasPrices, maximumCoinAmount.String()+"anoah")
			},
			errorSubstr: "too large to multiply by the maximum gas limit",
		},
		{
			name:        "empty minimum gas prices",
			mutate:      func(v *viper.Viper) { v.Set(server.FlagMinGasPrices, "") },
			errorSubstr: "set min gas price",
		},
		{
			name: "snapshots with prune everything",
			mutate: func(v *viper.Viper) {
				v.Set("pruning", "everything")
				v.Set("state-sync.snapshot-interval", 100)
			},
			errorSubstr: "cannot enable state sync snapshots",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := tt.doc
			if doc == "" {
				doc = renderAppTOML(t, appConfigTemplate)
			}
			v := viperFromTOML(t, doc)
			// The node's viper, as InterceptConfigsAndCreateContext sets it up.
			v.SetEnvPrefix("arkd")
			v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
			v.AutomaticEnv()
			for key, value := range tt.env {
				t.Setenv(key, value)
			}
			if tt.mutate != nil {
				tt.mutate(v)
			}

			cfg, err := readAppConfig(v)
			if tt.errorSubstr != "" {
				require.ErrorIs(t, err, errortypes.ErrAppConfig)
				require.ErrorContains(t, err, tt.errorSubstr)
				return
			}
			require.NoError(t, err)
			tt.check(t, cfg)
		})
	}
}

func TestAbsentSections(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want []string
	}{
		{
			name: "generated file",
			doc:  renderAppTOML(t, appConfigTemplate),
		},
		{
			name: "pricefeed missing",
			doc:  "[prometheus]\nenabled = false\n",
			want: []string{"pricefeed"},
		},
		{
			name: "both missing",
			doc:  "minimum-gas-prices = \"0anoah\"\n",
			want: []string{"pricefeed", "prometheus"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, absentSections(viperFromTOML(t, tt.doc)))
		})
	}
}

func TestDeadPrometheusRetention(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		sink      string
		retention int64
		want      bool
	}{
		{name: "retention behind the replaced fan-out", enabled: true, sink: otelSink, retention: 60, want: true},
		{name: "no retention", enabled: true, sink: otelSink},
		{name: "another sink keeps its fan-out", enabled: true, sink: "mem", retention: 60},
		{name: "legacy bridge off", sink: otelSink, retention: 60},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaultAppConfig()
			cfg.Telemetry.Enabled = tt.enabled                   //nolint:staticcheck // the legacy bridge is what the sink replaces
			cfg.Telemetry.MetricsSink = tt.sink                  //nolint:staticcheck // same
			cfg.Telemetry.PrometheusRetentionTime = tt.retention //nolint:staticcheck // same
			require.Equal(t, tt.want, cfg.deadPrometheusRetention())
		})
	}
}

func TestUnroutedGoMetrics(t *testing.T) {
	tests := []struct {
		name       string
		enabled    bool
		sink       string
		prometheus bool
		want       bool
	}{
		{
			name:    "otel sink without the endpoint",
			enabled: true,
			sink:    otelSink,
			want:    true,
		},
		{
			name:       "otel sink with the endpoint",
			enabled:    true,
			sink:       otelSink,
			prometheus: true,
		},
		{
			name: "legacy bridge off",
			sink: otelSink,
		},
		{
			name:    "another sink",
			enabled: true,
			sink:    "mem",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaultAppConfig()
			cfg.Telemetry.Enabled = tt.enabled  //nolint:staticcheck // the legacy bridge is what the sink replaces
			cfg.Telemetry.MetricsSink = tt.sink //nolint:staticcheck // same
			cfg.Prometheus.Enabled = tt.prometheus
			require.Equal(t, tt.want, cfg.unroutedGoMetrics())
		})
	}
}

// The SDK writes app.toml from the template only when the file is missing
// and never rewrites it. Both Ark tables must land in a fresh file, and a
// file from before they existed must still read, on their defaults, with
// the absence reported.
func TestInterceptConfigsWritesArkSections(t *testing.T) {
	home := t.TempDir()
	cmd := &cobra.Command{Use: "start"}
	cmd.Flags().String(flags.FlagHome, home, "")
	tmpl, cfg := initAppConfig()

	svrCtx, err := server.InterceptConfigsAndCreateContext(cmd, tmpl, cfg, initCometBFTConfig())
	require.NoError(t, err)

	appToml := filepath.Join(home, "config", "app.toml")
	written, err := os.ReadFile(appToml)
	require.NoError(t, err)
	require.Contains(t, string(written), "[pricefeed]\n")
	require.Contains(t, string(written), "[prometheus]\n")
	require.Empty(t, absentSections(svrCtx.Viper))

	decoded, err := readAppConfig(svrCtx.Viper)
	require.NoError(t, err)
	requireDefaultAppConfig(t, decoded)

	require.NoError(t, os.WriteFile(appToml, []byte("minimum-gas-prices = \"0anoah\"\n"), 0o600))
	svrCtx, err = server.InterceptConfigsAndCreateContext(cmd, tmpl, cfg, initCometBFTConfig())
	require.NoError(t, err)
	require.Equal(t, arkSections, absentSections(svrCtx.Viper))

	decoded, err = readAppConfig(svrCtx.Viper)
	require.NoError(t, err)
	require.Equal(t, defaultAppConfig().PriceFeed, decoded.PriceFeed)
	require.Equal(t, defaultAppConfig().Prometheus, decoded.Prometheus)
}

func TestMempoolConfigRequiresProposalChecking(t *testing.T) {
	for _, tc := range []struct {
		name, doc string
		want      int
		invalid   bool
	}{
		{"missing defaults safely", "minimum-gas-prices = \"0anoah\"\n", mempool.DefaultMaxTx, false},
		{"zero retains real pool", "minimum-gas-prices = \"0anoah\"\n[mempool]\nmax-txs = 0\n", 0, false},
		{"negative rejected", "minimum-gas-prices = \"0anoah\"\n[mempool]\nmax-txs = -1\n", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := readAppConfig(viperFromTOML(t, tc.doc))
			if tc.invalid {
				require.ErrorContains(t, err, "max-txs must be between")
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, cfg.Mempool.MaxTxs)
			}
		})
	}
}
