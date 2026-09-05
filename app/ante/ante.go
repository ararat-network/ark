// Package ante assembles the ante chain every signed transaction clears
// before its messages execute and the post chain that charges its transfer
// tax once they have, and owns the message policy both seams enforce: the
// decorators here for signed transactions, and package app's execution
// policy router for execution-generated messages.
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

	treasurykeeper "github.com/ararat-network/ark/x/treasury/keeper"
)

// NewAnteHandler assembles the ante chain, in order. The runtime's pre-defined
// handler is disabled in app_config (SkipAnteHandler), so this list is the
// whole chain, and its order is consensus: it fixes the error code and the gas
// burned for every transaction failing more than one check.
//
// The list is spelled out rather than taken from sdkante.NewAnteHandler because
// the Wasm decorators have to sit immediately after context setup and the SDK's
// constructor admits no insertion point. The SDK's own decorators keep the
// relative order of its v0.54.3 default list; an upgrade adding one must be
// mirrored here by hand.
//
// Ark's decorators follow one rule: a check that can refuse without moving a
// balance goes before any charge, and the charges follow in the order money
// moves — the gas fee here, inside FeeDecorator, and the transfer tax after
// the messages, inside NewPostHandler's TransferTaxDecorator. A refused
// transaction is then never charged first, and one failing both reports the
// standard fee error wallets already handle.
func NewAnteHandler(
	cdc codec.Codec,
	txConfig client.TxConfig,
	accountKeeper authkeeper.AccountKeeper,
	bankKeeper bankkeeper.BaseKeeper,
	feeGrantKeeper feegrantkeeper.Keeper,
	stakingKeeper *stakingkeeper.Keeper,
	treasuryKeeper *treasurykeeper.Keeper,
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

		// Wasm goes here, immediately after setup, because these establish
		// context the rest of the transaction is measured against.
		// CountTXDecorator is the load-bearing one: it stamps a per-block
		// transaction index that contract address derivation reads, so an
		// instantiation's address is a function of position in the block rather
		// than of anything a caller controls. It is also why this cannot simply
		// wrap the SDK handler from outside — a second SetUpContext would reset
		// the gas meter and make these store reads free.
		wasmkeeper.NewLimitSimulationGasDecorator(wasmNodeConfig.SimulationGasLimit),
		wasmkeeper.NewCountTXDecorator(wasmTxCounterStore),
		wasmkeeper.NewGasRegisterDecorator(wasmGasRegister),
		wasmkeeper.NewTxContractsDecorator(),

		sdkante.NewExtensionOptionsDecorator(nil),
		sdkante.NewValidateBasicDecorator(),
		sdkante.NewTxTimeoutHeightDecorator(),
		sdkante.NewValidateMemoDecorator(accountKeeper),
		sdkante.NewConsumeGasForTxSizeDecorator(accountKeeper),

		// Ark's message policy, before any charge because neither moves a
		// balance: a stake floor on votes, a fan-out cap and quadratic gas
		// surcharge on MultiSend. gov_vote.go and multisend.go own both,
		// authz recursion included; the policy router applies the same two to
		// execution-generated messages.
		NewGovVoteDecorator(cdc, stakingKeeper),
		NewMultiSendDecorator(cdc),

		// The ante half of the fee mechanism, fee.go's FeeDecorator in place
		// of the SDK's. It prices the transfer tax, holds the declared fee to
		// it — refused short, before anything is deducted, so a signer is
		// never taxed past what they signed — settles the fee by denomination
		// against Treasury's consensus base fee rather than node-local min
		// gas prices, a validity rule enforced in CheckTx and FinalizeBlock
		// alike so a proposer cannot include what every mempool would refuse,
		// then deducts the base fee and the NOAH tip to the fee collector.
		// The tax is charged after the messages by the post chain, on the
		// terms the policy router charges execution-generated messages, and
		// whether a payer can afford it is judged there rather than here.
		// Node-local minimum-gas-prices should be zero. The tally is the
		// controller's input: what cleared the gate reports its declared gas,
		// and Treasury's EndBlocker prices the next block from the total.
		NewFeeDecorator(accountKeeper, bankKeeper, feeGrantKeeper, treasuryKeeper),
		NewGasTallyDecorator(treasuryKeeper),

		// SetPubKey must precede every signature-verification decorator.
		sdkante.NewSetPubKeyDecorator(accountKeeper),
		sdkante.NewValidateSigCountDecorator(accountKeeper),
		sdkante.NewSigGasConsumeDecorator(accountKeeper, sdkante.DefaultSigVerificationGasConsumer),
		sdkante.NewSigVerificationDecorator(accountKeeper, txConfig.SignModeHandler()),
		sdkante.NewIncrementSequenceDecorator(accountKeeper),

		// Last, as it was when it wrapped the chain from outside: a redundant
		// relay is only redundant once the transaction is otherwise valid.
		ibcante.NewRedundantRelayDecorator(ibcKeeper),
	)
}

// NewPostHandler assembles the post chain, which BaseApp runs after a
// transaction's messages on their own branch, so what it writes commits with
// them and is discarded with them. One decorator: transfer_tax.go's
// TransferTaxDecorator, which charges the transfer tax FeeDecorator priced,
// held the fee to, and handed on through the context, once the messages
// have succeeded and never otherwise. The runtime installs no post chain of
// its own, so this is the whole of it, and it is only correct beside an ante
// chain carrying FeeDecorator.
func NewPostHandler(
	accountKeeper authkeeper.AccountKeeper,
	bankKeeper bankkeeper.BaseKeeper,
	feeGrantKeeper feegrantkeeper.Keeper,
) sdk.PostHandler {
	return sdk.ChainPostDecorators(NewTransferTaxDecorator(accountKeeper, bankKeeper, feeGrantKeeper))
}
