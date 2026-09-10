package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"

	"cosmossdk.io/log/v2"

	"github.com/ararat-network/ark/pkg/grpcconn"
	"github.com/ararat-network/ark/pkg/tlsconfig"
	"github.com/ararat-network/ark/pricefeed/api"
	"github.com/ararat-network/ark/pricefeed/validation"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

const (
	defaultChainAddress = "127.0.0.1:9090"

	flagChainAddress                 = "chain-address"
	flagBurnInPeriod                 = "burn-in-period"
	flagValidationPeriod             = "validation-period"
	flagNumChecks                    = "num-checks"
	flagRequiredPriceLivenessPercent = "required-price-liveness-percent"
	flagMaxResponseAge               = "max-response-age"
	flagMaxFutureSkew                = "max-future-skew"
	flagRequestTimeout               = "request-timeout"
	flagFeedRefreshInterval          = "feed-refresh-interval"
)

type checkOptions struct {
	address      string
	tls          tlsconfig.Client
	chainAddress string
	chainTLS     tlsconfig.Client
	cfg          validation.Config
}

func newCheckCmd() *cobra.Command {
	options := checkOptions{
		address:      defaultAddress,
		chainAddress: defaultChainAddress,
		cfg:          validation.DefaultConfig(),
	}

	checkCmd := &cobra.Command{
		Use:   "check",
		Short: "Check a running sidecar's price liveness against the chain's feed set.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger, err := newLogger(defaultLogLevel, defaultLogJSON)
			if err != nil {
				return err
			}

			results, checkErr := runCheck(cmd.Context(), logger, options)
			return writeCheckOutcome(cmd.OutOrStdout(), results, checkErr)
		},
	}

	flags := checkCmd.Flags()
	flags.StringVar(&options.address, flagAddress, options.address, "Sidecar gRPC address.")
	addClientTLSFlags(flags, "", "sidecar", &options.tls)
	flags.StringVar(&options.chainAddress, flagChainAddress, options.chainAddress, "Chain gRPC address the active feed set is read from.")
	addClientTLSFlags(flags, chainFlagPrefix, "chain", &options.chainTLS)
	flags.DurationVar(&options.cfg.BurnInPeriod, flagBurnInPeriod, options.cfg.BurnInPeriod, "Time to wait before sampling begins.")
	flags.DurationVar(&options.cfg.ValidationPeriod, flagValidationPeriod, options.cfg.ValidationPeriod, "Duration over which prices are sampled.")
	flags.IntVar(&options.cfg.NumChecks, flagNumChecks, options.cfg.NumChecks, "Number of price samples to take.")
	flags.Float64Var(
		&options.cfg.RequiredPriceLivenessPercent,
		flagRequiredPriceLivenessPercent,
		options.cfg.RequiredPriceLivenessPercent,
		"Minimum required price liveness percentage per feed.",
	)
	flags.DurationVar(&options.cfg.MaxResponseAge, flagMaxResponseAge, options.cfg.MaxResponseAge, "Maximum accepted age of a price response.")
	flags.DurationVar(&options.cfg.MaxFutureSkew, flagMaxFutureSkew, options.cfg.MaxFutureSkew, "Maximum accepted future skew of a price response.")
	flags.DurationVar(&options.cfg.RequestTimeout, flagRequestTimeout, options.cfg.RequestTimeout, "Timeout for each RPC.")
	flags.DurationVar(
		&options.cfg.FeedRefreshInterval,
		flagFeedRefreshInterval,
		options.cfg.FeedRefreshInterval,
		"Interval at which the active feed set is refreshed.",
	)

	return checkCmd
}

// writeCheckOutcome reports every liveness result before returning the
// failure that produced them.
func writeCheckOutcome(w io.Writer, results validation.LivenessResults, checkErr error) error {
	if errors.Is(checkErr, validation.ErrNoActiveFeeds) {
		if _, err := fmt.Fprintln(w, "check skipped: no active feeds (oracle voting disabled)"); err != nil {
			return fmt.Errorf("writing check result: %w", err)
		}
		return nil
	}

	if len(results) > 0 {
		feeds := make([]string, 0, len(results))
		for denom := range results {
			feeds = append(feeds, denom)
		}
		sort.Strings(feeds)

		if _, err := fmt.Fprintln(w, "liveness results:"); err != nil {
			return errors.Join(checkErr, fmt.Errorf("writing check results: %w", err))
		}
		for _, denom := range feeds {
			if _, err := fmt.Fprintf(w, "%s: %.2f%%\n", denom, results[denom]); err != nil {
				return errors.Join(checkErr, fmt.Errorf("writing check result for %s: %w", denom, err))
			}
		}
	}
	if checkErr != nil {
		return fmt.Errorf("check failed: %w", checkErr)
	}

	if _, err := fmt.Fprintln(w, "check passed"); err != nil {
		return fmt.Errorf("writing check result: %w", err)
	}
	return nil
}

func runCheck(
	ctx context.Context,
	logger log.Logger,
	options checkOptions,
	dialOptions ...grpc.DialOption,
) (results validation.LivenessResults, err error) {
	if strings.TrimSpace(options.address) == "" {
		return nil, errors.New("sidecar address cannot be empty")
	}
	if strings.TrimSpace(options.chainAddress) == "" {
		return nil, errors.New("chain address cannot be empty")
	}

	sidecarMaterial, err := grpcconn.LoadClient(options.tls, options.address)
	if err != nil {
		return nil, fmt.Errorf("sidecar connection credentials: %w", err)
	}
	chainMaterial, err := grpcconn.LoadClient(options.chainTLS, options.chainAddress)
	if err != nil {
		return nil, fmt.Errorf("chain connection credentials: %w", err)
	}

	sidecarConn, err := grpc.NewClient(options.address, grpcconn.DialOptions(sidecarMaterial.Config, dialOptions...)...)
	if err != nil {
		return nil, fmt.Errorf("creating sidecar connection: %w", err)
	}
	defer func() {
		if closeErr := sidecarConn.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing sidecar connection: %w", closeErr))
		}
	}()

	chainConn, err := grpc.NewClient(options.chainAddress, grpcconn.DialOptions(chainMaterial.Config, dialOptions...)...)
	if err != nil {
		return nil, fmt.Errorf("creating chain connection: %w", err)
	}
	defer func() {
		if closeErr := chainConn.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing chain connection: %w", closeErr))
		}
	}()

	validator, err := validation.NewValidator(
		logger,
		api.NewPriceFeedClient(sidecarConn),
		oracletypes.NewQueryClient(chainConn),
		options.cfg,
	)
	if err != nil {
		return nil, fmt.Errorf("creating validator: %w", err)
	}

	return validator.Run(ctx)
}
