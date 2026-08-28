package providertest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ararat-network/ark/pricefeed/sidecar/providers/base"
	"github.com/ararat-network/ark/pricefeed/sidecar/providers/types"
	oracletypes "github.com/ararat-network/ark/pricefeed/sidecar/types"
)

// Builder constructs a provider for a test run.
type Builder func(context.Context) (*base.Provider, error)

// Config controls how long a provider test run observes prices.
type Config struct {
	// TestDuration is the total amount of time to collect prices.
	TestDuration time.Duration
	// PollInterval is how often prices are sampled from the provider.
	PollInterval time.Duration
	// BurnInInterval is how long the provider is allowed to run before sampling starts.
	BurnInInterval time.Duration
	// ExpectedPriceCount is the number of prices expected on each poll. If zero,
	// the harness expects one price per configured ticker.
	ExpectedPriceCount int
}

// Validate checks that the test timing configuration can produce at least one poll.
func (c Config) Validate() error {
	if c.TestDuration == 0 {
		return errors.New("test duration cannot be 0")
	}
	if c.PollInterval == 0 {
		return errors.New("poll interval cannot be 0")
	}
	if c.TestDuration/c.PollInterval < 1 {
		return errors.New("ratio of test duration to poll interval must be GTE 1")
	}
	if c.ExpectedPriceCount < 0 {
		return errors.New("expected price count cannot be negative")
	}

	return nil
}

// DefaultProviderTestConfig returns a conservative config for manual provider runs.
func DefaultProviderTestConfig() Config {
	return Config{
		TestDuration:   time.Minute,
		PollInterval:   5 * time.Second,
		BurnInInterval: 5 * time.Second,
	}
}

// PriceResults contains price snapshots collected during a provider test run.
type PriceResults []PriceResult

// PriceResult is one snapshot of provider prices.
type PriceResult struct {
	Prices map[oracletypes.Pair]types.Result
	Time   time.Time
}

// Run builds a provider and collects price snapshots from it.
func Run(ctx context.Context, build Builder, cfg Config) (PriceResults, error) {
	if build == nil {
		return nil, errors.New("builder is nil")
	}

	provider, err := build(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to build provider: %w", err)
	}

	return RunProvider(ctx, provider, cfg)
}

// RunProvider runs provider, samples prices, and waits for cleanup before returning.
func RunProvider(ctx context.Context, provider *base.Provider, cfg Config) (PriceResults, error) {
	if ctx == nil {
		return nil, errors.New("context cannot be nil")
	}
	if provider == nil {
		return nil, errors.New("provider is nil")
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	expectedPriceCount := cfg.ExpectedPriceCount
	if expectedPriceCount == 0 {
		expectedPriceCount = len(provider.GetTickers())
	}
	if expectedPriceCount == 0 {
		return nil, errors.New("expected price count cannot be 0")
	}

	runCtx, cancel := context.WithCancel(ctx)
	doneCh := make(chan struct{})
	var runErr error
	go func() {
		defer close(doneCh)
		runErr = provider.Run(runCtx)
	}()
	defer func() {
		cancel()
		<-doneCh
	}()

	if cfg.BurnInInterval > 0 {
		burnInTimer := time.NewTimer(cfg.BurnInInterval)
		select {
		case <-burnInTimer.C:
		case <-doneCh:
			return nil, providerStoppedError("provider stopped during burn-in", runErr)
		case <-ctx.Done():
			burnInTimer.Stop()
			return nil, ctx.Err()
		}
	}

	priceResults := make(PriceResults, 0, cfg.TestDuration/cfg.PollInterval)
	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	timer := time.NewTimer(cfg.TestDuration)
	defer timer.Stop()

	for {
		select {
		case <-ticker.C:
			prices := provider.GetPrices()
			if len(prices) != expectedPriceCount {
				return nil, fmt.Errorf("expected %d prices, got %d", expectedPriceCount, len(prices))
			}

			priceResults = append(priceResults, PriceResult{
				Prices: prices,
				Time:   time.Now().UTC(),
			})
		case <-timer.C:
			return priceResults, nil
		case <-doneCh:
			return nil, providerStoppedError("provider stopped while collecting prices", runErr)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func providerStoppedError(message string, err error) error {
	if err == nil {
		return errors.New(message)
	}

	return fmt.Errorf("%s: %w", message, err)
}
