package ante

import (
	"fmt"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
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

// TransferTaxDecorator collects FeeDecorator's declared tax after successful messages, on their
// cache branch. Missing tax context or an unaffordable charge fails execution; admission and
// proposal checks skip collection. See README.md for rollback, simulation, and feegrant rules.
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
	// Tax affordability depends on the balances and allowances left by the
	// messages. CheckTx and proposal verification do not execute those messages.
	if !success || (!simulate && ctx.ExecMode() != sdk.ExecModeFinalize) {
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
		return ctx, errorsmod.Wrap(errortypes.ErrTxDecode, "Tx must be a FeeTx")
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
