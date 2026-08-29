package keeper

import (
	"context"
	"errors"
	"fmt"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v11/modules/apps/transfer/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/ararat-network/ark/pkg/decimal"
	markettypes "github.com/ararat-network/ark/x/market/types"
	oracletypes "github.com/ararat-network/ark/x/oracle/types"
	"github.com/ararat-network/ark/x/treasury/types"
)

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
	// Treasury holds a cap for it. Caps are derived from oracle-priced
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
				var err error
				cap, err = k.GetTaxCap(ctx, principal.Denom)
				if errors.Is(err, collections.ErrNotFound) {
					// No factor has ever been derived for this denomination:
					// it is outside the registry, or a member whose first
					// refresh has not landed. Untaxed either way, because
					// taxing uncapped would be unbounded and rejecting would
					// let a dark oracle block transfers. A nil entry memoises
					// the miss for the rest of the transaction.
					cap = math.Int{}
				} else if err != nil {
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

// GetTaxCap derives one denomination's tax cap from its stored conversion
// factor: ReferenceTaxCap × factor, truncated to base units. A zero reference
// is the uncapped sentinel and derives zero for every member; a positive
// reference whose product truncates below one floors at one rather than
// producing the zero that would read as uncapped, which would lift the
// ceiling a small reference cap was asking to tighten. A missing entry
// returns collections.ErrNotFound: the factor set is the tax base, and
// absence means untaxed to the callers that own that judgement.
func (k Keeper) GetTaxCap(ctx context.Context, denom string) (math.Int, error) {
	entry, err := k.ConversionFactors.Get(ctx, denom)
	if err != nil {
		return math.Int{}, err
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.Int{}, fmt.Errorf("getting params: %w", err)
	}
	return deriveTaxCap(params, entry), nil
}

// deriveTaxCap is the one place the cap arithmetic lives; GetTaxCap and the
// TaxCaps query both price through it. The product is checked because this
// runs at read time, where a lopsided factor meets whatever reference a later
// governance vote chose: a ceiling too large for the decimal domain is no
// ceiling, so an unrepresentable product derives the uncapped sentinel rather
// than panicking a read.
func deriveTaxCap(params types.Params, entry types.ConversionFactor) math.Int {
	if params.ReferenceTaxCap.IsZero() {
		return math.ZeroInt()
	}
	product, err := decimal.Mul(entry.Factor, math.LegacyNewDecFromInt(params.ReferenceTaxCap))
	if err != nil {
		return math.ZeroInt()
	}
	cap := product.TruncateInt()
	if !cap.IsPositive() {
		cap = math.OneInt()
	}
	return cap
}

// rescaleTaxCap re-expresses the reference tax cap in the new unit and emits
// the rebase event, returning the converted amount for the caller's single
// params write. Conversion failure fails the re-point: the handed set prices
// both legs by the caller's contract.
func (k Keeper) rescaleTaxCap(ctx context.Context, params types.Params, to string, rates oracletypes.RateSet) (math.Int, error) {
	oldCap := sdk.NewCoin(params.ReferenceDenom, params.ReferenceTaxCap)
	newCap := sdk.NewCoin(to, params.ReferenceTaxCap)
	if params.ReferenceTaxCap.IsPositive() {
		converted, err := rates.Convert(sdk.NewDecCoinFromCoin(oldCap), to)
		if err != nil {
			return math.Int{}, err
		}
		coin, _ := converted.TruncateDecimal()
		// A positive cap truncating to zero would silently become the uncapped
		// sentinel, and unlimited taxation by rounding accident is not a unit
		// change. Flooring at one base unit is the same degrade the derived
		// caps apply: the tightest finite ceiling, where refusing would wedge
		// the re-point on a rate pair no retry can mend.
		if !coin.Amount.IsPositive() {
			coin.Amount = math.OneInt()
		}
		newCap = coin
	}
	if err := sdk.UnwrapSDKContext(ctx).EventManager().EmitTypedEvent(&types.EventReferenceTaxCapRebased{
		OldCap: oldCap,
		NewCap: newCap,
	}); err != nil {
		return math.Int{}, fmt.Errorf("emitting Treasury reference tax cap rebase event: %w", err)
	}
	return newCap.Amount, nil
}

func extractTaxInputs(msg sdk.Msg, inputs *[]sdk.Coins, depth int) error {
	if msg == nil {
		return fmt.Errorf("nil SDK message")
	}
	// The decoder's unpack-depth cap is the recursion bound: a signed
	// transaction nested this deep cannot decode, so the check binds only
	// for messages arriving off the tx path through the router.
	if depth >= codectypes.MaxUnpackAnyRecursionDepth {
		return fmt.Errorf("nested authz messages exceed maximum depth %d", codectypes.MaxUnpackAnyRecursionDepth)
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
	// Funding a vesting account is a transfer: the coins leave the funder at
	// creation and only their release is scheduled. Terra Classic left these
	// untaxed and they became the standard dodge around its transfer tax.
	case *vestingtypes.MsgCreateVestingAccount:
		if typed == nil {
			return fmt.Errorf("nil vesting account message")
		}
		return addCoins(typed.Amount)
	case *vestingtypes.MsgCreatePermanentLockedAccount:
		if typed == nil {
			return fmt.Errorf("nil permanent locked account message")
		}
		return addCoins(typed.Amount)
	case *vestingtypes.MsgCreatePeriodicVestingAccount:
		if typed == nil {
			return fmt.Errorf("nil periodic vesting account message")
		}
		// The whole schedule is funded at creation, so the periods sum to one
		// input; presenting each period alone would apply the cap per period.
		totals := make(map[string]math.Int)
		for _, period := range typed.VestingPeriods {
			if err := period.Amount.Validate(); err != nil {
				return fmt.Errorf("invalid taxable coins: %w", err)
			}
			for _, coin := range period.Amount {
				current, found := totals[coin.Denom]
				if !found {
					current = math.ZeroInt()
				}
				sum, err := current.SafeAdd(coin.Amount)
				if err != nil {
					return fmt.Errorf("summing vesting periods for denom %s: %w", coin.Denom, err)
				}
				totals[coin.Denom] = sum
			}
		}
		total := make([]sdk.Coin, 0, len(totals))
		for denom, amount := range totals {
			total = append(total, sdk.NewCoin(denom, amount))
		}
		return addCoins(sdk.NewCoins(total...))
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
	case *ibctransfertypes.MsgTransfer:
		if typed == nil {
			return fmt.Errorf("nil IBC transfer message")
		}
		// Only the outbound leg is an input. A forwarded hop, acknowledgement,
		// timeout, or refund is protocol continuation of this same transfer and
		// is never re-presented here (D46).
		return addCoins(sdk.Coins{typed.Token})
	case *wasmtypes.MsgExecuteContract:
		if typed == nil {
			return fmt.Errorf("nil Wasm execute message")
		}
		return addCoins(typed.Funds)
	case *wasmtypes.MsgInstantiateContract:
		if typed == nil {
			return fmt.Errorf("nil Wasm instantiate message")
		}
		return addCoins(typed.Funds)
	case *wasmtypes.MsgInstantiateContract2:
		if typed == nil {
			return fmt.Errorf("nil Wasm instantiate2 message")
		}
		return addCoins(typed.Funds)
	case *wasmtypes.MsgStoreAndInstantiateContract:
		if typed == nil {
			return fmt.Errorf("nil Wasm store-and-instantiate message")
		}
		// Authority-gated while uploads are AllowNobody, taxed anyway so a
		// params change opening uploads cannot mint an untaxed instantiate.
		return addCoins(typed.Funds)
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
