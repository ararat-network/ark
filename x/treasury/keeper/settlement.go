package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/decimal"
	claimstypes "github.com/ararat-network/ark/x/claims/types"
	markettypes "github.com/ararat-network/ark/x/market/types"
	reservetypes "github.com/ararat-network/ark/x/reserve/types"
	"github.com/ararat-network/ark/x/treasury/types"
)

// SettleConversions settles one block's recorded conversion flow and returns
// the NOAH Market must burn to finish it: §7's waterfall and coverage draw,
// run once per block from Market's EndBlocker. Every mint and burn the block
// performed has already landed, so liability is valued once against final
// state and no conversion's position in the block changes its placement.
//
// Errors are settlement-fatal, because Market's EndBlocker has no transaction
// left to abort: what reaches this path is state corruption or an arithmetic
// impossibility, while a valuation that could not cover every recognised
// liability parks and discloses rather than failing.
func (k Keeper) SettleConversions(ctx context.Context, totals markettypes.ConversionTotals) (math.Int, error) {
	// Ahead of both guards below, because the risk series measure elapsed time
	// rather than elapsed activity: an idle block still decays flow and still
	// records a reference price, so a quiet stretch cools the estimate instead
	// of freezing it at whatever the last converting block saw.
	if err := k.sampleExposure(ctx, totals); err != nil {
		return math.Int{}, err
	}
	if err := totals.Validate(); err != nil {
		return math.Int{}, fmt.Errorf("settling conversions: %w", err)
	}
	if totals.IsZero() {
		return math.ZeroInt(), nil
	}

	partition, err := k.liabilityPartitionValue(ctx)
	if err != nil {
		return math.Int{}, err
	}
	// A complete valuation discloses nothing: an event every settling block
	// would bury the blocks that matter.
	if !partition.complete {
		if err := k.discloseIncompleteValuation(ctx, partition); err != nil {
			return math.Int{}, err
		}
	}

	burn := math.ZeroInt()
	if totals.GrossOffer.IsPositive() {
		overflow, err := k.allocateExpansionPrincipal(ctx, totals.GrossOffer, partition)
		if err != nil {
			return math.Int{}, err
		}
		burn = burn.Add(overflow)
	}

	// The draw reads the Buffer after the waterfall credited it, so a block's
	// expansions replenish the Buffer its redemptions then draw against.
	if totals.RedemptionOutput.IsPositive() {
		drawn, err := k.drawRedemptionCoverage(ctx, totals, partition)
		if err != nil {
			return math.Int{}, err
		}
		burn = burn.Add(drawn)
	}

	return burn, nil
}

// allocateExpansionPrincipal places the block's gross expansion offer —
// principal and spread alike (D6) — down the fund waterfall and returns what
// overflowed every funded target.
func (k Keeper) allocateExpansionPrincipal(ctx context.Context, offer math.Int, partition liabilityPartition) (math.Int, error) {
	// An incomplete aggregate cannot answer what capital a fund is owed, so the
	// whole block's offer parks in the Reserve. That stays revisable — the
	// Reserve's Buffer commitment is authority-gated and reads only its own
	// balance (§6.4) — where principal committed to the Buffer is not, and
	// principal burned is less so.
	bufferCredit := math.ZeroInt()
	reserveCredit := offer
	insuranceCredit := math.ZeroInt()
	overflowBurn := math.ZeroInt()
	if partition.complete {
		policy, err := k.EconomicPolicy.Get(ctx)
		if err != nil {
			return math.Int{}, fmt.Errorf("getting economic policy: %w", err)
		}
		// Each committee-operated fund is asked what it is worth rather than read
		// behind its back; the Buffer has no operator to ask, so its balance is
		// read directly.
		insuranceBalance, err := k.claimsKeeper.RecognisedCapital(ctx)
		if err != nil {
			return math.Int{}, fmt.Errorf("getting Insurance recognised capital: %w", err)
		}
		reserveBalance, err := k.reserveKeeper.RecognisedCapital(ctx)
		if err != nil {
			return math.Int{}, fmt.Errorf("getting Reserve recognised capital: %w", err)
		}
		bufferBalance := k.getBalance(ctx, types.RedemptionBufferName)
		// The net basis, because this is a flow rather than a bound: no claim
		// arrives from paper the Reserve holds (D67), so filling a gap against it
		// would sequester principal that should overflow-burn.
		net, err := partition.net()
		if err != nil {
			return math.Int{}, err
		}
		// The targets' basis scales; nothing else here does. The draw below
		// divides by the raw partition on purpose (D73) — scaling a payment
		// denominator would ration the exits the Buffer exists to fund — so the
		// multiplier is applied to this figure alone and never to the partition.
		basis, err := k.exposureAdjusted(ctx, net)
		if err != nil {
			return math.Int{}, err
		}
		targets := policy.FundTargets(basis)

		remaining := offer
		bufferCredit = math.MinInt(remaining, shortfall(targets.Buffer, bufferBalance))
		remaining = remaining.Sub(bufferCredit)
		reserveCredit = math.MinInt(remaining, shortfall(targets.Reserve, reserveBalance))
		remaining = remaining.Sub(reserveCredit)
		insuranceCredit = math.MinInt(remaining, shortfall(targets.Insurance, insuranceBalance))
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
			return math.Int{}, fmt.Errorf("crediting %s: %w", credit.module, err)
		}
	}

	// The split alone cannot say whether principal parked: it is byte-identical
	// to a complete valuation whose Buffer and Insurance were already full. What
	// distinguishes them is EventLiabilityIncomplete, which settlement emitted
	// for this same block before allocating.
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventExpansionAllocated{
		Denom:                  chain.NoahBaseDenom,
		RedemptionBufferCredit: bufferCredit,
		StrategicReserveCredit: reserveCredit,
		InsuranceCredit:        insuranceCredit,
		OverflowBurn:           overflowBurn,
	}); err != nil {
		return math.Int{}, fmt.Errorf("emitting Treasury expansion allocation event: %w", err)
	}

	return overflowBurn, nil
}

// drawRedemptionCoverage funds the Buffer's coverage share of everything the
// block redeemed, and returns what Market must burn against output it has
// already minted. It never reads the completeness flag, because the claimable
// aggregate already excludes the supply that cannot redeem — a suspension
// elsewhere raises coverage for healthy exits rather than switching the Buffer
// off during the contagion it was built for.
func (k Keeper) drawRedemptionCoverage(ctx context.Context, totals markettypes.ConversionTotals, partition liabilityPartition) (math.Int, error) {
	// Coverage divides the Buffer across the claims that could arrive when the
	// block's redemptions quoted, so the basis is pre-burn liability: the scan
	// runs after those burns, and adding the retired value back reconstructs it.
	// Reserve-held paper stays out, as it does for every flow — the Reserve has
	// no path to Market, so counting it would pay real holders' shares away to a
	// claimant that never arrives.
	net, err := partition.net()
	if err != nil {
		return math.Int{}, err
	}
	basis, err := decimal.Add(net, totals.RedeemedValue)
	if err != nil {
		return math.Int{}, fmt.Errorf("reconstructing the pre-burn coverage basis: %w", err)
	}
	if !basis.IsPositive() {
		return math.Int{}, fmt.Errorf(
			"redeemed liability %s left no coverage basis",
			totals.RedeemedValue,
		)
	}

	// The Buffer's share of the output, multiplied before it is divided, which
	// is the safety property rather than a preference: forming the ratio first
	// would round an intermediate against its own bound of one, and the output
	// would then amplify that into a draw above the balance it is paid from.
	// Multiplying leaves one rounding on a whole-anoah quantity, and monotone
	// rounding cannot cross an exactly-representable bound, so both checks below
	// hold however the last place falls.
	bufferBalance := k.getBalance(ctx, types.RedemptionBufferName)
	share, err := totals.RedemptionOutput.SafeMul(bufferBalance)
	if err != nil {
		return math.Int{}, fmt.Errorf("valuing the Buffer's share of the block's output: %w", err)
	}
	covered, err := decimal.Quo(math.LegacyNewDecFromInt(share), basis)
	if err != nil {
		return math.Int{}, fmt.Errorf("calculating Buffer-funded output: %w", err)
	}
	// Coverage caps at one: a Buffer at least the size of the claims against it
	// funds the whole output and no more.
	drawn := math.MinInt(totals.RedemptionOutput, covered.TruncateInt())
	// Unreachable: the block's output never exceeds the basis
	// (ConversionTotals.Validate bounds it by the redeemed value inside it), so
	// the share never exceeds the Buffer and one monotone rounding cannot lift it
	// past a whole-anoah bound. Checked anyway, as the last point before the
	// Buffer is debited.
	if drawn.GT(bufferBalance) {
		return math.Int{}, fmt.Errorf(
			"buffer draw %s exceeds the Buffer balance %s",
			drawn,
			bufferBalance,
		)
	}

	if drawn.IsPositive() {
		if err := k.bankKeeper.SendCoinsFromModuleToModule(
			ctx,
			types.RedemptionBufferName,
			markettypes.ModuleName,
			chain.NoahCoins(drawn),
		); err != nil {
			return math.Int{}, fmt.Errorf("drawing redemption buffer: %w", err)
		}
	}

	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventRedemptionBufferDrawn{
		Denom:   chain.NoahBaseDenom,
		Payment: drawn,
	}); err != nil {
		return math.Int{}, fmt.Errorf("emitting Treasury redemption buffer event: %w", err)
	}

	return drawn, nil
}
