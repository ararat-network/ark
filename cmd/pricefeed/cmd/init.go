package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	oracleconfig "github.com/ararat-network/ark/pricefeed/config"
)

const (
	defaultForce = false

	flagForce = "force"
)

func newInitCmd(configPath *string) *cobra.Command {
	var force bool

	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialise the oracle config file.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return initRuntimeConfig(*configPath, force)
		},
	}

	initCmd.Flags().BoolVar(&force, flagForce, defaultForce, "Overwrite the oracle config file if it already exists.")

	return initCmd
}

// initRuntimeConfig writes a validated default config with owner-only file
// permissions. It refuses to replace an existing file unless force is set.
func initRuntimeConfig(path string, force bool) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("oracle config path cannot be empty")
	}
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("oracle config %q already exists; pass --%s to overwrite", path, flagForce)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("checking oracle config: %w", err)
	}

	cfg := oracleconfig.Default()
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("default oracle config is invalid: %w", err)
	}

	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating oracle config directory: %w", err)
		}
	}

	bz, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding oracle config: %w", err)
	}

	return os.WriteFile(path, append(bz, '\n'), 0o600)
}
