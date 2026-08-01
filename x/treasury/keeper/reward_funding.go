package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	chain "ark/pkg/chain"
	"ark/pkg/decimal"
	assettypes "ark/x/asset/types"
	oracletypes "ark/x/oracle/types"
	"ark/x/treasury/types"
)

func (k Keeper) updateRewardFunding(ctx context.Context) (types.RewardFundingState, error) {
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

	validatorRewards := k.bankKeeper.GetAllBalances(
		ctx,
		k.accountKeeper.GetModuleAddress(authtypes.FeeCollectorName),
	)
	if !validatorRewards.IsZero() {
		pricings, err := k.assetKeeper.Pricings(ctx, nil, validatorRewards.Denoms()...)
		if err != nil {
			return types.RewardFundingState{}, fmt.Errorf("pricing validator fees: %w", err)
		}
		validatorFeeValue, err := valueRewards(validatorRewards, pricings)
		if err != nil {
			return types.RewardFundingState{}, fmt.Errorf("valuing validator fees: %w", err)
		}
		funding.ValidatorFeeValue, err = funding.ValidatorFeeValue.SafeAdd(validatorFeeValue)
		if err != nil {
			return types.RewardFundingState{}, fmt.Errorf("adding validator fee value: %w", err)
		}
	}
	funding.BlocksRemaining--

	if err := k.RewardFunding.Set(ctx, funding); err != nil {
		return types.RewardFundingState{}, fmt.Errorf("setting reward funding state: %w", err)
	}
	return funding, nil
}

func (k Keeper) settleRewardFunding(ctx context.Context, funding types.RewardFundingState) error {
	stabilityTax := k.bankKeeper.GetAllBalances(
		ctx,
		k.accountKeeper.GetModuleAddress(types.StabilityTaxCollectorName),
	)
	pricings, err := k.assetKeeper.Pricings(ctx, nil, stabilityTax.Denoms()...)
	if err != nil {
		return err
	}
	priced, err := k.routeUnpricedTax(ctx, stabilityTax, pricings)
	if err != nil {
		return err
	}

	validatorTarget := funding.ValidatorTarget
	oracleTarget := funding.OracleTarget
	if validatorTarget.IsZero() && oracleTarget.IsZero() {
		return k.allocateStabilityTax(ctx, sdk.NewCoins(), priced)
	}

	stabilityTaxValue, err := valueRewards(priced, pricings)
	if err != nil {
		return fmt.Errorf("valuing stability tax: %w", err)
	}

	validatorGap := shortfall(validatorTarget, funding.ValidatorFeeValue)
	desiredValidatorTax := shortfall(stabilityTaxValue, oracleTarget)
	if desiredValidatorTax.GT(validatorGap) {
		desiredValidatorTax = validatorGap
	}

	validatorTax := allocateValidatorTax(priced, pricings, desiredValidatorTax, stabilityTaxValue)
	oracleTax, _ := priced.SafeSub(validatorTax...)

	validatorTaxValue, err := valueRewards(validatorTax, pricings)
	if err != nil {
		return fmt.Errorf("valuing validator stability tax: %w", err)
	}
	oracleOrganic, err := valueRewards(oracleTax, pricings)
	if err != nil {
		return fmt.Errorf("valuing Oracle stability tax: %w", err)
	}
	validatorOrganic, err := funding.ValidatorFeeValue.SafeAdd(validatorTaxValue)
	if err != nil {
		return fmt.Errorf("adding validator organic rewards: %w", err)
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

// routeUnpricedTax partitions the collector balance by pricing
// verdict, before settlement touches it, returning the priced remainder.
// Unpriced coins follow the verdict on why the rate is missing: written-off
// and retired supply has no feed to wait for, so it moves to the strategic
// reserve, while everything else — a member's stale feed, a suspension that
// may yet recover — stays in the collector for a window that can price it.
// Unrecognised denominations defer, because value never moves on a state this
// function does not understand.
func (k Keeper) routeUnpricedTax(ctx context.Context, stabilityTax sdk.Coins, pricings assettypes.DenomPricings) (sdk.Coins, error) {
	var priced, deferred, moved sdk.Coins
	for _, coin := range stabilityTax {
		pricing := pricings[coin.Denom]
		switch {
		case pricing.Priced:
			priced = append(priced, coin)
		case pricing.Reason == assettypes.UnpricedWrittenOff || pricing.Reason == assettypes.UnpricedRetired:
			moved = append(moved, coin)
		default:
			deferred = append(deferred, coin)
		}
	}
	if moved.IsZero() && deferred.IsZero() {
		return priced, nil
	}

	if !moved.IsZero() {
		if err := k.bankKeeper.SendCoinsFromModuleToModule(
			ctx,
			types.StabilityTaxCollectorName,
			types.StrategicReserveName,
			moved,
		); err != nil {
			return nil, fmt.Errorf("moving written-off stability tax to the strategic reserve: %w", err)
		}
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventUnpricedStabilityTaxRouted{
		Moved:    moved,
		Deferred: deferred,
	}); err != nil {
		return nil, fmt.Errorf("emitting Treasury unpriced stability tax event: %w", err)
	}
	return priced, nil
}

// valueRewards sums the NOAH value of the coins the registry could price.
// Availability is settled before valuation — an unpriced verdict counts as
// zero here, and settlement partitions the collector before valuing it — so an
// error from the remaining arithmetic is a bug or state beyond the supported
// economic domain, and it fails the block transition. That is deliberately
// harsher than the cap-refresh and liability paths, where out-of-range reads
// are an expected degraded mode with a retry to return to: those paths have a
// conservative fallback to degrade into, while a reward window valued from a
// partially summed pot would misallocate rather than degrade.
func valueRewards(rewards sdk.Coins, pricings assettypes.DenomPricings) (math.Int, error) {
	value := math.LegacyZeroDec()
	for _, coin := range rewards {
		if !pricings[coin.Denom].Priced {
			continue
		}
		converted, err := pricings.Convert(sdk.NewDecCoinFromCoin(coin), chain.NoahBaseDenom)
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

func allocateValidatorTax(tax sdk.Coins, pricings assettypes.DenomPricings, validatorValue, totalValue math.Int) sdk.Coins {
	if !validatorValue.IsPositive() || !totalValue.IsPositive() {
		return sdk.NewCoins()
	}

	validatorTax := make(sdk.Coins, 0, len(tax))
	for _, coin := range tax {
		if !pricings[coin.Denom].Priced {
			continue
		}
		amount := coin.Amount.Mul(validatorValue).Quo(totalValue)
		if amount.IsPositive() {
			validatorTax = append(validatorTax, sdk.NewCoin(coin.Denom, amount))
		}
	}
	return validatorTax
}
