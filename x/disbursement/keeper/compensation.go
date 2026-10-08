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

// awardValue prices a compensation award in the reference unit at the fresh rates Market quotes
// with, rounded up so the whole award counts against the cap.
func (k *Keeper) awardValue(ctx context.Context, amount sdk.Coin) (math.Int, error) {
	reference, err := k.oracleKeeper.GetReferenceDenom(ctx)
	if err != nil {
		return math.Int{}, err
	}
	if reference == "" {
		return math.Int{}, errors.New("no reference denomination to value the award in")
	}
	rates, err := k.oracleKeeper.GetRateSet(ctx, amount.Denom, reference)
	if err != nil {
		return math.Int{}, fmt.Errorf("valuing the award: %w", err)
	}
	value, err := rates.Convert(sdk.NewDecCoinFromCoin(amount), reference)
	if err != nil {
		return math.Int{}, fmt.Errorf("valuing the award: %w", err)
	}
	return value.Amount.Ceil().TruncateInt(), nil
}

// checkCompensationCap bounds a record's compensation at the cap a window. It charges every
// compensation award to the record that is still running or was made inside the trailing window: in
// its first window, what it pays there, a window of its schedule plus its backdate (D122); after that,
// while it runs, its value spread over its duration (D119). A cancelled award counts for its first
// window, so a cancel restores no room inside it, and then stops, as it stops paying (D109). A clock
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
