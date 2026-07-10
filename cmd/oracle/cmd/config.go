package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	oracleconfig "ark/oracle/config"
	transporttypes "ark/oracle/types"
)

func newConfigCmd(configPath *string) *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage oracle config.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	configCmd.AddCommand(
		newConfigValidateCmd(configPath),
		newConfigReloadCmd(),
	)

	return configCmd
}

func newConfigValidateCmd(configPath *string) *cobra.Command {
	validateCmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate the oracle config.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := oracleconfig.Load(*configPath); err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), "oracle config is valid")
			return nil
		},
	}

	return validateCmd
}

func newConfigReloadCmd() *cobra.Command {
	adminAddress := defaultAdminAddress

	reloadCmd := &cobra.Command{
		Use:   "reload",
		Short: "Reload the running oracle's startup config.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := reloadRuntimeConfig(cmd.Context(), adminAddress); err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), "oracle config reloaded")
			return nil
		},
	}

	reloadCmd.Flags().StringVar(&adminAddress, flagAdminAddress, adminAddress, "Oracle admin gRPC address.")

	return reloadCmd
}

// reloadRuntimeConfig is a short-lived admin client. The running sidecar owns
// reading, validating, and applying the config file fixed at its construction.
func reloadRuntimeConfig(ctx context.Context, address string, dialOptions ...grpc.DialOption) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(address) == "" {
		return errors.New("oracle admin address cannot be empty")
	}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithNoProxy(),
	}
	opts = append(opts, dialOptions...)

	conn, err := grpc.NewClient(address, opts...)
	if err != nil {
		return fmt.Errorf("dialling oracle admin endpoint: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing oracle admin connection: %w", closeErr))
		}
	}()

	client := transporttypes.NewOracleAdminClient(conn)
	if _, err := client.ReloadConfig(ctx, &transporttypes.OracleReloadConfigRequest{}); err != nil {
		return fmt.Errorf("reloading oracle config: %w", err)
	}

	return nil
}
