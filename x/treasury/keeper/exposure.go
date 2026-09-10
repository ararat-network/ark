package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/decimal"
	claimstypes "github.com/ararat-network/ark/x/claims/types"
	markettypes "github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	reservetypes "github.com/ararat-network/ark/x/reserve/types"
	"github.com/ararat-network/ark/x/treasury/types"
)

// blocksPerYear is the annualisation factor for the variance series, held as a
// Dec so the square root below takes one argument rather than composing two.
var blocksPerYear = math.LegacyNewDec(int64(chain.BlocksPerYear))

// maxReturn clamps volatility samples to [-1, 1], limiting feed discontinuities. Squared samples
// and convex EWMA weights keep variance in [0, 1], bounding annualised volatility.
var maxReturn = math.LegacyOneDec()

// sampleExposure updates risk series before conversion-total guards so idle blocks still decay flow
// and observe prices. Malformed totals fail subsequent validation and cannot commit the sample.
func (k Keeper) sampleExposure(ctx context.Context, totals markettypes.ConversionTotals) error {
	state, err := k.getExposureState(ctx)
	if err != nil {
		return err
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}

	if err := k.sampleVolatility(ctx, &state, params); err != nil {
		return err
	}
	if err := sampleFlow(&state, params, totals); err != nil {
		return err
	}

	if err := k.ExposureState.Set(ctx, state); err != nil {
		return fmt.Errorf("setting exposure state: %w", err)
	}
	return nil
}

// sampleVolatility skips unavailable reference observations without changing the anchor or
// recording false calm. A returning feed measures the move across the gap.
func (k Keeper) sampleVolatility(ctx context.Context, state *types.ExposureState, params types.Params) error {
	reference, err := k.oracleKeeper.GetReferenceDenom(ctx)
	if err != nil {
		return fmt.Errorf("getting protocol reference: %w", err)
	}
	if reference == "" {
		return nil
	}
	rates, err := k.oracleKeeper.GetAvailableRateSet(ctx, reference)
	if err != nil {
		return fmt.Errorf("getting reference rate for exposure sampling: %w", err)
	}
	price, ok := rates[reference]
	if !ok || price.IsNil() || !price.IsPositive() {
		return nil
	}

	previous := state.LastReferencePrice
	state.LastReferencePrice = price
	// The first sample of a chain's life, or the first after a reference move
	// that reset the anchor: a price with nothing to compare against yields no
	// return, and the next block measures from here.
	if previous.IsNil() || !previous.IsPositive() {
		return nil
	}

	ratio, err := decimal.Quo(price, previous)
	if err != nil {
		return fmt.Errorf("valuing the reference return: %w", err)
	}
	sample, err := decimal.Sub(ratio, math.LegacyOneDec())
	if err != nil {
		return fmt.Errorf("valuing the reference return: %w", err)
	}
	if sample.GT(maxReturn) {
		sample = maxReturn
	}
	if sample.LT(maxReturn.Neg()) {
		sample = maxReturn.Neg()
	}
	squared, err := decimal.Mul(sample, sample)
	if err != nil {
		return fmt.Errorf("squaring the reference return: %w", err)
	}

	variance, err := ewma(state.VolatilityVariance, squared, params.VolatilityDecay)
	if err != nil {
		return fmt.Errorf("folding the volatility series: %w", err)
	}
	state.VolatilityVariance = variance
	return nil
}

// sampleFlow tracks positive redemption value net of expansion in absolute NOAH. Liability
// normalisation occurs at refresh so the EWMA does not mix historical denominators.
func sampleFlow(state *types.ExposureState, params types.Params, totals markettypes.ConversionTotals) error {
	sample := math.LegacyZeroDec()
	if !totals.RedeemedValue.IsNil() && totals.RedeemedValue.IsPositive() {
		principal := math.LegacyZeroDec()
		if !totals.EligiblePrincipal.IsNil() {
			principal = math.LegacyNewDecFromInt(totals.EligiblePrincipal)
		}
		net, err := decimal.Sub(totals.RedeemedValue, principal)
		if err != nil {
			return fmt.Errorf("valuing net redemption flow: %w", err)
		}
		if net.IsPositive() {
			sample = net
		}
	}

	flow, err := ewma(state.FlowPressure, sample, params.FlowDecay)
	if err != nil {
		return fmt.Errorf("folding the flow series: %w", err)
	}
	state.FlowPressure = flow
	return nil
}

// ewma computes decay*previous + (1-decay)*sample. Validated decay in [0, 1) keeps the result
// within the inputs' convex hull.
func ewma(previous, sample, decay math.LegacyDec) (math.LegacyDec, error) {
	retained, err := decimal.Mul(previous, decay)
	if err != nil {
		return math.LegacyDec{}, err
	}
	weight, err := decimal.Sub(math.LegacyOneDec(), decay)
	if err != nil {
		return math.LegacyDec{}, err
	}
	contributed, err := decimal.Mul(sample, weight)
	if err != nil {
		return math.LegacyDec{}, err
	}
	return decimal.Add(retained, contributed)
}

// refreshExposure runs at cadence or retries an owed update from BeginBlock. Unavailable inputs
// retain the owed flag; store and codec faults propagate. Off-cadence blocks read only the flag.
func (k Keeper) refreshExposure(ctx context.Context) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("getting params: %w", err)
	}

	owed, err := k.ExposureRefreshPending.Get(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return fmt.Errorf("getting pending exposure refresh: %w", err)
	}
	if !owed {
		if !chain.IsPeriodLastBlock(ctx, params.ExposureRefreshPeriodBlocks) {
			return nil
		}
		if err := k.ExposureRefreshPending.Set(ctx, true); err != nil {
			return fmt.Errorf("recording pending exposure refresh: %w", err)
		}
	}

	applied, err := k.applyExposureRefresh(ctx, params)
	if err != nil {
		return err
	}
	if !applied {
		return nil
	}
	if err := k.ExposureRefreshPending.Set(ctx, false); err != nil {
		return fmt.Errorf("clearing pending exposure refresh: %w", err)
	}
	return nil
}

// applyExposureRefresh recomputes and stores the multiplier, reporting whether
// it could. A false return is an input the block could not supply, never a
// failure: the owed flag stays raised and the next block asks again.
func (k Keeper) applyExposureRefresh(ctx context.Context, params types.Params) (bool, error) {
	state, err := k.getExposureState(ctx)
	if err != nil {
		return false, err
	}

	partition, err := k.liabilityPartitionValue(ctx)
	if err != nil {
		if isUnusableRateInput(err) {
			k.Logger(ctx).Warn("skipping Treasury exposure refresh", "error", err)
			return false, nil
		}
		return false, err
	}
	// Use net liability, excluding Reserve-held paper that cannot claim redemption. Partial
	// valuation still feeds the model; missing information must not disable risk updates.
	net, err := partition.net()
	if err != nil {
		return false, err
	}

	policy, err := k.EconomicPolicy.Get(ctx)
	if err != nil {
		return false, fmt.Errorf("getting economic policy: %w", err)
	}

	circulating, err := k.circulatingNoah(ctx)
	if err != nil {
		return false, err
	}
	// No circulating NOAH is a chain with no denominator to measure against —
	// genesis before the first issuance, or every token held by the protocol
	// itself. Neither is a risk reading, so the multiplier holds.
	if !circulating.IsPositive() {
		return false, nil
	}

	ratio, err := decimal.Quo(net, math.LegacyNewDecFromInt(circulating))
	if err != nil {
		return false, fmt.Errorf("valuing the liability ratio: %w", err)
	}
	volatility, err := annualisedVolatility(state.VolatilityVariance)
	if err != nil {
		return false, err
	}
	// Flow becomes dimensionless here, against the same net basis the ratio
	// used, so both terms describe the same liability.
	flow := math.LegacyZeroDec()
	if net.IsPositive() {
		flow, err = decimal.Quo(state.FlowPressure, net)
		if err != nil {
			return false, fmt.Errorf("valuing flow pressure: %w", err)
		}
	}

	uncapped := composeMultiplier(policy, params.MultiplierCap, ratio, volatility, flow)
	previous := state.Multiplier
	if previous.IsNil() || previous.LT(math.LegacyOneDec()) {
		previous = math.LegacyOneDec()
	}
	multiplier, err := limitStep(previous, uncapped, params.MultiplierMaxStep)
	if err != nil {
		return false, err
	}
	if multiplier.GT(params.MultiplierCap) {
		multiplier = params.MultiplierCap
	}
	if multiplier.LT(math.LegacyOneDec()) {
		multiplier = math.LegacyOneDec()
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	state.LiabilityRatio = ratio
	state.FlowRatio = flow
	state.Multiplier = multiplier
	state.LastRefreshHeight = uint64(sdkCtx.BlockHeight())
	if err := k.ExposureState.Set(ctx, state); err != nil {
		return false, fmt.Errorf("setting exposure state: %w", err)
	}

	// Emitted on every completed refresh, an unchanged multiplier included: the
	// cadence is the liveness signal an indexer reads. No equality guard.
	if err := sdkCtx.EventManager().EmitTypedEvent(&types.EventExposureRefreshed{
		PreviousMultiplier:   previous,
		Multiplier:           multiplier,
		UncappedMultiplier:   uncapped,
		LiabilityRatio:       ratio,
		AnnualisedVolatility: volatility,
		FlowRatio:            flow,
	}); err != nil {
		return false, fmt.Errorf("emitting Treasury exposure update event: %w", err)
	}
	return true, nil
}

// composeMultiplier multiplies weighted indicator surcharges within the governance cap. Zero
// weights contribute one. Overflow saturates at the cap as a liveness backstop behind
// domain-bounded weights and indicators; see x/treasury/README.md.
func composeMultiplier(policy types.EconomicPolicy, ceiling math.LegacyDec, ratio, volatility, flow math.LegacyDec) math.LegacyDec {
	multiplier := math.LegacyOneDec()
	for _, term := range []struct {
		weight    math.LegacyDec
		indicator math.LegacyDec
	}{
		{policy.LiabilityRatioWeight, ratio},
		{policy.VolatilityWeight, volatility},
		{policy.FlowWeight, flow},
	} {
		if term.weight.IsZero() {
			continue
		}
		surcharge, err := decimal.Mul(term.weight, term.indicator)
		if err != nil {
			return ceiling
		}
		factor, err := decimal.Add(math.LegacyOneDec(), surcharge)
		if err != nil {
			return ceiling
		}
		multiplier, err = decimal.Mul(multiplier, factor)
		if err != nil {
			return ceiling
		}
	}
	return multiplier
}

// limitStep bounds one update's movement in either direction, so no single
// period can carry the multiplier far. It is what makes a one-period spike in
// any input unprofitable to induce: the target it would move is bounded, and
// the next period's decay has already begun eroding the spike.
func limitStep(previous, target, step math.LegacyDec) (math.LegacyDec, error) {
	if target.GT(previous) {
		ceiling, err := decimal.Add(previous, step)
		if err != nil {
			return math.LegacyDec{}, fmt.Errorf("limiting the exposure step: %w", err)
		}
		return math.LegacyMinDec(target, ceiling), nil
	}
	floor, err := decimal.Sub(previous, step)
	if err != nil {
		return math.LegacyDec{}, fmt.Errorf("limiting the exposure step: %w", err)
	}
	return math.LegacyMaxDec(target, floor), nil
}

// annualisedVolatility scales the per-block variance to a year and takes its
// root, in that order: one square root over the scaled figure rather than a
// root and a multiplication by a stored constant, which keeps the annualisation
// factor out of state and the error paths down to one.
func annualisedVolatility(variance math.LegacyDec) (math.LegacyDec, error) {
	if variance.IsNil() || !variance.IsPositive() {
		return math.LegacyZeroDec(), nil
	}
	scaled, err := decimal.Mul(variance, blocksPerYear)
	if err != nil {
		return math.LegacyDec{}, fmt.Errorf("annualising the volatility series: %w", err)
	}
	volatility, err := scaled.ApproxSqrt()
	if err != nil {
		return math.LegacyDec{}, fmt.Errorf("rooting the annualised variance: %w", err)
	}
	return volatility, nil
}

// circulatingNoah subtracts raw NOAH balances in protocol funds from total supply. Recognised
// capital is unsuitable because it includes external-asset credits absent from NOAH supply.
func (k Keeper) circulatingNoah(ctx context.Context) (math.Int, error) {
	circulating := k.bankKeeper.GetSupply(ctx, chain.NoahBaseDenom).Amount
	for _, moduleName := range []string{
		types.SubsidyPoolName,
		types.RedemptionBufferName,
		reservetypes.StrategicReserveName,
		claimstypes.InsuranceName,
	} {
		held := k.getBalance(ctx, moduleName)
		remaining, err := circulating.SafeSub(held)
		if err != nil {
			return math.Int{}, fmt.Errorf("netting %s NOAH from circulating supply: %w", moduleName, err)
		}
		circulating = remaining
	}
	return circulating, nil
}

// exposureAdjusted rounds a liability requirement upward by the current multiplier. Redemption
// payments and the model's own liability ratio use the raw basis to avoid exit rationing or
// feedback compounding.
func (k Keeper) exposureAdjusted(ctx context.Context, basis math.LegacyDec) (math.LegacyDec, error) {
	state, err := k.getExposureState(ctx)
	if err != nil {
		return math.LegacyDec{}, err
	}
	multiplier := state.Multiplier
	if multiplier.IsNil() || multiplier.LTE(math.LegacyOneDec()) {
		return basis, nil
	}
	if !basis.IsPositive() {
		return basis, nil
	}
	scaled, err := decimal.Mul(basis, multiplier)
	if err != nil {
		return math.LegacyDec{}, fmt.Errorf("scaling the liability basis by the exposure multiplier: %w", err)
	}
	return scaled, nil
}

// rescaleReferencePrice converts the stored price anchor by the reciprocal of a quantity's rebase:
// p * rate[to] / rate[from]. Convert therefore receives reversed denominations. Variance,
// multiplier, and NOAH flow are unit-independent; see x/treasury/README.md.
func (k Keeper) rescaleReferencePrice(ctx context.Context, from, to string, rates oracletypes.RateSet) error {
	state, err := k.getExposureState(ctx)
	if err != nil {
		return err
	}
	if state.LastReferencePrice.IsNil() || !state.LastReferencePrice.IsPositive() {
		return nil
	}
	if from == to {
		return nil
	}

	rebased, err := rates.Convert(sdk.NewDecCoinFromDec(to, state.LastReferencePrice), from)
	if err != nil {
		// Drop an unconvertible observational anchor rather than block the reference change. The
		// next usable price seeds the new unit before return sampling resumes.
		k.Logger(ctx).Warn(
			"dropping exposure reference anchor across reference move",
			"from", from,
			"to", to,
			"error", err,
		)
		state.LastReferencePrice = math.LegacyZeroDec()
	} else {
		state.LastReferencePrice = rebased.Amount
	}

	if err := k.ExposureState.Set(ctx, state); err != nil {
		return fmt.Errorf("setting rescaled exposure state: %w", err)
	}
	return nil
}

// getExposureState reads the stored state, answering the default for a store
// that has never held one. Genesis always writes it, so the fallback covers
// only a keeper reached before import — a test, or a query against an
// uninitialised chain — and answers with the inert state rather than an error.
func (k Keeper) getExposureState(ctx context.Context) (types.ExposureState, error) {
	state, err := k.ExposureState.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.DefaultExposureState(), nil
		}
		return types.ExposureState{}, fmt.Errorf("getting exposure state: %w", err)
	}
	return state, nil
}
