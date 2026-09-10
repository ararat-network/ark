// Package cmd owns sidecar CLI configuration, logging, telemetry, and administrative clients.
// pricefeed/sidecar owns service execution; pricefeed/validation owns liveness checks.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pricefeed/sidecar"
)

const (
	// serviceName names the binary, its logs, and its metrics alike, so the
	// signals join on one name.
	serviceName = "pricefeed"

	// The sidecar keeps its own directory under the home rather than sharing
	// the node's config/, which holds the consensus key.
	defaultHomeDir      = ".ark"
	defaultPricefeedDir = "pricefeed"
	defaultConfigFile   = "pricefeed.toml"

	defaultLogLevel  = "info"
	defaultLogFormat = logFormatPlain

	flagConfig    = "config"
	flagLogLevel  = "log-level"
	flagLogFormat = "log-format"

	logFormatPlain = "plain"
	logFormatJSON  = "json"
)

// rootOptions are the persistent flags every command shares: the runtime
// config path, and the log flags the logger is built from before a command
// runs.
type rootOptions struct {
	configPath string
	logLevel   string
	logFormat  string
	logger     log.Logger
}

// NewRootCmd constructs a fresh command tree for the pricefeed executable.
func NewRootCmd() *cobra.Command {
	options := &rootOptions{
		configPath: defaultConfigPath(),
		logLevel:   defaultLogLevel,
		logFormat:  defaultLogFormat,
	}

	rootCmd := &cobra.Command{
		Use:           serviceName,
		Short:         "Price-feed sidecar command",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			logger, err := newLogger(options.logLevel, options.logFormat)
			if err != nil {
				return err
			}
			options.logger = logger
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	flags := rootCmd.PersistentFlags()
	flags.StringVar(&options.configPath, flagConfig, options.configPath, "Path to the price-feed runtime config file.")
	flags.StringVar(&options.logLevel, flagLogLevel, options.logLevel, "Log level (debug, info, warn, error, disabled).")
	flags.StringVar(&options.logFormat, flagLogFormat, options.logFormat, "Log format (plain or json).")

	rootCmd.AddCommand(
		newStartCmd(options),
		newPricesCmd(),
		newCheckCmd(options),
		newInitCmd(options),
		newConfigCmd(options),
		newVersionCmd(),
	)

	return rootCmd
}

func defaultConfigPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" {
		return ""
	}

	return filepath.Join(homeDir, defaultHomeDir, defaultPricefeedDir, defaultConfigFile)
}

func newLogger(level, format string) (log.Logger, error) {
	filter, err := log.ParseLogLevel(level)
	if err != nil {
		return nil, err
	}

	opts := []log.Option{
		log.FilterOption(filter),
		log.ColorOption(false),
	}
	switch format {
	case logFormatPlain:
	case logFormatJSON:
		opts = append(opts, log.OutputJSONOption())
	default:
		return nil, fmt.Errorf("unsupported log format %q; expected %s or %s", format, logFormatPlain, logFormatJSON)
	}

	return log.NewLogger(os.Stderr, opts...).With("service", serviceName), nil
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the sidecar build version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), sidecar.Version())
		},
	}
}
