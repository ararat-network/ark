package ante

import (
	"fmt"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	sdkante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	treasurytypes "github.com/ararat-network/ark/x/treasury/types"
)

// AttributeKeyTransferTax names, on the tx event TransferTaxDecorator emits
// once the messages have succeeded, the transfer tax a transaction paid
// within its fee.
const AttributeKeyTransferTax = "transfer_tax"

// transferTaxKey keys, on the context FeeDecorator hands on, the tax it
// priced and held the declared fee to. BaseApp carries the ante's context
// into the messages and the post chain, so the charge reads the figure the
// declaration was judged against and nothing is computed twice.
type transferTaxKey struct{}

func withTransferTax(ctx sdk.Context, tax sdk.Coins) sdk.Context {
	return ctx.WithValue(transferTaxKey{}, tax)
}

func transferTaxFromContext(ctx sdk.Context) (sdk.Coins, bool) {
	tax, ok := ctx.Value(transferTaxKey{}).(sdk.Coins)
	return tax, ok
}

// TransferTaxDecorator is the post half of the fee mechanism: it charges the
// transfer tax FeeDecorator priced, held the declared fee to, and handed on
// through the context, once the messages have run, and only when they
// succeeded. BaseApp runs the post chain on the messages' own branch —
// written with them, discarded with them — so the charge commits with the
// transfer that owes it and a transaction that fails is never taxed, on the
// terms the execution policy router already charges a contract's dispatches
// (D42, D82). A payer or granter that cannot cover the tax once the messages
// have moved what they move fails the transaction here; BaseApp then
// discards the messages and keeps the ante's gas charge, so the transfer
// does not happen and the gas that ran it is paid.
//
// The figure is the ante's, not a second computation: the same number the
// signed fee was held to, so the charge cannot exceed the declaration by
// construction. A context carrying no figure is a wiring fault — the post
// chain running without FeeDecorator — and fails the transaction rather than
// letting a transfer through untaxed. The ante prices nothing at height
// zero and hands on nothing, so genesis charges nothing here without a rule
// of its own.
//
// A granter bears the tax as they bear the gas fee, through a draw on the
// allowance for the tax alone.
type TransferTaxDecorator struct {
	accountKeeper  sdkante.AccountKeeper
	bankKeeper     authtypes.BankKeeper
	feegrantKeeper sdkante.FeegrantKeeper
}

func NewTransferTaxDecorator(
	accountKeeper sdkante.AccountKeeper,
	bankKeeper authtypes.BankKeeper,
	feegrantKeeper sdkante.FeegrantKeeper,
) TransferTaxDecorator {
	return TransferTaxDecorator{
		accountKeeper:  accountKeeper,
		bankKeeper:     bankKeeper,
		feegrantKeeper: feegrantKeeper,
	}
}

func (d TransferTaxDecorator) PostHandle(ctx sdk.Context, tx sdk.Tx, simulate, success bool, next sdk.PostHandler) (sdk.Context, error) {
	if !success {
		return next(ctx, tx, simulate, success)
	}
	tax, ok := transferTaxFromContext(ctx)
	if !ok {
		return ctx, fmt.Errorf("no transfer tax on the context: the post chain is running without FeeDecorator")
	}
	if tax.IsZero() {
		return next(ctx, tx, simulate, success)
	}
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
	}
	if addr := d.accountKeeper.GetModuleAddress(treasurytypes.TransferTaxCollectorName); addr == nil {
		return ctx, fmt.Errorf("transfer tax collector module account (%s) has not been set", treasurytypes.TransferTaxCollectorName)
	}

	deductFrom, payer, sponsored := chargedAccount(feeTx)
	if sponsored {
		if err := d.feegrantKeeper.UseGrantedFees(ctx, deductFrom, payer, tax, tx.GetMsgs()); err != nil {
			return ctx, errorsmod.Wrapf(err, "%s does not allow to pay fees for %s", deductFrom, payer)
		}
	}
	if err := d.bankKeeper.SendCoinsFromAccountToModule(ctx, deductFrom, treasurytypes.TransferTaxCollectorName, tax); err != nil {
		return ctx, errorsmod.Wrapf(err, "collecting transfer tax %s", tax)
	}

	ctx.EventManager().EmitEvents(sdk.Events{sdk.NewEvent(sdk.EventTypeTx,
		sdk.NewAttribute(AttributeKeyTransferTax, tax.String()),
		sdk.NewAttribute(sdk.AttributeKeyFeePayer, deductFrom.String()),
	)})
	return next(ctx, tx, simulate, success)
}
