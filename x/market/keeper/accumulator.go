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

// The block's conversion accumulators. Each key holds a running total that the
// EndBlocker reads once and hands to Treasury; the transient store resets with
// the block, so nothing here is ever cleared by hand and an absent key is
// simply a block that has not seen that kind of conversion yet.
//
// Every total is already in NOAH, and holds only what settlement decides. The
// supply behind a redemption is absent because settlement divides by what was
// redeemed and credits what was minted, and no denomination answers either
// question.
var (
	eligiblePrincipalKey = []byte{0x01}
	redemptionOutputKey  = []byte{0x02}
	redeemedValueKey     = []byte{0x03}
	grossOfferKey        = []byte{0x04}
)

// recordExpansion books one expansion: the gross NOAH offer the waterfall
// places (D6), and the NOAH value of the stable supply it minted, which is
// what liability grew by and what the flow indicator reads.
//
// Preconditions the swap path has already established, and this does not
// restate: the offer is NOAH because settleSwap's branch tested it, the offer is
// a valid positive coin because quoteSwap refused anything else, and the output
// is a valid positive coin because applySpread refuses a quote that truncates to
// nothing.
//
// The valuation happens here, at the rate this conversion quoted, so the
// truncation lands once per conversion exactly as per-conversion settlement
// produced it.
func (k Keeper) recordExpansion(ctx context.Context, offer sdk.Coin, output sdk.Coin, rates oracletypes.RateSet) error {
	converted, err := rates.Convert(sdk.NewDecCoinFromCoin(output), chain.NoahBaseDenom)
	if err != nil {
		return fmt.Errorf("valuing stable output: %w", err)
	}
	eligible := converted.Amount.TruncateInt()
	// The bound no caller can have established, because it is a fact about the
	// rate rather than about the coins: an output worth more than the offer that
	// bought it would record principal the conversion never took custody of. It
	// is refused before anything is recorded, so a bad quote fails its own
	// transaction.
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

// recordRedemption accumulates one redemption: the NOAH value of the stable
// supply it burned, and the NOAH it minted against that value.
//
// Both redemption paths arrive here already valued, because settlement asks the
// same two questions of each and only the rate behind the value differs — a
// quoted redemption carries the oracle set's, a settled one the redemption
// plan's own committed rate, which never appears in the oracle set at all. That
// difference stays with the caller holding the rate, which is why nothing here
// takes a rate set: a total that merely sums what its callers decided has no
// business re-deciding it.
//
// The bound between the two is checked here rather than at settlement so a
// conversion that minted more than it retired fails its own transaction. It is
// what ultimately keeps the coverage draw inside the Buffer.
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

// addInt folds a delta into a running integer total. The addition is checked
// rather than trusted to fit: every input is bounded by a conversion Market
// already settled, but the sum over a block has no such bound of its own, and
// an overflow discovered here fails the transaction that caused it instead of
// the block that has to settle it.
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
