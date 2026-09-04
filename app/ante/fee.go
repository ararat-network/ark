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
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	sdkante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	chain "github.com/ararat-network/ark/pkg/chain"
	"github.com/ararat-network/ark/pkg/decimal"
	treasurykeeper "github.com/ararat-network/ark/x/treasury/keeper"
)

// AttributeKeyTransferTax names, on the tx event TransferTaxDecorator emits
// once the messages have succeeded, the transfer tax a transaction paid
// within its fee.
const AttributeKeyTransferTax = "transfer_tax"

// AttributeKeyTip names, on the SDK's tx event, the NOAH a transaction paid
// above the base fee.
const AttributeKeyTip = "tip"

// SimulatedFeeTransferGas is what a fee-less simulation consumes in place of
// the gas fee transfer it cannot make: the store traffic of a one-coin send
// to the fee collector — the payer's account and balance, the collector's
// balance and account check — at the store's prices with amounts at the
// 2^128 quantity bound, wider than any balance the chain issues, so a real
// transfer costs no more. TestGasEstimateMatchesExecution measures a real
// transfer against it and fails if it falls below the measurement or drifts
// loose above it.
const SimulatedFeeTransferGas = 21_000

// useBaseFeeGate gates the consensus base fee, and no genesis can stand in
// for it: Validate refuses a zero MinBaseGasPrice, the controller floors the
// live price at it every block, and the requirement ceils to at least one base
// unit, so the gate is unsatisfiable-free by construction.
var useBaseFeeGate = true

// SetBaseFeeGate switches the base-fee gate, and false disables it outright:
// the fee is then deducted as a simulation deducts it, nothing refused. It
// exists for the simulation harness, which draws every transaction's fee
// uniformly from the sender's spendable balance and cannot be told what a
// transaction owes: it spends senders down to an empty balance and then
// offers no fee at all, which no base fee can accept. The Hub switches its
// own fee market off for the same reason. Never call it outside a test.
func SetBaseFeeGate(enabled bool) {
	useBaseFeeGate = enabled
}

// FeeDecorator is the ante half of the fee mechanism. It prices the transfer
// tax a transaction's messages owe, holds the declared fee to it, settles the
// fee by denomination against Treasury's consensus base fee, and deducts the
// base fee and the NOAH tip to the fee collector — on the ante's cache
// branch, so a refusal anywhere moves nothing. The tax itself is charged by
// TransferTaxDecorator after the messages, on their branch, so a transaction
// that fails is never taxed (D82). What this decorator judges about the tax
// is the declaration: whether the signed fee covers it. It prices the tax to
// judge that, the settlement needs the same figure, since a stable leg's
// slack above its tax is what pays the base fee, and it hands the figure on
// through the context so the charge is the declaration's own number and the
// tax is computed once. Affordability is the charge's own judgement, against
// the balance the messages left. It replaces the SDK's DeductFeeDecorator,
// whose deduction it mirrors — granter resolution, account check, collector
// send, tx event — from x/auth/ante/fee.go at v0.54.3; an SDK upgrade
// touching that file is re-diffed here by hand, as the decorator list
// already is. Owning it is what lets an estimate move what execution moves:
// the SDK skipped its checker under simulation and deducted a declared fee
// whole, tax included.
//
// The fee is a ceiling and the tip is NOAH (D80). A leg in any other
// denomination is charged the exact tax it declares, plus
// ceil(BaseGasPrice × gas limit × factor) if it is the first leg in
// denomination order whose slack above the tax covers it; the rest never
// leaves the payer. The NOAH leg is charged whole: base fee if no stable
// leg covered it, remainder tip. NOAH carries the tip because the tax is
// never owed in it, so a tip and a padded tax are told apart by
// denomination where one denomination split by subtraction could not. A
// fee short of the tax is refused before anything is deducted (D78).
//
// Under simulation, and in the harness with the gate off, nothing is
// refused on fee grounds: the tax is set aside, the base fee taken where a
// leg covers it, a NOAH leg charged as the tip it would be.
// An estimate still carries what execution will cost: the settlement's
// reads are made and not enforced, and a fee-less estimate consumes
// SimulatedFeeTransferGas for the transfer it cannot make. Height zero
// waives tax and gate alike: gentxs carry no fees, and a transfer tax at
// genesis defends nothing, since whoever can place a taxable message in a
// gentx can write the balances directly.
//
// A fee granter bears base fee, tip, and tax together, each drawn on the
// allowance as it is charged — base fee and tip here, the tax after the
// messages — so the allowance records what left the granter and nothing
// more. Strictness is unchanged: a grant that will not cover a charge fails
// the transaction rather than falling back to the payer.
//
// Priority is the tip in reference units per gas unit, through the factor
// NOAH is priced with: what a transaction chose to pay above the
// requirement, never the tax or a stable leg's slack, neither of which buys
// block space. A NOAH leg the table cannot price is refused, as a NOAH gas
// fee is while the same entry is absent: a tip nothing can rank would be
// paid for nothing.
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
		return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
	}
	if !simulate && ctx.BlockHeight() > 0 && feeTx.GetGas() == 0 {
		return ctx, errorsmod.Wrap(sdkerrors.ErrInvalidGasLimit, "must provide positive gas")
	}

	var (
		tax     sdk.Coins
		settled settlement
	)
	if ctx.BlockHeight() == 0 {
		settled = settlement{gasFee: feeTx.GetFee(), tip: math.ZeroInt()}
	} else {
		var err error
		tax, err = d.treasury.ComputeTax(ctx, tx.GetMsgs())
		if err != nil {
			return ctx, err
		}
		settled, err = d.settle(ctx, feeTx, tax, !simulate && useBaseFeeGate)
		if err != nil {
			return ctx, err
		}
	}

	if err := d.deduct(ctx, tx, feeTx, settled); err != nil {
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
// the tip alone, and the rank it buys. The tax is what the messages owe,
// known before the fee is read, so it is not here.
type settlement struct {
	gasFee   sdk.Coins
	tip      math.Int
	priority int64
}

// settle partitions the fee by denomination and prices it. Enforcing, it
// refuses before anything moves: a fee short of the tax, a fee no leg can
// cover the base fee from, a NOAH leg the table cannot price. Not enforcing
// — simulation, or the harness with the gate off — it refuses nothing,
// settles what it can, and makes the reads execution makes so the estimate
// carries them.
func (d FeeDecorator) settle(ctx sdk.Context, feeTx sdk.FeeTx, tax sdk.Coins, enforce bool) (settlement, error) {
	fee := feeTx.GetFee()
	gas := feeTx.GetGas()
	settled := settlement{tip: math.ZeroInt()}

	// A tx's fee is decoded Coins, never constructed ones: ValidateBasic
	// rejects only nil and negative amounts. Sorting a copy before validating
	// it drops only the sortedness check — junk denoms, zero legs and repeats
	// still fail — which leaves the encoded order free to name the paying
	// leg. Read the fee by denomination through byDenom, since AmountOf
	// binary searches; fee itself is only ever ranged over.
	byDenom := slices.Clone(fee).Sort()
	if err := byDenom.Validate(); err != nil {
		return settlement{}, errorsmod.Wrapf(sdkerrors.ErrInvalidCoins,
			"invalid fee %s: %v", fee, err)
	}

	// The declaration. Refused whole, whatever the fee offers for gas.
	if enforce {
		for _, coin := range tax {
			if byDenom.AmountOf(coin.Denom).LT(coin.Amount) {
				return settlement{}, errorsmod.Wrapf(sdkerrors.ErrInsufficientFee,
					"fee %s does not cover transfer tax %s", fee, tax)
			}
		}
	}

	params, err := d.treasury.Params.Get(ctx)
	if err != nil {
		return settlement{}, err
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
			// Genesis mandates the NOAH cross and nothing deletes it, so a
			// missing factor refuses the leg on broken state rather than on
			// NOAH being an unaccepted fee denomination.
			return settlement{}, errorsmod.Wrapf(err,
				"reading the NOAH gas factor to price %s",
				sdk.NewCoin(chain.NoahBaseDenom, noah))
		}
	}

	// A fee-less estimate gives the loop below no leg to price, so it reads
	// NOAH's factor in place of the one the eventual fee will name — one
	// more than the reference, whose identity factor reads nothing, so a
	// reference payer's estimate runs one read high. A failure is the same
	// broken state as above, with no leg of its own to name.
	if fee.IsZero() && !enforce {
		if _, _, err := d.treasury.GetRequiredGasFee(ctx, params, price, gas, chain.NoahBaseDenom); err != nil {
			return settlement{}, errorsmod.Wrap(err, "reading the NOAH gas factor")
		}
	}

	// The reference requirement ceil(price × gas) is zero exactly when the
	// price or the gas limit is, and a zero requirement is covered by
	// construction.
	covered := price.IsZero() || gas == 0

	// Stable legs in the payer's own order: the first whose slack above its
	// declared tax covers its own requirement pays the base fee, so the fee's
	// encoding is how a payer names the leg it comes from. The legs are
	// value-equivalent through their factors, so the choice costs at most a
	// rounding unit. A leg with no factor is a tax-only ceiling and
	// a leg short of its requirement is left for the next; neither is
	// refused, because neither is charged past the tax it declared.
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

	// NOAH pays the base fee only when no stable leg did. The rest of the
	// leg is the tip either way.
	tip := noah
	if !covered && noah.IsPositive() && noah.GTE(noahRequired.Amount) {
		settled.gasFee = sdk.NewCoins(noahRequired)
		tip = noah.Sub(noahRequired.Amount)
		covered = true
	}
	if !covered && enforce {
		// Only refusal derives the reference figure it quotes — identity
		// factor, so it cannot miss.
		reference, _, err := d.treasury.GetRequiredGasFee(ctx, params, price, gas, params.ReferenceDenom)
		if err != nil {
			return settlement{}, err
		}
		return settlement{}, errorsmod.Wrapf(sdkerrors.ErrInsufficientFee,
			"base fee requires %s or its equivalent in an accepted fee denomination%s, got %s",
			reference, besideTax(tax), fee)
	}

	if tip.IsPositive() {
		settled.tip = tip
		settled.gasFee = settled.gasFee.Add(sdk.NewCoin(chain.NoahBaseDenom, tip))
		if !noahFactor.IsPositive() {
			// Unreachable: every stored factor is written positive. Kept so
			// the division can never be reached by zero or a negative, which
			// the checked call below does not judge.
			return settlement{}, errorsmod.Wrapf(sdkerrors.ErrInvalidCoins,
				"non-positive gas factor for %s", chain.NoahBaseDenom)
		}
		// The numerator is a declared fee amount, bounded only by the decoder
		// at 2^256, so a factor below one carries the quotient out of the Dec
		// domain where the stock Quo would panic.
		value, err := decimal.Quo(math.LegacyNewDecFromInt(tip), noahFactor)
		if err != nil {
			return settlement{}, errorsmod.Wrapf(sdkerrors.ErrInvalidCoins,
				"ranking the %s tip: %v", chain.NoahBaseDenom, err)
		}
		settled.priority = gasPriority(value.TruncateInt(), gas)
	}
	return settled, nil
}

// deduct moves what the ante moves: the base fee and tip to the fee
// collector, from the payer or from a granter through a draw on the
// allowance. It mirrors the SDK's checkDeductFee. The SDK's DeductFees helper
// is not used because its recipient is a package variable that only the
// SDK's own constructor sets. The tax does not move here:
// TransferTaxDecorator charges it once the messages have succeeded, and
// refusing an unaffordable one is that charge's own job.
func (d FeeDecorator) deduct(ctx sdk.Context, tx sdk.Tx, feeTx sdk.FeeTx, settled settlement) error {
	gasFee := settled.gasFee
	if addr := d.accountKeeper.GetModuleAddress(authtypes.FeeCollectorName); addr == nil {
		return fmt.Errorf("fee collector module account (%s) has not been set", authtypes.FeeCollectorName)
	}

	deductFrom, payer, sponsored := chargedAccount(feeTx)
	if sponsored {
		if err := d.feegrantKeeper.UseGrantedFees(ctx, deductFrom, payer, gasFee, tx.GetMsgs()); err != nil {
			return errorsmod.Wrapf(err, "%s does not allow to pay fees for %s", deductFrom, payer)
		}
	}
	if d.accountKeeper.GetAccount(ctx, deductFrom) == nil {
		return sdkerrors.ErrUnknownAddress.Wrapf("fee payer address: %s does not exist", deductFrom)
	}

	if !gasFee.IsZero() {
		if !gasFee.IsValid() {
			return errorsmod.Wrapf(sdkerrors.ErrInsufficientFee, "invalid fee amount: %s", gasFee)
		}
		if err := d.bankKeeper.SendCoinsFromAccountToModule(ctx, deductFrom, authtypes.FeeCollectorName, gasFee); err != nil {
			return errorsmod.Wrapf(sdkerrors.ErrInsufficientFunds, "%s", err.Error())
		}
	}

	// The fee named is what moved. The tax is named by the post event, since
	// an ante event outlives a failed transaction, which pays no tax.
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

// chargedAccount resolves who a transaction's charges come from: the fee
// payer, or the granter when one is named and is not the payer, in which
// case each charge is also a draw on the granter's allowance. It mirrors the
// SDK's checkDeductFee.
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

// GasTallyDecorator records each transaction's declared gas for the base-fee
// controller. Block execution only: CheckTx sees mempool traffic rather
// than the block, and a transaction failing later in the ante reverts the
// write with everything else, so the tally reads as the block's paid-for
// gas. Simulation tallies too, onto its discarded cache, so the estimate
// carries the write.
type GasTallyDecorator struct {
	treasury *treasurykeeper.Keeper
}

func NewGasTallyDecorator(treasury *treasurykeeper.Keeper) GasTallyDecorator {
	return GasTallyDecorator{treasury: treasury}
}

func (d GasTallyDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	// Genesis transactions are not block traffic and must not seed the first
	// block's tally. Baseapp happens not to stamp ExecModeFinalize on the
	// InitChain context, but the height gate is the rule.
	if (ctx.ExecMode() == sdk.ExecModeFinalize || simulate) && ctx.BlockHeight() != 0 {
		if feeTx, ok := tx.(sdk.FeeTx); ok {
			if err := d.treasury.TallyBlockGas(ctx, feeTx.GetGas()); err != nil {
				return ctx, err
			}
		}
	}
	return next(ctx, tx, simulate)
}
