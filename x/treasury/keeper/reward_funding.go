package keeper

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	chain "ark/pkg/chain"
	"ark/pkg/decimal"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/types"
)

// UpdateRewardFunding records the previous block's reward targets and organic
// validator funding in the current window.
func (k Keeper) UpdateRewardFunding(ctx context.Context) (types.RewardFundingState, error) {
	funding, err := k.RewardFunding.Get(ctx)
	if err != nil {
		return types.RewardFundingState{}, fmt.Errorf("getting reward funding state: %w", err)
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return types.RewardFundingState{}, fmt.Errorf("getting params: %w", err)
	}
	policy, err := k.MonetaryPolicy.Get(ctx)
	if err != nil {
		return types.RewardFundingState{}, fmt.Errorf("getting monetary policy: %w", err)
	}
	if funding.BlocksRemaining == 0 {
		funding.BlocksRemaining = params.RewardFundingWindow
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
			authtypes.NewModuleAddress(authtypes.FeeCollectorName),
		)
		if !validatorRewards.IsZero() {
			validatorFeeValue, _, valueErr := k.valueRewardCoins(ctx, validatorRewards)
			if valueErr != nil {
				if !isValuationUnavailable(valueErr) && !errors.Is(valueErr, decimal.ErrOutOfRange) {
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

// SettleRewardFunding settles one completed reward-funding window.
func (k Keeper) SettleRewardFunding(ctx context.Context, funding types.RewardFundingState) error {
	stabilityTax := k.bankKeeper.GetAllBalances(
		ctx,
		authtypes.NewModuleAddress(types.StabilityTaxCollectorName),
	)
	if !funding.ValuationComplete {
		return k.sendStabilityTaxToOracle(
			ctx,
			stabilityTax,
			"validator fee valuation was incomplete during the funding window",
		)
	}

	validatorTarget := funding.ValidatorTarget
	oracleTarget := funding.OracleTarget
	if validatorTarget.IsZero() && oracleTarget.IsZero() {
		return k.allocateStabilityTax(ctx, sdk.NewCoins(), stabilityTax)
	}

	stabilityTaxValue, rates, err := k.valueRewardCoins(ctx, stabilityTax)
	if err != nil {
		if isValuationUnavailable(err) || errors.Is(err, decimal.ErrOutOfRange) {
			return k.sendStabilityTaxToOracle(ctx, stabilityTax, err.Error())
		}
		return fmt.Errorf("valuing stability tax: %w", err)
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
		if isValuationUnavailable(err) || errors.Is(err, decimal.ErrOutOfRange) {
			return k.sendStabilityTaxToOracle(ctx, stabilityTax, fmt.Errorf("valuing validator stability tax: %w", err).Error())
		}
		return fmt.Errorf("valuing validator stability tax: %w", err)
	}
	oracleOrganic, err := valueRewards(oracleTax, rates)
	if err != nil {
		if isValuationUnavailable(err) || errors.Is(err, decimal.ErrOutOfRange) {
			return k.sendStabilityTaxToOracle(ctx, stabilityTax, fmt.Errorf("valuing Oracle stability tax: %w", err).Error())
		}
		return fmt.Errorf("valuing Oracle stability tax: %w", err)
	}
	validatorOrganic, err := funding.ValidatorFeeValue.SafeAdd(validatorTaxValue)
	if err != nil {
		return k.sendStabilityTaxToOracle(ctx, stabilityTax, fmt.Errorf("adding validator organic rewards: %w", err).Error())
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
			sdk.NewCoins(sdk.NewCoin(chain.MicroNoahDenom, validatorSubsidy)),
		); err != nil {
			return fmt.Errorf("topping up validator rewards: %w", err)
		}
	}
	if oracleSubsidy.IsPositive() {
		if err := k.bankKeeper.SendCoinsFromModuleToModule(
			ctx,
			types.SubsidyPoolName,
			oracletypes.ModuleName,
			sdk.NewCoins(sdk.NewCoin(chain.MicroNoahDenom, oracleSubsidy)),
		); err != nil {
			return fmt.Errorf("topping up Oracle rewards: %w", err)
		}
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeBlockRewardsToppedUp,
		sdk.NewAttribute(types.AttributeKeyTarget, formatRewardAmounts(validatorTarget, oracleTarget)),
		sdk.NewAttribute(types.AttributeKeyOrganic, formatRewardAmounts(validatorOrganic, oracleOrganic)),
		sdk.NewAttribute(types.AttributeKeyPaid, formatRewardAmounts(validatorSubsidy, oracleSubsidy)),
	))
	return nil
}

func (k Keeper) sendStabilityTaxToOracle(ctx context.Context, stabilityTax sdk.Coins, reason string) error {
	if err := k.allocateStabilityTax(ctx, sdk.NewCoins(), stabilityTax); err != nil {
		return err
	}
	k.Logger(ctx).Warn("skipping reward-funding settlement", "error", reason)
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeBlockRewardTopUpSkipped,
		sdk.NewAttribute(types.AttributeKeySkipReason, reason),
	))
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

func (k Keeper) valueRewardCoins(ctx context.Context, rewards sdk.Coins) (math.Int, oracletypes.RateSnapshot, error) {
	if rewards.IsZero() {
		rates, err := k.oracleKeeper.GetRateSnapshot(ctx)
		return math.ZeroInt(), rates, err
	}

	tobinTaxes, err := k.oracleKeeper.GetTobinTaxes(ctx)
	if err != nil {
		return math.Int{}, nil, fmt.Errorf("getting Tobin taxes: %w", err)
	}
	configured := make(map[string]struct{}, len(tobinTaxes))
	for _, tax := range tobinTaxes {
		configured[tax.Denom] = struct{}{}
	}

	denoms := make([]string, 0, len(rewards))
	for _, coin := range rewards {
		if coin.Denom == chain.MicroNoahDenom {
			continue
		}
		if _, ok := configured[coin.Denom]; ok {
			denoms = append(denoms, coin.Denom)
		}
	}
	slices.Sort(denoms)

	rates, err := k.oracleKeeper.GetRateSnapshot(ctx, denoms...)
	if err != nil {
		return math.Int{}, nil, err
	}
	value, err := valueRewards(rewards, rates)
	if err != nil {
		return math.Int{}, nil, err
	}
	return value, rates, nil
}

func valueRewards(rewards sdk.Coins, rates oracletypes.RateSnapshot) (math.Int, error) {
	value := math.LegacyZeroDec()
	for _, coin := range rewards {
		if _, ok := rates[coin.Denom]; !ok {
			continue
		}
		converted, err := rates.Convert(sdk.NewDecCoinFromCoin(coin), chain.MicroNoahDenom)
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

func allocateValidatorTax(tax sdk.Coins, rates oracletypes.RateSnapshot, validatorValue, totalValue math.Int) sdk.Coins {
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

func formatRewardAmounts(validator, oracle math.Int) string {
	return fmt.Sprintf(
		"validator=%s,oracle=%s",
		sdk.NewCoin(chain.MicroNoahDenom, validator),
		sdk.NewCoin(chain.MicroNoahDenom, oracle),
	)
}

func isValuationUnavailable(err error) bool {
	return errors.Is(err, oracletypes.ErrUnknownDenom) ||
		errors.Is(err, oracletypes.ErrStaleExchangeRate) ||
		errors.Is(err, oracletypes.ErrInvalidExchangeRate) ||
		errors.Is(err, oracletypes.ErrConversionOutOfRange)
}
