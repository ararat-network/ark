package validation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"google.golang.org/grpc"

	"cosmossdk.io/log/v2"

	"noah/oracle/transport/types"
)

const (
	DefaultNumChecks                    = 1000
	DefaultRequiredPriceLivenessPercent = 99.0
	DefaultValidationPeriod             = 10 * time.Minute
	DefaultBurnInPeriod                 = time.Minute
	DefaultMaxResponseAge               = time.Minute
)

// PriceClient is the public oracle price API used by validation.
type PriceClient interface {
	Prices(context.Context, *types.OraclePricesRequest, ...grpc.CallOption) (*types.OraclePricesResponse, error)
}

// Config configures a validation run against the public oracle price API.
type Config struct {
	// BurnInPeriod is how long to let the oracle run before validation checks begin.
	BurnInPeriod time.Duration
	// ValidationPeriod is how long to sample the price API.
	ValidationPeriod time.Duration
	// NumChecks is the number of price API checks to run over ValidationPeriod.
	NumChecks int
	// RequiredPriceLivenessPercent is the minimum successful response percentage per denom.
	RequiredPriceLivenessPercent float64
	// ExpectedDenoms is the denom set that must appear in price API responses.
	ExpectedDenoms []string
	// MaxResponseAge is the maximum accepted age of a price API response timestamp.
	MaxResponseAge time.Duration
}

// DefaultConfig returns a validation config with default timings and thresholds.
func DefaultConfig(expectedDenoms []string) Config {
	return Config{
		BurnInPeriod:                 DefaultBurnInPeriod,
		ValidationPeriod:             DefaultValidationPeriod,
		NumChecks:                    DefaultNumChecks,
		RequiredPriceLivenessPercent: DefaultRequiredPriceLivenessPercent,
		ExpectedDenoms:               append([]string(nil), expectedDenoms...),
		MaxResponseAge:               DefaultMaxResponseAge,
	}
}

// Validate checks whether the validation config can be used.
func (c Config) Validate() error {
	if c.BurnInPeriod < 0 {
		return errors.New("burn in period cannot be negative")
	}
	if c.ValidationPeriod <= 0 {
		return errors.New("validation period must be greater than zero")
	}
	if c.NumChecks <= 0 {
		return errors.New("num checks must be greater than zero")
	}
	if c.ValidationPeriod/time.Duration(c.NumChecks) <= 0 {
		return errors.New("validation period must be long enough for num checks")
	}
	if c.RequiredPriceLivenessPercent <= 0 || c.RequiredPriceLivenessPercent > 100 {
		return errors.New("required price liveness percent must be between 0 and 100")
	}
	if len(c.ExpectedDenoms) == 0 {
		return errors.New("expected denoms cannot be empty")
	}
	if c.MaxResponseAge <= 0 {
		return errors.New("max response age must be greater than zero")
	}

	return nil
}

// LivenessResults maps each expected denom to its observed liveness percentage.
type LivenessResults map[string]float64

// Validator samples the public oracle price API and checks price liveness.
type Validator struct {
	logger log.Logger
	client PriceClient
	cfg    Config
}

// NewValidator returns a validator using client and cfg.
func NewValidator(logger log.Logger, client PriceClient, cfg Config) (*Validator, error) {
	if logger == nil {
		return nil, errors.New("logger cannot be nil")
	}
	if client == nil {
		return nil, errors.New("price client cannot be nil")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	cfg.ExpectedDenoms = append([]string(nil), cfg.ExpectedDenoms...)
	return &Validator{
		logger: logger.With("process", "oracle_validation"),
		client: client,
		cfg:    cfg,
	}, nil
}

// Run samples the price API and returns liveness percentages for expected denoms.
func (v *Validator) Run(ctx context.Context) (LivenessResults, error) {
	if err := wait(ctx, v.cfg.BurnInPeriod); err != nil {
		return nil, err
	}

	missingCounts := make(map[string]int, len(v.cfg.ExpectedDenoms))
	checkInterval := v.cfg.ValidationPeriod / time.Duration(v.cfg.NumChecks)
	for range v.cfg.NumChecks {
		if err := wait(ctx, checkInterval); err != nil {
			return nil, err
		}

		missing := v.missingDenoms(ctx)
		for _, denom := range missing {
			missingCounts[denom]++
		}
	}

	results := make(LivenessResults, len(v.cfg.ExpectedDenoms))
	invalidDenoms := make([]string, 0)
	for _, denom := range v.cfg.ExpectedDenoms {
		liveness := float64(v.cfg.NumChecks-missingCounts[denom]) / float64(v.cfg.NumChecks) * 100
		results[denom] = liveness
		if liveness < v.cfg.RequiredPriceLivenessPercent {
			invalidDenoms = append(invalidDenoms, denom)
		}
	}

	if len(invalidDenoms) > 0 {
		sort.Strings(invalidDenoms)
		return results, fmt.Errorf("invalid denoms below liveness threshold: %v", invalidDenoms)
	}

	return results, nil
}

func (v *Validator) missingDenoms(ctx context.Context) []string {
	resp, err := v.client.Prices(ctx, &types.OraclePricesRequest{})
	if err != nil {
		v.logger.Error("failed to fetch oracle prices", "err", err)
		return append([]string(nil), v.cfg.ExpectedDenoms...)
	}
	if resp == nil {
		v.logger.Error("oracle price response is nil")
		return append([]string(nil), v.cfg.ExpectedDenoms...)
	}
	if time.Since(resp.Timestamp) > v.cfg.MaxResponseAge {
		v.logger.Error(
			"oracle price response is stale",
			"timestamp", resp.Timestamp.String(),
			"max_response_age", v.cfg.MaxResponseAge.String(),
		)
		return append([]string(nil), v.cfg.ExpectedDenoms...)
	}

	missing := make([]string, 0)
	for _, denom := range v.cfg.ExpectedDenoms {
		if _, ok := resp.Prices[denom]; !ok {
			missing = append(missing, denom)
		}
	}

	return missing
}

func wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
