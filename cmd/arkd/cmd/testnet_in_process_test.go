package cmd

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
)

// The in-process network refuses a commit timeout its first block cannot
// beat, and renders the SDK's app.toml again after the root pre-run has
// installed Ark's template.
func TestInProcessNetworkConfig(t *testing.T) {
	t.Cleanup(func() { serverconfig.SetConfigTemplate(appConfigTemplate) })

	_, err := inProcessNetworkConfig(testnetStartOptions{testnetOptions: testnetOptions{numValidators: 1, commitTimeout: 5 * time.Second}})
	require.ErrorContains(t, err, "within five seconds")

	serverconfig.SetConfigTemplate(appConfigTemplate)
	cfg, err := inProcessNetworkConfig(testnetStartOptions{testnetOptions: testnetOptions{numValidators: 1, commitTimeout: time.Second, chainID: "in-process"}})
	require.NoError(t, err)
	require.Equal(t, "in-process", cfg.ChainID)
	require.Equal(t, time.Second, cfg.TimeoutCommit)
	// What network.New does for each validator, and what panicked before.
	require.NotPanics(t, func() {
		serverconfig.WriteConfigFile(filepath.Join(t.TempDir(), "app.toml"), serverconfig.DefaultConfig())
	})
}
