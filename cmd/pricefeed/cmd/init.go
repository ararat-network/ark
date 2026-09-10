package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ararat-network/ark/pkg/fsutil"
	"github.com/ararat-network/ark/pricefeed/config"
)

const (
	defaultOverwrite = false

	flagOverwrite = "overwrite"
)

func newInitCmd(root *rootOptions) *cobra.Command {
	var overwrite bool

	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Write the default sidecar config file",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return initRuntimeConfig(root.configPath, overwrite)
		},
	}

	initCmd.Flags().BoolVar(&overwrite, flagOverwrite, defaultOverwrite, "Overwrite the config file if it already exists.")

	return initCmd
}

// initRuntimeConfig writes a validated default config with owner-only file
// permissions. It refuses to replace an existing file unless overwrite is set.
func initRuntimeConfig(path string, overwrite bool) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("pricefeed config path cannot be empty")
	}
	if _, err := os.Stat(path); err == nil && !overwrite {
		return fmt.Errorf("pricefeed config %q already exists; pass --%s to overwrite", path, flagOverwrite)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("checking pricefeed config: %w", err)
	}

	cfg := config.Default()
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("default pricefeed config is invalid: %w", err)
	}
	bz, err := config.Encode(cfg)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating pricefeed config directory: %w", err)
	}

	return fsutil.ReplaceFile(path, bz, 0o600)
}
