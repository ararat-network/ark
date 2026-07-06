package resolver

import (
	"context"
	"math/big"
	"slices"
	"sync"

	oraclemetrics "noah/oracle/sidecar/metrics"
	"noah/oracle/sidecar/types"
)

// Resolver aggregates provider pair prices and resolves them into final
// vote-target pair prices.
type Resolver struct {
	mtx sync.Mutex

	cfg         Config
	pairPrices  map[types.Pair][]*big.Float
	finalPrices types.Prices
}

// NewResolver returns a new price resolver.
func NewResolver(cfg Config) (*Resolver, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	r := &Resolver{
		cfg:         cfg,
		pairPrices:  make(map[types.Pair][]*big.Float),
		finalPrices: make(types.Prices),
	}

	return r, nil
}

// SetProviderPrices stores one provider's positive pair prices for the next
// resolution tick.
func (r *Resolver) SetProviderPrices(provider string, data types.Prices) {
	r.mtx.Lock()
	defer r.mtx.Unlock()

	ctx := context.Background()
	for pair, price := range data {
		if price == nil || price.Sign() != 1 {
			continue
		}

		copied := new(big.Float).Copy(price)
		r.pairPrices[pair] = append(r.pairPrices[pair], copied)

		floatPrice, _ := copied.Float64()
		oraclemetrics.RecordProviderPrice(ctx, provider, pair.String(), floatPrice)
	}
}

// ResolvePrices commits the latest final pair prices. It first builds provider
// medians per observed pair, then resolves only requested denoms. Configured
// routes are averaged; missing or empty routes use the direct ARK/QUOTE path.
func (r *Resolver) ResolvePrices(denoms []string) {
	r.mtx.Lock()
	defer r.mtx.Unlock()

	ctx := context.Background()

	medianPrices := make(types.Prices)
	for pair, prices := range r.pairPrices {
		if len(prices) == 0 {
			continue
		}

		medianPrices[pair] = calculateMedian(prices)
		oraclemetrics.RecordPairSampleCount(ctx, pair.String(), len(prices))
	}

	voteTargets := make(map[types.Pair][]*big.Float)

	for _, denom := range denoms {
		output, routes, ok := r.cfg.RoutesForDenom(denom)
		if !ok {
			continue
		}
		for _, route := range routes {
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

	r.finalPrices = recordFinalPrices(ctx, finalPrices)
}

// GetPrices returns a copy of the last committed final pair prices.
func (r *Resolver) GetPrices() types.Prices {
	r.mtx.Lock()
	defer r.mtx.Unlock()

	finalPrices := make(types.Prices)
	for denom, price := range r.finalPrices {
		finalPrices[denom] = new(big.Float).Copy(price)
	}

	return finalPrices
}

// Update replaces the resolver config and clears current provider observations
// and final prices. The caller must validate cfg before calling Update.
func (r *Resolver) Update(cfg Config) {
	r.mtx.Lock()
	defer r.mtx.Unlock()

	r.cfg = cfg
	r.pairPrices = make(map[types.Pair][]*big.Float)
	r.finalPrices = make(types.Prices)
}

// Reset clears current provider observations while preserving the last committed
// final prices.
func (r *Resolver) Reset() {
	r.mtx.Lock()
	defer r.mtx.Unlock()

	r.pairPrices = make(map[types.Pair][]*big.Float)
}

// resolveRoutePrice multiplies route step prices, accepting either the
// configured step pair or its inverse when only reciprocal provider data is
// available.
func resolveRoutePrice(prices types.Prices, steps []types.Pair) (*big.Float, bool) {
	finalPrice := new(big.Float).SetInt64(1)
	for _, pair := range steps {
		price, ok := prices[pair]
		if !ok || price == nil {
			inverse := pair.Inverse()
			price, ok = prices[inverse]
			if !ok || price == nil || price.Sign() != 1 {
				return nil, false
			}

			price = new(big.Float).Quo(new(big.Float).SetInt64(1), price)
		}
		finalPrice.Mul(finalPrice, price)
	}

	return finalPrice, true
}

// recordFinalPrices stores a defensive copy of final prices and records the
// aggregate-price metric for each committed pair.
func recordFinalPrices(ctx context.Context, prices types.Prices) types.Prices {
	finalPrices := make(types.Prices, len(prices))
	for pair, price := range prices {
		if price == nil {
			continue
		}

		copied := new(big.Float).Copy(price)
		finalPrices[pair] = copied

		floatPrice, _ := copied.Float64()
		oraclemetrics.RecordAggregatePrice(ctx, pair.String(), floatPrice)
	}

	return finalPrices
}

// calculateAverage returns the average of non-nil values.
func calculateAverage(values []*big.Float) *big.Float {
	if len(values) == 0 {
		return nil
	}

	sum := new(big.Float)
	count := uint64(0)
	for _, value := range values {
		if value == nil {
			continue
		}
		sum.Add(sum, value)
		count++
	}
	if count == 0 {
		return nil
	}

	return sum.Quo(sum, new(big.Float).SetUint64(count))
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
		return new(big.Float).Copy(values[mid])
	}

	median := new(big.Float).Add(values[mid-1], values[mid])
	return median.Quo(median, new(big.Float).SetUint64(2))
}
