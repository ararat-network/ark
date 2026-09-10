package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/decimal"
	"github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
)

// Transient NOAH totals accumulate expansion and redemption facts for EndBlock settlement. The
// store resets each block; an absent key means no contribution of that kind.
var (
	eligiblePrincipalKey = []byte{0x01}
	redemptionOutputKey  = []byte{0x02}
	redeemedValueKey     = []byte{0x03}
	grossOfferKey        = []byte{0x04}
)

// recordExpansion accumulates gross NOAH offered and the quoted NOAH value of minted stable supply.
// The swap path guarantees valid positive coins and a NOAH offer; valuation truncates once per
// conversion.
func (k Keeper) recordExpansion(ctx context.Context, offer sdk.Coin, output sdk.Coin, rates oracletypes.RateSet) error {
	converted, err := rates.Convert(sdk.NewDecCoinFromCoin(output), chain.NoahBaseDenom)
	if err != nil {
		return fmt.Errorf("valuing stable output: %w", err)
	}
	eligible := converted.Amount.TruncateInt()
	// Reject minted value above custody received before recording totals, so a bad quote fails its
	// transaction rather than block settlement.
	if eligible.GT(offer.Amount) {
		return fmt.Errorf(
			"stable output value %s exceeds gross offer %s",
			chain.NoahCoin(eligible),
			offer,
		)
	}
	if err := k.addInt(ctx, grossOfferKey, offer.Amount); err != nil {
		return fmt.Errorf("accumulating gross offer: %w", err)
	}
	if err := k.addInt(ctx, eligiblePrincipalKey, eligible); err != nil {
		return fmt.Errorf("accumulating eligible principal: %w", err)
	}

	return nil
}

// recordRedemption accumulates burned liability value and NOAH minted. Callers value ordinary and
// settlement redemptions at their respective rates. Rejecting mint above retired value here bounds
// the later coverage draw.
func (k Keeper) recordRedemption(ctx context.Context, redeemedValue math.LegacyDec, output math.Int) error {
	if math.LegacyNewDecFromInt(output).GT(redeemedValue) {
		return fmt.Errorf("NOAH output %s exceeds redeemed liability %s", output, redeemedValue)
	}

	if err := k.addDec(ctx, redeemedValueKey, redeemedValue); err != nil {
		return fmt.Errorf("accumulating redeemed liability: %w", err)
	}
	if err := k.addInt(ctx, redemptionOutputKey, output); err != nil {
		return fmt.Errorf("accumulating redemption output: %w", err)
	}

	return nil
}

// conversionTotals reads the block's recorded flow back.
func (k Keeper) conversionTotals(ctx context.Context) (types.ConversionTotals, error) {
	grossOffer, err := k.readInt(ctx, grossOfferKey)
	if err != nil {
		return types.ConversionTotals{}, fmt.Errorf("reading gross offer: %w", err)
	}
	eligiblePrincipal, err := k.readInt(ctx, eligiblePrincipalKey)
	if err != nil {
		return types.ConversionTotals{}, fmt.Errorf("reading eligible principal: %w", err)
	}
	redemptionOutput, err := k.readInt(ctx, redemptionOutputKey)
	if err != nil {
		return types.ConversionTotals{}, fmt.Errorf("reading redemption output: %w", err)
	}
	redeemedValue, err := k.readDec(ctx, redeemedValueKey)
	if err != nil {
		return types.ConversionTotals{}, fmt.Errorf("reading redeemed liability: %w", err)
	}

	return types.ConversionTotals{
		GrossOffer:        grossOffer,
		EligiblePrincipal: eligiblePrincipal,
		RedemptionOutput:  redemptionOutput,
		RedeemedValue:     redeemedValue,
	}, nil
}

// addInt checks running-total addition so overflow fails the contributing transaction, before
// EndBlock consumes the total. Individual conversion bounds do not bound the whole block sum.
func (k Keeper) addInt(ctx context.Context, key []byte, delta math.Int) error {
	current, err := k.readInt(ctx, key)
	if err != nil {
		return err
	}
	total, err := current.SafeAdd(delta)
	if err != nil {
		return err
	}
	encoded, err := total.Marshal()
	if err != nil {
		return err
	}

	return k.transientStoreService.OpenTransientStore(ctx).Set(key, encoded)
}

func (k Keeper) addDec(ctx context.Context, key []byte, delta math.LegacyDec) error {
	current, err := k.readDec(ctx, key)
	if err != nil {
		return err
	}
	total, err := decimal.Add(current, delta)
	if err != nil {
		return err
	}
	encoded, err := total.Marshal()
	if err != nil {
		return err
	}

	return k.transientStoreService.OpenTransientStore(ctx).Set(key, encoded)
}

// readInt reads a running total, treating an absent key as a block that has not
// recorded this kind of conversion yet.
func (k Keeper) readInt(ctx context.Context, key []byte) (math.Int, error) {
	bz, err := k.transientStoreService.OpenTransientStore(ctx).Get(key)
	if err != nil {
		return math.Int{}, err
	}
	if bz == nil {
		return math.ZeroInt(), nil
	}

	var value math.Int
	if err := value.Unmarshal(bz); err != nil {
		return math.Int{}, err
	}

	return value, nil
}

func (k Keeper) readDec(ctx context.Context, key []byte) (math.LegacyDec, error) {
	bz, err := k.transientStoreService.OpenTransientStore(ctx).Get(key)
	if err != nil {
		return math.LegacyDec{}, err
	}
	if bz == nil {
		return math.LegacyZeroDec(), nil
	}

	var value math.LegacyDec
	if err := value.Unmarshal(bz); err != nil {
		return math.LegacyDec{}, err
	}

	return value, nil
}
