package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/client"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"

	"github.com/ararat-network/ark/app/mempool"
	"github.com/ararat-network/ark/pkg/telemetry"
	pricefeedclient "github.com/ararat-network/ark/pricefeed/client"
)

// runConfig runs `arkd config args...` against home and returns stdout and
// stderr. An empty home is the standalone case: paths must be given. The
// subtree stands in for the root here, so it silences cobra as the root does.
func runConfig(t *testing.T, home string, args ...string) (string, string, error) {
	t.Helper()
	cmd := newConfigCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	clientCtx := client.Context{}.WithHomeDir(home).WithOutput(&stdout)
	ctx := context.WithValue(context.Background(), client.ClientContextKey, &clientCtx)
	err := cmd.ExecuteContext(ctx)
	return stdout.String(), stderr.String(), err
}

// decodeAppTOML reads the file back the way start does.
func decodeAppTOML(t *testing.T, path string) (arkAppConfig, error) {
	t.Helper()
	return readAppConfig(viperFromTOML(t, readFile(t, path)))
}

func TestConfigCommandTree(t *testing.T) {
	var names []string
	for _, sub := range newConfigCmd().Commands() {
		names = append(names, sub.Name())
	}
	require.ElementsMatch(t, []string{"migrate", "diff", "get", "set", "validate", "view", "home"}, names)

	// confix's own commands read Ark's keys like any other.
	home := t.TempDir()
	writeHomeConfig(t, home, "app", renderAppTOML(t, appConfigTemplate))
	stdout, _, err := runConfig(t, home, "get", "app", "prometheus.address")
	require.NoError(t, err)
	require.Contains(t, stdout, "localhost:9464")
}

// An app.toml written before the Ark tables existed gains them as arkd init
// would have written them: banner, per-key comments, and template order.
func TestConfigMigrateAddsArkTables(t *testing.T) {
	home := t.TempDir()
	path := writeHomeConfig(t, home, "app", renderAppTOML(t, serverconfig.DefaultConfigTemplate))

	stdout, stderr, err := runConfig(t, home, "migrate", "--"+flagVerbose)
	require.NoError(t, err)
	require.Empty(t, stdout)
	require.Contains(t, stderr, "add [pricefeed]")
	require.Contains(t, stderr, "add [prometheus]")

	migrated := readFile(t, path)
	require.Contains(t, migrated, "###                               Price Feed                                ###")
	require.Contains(t, migrated, "# Address is the listen address of the scrape endpoint.")
	require.Less(t, strings.Index(migrated, "[mempool]"), strings.Index(migrated, "[pricefeed]"))
	require.Less(t, strings.Index(migrated, "[pricefeed]"), strings.Index(migrated, "[prometheus]"))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o644), info.Mode().Perm(), "the file keeps its mode")

	cfg, err := decodeAppTOML(t, path)
	require.NoError(t, err)
	require.Equal(t, pricefeedclient.NewDefaultConfig(), cfg.PriceFeed)
	require.Equal(t, telemetry.DefaultPrometheusConfig(), cfg.Prometheus)
	require.Equal(t, "0anoah", cfg.MinGasPrices)
	require.Equal(t, mempool.DefaultMaxTx, cfg.Mempool.MaxTxs)

	// A current file is left alone, byte for byte.
	stdout, _, err = runConfig(t, home, "migrate")
	require.NoError(t, err)
	require.Contains(t, stdout, "is current")
	require.Equal(t, migrated, readFile(t, path))
}

// A table the operator wrote by hand keeps its values and gains only the keys
// it lacks; the table is not written twice.
func TestConfigMigrateFillsPartialTable(t *testing.T) {
	home := t.TempDir()
	path := writeHomeConfig(t, home, "app",
		renderAppTOML(t, serverconfig.DefaultConfigTemplate)+"\n[pricefeed]\nenabled = true\n")

	_, stderr, err := runConfig(t, home, "migrate", "--"+flagVerbose)
	require.NoError(t, err)
	require.NotContains(t, stderr, "add [pricefeed]")
	require.Contains(t, stderr, `add pricefeed.sidecar_addresses = ["localhost:8080"]`)
	require.Contains(t, stderr, "add [prometheus]")

	cfg, err := decodeAppTOML(t, path)
	require.NoError(t, err)
	expected := pricefeedclient.NewDefaultConfig()
	expected.Enabled = true
	require.Equal(t, expected, cfg.PriceFeed)
	require.Equal(t, 1, strings.Count(readFile(t, path), "[pricefeed]"))
}

// Keys and tables this binary has no use for go, a table's keys with it.
func TestConfigMigrateRemovesUnknownKeys(t *testing.T) {
	home := t.TempDir()
	path := writeHomeConfig(t, home, "app",
		"legacy-key = 1\n"+renderAppTOML(t, appConfigTemplate)+"\n[legacy]\nfoo = \"bar\"\n\n[legacy.sub]\nbaz = 2\n")

	_, stderr, err := runConfig(t, home, "migrate", "--"+flagVerbose)
	require.NoError(t, err)
	require.Contains(t, stderr, "remove legacy-key")
	require.Contains(t, stderr, "remove [legacy]")
	require.Contains(t, stderr, "remove [legacy.sub]")
	require.NotContains(t, stderr, "legacy.foo")
	require.NotContains(t, stderr, "add ")

	require.NotContains(t, readFile(t, path), "legacy")
	_, err = decodeAppTOML(t, path)
	require.NoError(t, err)
}

// The result must pass the validation start applies, or the file is not
// touched; --skip-validate writes it anyway.
func TestConfigMigrateValidatesResult(t *testing.T) {
	home := t.TempDir()
	content := renderAppTOML(t, serverconfig.DefaultConfigTemplate) + "\n[pricefeed]\ninterval = \"2m\"\n"
	path := writeHomeConfig(t, home, "app", content)

	_, _, err := runConfig(t, home, "migrate")
	require.ErrorContains(t, err, "[pricefeed]")
	require.ErrorContains(t, err, "interval")
	require.Equal(t, content, readFile(t, path))

	_, _, err = runConfig(t, home, "migrate", "--"+flagSkipValidate)
	require.NoError(t, err)
	require.Contains(t, readFile(t, path), `interval = "2m"`)
}

func TestConfigMigratePathArgument(t *testing.T) {
	dir := t.TempDir()
	content := renderAppTOML(t, serverconfig.DefaultConfigTemplate)
	path := filepath.Join(dir, "node0-app.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	// Standalone: no home, so the path is required and --stdout leaves it be.
	_, _, err := runConfig(t, "", "migrate")
	require.ErrorContains(t, err, "no home directory")

	_, _, err = runConfig(t, "", "migrate", "v0.50")
	require.ErrorContains(t, err, "no version argument")

	stdout, _, err := runConfig(t, "", "migrate", "--"+flagStdout, path)
	require.NoError(t, err)
	require.Contains(t, stdout, "[prometheus]")
	require.Equal(t, content, readFile(t, path))

	_, _, err = runConfig(t, "", "migrate", path)
	require.NoError(t, err)
	require.Contains(t, readFile(t, path), "[prometheus]")
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// diff is migrate's plan read out, then the values migrate would keep.
func TestConfigDiff(t *testing.T) {
	home := t.TempDir()
	content := strings.Replace(
		renderAppTOML(t, serverconfig.DefaultConfigTemplate),
		`minimum-gas-prices = "0anoah"`, `minimum-gas-prices = "0.1anoah"`, 1,
	) + "\n[legacy]\nfoo = 1\n"
	path := writeHomeConfig(t, home, "app", content)

	stdout, _, err := runConfig(t, home, "diff")
	require.NoError(t, err)
	require.Equal(t, strings.Join([]string{
		"add [pricefeed]",
		"add [pricefeed.tls]",
		"add [prometheus]",
		"remove [legacy]",
		`keep minimum-gas-prices = "0.1anoah" (default "0anoah")`,
	}, "\n")+"\n", stdout)
	require.Equal(t, content, readFile(t, path), "diff never writes")

	writeHomeConfig(t, home, "app", renderAppTOML(t, appConfigTemplate))
	stdout, _, err = runConfig(t, home, "diff")
	require.NoError(t, err)
	require.Contains(t, stdout, "matches this binary's defaults")
}

// set on app.toml answers to the same validation as start, config.toml to
// CometBFT's, and other files keep confix's check.
func TestConfigSet(t *testing.T) {
	home := t.TempDir()
	path := writeHomeConfig(t, home, "app", renderAppTOML(t, appConfigTemplate))

	_, _, err := runConfig(t, home, "set", "app", "pricefeed.interval", "2m")
	require.ErrorContains(t, err, "[pricefeed]")
	require.Contains(t, readFile(t, path), `interval = "1.5s"`)

	_, _, err = runConfig(t, home, "set", "app", "pricefeed.interval", "2s")
	require.NoError(t, err)
	_, _, err = runConfig(t, home, "set", "app", "pricefeed.enabled", "true")
	require.NoError(t, err)
	cfg, err := decodeAppTOML(t, path)
	require.NoError(t, err)
	require.True(t, cfg.PriceFeed.Enabled)
	require.Equal(t, "2s", cfg.PriceFeed.Interval.String())

	_, _, err = runConfig(t, home, "set", "app", "pricefeed.missing", "1")
	require.ErrorContains(t, err, "not found")
	_, _, err = runConfig(t, home, "set", "app", "pricefeed", "1")
	require.ErrorContains(t, err, "is a table")

	clientPath := writeHomeConfig(t, home, "client",
		"chain-id = \"\"\nkeyring-backend = \"os\"\noutput = \"text\"\nnode = \"tcp://localhost:26657\"\nbroadcast-mode = \"sync\"\n")
	_, _, err = runConfig(t, home, "set", "client", "chain-id", "ark-1")
	require.NoError(t, err)
	require.Contains(t, readFile(t, clientPath), `chain-id = "ark-1"`)

	// The command start's mempool error tells the operator to run.
	cometPath := writeHomeConfig(t, home, "config", "log_format = \"plain\"\n\n[mempool]\ntype = \"nop\"\n")
	_, _, err = runConfig(t, home, "set", "config", "mempool.type", "flood")
	require.NoError(t, err)
	require.Contains(t, readFile(t, cometPath), `type = "flood"`)
	_, _, err = runConfig(t, home, "set", "config", "log_format", "yaml")
	require.ErrorContains(t, err, "log_format")
	require.Contains(t, readFile(t, cometPath), `log_format = "plain"`)
}

// set takes a .toml path in place of a name, as validate does, with or
// without a home, and the path's suffix picks the check: a node's app.toml
// gets Ark's.
func TestConfigSetPathArgument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node0-app.toml")
	require.NoError(t, os.WriteFile(path, []byte(renderAppTOML(t, appConfigTemplate)), 0o600))

	for _, home := range []string{"", t.TempDir()} {
		_, _, err := runConfig(t, home, "set", path, "pricefeed.interval", "2m")
		require.ErrorContains(t, err, "[pricefeed]")

		_, _, err = runConfig(t, home, "set", path, "pricefeed.enabled", "true")
		require.NoError(t, err)
	}
	cfg, err := decodeAppTOML(t, path)
	require.NoError(t, err)
	require.True(t, cfg.PriceFeed.Enabled)
}

// TestConfigValidate runs the checks start applies over a file alone: the
// home's app.toml by default, a named file, or a path.
func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		content string
		args    []string
		byPath  bool
		wantErr string
	}{
		{
			name:    "app.toml by default",
			file:    "app",
			content: renderAppTOML(t, appConfigTemplate),
		},
		{
			name:    "app.toml the SDK wrote is still valid",
			file:    "app",
			content: renderAppTOML(t, serverconfig.DefaultConfigTemplate),
		},
		{
			name:    "app.toml with an unusable minimum gas price",
			file:    "app",
			content: "minimum-gas-prices = \"" + maximumCoinAmount.String() + "anoah\"\n",
			wantErr: "too large to multiply",
		},
		{
			name:    "config.toml by name",
			file:    "config",
			content: "[mempool]\ntype = \"flood\"\n",
			args:    []string{"config"},
		},
		{
			name:    "config.toml CometBFT refuses",
			file:    "config",
			content: "[mempool]\ntype = \"nop\"\nsize = -1\n",
			args:    []string{"config"},
			wantErr: "is invalid",
		},
		{
			name:    "a node's app.toml by path gets Ark's check",
			file:    "node0-app",
			content: renderAppTOML(t, serverconfig.DefaultConfigTemplate) + "\n[pricefeed]\ninterval = \"2m\"\n",
			byPath:  true,
			wantErr: "[pricefeed]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			path := writeHomeConfig(t, home, tt.file, tt.content)
			args := tt.args
			if tt.byPath {
				args = []string{path}
			}

			stdout, _, err := runConfig(t, home, append([]string{"validate"}, args...)...)

			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, path+" is valid\n", stdout)
		})
	}

	t.Run("a path with no home", func(t *testing.T) {
		path := writeHomeConfig(t, t.TempDir(), "app", renderAppTOML(t, appConfigTemplate))

		stdout, _, err := runConfig(t, "", "validate", path)

		require.NoError(t, err)
		require.Equal(t, path+" is valid\n", stdout)
	})
}
