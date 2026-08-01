package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	markettypes "ark/x/market/types"
	"ark/x/treasury/types"
)

const maxTaxMessageDepth = 32

// ComputeTax calculates stability tax with the cap applied independently to
// each message input.
func (k Keeper) ComputeTax(ctx context.Context, msgs []sdk.Msg) (sdk.Coins, error) {
	var inputs []sdk.Coins
	for i, msg := range msgs {
		if err := extractTaxInputs(msg, &inputs, 0); err != nil {
			return nil, errorsmod.Wrapf(types.ErrInvalidTaxMessage, "message %d: %v", i, err)
		}
	}
	if len(inputs) == 0 {
		return sdk.NewCoins(), nil
	}

	policy, err := k.MonetaryPolicy.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting monetary policy: %w", err)
	}

	if policy.StabilityTaxRate.IsZero() {
		return sdk.NewCoins(), nil
	}

	// The tax base is the cap set: a denomination is taxed exactly when
	// Treasury holds a cap for it. Caps are derived from priced-live
	// membership and then kept, so lifecycle status governs what a cap is
	// worth and never whether transfers of outstanding supply are taxed —
	// suspended, written-off, and retirement-residual supply all still move
	// between holders, and a transfer tax that exempted them would price
	// distress below ordinary money.
	caps := make(map[string]math.Int)
	taxAmounts := make(map[string]math.Int)
	for _, input := range inputs {
		for _, principal := range input {
			cap, loaded := caps[principal.Denom]
			if !loaded {
				stored, err := k.TaxCaps.Get(ctx, principal.Denom)
				switch {
				case err == nil:
					if stored.IsNegative() {
						return nil, fmt.Errorf("negative tax cap %s for denom %s", stored, principal.Denom)
					}
					cap = stored
				case errors.Is(err, collections.ErrNotFound):
					// No cap has ever been derived for this denomination: it
					// is outside the registry, or a member whose first rebuild
					// has not landed. Untaxed either way, because taxing
					// uncapped would be unbounded and rejecting would let a
					// stalled refresh block transfers. A nil entry memoises
					// the miss for the rest of the transaction.
					cap = math.Int{}
				default:
					return nil, fmt.Errorf("getting tax cap for denom %s: %w", principal.Denom, err)
				}
				caps[principal.Denom] = cap
			}
			if cap.IsNil() {
				continue
			}

			tax := policy.StabilityTaxRate.MulInt(principal.Amount).TruncateInt()
			if cap.IsPositive() && tax.GT(cap) {
				tax = cap
			}
			if tax.IsPositive() {
				current, found := taxAmounts[principal.Denom]
				if !found {
					current = math.ZeroInt()
				}
				tax, err = current.SafeAdd(tax)
				if err != nil {
					return nil, errorsmod.Wrapf(
						types.ErrTaxOutOfRange,
						"summing stability tax for denom %s: %v",
						principal.Denom,
						err,
					)
				}
				taxAmounts[principal.Denom] = tax
			}
		}
	}

	taxes := make([]sdk.Coin, 0, len(taxAmounts))
	for denom, amount := range taxAmounts {
		taxes = append(taxes, sdk.NewCoin(denom, amount))
	}
	return sdk.NewCoins(taxes...), nil
}

func extractTaxInputs(msg sdk.Msg, inputs *[]sdk.Coins, depth int) error {
	if msg == nil {
		return fmt.Errorf("nil SDK message")
	}
	if depth > maxTaxMessageDepth {
		return fmt.Errorf("nested authz messages exceed maximum depth %d", maxTaxMessageDepth)
	}
	addCoins := func(coins sdk.Coins) error {
		if err := coins.Validate(); err != nil {
			return fmt.Errorf("invalid taxable coins: %w", err)
		}
		if !coins.IsZero() {
			*inputs = append(*inputs, coins)
		}
		return nil
	}

	switch typed := msg.(type) {
	case *banktypes.MsgSend:
		if typed == nil {
			return fmt.Errorf("nil bank send message")
		}
		return addCoins(typed.Amount)
	case *banktypes.MsgMultiSend:
		if typed == nil {
			return fmt.Errorf("nil bank multi-send message")
		}
		for _, input := range typed.Inputs {
			if err := addCoins(input.Coins); err != nil {
				return err
			}
		}
		return nil
	case *markettypes.MsgSwapSend:
		if typed == nil {
			return fmt.Errorf("nil Market swap-send message")
		}
		return addCoins(sdk.Coins{typed.OfferCoin})
	case *markettypes.MsgSwap:
		if typed == nil {
			return fmt.Errorf("nil Market swap message")
		}
		return nil
	case *authz.MsgExec:
		if typed == nil {
			return fmt.Errorf("nil authz execution message")
		}
		nested, err := typed.GetMessages()
		if err != nil {
			return fmt.Errorf("getting nested authz messages: %w", err)
		}
		for _, nestedMsg := range nested {
			if err := extractTaxInputs(nestedMsg, inputs, depth+1); err != nil {
				return err
			}
		}
		return nil
	default:
		return nil
	}
}
