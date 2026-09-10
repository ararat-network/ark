package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"text/template"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// renderAppTOML renders tmpl with the defaults arkd writes, so a test starts
// from the SDK's file alone or from the whole of Ark's.
func renderAppTOML(t *testing.T, tmpl string) string {
	t.Helper()
	parsed, err := template.New("app.toml").Parse(tmpl)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, parsed.Execute(&buf, defaultAppConfig()))
	return buf.String()
}

// viperFromTOML is the node's viper over doc alone: no flags and no
// environment.
func viperFromTOML(t *testing.T, doc string) *viper.Viper {
	t.Helper()
	v, err := tomlViper([]byte(doc))
	require.NoError(t, err)
	return v
}

// writeHomeConfig writes content as <home>/config/<name>.toml and returns the
// path.
func writeHomeConfig(t *testing.T, home, name, content string) string {
	t.Helper()
	dir := filepath.Join(home, "config")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, name+".toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644)) //nolint:gosec // the mode the SDK writes app.toml with; migrate must keep it
	return path
}

// writeOtelFile puts doc where the start command reads otel.yaml.
func writeOtelFile(t *testing.T, root, doc string) {
	t.Helper()
	dir := filepath.Join(root, "config")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "otel.yaml"), []byte(doc), 0o600))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}
