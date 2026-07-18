package app

import (
	"errors"
	"math"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	treasurytypes "ark/x/treasury/types"
)

func (app *ArkApp) treasuryFeeChecker(ctx sdk.Context, tx sdk.Tx) (sdk.Coins, int64, error) {
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return nil, 0, errorsmod.Wrap(sdkerrors.ErrTxDecode, "Tx must be a FeeTx")
	}

	fee := feeTx.GetFee()
	tax, err := app.TreasuryKeeper.ComputeTax(ctx, tx.GetMsgs())
	if err != nil {
		if ctx.BlockHeight() != 0 || !errors.Is(err, collections.ErrNotFound) {
			return nil, 0, err
		}
		tax = sdk.NewCoins()
	}
	if !fee.IsAllGTE(tax) {
		return nil, 0, errorsmod.Wrapf(
			sdkerrors.ErrInsufficientFee,
			"insufficient fees to cover stability tax; got: %s required tax: %s",
			fee,
			tax,
		)
	}
	gasFee, _ := fee.SafeSub(tax...)

	gas := feeTx.GetGas()
	if ctx.IsCheckTx() {
		minGasPrices := ctx.MinGasPrices()
		if !minGasPrices.IsZero() {
			requiredFees := make(sdk.Coins, len(minGasPrices))
			gasLimit := sdkmath.LegacyNewDec(int64(gas))
			for i, gasPrice := range minGasPrices {
				requiredFees[i] = sdk.NewCoin(
					gasPrice.Denom,
					gasPrice.Amount.Mul(gasLimit).Ceil().RoundInt(),
				)
			}
			if !gasFee.IsAnyGTE(requiredFees) {
				return nil, 0, errorsmod.Wrapf(
					sdkerrors.ErrInsufficientFee,
					"insufficient gas fees; got: %s required: %s",
					gasFee,
					requiredFees,
				)
			}
		}
	}

	return fee, txPriority(gasFee, gas), nil
}

func txPriority(fee sdk.Coins, gas uint64) int64 {
	if gas == 0 {
		return 0
	}

	var priority int64
	for _, coin := range fee {
		coinPriority := int64(math.MaxInt64)
		gasPrice := coin.Amount.Quo(sdkmath.NewIntFromUint64(gas))
		if gasPrice.IsInt64() {
			coinPriority = gasPrice.Int64()
		}
		if priority == 0 || coinPriority < priority {
			priority = coinPriority
		}
	}
	return priority
}

func (app *ArkApp) routeStabilityTax(next sdk.AnteHandler) sdk.AnteHandler {
	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		newCtx, err := next(ctx, tx, simulate)
		if err != nil || simulate {
			return newCtx, err
		}

		tax, err := app.TreasuryKeeper.ComputeTax(newCtx, tx.GetMsgs())
		if err != nil {
			if newCtx.BlockHeight() != 0 || !errors.Is(err, collections.ErrNotFound) {
				return newCtx, err
			}
			tax = sdk.NewCoins()
		}
		if tax.IsZero() {
			return newCtx, nil
		}

		if err := app.BankKeeper.SendCoinsFromModuleToModule(
			newCtx,
			authtypes.FeeCollectorName,
			treasurytypes.StabilityTaxCollectorName,
			tax,
		); err != nil {
			return newCtx, err
		}
		return newCtx, nil
	}
}
