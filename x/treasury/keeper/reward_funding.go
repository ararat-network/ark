package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/decimal"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	reservetypes "github.com/ararat-network/ark/x/reserve/types"
	"github.com/ararat-network/ark/x/treasury/types"
)

// taxSplit contains each reward leg's transfer-tax share and resulting organic earnings. Pure split
// arithmetic is separate from subsidy reads and transfers.
type taxSplit struct {
	validatorTax     sdk.Coins
	oracleTax        sdk.Coins
	validatorOrganic math.Int
	oracleOrganic    math.Int
}

// advanceRewardFunding accrues each real block's earned fees, settles a closing window, and resets
// its accounting. EndBlock sees this block's fees; the height guard excludes pre-block calls.
func (k Keeper) advanceRewardFunding(ctx context.Context) error {
	funding, err := k.updateRewardFunding(ctx)
	if err != nil {
		return err
	}
	if funding.BlocksRemaining > 0 {
		return nil
	}

	if err := k.settleRewardFunding(ctx, funding); err != nil {
		return err
	}
	if err := k.RewardFunding.Set(ctx, types.DefaultRewardFundingState()); err != nil {
		return fmt.Errorf("resetting reward funding state: %w", err)
	}
	return nil
}

// updateRewardFunding accrues one block of reward targets and validator fee
// value into the open window, opening a fresh window first when none is.
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
	policy, err := k.EconomicPolicy.Get(ctx)
	if err != nil {
		return types.RewardFundingState{}, fmt.Errorf("getting economic policy: %w", err)
	}

	// MaxBlockRewardTarget and MaxRewardFundingWindow bound window accrual below Int range. Checked
	// additions remain diagnostic backstops in this EndBlock path.
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
		pricings, err := k.assetKeeper.Pricings(ctx, validatorRewards.Denoms()...)
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

// settleRewardFunding closes a window: it gathers the collected transfer tax,
// plans the split and any subsidy against the window's targets, and then moves
// the value. Nothing is transferred before the whole plan is known.
func (k Keeper) settleRewardFunding(ctx context.Context, funding types.RewardFundingState) error {
	transferTax := k.bankKeeper.GetAllBalances(
		ctx,
		k.accountKeeper.GetModuleAddress(types.TransferTaxCollectorName),
	)
	pricings, err := k.assetKeeper.Pricings(ctx, transferTax.Denoms()...)
	if err != nil {
		return fmt.Errorf("pricing transfer tax: %w", err)
	}
	priced, err := k.routeUnpricedTax(ctx, transferTax, pricings)
	if err != nil {
		return err
	}

	// With no targets to serve there is nothing to size a split against, so the
	// tax goes to Oracle whole rather than being valued first.
	if funding.ValidatorTarget.IsZero() && funding.OracleTarget.IsZero() {
		return k.sendTransferTax(ctx, sdk.NewCoins(), priced)
	}

	split, err := planTaxSplit(funding, priced, pricings)
	if err != nil {
		return err
	}

	// Read subsidy custody after validating the tax split. A pool below the combined shortfall pays
	// both legs proportionally; non-negative balances exclude a zero divisor.
	subsidyBalance := k.getBalance(ctx, types.SubsidyPoolName)
	validatorShortfall := shortfall(funding.ValidatorTarget, split.validatorOrganic)
	oracleShortfall := shortfall(funding.OracleTarget, split.oracleOrganic)
	validatorSubsidy, oracleSubsidy := validatorShortfall, oracleShortfall
	totalShortfall, err := validatorShortfall.SafeAdd(oracleShortfall)
	if err != nil {
		return fmt.Errorf("summing the window's reward shortfalls: %w", err)
	}
	if subsidyBalance.LT(totalShortfall) {
		// Check the product before dividing a scarce pool pro rata. This branch has positive
		// totalShortfall; flooring Oracle's share leaves a non-negative validator remainder and
		// spends exactly the pool.
		scaledOracle, err := subsidyBalance.SafeMul(oracleShortfall)
		if err != nil {
			return fmt.Errorf("sizing the Oracle subsidy share: %w", err)
		}
		oracleSubsidy = scaledOracle.Quo(totalShortfall)
		validatorSubsidy = subsidyBalance.Sub(oracleSubsidy)
	}

	if err := k.sendTransferTax(ctx, split.validatorTax, split.oracleTax); err != nil {
		return err
	}
	if validatorSubsidy.IsPositive() {
		if err := k.bankKeeper.SendCoinsFromModuleToModule(
			ctx,
			types.SubsidyPoolName,
			authtypes.FeeCollectorName,
			chain.NoahCoins(validatorSubsidy),
		); err != nil {
			return fmt.Errorf("topping up validator rewards: %w", err)
		}
	}
	if oracleSubsidy.IsPositive() {
		if err := k.bankKeeper.SendCoinsFromModuleToModule(
			ctx,
			types.SubsidyPoolName,
			oracletypes.ModuleName,
			chain.NoahCoins(oracleSubsidy),
		); err != nil {
			return fmt.Errorf("topping up Oracle rewards: %w", err)
		}
	}

	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventBlockRewardsToppedUp{
		Denom:            chain.NoahBaseDenom,
		ValidatorTarget:  funding.ValidatorTarget,
		OracleTarget:     funding.OracleTarget,
		ValidatorOrganic: split.validatorOrganic,
		OracleOrganic:    split.oracleOrganic,
		ValidatorPaid:    validatorSubsidy,
		OraclePaid:       oracleSubsidy,
	}); err != nil {
		return fmt.Errorf("emitting Treasury reward top-up event: %w", err)
	}
	return nil
}

// routeUnpricedTax returns priced collector coins, moves written-off/retired residue to Reserve,
// and defers other unpriced holdings. Unrecognised custody is unreachable under admission/import
// rules but safely defers. See x/treasury/README.md for custody and valuation boundaries.
func (k Keeper) routeUnpricedTax(ctx context.Context, transferTax sdk.Coins, pricings assettypes.AssetPricings) (sdk.Coins, error) {
	var priced, deferred, moved sdk.Coins
	for _, coin := range transferTax {
		verdict := pricings[coin.Denom]
		switch {
		case verdict.IsPriced():
			priced = append(priced, coin)
		case verdict.Reason == assettypes.UnpricedReason_UNPRICED_REASON_WRITTEN_OFF || verdict.Reason == assettypes.UnpricedReason_UNPRICED_REASON_RETIRED:
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
			types.TransferTaxCollectorName,
			reservetypes.StrategicReserveName,
			moved,
		); err != nil {
			return nil, fmt.Errorf("moving written-off transfer tax to the strategic reserve: %w", err)
		}
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventUnpricedTransferTaxRouted{
		Moved:    moved,
		Deferred: deferred,
	}); err != nil {
		return nil, fmt.Errorf("emitting Treasury unpriced transfer tax event: %w", err)
	}
	return priced, nil
}

// planTaxSplit computes priced-tax shares and organic earnings from a window and pot, without state
// reads or transfers. Subsidy funding is handled after this split succeeds.
func planTaxSplit(funding types.RewardFundingState, priced sdk.Coins, pricings assettypes.AssetPricings) (taxSplit, error) {
	transferTaxValue, err := valueRewards(priced, pricings)
	if err != nil {
		return taxSplit{}, fmt.Errorf("valuing transfer tax: %w", err)
	}

	// The validator leg takes what its own gap still needs, capped at what the
	// pot can spare once the Oracle target is served: the tax answers the Oracle
	// target first, and subsidy covers whatever either leg is left short.
	validatorGap := shortfall(funding.ValidatorTarget, funding.ValidatorFeeValue)
	desiredValidatorTax := math.MinInt(shortfall(transferTaxValue, funding.OracleTarget), validatorGap)

	// Split each denomination proportionally by value to avoid asset skew. Positive pot/share
	// guards exclude zero division; checked products diagnose overflow before block settlement
	// spends funds.
	validatorTax := make(sdk.Coins, 0, len(priced))
	if desiredValidatorTax.IsPositive() && transferTaxValue.IsPositive() {
		for _, coin := range priced {
			if !pricings[coin.Denom].IsPriced() {
				continue
			}
			scaled, err := coin.Amount.SafeMul(desiredValidatorTax)
			if err != nil {
				return taxSplit{}, fmt.Errorf("sizing the validator share of %s: %w", coin.Denom, err)
			}
			amount := scaled.Quo(transferTaxValue)
			if amount.IsPositive() {
				validatorTax = append(validatorTax, sdk.NewCoin(coin.Denom, amount))
			}
		}
	}

	// desiredValidatorTax <= transferTaxValue bounds every share by its coin amount. Keep the
	// checked subtraction as a backstop before forming the Oracle remainder.
	oracleTax, hasNegative := priced.SafeSub(validatorTax...)
	if hasNegative {
		return taxSplit{}, fmt.Errorf(
			"validator transfer tax %s exceeds the priced pot %s",
			validatorTax,
			priced,
		)
	}

	// Both legs are valued from their own coins rather than one being derived
	// from the other, because valuation truncates once per leg and a subtracted
	// remainder would absorb that rounding instead of carrying its own.
	validatorTaxValue, err := valueRewards(validatorTax, pricings)
	if err != nil {
		return taxSplit{}, fmt.Errorf("valuing validator transfer tax: %w", err)
	}
	oracleOrganic, err := valueRewards(oracleTax, pricings)
	if err != nil {
		return taxSplit{}, fmt.Errorf("valuing Oracle transfer tax: %w", err)
	}
	validatorOrganic, err := funding.ValidatorFeeValue.SafeAdd(validatorTaxValue)
	if err != nil {
		return taxSplit{}, fmt.Errorf("adding validator organic rewards: %w", err)
	}

	return taxSplit{
		validatorTax:     validatorTax,
		oracleTax:        oracleTax,
		validatorOrganic: validatorOrganic,
		oracleOrganic:    oracleOrganic,
	}, nil
}

// sendTransferTax pays each leg its settled share out of the collector.
func (k Keeper) sendTransferTax(ctx context.Context, validator, oracle sdk.Coins) error {
	if !validator.IsZero() {
		if err := k.bankKeeper.SendCoinsFromModuleToModule(
			ctx,
			types.TransferTaxCollectorName,
			authtypes.FeeCollectorName,
			validator,
		); err != nil {
			return fmt.Errorf("allocating transfer tax to validators: %w", err)
		}
	}
	if !oracle.IsZero() {
		if err := k.bankKeeper.SendCoinsFromModuleToModule(
			ctx,
			types.TransferTaxCollectorName,
			oracletypes.ModuleName,
			oracle,
		); err != nil {
			return fmt.Errorf("allocating transfer tax to Oracle: %w", err)
		}
	}
	return nil
}

// valueRewards sums priced NOAH values and counts unpriced verdicts as zero. Arithmetic failures
// propagate: a partial sum could misallocate rewards and cannot serve as a degraded valuation.
func valueRewards(rewards sdk.Coins, pricings assettypes.AssetPricings) (math.Int, error) {
	value := math.LegacyZeroDec()
	for _, coin := range rewards {
		if !pricings[coin.Denom].IsPriced() {
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
