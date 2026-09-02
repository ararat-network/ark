package ante

import (
	"bytes"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	sdkante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"

	treasurykeeper "github.com/ararat-network/ark/x/treasury/keeper"
	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// StabilityTaxDecorator charges the stability tax a signed transaction's
// messages owe, straight to the collector — the same direct-send terms the
// policy router applies to execution-generated messages. The declared fee
// never carries the tax, so any gas-pricing mechanism reads GetFee() without
// treasury arithmetic. A fee granter bears the tax on gas's own terms:
// resolved as checkDeductFee resolves it, drawn through the allowance — its
// second draw this transaction, after DeductFee's — and strict, so a grant
// that will not cover the tax denom fails the transaction rather than
// falling back to the payer. Simulation is charged on the same terms, as the
// SDK charges a declared fee: baseapp discards its state, and an estimate
// then carries exactly the reads and writes block execution will.
type StabilityTaxDecorator struct {
	treasury *treasurykeeper.Keeper
	bank     bankkeeper.BaseKeeper
	feegrant sdkante.FeegrantKeeper
}

func NewStabilityTaxDecorator(treasury *treasurykeeper.Keeper, bank bankkeeper.BaseKeeper, feegrant sdkante.FeegrantKeeper) StabilityTaxDecorator {
	return StabilityTaxDecorator{treasury: treasury, bank: bank, feegrant: feegrant}
}

func (d StabilityTaxDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
	}

	// Height zero carries only genesis transactions, and a transfer tax at
	// genesis defends nothing: whoever can place a taxable message in a
	// gentx can write the balances directly. Waived outright, so the outcome
	// does not turn on whether Treasury genesis has run yet.
	if ctx.BlockHeight() == 0 {
		return next(ctx, tx, simulate)
	}

	tax, err := d.treasury.ComputeTax(ctx, tx.GetMsgs())
	if err != nil {
		return ctx, err
	}
	if tax.IsZero() {
		return next(ctx, tx, simulate)
	}

	payer := sdk.AccAddress(feeTx.FeePayer())
	taxPayer := payer
	if granter := feeTx.FeeGranter(); granter != nil {
		granterAddr := sdk.AccAddress(granter)
		if !bytes.Equal(granterAddr, payer) {
			if err := d.feegrant.UseGrantedFees(ctx, granterAddr, payer, tax, tx.GetMsgs()); err != nil {
				return ctx, errorsmod.Wrapf(err, "%s does not allow to pay stability tax for %s", granterAddr, payer)
			}
		}
		taxPayer = granterAddr
	}

	if err := d.bank.SendCoinsFromAccountToModule(
		ctx,
		taxPayer,
		treasurytypes.StabilityTaxCollectorName,
		tax,
	); err != nil {
		return ctx, errorsmod.Wrapf(err, "collecting stability tax %s", tax)
	}
	return next(ctx, tx, simulate)
}
