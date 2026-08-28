package validation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"google.golang.org/grpc"

	chain "ark/pkg/chain"
	"ark/pkg/encoding"
	"ark/pricefeed/api"
	oracletypes "ark/x/oracle/types"
)

// PriceClient is the public oracle price API used by validation.
type PriceClient interface {
	Prices(context.Context, *api.PricesRequest, ...grpc.CallOption) (*api.PricesResponse, error)
}

// FeedClient is the on-chain oracle query used to load active feeds.
type FeedClient interface {
	Feeds(
		context.Context,
		*oracletypes.QueryFeedsRequest,
		...grpc.CallOption,
	) (*oracletypes.QueryFeedsResponse, error)
}

// ErrNoActiveFeeds indicates that the chain has intentionally disabled oracle
// voting by publishing an authoritative empty feed set.
var ErrNoActiveFeeds = errors.New("no active feeds")

func (v *Validator) loadActiveFeeds(ctx context.Context, timeout time.Duration) ([]string, error) {
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := v.feedClient.Feeds(
		requestCtx,
		&oracletypes.QueryFeedsRequest{},
		grpc.WaitForReady(true),
	)
	if err != nil {
		return nil, fmt.Errorf("querying chain feeds: %w", err)
	}
	if resp == nil {
		return nil, errors.New("chain feed response is nil")
	}
	if len(resp.Feeds.Denoms) == 0 {
		return nil, ErrNoActiveFeeds
	}
	if len(resp.Feeds.Denoms) > oracletypes.MaxFeeds {
		return nil, fmt.Errorf(
			"active feed count %d exceeds maximum %d",
			len(resp.Feeds.Denoms),
			oracletypes.MaxFeeds,
		)
	}

	activeFeeds := append([]string(nil), resp.Feeds.Denoms...)
	seenFeeds := make(map[string]struct{}, len(activeFeeds))
	for _, denom := range activeFeeds {
		if err := chain.ValidatePricedDenom(denom); err != nil {
			return nil, fmt.Errorf("invalid active feed %q: %w", denom, err)
		}
		if _, ok := seenFeeds[denom]; ok {
			return nil, fmt.Errorf("duplicate active feed %q", denom)
		}
		seenFeeds[denom] = struct{}{}
	}
	sort.Strings(activeFeeds)

	return activeFeeds, nil
}

func (v *Validator) samplePrices(ctx context.Context, activeFeeds []string) []string {
	resp, err := v.client.Prices(
		ctx,
		&api.PricesRequest{},
		grpc.WaitForReady(true),
	)
	if err != nil {
		v.logger.Error("failed to fetch oracle prices", "err", err)
		return allMissing(activeFeeds)
	}
	if resp == nil {
		v.logger.Error("oracle price response is nil")
		return allMissing(activeFeeds)
	}

	now := time.Now().UTC()
	age := now.Sub(resp.Timestamp)
	if resp.Timestamp.IsZero() || age > v.cfg.MaxResponseAge || age < -v.cfg.MaxFutureSkew {
		v.logger.Error(
			"oracle price response timestamp is invalid",
			"timestamp", resp.Timestamp.String(),
			"age", age.String(),
			"max_response_age", v.cfg.MaxResponseAge.String(),
			"max_future_skew", v.cfg.MaxFutureSkew.String(),
		)
		return allMissing(activeFeeds)
	}

	missing := make([]string, 0)
	for _, denom := range activeFeeds {
		rawPrice, ok := resp.Prices[denom]
		if !ok {
			v.logger.Error("oracle price is missing", "denom", denom)
			missing = append(missing, denom)
			continue
		}

		price, err := encoding.DecodeLegacyDec(rawPrice)
		if err != nil {
			v.logger.Error("oracle price is invalid", "denom", denom, "err", err)
			missing = append(missing, denom)
			continue
		}
		if !price.IsPositive() {
			v.logger.Error("oracle price is not positive", "denom", denom, "price", price.String())
			missing = append(missing, denom)
		}
	}

	return missing
}

func allMissing(feeds []string) []string {
	return append([]string(nil), feeds...)
}
