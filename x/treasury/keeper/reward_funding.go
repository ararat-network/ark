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
	reservetypes "ark/x/reserve/types"
	"ark/x/treasury/types"
)

// taxSplit is how a closed window's priced stability tax divides between the
// validator and Oracle legs, with what each leg earned organically once its
// share is counted. Deriving it is kept separate from spending it so the
// arithmetic that settles who is owed what for a whole window is reachable from
// plain inputs, with no balance to read or transfer to mock.
type taxSplit struct {
	validatorTax     sdk.Coins
	oracleTax        sdk.Coins
	validatorOrganic math.Int
	oracleOrganic    math.Int
}

// advanceRewardFunding accrues this block into the reward-funding window and
// settles the window once it closes, leaving fresh accounting behind for the
// next one. The whole window state machine lives here so the ABCI hook is not
// the place that knows when a window opens, closes, or resets.
//
// Genesis height accrues nothing. Accrual values the fee collector, which at
// BeginBlock holds the previous block's fees; height 1 has no previous block,
// so opening a window there would charge it for a block whose organic revenue
// is structurally absent rather than merely zero.
func (k Keeper) advanceRewardFunding(ctx context.Context) error {
	if sdk.UnwrapSDKContext(ctx).BlockHeight() <= 1 {
		return nil
	}

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

// settleRewardFunding closes a window: it gathers the collected stability tax,
// plans the split and any subsidy against the window's targets, and then moves
// the value. Nothing is transferred before the whole plan is known.
func (k Keeper) settleRewardFunding(ctx context.Context, funding types.RewardFundingState) error {
	stabilityTax := k.bankKeeper.GetAllBalances(
		ctx,
		k.accountKeeper.GetModuleAddress(types.StabilityTaxCollectorName),
	)
	pricings, err := k.assetKeeper.Pricings(ctx, nil, stabilityTax.Denoms()...)
	if err != nil {
		return fmt.Errorf("pricing stability tax: %w", err)
	}
	priced, err := k.routeUnpricedTax(ctx, stabilityTax, pricings)
	if err != nil {
		return err
	}

	// With no targets to serve there is nothing to size a split against, so the
	// tax goes to Oracle whole rather than being valued first.
	if funding.ValidatorTarget.IsZero() && funding.OracleTarget.IsZero() {
		return k.sendStabilityTax(ctx, sdk.NewCoins(), priced)
	}

	split, err := planTaxSplit(funding, priced, pricings)
	if err != nil {
		return err
	}

	// The pool is read only once the split is known sound: a window that fails
	// valuation aborts the block, and should not have charged it for a balance
	// nothing will spend. A pool short of the combined gap then pays both legs
	// the same fraction of what they are owed, rather than serving one in full
	// and starving the other. A zero combined gap never reaches that branch,
	// because no balance is negative.
	subsidyBalance := k.balance(ctx, types.SubsidyPoolName)
	validatorShortfall := shortfall(funding.ValidatorTarget, split.validatorOrganic)
	oracleShortfall := shortfall(funding.OracleTarget, split.oracleOrganic)
	validatorSubsidy, oracleSubsidy := validatorShortfall, oracleShortfall
	if totalShortfall := validatorShortfall.Add(oracleShortfall); subsidyBalance.LT(totalShortfall) {
		oracleSubsidy = subsidyBalance.Mul(oracleShortfall).Quo(totalShortfall)
		validatorSubsidy = subsidyBalance.Sub(oracleSubsidy)
	}

	if err := k.sendStabilityTax(ctx, split.validatorTax, split.oracleTax); err != nil {
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

// routeUnpricedTax partitions the collector balance by pricing
// verdict, before settlement touches it, returning the priced remainder.
// Unpriced coins follow the verdict on why the rate is missing: written-off
// and retired supply has no feed to wait for, so it moves to the strategic
// reserve, while everything else — a member's stale feed, a suspension that
// may yet recover — stays in the collector for a window that can price it.
// Unrecognised denominations defer, because value never moves on a state this
// function does not understand.
func (k Keeper) routeUnpricedTax(ctx context.Context, stabilityTax sdk.Coins, pricings assettypes.AssetPricings) (sdk.Coins, error) {
	var priced, deferred, moved sdk.Coins
	for _, coin := range stabilityTax {
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
			types.StabilityTaxCollectorName,
			reservetypes.StrategicReserveName,
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

// planTaxSplit decides how the priced stability tax divides between the
// validator and Oracle legs and what each leg therefore earned organically. It
// reads no state and moves no value, so a window and a pot fully determine the
// split. Subsidies are not its business: settlement sizes those against the
// pool once this split is known sound.
func planTaxSplit(funding types.RewardFundingState, priced sdk.Coins, pricings assettypes.AssetPricings) (taxSplit, error) {
	stabilityTaxValue, err := valueRewards(priced, pricings)
	if err != nil {
		return taxSplit{}, fmt.Errorf("valuing stability tax: %w", err)
	}

	// The validator leg takes what its own gap still needs, capped at what the
	// pot can spare once the Oracle target is served: the tax answers the Oracle
	// target first, and subsidy covers whatever either leg is left short.
	validatorGap := shortfall(funding.ValidatorTarget, funding.ValidatorFeeValue)
	desiredValidatorTax := math.MinInt(shortfall(stabilityTaxValue, funding.OracleTarget), validatorGap)

	// That share comes out denomination by denomination, pro rata by value, so
	// neither leg is handed a pot skewed towards one asset. A non-positive pot
	// or share leaves the validator leg empty, which also keeps the division
	// below away from a zero divisor.
	validatorTax := make(sdk.Coins, 0, len(priced))
	if desiredValidatorTax.IsPositive() && stabilityTaxValue.IsPositive() {
		for _, coin := range priced {
			if !pricings[coin.Denom].IsPriced() {
				continue
			}
			amount := coin.Amount.Mul(desiredValidatorTax).Quo(stabilityTaxValue)
			if amount.IsPositive() {
				validatorTax = append(validatorTax, sdk.NewCoin(coin.Denom, amount))
			}
		}
	}

	// desiredValidatorTax cannot exceed stabilityTaxValue — a shortfall is
	// bounded by its own target, and the cap above only lowers it — so every
	// pro-rata share is at most its own coin and this remainder stays
	// non-negative. It is checked rather than assumed because that invariant
	// lives in the lines above and could stop holding there.
	oracleTax, hasNegative := priced.SafeSub(validatorTax...)
	if hasNegative {
		return taxSplit{}, fmt.Errorf(
			"validator stability tax %s exceeds the priced pot %s",
			validatorTax,
			priced,
		)
	}

	// Both legs are valued from their own coins rather than one being derived
	// from the other, because valuation truncates once per leg and a subtracted
	// remainder would absorb that rounding instead of carrying its own.
	validatorTaxValue, err := valueRewards(validatorTax, pricings)
	if err != nil {
		return taxSplit{}, fmt.Errorf("valuing validator stability tax: %w", err)
	}
	oracleOrganic, err := valueRewards(oracleTax, pricings)
	if err != nil {
		return taxSplit{}, fmt.Errorf("valuing Oracle stability tax: %w", err)
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

// sendStabilityTax pays each leg its settled share out of the collector.
func (k Keeper) sendStabilityTax(ctx context.Context, validator, oracle sdk.Coins) error {
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

// valueRewards sums the NOAH value of the coins the registry could price.
// Availability is settled before valuation — an unpriced verdict counts as
// zero here, and settlement partitions the collector before valuing it — so an
// error from the remaining arithmetic is a bug or state beyond the supported
// economic domain, and it fails the block transition. That is deliberately
// harsher than the cap-refresh and liability paths, where out-of-range reads
// are an expected degraded mode with a retry to return to: those paths have a
// conservative fallback to degrade into, while a reward window valued from a
// partially summed pot would misallocate rather than degrade.
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
