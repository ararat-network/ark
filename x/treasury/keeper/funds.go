package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	"ark/pkg/decimal"
	markettypes "ark/x/market/types"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/types"
)

type fundStatus struct {
	bufferBalance         math.Int
	bufferTarget          math.Int
	reserveBalance        math.Int
	reserveTarget         math.Int
	insuranceBalance      math.Int
	insuranceReserved     math.Int
	insuranceUnencumbered math.Int
	insuranceTarget       math.Int
}

// RouteExpansion derives and executes the complete expansion-principal
// waterfall from Market's escrow and returns the NOAH Market must burn: the
// quote spread plus whatever principal overflowed every funded target.
// Treasury itself neither mints nor burns.
//
// The credit split behind that total stays execution-local and reaches
// observers through EventExpansionAllocated rather than the caller. Market owns
// burn, mint, and payout, never a decision about which fund was short; handing
// it the split would invite settlement to branch on Treasury policy. The total
// is exactly the residue left in Market's account once the credits are sent, so
// burning it settles the escrow rather than acting on that policy.
//
// The stable output is whatever Market's ask leg produced, so it is already
// ACTIVE: eligibility is decided at quote time and nothing outside Market can
// hold a quote and settle it.
func (k Keeper) RouteExpansion(
	ctx context.Context,
	grossOffer sdk.Coin,
	stableOutput sdk.Coin,
	quoteRates oracletypes.RateSet,
) (sdk.Coin, error) {
	if err := validatePositiveNoahCoin(grossOffer); err != nil {
		return sdk.Coin{}, fmt.Errorf("invalid gross offer: %w", err)
	}
	if err := stableOutput.Validate(); err != nil {
		return sdk.Coin{}, fmt.Errorf("invalid stable output: %w", err)
	}
	if !stableOutput.IsPositive() {
		return sdk.Coin{}, fmt.Errorf("stable output must be positive: %s", stableOutput)
	}

	convertedOutput, err := quoteRates.Convert(sdk.NewDecCoinFromCoin(stableOutput), chain.NoahBaseDenom)
	if err != nil {
		return sdk.Coin{}, fmt.Errorf("valuing stable output: %w", err)
	}
	eligible := convertedOutput.Amount.TruncateInt()
	if eligible.GT(grossOffer.Amount) {
		return sdk.Coin{}, fmt.Errorf(
			"stable output value %s exceeds gross offer %s",
			sdk.NewCoin(chain.NoahBaseDenom, eligible),
			grossOffer,
		)
	}
	spread := grossOffer.Amount.Sub(eligible)

	liabilityNoah, complete, err := k.cachedLiabilityValue(ctx, quoteRates)
	if err != nil {
		return sdk.Coin{}, err
	}
	if complete {
		liabilityNoah, err = decimal.Add(liabilityNoah, convertedOutput.Amount)
		if err != nil {
			return sdk.Coin{}, fmt.Errorf("adding stable output to aggregate liability: %w", err)
		}
	}

	// Targets derive from a complete valuation only, which is why the waterfall
	// runs solely under the branch below: there the claimable aggregate and full
	// outstanding exposure are the same number. They part company the moment
	// some supply cannot be valued, and then they answer opposite questions
	// about it — coverage asks what can show up and claim, so unclaimable supply
	// is rightly excluded, while a target asks what capital is owed against an
	// obligation, and a suspended or unpriced asset is still an obligation, the
	// kind Insurance and the Reserve exist for. Sizing a target off the
	// claimable figure would call for less capital exactly when an asset has
	// just failed.
	//
	// The exposure a target would need is exactly what could not be valued, so
	// there is no honest basis to size one against. The whole eligible principal
	// therefore parks in the Buffer rather than overflowing into a burn sized
	// off an understated exposure. The asymmetry in the errors decides it:
	// under-crediting Reserve and Insurance here is temporary, because their
	// gaps persist and later expansions fill them, while burning principal we
	// only thought was surplus is not.
	bufferCredit := eligible
	reserveCredit := math.ZeroInt()
	insuranceCredit := math.ZeroInt()
	overflowBurn := math.ZeroInt()
	if complete {
		status, err := k.calculateFundStatus(ctx, liabilityNoah)
		if err != nil {
			return sdk.Coin{}, err
		}
		bufferGap := shortfall(status.bufferTarget, status.bufferBalance)
		reserveGap := shortfall(status.reserveTarget, status.reserveBalance)
		insuranceGap := shortfall(status.insuranceTarget, status.insuranceUnencumbered)
		remaining := eligible
		bufferCredit = math.MinInt(remaining, bufferGap)
		remaining = remaining.Sub(bufferCredit)
		reserveCredit = math.MinInt(remaining, reserveGap)
		remaining = remaining.Sub(reserveCredit)
		insuranceCredit = math.MinInt(remaining, insuranceGap)
		remaining = remaining.Sub(insuranceCredit)
		overflowBurn = remaining
	}

	credits := []struct {
		module string
		amount math.Int
	}{
		{types.RedemptionBufferName, bufferCredit},
		{types.StrategicReserveName, reserveCredit},
		{types.InsuranceName, insuranceCredit},
	}
	for _, credit := range credits {
		if credit.amount.IsZero() {
			continue
		}
		if err := k.bankKeeper.SendCoinsFromModuleToModule(
			ctx,
			markettypes.ModuleName,
			credit.module,
			sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, credit.amount)),
		); err != nil {
			return sdk.Coin{}, fmt.Errorf("crediting %s: %w", credit.module, err)
		}
	}

	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventExpansionAllocated{
		Denom:                   chain.NoahBaseDenom,
		RedemptionBufferCredit:  bufferCredit,
		StrategicReserveCredit:  reserveCredit,
		InsuranceCredit:         insuranceCredit,
		SpreadAndDustBurn:       spread,
		OverflowBurn:            overflowBurn,
		TargetValuationComplete: complete,
	}); err != nil {
		return sdk.Coin{}, fmt.Errorf("emitting Treasury expansion allocation event: %w", err)
	}

	return sdk.NewCoin(chain.NoahBaseDenom, spread.Add(overflowBurn)), nil
}

// DrawRedemptionBuffer funds the current Buffer coverage share of the quoted
// NOAH output using pre-burn claimable liability and Buffer state.
//
// The redeemed denomination arrives already recognised as liability: Market
// converts out of priced-live assets only, and routes a suspended asset through
// settlement, which requires an activated plan. Neither path can reach here with
// a denomination Treasury does not carry.
//
// The draw runs whether or not valuation is complete. The claimable aggregate
// excludes exactly the supply that cannot currently redeem — a member whose
// feed is stale cannot quote, and suspension without an activated plan closes
// both exits — so nothing the excluded supply will later claim is being spent,
// and the exits still open are the ones the Buffer exists to dampen. A
// suspension elsewhere therefore raises coverage for the healthy exits instead
// of switching the Buffer off during exactly the contagion it was built for;
// the completeness flag is audit disclosure, never a kill switch.
//
// The draw returns the payment alone. The liability figures behind the share,
// and the completeness flag itself, stay execution-local and reach observers
// through EventRedemptionBufferDrawn rather than the caller: handing settlement
// a completeness flag would invite it to branch on valuation state, which is
// the coupling the claimable-liability denominator exists to remove.
func (k Keeper) DrawRedemptionBuffer(
	ctx context.Context,
	redeemedStable sdk.Coin,
	noahOutput math.Int,
	quoteRates oracletypes.RateSet,
) (math.Int, error) {
	if err := redeemedStable.Validate(); err != nil {
		return math.Int{}, fmt.Errorf("invalid redeemed stable coin: %w", err)
	}
	if !redeemedStable.IsPositive() {
		return math.Int{}, fmt.Errorf("redeemed stable coin must be positive: %s", redeemedStable)
	}
	if noahOutput.IsNil() || !noahOutput.IsPositive() {
		return math.Int{}, fmt.Errorf("NOAH output must be set and positive")
	}

	convertedRedemption, err := quoteRates.Convert(sdk.NewDecCoinFromCoin(redeemedStable), chain.NoahBaseDenom)
	if err != nil {
		return math.Int{}, fmt.Errorf("valuing redeemed stable coin: %w", err)
	}
	redeemedNoah := convertedRedemption.Amount
	noahOutputInt := math.LegacyNewDecFromInt(noahOutput)
	if noahOutputInt.GT(redeemedNoah) {
		return math.Int{}, fmt.Errorf(
			"NOAH output %s exceeds redeemed liability %s",
			noahOutput,
			redeemedNoah,
		)
	}

	liabilityNoah, complete, err := k.cachedLiabilityValue(ctx, quoteRates)
	if err != nil {
		return math.Int{}, err
	}
	// The redeemed denomination is claimable by definition — it just quoted —
	// so its full supply is in the aggregate and this bound holds for every
	// redemption Market can produce. Failing it means the snapshot and the
	// quote disagree about state, which no draw should paper over.
	if redeemedNoah.GT(liabilityNoah) {
		return math.Int{}, fmt.Errorf("redeemed liability %s exceeds claimable liability %s", redeemedNoah, liabilityNoah)
	}

	bufferBalance := k.balance(ctx, types.RedemptionBufferName)
	bufferNoah := math.LegacyNewDecFromInt(bufferBalance)
	coverage := math.LegacyOneDec()
	if bufferNoah.LT(liabilityNoah) {
		coverage, err = decimal.Quo(bufferNoah, liabilityNoah)
		if err != nil {
			return math.Int{}, fmt.Errorf("calculating Buffer coverage: %w", err)
		}
	}
	coveredOutput, err := decimal.Mul(noahOutputInt, coverage)
	if err != nil {
		return math.Int{}, fmt.Errorf("calculating Buffer-funded output: %w", err)
	}
	bufferPaid := coveredOutput.TruncateInt()
	if bufferPaid.IsPositive() {
		if err := k.bankKeeper.SendCoinsFromModuleToModule(
			ctx,
			types.RedemptionBufferName,
			markettypes.ModuleName,
			sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, bufferPaid)),
		); err != nil {
			return math.Int{}, fmt.Errorf("drawing redemption buffer: %w", err)
		}
	}

	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventRedemptionBufferDrawn{
		Denom:                      chain.NoahBaseDenom,
		Payment:                    bufferPaid,
		AggregateValuationComplete: complete,
	}); err != nil {
		return math.Int{}, fmt.Errorf("emitting Treasury redemption buffer event: %w", err)
	}
	return bufferPaid, nil
}

func (k Keeper) calculateFundStatus(ctx context.Context, liabilityNoah math.LegacyDec) (fundStatus, error) {
	policy, err := k.MonetaryPolicy.Get(ctx)
	if err != nil {
		return fundStatus{}, fmt.Errorf("getting monetary policy: %w", err)
	}
	insuranceReserved, err := k.InsuranceReserved.Get(ctx)
	if err != nil {
		return fundStatus{}, fmt.Errorf("getting Insurance reservation: %w", err)
	}
	bufferBalance := k.balance(ctx, types.RedemptionBufferName)
	reserveBalance := k.balance(ctx, types.StrategicReserveName)
	insuranceBalance := k.balance(ctx, types.InsuranceName)
	insuranceUnencumbered := insuranceBalance.Sub(insuranceReserved)
	bufferTarget := policy.RedemptionBufferTargetRatio.MulRoundUp(liabilityNoah).Ceil().TruncateInt()
	reserveTarget := policy.StrategicReserveTargetRatio.MulRoundUp(liabilityNoah).Ceil().TruncateInt()
	insuranceTarget := policy.InsuranceTargetRatio.MulRoundUp(liabilityNoah).Ceil().TruncateInt()
	return fundStatus{
		bufferBalance:         bufferBalance,
		bufferTarget:          bufferTarget,
		reserveBalance:        reserveBalance,
		reserveTarget:         reserveTarget,
		insuranceBalance:      insuranceBalance,
		insuranceReserved:     insuranceReserved,
		insuranceUnencumbered: insuranceUnencumbered,
		insuranceTarget:       insuranceTarget,
	}, nil
}

func (k Keeper) balance(ctx context.Context, moduleName string) math.Int {
	addr := k.accountKeeper.GetModuleAddress(moduleName)
	return k.bankKeeper.GetBalance(ctx, addr, chain.NoahBaseDenom).Amount
}

func validatePositiveNoahCoin(coin sdk.Coin) error {
	if err := coin.Validate(); err != nil {
		return err
	}
	if coin.Denom != chain.NoahBaseDenom || !coin.IsPositive() {
		return fmt.Errorf("coin must be positive %s", chain.NoahBaseDenom)
	}
	return nil
}
