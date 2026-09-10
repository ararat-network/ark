package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"

	"github.com/ararat-network/ark/pkg/grpcconn"
	"github.com/ararat-network/ark/pricefeed/api"
	"github.com/ararat-network/ark/pricefeed/config"
)

func newConfigCmd(root *rootOptions) *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage the sidecar config file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	configCmd.AddCommand(
		newConfigValidateCmd(root),
		newConfigReloadCmd(),
	)

	return configCmd
}

func newConfigValidateCmd(root *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate the sidecar config file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := config.Load(root.configPath); err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), "pricefeed config is valid")
			return nil
		},
	}
}

func newConfigReloadCmd() *cobra.Command {
	adminAddress := defaultAdminAddress

	reloadCmd := &cobra.Command{
		Use:   "reload",
		Short: "Reload the running sidecar's config file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := reloadRuntimeConfig(cmd.Context(), adminAddress); err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), "pricefeed config reloaded")
			return nil
		},
	}

	reloadCmd.Flags().StringVar(&adminAddress, flagAdminAddress, adminAddress, "Sidecar admin gRPC address.")

	return reloadCmd
}

// reloadRuntimeConfig is a short-lived admin client. The running sidecar owns
// reading, validating, and applying the config file fixed at its construction.
func reloadRuntimeConfig(ctx context.Context, address string, dialOptions ...grpc.DialOption) (err error) {
	if strings.TrimSpace(address) == "" {
		return errors.New("admin address cannot be empty")
	}
	// The admin listener is loopback and plaintext, so the client dials the
	// same way and carries no transport flags.
	if !grpcconn.Loopback(address) {
		return fmt.Errorf("admin address %q must be loopback", address)
	}

	conn, err := grpc.NewClient(address, grpcconn.DialOptions(nil, dialOptions...)...)
	if err != nil {
		return fmt.Errorf("dialling admin endpoint: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing admin connection: %w", closeErr))
		}
	}()

	if _, err := api.NewPriceFeedAdminClient(conn).ReloadConfig(ctx, &api.ReloadConfigRequest{}); err != nil {
		return fmt.Errorf("reloading pricefeed config: %w", err)
	}

	return nil
}
