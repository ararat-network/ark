package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	assettypes "github.com/ararat-network/ark/x/asset/types"
	"github.com/ararat-network/ark/x/disbursement/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// validateCompensation admits NOAH and any active registry asset as a compensation denomination.
func (k *Keeper) validateCompensation(ctx context.Context, denom string) error {
	if denom == chain.NoahBaseDenom {
		return nil
	}
	asset, err := k.assetKeeper.GetAsset(ctx, denom)
	if err != nil {
		return fmt.Errorf("compensation asset: %w", err)
	}
	if asset.Status != assettypes.AssetStatus_ASSET_STATUS_ACTIVE {
		return errors.New("new compensation requires an active asset")
	}
	return nil
}

// compensationSchedule is a count of compensation periods, one for a lump. The clock reaches back at
// most a quarter, which the cap charges in the award's first window.
func (k *Keeper) compensationSchedule(ctx context.Context, startTime uint64, periods uint32) (types.Schedule, error) {
	now := chain.BlockSeconds(ctx)
	if err := types.ValidateStartTime(startTime, now); err != nil {
		return types.Schedule{}, err
	}
	if startTime+types.MaxCompensationBackdate < now {
		return types.Schedule{}, errors.New("a compensation clock starts at most a quarter before its award")
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return types.Schedule{}, err
	}
	return types.CompensationSchedule(params.CompensationPeriod, periods)
}

// valuation prices an award in the reference unit at the fresh rates Market quotes with.
type valuation struct {
	reference string
	rates     oracletypes.RateSet
}

// valuation reads the reference and the fresh rates for denom, refusing an award the oracle cannot
// value.
func (k *Keeper) valuation(ctx context.Context, denom string) (valuation, error) {
	reference, err := k.oracleKeeper.GetReferenceDenom(ctx)
	if err != nil {
		return valuation{}, err
	}
	if reference == "" {
		return valuation{}, errors.New("no reference denomination to value the award in")
	}
	rates, err := k.oracleKeeper.GetRateSet(ctx, denom, reference)
	if err != nil {
		return valuation{}, fmt.Errorf("valuing the award: %w", err)
	}
	return valuation{reference: reference, rates: rates}, nil
}

// value is what an award drew, custody in its denomination and the NOAH it converted, each rounded
// up; with no conversion it is the award's face value.
func (v valuation) value(custody sdk.Coin, offer math.Int) (math.Int, error) {
	value, err := v.rates.ConvertCeilSum(v.reference, custody, chain.NoahCoin(offer))
	if err != nil {
		return math.Int{}, fmt.Errorf("valuing the award: %w", err)
	}
	return value, nil
}

// awardValue prices a compensation award at its face value, rounded up so the whole award counts
// against the cap.
func (k *Keeper) awardValue(ctx context.Context, amount sdk.Coin) (math.Int, error) {
	v, err := k.valuation(ctx, amount.Denom)
	if err != nil {
		return math.Int{}, err
	}
	return v.value(amount, math.ZeroInt())
}

// chargeAllowance charges a committee award to its term's allowance and returns the charge: the
// award's face value, or what it drew when it converted, its custody and the NOAH the buy cost. A
// conversion's spread counts against the allowance as well as its face value, so a stolen key cannot
// spend past the allowance through a steep buy that cancelling the award would not undo.
func (k *Keeper) chargeAllowance(ctx context.Context, amount sdk.Coin, value math.Int, f funding) (math.Int, error) {
	charge := value
	if c := f.conversion; c != nil {
		v, err := k.valuation(ctx, amount.Denom)
		if err != nil {
			return math.Int{}, err
		}
		if charge, err = v.value(sdk.NewCoin(amount.Denom, f.custody), c.offer); err != nil {
			return math.Int{}, err
		}
	}
	if err := chain.ValidateAmount(charge, true); err != nil {
		return math.Int{}, fmt.Errorf("award charge: %w", err)
	}
	total, err := k.usageAfter(ctx, charge)
	if err != nil {
		return math.Int{}, err
	}
	if err := k.CompensationUsed.Set(ctx, total); err != nil {
		return math.Int{}, err
	}
	return charge, nil
}

// usageAfter is the term's usage once charge is added, refused past the allowance.
func (k *Keeper) usageAfter(ctx context.Context, charge math.Int) (math.Int, error) {
	mandate, err := k.GrantsMandate.Get(ctx)
	if err != nil {
		return math.Int{}, err
	}
	used, err := k.CompensationUsed.Get(ctx)
	if err != nil {
		return math.Int{}, err
	}
	total, err := used.SafeAdd(charge)
	if err != nil {
		return math.Int{}, err
	}
	if total.GT(mandate.CompensationAllowance) {
		return math.Int{}, fmt.Errorf("award charges %s, taking usage past the term's compensation allowance %s", charge, mandate.CompensationAllowance)
	}
	return total, nil
}

// checkCompensationCap bounds a record's compensation at the cap a window. It charges every
// compensation award to the record that is still running or was made inside the trailing window: in
// its first window, what it pays there, a window of its schedule plus its backdate; after that,
// while it runs, its value spread over its duration. A cancelled award counts for its first
// window, so a cancel restores no room inside it, and then stops, as it stops paying. A clock
// that has not started yet counts from the award.
func (k *Keeper) checkCompensationCap(ctx context.Context, beneficiary sdk.AccAddress, value math.Int, schedule types.Schedule, start, at uint64) error {
	p, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	// Times are bounded, so the sums fit.
	firstWindow := func(start, awarded uint64) uint64 {
		return p.CompensationWindow + types.CompensationBackdate(start, awarded)
	}
	charged, err := types.CompensationCharge(value, schedule.Duration(), firstWindow(start, at))
	if err != nil {
		return err
	}
	if err := k.GrantsByBeneficiary.Walk(ctx, collections.NewPrefixedPairRange[sdk.AccAddress, uint64](beneficiary), func(key collections.Pair[sdk.AccAddress, uint64], _ bool) (bool, error) {
		g, err := k.Grants.Get(ctx, key.K2())
		if err != nil {
			return true, err
		}
		if g.Kind != types.GrantKind_GRANT_KIND_COMPENSATION {
			return false, nil
		}
		recent := g.CreatedTime+p.CompensationWindow > at
		running := !g.Cancelled() && g.StartTime+g.Schedule.Duration() > at
		if !recent && !running {
			return false, nil
		}
		span := p.CompensationWindow
		if recent {
			span = firstWindow(g.StartTime, g.CreatedTime)
		}
		charge, err := types.CompensationCharge(g.AwardValue, g.Schedule.Duration(), span)
		if err != nil {
			return true, err
		}
		charged, err = charged.SafeAdd(charge)
		return err != nil, err
	}); err != nil {
		return err
	}
	if charged.GT(p.CompensationCap) {
		return fmt.Errorf("award takes the record's charged compensation to %s, past the compensation cap of %s", charged, p.CompensationCap)
	}
	return nil
}
