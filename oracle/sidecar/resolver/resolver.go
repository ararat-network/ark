package resolver

import (
	"context"
	"math/big"
	"slices"
	"time"

	oraclemetrics "ark/oracle/sidecar/metrics"
	"ark/oracle/sidecar/types"
)

// ResolvePrices returns final pair prices for one complete provider snapshot. It
// builds provider medians for the required route pairs after normalising
// reciprocal observations, then uses active bootstrap prices only for route
// pairs without provider samples. It resolves only requested denoms. Configured
// routes are averaged; missing or empty routes use the direct NOAH/QUOTE path.
func ResolvePrices(
	ctx context.Context,
	cfg Config,
	providerPrices map[string]types.Prices,
	denoms []string,
	now time.Time,
) types.Prices {
	recordProviderPrices(ctx, providerPrices)

	medianPrices := make(types.Prices)
	resolvedPairs := make(map[types.Pair]struct{})
	voteTargets := make(map[types.Pair][]*big.Float)

	for _, denom := range denoms {
		output, routes, ok := cfg.RoutesForDenom(denom)
		if !ok {
			continue
		}
		for _, route := range routes {
			for _, pair := range route.Pairs {
				if _, ok := resolvedPairs[pair]; ok {
					continue
				}
				resolvedPairs[pair] = struct{}{}

				samples := providerSamples(providerPrices, pair)
				if len(samples) == 0 {
					bootstrapPrice, ok := cfg.bootstrapPrice(pair, now)
					if !ok {
						continue
					}
					medianPrices[pair] = bootstrapPrice
					oraclemetrics.RecordBootstrapPriceUse(ctx, pair.String())
					continue
				}
				medianPrices[pair] = calculateMedian(samples)
				oraclemetrics.RecordPairSampleCount(ctx, pair.String(), len(samples))
			}

			finalPrice, ok := resolveRoutePrice(medianPrices, route.Pairs)
			if !ok {
				continue
			}
			voteTargets[output] = append(voteTargets[output], finalPrice)
			floatPrice, _ := finalPrice.Float64()
			oraclemetrics.RecordRoutePrice(ctx, output.String(), route.Name, floatPrice)
		}
	}

	finalPrices := make(types.Prices)
	for pair, prices := range voteTargets {
		if len(prices) == 0 {
			continue
		}
		finalPrices[pair] = calculateAverage(prices)
		oraclemetrics.RecordResolvedSourceCount(ctx, pair.String(), len(prices))
	}

	return recordFinalPrices(ctx, finalPrices)
}

// bootstrapPrice returns an active configured price for pair. Config validation
// guarantees parseability; the checks remain defensive because ResolvePrices is
// also directly callable in tests and other package code.
func (c Config) bootstrapPrice(pair types.Pair, now time.Time) (*big.Float, bool) {
	for _, bootstrap := range c.BootstrapPrices {
		if bootstrap.Pair != pair {
			continue
		}

		validUntil, err := time.Parse(time.RFC3339, bootstrap.ValidUntil)
		if err != nil || !now.Before(validUntil) {
			return nil, false
		}
		price, err := types.ParsePrice(bootstrap.Price)
		if err != nil || !validPrice(price) {
			return nil, false
		}

		return copyPrice(price), true
	}

	return nil, false
}

// providerSamples returns at most one price per provider normalised to pair's
// orientation. A direct observation takes precedence when a provider exposes
// both orientations.
func providerSamples(providerPrices map[string]types.Prices, pair types.Pair) []*big.Float {
	samples := make([]*big.Float, 0, len(providerPrices))
	for _, prices := range providerPrices {
		if price := prices[pair]; validPrice(price) {
			samples = append(samples, copyPrice(price))
			continue
		}

		inverse := prices[pair.Inverse()]
		if !validPrice(inverse) {
			continue
		}
		price := newPriceFloat().Quo(newPriceFloat().SetInt64(1), inverse)
		if validPrice(price) {
			samples = append(samples, price)
		}
	}

	return samples
}

func recordProviderPrices(ctx context.Context, providerPrices map[string]types.Prices) {
	for provider, prices := range providerPrices {
		for pair, price := range prices {
			if !validPrice(price) {
				continue
			}

			floatPrice, _ := price.Float64()
			oraclemetrics.RecordProviderPrice(ctx, provider, pair.String(), floatPrice)
		}
	}
}

// resolveRoutePrice multiplies normalised median prices for each route step.
func resolveRoutePrice(prices types.Prices, steps []types.Pair) (*big.Float, bool) {
	var finalPrice *big.Float
	for _, pair := range steps {
		price, ok := prices[pair]
		if !ok || !validPrice(price) {
			return nil, false
		}
		if finalPrice == nil {
			finalPrice = copyPrice(price)
			continue
		}
		finalPrice = newPriceFloat().Mul(finalPrice, price)
		if !validPrice(finalPrice) {
			return nil, false
		}
	}

	return finalPrice, finalPrice != nil
}

// recordFinalPrices returns a defensive copy and records the aggregate-price
// metric for each resolved pair.
func recordFinalPrices(ctx context.Context, prices types.Prices) types.Prices {
	finalPrices := make(types.Prices, len(prices))
	for pair, price := range prices {
		if !validPrice(price) {
			continue
		}

		copied := copyPrice(price)
		finalPrices[pair] = copied

		floatPrice, _ := copied.Float64()
		oraclemetrics.RecordAggregatePrice(ctx, pair.String(), floatPrice)
	}

	return finalPrices
}

// calculateAverage returns the average of finite positive values.
func calculateAverage(values []*big.Float) *big.Float {
	if len(values) == 0 {
		return nil
	}

	sum := newPriceFloat()
	count := uint64(0)
	for _, value := range values {
		if !validPrice(value) {
			continue
		}
		sum.Add(sum, value)
		count++
	}
	if count == 0 {
		return nil
	}

	return newPriceFloat().Quo(sum, newPriceFloat().SetUint64(count))
}

// calculateMedian sorts values in place and returns the median. It returns an
// average if the number of values is even.
func calculateMedian(values []*big.Float) *big.Float {
	if len(values) == 0 {
		return nil
	}

	slices.SortFunc(values, func(a, b *big.Float) int {
		return a.Cmp(b)
	})

	mid := len(values) / 2
	if len(values)%2 == 1 {
		return copyPrice(values[mid])
	}

	median := newPriceFloat().Add(values[mid-1], values[mid])
	return newPriceFloat().Quo(median, newPriceFloat().SetUint64(2))
}

func validPrice(price *big.Float) bool {
	return price != nil && price.Sign() == 1 && types.ValidatePrice(price) == nil
}

func copyPrice(price *big.Float) *big.Float {
	return newPriceFloat().Set(price)
}

func newPriceFloat() *big.Float {
	return new(big.Float).SetPrec(types.PricePrecisionBits)
}
