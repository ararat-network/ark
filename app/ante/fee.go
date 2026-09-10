package ante

import (
	"bytes"
	"errors"
	"fmt"
	stdmath "math"
	"slices"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	sdkante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/decimal"
	treasurykeeper "github.com/ararat-network/ark/x/treasury/keeper"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// AttributeKeyTip names, on the SDK's tx event, the NOAH a transaction paid
// above the base fee.
const AttributeKeyTip = "tip"

// SimulatedFeeTransferGas covers fee-less simulation's unexecuted gas-fee transfer: a stable fee
// plus NOAH tip at the 2^128 balance bound. Payable fees meter their actual transfer; see
// README.md.
const SimulatedFeeTransferGas = 38_000

// useBaseFeeGate gates the consensus base fee.
var useBaseFeeGate = true

// SetBaseFeeGate switches the base-fee gate; false deducts the fee as a
// simulation does, refusing nothing. It exists for the simulation harness,
// which draws every fee from the sender's spendable balance and cannot be
// told what a transaction owes. Never call it outside a test.
func SetBaseFeeGate(enabled bool) {
	useBaseFeeGate = enabled
}

// BaseFeeGate reports the gate, so a harness that switches it off can
// restore what it found.
func BaseFeeGate() bool {
	return useBaseFeeGate
}

// FeeDecorator checks the signed tax ceiling, settles the consensus gas fee and NOAH tip, and
// carries the tax amount to TransferTaxDecorator. The granter, if present, bears all charges. See
// README.md and docs/clients/CLIENT_FEES.md for settlement and simulation rules.
type FeeDecorator struct {
	accountKeeper  sdkante.AccountKeeper
	bankKeeper     authtypes.BankKeeper
	feegrantKeeper sdkante.FeegrantKeeper
	treasury       *treasurykeeper.Keeper
}

func NewFeeDecorator(
	accountKeeper sdkante.AccountKeeper,
	bankKeeper authtypes.BankKeeper,
	feegrantKeeper sdkante.FeegrantKeeper,
	treasury *treasurykeeper.Keeper,
) FeeDecorator {
	return FeeDecorator{
		accountKeeper:  accountKeeper,
		bankKeeper:     bankKeeper,
		feegrantKeeper: feegrantKeeper,
		treasury:       treasury,
	}
}

func (d FeeDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, errorsmod.Wrap(errortypes.ErrTxDecode, "Tx must be a FeeTx")
	}
	if !simulate && ctx.BlockHeight() > 0 && feeTx.GetGas() == 0 {
		return ctx, errorsmod.Wrap(errortypes.ErrInvalidGasLimit, "must provide positive gas")
	}

	var (
		tax     sdk.Coins
		settled settlement
	)
	if ctx.BlockHeight() == 0 {
		settled = settlement{gasFee: feeTx.GetFee(), tip: math.ZeroInt()}
	} else {
		// One Params read prices the tax and settles the fee.
		params, err := d.treasury.Params.Get(ctx)
		if err != nil {
			return ctx, fmt.Errorf("getting treasury params: %w", err)
		}
		tax, _, err = d.treasury.ComputeTaxWithParams(ctx, params, tx.GetMsgs())
		if err != nil {
			return ctx, err
		}
		settled, err = d.settle(ctx, params, feeTx, tax, !simulate && useBaseFeeGate)
		if err != nil {
			return ctx, err
		}
	}

	if err := d.deduct(ctx, feeTx, settled); err != nil {
		return ctx, err
	}
	if simulate && ctx.BlockHeight() != 0 && settled.gasFee.IsZero() {
		// An estimate cannot move the fee it is sizing, so it pays for the
		// transfer in gas alone.
		ctx.GasMeter().ConsumeGas(SimulatedFeeTransferGas, "simulated gas fee transfer")
	}
	return next(withTransferTax(ctx, tax).WithPriority(settled.priority), tx, simulate)
}

// settlement is what the fee settles to: what reaches the fee collector —
// the base fee in the one denomination that covered it, plus the NOAH leg —
// the tip within that, and the rank the tip buys.
type settlement struct {
	gasFee   sdk.Coins
	tip      math.Int
	priority int64
}

// settle prices and partitions the fee. Enforcement rejects insufficient tax or base-fee coverage
// before deduction; simulation performs the same reads without those refusals. An unpriceable NOAH
// leg fails in either mode.
func (d FeeDecorator) settle(
	ctx sdk.Context, params treasurytypes.Params, feeTx sdk.FeeTx, tax sdk.Coins, enforce bool,
) (settlement, error) {
	fee := feeTx.GetFee()
	gas := feeTx.GetGas()
	settled := settlement{tip: math.ZeroInt()}

	// Sorting a copy before validating it drops only the sortedness check —
	// junk denoms, zero legs and repeats still fail — which leaves the encoded
	// order free to name the paying leg. byDenom answers lookups by
	// denomination, since AmountOf binary searches; fee is only ranged over.
	byDenom := slices.Clone(fee).Sort()
	if err := byDenom.Validate(); err != nil {
		return settlement{}, errorsmod.Wrapf(errortypes.ErrInvalidCoins,
			"invalid fee %s: %v", fee, err)
	}

	// A fee short of the declared tax is refused whole.
	if enforce {
		for _, coin := range tax {
			if byDenom.AmountOf(coin.Denom).LT(coin.Amount) {
				return settlement{}, errorsmod.Wrapf(errortypes.ErrInsufficientFee,
					"fee %s does not cover transfer tax %s", fee, tax)
			}
		}
	}

	price, err := d.treasury.BaseGasPrice.Get(ctx)
	if err != nil {
		return settlement{}, err
	}

	noah := byDenom.AmountOf(chain.NoahBaseDenom)
	var (
		noahRequired sdk.Coin
		noahFactor   math.LegacyDec
	)
	if noah.IsPositive() {
		noahRequired, noahFactor, err = d.treasury.GetRequiredGasFee(ctx, params, price, gas, chain.NoahBaseDenom)
		if err != nil {
			// Genesis mandates the NOAH cross, so a missing factor is broken
			// state rather than NOAH being an unaccepted fee denomination.
			return settlement{}, errorsmod.Wrapf(err,
				"reading the NOAH gas factor to price %s",
				sdk.NewCoin(chain.NoahBaseDenom, noah))
		}
	}

	// A fee-less estimate gives the loop below no leg to price, so it reads
	// NOAH's factor in place of the one the eventual fee will name.
	if fee.IsZero() && !enforce {
		if _, _, err := d.treasury.GetRequiredGasFee(ctx, params, price, gas, chain.NoahBaseDenom); err != nil {
			return settlement{}, errorsmod.Wrap(err, "reading the NOAH gas factor")
		}
	}

	// ceil(price × gas) is zero exactly when the price or the gas limit is,
	// and a zero requirement is covered.
	covered := price.IsZero() || gas == 0

	// Stable legs in the payer's own order: the first whose slack above its
	// declared tax covers its own requirement pays the base fee. A leg with no
	// factor, or one short of its requirement, is left at the tax it declared
	// and passed over.
	for _, coin := range fee {
		if covered {
			break
		}
		if coin.Denom == chain.NoahBaseDenom {
			continue
		}
		slack := coin.Amount.Sub(tax.AmountOf(coin.Denom))
		if !slack.IsPositive() {
			continue
		}
		required, _, err := d.treasury.GetRequiredGasFee(ctx, params, price, gas, coin.Denom)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				continue
			}
			return settlement{}, err
		}
		if slack.LT(required.Amount) {
			continue
		}
		settled.gasFee = sdk.NewCoins(required)
		covered = true
	}

	// NOAH pays the base fee only when no stable leg did; the rest of the leg
	// is the tip.
	tip := noah
	if !covered && noah.IsPositive() && noah.GTE(noahRequired.Amount) {
		settled.gasFee = sdk.NewCoins(noahRequired)
		tip = noah.Sub(noahRequired.Amount)
		covered = true
	}
	if !covered && enforce {
		// Only the refusal needs the reference figure; its factor is the
		// identity, so it cannot miss.
		reference, _, err := d.treasury.GetRequiredGasFee(ctx, params, price, gas, params.ReferenceDenom)
		if err != nil {
			return settlement{}, err
		}
		return settlement{}, errorsmod.Wrapf(errortypes.ErrInsufficientFee,
			"base fee requires %s or its equivalent in an accepted fee denomination%s, got %s",
			reference, besideTax(tax), fee)
	}

	if tip.IsPositive() {
		settled.tip = tip
		settled.gasFee = settled.gasFee.Add(sdk.NewCoin(chain.NoahBaseDenom, tip))
		if !noahFactor.IsPositive() {
			// Unreachable: every stored factor is written positive. Kept
			// because the division below does not judge its divisor's sign.
			return settlement{}, errorsmod.Wrapf(errortypes.ErrInvalidCoins,
				"non-positive gas factor for %s", chain.NoahBaseDenom)
		}
		// The numerator is a declared fee amount, bounded only by the decoder
		// at 2^256, so a factor below one carries the quotient out of the Dec
		// domain, where the stock Quo panics.
		value, err := decimal.Quo(math.LegacyNewDecFromInt(tip), noahFactor)
		if err != nil {
			return settlement{}, errorsmod.Wrapf(errortypes.ErrInvalidCoins,
				"ranking the %s tip: %v", chain.NoahBaseDenom, err)
		}
		settled.priority = gasPriority(value.TruncateInt(), gas)
	}
	return settled, nil
}

// deduct moves the base fee and tip to the fee collector, from the payer or
// from a granter through a draw on the allowance. The SDK's DeductFees helper
// is unusable here: its recipient is a package variable that only the SDK's
// own constructor sets. The tax moves in TransferTaxDecorator.
func (d FeeDecorator) deduct(ctx sdk.Context, feeTx sdk.FeeTx, settled settlement) error {
	gasFee := settled.gasFee
	if addr := d.accountKeeper.GetModuleAddress(authtypes.FeeCollectorName); addr == nil {
		return fmt.Errorf("fee collector module account (%s) has not been set", authtypes.FeeCollectorName)
	}

	deductFrom, payer, sponsored := chargedAccount(feeTx)
	if sponsored {
		if err := d.feegrantKeeper.UseGrantedFees(ctx, deductFrom, payer, gasFee, feeTx.GetMsgs()); err != nil {
			return errorsmod.Wrapf(err, "%s does not allow to pay fees for %s", deductFrom, payer)
		}
	}
	if d.accountKeeper.GetAccount(ctx, deductFrom) == nil {
		return errortypes.ErrUnknownAddress.Wrapf("fee payer address: %s does not exist", deductFrom)
	}

	if !gasFee.IsZero() {
		if !gasFee.IsValid() {
			return errorsmod.Wrapf(errortypes.ErrInsufficientFee, "invalid fee amount: %s", gasFee)
		}
		if err := d.bankKeeper.SendCoinsFromAccountToModule(ctx, deductFrom, authtypes.FeeCollectorName, gasFee); err != nil {
			return errorsmod.Wrapf(errortypes.ErrInsufficientFunds, "%s", err.Error())
		}
	}

	// The fee named is what moved; the tax is named by the post event, since
	// an ante event outlives a failed transaction.
	attributes := []sdk.Attribute{
		sdk.NewAttribute(sdk.AttributeKeyFee, gasFee.String()),
		sdk.NewAttribute(sdk.AttributeKeyFeePayer, deductFrom.String()),
	}
	if settled.tip.IsPositive() {
		attributes = append(attributes, sdk.NewAttribute(AttributeKeyTip, sdk.NewCoin(chain.NoahBaseDenom, settled.tip).String()))
	}
	ctx.EventManager().EmitEvents(sdk.Events{sdk.NewEvent(sdk.EventTypeTx, attributes...)})
	return nil
}

// chargedAccount resolves the fee payer or sponsoring granter. Each sponsored charge draws the
// granter's allowance. Account resolution must agree with app/client.payerOf.
func chargedAccount(feeTx sdk.FeeTx) (deductFrom, payer sdk.AccAddress, sponsored bool) {
	payer = sdk.AccAddress(feeTx.FeePayer())
	deductFrom = payer
	if granter := feeTx.FeeGranter(); granter != nil {
		deductFrom = sdk.AccAddress(granter)
		sponsored = !bytes.Equal(deductFrom, payer)
	}
	return deductFrom, payer, sponsored
}

// besideTax qualifies a refusal with the tax the fee also had to carry, when
// there was one.
func besideTax(tax sdk.Coins) string {
	if tax.IsZero() {
		return ""
	}
	return fmt.Sprintf(" in addition to transfer tax %s", tax)
}

// gasPriority ranks a transaction by the reference value it pays per gas
// unit, saturating at the int64 ceiling rather than overflowing on an
// absurd fee.
func gasPriority(value math.Int, gas uint64) int64 {
	if gas == 0 {
		return 0
	}
	perGas := value.Quo(math.NewIntFromUint64(gas))
	if !perGas.IsInt64() {
		return stdmath.MaxInt64
	}
	return perGas.Int64()
}
