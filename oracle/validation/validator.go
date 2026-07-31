package validation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"cosmossdk.io/log/v2"
)

// LivenessResults maps each feed observed as active to its liveness percentage.
type LivenessResults map[string]float64

// Validator samples the public oracle price API and checks price liveness.
type Validator struct {
	logger     log.Logger
	client     PriceClient
	feedClient FeedClient
	cfg        Config
}

// NewValidator returns a validator using client, feedClient, and cfg.
func NewValidator(
	logger log.Logger,
	client PriceClient,
	feedClient FeedClient,
	cfg Config,
) (*Validator, error) {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	if client == nil {
		return nil, errors.New("price client cannot be nil")
	}
	if feedClient == nil {
		return nil, errors.New("feed client cannot be nil")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &Validator{
		logger:     logger.With("component", "validation"),
		client:     client,
		feedClient: feedClient,
		cfg:        cfg,
	}, nil
}

// Run samples the price API and returns liveness percentages for active feeds.
func (v *Validator) Run(ctx context.Context) (LivenessResults, error) {
	if err := wait(ctx, v.cfg.BurnInPeriod); err != nil {
		return nil, err
	}

	activeFeeds, err := v.loadActiveFeeds(ctx, v.cfg.RequestTimeout)
	if err != nil {
		return nil, fmt.Errorf("load initial active feeds: %w", err)
	}

	checkCounts := make(map[string]int, len(activeFeeds))
	missingCounts := make(map[string]int, len(activeFeeds))
	checkInterval := v.cfg.ValidationPeriod / time.Duration(v.cfg.NumChecks)
	nextFeedRefresh := v.cfg.FeedRefreshInterval
	elapsed := time.Duration(0)
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for range v.cfg.NumChecks {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}

		requestTimeout := min(v.cfg.RequestTimeout, checkInterval)
		elapsed += checkInterval
		if elapsed >= nextFeedRefresh {
			refreshedFeeds, refreshErr := v.loadActiveFeeds(ctx, requestTimeout)
			if refreshErr != nil {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				v.logger.Warn("failed to refresh active feeds; using last known feeds", "err", refreshErr)
			} else {
				activeFeeds = refreshedFeeds
			}
			nextFeedRefresh = elapsed + v.cfg.FeedRefreshInterval
		}

		requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
		missingFeeds := v.samplePrices(requestCtx, activeFeeds)
		cancel()
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		for _, denom := range activeFeeds {
			checkCounts[denom]++
		}
		for _, denom := range missingFeeds {
			missingCounts[denom]++
		}
	}

	results := make(LivenessResults, len(checkCounts))
	invalidFeeds := make([]string, 0)
	for denom, numChecks := range checkCounts {
		liveness := float64(numChecks-missingCounts[denom]) / float64(numChecks) * 100
		results[denom] = liveness
		if liveness < v.cfg.RequiredPriceLivenessPercent {
			invalidFeeds = append(invalidFeeds, denom)
		}
	}

	if len(invalidFeeds) > 0 {
		sort.Strings(invalidFeeds)
		return results, fmt.Errorf("invalid feeds below liveness threshold: %v", invalidFeeds)
	}

	return results, nil
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
