package validation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"cosmossdk.io/log/v2"
)

// LivenessResults maps each denom observed as active to its liveness percentage.
type LivenessResults map[string]float64

// Validator samples the public oracle price API and checks price liveness.
type Validator struct {
	logger           log.Logger
	client           PriceClient
	voteTargetClient VoteTargetClient
	cfg              Config
}

// NewValidator returns a validator using client, voteTargetClient, and cfg.
func NewValidator(
	logger log.Logger,
	client PriceClient,
	voteTargetClient VoteTargetClient,
	cfg Config,
) (*Validator, error) {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	if client == nil {
		return nil, errors.New("price client cannot be nil")
	}
	if voteTargetClient == nil {
		return nil, errors.New("vote target client cannot be nil")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &Validator{
		logger:           logger.With("component", "validation"),
		client:           client,
		voteTargetClient: voteTargetClient,
		cfg:              cfg,
	}, nil
}

// Run samples the price API and returns liveness percentages for active denoms.
func (v *Validator) Run(ctx context.Context) (LivenessResults, error) {
	if err := wait(ctx, v.cfg.BurnInPeriod); err != nil {
		return nil, err
	}

	activeDenoms, err := v.loadActiveDenoms(ctx, v.cfg.RequestTimeout)
	if err != nil {
		return nil, fmt.Errorf("load initial active denoms: %w", err)
	}

	checkCounts := make(map[string]int, len(activeDenoms))
	missingCounts := make(map[string]int, len(activeDenoms))
	checkInterval := v.cfg.ValidationPeriod / time.Duration(v.cfg.NumChecks)
	nextDenomRefresh := v.cfg.DenomRefreshInterval
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
		if elapsed >= nextDenomRefresh {
			refreshedDenoms, refreshErr := v.loadActiveDenoms(ctx, requestTimeout)
			if refreshErr != nil {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				v.logger.Warn("failed to refresh active denoms; using last known denoms", "err", refreshErr)
			} else {
				activeDenoms = refreshedDenoms
			}
			nextDenomRefresh = elapsed + v.cfg.DenomRefreshInterval
		}

		requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
		missingDenoms := v.samplePrices(requestCtx, activeDenoms)
		cancel()
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		for _, denom := range activeDenoms {
			checkCounts[denom]++
		}
		for _, denom := range missingDenoms {
			missingCounts[denom]++
		}
	}

	results := make(LivenessResults, len(checkCounts))
	invalidDenoms := make([]string, 0)
	for denom, numChecks := range checkCounts {
		liveness := float64(numChecks-missingCounts[denom]) / float64(numChecks) * 100
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
