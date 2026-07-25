package validation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"google.golang.org/grpc"

	"ark/oracle/types"
	"ark/pkg/chain"
	"ark/pkg/encoding"
	oracletypes "ark/x/oracle/types"
)

// PriceClient is the public oracle price API used by validation.
type PriceClient interface {
	Prices(context.Context, *types.OraclePricesRequest, ...grpc.CallOption) (*types.OraclePricesResponse, error)
}

// VoteTargetClient is the on-chain oracle query used to load active denoms.
type VoteTargetClient interface {
	VoteTargets(
		context.Context,
		*oracletypes.QueryVoteTargetsRequest,
		...grpc.CallOption,
	) (*oracletypes.QueryVoteTargetsResponse, error)
}

// ErrNoActiveVoteTargets indicates that the chain has intentionally disabled
// oracle voting by publishing an authoritative empty target snapshot.
var ErrNoActiveVoteTargets = errors.New("no active vote targets")

func (v *Validator) loadActiveDenoms(ctx context.Context, timeout time.Duration) ([]string, error) {
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resp, err := v.voteTargetClient.VoteTargets(
		requestCtx,
		&oracletypes.QueryVoteTargetsRequest{},
		grpc.WaitForReady(true),
	)
	if err != nil {
		return nil, fmt.Errorf("querying chain vote targets: %w", err)
	}
	if resp == nil {
		return nil, errors.New("chain vote target response is nil")
	}
	if len(resp.VoteTargets) == 0 {
		return nil, ErrNoActiveVoteTargets
	}
	if len(resp.VoteTargets) > oracletypes.MaxVoteTargets {
		return nil, fmt.Errorf(
			"active vote target count %d exceeds maximum %d",
			len(resp.VoteTargets),
			oracletypes.MaxVoteTargets,
		)
	}

	activeDenoms := append([]string(nil), resp.VoteTargets...)
	seenDenoms := make(map[string]struct{}, len(activeDenoms))
	for _, denom := range activeDenoms {
		if err := chain.ValidateNativeBaseDenom(denom); err != nil {
			return nil, fmt.Errorf("invalid active denom %q: %w", denom, err)
		}
		if _, ok := seenDenoms[denom]; ok {
			return nil, fmt.Errorf("duplicate active denom %q", denom)
		}
		seenDenoms[denom] = struct{}{}
	}
	sort.Strings(activeDenoms)

	return activeDenoms, nil
}

func (v *Validator) samplePrices(ctx context.Context, activeDenoms []string) []string {
	resp, err := v.client.Prices(
		ctx,
		&types.OraclePricesRequest{},
		grpc.WaitForReady(true),
	)
	if err != nil {
		v.logger.Error("failed to fetch oracle prices", "err", err)
		return allMissing(activeDenoms)
	}
	if resp == nil {
		v.logger.Error("oracle price response is nil")
		return allMissing(activeDenoms)
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
		return allMissing(activeDenoms)
	}

	missing := make([]string, 0)
	for _, denom := range activeDenoms {
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

func allMissing(denoms []string) []string {
	return append([]string(nil), denoms...)
}
