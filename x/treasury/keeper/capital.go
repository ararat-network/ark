package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	"ark/pkg/decimal"
	claimstypes "ark/x/claims/types"
	markettypes "ark/x/market/types"
	oracletypes "ark/x/oracle/types"
	reservetypes "ark/x/reserve/types"
	"ark/x/treasury/types"
)

type fundStatus struct {
	bufferBalance    math.Int
	bufferTarget     math.Int
	reserveBalance   math.Int
	reserveTarget    math.Int
	insuranceBalance math.Int
	insuranceTarget  math.Int
}

// RouteExpansion derives and executes the expansion-principal waterfall from
// Market's escrow and returns the NOAH Market must burn: the quote spread plus
// whatever principal overflowed every funded target. The credit split behind
// that total stays execution-local and reaches observers through
// EventExpansionAllocated rather than the caller, because handing Market the
// split would invite settlement to branch on Treasury policy. The stable output
// is whatever Market's ask leg produced, so it is already ACTIVE.
func (k Keeper) RouteExpansion(
	ctx context.Context,
	grossOffer sdk.Coin,
	stableOutput sdk.Coin,
	quoteRates oracletypes.RateSet,
) (sdk.Coin, error) {
	if err := grossOffer.Validate(); err != nil {
		return sdk.Coin{}, fmt.Errorf("invalid gross offer: %w", err)
	}
	if grossOffer.Denom != chain.NoahBaseDenom || !grossOffer.IsPositive() {
		return sdk.Coin{}, fmt.Errorf("invalid gross offer: coin must be positive %s", chain.NoahBaseDenom)
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
			chain.NoahCoin(eligible),
			grossOffer,
		)
	}
	spread := grossOffer.Amount.Sub(eligible)

	liabilityNoah, complete, err := k.cachedLiabilityValue(ctx, quoteRates)
	if err != nil {
		return sdk.Coin{}, err
	}

	// Targets derive from a complete valuation only, so the waterfall runs
	// solely under the branch below: an incomplete aggregate either omits failed
	// supply outright or carries it on a rate the freshness gate rejected, and
	// neither answers what capital a fund is owed. The whole eligible principal
	// therefore parks in the Reserve, whose allocation stays revisable — its
	// Buffer commitment is authority-gated and reads only its own balance
	// (§6.4), so an operator can still move the principal once valuation
	// recovers. The Buffer has no such exit, so crediting the Reserve wrongly is
	// recoverable where committing principal to the Buffer is not, and burning
	// it is less so.
	bufferCredit := math.ZeroInt()
	reserveCredit := eligible
	insuranceCredit := math.ZeroInt()
	overflowBurn := math.ZeroInt()
	if complete {
		// The mint this expansion is about to perform is itself liability the
		// targets must answer for, so the waterfall sizes against post-mint
		// exposure rather than the snapshot the quote was drawn under.
		postMintLiability, err := decimal.Add(liabilityNoah, convertedOutput.Amount)
		if err != nil {
			return sdk.Coin{}, fmt.Errorf("adding stable output to aggregate liability: %w", err)
		}
		status, err := k.calculateFundStatus(ctx, postMintLiability)
		if err != nil {
			return sdk.Coin{}, err
		}
		bufferGap := shortfall(status.bufferTarget, status.bufferBalance)
		reserveGap := shortfall(status.reserveTarget, status.reserveBalance)
		insuranceGap := shortfall(status.insuranceTarget, status.insuranceBalance)
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
		{reservetypes.StrategicReserveName, reserveCredit},
		{claimstypes.InsuranceName, insuranceCredit},
	}
	for _, credit := range credits {
		if credit.amount.IsZero() {
			continue
		}
		if err := k.bankKeeper.SendCoinsFromModuleToModule(
			ctx,
			markettypes.ModuleName,
			credit.module,
			chain.NoahCoins(credit.amount),
		); err != nil {
			return sdk.Coin{}, fmt.Errorf("crediting %s: %w", credit.module, err)
		}
	}

	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventExpansionAllocated{
		Denom:                  chain.NoahBaseDenom,
		RedemptionBufferCredit: bufferCredit,
		StrategicReserveCredit: reserveCredit,
		InsuranceCredit:        insuranceCredit,
		SpreadAndDustBurn:      spread,
		OverflowBurn:           overflowBurn,
	}); err != nil {
		return sdk.Coin{}, fmt.Errorf("emitting Treasury expansion allocation event: %w", err)
	}

	return chain.NoahCoin(spread.Add(overflowBurn)), nil
}

// DrawRedemptionBuffer funds the current Buffer coverage share of the quoted
// NOAH output using pre-burn claimable liability and Buffer state; the redeemed
// denomination arrives already recognised, since Market converts out of
// oracle-priced assets only and routes a suspended asset through settlement.
// The draw runs whether or not valuation is complete and never reads the flag,
// because the claimable aggregate already excludes the supply that cannot
// redeem — a suspension elsewhere raises coverage for healthy exits instead of
// switching the Buffer off during the contagion it was built for. Disclosure
// belongs to EventLiabilityIncomplete, and the draw returns the payment alone:
// handing settlement a completeness flag would invite it to branch on valuation
// state.
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

	liabilityNoah, _, err := k.cachedLiabilityValue(ctx, quoteRates)
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
			chain.NoahCoins(bufferPaid),
		); err != nil {
			return math.Int{}, fmt.Errorf("drawing redemption buffer: %w", err)
		}
	}

	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventRedemptionBufferDrawn{
		Denom:   chain.NoahBaseDenom,
		Payment: bufferPaid,
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
	// Both committee-operated funds report their own recognised capital (§7.2);
	// Treasury owns only the requirement each is measured against, and asks each
	// operator what its fund is worth rather than reading the module account
	// behind its back. Insurance already differs from its raw balance because an
	// approved pending claim is encumbered, and Reserve will once it gains
	// haircut external value in §20.1 — which is why the figure is asked for
	// rather than read. The Buffer keeps a direct balance read: it has no
	// operator, so Treasury is its operator.
	insuranceBalance, err := k.claimsKeeper.RecognisedCapital(ctx)
	if err != nil {
		return fundStatus{}, fmt.Errorf("getting Insurance recognised capital: %w", err)
	}
	reserveBalance, err := k.reserveKeeper.RecognisedCapital(ctx)
	if err != nil {
		return fundStatus{}, fmt.Errorf("getting Reserve recognised capital: %w", err)
	}
	bufferBalance := k.balance(ctx, types.RedemptionBufferName)
	bufferTarget := policy.RedemptionBufferTargetRatio.MulRoundUp(liabilityNoah).Ceil().TruncateInt()
	reserveTarget := policy.StrategicReserveTargetRatio.MulRoundUp(liabilityNoah).Ceil().TruncateInt()
	insuranceTarget := policy.InsuranceTargetRatio.MulRoundUp(liabilityNoah).Ceil().TruncateInt()
	return fundStatus{
		bufferBalance:    bufferBalance,
		bufferTarget:     bufferTarget,
		reserveBalance:   reserveBalance,
		reserveTarget:    reserveTarget,
		insuranceBalance: insuranceBalance,
		insuranceTarget:  insuranceTarget,
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
