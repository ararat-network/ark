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

	tobinTaxes, err := k.oracleKeeper.GetTobinTaxes(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting taxable denominations: %w", err)
	}
	taxableDenoms := make(map[string]struct{}, len(tobinTaxes))
	for _, tobinTax := range tobinTaxes {
		taxableDenoms[tobinTax.Denom] = struct{}{}
	}

	caps := make(map[string]math.Int)
	taxAmounts := make(map[string]math.Int)
	for _, input := range inputs {
		for _, principal := range input {
			if _, taxable := taxableDenoms[principal.Denom]; !taxable {
				continue
			}

			cap, loaded := caps[principal.Denom]
			if !loaded {
				cap, err = k.TaxCaps.Get(ctx, principal.Denom)
				if err != nil {
					if errors.Is(err, collections.ErrNotFound) {
						return nil, errorsmod.Wrapf(
							types.ErrTaxCapUnavailable,
							"configured denom %s has no current tax cap",
							principal.Denom,
						)
					}
					return nil, fmt.Errorf("getting tax cap for configured denom %s: %w", principal.Denom, err)
				}
				if cap.IsNil() {
					return nil, fmt.Errorf("tax cap is unset for configured denom %s", principal.Denom)
				}
				if cap.IsNegative() {
					return nil, fmt.Errorf("negative tax cap %s for configured denom %s", cap, principal.Denom)
				}
				caps[principal.Denom] = cap
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
