package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "github.com/ararat-network/ark/pkg/chain"
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

// maxReturn bounds one volatility sample. A block-to-block return outside
// [-1, 1] is a rate discontinuity rather than a market move — a feed returning
// after an outage, or a denomination redefined underneath the reference — and
// admitting it would let one such event dominate a series that is supposed to
// describe ordinary variation. Clamping also bounds the variance series to
// [0, 1] by induction, which is what keeps annualised volatility inside the
// representable domain without a further check.
var maxReturn = math.LegacyOneDec()

// sampleExposure folds this block's observations into the two running series.
// It runs first in SettleConversions, before the totals are validated or
// tested for emptiness: an idle block still decays flow toward zero and still
// records a price, so the series describe elapsed time rather than elapsed
// activity, and a chain that stops converting does not freeze its risk estimate
// at whatever the last busy block saw.
//
// The totals are read but never trusted for arithmetic beyond a subtraction
// bounded at zero. Validate runs immediately after and fails the block on
// anything malformed, so the fold cannot act on a figure settlement then
// rejects.
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

// sampleVolatility folds one squared return into the variance series.
//
// A reference the Oracle cannot price this block is skipped rather than
// treated as a zero return: an absent rate is absent evidence, and recording
// calm because the feed went dark would understate risk during exactly the
// outage that suggests it. The stored price is left untouched, so the return
// resumes across the gap when the feed returns — measuring the move that
// actually happened rather than pretending the gap did not.
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

// sampleFlow folds this block's net redemption value into the flow series.
//
// Only the redemption side counts, and only its excess over expansion: what
// the indicator is for is one-directional pressure, and a block whose
// expansions matched its redemptions exerted none. The figure stays absolute
// NOAH rather than a ratio, because dividing here would fix it against the
// liability of the block it was sampled in, and a series spanning hours would
// then mix denominators. The division happens once, at application.
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

// ewma folds one sample into a running series: decay * previous + (1 - decay) *
// sample. Validation keeps decay in [0, 1), so both weights are non-negative
// and sum to one, and the result stays inside the convex hull of the two
// inputs — which is what bounds the variance series once its samples are
// clamped.
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

// refreshExposure recomputes the multiplier when the cadence is due or an
// earlier attempt is owed. It runs from BeginBlocker, after the block's other
// Treasury work, and reads one bool on every block that is neither.
//
// An update that cannot be computed raises the owed flag and returns: the
// cadence boundary is an instant, so a period that passes while liability
// cannot be valued would otherwise be forgiven and the multiplier would hold a
// figure from before the conditions that made valuation fail. Only inputs are
// treated this way. A store or codec fault still fails the block.
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
	// The net basis, matching the waterfall: paper the Reserve holds raises no
	// claim, so counting it would report leverage the protocol does not carry.
	// An incomplete partition is used as it stands rather than skipped — it
	// understates exposure, which understates the multiplier, and a risk model
	// that switched off during partial information would be blindest exactly
	// when an asset has just failed.
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

// composeMultiplier folds the three surcharges into one factor. The weights
// arrive from EconomicPolicy and the saturation ceiling from Params, which is
// the split that makes the delegation safe: the committee sets how much each
// measurement is worth, and governance alone sets how far the answer may go.
//
// Each term is
// one plus a weighted indicator, so a zero weight contributes exactly one and
// switches its indicator off without disturbing the others; all three zero
// gives one, which is the launch configuration and reproduces unscaled targets.
//
// It returns no error, deliberately. Arithmetic leaving the representable
// domain saturates at the cap rather than failing: the caller is a
// BeginBlocker, where a checked error and a panic are the same outcome — the
// block fails — and the honest reading of an
// unrepresentable composite is that risk is past anything the model can
// measure, which is what the cap already means. The parameter domain caps make
// this unreachable in practice: three weights bounded by MaxExposureWeight
// against indicators bounded by supply, one, and the annualisation constant
// cannot approach the Dec limit. It is kept as the loud backstop behind them.
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

// circulatingNoah is total NOAH supply less every balance held by a protocol
// fund. The four accounts hold NOAH that no one can sell, so counting it would
// overstate the market capitalisation the liability ratio measures against and
// report the protocol as less levered than it is.
//
// Raw balances, deliberately, where the waterfall asks each fund what it
// recognises: recognised capital includes haircut credit for external assets
// the Reserve holds (D58), which is not circulating NOAH and would subtract
// something that was never in the supply figure to begin with.
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

// exposureAdjusted scales a liability basis by the current multiplier. It is
// the one entry point every requirement-family consumer uses, so what the
// multiplier reaches is decided here rather than at each call site.
//
// Two consumers deliberately do not call it. The redemption draw divides by the
// raw basis, because scaling a payment denominator rations the exits the Buffer
// exists to fund (D73), and the multiplier's own liability ratio reads the raw
// basis, because a controller feeding its output back into its input would
// compound.
//
// Rounding is up, per the direction rule: this figure sizes a requirement, and
// a requirement that rounds down asks for less capital than the model called
// for.
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

// rescaleReferencePrice re-expresses the stored reference price when governance
// re-points the protocol reference. Without it the next block would read a
// price in the new unit against an anchor in the old one and record the unit
// change as a market move — a spurious return whose size is the cross rate
// itself, which is exactly the kind of discontinuity the sample clamp exists to
// blunt and should never be asked to absorb.
//
// The anchor is a price — NOAH per one unit of the outgoing reference — not a
// quantity of that unit, so it moves by the reciprocal of the factor a
// quantity moves by: p × rate[to] / rate[from] (D76). Convert computes exactly
// that when its arguments are given in the opposite order to this function's,
// which is why they are reversed below rather than by mistake. The reversal is
// the whole operation, and straightening it out would record the square of the
// cross rate as a market move.
//
// The three other stored figures need no adjustment: variance and the
// multiplier are dimensionless, and flow pressure is NOAH-valued, which no
// reference move touches.
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
		// The anchor is dropped rather than the transition failed: a reference
		// move is governance re-pointing the unit every rate is quoted in, and
		// refusing it because one risk-model anchor could not be converted
		// would let an observational series veto an economic decision. The next
		// block records a price in the new unit and the series resumes one
		// sample later.
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
