package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	chain "ark/pkg/chain"
	"ark/pkg/decimal"
	oracletypes "ark/x/oracle/types"
)

var liabilitySnapshotKey = []byte{0x01}

// RecordSupplyChange updates an existing block-local liability snapshot after
// Market has successfully applied its burn and mint. It deliberately does not
// build a snapshot when none exists; a later valuation will read current Bank
// supply directly.
func (k Keeper) RecordSupplyChange(ctx context.Context, burned sdk.Coin, minted sdk.Coin, rates oracletypes.RateSet) error {
	if err := burned.Validate(); err != nil {
		return fmt.Errorf("invalid burned coin: %w", err)
	}
	if err := minted.Validate(); err != nil {
		return fmt.Errorf("invalid minted coin: %w", err)
	}

	liability, found, err := k.loadLiabilitySnapshot(ctx)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}

	burnedValue, err := liabilityCoinValue(burned, rates)
	if err != nil {
		return fmt.Errorf("valuing burned supply %s: %w", burned, err)
	}
	liability, err = decimal.Sub(liability, burnedValue)
	if err != nil {
		return fmt.Errorf("subtracting burned supply %s from cached liability: %w", burned, err)
	}
	if liability.IsNegative() {
		return fmt.Errorf("burned supply %s exceeds cached liability", burned)
	}

	mintedValue, err := liabilityCoinValue(minted, rates)
	if err != nil {
		return fmt.Errorf("valuing minted supply %s: %w", minted, err)
	}
	liability, err = decimal.Add(liability, mintedValue)
	if err != nil {
		return fmt.Errorf("adding minted supply %s to cached liability: %w", minted, err)
	}

	return k.storeLiabilitySnapshot(ctx, liability)
}

func (k Keeper) nominalLiabilityValue(
	ctx context.Context,
	tobinTaxes []oracletypes.TobinTax,
	rates oracletypes.RateSet,
) (math.LegacyDec, bool, error) {
	if rates == nil {
		rates = oracletypes.RateSet{}
	}
	coins := make([]sdk.Coin, 0, len(tobinTaxes))
	needed := make([]string, 0, len(tobinTaxes))
	for _, tax := range tobinTaxes {
		supply := k.bankKeeper.GetSupply(ctx, tax.Denom)
		if supply.Amount.IsPositive() {
			coins = append(coins, supply)
			if _, ok := rates[tax.Denom]; !ok {
				needed = append(needed, tax.Denom)
			}
		}
	}
	if len(needed) > 0 {
		captured, err := k.oracleKeeper.GetRateSet(ctx, needed...)
		if err != nil {
			if isValuationUnavailable(err) {
				return math.LegacyZeroDec(), false, nil
			}
			return math.LegacyDec{}, false, fmt.Errorf("capturing aggregate liability rates: %w", err)
		}
		for denom, rate := range captured {
			if _, exists := rates[denom]; !exists {
				rates[denom] = rate
			}
		}
	}

	total := math.LegacyZeroDec()
	for _, coin := range coins {
		converted, err := rates.Convert(sdk.NewDecCoinFromCoin(coin), chain.NoahBaseDenom)
		if err != nil {
			if isValuationUnavailable(err) {
				return math.LegacyZeroDec(), false, nil
			}
			return math.LegacyDec{}, false, fmt.Errorf("valuing liability %s: %w", coin.Denom, err)
		}
		total, err = decimal.Add(total, converted.Amount)
		if errors.Is(err, decimal.ErrOutOfRange) {
			return math.LegacyZeroDec(), false, nil
		}
		if err != nil {
			return math.LegacyDec{}, false, fmt.Errorf("summing liability %s: %w", coin.Denom, err)
		}
	}
	return total, true, nil
}

// cachedLiabilityValue returns the current block's aggregate stable liability.
// The first caller derives it from Bank and Oracle state; later callers reuse
// the transient snapshot maintained by Market supply changes.
func (k Keeper) cachedLiabilityValue(
	ctx context.Context,
	tobinTaxes []oracletypes.TobinTax,
	rates oracletypes.RateSet,
) (math.LegacyDec, bool, error) {
	liability, found, err := k.loadLiabilitySnapshot(ctx)
	if err != nil {
		return math.LegacyDec{}, false, err
	}
	if found {
		return liability, true, nil
	}

	liability, complete, err := k.nominalLiabilityValue(ctx, tobinTaxes, rates)
	if err != nil {
		return math.LegacyDec{}, false, err
	}
	if !complete {
		return liability, false, nil
	}
	if err := k.storeLiabilitySnapshot(ctx, liability); err != nil {
		return math.LegacyDec{}, false, err
	}
	return liability, true, nil
}

func liabilityCoinValue(coin sdk.Coin, rates oracletypes.RateSet) (math.LegacyDec, error) {
	if coin.Denom == chain.NoahBaseDenom || coin.Amount.IsZero() {
		return math.LegacyZeroDec(), nil
	}

	converted, err := rates.Convert(sdk.NewDecCoinFromCoin(coin), chain.NoahBaseDenom)
	if err != nil {
		return math.LegacyDec{}, err
	}
	return converted.Amount, nil
}

func (k Keeper) loadLiabilitySnapshot(ctx context.Context) (math.LegacyDec, bool, error) {
	store := k.transientStoreService.OpenTransientStore(ctx)
	bz, err := store.Get(liabilitySnapshotKey)
	if err != nil {
		return math.LegacyDec{}, false, fmt.Errorf("getting cached liability: %w", err)
	}
	if bz == nil {
		return math.LegacyDec{}, false, nil
	}
	if len(bz) == 0 {
		return math.LegacyDec{}, false, fmt.Errorf("cached liability is empty")
	}

	var liability math.LegacyDec
	if err := liability.Unmarshal(bz); err != nil {
		return math.LegacyDec{}, false, fmt.Errorf("decoding cached liability: %w", err)
	}
	if liability.IsNil() || liability.IsNegative() || !liability.IsInValidRange() {
		return math.LegacyDec{}, false, fmt.Errorf("cached liability is invalid")
	}
	return liability, true, nil
}

func (k Keeper) storeLiabilitySnapshot(ctx context.Context, liability math.LegacyDec) error {
	if liability.IsNil() || liability.IsNegative() || !liability.IsInValidRange() {
		return fmt.Errorf("cannot cache invalid liability")
	}
	value, err := liability.Marshal()
	if err != nil {
		return fmt.Errorf("encoding cached liability: %w", err)
	}

	store := k.transientStoreService.OpenTransientStore(ctx)
	if err := store.Set(liabilitySnapshotKey, value); err != nil {
		return fmt.Errorf("setting cached liability: %w", err)
	}
	return nil
}
