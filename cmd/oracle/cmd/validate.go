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
	"google.golang.org/grpc/credentials/insecure"

	"cosmossdk.io/log/v2"

	transporttypes "ark/oracle/types"
	"ark/oracle/validation"
	oracletypes "ark/x/oracle/types"
)

const (
	defaultOracleAddress = defaultAddress
	defaultChainAddress  = "127.0.0.1:9090"

	flagOracleAddress                = "oracle-address"
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

type validateOptions struct {
	oracleAddress string
	chainAddress  string
	cfg           validation.Config
}

func newValidateCmd() *cobra.Command {
	options := validateOptions{
		oracleAddress: defaultOracleAddress,
		chainAddress:  defaultChainAddress,
		cfg:           validation.DefaultConfig(),
	}

	validateCmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a running oracle sidecar.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger, err := newLogger(defaultLogLevel, defaultLogJSON)
			if err != nil {
				return err
			}

			results, validationErr := runValidation(cmd.Context(), logger, options)
			return writeValidationOutcome(cmd.OutOrStdout(), results, validationErr)
		},
	}

	flags := validateCmd.Flags()
	flags.StringVar(&options.oracleAddress, flagOracleAddress, options.oracleAddress, "Running oracle gRPC address.")
	flags.StringVar(&options.chainAddress, flagChainAddress, options.chainAddress, "Chain gRPC address used to query the oracle feed set.")
	flags.DurationVar(&options.cfg.BurnInPeriod, flagBurnInPeriod, options.cfg.BurnInPeriod, "Time to wait before validation begins.")
	flags.DurationVar(&options.cfg.ValidationPeriod, flagValidationPeriod, options.cfg.ValidationPeriod, "Duration over which prices are sampled.")
	flags.IntVar(&options.cfg.NumChecks, flagNumChecks, options.cfg.NumChecks, "Number of price checks to run.")
	flags.Float64Var(
		&options.cfg.RequiredPriceLivenessPercent,
		flagRequiredPriceLivenessPercent,
		options.cfg.RequiredPriceLivenessPercent,
		"Minimum required price liveness percentage per feed.",
	)
	flags.DurationVar(&options.cfg.MaxResponseAge, flagMaxResponseAge, options.cfg.MaxResponseAge, "Maximum accepted age of an oracle price response.")
	flags.DurationVar(&options.cfg.MaxFutureSkew, flagMaxFutureSkew, options.cfg.MaxFutureSkew, "Maximum accepted future skew of an oracle price response.")
	flags.DurationVar(&options.cfg.RequestTimeout, flagRequestTimeout, options.cfg.RequestTimeout, "Timeout for each validation RPC.")
	flags.DurationVar(
		&options.cfg.FeedRefreshInterval,
		flagFeedRefreshInterval,
		options.cfg.FeedRefreshInterval,
		"Interval at which the active feed set is refreshed.",
	)

	return validateCmd
}

// writeValidationOutcome reports all available liveness results before
// returning the validation failure that produced them.
func writeValidationOutcome(
	w io.Writer,
	results validation.LivenessResults,
	validationErr error,
) error {
	if errors.Is(validationErr, validation.ErrNoActiveFeeds) {
		if _, err := fmt.Fprintln(w, "oracle validation skipped: no active feeds (oracle voting disabled)"); err != nil {
			return fmt.Errorf("writing disabled oracle validation result: %w", err)
		}
		return nil
	}

	if len(results) > 0 {
		feeds := make([]string, 0, len(results))
		for denom := range results {
			feeds = append(feeds, denom)
		}
		sort.Strings(feeds)

		if _, err := fmt.Fprintln(w, "oracle validation results:"); err != nil {
			return errors.Join(validationErr, fmt.Errorf("writing validation results: %w", err))
		}
		for _, denom := range feeds {
			if _, err := fmt.Fprintf(w, "%s: %.2f%%\n", denom, results[denom]); err != nil {
				return errors.Join(validationErr, fmt.Errorf("writing validation result for %s: %w", denom, err))
			}
		}
	}
	if validationErr != nil {
		return fmt.Errorf("oracle validation failed: %w", validationErr)
	}

	fmt.Fprintln(w, "oracle validation passed")
	return nil
}

func runValidation(
	ctx context.Context,
	logger log.Logger,
	options validateOptions,
	dialOptions ...grpc.DialOption,
) (results validation.LivenessResults, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if logger == nil {
		logger = log.NewNopLogger()
	}
	if strings.TrimSpace(options.oracleAddress) == "" {
		return nil, errors.New("oracle address cannot be empty")
	}
	if strings.TrimSpace(options.chainAddress) == "" {
		return nil, errors.New("chain address cannot be empty")
	}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithNoProxy(),
	}
	opts = append(opts, dialOptions...)

	oracleConn, err := grpc.NewClient(options.oracleAddress, opts...)
	if err != nil {
		return nil, fmt.Errorf("creating oracle connection: %w", err)
	}
	defer func() {
		if closeErr := oracleConn.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing oracle connection: %w", closeErr))
		}
	}()

	chainConn, err := grpc.NewClient(options.chainAddress, opts...)
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
		transporttypes.NewOracleClient(oracleConn),
		oracletypes.NewQueryClient(chainConn),
		options.cfg,
	)
	if err != nil {
		return nil, fmt.Errorf("creating oracle validator: %w", err)
	}

	return validator.Run(ctx)
}
