package cmd

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	oracleconfig "github.com/ararat-network/ark/pricefeed/config"
)

func TestInitCmdWritesDefaultRuntimeConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "oracle.json")
	cmd := NewRootCmd()
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"--" + flagConfig, cfgPath, "init"})

	err := cmd.Execute()

	require.NoError(t, err)
	require.Empty(t, out.String())

	cfg, err := oracleconfig.Load(cfgPath)
	require.NoError(t, err)
	require.NoError(t, cfg.Validate())
}

func TestInitCmdRefusesToOverwriteWithoutForce(t *testing.T) {
	cfgPath := writeOracleConfig(t, validOracleConfigJSON())
	cmd := NewRootCmd()
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{"--" + flagConfig, cfgPath, "init"})

	err := cmd.Execute()

	require.ErrorContains(t, err, "already exists")
}
