package keeper

import (
	"context"
	"fmt"
	"sort"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	"github.com/ararat-network/ark/x/reserve/types"
)

// AssetRecognitions returns one row per asset over the union of the
// eligibility set and everything attested. Credits are solved jointly by
// SolveRecognition rather than clipped independently, and an unusable rate
// degrades its own row to zero. Rows are ordered by denomination so the fold
// is deterministic.
func (k Keeper) AssetRecognitions(ctx context.Context) ([]types.AssetRecognition, error) {
	entries := make(map[string]types.EligibilityEntry)
	eligibleDenoms := make([]string, 0)
	if err := k.RecognitionPolicy.Walk(ctx, nil, func(denom string, entry types.EligibilityEntry) (bool, error) {
		entries[denom] = entry
		eligibleDenoms = append(eligibleDenoms, denom)
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating Reserve recognition policy: %w", err)
	}

	// One walk of the open set serves every asset. Impaired positions are open,
	// so they are tallied separately: excluded from credit but still reported.
	attested := make(map[string]math.Int)
	impaired := make(map[string]math.Int)
	if err := k.OpenPositions.Walk(ctx, nil, func(_ uint64, position types.Position) (bool, error) {
		quantities := attested
		if position.Impaired {
			quantities = impaired
		}
		sum := position.Quantity.Amount
		if running, ok := quantities[position.Quantity.Denom]; ok {
			updated, err := running.SafeAdd(position.Quantity.Amount)
			if err != nil {
				return false, fmt.Errorf("summing attested quantity of %s: %w", position.Quantity.Denom, err)
			}
			sum = updated
		}
		quantities[position.Quantity.Denom] = sum
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("iterating positions: %w", err)
	}

	denomSet := make(map[string]struct{}, len(entries)+len(attested)+len(impaired))
	for denom := range entries {
		denomSet[denom] = struct{}{}
	}
	for denom := range attested {
		denomSet[denom] = struct{}{}
	}
	for denom := range impaired {
		denomSet[denom] = struct{}{}
	}
	if len(denomSet) == 0 {
		return nil, nil
	}

	// Rates are read once for the whole fold, one request per entry carrying
	// that entry's own denomination and staleness window.
	requests := make([]oracletypes.RateRequest, 0, len(eligibleDenoms))
	for _, denom := range eligibleDenoms {
		requests = append(requests, oracletypes.RateRequest{
			Denom:  denom,
			MaxAge: entries[denom].MaxRateAge,
		})
	}

	rates := oracletypes.NewRateSet()
	if len(requests) > 0 {
		var err error
		rates, err = k.oracleKeeper.GetRateSetWithin(ctx, requests)
		if err != nil {
			return nil, fmt.Errorf("getting Reserve asset rates: %w", err)
		}
	}

	denoms := make([]string, 0, len(denomSet))
	for denom := range denomSet {
		denoms = append(denoms, denom)
	}
	sort.Strings(denoms)

	assets := make([]types.AssetRecognition, 0, len(denoms))
	stakes := make([]types.RecognitionStake, 0, len(denoms))
	for _, denom := range denoms {
		row := types.AssetRecognition{
			Denom:               denom,
			AttestedQuantity:    math.ZeroInt(),
			ImpairedQuantity:    math.ZeroInt(),
			Rate:                math.LegacyZeroDec(),
			HaircutFactor:       math.LegacyZeroDec(),
			RecognitionCapRatio: math.LegacyZeroDec(),
			EffectiveCap:        chain.NoahCoin(math.ZeroInt()),
			Credit:              chain.NoahCoin(math.ZeroInt()),
		}
		if quantity, ok := attested[denom]; ok {
			row.AttestedQuantity = quantity
		}
		if quantity, ok := impaired[denom]; ok {
			row.ImpairedQuantity = quantity
		}
		stake := types.RecognitionStake{Ratio: math.LegacyZeroDec(), Raw: math.LegacyZeroDec()}
		if entry, listed := entries[denom]; listed {
			row.Eligible = true
			row.HaircutFactor = entry.HaircutFactor
			row.RecognitionCapRatio = entry.RecognitionCapRatio
			stake.Ratio = entry.RecognitionCapRatio
			// An absent, non-positive, or over-age rate leaves the row at zero:
			// there is no fallback source.
			if rate, usable := rates[denom]; usable {
				row.Rate = rate
				raw, err := entry.RawCredit(rates, row.AttestedQuantity)
				if err != nil {
					return nil, err
				}
				stake.Raw = raw
			}
		}
		assets = append(assets, row)
		stakes = append(stakes, stake)
	}

	// The solve levers only the par-counted NOAH balance, the one
	// bank-verifiable term.
	solved, err := types.SolveRecognition(k.balance(ctx), stakes)
	if err != nil {
		return nil, fmt.Errorf("solving Reserve recognition: %w", err)
	}
	for i, result := range solved {
		assets[i].EffectiveCap = chain.NoahCoin(result.Ceiling)
		assets[i].Credit = chain.NoahCoin(result.Credit)
	}

	return assets, nil
}

// valueMovement prices one proven coin movement in anoah at this block's
// rate, refusing rather than guessing when no usable rate exists. NOAH is par;
// everything else converts through the rate set, which quotes units per one
// NOAH, so valuing in NOAH divides. The quotient is checked because a small
// enough rate leaves representable range, which must be an error rather than a
// panic.
func (k Keeper) valueMovement(ctx context.Context, coin sdk.Coin) (math.Int, error) {
	if coin.Denom == chain.NoahBaseDenom {
		return coin.Amount, nil
	}
	// The all-or-nothing read, not the available one: a movement derives
	// exactly one value per denomination it names, and has no unvalued outcome
	// to degrade to. Its typed refusal also separates a feed the Oracle has
	// never priced from one that has gone dark, which a verdict re-derived from
	// an omitted key cannot say.
	rates, err := k.oracleKeeper.GetRateSet(ctx, coin.Denom)
	if err != nil {
		return math.Int{}, fmt.Errorf(
			"no fresh price feed for %s: a Reserve movement must be booked at a real valuation: %w",
			coin.Denom,
			err,
		)
	}
	rate := rates[coin.Denom]
	if !rate.IsPositive() {
		return math.Int{}, fmt.Errorf(
			"no usable price feed for %s: a Reserve movement must be booked at a real valuation",
			coin.Denom,
		)
	}

	value, err := rates.Convert(sdk.NewDecCoinFromCoin(coin), chain.NoahBaseDenom)
	if err != nil {
		return math.Int{}, fmt.Errorf("valuing %s in anoah: %w", coin, err)
	}
	return value.Amount.TruncateInt(), nil
}

// sumRecognised folds asset credits onto the par-counted balance.
func sumRecognised(balance math.Int, assets []types.AssetRecognition) (math.Int, error) {
	recognised := balance
	for _, asset := range assets {
		var err error
		recognised, err = recognised.SafeAdd(asset.Credit.Amount)
		if err != nil {
			return math.Int{}, fmt.Errorf("summing recognised capital: %w", err)
		}
	}
	return recognised, nil
}
