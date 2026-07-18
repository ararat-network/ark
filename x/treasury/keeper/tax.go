package keeper

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	markettypes "ark/x/market/types"
	oracletypes "ark/x/oracle/types"
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
			if tax.GT(cap) {
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

// BuildTaxCaps derives a complete cap set from one immutable Oracle snapshot.
func (k Keeper) BuildTaxCaps(
	ctx context.Context,
	params types.Params,
) ([]types.TaxCap, error) {
	referenceTaxCap := params.ReferenceTaxCap
	tobinTaxes, err := k.oracleKeeper.GetTobinTaxes(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting Tobin taxes: %w", err)
	}
	if !slices.ContainsFunc(tobinTaxes, func(tax oracletypes.TobinTax) bool {
		return tax.Denom == referenceTaxCap.Denom
	}) {
		return nil, fmt.Errorf("reference tax cap denom %s is not configured in oracle", referenceTaxCap.Denom)
	}

	caps := make([]types.TaxCap, 0, len(tobinTaxes))
	denoms := make([]string, len(tobinTaxes))
	for i, tax := range tobinTaxes {
		denoms[i] = tax.Denom
	}
	rates, err := k.oracleKeeper.GetRateSnapshot(ctx, denoms...)
	if err != nil {
		return nil, fmt.Errorf("capturing tax-cap rates: %w", err)
	}

	reference := sdk.NewDecCoinFromCoin(referenceTaxCap)
	for _, tax := range tobinTaxes {
		if tax.Denom == reference.Denom {
			caps = append(caps, types.TaxCap{Denom: tax.Denom, TaxCap: referenceTaxCap.Amount})
			continue
		}
		converted, err := rates.Convert(reference, tax.Denom)
		if err != nil {
			return nil, fmt.Errorf("converting tax cap from %s to %s: %w", reference.Denom, tax.Denom, err)
		}
		coin, _ := converted.TruncateDecimal()
		caps = append(caps, types.TaxCap{Denom: tax.Denom, TaxCap: coin.Amount})
	}
	return caps, nil
}

// ReplaceTaxCaps atomically replaces the derived cap collection in the
// caller's cached state transition.
func (k Keeper) ReplaceTaxCaps(ctx context.Context, caps []types.TaxCap) error {
	if err := k.TaxCaps.Clear(ctx, nil); err != nil {
		return fmt.Errorf("clearing tax caps: %w", err)
	}
	for _, cap := range caps {
		if err := k.TaxCaps.Set(ctx, cap.Denom, cap.TaxCap); err != nil {
			return fmt.Errorf("setting tax cap %s: %w", cap.Denom, err)
		}
	}
	return nil
}

// TaxCapDenomsMismatch reports whether Oracle's configured native-stable set
// differs from the stored cap set.
func (k Keeper) TaxCapDenomsMismatch(ctx context.Context) (bool, error) {
	tobinTaxes, err := k.oracleKeeper.GetTobinTaxes(ctx)
	if err != nil {
		return false, err
	}
	expected := make(map[string]struct{}, len(tobinTaxes))
	for _, tax := range tobinTaxes {
		expected[tax.Denom] = struct{}{}
	}
	mismatch := false
	if err := k.TaxCaps.Walk(ctx, nil, func(denom string, _ math.Int) (bool, error) {
		if _, ok := expected[denom]; !ok {
			mismatch = true
			return true, nil
		}
		delete(expected, denom)
		return false, nil
	}); err != nil {
		return false, err
	}
	return mismatch || len(expected) > 0, nil
}
