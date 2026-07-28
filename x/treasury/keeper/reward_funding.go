package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	chain "ark/pkg/chain"
	"ark/pkg/decimal"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/types"
)

func (k Keeper) updateRewardFunding(ctx context.Context, configuredDenoms map[string]struct{}) (types.RewardFundingState, error) {
	funding, err := k.RewardFunding.Get(ctx)
	if err != nil {
		return types.RewardFundingState{}, fmt.Errorf("getting reward funding state: %w", err)
	}
	if funding.BlocksRemaining == 0 {
		params, err := k.Params.Get(ctx)
		if err != nil {
			return types.RewardFundingState{}, fmt.Errorf("getting params: %w", err)
		}
		funding.BlocksRemaining = params.RewardFundingWindow
	}
	policy, err := k.MonetaryPolicy.Get(ctx)
	if err != nil {
		return types.RewardFundingState{}, fmt.Errorf("getting monetary policy: %w", err)
	}

	funding.ValidatorTarget, err = funding.ValidatorTarget.SafeAdd(policy.ValidatorBlockRewardTarget)
	if err != nil {
		return types.RewardFundingState{}, fmt.Errorf("adding validator reward target: %w", err)
	}
	funding.OracleTarget, err = funding.OracleTarget.SafeAdd(policy.OracleBlockRewardTarget)
	if err != nil {
		return types.RewardFundingState{}, fmt.Errorf("adding Oracle reward target: %w", err)
	}
	if funding.ValuationComplete {
		validatorRewards := k.bankKeeper.GetAllBalances(
			ctx,
			k.accountKeeper.GetModuleAddress(authtypes.FeeCollectorName),
		)
		if !validatorRewards.IsZero() {
			validatorFeeValue, _, valueErr := k.valueRewardCoins(ctx, validatorRewards, configuredDenoms)
			if valueErr != nil {
				if !isSkippableValuation(valueErr) {
					return types.RewardFundingState{}, fmt.Errorf("valuing validator fees: %w", valueErr)
				}
				funding.ValuationComplete = false
				k.Logger(ctx).Warn(
					"marking reward-funding window valuation incomplete",
					"error",
					valueErr.Error(),
				)
			} else if accumulatedFeeValue, addErr := funding.ValidatorFeeValue.SafeAdd(validatorFeeValue); addErr != nil {
				funding.ValuationComplete = false
				k.Logger(ctx).Warn(
					"marking reward-funding window valuation incomplete",
					"error",
					fmt.Errorf("adding validator fee value: %w", addErr).Error(),
				)
			} else {
				funding.ValidatorFeeValue = accumulatedFeeValue
			}
		}
	}
	funding.BlocksRemaining--

	if err := k.RewardFunding.Set(ctx, funding); err != nil {
		return types.RewardFundingState{}, fmt.Errorf("setting reward funding state: %w", err)
	}
	return funding, nil
}

func (k Keeper) settleRewardFunding(ctx context.Context, funding types.RewardFundingState, configuredDenoms map[string]struct{}) error {
	stabilityTax := k.bankKeeper.GetAllBalances(
		ctx,
		k.accountKeeper.GetModuleAddress(types.StabilityTaxCollectorName),
	)
	if !funding.ValuationComplete {
		return k.sendStabilityTaxToOracle(
			ctx,
			stabilityTax,
			types.EventSkipReason_EVENT_SKIP_REASON_WINDOW_VALUATION_INCOMPLETE,
			"validator fee valuation was incomplete during the funding window",
		)
	}

	validatorTarget := funding.ValidatorTarget
	oracleTarget := funding.OracleTarget
	if validatorTarget.IsZero() && oracleTarget.IsZero() {
		return k.allocateStabilityTax(ctx, sdk.NewCoins(), stabilityTax)
	}

	stabilityTaxValue, rates, err := k.valueRewardCoins(ctx, stabilityTax, configuredDenoms)
	if err != nil {
		err = fmt.Errorf("valuing stability tax: %w", err)
		if isSkippableValuation(err) {
			return k.sendStabilityTaxToOracle(ctx, stabilityTax, eventSkipReason(err), err.Error())
		}
		return err
	}

	validatorGap := shortfall(validatorTarget, funding.ValidatorFeeValue)
	desiredValidatorTax := shortfall(stabilityTaxValue, oracleTarget)
	if desiredValidatorTax.GT(validatorGap) {
		desiredValidatorTax = validatorGap
	}

	validatorTax := allocateValidatorTax(stabilityTax, rates, desiredValidatorTax, stabilityTaxValue)
	oracleTax, _ := stabilityTax.SafeSub(validatorTax...)

	validatorTaxValue, err := valueRewards(validatorTax, rates)
	if err != nil {
		err = fmt.Errorf("valuing validator stability tax: %w", err)
		if isSkippableValuation(err) {
			return k.sendStabilityTaxToOracle(ctx, stabilityTax, eventSkipReason(err), err.Error())
		}
		return err
	}
	oracleOrganic, err := valueRewards(oracleTax, rates)
	if err != nil {
		err = fmt.Errorf("valuing Oracle stability tax: %w", err)
		if isSkippableValuation(err) {
			return k.sendStabilityTaxToOracle(ctx, stabilityTax, eventSkipReason(err), err.Error())
		}
		return err
	}
	validatorOrganic, err := funding.ValidatorFeeValue.SafeAdd(validatorTaxValue)
	if err != nil {
		return k.sendStabilityTaxToOracle(
			ctx,
			stabilityTax,
			types.EventSkipReason_EVENT_SKIP_REASON_ARITHMETIC_OUT_OF_RANGE,
			fmt.Errorf("adding validator organic rewards: %w", err).Error(),
		)
	}

	validatorShortfall := shortfall(validatorTarget, validatorOrganic)
	oracleShortfall := shortfall(oracleTarget, oracleOrganic)
	subsidyBalance := k.balance(ctx, types.SubsidyPoolName)
	validatorSubsidy := validatorShortfall
	oracleSubsidy := oracleShortfall
	totalShortfall := validatorShortfall.Add(oracleShortfall)
	if subsidyBalance.LT(totalShortfall) {
		oracleSubsidy = subsidyBalance.Mul(oracleShortfall).Quo(totalShortfall)
		validatorSubsidy = subsidyBalance.Sub(oracleSubsidy)
	}

	if err := k.allocateStabilityTax(ctx, validatorTax, oracleTax); err != nil {
		return err
	}

	if validatorSubsidy.IsPositive() {
		if err := k.bankKeeper.SendCoinsFromModuleToModule(
			ctx,
			types.SubsidyPoolName,
			authtypes.FeeCollectorName,
			sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, validatorSubsidy)),
		); err != nil {
			return fmt.Errorf("topping up validator rewards: %w", err)
		}
	}
	if oracleSubsidy.IsPositive() {
		if err := k.bankKeeper.SendCoinsFromModuleToModule(
			ctx,
			types.SubsidyPoolName,
			oracletypes.ModuleName,
			sdk.NewCoins(sdk.NewCoin(chain.NoahBaseDenom, oracleSubsidy)),
		); err != nil {
			return fmt.Errorf("topping up Oracle rewards: %w", err)
		}
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventBlockRewardsToppedUp{
		Denom:            chain.NoahBaseDenom,
		ValidatorTarget:  validatorTarget,
		OracleTarget:     oracleTarget,
		ValidatorOrganic: validatorOrganic,
		OracleOrganic:    oracleOrganic,
		ValidatorPaid:    validatorSubsidy,
		OraclePaid:       oracleSubsidy,
	}); err != nil {
		return fmt.Errorf("emitting Treasury reward top-up event: %w", err)
	}
	return nil
}

func (k Keeper) sendStabilityTaxToOracle(ctx context.Context, stabilityTax sdk.Coins, reason types.EventSkipReason, detail string) error {
	if err := k.allocateStabilityTax(ctx, sdk.NewCoins(), stabilityTax); err != nil {
		return err
	}
	k.Logger(ctx).Warn("skipping reward-funding settlement", "reason", reason.String(), "error", detail)
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventBlockRewardTopUpSkipped{
		Reason: reason,
	}); err != nil {
		return fmt.Errorf("emitting Treasury reward top-up skip event: %w", err)
	}
	return nil
}

func (k Keeper) allocateStabilityTax(ctx context.Context, validator, oracle sdk.Coins) error {
	if !validator.IsZero() {
		if err := k.bankKeeper.SendCoinsFromModuleToModule(
			ctx,
			types.StabilityTaxCollectorName,
			authtypes.FeeCollectorName,
			validator,
		); err != nil {
			return fmt.Errorf("allocating stability tax to validators: %w", err)
		}
	}
	if !oracle.IsZero() {
		if err := k.bankKeeper.SendCoinsFromModuleToModule(
			ctx,
			types.StabilityTaxCollectorName,
			oracletypes.ModuleName,
			oracle,
		); err != nil {
			return fmt.Errorf("allocating stability tax to Oracle: %w", err)
		}
	}
	return nil
}

func (k Keeper) valueRewardCoins(ctx context.Context, rewards sdk.Coins, configuredDenoms map[string]struct{}) (math.Int, oracletypes.RateSet, error) {
	if rewards.IsZero() {
		rates, err := k.oracleKeeper.GetRateSet(ctx)
		return math.ZeroInt(), rates, err
	}
	if len(rewards) == 1 && rewards[0].Denom == chain.NoahBaseDenom {
		return rewards[0].Amount, oracletypes.RateSet{
			chain.NoahBaseDenom: math.LegacyOneDec(),
		}, nil
	}

	denoms := make([]string, 0, len(rewards))
	for _, coin := range rewards {
		if coin.Denom == chain.NoahBaseDenom {
			continue
		}
		if _, ok := configuredDenoms[coin.Denom]; ok {
			denoms = append(denoms, coin.Denom)
		}
	}

	rates, err := k.oracleKeeper.GetRateSet(ctx, denoms...)
	if err != nil {
		return math.Int{}, nil, err
	}
	value, err := valueRewards(rewards, rates)
	if err != nil {
		return math.Int{}, nil, err
	}
	return value, rates, nil
}

func valueRewards(rewards sdk.Coins, rates oracletypes.RateSet) (math.Int, error) {
	value := math.LegacyZeroDec()
	for _, coin := range rewards {
		if _, ok := rates[coin.Denom]; !ok {
			continue
		}
		converted, err := rates.Convert(sdk.NewDecCoinFromCoin(coin), chain.NoahBaseDenom)
		if err != nil {
			return math.Int{}, err
		}
		value, err = decimal.Add(value, converted.Amount)
		if err != nil {
			return math.Int{}, fmt.Errorf("adding reward value: %w", err)
		}
	}
	return value.TruncateInt(), nil
}

func shortfall(target, actual math.Int) math.Int {
	if target.LTE(actual) {
		return math.ZeroInt()
	}
	return target.Sub(actual)
}

func allocateValidatorTax(tax sdk.Coins, rates oracletypes.RateSet, validatorValue, totalValue math.Int) sdk.Coins {
	if !validatorValue.IsPositive() || !totalValue.IsPositive() {
		return sdk.NewCoins()
	}

	validatorTax := make(sdk.Coins, 0, len(tax))
	for _, coin := range tax {
		if _, ok := rates[coin.Denom]; !ok {
			continue
		}
		amount := coin.Amount.Mul(validatorValue).Quo(totalValue)
		if amount.IsPositive() {
			validatorTax = append(validatorTax, sdk.NewCoin(coin.Denom, amount))
		}
	}
	return validatorTax
}

func isValuationUnavailable(err error) bool {
	return errors.Is(err, oracletypes.ErrUnknownDenom) ||
		errors.Is(err, oracletypes.ErrStaleExchangeRate) ||
		errors.Is(err, oracletypes.ErrInvalidExchangeRate) ||
		errors.Is(err, oracletypes.ErrConversionOutOfRange)
}

func isSkippableValuation(err error) bool {
	return isValuationUnavailable(err) || errors.Is(err, decimal.ErrOutOfRange)
}

func eventSkipReason(err error) types.EventSkipReason {
	switch {
	case errors.Is(err, oracletypes.ErrUnknownDenom):
		return types.EventSkipReason_EVENT_SKIP_REASON_UNKNOWN_DENOM
	case errors.Is(err, oracletypes.ErrStaleExchangeRate):
		return types.EventSkipReason_EVENT_SKIP_REASON_STALE_EXCHANGE_RATE
	case errors.Is(err, oracletypes.ErrInvalidExchangeRate):
		return types.EventSkipReason_EVENT_SKIP_REASON_INVALID_EXCHANGE_RATE
	case errors.Is(err, oracletypes.ErrConversionOutOfRange):
		return types.EventSkipReason_EVENT_SKIP_REASON_CONVERSION_OUT_OF_RANGE
	case errors.Is(err, decimal.ErrOutOfRange):
		return types.EventSkipReason_EVENT_SKIP_REASON_ARITHMETIC_OUT_OF_RANGE
	default:
		return types.EventSkipReason_EVENT_SKIP_REASON_VALUATION_UNAVAILABLE
	}
}
