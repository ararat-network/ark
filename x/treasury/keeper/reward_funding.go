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

// taxSplit is how a closed window's priced transfer tax divides between the
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
// the place that knows when a window opens, closes, or resets. At EndBlock
// the fee collector holds this block's own fees, so every real block accrues,
// the first included; the guard covers only a hook driven before any block
// exists.
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

	// The targets accumulate for a whole window, and the two ceilings on their
	// inputs are what keep that safe: a per-block target under
	// MaxBlockRewardTarget, over a window under MaxRewardFundingWindow, tops
	// out ninety-five bits below the integer limit. Both are refused at the
	// write, so no policy or params value reaching here can overflow these
	// sums. The additions stay checked because this runs in an EndBlocker,
	// where the alternative to an error is a panicking block — but nothing
	// depends on them firing.
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

	// The pool is read only once the split is known sound: a window that fails
	// valuation aborts the block, and should not have charged it for a balance
	// nothing will spend. A pool short of the combined gap then pays both legs
	// the same fraction of what they are owed, rather than serving one in full
	// and starving the other. A zero combined gap never reaches that branch,
	// because no balance is negative.
	subsidyBalance := k.getBalance(ctx, types.SubsidyPoolName)
	validatorShortfall := shortfall(funding.ValidatorTarget, split.validatorOrganic)
	oracleShortfall := shortfall(funding.OracleTarget, split.oracleOrganic)
	validatorSubsidy, oracleSubsidy := validatorShortfall, oracleShortfall
	totalShortfall, err := validatorShortfall.SafeAdd(oracleShortfall)
	if err != nil {
		return fmt.Errorf("summing the window's reward shortfalls: %w", err)
	}
	if subsidyBalance.LT(totalShortfall) {
		// The pool is shared pro rata by the Oracle leg's share of the combined
		// gap. The product is checked rather than trusted to fit: both factors
		// are chain balances with no ceiling of their own, and this runs in a
		// block hook where an overflow would panic the block rather than fail
		// a transaction. The division is safe — this branch cannot be entered
		// with a zero total, because no balance is negative — and floors, so
		// the validator remainder below stays non-negative and the two legs
		// together spend exactly the pool.
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

// routeUnpricedTax partitions the collector balance by pricing
// verdict, before settlement touches it, returning the priced remainder.
// Unpriced coins follow the verdict on why the rate is missing: written-off
// and retired supply has no feed to wait for, so it moves to the strategic
// reserve, while everything else — a member's stale feed, a suspension that
// may yet recover — stays in the collector for a window that can price it.
//
// The deferring arm has two live inhabitants, both members: an unavailable
// feed and a suspension carrying no settlement plan. An unrecognised verdict
// is not one of them, because every coin that can reach this account is NOAH
// or a registry member — tax is collected only in capped denominations and a
// cap names a member, genesis validates both the cap set and any seeded
// collector balance against the registry, and no other door into the account
// exists: it is a blocked address, so the ante handler's routing out of the
// fee collector is the sole inbound path.
//
// The arm still catches it, because what makes it unreachable is an invariant
// held elsewhere, and deferring is the only safe reading of a state this
// function does not understand: value never moves on one. Note that the fee
// collector, valued a step earlier by updateRewardFunding, has no such
// invariant — fee denominations are unrestricted — so unrecognised verdicts
// are ordinary there and valueRewards counts them at zero.
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

// planTaxSplit decides how the priced transfer tax divides between the
// validator and Oracle legs and what each leg therefore earned organically. It
// reads no state and moves no value, so a window and a pot fully determine the
// split. Subsidies are not its business: settlement sizes those against the
// pool once this split is known sound.
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

	// That share comes out denomination by denomination, pro rata by value, so
	// neither leg is handed a pot skewed towards one asset. A non-positive pot
	// or share leaves the validator leg empty, which also keeps the division
	// below away from a zero divisor. The product is checked for the same
	// reason the subsidy split's is: a balance times a target has no ceiling of
	// its own, and this is reached from an EndBlocker.
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

	// desiredValidatorTax cannot exceed transferTaxValue — a shortfall is
	// bounded by its own target, and the cap above only lowers it — so every
	// pro-rata share is at most its own coin and this remainder stays
	// non-negative. It is checked rather than assumed because that invariant
	// lives in the lines above and could stop holding there.
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
