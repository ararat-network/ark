package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"ark/oracle/sidecar"
)

const (
	defaultHomeDir    = ".ark"
	defaultOracleDir  = "oracle"
	defaultConfigFile = "config.json"

	flagConfig = "config"
)

// NewRootCmd constructs a fresh command tree for the oracle executable.
func NewRootCmd() *cobra.Command {
	configPath := defaultConfigPath()

	rootCmd := &cobra.Command{
		Use:           "oracle",
		Short:         "Oracle sidecar command.",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	rootCmd.PersistentFlags().StringVar(&configPath, flagConfig, configPath, "Path to the oracle runtime config file.")

	rootCmd.AddCommand(
		newStartCmd(&configPath),
		newPricesCmd(),
		newValidateCmd(),
		newInitCmd(&configPath),
		newConfigCmd(&configPath),
		newVersionCmd(),
	)

	return rootCmd
}

func defaultConfigPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" {
		return ""
	}

	return filepath.Join(homeDir, defaultHomeDir, defaultOracleDir, defaultConfigFile)
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of the oracle.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), sidecar.Version())
		},
	}
}
