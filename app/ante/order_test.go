package ante_test

import (
	"bytes"
	"reflect"
	"testing"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkante "github.com/cosmos/cosmos-sdk/x/auth/ante"

	"github.com/ararat-network/ark/app"
	"github.com/ararat-network/ark/app/ante"
	chain "github.com/ararat-network/ark/pkg/chain"
)

// decoratorNames qualifies each decorator by package path, because Ark's ante
// package and the SDK's are both named "ante" and %T cannot tell them apart.
func decoratorNames(decorators []sdk.AnteDecorator) []string {
	names := make([]string, len(decorators))
	for i, decorator := range decorators {
		t := reflect.TypeOf(decorator)
		// The Wasm decorators are registered as pointers; name what they point at.
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		names[i] = t.PkgPath() + "." + t.Name()
	}
	return names
}

// The ante order is consensus: it fixes the error code and the gas burned for
// every transaction that fails more than one check. This pins the whole chain
// so a reorder, an insertion, or a decorator silently dropped during an SDK
// upgrade fails here rather than at a chain split.
func TestAnteDecoratorOrder(t *testing.T) {
	arkApp := app.Setup(t, false)

	decorators := ante.NewAnteDecorators(
		arkApp.AppCodec(),
		arkApp.GetTxConfig(),
		arkApp.AccountKeeper,
		arkApp.BankKeeper,
		arkApp.FeeGrantKeeper,
		arkApp.StakingKeeper,
		arkApp.TreasuryKeeper,
		arkApp.IBCKeeper,
		arkApp.WasmKeeper.GetGasRegister(),
		wasmtypes.DefaultNodeConfig(),
		runtime.NewKVStoreService(arkApp.GetKey(wasmtypes.StoreKey)),
	)

	const (
		sdkAnte  = "github.com/cosmos/cosmos-sdk/x/auth/ante."
		wasmAnte = "github.com/CosmWasm/wasmd/x/wasm/keeper."
		arkAnte  = "github.com/ararat-network/ark/app/ante."
		ibcAnte  = "github.com/cosmos/ibc-go/v11/modules/core/ante."
	)

	require.Equal(t, []string{
		sdkAnte + "SetUpContextDecorator",

		wasmAnte + "LimitSimulationGasDecorator",
		wasmAnte + "CountTXDecorator",
		wasmAnte + "GasRegisterDecorator",
		wasmAnte + "TxContractsDecorator",

		sdkAnte + "RejectExtensionOptionsDecorator",
		sdkAnte + "ValidateBasicDecorator",
		sdkAnte + "TxTimeoutHeightDecorator",
		sdkAnte + "ValidateMemoDecorator",
		sdkAnte + "ConsumeTxSizeGasDecorator",

		// Message policy refuses before anything is charged.
		arkAnte + "GovVoteDecorator",
		arkAnte + "MultiSendDecorator",

		// Then the charges, in the order money moves.
		sdkAnte + "DeductFeeDecorator",
		arkAnte + "GasTallyDecorator",
		arkAnte + "StabilityTaxDecorator",

		sdkAnte + "SetPubKeyDecorator",
		sdkAnte + "ValidateSigCountDecorator",
		sdkAnte + "SigGasConsumeDecorator",
		sdkAnte + "SigVerificationDecorator",
		sdkAnte + "IncrementSequenceDecorator",

		ibcAnte + "RedundantRelayDecorator",
	}, decoratorNames(decorators))
}

// A transaction that fails both the fee gate and the tax charge reports the
// fee error, because the gate runs first and refuses without moving a balance.
// The reverse order charged an underpriced transaction its tax before refusing
// it, and reported insufficient funds for a fee problem. TestAnteDecoratorOrder
// pins that these two sit this way round in the production chain.
func TestBaseFeeRefusalPrecedesTheTaxCharge(t *testing.T) {
	arkApp, ctx, tx := setupTreasuryAnteTest(t)

	// A payer holding nothing: it can pay neither the fee nor the tax.
	tx.payer = sdk.AccAddress(bytes.Repeat([]byte{7}, 20))
	arkApp.AccountKeeper.SetAccount(ctx, arkApp.AccountKeeper.NewAccountWithAddress(ctx, tx.payer))

	// Priced below the consensus floor, so the gate refuses it too.
	tx.gas = 1_000_000
	tx.fee = sdk.NewCoins(sdk.NewInt64Coin(chain.USDBaseDenom, 1))

	handler := sdk.ChainAnteDecorators(
		sdkante.NewDeductFeeDecorator(
			arkApp.AccountKeeper,
			arkApp.BankKeeper,
			arkApp.FeeGrantKeeper,
			ante.NewBaseFeeChecker(arkApp.TreasuryKeeper),
		),
		ante.NewGasTallyDecorator(arkApp.TreasuryKeeper),
		ante.NewStabilityTaxDecorator(arkApp.TreasuryKeeper, arkApp.BankKeeper, arkApp.FeeGrantKeeper),
	)

	_, err := handler(ctx, tx, false)
	require.ErrorContains(t, err, "base fee requires")
	require.NotContains(t, err.Error(), "collecting stability tax")
}
