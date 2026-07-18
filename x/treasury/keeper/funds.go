package keeper

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"

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
// waterfall from Market's escrow. It neither mints nor burns.
func (k Keeper) RouteExpansion(
	ctx context.Context,
	grossOffer sdk.Coin,
	stableOutput sdk.Coin,
	quoteRates oracletypes.RateSnapshot,
) (types.ExpansionAllocation, error) {
	if err := validatePositiveNoahCoin(grossOffer); err != nil {
		return types.ExpansionAllocation{}, fmt.Errorf("invalid gross offer: %w", err)
	}
	if err := stableOutput.Validate(); err != nil {
		return types.ExpansionAllocation{}, fmt.Errorf("invalid stable output: %w", err)
	}
	if !stableOutput.IsPositive() {
		return types.ExpansionAllocation{}, fmt.Errorf("stable output must be positive: %s", stableOutput)
	}
	tobinTaxes, err := k.oracleKeeper.GetTobinTaxes(ctx)
	if err != nil {
		return types.ExpansionAllocation{}, fmt.Errorf("getting Tobin taxes: %w", err)
	}
	if !slices.ContainsFunc(tobinTaxes, func(tax oracletypes.TobinTax) bool {
		return tax.Denom == stableOutput.Denom
	}) {
		return types.ExpansionAllocation{}, fmt.Errorf("stable output denom %s is not configured in oracle", stableOutput.Denom)
	}

	convertedOutput, err := quoteRates.Convert(sdk.NewDecCoinFromCoin(stableOutput), chain.MicroNoahDenom)
	if err != nil {
		return types.ExpansionAllocation{}, fmt.Errorf("valuing stable output: %w", err)
	}
	eligible := convertedOutput.Amount.TruncateInt()
	if eligible.GT(grossOffer.Amount) {
		return types.ExpansionAllocation{}, fmt.Errorf(
			"stable output value %s exceeds gross offer %s",
			sdk.NewCoin(chain.MicroNoahDenom, eligible),
			grossOffer,
		)
	}
	spread := grossOffer.Amount.Sub(eligible)

	allocation := types.ExpansionAllocation{
		EligiblePrincipalNoah:   eligible,
		RedemptionBufferCredit:  math.ZeroInt(),
		StrategicReserveCredit:  math.ZeroInt(),
		InsuranceCredit:         math.ZeroInt(),
		SpreadAndDustBurn:       spread,
		OverflowBurn:            math.ZeroInt(),
		TargetValuationComplete: true,
	}

	liabilityNoah, complete, err := k.cachedLiabilityValue(ctx, tobinTaxes, quoteRates)
	if err != nil {
		return types.ExpansionAllocation{}, err
	}
	if complete {
		liabilityNoah, err = decimal.Add(liabilityNoah, convertedOutput.Amount)
		if errors.Is(err, decimal.ErrOutOfRange) {
			liabilityNoah = math.LegacyZeroDec()
			complete = false
		} else if err != nil {
			return types.ExpansionAllocation{}, fmt.Errorf("adding stable output to aggregate liability: %w", err)
		}
	}
	var status fundStatus
	var bufferGap, reserveGap, insuranceGap math.Int
	if !complete {
		allocation.TargetValuationComplete = false
		allocation.RedemptionBufferCredit = eligible
	} else {
		status, err = k.calculateFundStatus(ctx, liabilityNoah)
		if err != nil {
			return types.ExpansionAllocation{}, err
		}
		bufferGap = shortfall(status.bufferTarget, status.bufferBalance)
		reserveGap = shortfall(status.reserveTarget, status.reserveBalance)
		insuranceGap = shortfall(status.insuranceTarget, status.insuranceUnencumbered)
		remaining := eligible
		allocation.RedemptionBufferCredit = math.MinInt(remaining, bufferGap)
		remaining = remaining.Sub(allocation.RedemptionBufferCredit)
		allocation.StrategicReserveCredit = math.MinInt(remaining, reserveGap)
		remaining = remaining.Sub(allocation.StrategicReserveCredit)
		allocation.InsuranceCredit = math.MinInt(remaining, insuranceGap)
		remaining = remaining.Sub(allocation.InsuranceCredit)
		allocation.OverflowBurn = remaining
	}

	credits := []struct {
		module string
		amount math.Int
	}{
		{types.RedemptionBufferName, allocation.RedemptionBufferCredit},
		{types.StrategicReserveName, allocation.StrategicReserveCredit},
		{types.InsuranceName, allocation.InsuranceCredit},
	}
	for _, credit := range credits {
		if credit.amount.IsZero() {
			continue
		}
		if err := k.bankKeeper.SendCoinsFromModuleToModule(
			ctx,
			markettypes.ModuleName,
			credit.module,
			sdk.NewCoins(sdk.NewCoin(chain.MicroNoahDenom, credit.amount)),
		); err != nil {
			return types.ExpansionAllocation{}, fmt.Errorf("crediting %s: %w", credit.module, err)
		}
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeExpansionAllocated,
		sdk.NewAttribute(types.AttributeKeyRedemptionBufferCredit, sdk.NewCoin(chain.MicroNoahDenom, allocation.RedemptionBufferCredit).String()),
		sdk.NewAttribute(types.AttributeKeyStrategicReserveCredit, sdk.NewCoin(chain.MicroNoahDenom, allocation.StrategicReserveCredit).String()),
		sdk.NewAttribute(types.AttributeKeyInsuranceCredit, sdk.NewCoin(chain.MicroNoahDenom, allocation.InsuranceCredit).String()),
		sdk.NewAttribute(types.AttributeKeySpreadAndDustBurn, sdk.NewCoin(chain.MicroNoahDenom, allocation.SpreadAndDustBurn).String()),
		sdk.NewAttribute(types.AttributeKeyOverflowBurn, sdk.NewCoin(chain.MicroNoahDenom, allocation.OverflowBurn).String()),
		sdk.NewAttribute(types.AttributeKeyTargetValuationComplete, strconv.FormatBool(complete)),
	))

	return allocation, nil
}

// DrawRedemptionBuffer pays the proportional Buffer entitlement using
// pre-burn liability and Buffer state.
func (k Keeper) DrawRedemptionBuffer(
	ctx context.Context,
	redeemedStable sdk.Coin,
	noahOutput math.Int,
	quoteRates oracletypes.RateSnapshot,
) (types.BufferDraw, error) {
	if err := redeemedStable.Validate(); err != nil {
		return types.BufferDraw{}, fmt.Errorf("invalid redeemed stable coin: %w", err)
	}
	if !redeemedStable.IsPositive() {
		return types.BufferDraw{}, fmt.Errorf("redeemed stable coin must be positive: %s", redeemedStable)
	}
	if noahOutput.IsNil() || !noahOutput.IsPositive() {
		return types.BufferDraw{}, fmt.Errorf("NOAH output must be set and positive")
	}

	tobinTaxes, err := k.oracleKeeper.GetTobinTaxes(ctx)
	if err != nil {
		return types.BufferDraw{}, fmt.Errorf("getting Tobin taxes: %w", err)
	}
	if !slices.ContainsFunc(tobinTaxes, func(tax oracletypes.TobinTax) bool {
		return tax.Denom == redeemedStable.Denom
	}) {
		return types.BufferDraw{}, fmt.Errorf("redeemed denom %s is not configured in oracle", redeemedStable.Denom)
	}

	convertedRedemption, err := quoteRates.Convert(sdk.NewDecCoinFromCoin(redeemedStable), chain.MicroNoahDenom)
	if err != nil {
		return types.BufferDraw{}, fmt.Errorf("valuing redeemed stable coin: %w", err)
	}
	redeemedNoah := convertedRedemption.Amount

	liabilityNoah, complete, err := k.cachedLiabilityValue(ctx, tobinTaxes, quoteRates)
	if err != nil {
		return types.BufferDraw{}, err
	}
	draw := types.BufferDraw{
		AggregateLiabilityNoah: liabilityNoah,
		RedeemedLiabilityNoah:  redeemedNoah,
		BufferPaid:             math.ZeroInt(),
		ValuationComplete:      complete,
	}
	if complete {
		if redeemedNoah.GT(liabilityNoah) {
			return types.BufferDraw{}, fmt.Errorf("redeemed liability %s exceeds aggregate liability %s", redeemedNoah, liabilityNoah)
		}

		bufferBalance := k.balance(ctx, types.RedemptionBufferName)
		entitlement := redeemedNoah.MulInt(bufferBalance).Quo(liabilityNoah).TruncateInt()
		draw.BufferPaid = math.MinInt(entitlement, noahOutput)
		if draw.BufferPaid.IsPositive() {
			if err := k.bankKeeper.SendCoinsFromModuleToModule(
				ctx,
				types.RedemptionBufferName,
				markettypes.ModuleName,
				sdk.NewCoins(sdk.NewCoin(chain.MicroNoahDenom, draw.BufferPaid)),
			); err != nil {
				return types.BufferDraw{}, fmt.Errorf("drawing redemption buffer: %w", err)
			}
		}
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeRedemptionBufferDrawn,
		sdk.NewAttribute(types.AttributeKeyRedemptionBufferPayment, sdk.NewCoin(chain.MicroNoahDenom, draw.BufferPaid).String()),
		sdk.NewAttribute(types.AttributeKeyAggregateValuationComplete, strconv.FormatBool(draw.ValuationComplete)),
	))
	return draw, nil
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
	return k.bankKeeper.GetBalance(ctx, addr, chain.MicroNoahDenom).Amount
}

func validatePositiveNoahCoin(coin sdk.Coin) error {
	if err := coin.Validate(); err != nil {
		return err
	}
	if coin.Denom != chain.MicroNoahDenom || !coin.IsPositive() {
		return fmt.Errorf("coin must be positive %s", chain.MicroNoahDenom)
	}
	return nil
}
