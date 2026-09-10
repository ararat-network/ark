// Package ante owns signed-transaction validation, fee deduction, and post-execution transfer tax.
// Its message validators also serve the execution-policy router.
package ante

import (
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	ibcante "github.com/cosmos/ibc-go/v11/modules/core/ante"
	ibckeeper "github.com/cosmos/ibc-go/v11/modules/core/keeper"

	"cosmossdk.io/core/store"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	feegrantkeeper "github.com/cosmos/cosmos-sdk/x/feegrant/keeper"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"

	"github.com/ararat-network/ark/app/mempool"
	treasurykeeper "github.com/ararat-network/ark/x/treasury/keeper"
)

// NewAnteHandler assembles the complete ante chain in consensus order. Message and fee policy
// precede deduction; signatures precede privilege checks. Ante failures discard its writes. See
// README.md for ordering and rollback contracts.
func NewAnteHandler(
	cdc codec.Codec,
	txConfig client.TxConfig,
	accountKeeper authkeeper.AccountKeeper,
	bankKeeper bankkeeper.BaseKeeper,
	feeGrantKeeper feegrantkeeper.Keeper,
	stakingKeeper *stakingkeeper.Keeper,
	treasuryKeeper *treasurykeeper.Keeper,
	privileges mempool.Set,
	ibcKeeper *ibckeeper.Keeper,
	wasmGasRegister wasmtypes.GasRegister,
	wasmNodeConfig wasmtypes.NodeConfig,
	wasmTxCounterStore store.KVStoreService,
) sdk.AnteHandler {
	return sdk.ChainAnteDecorators(
		// SetUpContext must be first: it installs the gas meter the decorators
		// below spend against and the panic recovery that turns running out of
		// gas into an error.
		sdkante.NewSetUpContextDecorator(),

		// Wasm context decorators follow setup so later checks meter their reads. CountTXDecorator
		// supplies the block transaction index used in contract addresses; another context setup
		// would reset their gas charges.
		wasmkeeper.NewLimitSimulationGasDecorator(wasmNodeConfig.SimulationGasLimit),
		wasmkeeper.NewCountTXDecorator(wasmTxCounterStore),
		wasmkeeper.NewGasRegisterDecorator(wasmGasRegister),
		wasmkeeper.NewTxContractsDecorator(),

		sdkante.NewExtensionOptionsDecorator(nil),
		sdkante.NewValidateBasicDecorator(),
		sdkante.NewTxTimeoutHeightDecorator(),
		sdkante.NewValidateMemoDecorator(accountKeeper),
		sdkante.NewConsumeGasForTxSizeDecorator(accountKeeper),

		// Consensus message policy stays before fee collection. The vote
		// floor also covers authz; priority-only eligibility is evaluated
		// separately after signature verification below.
		NewGovVoteDecorator(cdc, stakingKeeper),
		NewMultiSendDecorator(cdc),

		// FeeDecorator enforces Treasury's consensus base fee and the signed tax ceiling, then
		// deducts gas and the NOAH tip. The post handler collects tax after successful messages;
		// GasTally feeds the next block's base-fee update.
		NewFeeDecorator(accountKeeper, bankKeeper, feeGrantKeeper, treasuryKeeper),
		NewGasTallyDecorator(treasuryKeeper),

		// SetPubKey must precede every signature-verification decorator.
		sdkante.NewSetPubKeyDecorator(accountKeeper),
		sdkante.NewValidateSigCountDecorator(accountKeeper),
		sdkante.NewSigGasConsumeDecorator(accountKeeper, sdkante.DefaultSigVerificationGasConsumer),
		sdkante.NewSigVerificationDecorator(accountKeeper, txConfig.SignModeHandler()),
		NewPrivilegeDecorator(privileges),
		sdkante.NewIncrementSequenceDecorator(accountKeeper),

		// Last, as it was when it wrapped the chain from outside: a redundant
		// relay is only redundant once the transaction is otherwise valid.
		ibcante.NewRedundantRelayDecorator(ibcKeeper),
	)
}

// NewPostHandler collects the tax priced by FeeDecorator after successful messages. BaseApp commits
// or discards the post writes with the messages; the ante chain must include FeeDecorator.
func NewPostHandler(
	accountKeeper authkeeper.AccountKeeper,
	bankKeeper bankkeeper.BaseKeeper,
	feeGrantKeeper feegrantkeeper.Keeper,
) sdk.PostHandler {
	return sdk.ChainPostDecorators(NewTransferTaxDecorator(accountKeeper, bankKeeper, feeGrantKeeper))
}
