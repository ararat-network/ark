package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ararat-network/ark/pricefeed/config"
)

const (
	defaultForce = false

	flagForce = "force"
)

func newInitCmd(configPath *string) *cobra.Command {
	var force bool

	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Write the default sidecar config file.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return initRuntimeConfig(*configPath, force)
		},
	}

	initCmd.Flags().BoolVar(&force, flagForce, defaultForce, "Overwrite the config file if it already exists.")

	return initCmd
}

// initRuntimeConfig writes a validated default config with owner-only file
// permissions. It refuses to replace an existing file unless force is set.
func initRuntimeConfig(path string, force bool) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("pricefeed config path cannot be empty")
	}
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("pricefeed config %q already exists; pass --%s to overwrite", path, flagForce)
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

	return replaceFile(path, bz)
}

// replaceFile writes data to a fresh owner-only file beside path and renames
// it into place: a reader never sees a partial file, and a mode the old file
// had is not kept.
func replaceFile(path string, data []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("creating pricefeed config: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("writing pricefeed config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("writing pricefeed config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing pricefeed config: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replacing pricefeed config: %w", err)
	}

	return nil
}
